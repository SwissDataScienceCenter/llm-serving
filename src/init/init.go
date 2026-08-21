package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/urfave/cli/v3"
)

type OpenWebUi struct {
	Host          string   `koanf:"host"`
	AdminUser     string   `koanf:"admin_user"`
	AdminEmail    string   `koanf:"admin_email"`
	AdminPassword string   `koanf:"admin_password"`
	ModelIds      []string `koanf:"model_ids"`
	// Whether users may mint and use API keys. Tied to the gateway accepting the
	// identity JWT OpenWebUI forwards: without that, a key reaches OpenWebUI's own
	// API but every model call fails, so enabling it alone only widens the surface.
	EnableApiKeys bool `koanf:"enable_api_keys"`
}

// url builds an absolute OpenWebUI API URL. Plain http: the call is in-cluster.
func (o OpenWebUi) url(path string) string { return "http://" + o.Host + path }

type Config struct {
	Host      string    `koanf:"host"`
	OpenWebui OpenWebUi `koanf:"open_webui"`
}

var ErrUserExists error = errors.New("User already exists")

var k = koanf.New(".")

func main() {
	fmt.Println("Initializing OpenWebUI")
	var conf Config
	configPath, ok := os.LookupEnv("CONFIG_PATH")
	if !ok {
		configPath = "config.toml"
	}
	f := file.Provider(configPath)
	if err := k.Load(f, toml.Parser()); err != nil {
		log.Fatalf("error loading config: %v", err)
	}

	err := k.Load(env.Provider(".", env.Opt{
		Prefix: "INIT_",
		TransformFunc: func(k, v string) (string, any) {
			k = strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(k, "INIT_")), "__", ".")
			if strings.Contains(v, " ") {
				return k, strings.Split(v, " ")
			}
			return k, v
		},
	}), nil)
	if err != nil {
		log.Fatalf("couldn't load config from env: %v", err)
	}

	if err := k.UnmarshalWithConf("", &conf, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		log.Fatalf("couldn't unmarshal config: %v", err)
	}

	fmt.Println("config loaded")

	cmd := &cli.Command{
		Commands: []*cli.Command{
			{
				Name:  "init",
				Usage: "initialize openwebui admin and config",
				Action: func(context.Context, *cli.Command) error {
					return initOpenWebui(conf)
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func initOpenWebui(conf Config) error {
	fmt.Println("creating admin user")
	adminToken, err := createOpenWebuiAdmin(conf)
	if err != nil {
		if !errors.Is(err, ErrUserExists) {
			return err
		}
		// On upgrade
		fmt.Println("admin already exists, signing in to refresh config")
		adminToken, err = signinOpenWebuiAdmin(conf)
		if err != nil {
			return err
		}
	}

	// Runs on upgrades too: OpenWebUI stores this in its database, so env vars cannot
	// reach an instance that already booted. Each step is a fetch-mutate-post round
	// trip, idempotent on repeat but only because they are serialized -- running them
	// concurrently would make them overwrite each other's keys.
	fmt.Println("configuring openwebui")
	if err := setupOpenWebuiConfig(conf, adminToken); err != nil {
		return err
	}

	fmt.Println("granting users the api_keys feature")
	if err := setupUserPermissions(conf, adminToken); err != nil {
		return err
	}

	fmt.Println("setting up oauth and models")
	if err := setupOpenaiConfig(conf, adminToken); err != nil {
		return err
	}

	fmt.Println("initialization complete")
	return nil
}

func createOpenWebuiAdmin(conf Config) (string, error) {
	if conf.OpenWebui.AdminEmail == "" {
		return "", fmt.Errorf("admin email not set")
	}
	if conf.OpenWebui.AdminPassword == "" {
		return "", fmt.Errorf("admin password not set")
	}

	signupURL := conf.OpenWebui.url("/api/v1/auths/signup")
	res, err := postAuth(signupURL, map[string]string{
		"name":     conf.OpenWebui.AdminUser,
		"email":    conf.OpenWebui.AdminEmail,
		"password": conf.OpenWebui.AdminPassword,
	})
	if err != nil {
		return "", fmt.Errorf("user creation request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == 403 {
			return "", ErrUserExists
		}
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("user creation failed (status %d): %s", res.StatusCode, string(body))
	}

	return tokenFromResponse(res)
}

func signinOpenWebuiAdmin(conf Config) (string, error) {
	signinURL := conf.OpenWebui.url("/api/v1/auths/signin")
	res, err := postAuth(signinURL, map[string]string{
		"email":    conf.OpenWebui.AdminEmail,
		"password": conf.OpenWebui.AdminPassword,
	})
	if err != nil {
		return "", fmt.Errorf("signin request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("signin failed (status %d): %s", res.StatusCode, string(body))
	}

	return tokenFromResponse(res)
}

func postAuth(url string, data map[string]string) (*http.Response, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("data marshal failed: %w", err)
	}
	return http.Post(url, "application/json", bytes.NewReader(payload))
}

func tokenFromResponse(res *http.Response) (string, error) {
	var userdata struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&userdata); err != nil {
		return "", fmt.Errorf("userdata parse failed: %w", err)
	}
	if userdata.Token == "" {
		return "", fmt.Errorf("token not in response")
	}
	return userdata.Token, nil
}

// configRoundTrip fetches a JSON config document, applies mutate and posts the
// result back. OpenWebUI's config endpoints replace the whole document instead of
// merging, so keys the mutation does not touch have to be carried over from the GET.
func configRoundTrip(getURL, postURL, adminToken string, mutate func(map[string]any) error) error {
	// Without a timeout an unresponsive OpenWebUI wedges the init Job forever, and
	// the Job has no activeDeadlineSeconds to cut it short.
	client := http.Client{Timeout: 30 * time.Second}
	auth := fmt.Sprintf("Bearer %s", adminToken)

	getReq, err := http.NewRequest("GET", getURL, nil)
	if err != nil {
		return fmt.Errorf("GET request creation failed: %w", err)
	}
	getReq.Header.Set("Authorization", auth)

	getResp, err := client.Do(getReq)
	if err != nil {
		return fmt.Errorf("config fetch failed: %w", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getResp.Body)
		return fmt.Errorf("config fetch failed (status %d): %s", getResp.StatusCode, string(body))
	}

	var config map[string]any
	if err := json.NewDecoder(getResp.Body).Decode(&config); err != nil {
		return fmt.Errorf("config parse failed: %w", err)
	}

	if err := mutate(config); err != nil {
		return fmt.Errorf("config mutation failed: %w", err)
	}

	payload, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("config marshal failed: %w", err)
	}

	updateReq, err := http.NewRequest("POST", postURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("UPDATE request creation failed: %w", err)
	}
	updateReq.Header.Set("Authorization", auth)
	updateReq.Header.Set("Content-Type", "application/json")

	updateResp, err := client.Do(updateReq)
	if err != nil {
		return fmt.Errorf("config update failed: %w", err)
	}
	defer updateResp.Body.Close()

	if updateResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(updateResp.Body)
		return fmt.Errorf("config update failed (status %d): %s", updateResp.StatusCode, string(body))
	}

	return nil
}

func setupOpenWebuiConfig(conf Config, adminToken string) error {
	configURL := conf.OpenWebui.url("/api/v1/auths/admin/config")
	return configRoundTrip(configURL, configURL, adminToken, func(config map[string]any) error {
		config["DEFAULT_USER_ROLE"] = "user"
		// Gates both minting and presenting an sk- key, and defaults to off.
		config["ENABLE_API_KEYS"] = conf.OpenWebui.EnableApiKeys
		return nil
	})
}

// setupUserPermissions grants non-admin users the api_keys feature. Admins bypass
// the permission check, so without this only the admin account could use a key.
func setupUserPermissions(conf Config, adminToken string) error {
	permsURL := conf.OpenWebui.url("/api/v1/users/default/permissions")
	return configRoundTrip(permsURL, permsURL, adminToken, func(perms map[string]any) error {
		features, found := perms["features"]
		if !found {
			features = make(map[string]any)
			perms["features"] = features
		}
		// Replacing a features map we failed to recognise would silently drop every
		// other permission in it, so refuse rather than guess.
		grants, ok := features.(map[string]any)
		if !ok {
			return fmt.Errorf("features permission is %T, want an object", features)
		}
		grants["api_keys"] = conf.OpenWebui.EnableApiKeys
		return nil
	})
}

func setupOpenaiConfig(conf Config, adminToken string) error {
	return configRoundTrip(
		conf.OpenWebui.url("/openai/config"),
		conf.OpenWebui.url("/openai/config/update"),
		adminToken,
		func(config map[string]any) error {
			// No Authorization header upstream: the gateway identifies the caller from
			// the signed per-user JWT OpenWebUI forwards alongside the request.
			// Keys must be the connection's index as a string; others are dropped.
			config["OPENAI_API_CONFIGS"] = map[string]any{
				"0": map[string]any{
					"auth_type":       "none",
					"model_ids":       conf.OpenWebui.ModelIds,
					"enabled":         true,
					"connection_type": "external",
				},
			}
			return nil
		},
	)
}

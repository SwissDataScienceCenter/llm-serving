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
}

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
		if errors.Is(err, ErrUserExists) {
			// On upgrade
			fmt.Println("admin already exists, signing in to refresh model config")
			adminToken, err = signinOpenWebuiAdmin(conf)
			if err != nil {
				return err
			}
			return setupOpenaiConfig(conf, adminToken)
		}
		return err
	}

	fmt.Println("configuring openwebui")
	err = setupOpenWebuiConfig(conf, adminToken)
	if err != nil {
		return err
	}

	fmt.Println("setting up oauth and models")
	err = setupOpenaiConfig(conf, adminToken)
	if err != nil {
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

	signupURL := fmt.Sprintf("http://%s/api/v1/auths/signup", conf.OpenWebui.Host)
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
	signinURL := fmt.Sprintf("http://%s/api/v1/auths/signin", conf.OpenWebui.Host)
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

func setupOpenWebuiConfig(conf Config, adminToken string) error {
	configURL := fmt.Sprintf("http://%s/api/v1/auths/admin/config", conf.OpenWebui.Host)
	getReq, err := http.NewRequest("GET", configURL, nil)
	if err != nil {
		return fmt.Errorf("GET request creation failed: %w", err)
	}
	getReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", adminToken))

	client := http.Client{}
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

	config["DEFAULT_USER_ROLE"] = "user"

	updateURL := fmt.Sprintf("http://%s/api/v1/auths/admin/config", conf.OpenWebui.Host)
	payload, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("config marshal failed: %w", err)
	}

	updateReq, err := http.NewRequest("POST", updateURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("UPDATE request creation failed: %w", err)
	}
	updateReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", adminToken))
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

func setupOpenaiConfig(conf Config, adminToken string) error {
	configURL := fmt.Sprintf("http://%s/openai/config", conf.OpenWebui.Host)
	getReq, err := http.NewRequest("GET", configURL, nil)
	if err != nil {
		return fmt.Errorf("GET request creation failed: %w", err)
	}
	getReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", adminToken))

	client := http.Client{}
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

	// Configure OpenWebUI to use OAuth authentication for the gateway
	fmt.Println("setting provider auth type")
	config["OPENAI_API_CONFIGS"] = make(map[int]any)
	api_conf := config["OPENAI_API_CONFIGS"].(map[int]any)
	c := make(map[string]any)

	c["auth_type"] = "system_oauth"
	c["model_ids"] = conf.OpenWebui.ModelIds
	c["enabled"] = true
	c["connection_type"] = "external"
	api_conf[0] = c

	updateURL := fmt.Sprintf("http://%s/openai/config/update", conf.OpenWebui.Host)
	payload, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("config marshal failed: %w", err)
	}

	updateReq, err := http.NewRequest("POST", updateURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("UPDATE request creation failed: %w", err)
	}
	updateReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", adminToken))
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

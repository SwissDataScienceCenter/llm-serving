package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// configServer stands in for OpenWebUI: it serves current on GET and captures
// whatever gets posted back.
type configServer struct {
	current map[string]any
	posted  map[string]any
	authSaw string
}

// start serves the config and returns the server's host:port.
func (c *configServer) start(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.authSaw = r.Header.Get("Authorization")
		switch r.Method {
		case "GET":
			if err := json.NewEncoder(w).Encode(c.current); err != nil {
				t.Errorf("encoding current config: %v", err)
			}
		case "POST":
			if err := json.NewDecoder(r.Body).Decode(&c.posted); err != nil {
				t.Errorf("posted body is not JSON: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// The config endpoints replace the whole document, so anything the mutation does
// not touch has to survive the round trip or unrelated settings get reset.
func TestConfigRoundTripPreservesUntouchedKeys(t *testing.T) {
	srv := &configServer{current: map[string]any{
		"DEFAULT_USER_ROLE": "pending",
		"UNRELATED":         "keep-me",
		"NESTED":            map[string]any{"a": float64(1)},
	}}
	url := "http://" + srv.start(t)

	err := configRoundTrip(url, url, "tok", func(config map[string]any) error {
		config["DEFAULT_USER_ROLE"] = "user"
		return nil
	})
	if err != nil {
		t.Fatalf("configRoundTrip: %v", err)
	}

	if srv.posted["UNRELATED"] != "keep-me" {
		t.Errorf("UNRELATED dropped, got %v", srv.posted["UNRELATED"])
	}
	if nested, ok := srv.posted["NESTED"].(map[string]any); !ok || nested["a"] != float64(1) {
		t.Errorf("NESTED dropped, got %v", srv.posted["NESTED"])
	}
	if srv.posted["DEFAULT_USER_ROLE"] != "user" {
		t.Errorf("mutation not applied, got %v", srv.posted["DEFAULT_USER_ROLE"])
	}
	if srv.authSaw != "Bearer tok" {
		t.Errorf("admin token not sent, got %q", srv.authSaw)
	}
}

func TestConfigRoundTripReportsFetchFailure(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	t.Cleanup(s.Close)

	err := configRoundTrip(s.URL, s.URL, "tok", func(map[string]any) error {
		t.Error("mutate must not run when the fetch fails")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("want an error naming the status, got %v", err)
	}
}

func TestSetupOpenWebuiConfigGatesApiKeys(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			srv := &configServer{current: map[string]any{"ENABLE_API_KEYS": !enabled}}
			conf := Config{OpenWebui: OpenWebUi{Host: srv.start(t), EnableApiKeys: enabled}}

			if err := setupOpenWebuiConfig(conf, "tok"); err != nil {
				t.Fatalf("setupOpenWebuiConfig: %v", err)
			}
			if srv.posted["ENABLE_API_KEYS"] != enabled {
				t.Errorf("ENABLE_API_KEYS is %v, want %v", srv.posted["ENABLE_API_KEYS"], enabled)
			}
			if srv.posted["DEFAULT_USER_ROLE"] != "user" {
				t.Errorf("DEFAULT_USER_ROLE not set, got %v", srv.posted["DEFAULT_USER_ROLE"])
			}
		})
	}
}

// features is absent on some responses and present on others; either way the grant
// must land without clobbering sibling permission groups.
func TestSetupUserPermissionsGrantsApiKeys(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current map[string]any
	}{
		{"features absent", map[string]any{"workspace": map[string]any{"models": true}}},
		{"features present", map[string]any{
			"workspace": map[string]any{"models": true},
			"features":  map[string]any{"web_search": true, "api_keys": false},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := &configServer{current: tc.current}
			conf := Config{OpenWebui: OpenWebUi{Host: srv.start(t), EnableApiKeys: true}}

			if err := setupUserPermissions(conf, "tok"); err != nil {
				t.Fatalf("setupUserPermissions: %v", err)
			}
			features, ok := srv.posted["features"].(map[string]any)
			if !ok {
				t.Fatalf("features missing from posted permissions: %v", srv.posted)
			}
			if features["api_keys"] != true {
				t.Errorf("api_keys not granted, got %v", features["api_keys"])
			}
			if ws, ok := srv.posted["workspace"].(map[string]any); !ok || ws["models"] != true {
				t.Errorf("sibling permission group lost, got %v", srv.posted["workspace"])
			}
			if _, had := tc.current["features"]; had && features["web_search"] != true {
				t.Errorf("existing feature lost, got %v", features)
			}
		})
	}
}

// Replacing a features value of an unexpected shape would drop every permission in
// it, so the grant must refuse instead.
func TestSetupUserPermissionsRejectsWrongTypedFeatures(t *testing.T) {
	srv := &configServer{current: map[string]any{"features": "not-an-object"}}
	conf := Config{OpenWebui: OpenWebUi{Host: srv.start(t), EnableApiKeys: true}}

	err := setupUserPermissions(conf, "tok")
	if err == nil {
		t.Fatalf("want an error, got nil (posted %v)", srv.posted)
	}
	if !strings.Contains(err.Error(), "want an object") {
		t.Errorf("error should name the problem, got %v", err)
	}
	if srv.posted != nil {
		t.Errorf("permissions were posted anyway: %v", srv.posted)
	}
}

// The gateway needs no Authorization header from OpenWebUI, and OpenWebUI drops
// config entries whose key is not the connection's index as a string.
func TestSetupOpenaiConfigUsesNoAuthAndStringIndex(t *testing.T) {
	srv := &configServer{current: map[string]any{
		"ENABLE_OPENAI_API":    true,
		"OPENAI_API_BASE_URLS": []any{"https://gateway.example.invalid/v1"},
		"OPENAI_API_KEYS":      []any{""},
		"OPENAI_API_CONFIGS":   map[string]any{},
	}}
	conf := Config{OpenWebui: OpenWebUi{Host: srv.start(t), ModelIds: []string{"org/model-a"}}}

	if err := setupOpenaiConfig(conf, "tok"); err != nil {
		t.Fatalf("setupOpenaiConfig: %v", err)
	}

	configs, ok := srv.posted["OPENAI_API_CONFIGS"].(map[string]any)
	if !ok {
		t.Fatalf("OPENAI_API_CONFIGS missing: %v", srv.posted)
	}
	conn, ok := configs["0"].(map[string]any)
	if !ok {
		t.Fatalf(`connection must be keyed "0", got keys %v`, configs)
	}
	if conn["auth_type"] != "none" {
		t.Errorf("auth_type is %v, want none", conn["auth_type"])
	}
	if conn["connection_type"] != "external" || conn["enabled"] != true {
		t.Errorf("connection flags wrong: %v", conn)
	}
	models, ok := conn["model_ids"].([]any)
	if !ok || len(models) != 1 || models[0] != "org/model-a" {
		t.Errorf("model_ids wrong: %v", conn["model_ids"])
	}
	// update_config rejects a payload without this, so the round trip has to carry it.
	if srv.posted["ENABLE_OPENAI_API"] != true {
		t.Errorf("ENABLE_OPENAI_API lost: %v", srv.posted["ENABLE_OPENAI_API"])
	}
}

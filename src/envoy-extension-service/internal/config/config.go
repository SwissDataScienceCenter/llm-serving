// Package config provides configuration loading via koanf.
package config

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config holds the runtime configuration for the extension service.
type Config struct {
	GRPC struct {
		Host string `koanf:"host"`
		Port string `koanf:"port"`
	} `koanf:"grpc"`

	OpenMeter struct {
		URL        string `koanf:"url"`
		ApiSecret  string `koanf:"api_secret"`
		FeatureKey string `koanf:"feature_key"`
	} `koanf:"openmeter"`
}

// Load reads configuration from an optional file and environment variables,
// with environment variables taking precedence over file values.
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	// File (optional). If path is empty, only env is used.
	if path != "" {
		if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
			return nil, fmt.Errorf("loading config file %q: %w", path, err)
		}
	}

	// Environment variables. Env vars override file values.
	// e.g. GRPC__HOST -> grpc.host, OPENMETER__API_SECRET -> openmeter.api_secret
	if err := k.Load(env.Provider(".", env.Opt{
		Prefix: "ENVOYEXT__",
		TransformFunc: func(s, v string) (string, any) {
			return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, "ENVOYEXT__")), "__", "."), v
		},
	}), nil); err != nil {
		return nil, fmt.Errorf("loading env config: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}
	return &cfg, nil
}

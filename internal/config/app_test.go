package config_test

import (
	"strings"
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/config"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

var appEnvKeys = []string{"APP_ENV", "APP_SERVICE", "APP_VERSION", "APP_PUBLIC_URL"}

func TestLoadApp_Defaults(t *testing.T) {
	testkit.UnsetEnv(t, appEnvKeys...)

	cfg, err := config.LoadApp()
	if err != nil {
		t.Fatalf("LoadApp() error = %v", err)
	}
	if cfg.Environment != "local" {
		t.Fatalf("Environment = %q, want %q", cfg.Environment, "local")
	}
	if cfg.Service != "api" {
		t.Fatalf("Service = %q, want %q", cfg.Service, "api")
	}
	if cfg.Version != "dev" {
		t.Fatalf("Version = %q, want %q", cfg.Version, "dev")
	}
	if cfg.PublicURL != "http://localhost:3000" {
		t.Fatalf("PublicURL = %q, want %q", cfg.PublicURL, "http://localhost:3000")
	}
}

func TestLoadApp_CustomValues(t *testing.T) {
	testkit.UnsetEnv(t, appEnvKeys...)

	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_SERVICE", "worker")
	t.Setenv("APP_VERSION", "1.2.3")

	cfg, err := config.LoadApp()
	if err != nil {
		t.Fatalf("LoadApp() error = %v", err)
	}
	if cfg.Environment != "production" {
		t.Fatalf("Environment = %q, want %q", cfg.Environment, "production")
	}
	if cfg.Service != "worker" {
		t.Fatalf("Service = %q, want %q", cfg.Service, "worker")
	}
	if cfg.Version != "1.2.3" {
		t.Fatalf("Version = %q, want %q", cfg.Version, "1.2.3")
	}
}

func TestLoadApp_PublicURL(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"origin", "https://app.example.com", "https://app.example.com"},
		// Trimmed so a caller appending "/verify-email" never produces "//".
		{"trailing_slash_trimmed", "https://app.example.com/", "https://app.example.com"},
		{"base_path_kept", "https://example.com/app/", "https://example.com/app"},
		{"local_port", "http://localhost:5173", "http://localhost:5173"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testkit.UnsetEnv(t, appEnvKeys...)
			t.Setenv("APP_PUBLIC_URL", tc.value)

			cfg, err := config.LoadApp()
			if err != nil {
				t.Fatalf("LoadApp() error = %v", err)
			}
			if cfg.PublicURL != tc.want {
				t.Fatalf("PublicURL = %q, want %q", cfg.PublicURL, tc.want)
			}
		})
	}
}

// TestLoadApp_PublicURL_RejectsUnusableValues covers values that would
// build a broken or misleading emailed link, so they fail startup with
// the variable named instead.
func TestLoadApp_PublicURL_RejectsUnusableValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"missing_scheme", "app.example.com"},
		{"unsupported_scheme", "ftp://app.example.com"},
		{"missing_host", "https://"},
		{"query", "https://app.example.com?ref=mail"},
		{"empty_query", "https://app.example.com?"},
		{"fragment", "https://app.example.com/#/home"},
		{"credentials", "https://user:pass@app.example.com"},
		{"unparseable", "https://app.example.com:port"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testkit.UnsetEnv(t, appEnvKeys...)
			t.Setenv("APP_PUBLIC_URL", tc.value)

			_, err := config.LoadApp()
			if err == nil {
				t.Fatalf("LoadApp() error = nil, want error for APP_PUBLIC_URL=%q", tc.value)
			}
			if !strings.Contains(err.Error(), "APP_PUBLIC_URL") {
				t.Fatalf("LoadApp() error = %v, want it to name APP_PUBLIC_URL", err)
			}
		})
	}
}

package config_test

import (
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/config"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestLoadLog_Default(t *testing.T) {
	testkit.UnsetEnv(t, "LOG_LEVEL")

	cfg, err := config.LoadLog()
	if err != nil {
		t.Fatalf("LoadLog() error = %v", err)
	}
	if cfg.Level != "info" {
		t.Fatalf("Level = %q, want %q", cfg.Level, "info")
	}
}

func TestLoadLog_ValidLevels(t *testing.T) {
	tests := []struct {
		env  string
		want string
	}{
		{"debug", "debug"},
		{"INFO", "info"},
		{"Warn", "warn"},
		{"error", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			testkit.UnsetEnv(t, "LOG_LEVEL")
			t.Setenv("LOG_LEVEL", tt.env)

			cfg, err := config.LoadLog()
			if err != nil {
				t.Fatalf("LoadLog() error = %v", err)
			}
			if cfg.Level != tt.want {
				t.Fatalf("Level = %q, want %q", cfg.Level, tt.want)
			}
		})
	}
}

func TestLoadLog_InvalidLevel(t *testing.T) {
	testkit.UnsetEnv(t, "LOG_LEVEL")
	t.Setenv("LOG_LEVEL", "verbose")

	if _, err := config.LoadLog(); err == nil {
		t.Fatalf("LoadLog() error = nil, want error for invalid level")
	}
}

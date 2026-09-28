package database_test

import (
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/platform/database"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

var dbEnvKeys = []string{
	"DB_DSN",
	"DB_MAX_CONNS",
	"DB_MIN_IDLE_CONNS",
	"DB_MAX_CONN_LIFETIME",
	"DB_MAX_CONN_LIFETIME_JITTER",
	"DB_MAX_CONN_IDLE_TIME",
	"DB_HEALTH_CHECK_PERIOD",
	"DB_CONNECT_TIMEOUT",
	"DB_PING_TIMEOUT",
}

const validDSN = "postgres://postgres:postgres@localhost:5432/app?sslmode=disable"

func TestLoadConfig_RequiresDSN(t *testing.T) {
	testkit.UnsetEnv(t, dbEnvKeys...)

	if _, err := database.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for missing DB_DSN")
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	testkit.UnsetEnv(t, dbEnvKeys...)
	t.Setenv("DB_DSN", validDSN)

	cfg, err := database.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.DSN != validDSN {
		t.Fatalf("DSN = %q, want %q", cfg.DSN, validDSN)
	}
	if cfg.MaxConns != 10 {
		t.Fatalf("MaxConns = %d, want %d", cfg.MaxConns, 10)
	}
	if cfg.MinIdleConns != 2 {
		t.Fatalf("MinIdleConns = %d, want %d", cfg.MinIdleConns, 2)
	}
	if cfg.MaxConnLifetime != time.Hour {
		t.Fatalf("MaxConnLifetime = %v, want %v", cfg.MaxConnLifetime, time.Hour)
	}
}

func TestLoadConfig_InvalidMaxConns(t *testing.T) {
	testkit.UnsetEnv(t, dbEnvKeys...)
	t.Setenv("DB_DSN", validDSN)
	t.Setenv("DB_MAX_CONNS", "0")

	if _, err := database.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for DB_MAX_CONNS=0")
	}
}

func TestLoadConfig_MinIdleExceedsMaxConns(t *testing.T) {
	testkit.UnsetEnv(t, dbEnvKeys...)
	t.Setenv("DB_DSN", validDSN)
	t.Setenv("DB_MAX_CONNS", "5")
	t.Setenv("DB_MIN_IDLE_CONNS", "10")

	if _, err := database.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error when DB_MIN_IDLE_CONNS > DB_MAX_CONNS")
	}
}

func TestLoadConfig_NegativeMinIdleConns(t *testing.T) {
	testkit.UnsetEnv(t, dbEnvKeys...)
	t.Setenv("DB_DSN", validDSN)
	t.Setenv("DB_MIN_IDLE_CONNS", "-1")

	if _, err := database.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for negative DB_MIN_IDLE_CONNS")
	}
}

func TestLoadConfig_InvalidDurations(t *testing.T) {
	tests := []string{
		"DB_MAX_CONN_LIFETIME",
		"DB_MAX_CONN_LIFETIME_JITTER",
		"DB_MAX_CONN_IDLE_TIME",
		"DB_HEALTH_CHECK_PERIOD",
		"DB_CONNECT_TIMEOUT",
		"DB_PING_TIMEOUT",
	}

	for _, key := range tests {
		t.Run(key, func(t *testing.T) {
			testkit.UnsetEnv(t, dbEnvKeys...)
			t.Setenv("DB_DSN", validDSN)
			t.Setenv(key, "0s")

			if _, err := database.LoadConfig(); err == nil {
				t.Fatalf("LoadConfig() error = nil, want error for %s=0s", key)
			}
		})
	}
}

func TestLoadConfig_MalformedDuration(t *testing.T) {
	testkit.UnsetEnv(t, dbEnvKeys...)
	t.Setenv("DB_DSN", validDSN)
	t.Setenv("DB_CONNECT_TIMEOUT", "not-a-duration")

	if _, err := database.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for malformed duration")
	}
}

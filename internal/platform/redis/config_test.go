package redis_test

import (
	"strings"
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/platform/redis"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestLoadConfig_Default(t *testing.T) {
	testkit.UnsetEnv(t, "REDIS_URL")

	cfg, err := redis.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.URL != "redis://localhost:6379/0" {
		t.Fatalf("URL = %q, want %q", cfg.URL, "redis://localhost:6379/0")
	}
}

// TestLoadConfig_AppliesTimeoutDefaults is the point of Options: every
// timeout must be bounded, because a Redis call that can block forever
// turns an optional dependency into a way to hang request handling.
func TestLoadConfig_AppliesTimeoutDefaults(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")

	cfg, err := redis.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	opts, err := cfg.Options()
	if err != nil {
		t.Fatalf("Options() error = %v", err)
	}

	timeouts := map[string]time.Duration{
		"DialTimeout":  opts.DialTimeout,
		"ReadTimeout":  opts.ReadTimeout,
		"WriteTimeout": opts.WriteTimeout,
		"PoolTimeout":  opts.PoolTimeout,
	}

	for name, value := range timeouts {
		if value <= 0 {
			t.Fatalf("%s = %s, want a bounded positive default", name, value)
		}
	}
}

// TestLoadConfig_URLQueryParametersOverrideDefaults covers the reason
// there is no environment variable per timeout: go-redis's own URL
// parser already accepts them.
func TestLoadConfig_URLQueryParametersOverrideDefaults(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://localhost:6379/0?read_timeout=5s&pool_size=42")

	cfg, err := redis.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	opts, err := cfg.Options()
	if err != nil {
		t.Fatalf("Options() error = %v", err)
	}

	if opts.ReadTimeout != 5*time.Second {
		t.Fatalf("ReadTimeout = %s, want the URL's 5s", opts.ReadTimeout)
	}
	if opts.PoolSize != 42 {
		t.Fatalf("PoolSize = %d, want the URL's 42", opts.PoolSize)
	}
}

func TestLoadConfig_ParsesDatabaseIndex(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://localhost:6379/3")

	cfg, err := redis.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	opts, err := cfg.Options()
	if err != nil {
		t.Fatalf("Options() error = %v", err)
	}

	if opts.DB != 3 {
		t.Fatalf("DB = %d, want 3", opts.DB)
	}
}

func TestLoadConfig_RejectsMalformedURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"wrong_scheme", "http://localhost:6379"},
		{"missing_scheme", "//localhost:6379"},
		{"not_a_url", "localhost:6379"},
		{"bad_db_index", "redis://localhost:6379/not-a-number"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REDIS_URL", tc.url)

			if _, err := redis.LoadConfig(); err == nil {
				t.Fatalf("LoadConfig() error = nil, want a startup failure for %q", tc.url)
			}
		})
	}
}

// TestLoadConfig_ErrorDoesNotLeakCredentials matters because REDIS_URL
// may embed a password and this error is what gets logged when a
// process fails to start.
func TestLoadConfig_ErrorDoesNotLeakCredentials(t *testing.T) {
	const password = "sup3rs3cr3t"

	t.Setenv("REDIS_URL", "redis://user:"+password+"@localhost:6379/not-a-number")

	_, err := redis.LoadConfig()
	if err == nil {
		t.Fatalf("LoadConfig() error = nil, want a failure")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("error contains the Redis password: %v", err)
	}
}

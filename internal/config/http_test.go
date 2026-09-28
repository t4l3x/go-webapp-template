package config_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/t4l3x/go-webapp-template/internal/config"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

var httpEnvKeys = []string{
	"HTTP_PORT",
	"HTTP_SHUTDOWN_TIMEOUT",
	"HTTP_CORS_ALLOWED_ORIGINS",
	"HTTP_TRUSTED_PROXIES",
}

func TestLoadHTTP_Defaults(t *testing.T) {
	testkit.UnsetEnv(t, httpEnvKeys...)

	cfg, err := config.LoadHTTP()
	if err != nil {
		t.Fatalf("LoadHTTP() error = %v", err)
	}

	if cfg.Port != 8080 {
		t.Fatalf("Port = %d, want %d", cfg.Port, 8080)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, 10*time.Second)
	}
	if len(cfg.CORSAllowedOrigins) != 0 {
		t.Fatalf("CORSAllowedOrigins = %v, want empty", cfg.CORSAllowedOrigins)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Fatalf("TrustedProxies = %v, want empty", cfg.TrustedProxies)
	}
}

func TestLoadHTTP_CustomValues(t *testing.T) {
	testkit.UnsetEnv(t, httpEnvKeys...)

	t.Setenv("HTTP_PORT", "9090")
	t.Setenv("HTTP_SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("HTTP_CORS_ALLOWED_ORIGINS", "https://a.example,https://b.example")

	cfg, err := config.LoadHTTP()
	if err != nil {
		t.Fatalf("LoadHTTP() error = %v", err)
	}

	if cfg.Port != 9090 {
		t.Fatalf("Port = %d, want %d", cfg.Port, 9090)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Fatalf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, 30*time.Second)
	}

	want := []string{"https://a.example", "https://b.example"}
	if len(cfg.CORSAllowedOrigins) != len(want) {
		t.Fatalf("CORSAllowedOrigins = %v, want %v", cfg.CORSAllowedOrigins, want)
	}
	for i := range want {
		if cfg.CORSAllowedOrigins[i] != want[i] {
			t.Fatalf("CORSAllowedOrigins = %v, want %v", cfg.CORSAllowedOrigins, want)
		}
	}
}

func TestLoadHTTP_InvalidPort(t *testing.T) {
	tests := []string{"0", "-1", "70000"}

	for _, port := range tests {
		t.Run(port, func(t *testing.T) {
			testkit.UnsetEnv(t, httpEnvKeys...)
			t.Setenv("HTTP_PORT", port)

			if _, err := config.LoadHTTP(); err == nil {
				t.Fatalf("LoadHTTP() error = nil, want error for port %q", port)
			}
		})
	}
}

func TestLoadHTTP_InvalidShutdownTimeout(t *testing.T) {
	testkit.UnsetEnv(t, httpEnvKeys...)
	t.Setenv("HTTP_SHUTDOWN_TIMEOUT", "0s")

	if _, err := config.LoadHTTP(); err == nil {
		t.Fatalf("LoadHTTP() error = nil, want error for zero shutdown timeout")
	}
}

func TestLoadHTTP_TrustedProxies(t *testing.T) {
	testkit.UnsetEnv(t, httpEnvKeys...)
	t.Setenv("HTTP_TRUSTED_PROXIES", "10.0.0.0/8,203.0.113.5/32")

	cfg, err := config.LoadHTTP()
	if err != nil {
		t.Fatalf("LoadHTTP() error = %v", err)
	}

	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("203.0.113.5/32"),
	}
	if len(cfg.TrustedProxies) != len(want) {
		t.Fatalf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}
	for i := range want {
		if cfg.TrustedProxies[i] != want[i] {
			t.Fatalf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
		}
	}
}

func TestLoadHTTP_TrustedProxies_RequiresCIDRNotation(t *testing.T) {
	testkit.UnsetEnv(t, httpEnvKeys...)
	t.Setenv("HTTP_TRUSTED_PROXIES", "203.0.113.5")

	if _, err := config.LoadHTTP(); err == nil {
		t.Fatalf("LoadHTTP() error = nil, want error for a bare IP without CIDR notation")
	}
}

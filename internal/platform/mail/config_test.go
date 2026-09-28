package mail_test

import (
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/mail"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

var mailEnvKeys = []string{
	"MAIL_HOST",
	"MAIL_PORT",
	"MAIL_USERNAME",
	"MAIL_PASSWORD",
	"MAIL_FROM",
	"MAIL_TLS_POLICY",
	"MAIL_TIMEOUT",
}

func TestLoadConfig_Defaults(t *testing.T) {
	testkit.UnsetEnv(t, mailEnvKeys...)

	cfg, err := mail.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.Host != "localhost" {
		t.Fatalf("Host = %q, want %q", cfg.Host, "localhost")
	}
	if cfg.Port != 1025 {
		t.Fatalf("Port = %d, want %d", cfg.Port, 1025)
	}
	if cfg.From != "no-reply@go-webapp-template.local" {
		t.Fatalf("From = %q, want %q", cfg.From, "no-reply@go-webapp-template.local")
	}
	if cfg.TLSPolicy != mail.TLSPolicyNone {
		t.Fatalf("TLSPolicy = %q, want %q by default (Mailpit does not speak TLS)", cfg.TLSPolicy, mail.TLSPolicyNone)
	}
}

func TestLoadConfig_CustomValues(t *testing.T) {
	testkit.UnsetEnv(t, mailEnvKeys...)

	t.Setenv("MAIL_HOST", "smtp.example.com")
	t.Setenv("MAIL_PORT", "587")
	t.Setenv("MAIL_USERNAME", "user")
	t.Setenv("MAIL_PASSWORD", "pass")
	t.Setenv("MAIL_FROM", "alerts@example.com")
	t.Setenv("MAIL_TLS_POLICY", "starttls")

	cfg, err := mail.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.Host != "smtp.example.com" {
		t.Fatalf("Host = %q, want %q", cfg.Host, "smtp.example.com")
	}
	if cfg.Port != 587 {
		t.Fatalf("Port = %d, want %d", cfg.Port, 587)
	}
	if cfg.TLSPolicy != mail.TLSPolicySTARTTLS {
		t.Fatalf("TLSPolicy = %q, want %q", cfg.TLSPolicy, mail.TLSPolicySTARTTLS)
	}
}

func TestLoadConfig_TLSPolicyImplicit(t *testing.T) {
	testkit.UnsetEnv(t, mailEnvKeys...)
	t.Setenv("MAIL_TLS_POLICY", "implicit")

	cfg, err := mail.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.TLSPolicy != mail.TLSPolicyImplicit {
		t.Fatalf("TLSPolicy = %q, want %q", cfg.TLSPolicy, mail.TLSPolicyImplicit)
	}
}

func TestLoadConfig_InvalidTLSPolicyRejected(t *testing.T) {
	testkit.UnsetEnv(t, mailEnvKeys...)
	t.Setenv("MAIL_TLS_POLICY", "yes-please")

	if _, err := mail.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for an unrecognized MAIL_TLS_POLICY")
	}
}

func TestLoadConfig_InvalidPort(t *testing.T) {
	tests := []string{"0", "-1", "70000"}

	for _, port := range tests {
		t.Run(port, func(t *testing.T) {
			testkit.UnsetEnv(t, mailEnvKeys...)
			t.Setenv("MAIL_PORT", port)

			if _, err := mail.LoadConfig(); err == nil {
				t.Fatalf("LoadConfig() error = nil, want error for port %q", port)
			}
		})
	}
}

func TestLoadConfig_InvalidFromAddress(t *testing.T) {
	testkit.UnsetEnv(t, mailEnvKeys...)
	t.Setenv("MAIL_FROM", "not-an-email")

	if _, err := mail.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for an invalid MAIL_FROM address")
	}
}

func TestLoadConfig_InvalidTimeout(t *testing.T) {
	testkit.UnsetEnv(t, mailEnvKeys...)
	t.Setenv("MAIL_TIMEOUT", "0s")

	if _, err := mail.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want error for MAIL_TIMEOUT=0s")
	}
}

func TestLoadConfig_PartialCredentialsRejected(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{"username_without_password", "user", ""},
		{"password_without_username", "", "pass"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testkit.UnsetEnv(t, mailEnvKeys...)
			t.Setenv("MAIL_USERNAME", tc.username)
			t.Setenv("MAIL_PASSWORD", tc.password)

			if _, err := mail.LoadConfig(); err == nil {
				t.Fatalf("LoadConfig() error = nil, want error for a one-sided credential (username=%q, password=%q)", tc.username, tc.password)
			}
		})
	}
}

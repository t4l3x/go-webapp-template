package localization_test

import (
	"testing"

	"github.com/t4l3x/go-webapp-template/internal/platform/localization"
	"github.com/t4l3x/go-webapp-template/internal/testkit"
)

func TestLoadConfig_Defaults(t *testing.T) {
	testkit.UnsetEnv(t, "I18N_DEFAULT_LOCALE")

	cfg, err := localization.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.DefaultLocale != "en" {
		t.Fatalf("DefaultLocale = %q, want %q", cfg.DefaultLocale, "en")
	}
}

func TestLoadConfig_CustomDefaultLocale(t *testing.T) {
	t.Setenv("I18N_DEFAULT_LOCALE", "lv")

	cfg, err := localization.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.DefaultLocale != "lv" {
		t.Fatalf("DefaultLocale = %q, want %q", cfg.DefaultLocale, "lv")
	}
}

func TestLoadConfig_RejectsMalformedDefaultLocale(t *testing.T) {
	tests := []struct {
		name   string
		locale string
	}{
		{"not_a_tag", "english"},
		{"punctuation", "en_US!"},
		{"digits", "123"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("I18N_DEFAULT_LOCALE", tc.locale)

			if _, err := localization.LoadConfig(); err == nil {
				t.Fatalf("LoadConfig() error = nil, want an error for %q", tc.locale)
			}
		})
	}
}

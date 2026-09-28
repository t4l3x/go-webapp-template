package localization

import (
	"fmt"

	"github.com/caarlos0/env/v11"
	"golang.org/x/text/language"
)

type Config struct {
	// DefaultLocale is the locale used when a recipient's own locale is
	// unknown, unsupported, or malformed. It is the last link in the
	// fallback chain, so a catalog for it must exist — the engine
	// refuses to start otherwise (see NewEngine).
	DefaultLocale string `env:"I18N_DEFAULT_LOCALE" envDefault:"en"`
}

// LoadConfig parses and validates localization settings. There is
// deliberately no I18N_SUPPORTED_LOCALES: the registered catalogs are
// the authoritative set of supported locales, and a second declaration
// of the same fact could only ever disagree with them.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("load localization config: %w", err)
	}

	if _, err := language.Parse(cfg.DefaultLocale); err != nil {
		return Config{}, fmt.Errorf(
			"I18N_DEFAULT_LOCALE must be a valid BCP 47 language tag, got %q: %w", cfg.DefaultLocale, err)
	}

	return cfg, nil
}

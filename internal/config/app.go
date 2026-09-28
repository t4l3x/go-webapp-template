package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/caarlos0/env/v11"
)

type App struct {
	Environment string `env:"APP_ENV" envDefault:"local"`
	Service     string `env:"APP_SERVICE" envDefault:"api"`
	Version     string `env:"APP_VERSION" envDefault:"dev"`

	// PublicURL is the user-facing application's origin — the frontend
	// people open in a browser, not this API. It is for absolute links
	// handed to people outside any request (e.g. the email-verification
	// link the worker mails), which is why it is general app config
	// rather than HTTP server config: a worker has no HTTP server.
	//
	// Loaded without a trailing slash, so callers append a path directly.
	PublicURL string `env:"APP_PUBLIC_URL" envDefault:"http://localhost:3000"`
}

func LoadApp() (App, error) {
	cfg, err := env.ParseAs[App]()
	if err != nil {
		return App{}, fmt.Errorf("load app config: %w", err)
	}

	// A link is built by appending a path and query to this value, so it
	// must be an absolute http(s) URL with a host and nothing after the
	// path.
	publicURL, err := url.Parse(cfg.PublicURL)
	if err != nil {
		return App{}, fmt.Errorf("APP_PUBLIC_URL must be a valid URL")
	}

	if publicURL.Scheme != "http" && publicURL.Scheme != "https" {
		return App{}, fmt.Errorf("APP_PUBLIC_URL must start with http:// or https://")
	}

	if publicURL.Host == "" {
		return App{}, fmt.Errorf("APP_PUBLIC_URL must include a host")
	}

	if publicURL.User != nil || publicURL.RawQuery != "" || publicURL.ForceQuery || publicURL.Fragment != "" {
		return App{}, fmt.Errorf("APP_PUBLIC_URL must not include credentials, a query, or a fragment")
	}

	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")

	return cfg, nil
}

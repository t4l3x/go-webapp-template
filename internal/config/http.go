package config

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/caarlos0/env/v11"
)

type HTTP struct {
	Port               int           `env:"HTTP_PORT" envDefault:"8080"`
	ShutdownTimeout    time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" envDefault:"10s"`
	CORSAllowedOrigins []string      `env:"HTTP_CORS_ALLOWED_ORIGINS" envSeparator:","`

	// TrustedProxies lists the IPs/CIDRs of reverse proxies allowed to
	// supply a client's real address via X-Forwarded-For/X-Real-IP.
	// Empty (the default) means forwarded headers are never trusted,
	// and the direct TCP peer address is always used instead.
	TrustedProxies []netip.Prefix `env:"HTTP_TRUSTED_PROXIES" envSeparator:","`
}

func LoadHTTP() (HTTP, error) {
	cfg, err := env.ParseAs[HTTP]()
	if err != nil {
		return HTTP{}, fmt.Errorf("load http config: %w", err)
	}

	if cfg.Port < 1 || cfg.Port > 65535 {
		return HTTP{}, fmt.Errorf("HTTP_PORT must be between 1 and 65535")
	}

	if cfg.ShutdownTimeout <= 0 {
		return HTTP{}, fmt.Errorf(
			"HTTP_SHUTDOWN_TIMEOUT must be greater than zero",
		)
	}

	return cfg, nil
}

package database

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/config"
)

func NewPostgres(
	lifecycle fx.Lifecycle,
	dbCfg Config,
	appCfg config.App,
	logger *slog.Logger,
) (*pgxpool.Pool, error) {
	postgresLogger := logger.With(
		"component", "postgres",
	)

	poolConfig, err := pgxpool.ParseConfig(dbCfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}

	poolConfig.MaxConns = dbCfg.MaxConns
	poolConfig.MinIdleConns = dbCfg.MinIdleConns

	poolConfig.MaxConnLifetime = dbCfg.MaxConnLifetime
	poolConfig.MaxConnLifetimeJitter = dbCfg.MaxConnLifetimeJitter
	poolConfig.MaxConnIdleTime = dbCfg.MaxConnIdleTime
	poolConfig.HealthCheckPeriod = dbCfg.HealthCheckPeriod
	poolConfig.PingTimeout = dbCfg.PingTimeout

	poolConfig.ConnConfig.ConnectTimeout = dbCfg.ConnectTimeout

	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}

	poolConfig.ConnConfig.RuntimeParams["application_name"] = appCfg.Service

	pool, err := pgxpool.NewWithConfig(
		context.Background(),
		poolConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			pingCtx, cancel := context.WithTimeout(
				ctx,
				dbCfg.PingTimeout,
			)
			defer cancel()

			if err := pool.Ping(pingCtx); err != nil {
				return fmt.Errorf("ping postgres: %w", err)
			}

			postgresLogger.Info("postgres connected")

			return nil
		},

		OnStop: func(context.Context) error {
			pool.Close()

			postgresLogger.Info("postgres closed")

			return nil
		},
	})

	return pool, nil
}

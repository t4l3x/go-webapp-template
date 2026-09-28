package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/fx"

	"github.com/t4l3x/go-webapp-template/internal/config"
)

type HTTPServer struct {
	server          *http.Server
	logger          *slog.Logger
	shutdowner      fx.Shutdowner
	shutdownTimeout time.Duration
}

func NewHTTPServer(
	cfg config.HTTP,
	logger *slog.Logger,
	shutdowner fx.Shutdowner,
	handler http.Handler,
) *HTTPServer {
	serverLogger := logger.With(
		"component", "http",
	)

	return &HTTPServer{
		server: &http.Server{
			Addr: net.JoinHostPort(
				"",
				strconv.Itoa(cfg.Port),
			),

			Handler: handler,

			// Protect against slow clients sending headers indefinitely.
			ReadHeaderTimeout: 5 * time.Second,

			// Close unused keep-alive connections after inactivity.
			IdleTimeout: 60 * time.Second,

			// Limit request headers to 1 MiB.
			MaxHeaderBytes: 1 << 20,

			// Route internal net/http errors through structured logging.
			ErrorLog: slog.NewLogLogger(
				serverLogger.Handler(),
				slog.LevelError,
			),
		},

		logger:          serverLogger,
		shutdowner:      shutdowner,
		shutdownTimeout: cfg.ShutdownTimeout,
	}
}

func RegisterHTTPServerLifecycle(
	lifecycle fx.Lifecycle,
	server *HTTPServer,
) {
	lifecycle.Append(fx.Hook{
		OnStart: server.Start,
		OnStop:  server.Stop,
	})
}

func (s *HTTPServer) Start(ctx context.Context) error {
	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(
		ctx,
		"tcp",
		s.server.Addr,
	)
	if err != nil {
		return fmt.Errorf(
			"listen on %s: %w",
			s.server.Addr,
			err,
		)
	}

	s.logger.Info(
		"http server started",
		"address", listener.Addr().String(),
	)

	go func() {
		err := s.server.Serve(listener)

		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return
		}

		s.logger.Error(
			"http server failed",
			"error", err,
		)

		if shutdownErr := s.shutdowner.Shutdown(
			fx.ExitCode(1),
		); shutdownErr != nil {
			s.logger.Error(
				"application shutdown request failed",
				"component", "http",
				"error", shutdownErr,
			)
		}
	}()

	return nil
}

func (s *HTTPServer) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(
		ctx,
		s.shutdownTimeout,
	)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		_ = s.server.Close()

		return fmt.Errorf("shutdown http server: %w", err)
	}

	s.logger.Info(
		"http server stopped",
		"component", "http",
	)

	return nil
}

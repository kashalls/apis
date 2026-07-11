package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kashalls/juno/internal/api"
	"github.com/kashalls/juno/internal/config"
	"github.com/kashalls/juno/internal/homeassistant"
	"github.com/kashalls/juno/internal/ratelimit"
)

const rateLimitInterval = 2 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadHomeAssistant()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := homeassistant.NewClient(cfg.HomeAssistantBaseURL, cfg.HomeAssistantToken)
	handlers := homeassistant.NewHandlers(client, ratelimit.NewLimiter(rateLimitInterval), cfg.HomeAssistantLightGroup)

	router := api.NewHomeAssistantRouter(api.HomeAssistantRouterConfig{
		Handlers:          handlers,
		TrustedProxyCIDRs: cfg.TrustedProxyCIDRs,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

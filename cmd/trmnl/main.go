package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kashalls/juno/internal/api"
	"github.com/kashalls/juno/internal/config"
	"github.com/kashalls/juno/internal/ratelimit"
	"github.com/kashalls/juno/internal/trmnl"
)

const (
	rateLimitInterval  = 5 * time.Minute
	imageSweepInterval = 10 * time.Minute
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadTRMNL()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	textClient := trmnl.NewClient(cfg.TRMNLTextWebhookURL)
	imageClient := trmnl.NewClient(cfg.TRMNLImageWebhookURL)
	handlers := trmnl.NewHandlers(
		textClient, ratelimit.NewLimiter(rateLimitInterval),
		imageClient, ratelimit.NewLimiter(rateLimitInterval),
		cfg.DataDir, cfg.PublicBaseURL,
	)

	imagesDir := filepath.Join(cfg.DataDir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return err
	}
	trmnl.StartImageSweeper(ctx, imagesDir, imageSweepInterval)

	router := api.NewTRMNLRouter(api.TRMNLRouterConfig{
		Handlers:          handlers,
		ImagesDir:         imagesDir,
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

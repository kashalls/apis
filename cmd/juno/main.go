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
	"github.com/kashalls/juno/internal/discord"
	"github.com/kashalls/juno/internal/homeassistant"
	"github.com/kashalls/juno/internal/lanyard"
	"github.com/kashalls/juno/internal/ratelimit"
	"github.com/kashalls/juno/internal/trmnl"
)

const (
	trmnlRateLimitInterval  = 5 * time.Minute
	imageSweepInterval      = 10 * time.Minute
	lightsRateLimitInterval = 2 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store := discord.NewStore()

	discordClient, err := discord.NewClient(cfg.DiscordBotToken, cfg.DiscordUserID, cfg.DiscordGuildID, store)
	if err != nil {
		return err
	}
	if err := discordClient.Open(); err != nil {
		return err
	}
	defer discordClient.Close()

	hub := lanyard.NewHub(store)

	trmnlTextClient := trmnl.NewClient(cfg.TRMNLTextWebhookURL)
	trmnlImageClient := trmnl.NewClient(cfg.TRMNLImageWebhookURL)
	trmnlHandlers := trmnl.NewHandlers(
		trmnlTextClient, ratelimit.NewLimiter(trmnlRateLimitInterval),
		trmnlImageClient, ratelimit.NewLimiter(trmnlRateLimitInterval),
		cfg.DataDir, cfg.PublicBaseURL,
	)

	homeAssistantClient := homeassistant.NewClient(cfg.HomeAssistantBaseURL, cfg.HomeAssistantToken)
	homeAssistantHandlers := homeassistant.NewHandlers(
		homeAssistantClient, ratelimit.NewLimiter(lightsRateLimitInterval),
		cfg.HomeAssistantLightGroup,
	)

	imagesDir := filepath.Join(cfg.DataDir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return err
	}
	trmnl.StartImageSweeper(ctx, imagesDir, imageSweepInterval)

	router := api.NewRouter(api.RouterConfig{
		Store:                 store,
		DiscordUserID:         cfg.DiscordUserID,
		Hub:                   hub,
		TRMNLHandlers:         trmnlHandlers,
		HomeAssistantHandlers: homeAssistantHandlers,
		ImagesDir:             imagesDir,
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

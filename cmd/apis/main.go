package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kashalls/apis/internal/api"
	"github.com/kashalls/apis/internal/config"
	"github.com/kashalls/apis/internal/github"
	"github.com/kashalls/apis/internal/homeassistant"
	"github.com/kashalls/apis/internal/mqtt"
	"github.com/kashalls/apis/internal/overwatch"
	"github.com/kashalls/apis/internal/ratelimit"
	"github.com/kashalls/apis/internal/redisconn"
	"github.com/kashalls/apis/internal/spotify"
	"github.com/kashalls/apis/internal/trmnl"
	"github.com/kashalls/apis/internal/wow"
)

const (
	homeAssistantRateLimitInterval = 2 * time.Second
	trmnlRateLimitInterval         = 5 * time.Minute
	imageSweepInterval             = 10 * time.Minute
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

	haClient := homeassistant.NewClient(cfg.HomeAssistantBaseURL, cfg.HomeAssistantToken)
	haHandlers := homeassistant.NewHandlers(haClient, ratelimit.NewLimiter(homeAssistantRateLimitInterval), cfg.HomeAssistantLightGroup)

	textClient := trmnl.NewClient(cfg.TRMNLTextWebhookURL)
	imageClient := trmnl.NewClient(cfg.TRMNLImageWebhookURL)
	trmnlHandlers := trmnl.NewHandlers(
		textClient, ratelimit.NewLimiter(trmnlRateLimitInterval),
		imageClient, ratelimit.NewLimiter(trmnlRateLimitInterval),
		cfg.DataDir, cfg.PublicBaseURL,
	)

	imagesDir := filepath.Join(cfg.DataDir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return err
	}
	trmnl.StartImageSweeper(ctx, imagesDir, imageSweepInterval)

	startupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rdb, err := redisconn.New(startupCtx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer rdb.Close()

	var mqttClient *mqtt.Client
	if cfg.MQTTBrokerURL != "" {
		mqttClient, err = mqtt.New(cfg.MQTTBrokerURL, cfg.MQTTUsername, cfg.MQTTPassword)
		if err != nil {
			return fmt.Errorf("connect to mqtt: %w", err)
		}
		defer mqttClient.Close()
	} else {
		slog.Info("MQTT_BROKER_URL not set, spotify current-track mqtt publishing disabled")
	}

	spotifyClient := spotify.NewClient(cfg.SpotifyClientID, cfg.SpotifyClientSecret, cfg.SpotifyRedirectURI, rdb, mqttClient, cfg.MQTTTopic)
	if authorized, err := spotifyClient.Authorized(startupCtx); err == nil && !authorized {
		slog.Info("spotify not authorized yet, visit /api/spotify/authorize to set up")
	}
	spotifyClient.StartCurrentPoller(ctx)
	spotifyHandlers := spotify.NewHandlers(spotifyClient)

	githubClient := github.NewClient(cfg.GitHubUsername, cfg.GitHubToken, rdb)
	githubHandlers := github.NewHandlers(githubClient)

	wowCharacters, err := wow.ParseCharacterRefs(cfg.WoWCharacters)
	if err != nil {
		return err
	}
	wowClient := wow.NewClient(cfg.BlizzardClientID, cfg.BlizzardClientSecret, cfg.BlizzardRegion, wowCharacters, rdb)
	wowHandlers := wow.NewHandlers(wowClient)

	overwatchHandlers := overwatch.NewHandlers(overwatch.NewClient(cfg.OverwatchBattleTags, rdb))

	router := api.NewRouter(api.RouterConfig{
		TRMNL:              trmnlHandlers,
		HomeAssistant:      haHandlers,
		Spotify:            spotifyHandlers,
		GitHub:             githubHandlers,
		WoW:                wowHandlers,
		Overwatch:          overwatchHandlers,
		TrustedProxyCIDRs:  cfg.TrustedProxyCIDRs,
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
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

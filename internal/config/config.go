// Package config loads Juno's runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port                    string
	DiscordBotToken         string
	DiscordUserID           string
	DiscordGuildID          string
	TRMNLTextWebhookURL     string
	TRMNLImageWebhookURL    string
	PublicBaseURL           string
	DataDir                 string
	HomeAssistantBaseURL    string
	HomeAssistantToken      string
	HomeAssistantLightGroup string
	TrustedProxyCIDRs       []string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                    getEnvDefault("PORT", "8080"),
		DiscordBotToken:         os.Getenv("DISCORD_BOT_TOKEN"),
		DiscordUserID:           os.Getenv("DISCORD_USER_ID"),
		DiscordGuildID:          os.Getenv("DISCORD_GUILD_ID"),
		TRMNLTextWebhookURL:     os.Getenv("TRMNL_TEXT_WEBHOOK_URL"),
		TRMNLImageWebhookURL:    os.Getenv("TRMNL_IMAGE_WEBHOOK_URL"),
		PublicBaseURL:           os.Getenv("PUBLIC_BASE_URL"),
		DataDir:                 getEnvDefault("DATA_DIR", "/data"),
		HomeAssistantBaseURL:    os.Getenv("HOME_ASSISTANT_BASE_URL"),
		HomeAssistantToken:      os.Getenv("HOME_ASSISTANT_TOKEN"),
		HomeAssistantLightGroup: os.Getenv("HOME_ASSISTANT_LIGHT_GROUP"),
		TrustedProxyCIDRs:       parseCIDRList(os.Getenv("TRUSTED_PROXY_CIDRS")),
	}

	var missing []string
	if cfg.DiscordBotToken == "" {
		missing = append(missing, "DISCORD_BOT_TOKEN")
	}
	if cfg.DiscordUserID == "" {
		missing = append(missing, "DISCORD_USER_ID")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %v", missing)
	}

	return cfg, nil
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseCIDRList(v string) []string {
	var cidrs []string
	for _, c := range strings.Split(v, ",") {
		if c = strings.TrimSpace(c); c != "" {
			cidrs = append(cidrs, c)
		}
	}
	return cidrs
}

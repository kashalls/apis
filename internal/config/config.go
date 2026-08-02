// Package config loads the apis binary's runtime configuration from
// environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port                    string
	TRMNLTextWebhookURL     string
	TRMNLImageWebhookURL    string
	PublicBaseURL           string
	DataDir                 string
	HomeAssistantBaseURL    string
	HomeAssistantToken      string
	HomeAssistantLightGroup string
	SpotifyClientID         string
	SpotifyClientSecret     string
	SpotifyRedirectURI      string
	RedisURL                string
	GitHubUsername          string
	GitHubToken             string
	MQTTBrokerURL           string
	MQTTUsername            string
	MQTTPassword            string
	MQTTTopic               string
	TrustedProxyCIDRs       []string
	CORSAllowedOrigins      []string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                    getEnvDefault("PORT", "8080"),
		TRMNLTextWebhookURL:     os.Getenv("TRMNL_TEXT_WEBHOOK_URL"),
		TRMNLImageWebhookURL:    os.Getenv("TRMNL_IMAGE_WEBHOOK_URL"),
		PublicBaseURL:           os.Getenv("PUBLIC_BASE_URL"),
		DataDir:                 getEnvDefault("DATA_DIR", "/data"),
		HomeAssistantBaseURL:    os.Getenv("HOME_ASSISTANT_BASE_URL"),
		HomeAssistantToken:      os.Getenv("HOME_ASSISTANT_TOKEN"),
		HomeAssistantLightGroup: os.Getenv("HOME_ASSISTANT_LIGHT_GROUP"),
		SpotifyClientID:         os.Getenv("SPOTIFY_CLIENT_ID"),
		SpotifyClientSecret:     os.Getenv("SPOTIFY_CLIENT_SECRET"),
		SpotifyRedirectURI:      os.Getenv("SPOTIFY_REDIRECT_URI"),
		RedisURL:                os.Getenv("REDIS_URL"),
		GitHubUsername:          os.Getenv("GITHUB_USERNAME"),
		GitHubToken:             os.Getenv("GITHUB_TOKEN"),
		MQTTBrokerURL:           os.Getenv("MQTT_BROKER_URL"),
		MQTTUsername:            os.Getenv("MQTT_USERNAME"),
		MQTTPassword:            os.Getenv("MQTT_PASSWORD"),
		MQTTTopic:               getEnvDefault("MQTT_TOPIC", "spotify/current"),
		TrustedProxyCIDRs:       parseCommaList(os.Getenv("TRUSTED_PROXY_CIDRS")),
		CORSAllowedOrigins:      parseCommaList(os.Getenv("CORS_ALLOWED_ORIGINS")),
	}

	var missing []string
	if cfg.TRMNLTextWebhookURL == "" {
		missing = append(missing, "TRMNL_TEXT_WEBHOOK_URL")
	}
	if cfg.TRMNLImageWebhookURL == "" {
		missing = append(missing, "TRMNL_IMAGE_WEBHOOK_URL")
	}
	if cfg.HomeAssistantBaseURL == "" {
		missing = append(missing, "HOME_ASSISTANT_BASE_URL")
	}
	if cfg.HomeAssistantToken == "" {
		missing = append(missing, "HOME_ASSISTANT_TOKEN")
	}
	if cfg.HomeAssistantLightGroup == "" {
		missing = append(missing, "HOME_ASSISTANT_LIGHT_GROUP")
	}
	if cfg.SpotifyClientID == "" {
		missing = append(missing, "SPOTIFY_CLIENT_ID")
	}
	if cfg.SpotifyClientSecret == "" {
		missing = append(missing, "SPOTIFY_CLIENT_SECRET")
	}
	if cfg.SpotifyRedirectURI == "" {
		missing = append(missing, "SPOTIFY_REDIRECT_URI")
	}
	if cfg.RedisURL == "" {
		missing = append(missing, "REDIS_URL")
	}
	if cfg.GitHubUsername == "" {
		missing = append(missing, "GITHUB_USERNAME")
	}
	if cfg.GitHubToken == "" {
		missing = append(missing, "GITHUB_TOKEN")
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

func parseCommaList(v string) []string {
	var items []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			items = append(items, s)
		}
	}
	return items
}

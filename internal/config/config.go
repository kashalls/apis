// Package config loads each binary's runtime configuration from
// environment variables. TRMNL and Home Assistant each run as their own
// binary/container and only load the subset of variables relevant to them.
package config

import (
	"fmt"
	"os"
	"strings"
)

// TRMNLConfig configures the TRMNL push server (cmd/trmnl).
type TRMNLConfig struct {
	Port                 string
	TRMNLTextWebhookURL  string
	TRMNLImageWebhookURL string
	PublicBaseURL        string
	DataDir              string
	TrustedProxyCIDRs    []string
}

func LoadTRMNL() (*TRMNLConfig, error) {
	cfg := &TRMNLConfig{
		Port:                 getEnvDefault("PORT", "8080"),
		TRMNLTextWebhookURL:  os.Getenv("TRMNL_TEXT_WEBHOOK_URL"),
		TRMNLImageWebhookURL: os.Getenv("TRMNL_IMAGE_WEBHOOK_URL"),
		PublicBaseURL:        os.Getenv("PUBLIC_BASE_URL"),
		DataDir:              getEnvDefault("DATA_DIR", "/data"),
		TrustedProxyCIDRs:    parseCIDRList(os.Getenv("TRUSTED_PROXY_CIDRS")),
	}

	var missing []string
	if cfg.TRMNLTextWebhookURL == "" {
		missing = append(missing, "TRMNL_TEXT_WEBHOOK_URL")
	}
	if cfg.TRMNLImageWebhookURL == "" {
		missing = append(missing, "TRMNL_IMAGE_WEBHOOK_URL")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %v", missing)
	}

	return cfg, nil
}

// HomeAssistantConfig configures the Home Assistant light server
// (cmd/homeassistant).
type HomeAssistantConfig struct {
	Port                    string
	HomeAssistantBaseURL    string
	HomeAssistantToken      string
	HomeAssistantLightGroup string
	TrustedProxyCIDRs       []string
}

func LoadHomeAssistant() (*HomeAssistantConfig, error) {
	cfg := &HomeAssistantConfig{
		Port:                    getEnvDefault("PORT", "8080"),
		HomeAssistantBaseURL:    os.Getenv("HOME_ASSISTANT_BASE_URL"),
		HomeAssistantToken:      os.Getenv("HOME_ASSISTANT_TOKEN"),
		HomeAssistantLightGroup: os.Getenv("HOME_ASSISTANT_LIGHT_GROUP"),
		TrustedProxyCIDRs:       parseCIDRList(os.Getenv("TRUSTED_PROXY_CIDRS")),
	}

	var missing []string
	if cfg.HomeAssistantBaseURL == "" {
		missing = append(missing, "HOME_ASSISTANT_BASE_URL")
	}
	if cfg.HomeAssistantToken == "" {
		missing = append(missing, "HOME_ASSISTANT_TOKEN")
	}
	if cfg.HomeAssistantLightGroup == "" {
		missing = append(missing, "HOME_ASSISTANT_LIGHT_GROUP")
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

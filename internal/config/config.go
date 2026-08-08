// Package config loads the apis binary's runtime configuration from
// environment variables.
package config

import "github.com/caarlos0/env/v11"

type Config struct {
	Port                    string   `env:"PORT" envDefault:"8080"`
	PublicBaseURL           string   `env:"PUBLIC_BASE_URL"`
	DataDir                 string   `env:"DATA_DIR" envDefault:"/data"`
	HomeAssistantBaseURL    string   `env:"HOME_ASSISTANT_BASE_URL,required,notEmpty"`
	HomeAssistantToken      string   `env:"HOME_ASSISTANT_TOKEN,required,notEmpty"`
	HomeAssistantLightGroup string   `env:"HOME_ASSISTANT_LIGHT_GROUP,required,notEmpty"`
	SpotifyClientID         string   `env:"SPOTIFY_CLIENT_ID,required,notEmpty"`
	SpotifyClientSecret     string   `env:"SPOTIFY_CLIENT_SECRET,required,notEmpty"`
	SpotifyRedirectURI      string   `env:"SPOTIFY_REDIRECT_URI,required,notEmpty"`
	RedisURL                string   `env:"REDIS_URL,required,notEmpty"`
	GitHubUsername          string   `env:"GITHUB_USERNAME,required,notEmpty"`
	GitHubToken             string   `env:"GITHUB_TOKEN,required,notEmpty"`
	MQTTBrokerURL           string   `env:"MQTT_BROKER_URL,required,notEmpty"`
	MQTTUsername            string   `env:"MQTT_USERNAME"`
	MQTTPassword            string   `env:"MQTT_PASSWORD"`
	MQTTTopic               string   `env:"MQTT_TOPIC" envDefault:"spotify/current"`
	TrustedProxyCIDRs       []string `env:"TRUSTED_PROXY_CIDRS"`
	CORSAllowedOrigins      []string `env:"CORS_ALLOWED_ORIGINS"`
}

func Load() (*Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

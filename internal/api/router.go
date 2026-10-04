package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/kashalls/apis/internal/github"
	"github.com/kashalls/apis/internal/homeassistant"
	"github.com/kashalls/apis/internal/overwatch"
	"github.com/kashalls/apis/internal/spotify"
	"github.com/kashalls/apis/internal/trmnl"
	"github.com/kashalls/apis/internal/wow"
)

type RouterConfig struct {
	TRMNL              *trmnl.Handlers
	HomeAssistant      *homeassistant.Handlers
	Spotify            *spotify.Handlers
	GitHub             *github.Handlers
	WoW                *wow.Handlers
	Overwatch          *overwatch.Handlers
	TrustedProxyCIDRs  []string
	CORSAllowedOrigins []string
}

// NewRouter builds the single router serving all three integrations,
// each mounted under its own /api/<service> prefix.
func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if len(cfg.TrustedProxyCIDRs) > 0 {
		r.Use(middleware.ClientIPFromXFF(cfg.TrustedProxyCIDRs...))
	} else {
		r.Use(middleware.ClientIPFromRemoteAddr)
	}
	r.Use(cors.Handler(corsOptions(cfg.CORSAllowedOrigins)))
	r.Use(skipNoisyLogger)
	r.Use(metricsMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", healthHandler)
	r.Handle("/metrics", promhttp.Handler())

	r.Route("/api", func(ar chi.Router) {
		ar.Route("/trmnl", cfg.TRMNL.Routes)
		ar.Route("/homeassistant", cfg.HomeAssistant.Routes)
		ar.Route("/spotify", cfg.Spotify.Routes)
		ar.Route("/github", cfg.GitHub.Routes)
		ar.Route("/wow", cfg.WoW.Routes)
		ar.Route("/overwatch", cfg.Overwatch.Routes)
	})

	return r
}

// corsOptions returns explicit AllowedOrigins when the caller configured
// some, otherwise falls back to a default policy: any ok8.sh origin
// (apex or subdomain) or any origin on port 3000 (local dev).
func corsOptions(allowedOrigins []string) cors.Options {
	opts := cors.Options{
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type"},
	}
	if len(allowedOrigins) > 0 {
		opts.AllowedOrigins = allowedOrigins
	} else {
		opts.AllowOriginFunc = defaultAllowedOrigin
	}
	return opts
}

func defaultAllowedOrigin(_ *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "ok8.sh" || strings.HasSuffix(host, ".ok8.sh") || u.Port() == "3000"
}

// skipNoisyLogger applies middleware.Logger to every request except
// /healthz and /metrics, keeping access logs free of health-check and
// scrape noise while still logging unmatched routes (404s/405s), which
// per-route middleware would miss.
func skipNoisyLogger(next http.Handler) http.Handler {
	logged := middleware.Logger(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		logged.ServeHTTP(w, r)
	})
}

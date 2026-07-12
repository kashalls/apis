package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/kashalls/juno/internal/discord"
	"github.com/kashalls/juno/internal/homeassistant"
	"github.com/kashalls/juno/internal/lanyard"
	"github.com/kashalls/juno/internal/trmnl"
)

// newBaseRouter sets up the middleware chain shared by every Juno binary:
// request IDs, client IP resolution, access logging (skipping /healthz
// noise), panic recovery, and a request timeout. Callers add their own
// routes on top.
func newBaseRouter(trustedProxyCIDRs []string) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if len(trustedProxyCIDRs) > 0 {
		r.Use(middleware.ClientIPFromXFF(trustedProxyCIDRs...))
	} else {
		r.Use(middleware.ClientIPFromRemoteAddr)
	}
	r.Use(skipHealthzLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", healthHandler)

	return r
}

type JunoRouterConfig struct {
	Store             *discord.Store
	DiscordUserID     string
	Hub               *lanyard.Hub
	TrustedProxyCIDRs []string
}

// NewJunoRouter serves the Discord/Lanyard presence API: GET /v1/users/{id}
// and the Lanyard-protocol WebSocket at /socket.
func NewJunoRouter(cfg JunoRouterConfig) http.Handler {
	r := newBaseRouter(cfg.TrustedProxyCIDRs)

	d := &discordAPI{store: cfg.Store, userID: cfg.DiscordUserID}
	r.Get("/v1/users/{id}", d.getUser)
	r.Get("/socket", cfg.Hub.ServeWS)

	return r
}

type TRMNLRouterConfig struct {
	Handlers          *trmnl.Handlers
	ImagesDir         string
	TrustedProxyCIDRs []string
}

// NewTRMNLRouter serves the TRMNL push API under /api and the
// uploaded-image file server at /images.
func NewTRMNLRouter(cfg TRMNLRouterConfig) http.Handler {
	r := newBaseRouter(cfg.TrustedProxyCIDRs)

	r.Route("/api", func(tr chi.Router) {
		tr.Post("/text", cfg.Handlers.PushText)
		tr.Post("/image", cfg.Handlers.PushImage)
	})

	fileServer := http.FileServer(http.Dir(cfg.ImagesDir))
	r.Handle("/images/*", http.StripPrefix("/images/", fileServer))

	return r
}

type HomeAssistantRouterConfig struct {
	Handlers          *homeassistant.Handlers
	TrustedProxyCIDRs []string
}

// NewHomeAssistantRouter serves the Home Assistant light API under /api.
func NewHomeAssistantRouter(cfg HomeAssistantRouterConfig) http.Handler {
	r := newBaseRouter(cfg.TrustedProxyCIDRs)

	r.Post("/api/color", cfg.Handlers.SetColor)

	return r
}

// skipHealthzLogger applies middleware.Logger to every request except
// /healthz, keeping access logs free of health-check noise while still
// logging unmatched routes (404s/405s), which per-route middleware would miss.
func skipHealthzLogger(next http.Handler) http.Handler {
	logged := middleware.Logger(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		logged.ServeHTTP(w, r)
	})
}

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

type RouterConfig struct {
	Store                 *discord.Store
	DiscordUserID         string
	Hub                   *lanyard.Hub
	TRMNLHandlers         *trmnl.Handlers
	HomeAssistantHandlers *homeassistant.Handlers
	ImagesDir             string
	TrustedProxyCIDRs     []string
}

func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if len(cfg.TrustedProxyCIDRs) > 0 {
		r.Use(middleware.ClientIPFromXFF(cfg.TrustedProxyCIDRs...))
	} else {
		r.Use(middleware.ClientIPFromRemoteAddr)
	}
	r.Use(skipHealthzLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", healthHandler)

	d := &discordAPI{store: cfg.Store, userID: cfg.DiscordUserID}
	r.Get("/v1/users/{id}", d.getUser)
	r.Get("/socket", cfg.Hub.ServeWS)

	r.Route("/api/trmnl", func(tr chi.Router) {
		tr.Post("/text", cfg.TRMNLHandlers.PushText)
		tr.Post("/image", cfg.TRMNLHandlers.PushImage)
	})

	r.Post("/api/lights/color", cfg.HomeAssistantHandlers.SetColor)

	fileServer := http.FileServer(http.Dir(cfg.ImagesDir))
	r.Handle("/images/*", http.StripPrefix("/images/", fileServer))

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

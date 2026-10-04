package wow

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	client *Client
}

func NewHandlers(client *Client) *Handlers {
	return &Handlers{client: client}
}

func (h *Handlers) Routes(r chi.Router) {
	r.Get("/characters", h.Characters)
}

func (h *Handlers) Characters(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"characters": h.client.Characters(r.Context())})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

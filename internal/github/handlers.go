package github

import (
	"encoding/json"
	"fmt"
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
	r.Get("/pinned", h.Pinned)
	r.Get("/contributions", h.Contributions)
}

func (h *Handlers) Pinned(w http.ResponseWriter, r *http.Request) {
	repos, err := h.client.Pinned(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to fetch pinned repos: %v", err), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"repositories": repos})
}

func (h *Handlers) Contributions(w http.ResponseWriter, r *http.Request) {
	contributions, err := h.client.Contributions(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to fetch contributions: %v", err), http.StatusBadGateway)
		return
	}
	writeJSON(w, contributions)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

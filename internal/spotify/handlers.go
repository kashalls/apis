package spotify

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/go-chi/chi/v5"
)

const defaultRecentsLimit = 10

type Handlers struct {
	client *Client

	mu    sync.Mutex
	state string
}

func NewHandlers(client *Client) *Handlers {
	return &Handlers{client: client}
}

func (h *Handlers) Routes(r chi.Router) {
	r.Get("/current", h.Current)
	r.Get("/recents", h.Recents)
	r.Get("/authorize", h.Authorize)
	r.Get("/setup", h.Setup)
}

// Authorize starts the one-time OAuth flow: it returns the Spotify consent
// URL to visit, remembering a random state value that Setup later verifies.
func (h *Handlers) Authorize(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfAuthorized(w, r) {
		return
	}

	b := make([]byte, 16)
	_, _ = rand.Read(b)
	state := hex.EncodeToString(b)

	h.mu.Lock()
	h.state = state
	h.mu.Unlock()

	writeJSON(w, map[string]string{"url": h.client.AuthorizeURL(state)})
}

// Setup is the OAuth redirect target: it verifies the state from Authorize
// and exchanges the code for tokens, completing the setup.
func (h *Handlers) Setup(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfAuthorized(w, r) {
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	expected := h.state
	h.mu.Unlock()
	state := r.URL.Query().Get("state")
	if expected == "" || subtle.ConstantTimeCompare([]byte(state), []byte(expected)) != 1 {
		http.Error(w, "state mismatch, restart at /api/spotify/authorize", http.StatusBadRequest)
		return
	}

	if err := h.client.Exchange(r.Context(), code); err != nil {
		http.Error(w, fmt.Sprintf("failed to complete setup: %v", err), http.StatusBadGateway)
		return
	}

	h.mu.Lock()
	h.state = ""
	h.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// Current returns the currently playing track/episode, or {"playing":false},
// served from the cache StartCurrentPoller keeps fresh.
func (h *Handlers) Current(w http.ResponseWriter, r *http.Request) {
	current := h.client.CachedCurrent()
	if current == nil {
		writeSpotifyError(w, ErrNotAuthorized)
		return
	}
	writeJSON(w, current)
}

// Recents returns the most recently finished listens, newest first. The
// optional limit query param (1-50) defaults to 10.
func (h *Handlers) Recents(w http.ResponseWriter, r *http.Request) {
	limit := defaultRecentsLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 50 {
			http.Error(w, "limit must be an integer between 1 and 50", http.StatusBadRequest)
			return
		}
		limit = n
	}

	listens, err := h.client.RecentListens(r.Context(), limit)
	if err != nil {
		writeSpotifyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"recents": listens})
}

// rejectIfAuthorized writes an error response (and reports true) when the
// OAuth setup is already done, or when the check itself failed.
func (h *Handlers) rejectIfAuthorized(w http.ResponseWriter, r *http.Request) bool {
	authorized, err := h.client.Authorized(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to check authorization: %v", err), http.StatusInternalServerError)
		return true
	}
	if authorized {
		http.Error(w, "already authorized", http.StatusBadRequest)
		return true
	}
	return false
}

func writeSpotifyError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotAuthorized) {
		http.Error(w, "spotify not authorized yet, start at /api/spotify/authorize", http.StatusServiceUnavailable)
		return
	}
	http.Error(w, fmt.Sprintf("failed to query spotify: %v", err), http.StatusBadGateway)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

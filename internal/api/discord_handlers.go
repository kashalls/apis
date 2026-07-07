package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kashalls/juno/internal/discord"
)

type discordAPI struct {
	store  *discord.Store
	userID string
}

type successResponse struct {
	Success bool `json:"success"`
	Data    any  `json:"data"`
}

type errorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

func (d *discordAPI) getUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id != d.userID {
		writeJSON(w, http.StatusNotFound, errorResponse{Success: false, Error: "user not found"})
		return
	}

	presence := d.store.Get()
	if presence == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Success: false, Error: "presence not yet available"})
		return
	}

	writeJSON(w, http.StatusOK, successResponse{Success: true, Data: presence})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

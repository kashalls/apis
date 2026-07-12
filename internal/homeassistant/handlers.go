package homeassistant

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/kashalls/apis/internal/ratelimit"
)

var hexColorPattern = regexp.MustCompile(`^#?([0-9a-fA-F]{6})$`)

type Handlers struct {
	client      *Client
	rateLimiter *ratelimit.Limiter
	lightGroup  string
}

func NewHandlers(client *Client, rateLimiter *ratelimit.Limiter, lightGroup string) *Handlers {
	return &Handlers{client: client, rateLimiter: rateLimiter, lightGroup: lightGroup}
}

type setColorRequest struct {
	Hex string `json:"hex"`
}

// SetColor changes the light group's color only - brightness is never
// accepted from the request or forwarded to Home Assistant.
func (h *Handlers) SetColor(w http.ResponseWriter, r *http.Request) {
	var req setColorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	color, err := parseHexColor(req.Hex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if ok, retryAfter := h.rateLimiter.Allow(); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		http.Error(w, "rate limit exceeded, try again later", http.StatusTooManyRequests)
		return
	}

	if err := h.client.SetLightColor(r.Context(), h.lightGroup, color); err != nil {
		http.Error(w, fmt.Sprintf("failed to set light color: %v", err), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func parseHexColor(hex string) (RGB, error) {
	matches := hexColorPattern.FindStringSubmatch(strings.TrimSpace(hex))
	if matches == nil {
		return RGB{}, fmt.Errorf("hex must be a 6-digit color like \"#ff8800\"")
	}

	v, err := strconv.ParseUint(matches[1], 16, 32)
	if err != nil {
		return RGB{}, fmt.Errorf("invalid hex color: %w", err)
	}

	return RGB{
		R: int(v >> 16 & 0xff),
		G: int(v >> 8 & 0xff),
		B: int(v & 0xff),
	}, nil
}

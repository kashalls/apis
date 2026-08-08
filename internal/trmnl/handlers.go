// Package trmnl serves a TRMNL device via its Polling private-plugin
// strategy (https://docs.trmnl.com/go/private-plugins/polling): TRMNL
// itself issues a GET to a URL we give it and expects the response
// body's root-level JSON keys to become the merge variables. PushText
// and PushImage publish new content to mqtt instead of calling TRMNL
// directly; StartQueueConsumer subscribes and appends each message onto
// a redis-backed queue, which PollText/PollImage serve from.
package trmnl

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/kashalls/apis/internal/mqtt"
	"github.com/kashalls/apis/internal/ratelimit"
)

const maxImageBytes = 2 << 20 // 2MB

var allowedImageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

type Handlers struct {
	mqtt         *mqtt.Client
	redis        *redis.Client
	textLimiter  *ratelimit.Limiter
	imageLimiter *ratelimit.Limiter

	dataDir       string
	publicBaseURL string
}

func NewHandlers(mqttClient *mqtt.Client, rdb *redis.Client, textLimiter, imageLimiter *ratelimit.Limiter, dataDir, publicBaseURL string) *Handlers {
	return &Handlers{
		mqtt:          mqttClient,
		redis:         rdb,
		textLimiter:   textLimiter,
		imageLimiter:  imageLimiter,
		dataDir:       dataDir,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}

// Routes registers the trmnl endpoints: POST enqueues a new message for
// TRMNL's Polling strategy to pick up via the matching GET, plus the
// file server for images uploaded via PushImage.
func (h *Handlers) Routes(r chi.Router) {
	r.Post("/text", h.PushText)
	r.Get("/text", h.PollText)
	r.Post("/image", h.PushImage)
	r.Get("/image", h.PollImage)

	imagesDir := filepath.Join(h.dataDir, "images")
	fileServer := http.FileServer(http.Dir(imagesDir))
	r.Handle("/images/*", http.StripPrefix("/images/", fileServer))
}

// publishOrRateLimit checks limiter and, only if it's not exceeded,
// publishes vars to the mqtt topic for StartQueueConsumer to pick up.
// Validation of the request body must happen before calling this, so a
// malformed request never consumes the quota.
func publishOrRateLimit(w http.ResponseWriter, r *http.Request, mqttClient *mqtt.Client, limiter *ratelimit.Limiter, topic string, vars map[string]any) {
	if ok, retryAfter := limiter.Allow(); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		http.Error(w, "rate limit exceeded, try again later", http.StatusTooManyRequests)
		return
	}

	payload, err := json.Marshal(vars)
	if err != nil {
		http.Error(w, fmt.Sprintf("encode message: %v", err), http.StatusInternalServerError)
		return
	}
	if err := mqttClient.Publish(topic, payload, 1, false); err != nil {
		http.Error(w, fmt.Sprintf("failed to queue message: %v", err), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type textRequest struct {
	Text   string `json:"text"`
	Author string `json:"author,omitempty"`
}

func (h *Handlers) PushText(w http.ResponseWriter, r *http.Request) {
	var req textRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}

	vars := map[string]any{"text": req.Text}
	if req.Author != "" {
		vars["author"] = req.Author
	}

	publishOrRateLimit(w, r, h.mqtt, h.textLimiter, textTopic, vars)
}

type imageURLRequest struct {
	ImageURL string `json:"image_url"`
}

func (h *Handlers) PushImage(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")

	var imageURL string
	switch {
	case strings.HasPrefix(contentType, "multipart/form-data"):
		url, err := h.storeUploadedImage(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		imageURL = url

	case strings.HasPrefix(contentType, "application/json"):
		var req imageURLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.ImageURL) == "" {
			http.Error(w, "image_url is required", http.StatusBadRequest)
			return
		}
		imageURL = req.ImageURL

	default:
		http.Error(w, "content-type must be multipart/form-data or application/json", http.StatusBadRequest)
		return
	}

	publishOrRateLimit(w, r, h.mqtt, h.imageLimiter, imageTopic, map[string]any{"image_url": imageURL})
}

// poll writes the current merge_variables for key as JSON, for TRMNL's
// Polling strategy to consume directly (root-level keys become merge
// variables - see nextQueued for the queue-drain/keep-last behavior).
func (h *Handlers) poll(w http.ResponseWriter, r *http.Request, key string) {
	vars, err := nextQueued(r.Context(), h.redis, key)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read queue: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(vars)
}

func (h *Handlers) PollText(w http.ResponseWriter, r *http.Request) {
	h.poll(w, r, textQueueKey)
}

func (h *Handlers) PollImage(w http.ResponseWriter, r *http.Request) {
	h.poll(w, r, imageQueueKey)
}

func (h *Handlers) storeUploadedImage(w http.ResponseWriter, r *http.Request) (string, error) {
	if h.publicBaseURL == "" {
		return "", fmt.Errorf("server has no PUBLIC_BASE_URL configured, cannot host uploaded images")
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes)
	if err := r.ParseMultipartForm(maxImageBytes); err != nil {
		return "", fmt.Errorf("image exceeds max size or is malformed: %w", err)
	}

	file, _, err := r.FormFile("image")
	if err != nil {
		return "", fmt.Errorf("missing \"image\" form file: %w", err)
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	sniffed := http.DetectContentType(buf[:n])

	ext, ok := allowedImageTypes[sniffed]
	if !ok {
		return "", fmt.Errorf("unsupported image type %q (allowed: png, jpeg, webp)", sniffed)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("read uploaded image: %w", err)
	}

	imagesDir := filepath.Join(h.dataDir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return "", fmt.Errorf("prepare image storage: %w", err)
	}

	name := randomFilename() + ext
	dst, err := os.Create(filepath.Join(imagesDir, name))
	if err != nil {
		return "", fmt.Errorf("save uploaded image: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return "", fmt.Errorf("save uploaded image: %w", err)
	}

	return fmt.Sprintf("%s/api/trmnl/images/%s", h.publicBaseURL, name), nil
}

func randomFilename() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

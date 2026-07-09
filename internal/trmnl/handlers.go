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

	"github.com/kashalls/juno/internal/ratelimit"
)

const maxImageBytes = 2 << 20 // 2MB

var allowedImageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

// endpoint pairs a TRMNL webhook client with its own independent rate
// limiter, since text and image pushes go to separate TRMNL private
// plugins (each with its own webhook UUID and quota).
type endpoint struct {
	client      *Client
	rateLimiter *ratelimit.Limiter
}

type Handlers struct {
	text          *endpoint
	image         *endpoint
	dataDir       string
	publicBaseURL string
}

func NewHandlers(textClient *Client, textRateLimiter *ratelimit.Limiter, imageClient *Client, imageRateLimiter *ratelimit.Limiter, dataDir, publicBaseURL string) *Handlers {
	return &Handlers{
		text:          &endpoint{client: textClient, rateLimiter: textRateLimiter},
		image:         &endpoint{client: imageClient, rateLimiter: imageRateLimiter},
		dataDir:       dataDir,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}

// pushOrRateLimit checks the endpoint's rate limit and, only if it's not
// exceeded, performs the actual TRMNL webhook push. Validation of the
// request body must happen before calling this, so a malformed request
// never consumes the quota.
func pushOrRateLimit(w http.ResponseWriter, r *http.Request, ep *endpoint, vars map[string]any) {
	if ok, retryAfter := ep.rateLimiter.Allow(); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		http.Error(w, "rate limit exceeded, try again later", http.StatusTooManyRequests)
		return
	}

	if err := ep.client.PushMergeVariables(r.Context(), vars); err != nil {
		http.Error(w, fmt.Sprintf("failed to push to trmnl: %v", err), http.StatusBadGateway)
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

	pushOrRateLimit(w, r, h.text, vars)
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

	pushOrRateLimit(w, r, h.image, map[string]any{"image_url": imageURL})
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

	return fmt.Sprintf("%s/images/%s", h.publicBaseURL, name), nil
}

func randomFilename() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

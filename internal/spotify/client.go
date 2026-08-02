// Package spotify is a client for the Spotify Web API
// (https://developer.spotify.com/documentation/web-api) using the
// authorization-code flow: a one-time browser authorization yields a
// refresh token, stored in Redis, which the client uses to mint
// short-lived access tokens for playback/history reads.
package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kashalls/apis/internal/mqtt"
)

const (
	accountsBaseURL = "https://accounts.spotify.com"
	apiBaseURL      = "https://api.spotify.com/v1"

	// scopes cover current playback (incl. device info) and listen history.
	scopes = "user-read-playback-state user-read-currently-playing user-read-recently-played"

	// The access token lives in Redis with a TTL slightly shorter than
	// Spotify's expiry, so it disappears (and gets refreshed) before it can
	// go stale mid-request. The refresh token has no expiry.
	accessTokenKey   = "spotify/access_token"
	refreshTokenKey  = "spotify/refresh_token"
	accessTokenSlack = 30 * time.Second
)

// ErrNotAuthorized is returned when no refresh token is stored yet, i.e.
// the /api/authorize + /api/setup flow hasn't been completed.
var ErrNotAuthorized = errors.New("spotify not authorized")

type Client struct {
	clientID     string
	clientSecret string
	redirectURI  string
	accountsURL  string
	apiURL       string
	httpClient   *http.Client
	redis        *redis.Client

	// mu serializes token refreshes within this process, so concurrent
	// requests hitting an expired access token don't all call Spotify.
	mu sync.Mutex

	// currentMu guards current, the cache StartCurrentPoller keeps fresh so
	// Handlers.Current can serve it without calling Spotify per request.
	currentMu sync.RWMutex
	current   *Current

	// mqtt is nil when MQTT isn't configured, in which case publishing is
	// skipped there (redis publish still happens either way).
	mqtt      *mqtt.Client
	mqttTopic string

	// lastPublishedMu guards lastPublished, used by publishIfChanged
	// (poll.go) to only publish on an actual playback change.
	lastPublishedMu sync.RWMutex
	lastPublished   *Current
}

func NewClient(clientID, clientSecret, redirectURI string, rdb *redis.Client, mqttClient *mqtt.Client, mqttTopic string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURI:  redirectURI,
		accountsURL:  accountsBaseURL,
		apiURL:       apiBaseURL,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		redis:        rdb,
		mqtt:         mqttClient,
		mqttTopic:    mqttTopic,
	}
}

// Authorized reports whether the one-time OAuth setup has completed.
func (c *Client) Authorized(ctx context.Context) (bool, error) {
	if err := c.redis.Get(ctx, refreshTokenKey).Err(); err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return false, fmt.Errorf("read refresh token: %w", err)
	}
	return true, nil
}

// AuthorizeURL builds the Spotify consent-page URL the user must visit to
// start the authorization-code flow.
func (c *Client) AuthorizeURL(state string) string {
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {c.clientID},
		"scope":         {scopes},
		"redirect_uri":  {c.redirectURI},
		"state":         {state},
	}
	return c.accountsURL + "/authorize?" + q.Encode()
}

// Exchange trades the authorization code from the OAuth callback for
// tokens and stores them in Redis.
func (c *Client) Exchange(ctx context.Context, code string) error {
	tr, err := c.tokenRequest(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {c.redirectURI},
	})
	if err != nil {
		return fmt.Errorf("exchange authorization code: %w", err)
	}
	return c.storeTokens(ctx, tr)
}

// accessToken returns a valid access token, refreshing it via the stored
// refresh token once the cached one's TTL has run out.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	tok, err := c.redis.Get(ctx, accessTokenKey).Result()
	if err == nil {
		return tok, nil
	}
	if !errors.Is(err, redis.Nil) {
		return "", fmt.Errorf("read access token: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Another request may have refreshed while we waited on the lock.
	tok, err = c.redis.Get(ctx, accessTokenKey).Result()
	if err == nil {
		return tok, nil
	}
	if !errors.Is(err, redis.Nil) {
		return "", fmt.Errorf("read access token: %w", err)
	}

	refresh, err := c.redis.Get(ctx, refreshTokenKey).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotAuthorized
	}
	if err != nil {
		return "", fmt.Errorf("read refresh token: %w", err)
	}

	tr, err := c.tokenRequest(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
	})
	if err != nil {
		return "", fmt.Errorf("refresh access token: %w", err)
	}
	if err := c.storeTokens(ctx, tr); err != nil {
		return "", err
	}
	return tr.AccessToken, nil
}

func (c *Client) storeTokens(ctx context.Context, tr *tokenResponse) error {
	// Spotify only returns a refresh token on the initial exchange, or on
	// refresh when it rotates the old one.
	if tr.RefreshToken != "" {
		if err := c.redis.Set(ctx, refreshTokenKey, tr.RefreshToken, 0).Err(); err != nil {
			return fmt.Errorf("store refresh token: %w", err)
		}
	}

	ttl := time.Duration(tr.ExpiresIn)*time.Second - accessTokenSlack
	if ttl <= 0 {
		ttl = time.Duration(tr.ExpiresIn) * time.Second
	}
	if err := c.redis.Set(ctx, accessTokenKey, tr.AccessToken, ttl).Err(); err != nil {
		return fmt.Errorf("store access token: %w", err)
	}
	return nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// tokenRequest posts a form to the accounts token endpoint with client
// credentials as basic auth.
func (c *Client) tokenRequest(ctx context.Context, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.accountsURL+"/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	return &tr, nil
}

// apiGet performs an authenticated GET against the Spotify Web API.
// It returns found=false for a 204 response (nothing playing).
func (c *Client) apiGet(ctx context.Context, path string, out any) (bool, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL+path, nil)
	if err != nil {
		return false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return false, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("spotify returned %d: %s", resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}
	return true, nil
}

type image struct {
	URL string `json:"url"`
}

// item covers both track and episode payloads: tracks carry artists and
// album images, episodes carry a show name and top-level images.
type item struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	DurationMS int    `json:"duration_ms"`
	Artists    []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Images []image `json:"images"`
	} `json:"album"`
	Images []image `json:"images"`
	Show   struct {
		Name string `json:"name"`
	} `json:"show"`
}

func (i *item) artistNames() []string {
	if i.Type == "episode" {
		return []string{i.Show.Name}
	}
	names := make([]string, 0, len(i.Artists))
	for _, a := range i.Artists {
		names = append(names, a.Name)
	}
	return names
}

func (i *item) image() string {
	images := i.Album.Images
	if i.Type == "episode" {
		images = i.Images
	}
	if len(images) == 0 {
		return ""
	}
	return images[0].URL
}

// Current describes the playback state returned by CurrentPlayback.
type Current struct {
	Playing  bool     `json:"playing"`
	ID       string   `json:"id,omitempty"`
	Type     string   `json:"type,omitempty"`
	Name     string   `json:"name,omitempty"`
	Artists  []string `json:"artists,omitempty"`
	Length   int      `json:"length,omitempty"`
	Progress int      `json:"progress,omitempty"`
	Image    string   `json:"image,omitempty"`
	Device   *Device  `json:"device,omitempty"`
}

type Device struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// CurrentPlayback returns the user's playback state; Playing is false when
// nothing is playing (or only an ad, which carries no item).
func (c *Client) CurrentPlayback(ctx context.Context) (*Current, error) {
	var state struct {
		IsPlaying  bool   `json:"is_playing"`
		ProgressMS int    `json:"progress_ms"`
		Item       *item  `json:"item"`
		Device     Device `json:"device"`
	}
	found, err := c.apiGet(ctx, "/me/player?additional_types=episode", &state)
	if err != nil {
		return nil, err
	}
	if !found || state.Item == nil {
		return &Current{}, nil
	}

	return &Current{
		Playing:  state.IsPlaying,
		ID:       state.Item.ID,
		Type:     state.Item.Type,
		Name:     state.Item.Name,
		Artists:  state.Item.artistNames(),
		Length:   state.Item.DurationMS,
		Progress: state.ProgressMS,
		Image:    state.Item.image(),
		Device:   &Device{Name: state.Device.Name, Type: state.Device.Type},
	}, nil
}

// Listen is one entry of the user's recently-played history.
type Listen struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Name       string   `json:"name"`
	Artists    []string `json:"artists"`
	Length     int      `json:"length"`
	Image      string   `json:"image"`
	ListenedAt string   `json:"listened_at"`
}

// RecentListens returns the last limit (1-50) finished tracks.
func (c *Client) RecentListens(ctx context.Context, limit int) ([]Listen, error) {
	var history struct {
		Items []struct {
			Track    item   `json:"track"`
			PlayedAt string `json:"played_at"`
		} `json:"items"`
	}
	path := fmt.Sprintf("/me/player/recently-played?limit=%d", limit)
	if _, err := c.apiGet(ctx, path, &history); err != nil {
		return nil, err
	}

	listens := make([]Listen, 0, len(history.Items))
	for _, it := range history.Items {
		listens = append(listens, Listen{
			ID:         it.Track.ID,
			Type:       it.Track.Type,
			Name:       it.Track.Name,
			Artists:    it.Track.artistNames(),
			Length:     it.Track.DurationMS,
			Image:      it.Track.image(),
			ListenedAt: it.PlayedAt,
		})
	}
	return listens, nil
}

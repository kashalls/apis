package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// fakeSpotify stands in for both accounts.spotify.com and api.spotify.com.
type fakeSpotify struct {
	accessToken string // the token /me/player currently accepts
}

func (f *fakeSpotify) accountsHandler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "id" || pass != "secret" {
			t.Errorf("token request basic auth = %q/%q", user, pass)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}

		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			if code := r.PostForm.Get("code"); code != "good-code" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			f.accessToken = "access-1"
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		case "refresh_token":
			if rt := r.PostForm.Get("refresh_token"); rt != "refresh-1" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			f.accessToken = "access-2"
			fmt.Fprint(w, `{"access_token":"access-2","expires_in":3600}`)
		default:
			http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
		}
	})
}

func (f *fakeSpotify) apiHandler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+f.accessToken {
			t.Errorf("api request auth = %q, want bearer %q", got, f.accessToken)
		}
		switch r.URL.Path {
		case "/me/player":
			fmt.Fprint(w, `{
				"is_playing": true,
				"progress_ms": 4200,
				"device": {"name": "Desktop", "type": "Computer"},
				"item": {
					"id": "track1", "name": "Song", "type": "track", "duration_ms": 200000,
					"artists": [{"name": "Artist A"}, {"name": "Artist B"}],
					"album": {"images": [{"url": "https://img/1"}]}
				}
			}`)
		case "/me/player/recently-played":
			fmt.Fprint(w, `{"items": [{
				"played_at": "2026-07-30T12:00:00Z",
				"track": {
					"id": "track2", "name": "Older Song", "type": "track", "duration_ms": 100000,
					"artists": [{"name": "Artist C"}],
					"album": {"images": [{"url": "https://img/2"}]}
				}
			}]}`)
		default:
			http.NotFound(w, r)
		}
	})
}

func newTestClient(t *testing.T) (*Client, *miniredis.Miniredis) {
	fake := &fakeSpotify{}
	accounts := httptest.NewServer(fake.accountsHandler(t))
	t.Cleanup(accounts.Close)
	apiSrv := httptest.NewServer(fake.apiHandler(t))
	t.Cleanup(apiSrv.Close)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	c := NewClient("id", "secret", "http://localhost/api/setup", rdb, nil, "")
	c.accountsURL = accounts.URL
	c.apiURL = apiSrv.URL
	return c, mr
}

func TestTokenLifecycle(t *testing.T) {
	c, mr := newTestClient(t)
	ctx := context.Background()

	if authorized, err := c.Authorized(ctx); err != nil || authorized {
		t.Fatalf("Authorized before setup = %v, %v; want false, nil", authorized, err)
	}
	if _, err := c.CurrentPlayback(ctx); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("CurrentPlayback before setup err = %v, want ErrNotAuthorized", err)
	}

	if err := c.Exchange(ctx, "good-code"); err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if authorized, err := c.Authorized(ctx); err != nil || !authorized {
		t.Fatalf("Authorized after setup = %v, %v; want true, nil", authorized, err)
	}
	if got := mr.TTL(accessTokenKey); got != 3600*time.Second-accessTokenSlack {
		t.Errorf("access token TTL = %v, want %v", got, 3600*time.Second-accessTokenSlack)
	}

	current, err := c.CurrentPlayback(ctx)
	if err != nil {
		t.Fatalf("CurrentPlayback: %v", err)
	}
	if !current.Playing || current.Name != "Song" || len(current.Artists) != 2 ||
		current.Image != "https://img/1" || current.Device.Name != "Desktop" {
		t.Errorf("unexpected current playback: %+v", current)
	}

	// Expire the cached access token: the next call must refresh and use
	// the rotated access token (asserted by the fake API handler).
	mr.FastForward(3600 * time.Second)
	listens, err := c.RecentListens(ctx, 10)
	if err != nil {
		t.Fatalf("RecentListens after expiry: %v", err)
	}
	if len(listens) != 1 || listens[0].Name != "Older Song" || listens[0].ListenedAt != "2026-07-30T12:00:00Z" {
		t.Errorf("unexpected recent listens: %+v", listens)
	}
	if got, _ := mr.Get(accessTokenKey); got != "access-2" {
		t.Errorf("stored access token after refresh = %q, want access-2", got)
	}
	if got, _ := mr.Get(refreshTokenKey); got != "refresh-1" {
		t.Errorf("refresh token after refresh = %q, want refresh-1 (unrotated)", got)
	}

	if err := c.Exchange(ctx, "bad-code"); err == nil {
		t.Error("Exchange with bad code succeeded, want error")
	}
}

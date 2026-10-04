// Package wow is a client for the World of Warcraft Profile API
// (https://community.developer.battle.net/documentation/world-of-warcraft/profile-apis),
// used to read public character summaries for display.
package wow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kashalls/apis/internal/redisconn"
)

const (
	tokenURL = "https://oauth.battle.net/token"

	// cacheTTL is how long a cached character is served before the next
	// request for it triggers a live refetch.
	cacheTTL = 30 * time.Minute

	accessTokenKey   = "wow/access_token"
	accessTokenSlack = 30 * time.Second
)

// CharacterRef identifies one character to look up. Flavor picks the game
// version: "retail" uses the plain profile namespace, anything else (e.g.
// "classicann" for TBC Anniversary, "classic1x" for Classic Era) is used
// as the namespace suffix, as in "profile-classicann-us".
type CharacterRef struct {
	Flavor string
	Realm  string
	Name   string
}

// ParseCharacterRefs parses "flavor/realm-slug/name" entries.
func ParseCharacterRefs(entries []string) ([]CharacterRef, error) {
	refs := make([]CharacterRef, 0, len(entries))
	for _, e := range entries {
		parts := strings.Split(strings.TrimSpace(e), "/")
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return nil, fmt.Errorf("invalid wow character %q, want flavor/realm-slug/name", e)
		}
		refs = append(refs, CharacterRef{
			Flavor: strings.ToLower(parts[0]),
			Realm:  strings.ToLower(parts[1]),
			Name:   strings.ToLower(parts[2]),
		})
	}
	return refs, nil
}

type Client struct {
	apiURL       string
	httpClient   *http.Client
	redis        *redis.Client
	clientID     string
	clientSecret string
	region       string
	characters   []CharacterRef
}

func NewClient(clientID, clientSecret, region string, characters []CharacterRef, rdb *redis.Client) *Client {
	return &Client{
		apiURL:       fmt.Sprintf("https://%s.api.blizzard.com", region),
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		redis:        rdb,
		clientID:     clientID,
		clientSecret: clientSecret,
		region:       region,
		characters:   characters,
	}
}

// Character is the trimmed-down view of a character served to clients.
type Character struct {
	Flavor            string        `json:"flavor"`
	Name              string        `json:"name"`
	Realm             string        `json:"realm"`
	Level             int           `json:"level"`
	Race              string        `json:"race"`
	Class             string        `json:"class"`
	Spec              string        `json:"spec,omitempty"`
	SpecIcon          string        `json:"spec_icon,omitempty"`
	Faction           string        `json:"faction"`
	Guild             string        `json:"guild,omitempty"`
	ItemLevel         int           `json:"item_level"`
	AchievementPoints int           `json:"achievement_points,omitempty"`
	LastLogin         int64         `json:"last_login"`
	MythicRating      *MythicRating `json:"mythic_rating,omitempty"`
	Media             Media         `json:"media"`
}

type MythicRating struct {
	Rating float64 `json:"rating"`
	Color  string  `json:"color"`
}

// Media holds render URLs. Which ones Blizzard provides varies by flavor,
// so any of them may be empty.
type Media struct {
	Avatar string `json:"avatar,omitempty"`
	Inset  string `json:"inset,omitempty"`
	Main   string `json:"main,omitempty"`
	// MainRaw is the transparent full-body render, for characters whose
	// "main" scene render isn't available.
	MainRaw string `json:"main_raw,omitempty"`
}

// Characters returns every configured character that could be fetched,
// each cached in Redis for cacheTTL. A character that fails to load is
// logged and left out rather than failing the whole list.
func (c *Client) Characters(ctx context.Context) []Character {
	out := make([]Character, 0, len(c.characters))
	for _, ref := range c.characters {
		key := fmt.Sprintf("cache/wow/character/%s/%s/%s", ref.Flavor, ref.Realm, ref.Name)
		char, err := redisconn.Cached(ctx, c.redis, key, cacheTTL, func(ctx context.Context) (*Character, error) {
			return c.fetchCharacter(ctx, ref)
		})
		if err != nil {
			slog.Warn("fetch wow character", "flavor", ref.Flavor, "realm", ref.Realm, "name", ref.Name, "err", err)
			continue
		}
		out = append(out, *char)
	}
	return out
}

func (c *Client) namespace(flavor string) string {
	if flavor == "retail" {
		return "profile-" + c.region
	}
	return fmt.Sprintf("profile-%s-%s", flavor, c.region)
}

type nameRef struct {
	Name string `json:"name"`
}

type profileResponse struct {
	Name           string  `json:"name"`
	Level          int     `json:"level"`
	Race           nameRef `json:"race"`
	CharacterClass nameRef `json:"character_class"`
	ActiveSpec     struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	} `json:"active_spec"`
	Faction           nameRef `json:"faction"`
	Guild             nameRef `json:"guild"`
	Realm             nameRef `json:"realm"`
	EquippedItemLevel int     `json:"equipped_item_level"`
	AchievementPoints int     `json:"achievement_points"`
	LastLogin         int64   `json:"last_login_timestamp"`
}

type mediaResponse struct {
	Assets []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"assets"`
}

type mythicResponse struct {
	CurrentMythicRating *struct {
		Rating float64 `json:"rating"`
		Color  struct {
			R int `json:"r"`
			G int `json:"g"`
			B int `json:"b"`
		} `json:"color"`
	} `json:"current_mythic_rating"`
}

func (c *Client) fetchCharacter(ctx context.Context, ref CharacterRef) (*Character, error) {
	base := fmt.Sprintf("/profile/wow/character/%s/%s", url.PathEscape(ref.Realm), url.PathEscape(ref.Name))
	ns := c.namespace(ref.Flavor)

	var profile profileResponse
	if err := c.get(ctx, base, ns, &profile); err != nil {
		return nil, fmt.Errorf("fetch profile: %w", err)
	}

	char := &Character{
		Flavor:            ref.Flavor,
		Name:              profile.Name,
		Realm:             profile.Realm.Name,
		Level:             profile.Level,
		Race:              profile.Race.Name,
		Class:             profile.CharacterClass.Name,
		Spec:              profile.ActiveSpec.Name,
		Faction:           profile.Faction.Name,
		Guild:             profile.Guild.Name,
		ItemLevel:         profile.EquippedItemLevel,
		AchievementPoints: profile.AchievementPoints,
		LastLogin:         profile.LastLogin,
	}

	// Media and Mythic+ are extras: a character without renders or without
	// any keystone runs (which 404s) should still show up.
	var media mediaResponse
	if err := c.get(ctx, base+"/character-media", ns, &media); err == nil {
		for _, a := range media.Assets {
			switch a.Key {
			case "avatar":
				char.Media.Avatar = a.Value
			case "inset":
				char.Media.Inset = a.Value
			case "main":
				char.Media.Main = a.Value
			case "main-raw":
				char.Media.MainRaw = a.Value
			}
		}
	}

	if ref.Flavor == "retail" {
		var specMedia mediaResponse
		if profile.ActiveSpec.ID > 0 {
			path := fmt.Sprintf("/data/wow/media/playable-specialization/%d", profile.ActiveSpec.ID)
			if err := c.get(ctx, path, "static-"+c.region, &specMedia); err == nil {
				for _, a := range specMedia.Assets {
					if a.Key == "icon" {
						char.SpecIcon = a.Value
					}
				}
			}
		}

		var mythic mythicResponse
		if err := c.get(ctx, base+"/mythic-keystone-profile", ns, &mythic); err == nil && mythic.CurrentMythicRating != nil {
			r := mythic.CurrentMythicRating
			char.MythicRating = &MythicRating{
				Rating: r.Rating,
				Color:  fmt.Sprintf("#%02x%02x%02x", r.Color.R, r.Color.G, r.Color.B),
			}
		}
	}

	return char, nil
}

func (c *Client) get(ctx context.Context, path, namespace string, out any) error {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return err
	}

	q := url.Values{"namespace": {namespace}, "locale": {"en_US"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return c.do(req, out)
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// accessToken returns a client-credentials token, cached in Redis until
// shortly before Blizzard expires it.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	tok, err := c.redis.Get(ctx, accessTokenKey).Result()
	if err == nil {
		return tok, nil
	}
	if !errors.Is(err, redis.Nil) {
		return "", fmt.Errorf("read access token: %w", err)
	}

	form := url.Values{"grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var tr tokenResponse
	if err := c.do(req, &tr); err != nil {
		return "", fmt.Errorf("request access token: %w", err)
	}

	ttl := time.Duration(tr.ExpiresIn)*time.Second - accessTokenSlack
	if ttl > 0 {
		_ = c.redis.Set(ctx, accessTokenKey, tr.AccessToken, ttl).Err()
	}
	return tr.AccessToken, nil
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d: %s", req.URL.Path, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

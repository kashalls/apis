// Package overwatch reads public Overwatch career profiles. Blizzard has
// no official Overwatch API, so this goes through OverFast
// (https://overfast-api.tekrop.fr), which scrapes the public career pages.
// Profiles set to private in-game come back without ranks or stats.
package overwatch

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kashalls/apis/internal/httpclient"
	"github.com/kashalls/apis/internal/redisconn"
)

const (
	overfastURL = "https://overfast-api.tekrop.fr"

	// cacheTTL is how long a cached player is served before the next
	// request for it triggers a live refetch.
	cacheTTL = 30 * time.Minute

	topHeroes = 3
)

type Client struct {
	http       *httpclient.Client
	redis      *redis.Client
	battleTags []string
}

// NewClient takes BattleTags in either "Name#1234" or "Name-1234" form.
func NewClient(battleTags []string, rdb *redis.Client) *Client {
	ids := make([]string, 0, len(battleTags))
	for _, t := range battleTags {
		ids = append(ids, strings.ReplaceAll(strings.TrimSpace(t), "#", "-"))
	}
	return &Client{
		http:       httpclient.New(overfastURL, httpclient.WithHeader("User-Agent", "apis")),
		redis:      rdb,
		battleTags: ids,
	}
}

// Player is the trimmed-down view of a career profile served to clients.
type Player struct {
	BattleTag   string       `json:"battletag"`
	Username    string       `json:"username"`
	Title       string       `json:"title,omitempty"`
	Avatar      string       `json:"avatar,omitempty"`
	Namecard    string       `json:"namecard,omitempty"`
	Endorsement int          `json:"endorsement,omitempty"`
	Season      int          `json:"season,omitempty"`
	Ranks       []Rank       `json:"ranks"`
	Stats       *Stats       `json:"stats,omitempty"`
	TopHeroes   []HeroPlayed `json:"top_heroes"`
}

type Rank struct {
	Role     string `json:"role"`
	Division string `json:"division"`
	Tier     int    `json:"tier"`
	RankIcon string `json:"rank_icon"`
	RoleIcon string `json:"role_icon"`
}

type Stats struct {
	GamesPlayed int     `json:"games_played"`
	GamesWon    int     `json:"games_won"`
	TimePlayed  int     `json:"time_played"`
	Winrate     float64 `json:"winrate"`
	KDA         float64 `json:"kda"`
}

type HeroPlayed struct {
	Hero       string  `json:"hero"`
	TimePlayed int     `json:"time_played"`
	Winrate    float64 `json:"winrate"`
}

// Players returns every configured player that could be fetched, each
// cached in Redis for cacheTTL. A player that fails to load is logged and
// left out rather than failing the whole list.
func (c *Client) Players(ctx context.Context) []Player {
	out := make([]Player, 0, len(c.battleTags))
	for _, tag := range c.battleTags {
		p, err := redisconn.Cached(ctx, c.redis, "cache/overwatch/player/"+tag, cacheTTL, func(ctx context.Context) (*Player, error) {
			return c.fetchPlayer(ctx, tag)
		})
		if err != nil {
			slog.Warn("fetch overwatch player", "battletag", tag, "err", err)
			continue
		}
		out = append(out, *p)
	}
	return out
}

type rankResponse struct {
	Division string `json:"division"`
	Tier     int    `json:"tier"`
	RoleIcon string `json:"role_icon"`
	RankIcon string `json:"rank_icon"`
}

type platformRanks struct {
	Season  *int          `json:"season"`
	Tank    *rankResponse `json:"tank"`
	Damage  *rankResponse `json:"damage"`
	Support *rankResponse `json:"support"`
	Open    *rankResponse `json:"open"`
}

type summaryResponse struct {
	Username    string  `json:"username"`
	Avatar      *string `json:"avatar"`
	Namecard    *string `json:"namecard"`
	Title       *string `json:"title"`
	Endorsement *struct {
		Level int `json:"level"`
	} `json:"endorsement"`
	Competitive *struct {
		PC *platformRanks `json:"pc"`
	} `json:"competitive"`
}

type statsSummary struct {
	GamesPlayed int     `json:"games_played"`
	GamesWon    int     `json:"games_won"`
	TimePlayed  int     `json:"time_played"`
	Winrate     float64 `json:"winrate"`
	KDA         float64 `json:"kda"`
}

type statsResponse struct {
	General *statsSummary            `json:"general"`
	Heroes  map[string]*statsSummary `json:"heroes"`
}

func (c *Client) fetchPlayer(ctx context.Context, tag string) (*Player, error) {
	base := "/players/" + url.PathEscape(tag)

	var summary summaryResponse
	if err := c.http.Do(ctx, http.MethodGet, base+"/summary", nil, &summary); err != nil {
		return nil, fmt.Errorf("fetch summary: %w", err)
	}

	p := &Player{
		BattleTag: tag,
		Username:  summary.Username,
		Avatar:    deref(summary.Avatar),
		Namecard:  deref(summary.Namecard),
		Title:     deref(summary.Title),
		Ranks:     []Rank{},
		TopHeroes: []HeroPlayed{},
	}
	if summary.Endorsement != nil {
		p.Endorsement = summary.Endorsement.Level
	}
	if summary.Competitive != nil && summary.Competitive.PC != nil {
		pc := summary.Competitive.PC
		if pc.Season != nil {
			p.Season = *pc.Season
		}
		for _, r := range []struct {
			role string
			rank *rankResponse
		}{{"tank", pc.Tank}, {"damage", pc.Damage}, {"support", pc.Support}, {"open", pc.Open}} {
			if r.rank != nil {
				p.Ranks = append(p.Ranks, Rank{
					Role: r.role, Division: r.rank.Division, Tier: r.rank.Tier,
					RankIcon: r.rank.RankIcon, RoleIcon: r.rank.RoleIcon,
				})
			}
		}
	}

	// Stats are missing for private profiles; still show the summary.
	var stats statsResponse
	if err := c.http.Do(ctx, http.MethodGet, base+"/stats/summary?platform=pc", nil, &stats); err == nil {
		if g := stats.General; g != nil {
			p.Stats = &Stats{
				GamesPlayed: g.GamesPlayed, GamesWon: g.GamesWon, TimePlayed: g.TimePlayed,
				Winrate: g.Winrate, KDA: g.KDA,
			}
		}
		for hero, s := range stats.Heroes {
			if s != nil && s.TimePlayed > 0 {
				p.TopHeroes = append(p.TopHeroes, HeroPlayed{Hero: hero, TimePlayed: s.TimePlayed, Winrate: s.Winrate})
			}
		}
		sort.Slice(p.TopHeroes, func(i, j int) bool { return p.TopHeroes[i].TimePlayed > p.TopHeroes[j].TimePlayed })
		if len(p.TopHeroes) > topHeroes {
			p.TopHeroes = p.TopHeroes[:topHeroes]
		}
	}

	return p, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

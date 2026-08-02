// Package github is a client for GitHub's GraphQL API
// (https://docs.github.com/en/graphql), used to read a user's pinned
// repositories and contribution calendar.
package github

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kashalls/apis/internal/httpclient"
	"github.com/kashalls/apis/internal/redisconn"
)

const graphqlURL = "https://api.github.com/graphql"

// cacheTTL is how long a cached value is served before the next request
// for it triggers a live refetch.
const cacheTTL = 30 * time.Minute

type Client struct {
	http     *httpclient.Client
	redis    *redis.Client
	username string
}

func NewClient(username, token string, rdb *redis.Client) *Client {
	return &Client{
		http: httpclient.New(graphqlURL,
			httpclient.WithBearerToken(token),
			httpclient.WithHeader("User-Agent", "apis"),
		),
		redis:    rdb,
		username: username,
	}
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type gqlError struct {
	Message string `json:"message"`
}

type gqlResponse[T any] struct {
	Data   T          `json:"data"`
	Errors []gqlError `json:"errors,omitempty"`
}

// doQuery posts a GraphQL query to the API and unwraps its data/errors
// envelope. path is "" since GraphQL has a single endpoint (the client's
// base URL).
func doQuery[T any](ctx context.Context, c *Client, query string, vars map[string]any) (T, error) {
	var zero T
	var resp gqlResponse[T]
	if err := c.http.Do(ctx, http.MethodPost, "", gqlRequest{Query: query, Variables: vars}, &resp); err != nil {
		return zero, err
	}
	if len(resp.Errors) > 0 {
		return zero, fmt.Errorf("github graphql: %s", resp.Errors[0].Message)
	}
	return resp.Data, nil
}

// PinnedRepo is one of the user's pinned repositories.
type PinnedRepo struct {
	Owner       string `json:"owner"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Stars       int    `json:"stars"`
	Forks       int    `json:"forks"`
	Language    string `json:"language,omitempty"`
	PushedAt    string `json:"pushed_at"`
	URL         string `json:"url"`
}

const pinnedQuery = `
query GithubUserPins($login: String!) {
  user(login: $login) {
    pinnedItems(first: 6, types: [REPOSITORY]) {
      edges {
        node {
          ... on Repository {
            owner { login }
            name
            description
            stargazerCount
            forkCount
            primaryLanguage { name }
            pushedAt
            url
          }
        }
      }
    }
  }
}`

type pinnedResult struct {
	User struct {
		PinnedItems struct {
			Edges []struct {
				Node struct {
					Owner struct {
						Login string `json:"login"`
					} `json:"owner"`
					Name            string `json:"name"`
					Description     string `json:"description"`
					StargazerCount  int    `json:"stargazerCount"`
					ForkCount       int    `json:"forkCount"`
					PrimaryLanguage *struct {
						Name string `json:"name"`
					} `json:"primaryLanguage"`
					PushedAt string `json:"pushedAt"`
					URL      string `json:"url"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"pinnedItems"`
	} `json:"user"`
}

// Pinned returns the user's pinned repositories, cached in Redis for
// cacheTTL.
func (c *Client) Pinned(ctx context.Context) ([]PinnedRepo, error) {
	return redisconn.Cached(ctx, c.redis, "cache/github/pinned", cacheTTL, c.fetchPinned)
}

func (c *Client) fetchPinned(ctx context.Context) ([]PinnedRepo, error) {
	data, err := doQuery[pinnedResult](ctx, c, pinnedQuery, map[string]any{"login": c.username})
	if err != nil {
		return nil, fmt.Errorf("fetch pinned repos: %w", err)
	}

	edges := data.User.PinnedItems.Edges
	repos := make([]PinnedRepo, 0, len(edges))
	for _, e := range edges {
		lang := ""
		if e.Node.PrimaryLanguage != nil {
			lang = e.Node.PrimaryLanguage.Name
		}
		repos = append(repos, PinnedRepo{
			Owner:       e.Node.Owner.Login,
			Name:        e.Node.Name,
			Description: e.Node.Description,
			Stars:       e.Node.StargazerCount,
			Forks:       e.Node.ForkCount,
			Language:    lang,
			PushedAt:    e.Node.PushedAt,
			URL:         e.Node.URL,
		})
	}
	return repos, nil
}

// ContributionDay is one day of the contribution calendar.
type ContributionDay struct {
	Count int    `json:"count"`
	Date  string `json:"date"`
}

// Contributions is the token owner's contribution calendar for the year.
type Contributions struct {
	Graph              [][]ContributionDay `json:"graph"`
	TotalContributions int                 `json:"total_contributions"`
}

const contributionsQuery = `
query GithubContributions {
  viewer {
    contributionsCollection {
      contributionCalendar {
        totalContributions
        weeks {
          contributionDays {
            contributionCount
            date
          }
        }
      }
    }
  }
}`

type contributionsResult struct {
	Viewer struct {
		ContributionsCollection struct {
			ContributionCalendar struct {
				TotalContributions int `json:"totalContributions"`
				Weeks              []struct {
					ContributionDays []struct {
						ContributionCount int    `json:"contributionCount"`
						Date              string `json:"date"`
					} `json:"contributionDays"`
				} `json:"weeks"`
			} `json:"contributionCalendar"`
		} `json:"contributionsCollection"`
	} `json:"viewer"`
}

// Contributions returns the token owner's contribution calendar, cached
// in Redis for cacheTTL.
func (c *Client) Contributions(ctx context.Context) (*Contributions, error) {
	return redisconn.Cached(ctx, c.redis, "cache/github/contributions", cacheTTL, c.fetchContributions)
}

func (c *Client) fetchContributions(ctx context.Context) (*Contributions, error) {
	data, err := doQuery[contributionsResult](ctx, c, contributionsQuery, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch contributions: %w", err)
	}

	cal := data.Viewer.ContributionsCollection.ContributionCalendar
	graph := make([][]ContributionDay, 0, len(cal.Weeks))
	for _, w := range cal.Weeks {
		days := make([]ContributionDay, 0, len(w.ContributionDays))
		for _, d := range w.ContributionDays {
			days = append(days, ContributionDay{Count: d.ContributionCount, Date: d.Date})
		}
		graph = append(graph, days)
	}
	return &Contributions{Graph: graph, TotalContributions: cal.TotalContributions}, nil
}

package discord

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/bwmarrin/discordgo"
)

type Client struct {
	session *discordgo.Session
	userID  string
	guildID string
	store   *Store

	userMu     sync.Mutex
	cachedUser *discordgo.User
}

func NewClient(token, userID, guildID string, store *Store) (*Client, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}
	session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMembers | discordgo.IntentsGuildPresences

	c := &Client{
		session: session,
		userID:  userID,
		guildID: guildID,
		store:   store,
	}

	session.AddHandler(c.onGuildCreate)
	session.AddHandler(c.onPresenceUpdate)

	return c, nil
}

func (c *Client) Open() error {
	return c.session.Open()
}

func (c *Client) Close() error {
	return c.session.Close()
}

func (c *Client) onGuildCreate(s *discordgo.Session, g *discordgo.GuildCreate) {
	if c.guildID != "" && g.ID != c.guildID {
		return
	}
	for _, p := range g.Presences {
		if p.User != nil && p.User.ID == c.userID {
			user := c.resolveUser(s, g.ID, p.User)
			c.store.Set(buildPresence(user, p.Status, p.ClientStatus, p.Activities))
			return
		}
	}
}

func (c *Client) onPresenceUpdate(s *discordgo.Session, p *discordgo.PresenceUpdate) {
	if p.User == nil || p.User.ID != c.userID {
		return
	}
	if c.guildID != "" && p.GuildID != c.guildID {
		return
	}

	// The gateway payload's user object is often partial (id only); the
	// session state merges it with prior data, so read the merged copy back.
	merged, err := s.State.Presence(p.GuildID, c.userID)
	if err != nil {
		slog.Warn("could not read merged presence from state", "err", err)
		merged = &p.Presence
	}

	user := c.resolveUser(s, p.GuildID, merged.User)
	slog.Debug("presence update", "user_id", c.userID, "status", merged.Status)
	c.store.Set(buildPresence(user, merged.Status, merged.ClientStatus, merged.Activities))
}

// resolveUser fills in username/avatar/discriminator for the tracked user.
// Discord's gateway presence payloads (both in GUILD_CREATE and
// PRESENCE_UPDATE) almost always carry a partial user object - id only -
// since those fields are only attached to member events, not presence ones.
// The Guild Member REST endpoint always returns the full nested user, so it
// backs a cache that's used whenever the gateway data is incomplete.
func (c *Client) resolveUser(s *discordgo.Session, guildID string, partial *discordgo.User) *discordgo.User {
	c.userMu.Lock()
	defer c.userMu.Unlock()

	if partial != nil && partial.Username != "" {
		c.cachedUser = partial
		return partial
	}

	if c.cachedUser != nil {
		return c.cachedUser
	}

	member, err := s.GuildMember(guildID, c.userID)
	if err != nil {
		slog.Warn("could not fetch guild member for user details", "user_id", c.userID, "err", err)
		return partial
	}

	c.cachedUser = member.User
	return member.User
}

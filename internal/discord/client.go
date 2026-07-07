package discord

import (
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

type Client struct {
	session *discordgo.Session
	userID  string
	guildID string
	store   *Store
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

func (c *Client) onGuildCreate(_ *discordgo.Session, g *discordgo.GuildCreate) {
	if c.guildID != "" && g.ID != c.guildID {
		return
	}
	for _, p := range g.Presences {
		if p.User != nil && p.User.ID == c.userID {
			c.store.Set(buildPresence(p.User, p.Status, p.ClientStatus, p.Activities))
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

	slog.Debug("presence update", "user_id", c.userID, "status", merged.Status)
	c.store.Set(buildPresence(merged.User, merged.Status, merged.ClientStatus, merged.Activities))
}

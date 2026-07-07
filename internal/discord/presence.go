package discord

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type DiscordUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	GlobalName    string `json:"global_name,omitempty"`
	Avatar        string `json:"avatar,omitempty"`
}

type Spotify struct {
	TrackID     string     `json:"track_id,omitempty"`
	Timestamps  Timestamps `json:"timestamps"`
	Album       string     `json:"album"`
	AlbumArtURL string     `json:"album_art_url,omitempty"`
	Artist      string     `json:"artist"`
	Song        string     `json:"song"`
}

type Timestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

type Presence struct {
	DiscordUser            DiscordUser           `json:"discord_user"`
	DiscordStatus          string                `json:"discord_status"`
	Activities             []*discordgo.Activity `json:"activities"`
	ListeningToSpotify     bool                  `json:"listening_to_spotify"`
	Spotify                *Spotify              `json:"spotify"`
	ActiveOnDiscordDesktop bool                  `json:"active_on_discord_desktop"`
	ActiveOnDiscordMobile  bool                  `json:"active_on_discord_mobile"`
	ActiveOnDiscordWeb     bool                  `json:"active_on_discord_web"`
}

func buildPresence(user *discordgo.User, status discordgo.Status, clientStatus discordgo.ClientStatus, activities []*discordgo.Activity) *Presence {
	p := &Presence{
		DiscordStatus:          string(status),
		Activities:             activities,
		ActiveOnDiscordDesktop: clientStatus.Desktop != "",
		ActiveOnDiscordMobile:  clientStatus.Mobile != "",
		ActiveOnDiscordWeb:     clientStatus.Web != "",
	}

	if user != nil {
		p.DiscordUser = DiscordUser{
			ID:            user.ID,
			Username:      user.Username,
			Discriminator: user.Discriminator,
			GlobalName:    user.GlobalName,
			Avatar:        user.Avatar,
		}
	}

	for _, a := range activities {
		if a.Type == discordgo.ActivityTypeListening && a.Name == "Spotify" {
			p.ListeningToSpotify = true
			p.Spotify = spotifyFromActivity(a)
			break
		}
	}

	return p
}

func spotifyFromActivity(a *discordgo.Activity) *Spotify {
	s := &Spotify{
		Album:  a.Assets.LargeText,
		Artist: a.State,
		Song:   a.Details,
		Timestamps: Timestamps{
			Start: a.Timestamps.StartTimestamp,
			End:   a.Timestamps.EndTimestamp,
		},
	}
	if strings.HasPrefix(a.Assets.LargeImageID, "spotify:") {
		imageID := strings.TrimPrefix(a.Assets.LargeImageID, "spotify:")
		s.AlbumArtURL = fmt.Sprintf("https://i.scdn.co/image/%s", imageID)
	}
	return s
}

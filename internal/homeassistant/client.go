// Package homeassistant is a thin client for the Home Assistant REST API
// (https://developers.home-assistant.io/docs/api/rest/), used to call
// services on entities such as turning lights on with a given color.
package homeassistant

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kashalls/apis/internal/httpclient"
)

type Client struct {
	http *httpclient.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{http: httpclient.New(baseURL, httpclient.WithBearerToken(token))}
}

// RGB is a color expressed as 0-255 red/green/blue components.
type RGB struct {
	R, G, B int
}

// SetLightColor calls light.turn_on on entityID with only an RGB color, so
// it never touches the light's current brightness - the merge_variables
// payload simply omits a "brightness" field, and Home Assistant leaves
// omitted attributes untouched.
func (c *Client) SetLightColor(ctx context.Context, entityID string, color RGB) error {
	body := map[string]any{
		"entity_id": entityID,
		"rgb_color": []int{color.R, color.G, color.B},
	}
	if err := c.http.Do(ctx, http.MethodPost, "/api/services/light/turn_on", body, nil); err != nil {
		return fmt.Errorf("set light color: %w", err)
	}
	return nil
}

// Package trmnl pushes content to a TRMNL device via a private-plugin
// webhook (https://docs.trmnl.com/go/private-plugins/webhooks). The webhook
// only accepts a JSON body of "merge_variables" rendered by a template
// configured once in the TRMNL dashboard - it does not accept raw image
// bytes, so images are pushed as a URL merge variable instead.
package trmnl

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kashalls/juno/internal/httpclient"
)

type Client struct {
	http *httpclient.Client
}

func NewClient(webhookURL string) *Client {
	return &Client{http: httpclient.New(webhookURL)}
}

type webhookPayload struct {
	MergeVariables map[string]any `json:"merge_variables"`
}

func (c *Client) PushMergeVariables(ctx context.Context, vars map[string]any) error {
	if err := c.http.Do(ctx, http.MethodPost, "", webhookPayload{MergeVariables: vars}, nil); err != nil {
		return fmt.Errorf("push trmnl webhook: %w", err)
	}
	return nil
}

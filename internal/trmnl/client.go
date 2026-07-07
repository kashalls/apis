// Package trmnl pushes content to a TRMNL device via a private-plugin
// webhook (https://docs.trmnl.com/go/private-plugins/webhooks). The webhook
// only accepts a JSON body of "merge_variables" rendered by a template
// configured once in the TRMNL dashboard - it does not accept raw image
// bytes, so images are pushed as a URL merge variable instead.
package trmnl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Client struct {
	webhookURL string
	httpClient *http.Client
}

func NewClient(webhookURL string) *Client {
	return &Client{
		webhookURL: webhookURL,
		httpClient: &http.Client{},
	}
}

type webhookPayload struct {
	MergeVariables map[string]any `json:"merge_variables"`
}

func (c *Client) PushMergeVariables(ctx context.Context, vars map[string]any) error {
	body, err := json.Marshal(webhookPayload{MergeVariables: vars})
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("trmnl webhook returned status %d", resp.StatusCode)
	}
	return nil
}

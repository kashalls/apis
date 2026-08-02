// Package mqtt is a thin wrapper around paho.mqtt.golang for publishing
// retained state updates to a broker.
package mqtt

import (
	"fmt"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

type Client struct {
	client paho.Client
}

// New connects to brokerURL and blocks until the connection succeeds or
// times out. username/password are only set on the connection if
// non-empty (most brokers allow anonymous connections).
func New(brokerURL, username, password string) (*Client, error) {
	opts := paho.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID("apis").
		SetAutoReconnect(true).
		SetConnectRetry(true)
	if username != "" {
		opts.SetUsername(username).SetPassword(password)
	}

	c := paho.NewClient(opts)
	token := c.Connect()
	if !token.WaitTimeout(10*time.Second) || token.Error() != nil {
		return nil, fmt.Errorf("connect to mqtt broker: %w", token.Error())
	}
	return &Client{client: c}, nil
}

// Publish sends a retained message (so new subscribers immediately get
// the last known value) at QoS 0 (fire-and-forget; a missed update
// self-heals on the next change).
func (c *Client) Publish(topic string, payload []byte) error {
	token := c.client.Publish(topic, 0, true, payload)
	token.Wait()
	return token.Error()
}

func (c *Client) Close() {
	c.client.Disconnect(250)
}

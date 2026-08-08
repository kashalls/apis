// Package mqtt is a thin wrapper around paho.mqtt.golang for publishing
// and subscribing to a broker.
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
// non-empty (most brokers allow anonymous connections). The session is
// persistent (not clean), so the broker retains undelivered QoS-1/2
// messages for this client ID across brief reconnects.
func New(brokerURL, username, password string) (*Client, error) {
	opts := paho.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID("apis").
		SetCleanSession(false).
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

// Publish sends payload to topic at the given QoS, retained or not.
func (c *Client) Publish(topic string, payload []byte, qos byte, retained bool) error {
	token := c.client.Publish(topic, qos, retained, payload)
	token.Wait()
	return token.Error()
}

// Subscribe registers handler to be called with the payload of every
// message received on topic, at the given QoS.
func (c *Client) Subscribe(topic string, qos byte, handler func(payload []byte)) error {
	token := c.client.Subscribe(topic, qos, func(_ paho.Client, m paho.Message) {
		handler(m.Payload())
	})
	if !token.WaitTimeout(10*time.Second) || token.Error() != nil {
		return fmt.Errorf("subscribe to %s: %w", topic, token.Error())
	}
	return nil
}

func (c *Client) Close() {
	c.client.Disconnect(250)
}

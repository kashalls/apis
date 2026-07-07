// Package lanyard implements a websocket protocol compatible with
// Lanyard's (https://github.com/Phineas/lanyard) client-facing API, scoped
// to a single tracked Discord user.
package lanyard

// Opcodes, matching Lanyard's own protocol.
const (
	OpEvent      = 0
	OpHello      = 1
	OpInitialize = 2
	OpHeartbeat  = 3
)

// Event types sent under OpEvent.
const (
	EventInitState      = "INIT_STATE"
	EventPresenceUpdate = "PRESENCE_UPDATE"
)

type ServerMessage struct {
	Op   int    `json:"op"`
	Type string `json:"t,omitempty"`
	Data any    `json:"d,omitempty"`
}

type HelloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

type ClientMessage struct {
	Op   int            `json:"op"`
	Data InitializeData `json:"d,omitempty"`
}

type InitializeData struct {
	SubscribeToID  string   `json:"subscribe_to_id,omitempty"`
	SubscribeToIDs []string `json:"subscribe_to_ids,omitempty"`
}

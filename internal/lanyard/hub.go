package lanyard

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/kashalls/juno/internal/discord"
)

const heartbeatInterval = 30 * time.Second

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type Hub struct {
	store *discord.Store
}

func NewHub(store *discord.Store) *Hub {
	return &Hub{store: store}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("websocket upgrade failed", "err", err)
		return
	}
	defer conn.Close()

	if err := conn.WriteJSON(ServerMessage{
		Op:   OpHello,
		Data: HelloData{HeartbeatInterval: int(heartbeatInterval.Milliseconds())},
	}); err != nil {
		return
	}

	updates, unsubscribe := h.store.Subscribe()
	defer unsubscribe()

	// Client op payloads (Initialize/Heartbeat) don't change server
	// behavior here since there's only ever one tracked user to serve, but
	// reads must still be drained to detect disconnects and pings.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn.SetReadDeadline(time.Now().Add(heartbeatInterval * 2))
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	if p := h.store.Get(); p != nil {
		if err := conn.WriteJSON(ServerMessage{Op: OpEvent, Type: EventInitState, Data: p}); err != nil {
			return
		}
	}

	for {
		select {
		case <-done:
			return
		case p, ok := <-updates:
			if !ok {
				return
			}
			if err := conn.WriteJSON(ServerMessage{Op: OpEvent, Type: EventPresenceUpdate, Data: p}); err != nil {
				return
			}
		}
	}
}

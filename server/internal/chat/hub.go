package chat

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Event struct {
	Type      string   `json:"type"`
	Message   *Message `json:"message,omitempty"`
	MessageID int64    `json:"messageId,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type Client struct {
	conn     *websocket.Conn
	send     chan []byte
	deviceID int64
}

type Hub struct {
	mu       sync.Mutex
	clients  map[*Client]struct{}
	upgrader websocket.Upgrader
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*Client]struct{}),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// CheckOrigin is left nil on purpose: gorilla/websocket then only
			// accepts same-origin browser requests. Combined with the device
			// cookie this blocks cross-site WebSocket hijacking.
		},
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request, deviceID int64, onMessage func(*Client, []byte) error) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &Client{conn: conn, send: make(chan []byte, 32), deviceID: deviceID}

	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	ready, _ := json.Marshal(Event{Type: "ready"})
	client.send <- ready

	go h.writePump(client)
	defer func() {
		h.mu.Lock()
		if _, ok := h.clients[client]; ok {
			delete(h.clients, client)
			close(client.send)
		}
		h.mu.Unlock()
		_ = conn.Close()
	}()

	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage || len(payload) == 0 || onMessage == nil {
			continue
		}
		if err := onMessage(client, payload); err != nil {
			h.send(client, Event{Type: "error", Error: err.Error()})
		}
	}
}

func (h *Hub) Broadcast(event Event) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		select {
		case client.send <- payload:
		default:
			// A stalled client should not block every other connected device.
		}
	}
}

// OnlineDevices returns the IDs of devices that currently hold a WebSocket.
func (h *Hub) OnlineDevices() map[int64]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	online := make(map[int64]bool, len(h.clients))
	for client := range h.clients {
		online[client.deviceID] = true
	}
	return online
}

// DisconnectDevice tells every connection of a revoked device to sign out and
// then closes it. The "revoked" event lets the browser return to the pairing
// screen instead of reconnecting forever.
func (h *Hub) DisconnectDevice(deviceID int64) {
	payload, err := json.Marshal(Event{Type: "revoked"})
	if err != nil {
		return
	}

	h.mu.Lock()
	targets := make([]*Client, 0, 1)
	for client := range h.clients {
		if client.deviceID != deviceID {
			continue
		}
		targets = append(targets, client)
		select {
		case client.send <- payload:
		default:
		}
	}
	h.mu.Unlock()

	for _, client := range targets {
		go func(c *Client) {
			time.Sleep(300 * time.Millisecond)
			_ = c.conn.Close()
		}(client)
	}
}

func (h *Hub) send(client *Client, event Event) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	select {
	case client.send <- payload:
	default:
	}
}

func (h *Hub) writePump(client *Client) {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case payload, ok := <-client.send:
			if !ok {
				return
			}
			_ = client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := client.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			_ = client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

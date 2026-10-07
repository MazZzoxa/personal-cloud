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
	conn *websocket.Conn
	send chan []byte
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
			CheckOrigin: func(r *http.Request) bool {
				// Authentication is intentionally deferred to v0.4. The current
				// product is a self-hosted LAN app without device identity.
				return true
			},
		},
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request, onMessage func(*Client, []byte) error) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &Client{conn: conn, send: make(chan []byte, 32)}

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

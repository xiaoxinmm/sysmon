package websocket

import (
	"encoding/json"
	"sync"

	gorillaws "github.com/gorilla/websocket"
)

func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// Conn wraps a websocket connection with a write mutex
type Conn struct {
	conn *gorillaws.Conn
	mu   sync.Mutex // protects writes
}

// Hub manages WebSocket connections
type Hub struct {
	mu      sync.Mutex
	clients map[*Conn]bool
}

// NewHub creates a new Hub
func NewHub() *Hub {
	return &Hub{clients: make(map[*Conn]bool)}
}

// Add registers a new connection
func (h *Hub) Add(conn *gorillaws.Conn) int {
	wc := &Conn{conn: conn}
	h.mu.Lock()
	h.clients[wc] = true
	n := len(h.clients)
	h.mu.Unlock()
	return n
}

// Remove unregisters and closes a connection
func (h *Hub) Remove(conn *gorillaws.Conn) int {
	h.mu.Lock()
	var target *Conn
	for c := range h.clients {
		if c.conn == conn {
			target = c
			break
		}
	}
	if target != nil {
		delete(h.clients, target)
		conn.Close()
	}
	n := len(h.clients)
	h.mu.Unlock()
	return n
}

// Count returns the number of active clients
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Broadcast sends data to all connected clients
func (h *Hub) Broadcast(data []byte) {
	h.mu.Lock()
	// Snapshot the clients under lock
	clients := make([]*Conn, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	// Write to each connection with per-connection lock
	var failed []*Conn
	for _, c := range clients {
		c.mu.Lock()
		err := c.conn.WriteMessage(gorillaws.TextMessage, data)
		c.mu.Unlock()
		if err != nil {
			failed = append(failed, c)
		}
	}

	// Clean up failed connections
	if len(failed) > 0 {
		h.mu.Lock()
		for _, c := range failed {
			if h.clients[c] {
				delete(h.clients, c)
				c.conn.Close()
			}
		}
		h.mu.Unlock()
	}
}

// WriteTo sends data to a specific connection with write lock
func (h *Hub) WriteTo(conn *gorillaws.Conn, data []byte) error {
	h.mu.Lock()
	var target *Conn
	for c := range h.clients {
		if c.conn == conn {
			target = c
			break
		}
	}
	h.mu.Unlock()

	if target == nil {
		return nil
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	return target.conn.WriteMessage(gorillaws.TextMessage, data)
}

// BroadcastJSON sends JSON data to all connected clients
func (h *Hub) BroadcastJSON(v interface{}) error {
	data, err := jsonMarshal(v)
	if err != nil {
		return err
	}
	h.Broadcast(data)
	return nil
}

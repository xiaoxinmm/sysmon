// Copyright (C) 2025 Russell Li (xiaoxinmm)
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	"sysmon/monitor"

	"github.com/gorilla/websocket"
)

//go:embed web
var webFS embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]bool
}

func newHub() *hub {
	return &hub{clients: make(map[*websocket.Conn]bool)}
}

func (h *hub) add(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = true
	h.mu.Unlock()
}

func (h *hub) remove(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
	conn.Close()
}

func (h *hub) broadcast(data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		err := conn.WriteMessage(websocket.TextMessage, data)
		if err != nil {
			conn.Close()
			delete(h.clients, conn)
		}
	}
}

type wsMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

type Snapshot struct {
	Timestamp int64              `json:"timestamp"`
	System    monitor.SystemInfo `json:"system"`
	CPU       monitor.CPUInfo    `json:"cpu"`
	Memory    monitor.MemInfo    `json:"memory"`
	Load      monitor.LoadInfo   `json:"load"`
}

func collect() Snapshot {
	return Snapshot{
		Timestamp: time.Now().UnixMilli(),
		System:    monitor.GetSystemInfo(),
		CPU:       monitor.GetCPUInfo(),
		Memory:    monitor.GetMemInfo(),
		Load:      monitor.GetLoadInfo(),
	}
}

func main() {
	port := flag.Int("port", 8888, "listen port")
	flag.Parse()

	h := newHub()

	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}
	http.Handle("/", http.FileServer(http.FS(webContent)))

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("websocket upgrade error: %v", err)
			return
		}
		h.add(conn)

		snap := collect()
		initMsg := wsMessage{Type: "snapshot", Payload: snap}
		data, _ := json.Marshal(initMsg)
		conn.WriteMessage(websocket.TextMessage, data)

		go func() {
			defer h.remove(conn)
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					break
				}
			}
		}()
	})

	// background broadcaster
	go func() {
		ticker := time.NewTicker(1500 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			snap := collect()
			msg := wsMessage{Type: "snapshot", Payload: snap}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			h.broadcast(data)
		}
	}()

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("sysmon listening on http://0.0.0.0%s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

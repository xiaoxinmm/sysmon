package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

const shellIdleTimeout = 30 * time.Minute

// shellMessage is the JSON protocol for text messages on the shell websocket.
type shellMessage struct {
	Type string `json:"type"`           // "resize"
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// handleShell serves the /ws/shell endpoint.
// Binary websocket messages carry stdin/stdout bytes.
// Text websocket messages carry JSON control commands (resize).
func handleShell(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Security: password must be set
		if cfg.Password == "" {
			http.Error(w, "shell disabled: no password configured", http.StatusForbidden)
			return
		}
		// Security: EnableShell must be true
		if !cfg.EnableShell {
			http.Error(w, "shell disabled in config", http.StatusForbidden)
			return
		}
		// Security: must be authenticated
		if !isAuthenticated(r, cfg.Password) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("shell: websocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		// Determine shell
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}

		cmd := exec.Command(shell)
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")

		ptmx, err := pty.Start(cmd)
		if err != nil {
			log.Printf("shell: failed to start pty: %v", err)
			conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","data":"failed to start shell"}`))
			return
		}

		// Cleanup on exit
		var closeOnce sync.Once
		cleanup := func() {
			closeOnce.Do(func() {
				ptmx.Close()
				if cmd.Process != nil {
					cmd.Process.Kill()
					cmd.Wait()
				}
			})
		}
		defer cleanup()

		// Idle timer
		idleTimer := time.NewTimer(shellIdleTimeout)
		defer idleTimer.Stop()

		resetIdle := func() {
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(shellIdleTimeout)
		}

		// PTY → WebSocket (stdout)
		done := make(chan struct{})
		go func() {
			defer close(done)
			buf := make([]byte, 4096)
			for {
				n, err := ptmx.Read(buf)
				if err != nil {
					return
				}
				if n > 0 {
					if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
						return
					}
				}
			}
		}()

		// WebSocket → PTY (stdin) + control messages
		go func() {
			defer cleanup()
			for {
				msgType, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				resetIdle()

				if msgType == websocket.TextMessage {
					// JSON control message
					var msg shellMessage
					if err := json.Unmarshal(data, &msg); err == nil {
						if msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
							pty.Setsize(ptmx, &pty.Winsize{
								Cols: msg.Cols,
								Rows: msg.Rows,
							})
						}
					}
				} else if msgType == websocket.BinaryMessage {
					// stdin data
					ptmx.Write(data)
				}
			}
		}()

		// Wait for PTY exit or idle timeout
		select {
		case <-done:
			// PTY closed
		case <-idleTimer.C:
			log.Printf("shell: session idle timeout, disconnecting")
			conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","data":"session timed out (30min idle)"}`))
		}
	}
}

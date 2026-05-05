package terminal

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	gorillaws "github.com/gorilla/websocket"
)

type wsConn struct {
	conn *gorillaws.Conn
	mu   sync.Mutex
}

func (w *wsConn) WriteMessage(messageType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(messageType, data)
}

const shellIdleTimeout = 30 * time.Minute

// ShellMessage is the JSON protocol for text messages on the shell websocket
type ShellMessage struct {
	Type string `json:"type"`           // "resize"
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// AuthChecker is a function that checks if a request is authenticated
type AuthChecker func(r *http.Request, password string) bool

// ShellTokenValidator validates a shell token
type ShellTokenValidator func(token, shellPassword string) bool

// HandleShell returns an HTTP handler for the /ws/shell endpoint.
// It requires the caller to provide auth checking and the WebSocket upgrader.
func HandleShell(
	shellEnabled bool,
	password string,
	shellPassword string,
	isAuth AuthChecker,
	validateToken ShellTokenValidator,
	upgrader *gorillaws.Upgrader,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !shellEnabled {
			http.Error(w, "shell disabled", http.StatusForbidden)
			return
		}
		if !isAuth(r, password) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		shellToken := r.URL.Query().Get("shell_token")
		if shellToken == "" || !validateToken(shellToken, shellPassword) {
			slog.Warn("shell: invalid or expired token", "remote", r.RemoteAddr)
			http.Error(w, "shell token invalid or expired", http.StatusUnauthorized)
			return
		}

		rawConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("shell: websocket upgrade failed", "error", err, "remote", r.RemoteAddr)
			return
		}
		conn := &wsConn{conn: rawConn}
		defer rawConn.Close()

		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}

		cmd := exec.Command(shell)
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")

		ptmx, err := pty.Start(cmd)
		if err != nil {
			slog.Error("shell: failed to start pty", "error", err)
			conn.WriteMessage(gorillaws.TextMessage, []byte(`{"type":"error","data":"failed to start shell"}`))
			return
		}
		slog.Info("shell: session started", "remote", r.RemoteAddr, "shell", shell)

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

		// PTY -> WebSocket (stdout)
		done := make(chan struct{})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("shell: panic in PTY->WS goroutine", "panic", r)
				}
			}()
			defer close(done)
			buf := make([]byte, 4096)
			for {
				n, err := ptmx.Read(buf)
				if err != nil {
					return
				}
				if n > 0 {
					if err := conn.WriteMessage(gorillaws.BinaryMessage, buf[:n]); err != nil {
						return
					}
				}
			}
		}()

		// WebSocket -> PTY (stdin) + control messages
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("shell: panic in WS->PTY goroutine", "panic", r)
				}
			}()
			defer cleanup()
			for {
				msgType, data, err := rawConn.ReadMessage()
				if err != nil {
					return
				}
				resetIdle()

				if msgType == gorillaws.TextMessage {
					var msg ShellMessage
					if err := json.Unmarshal(data, &msg); err == nil {
						if msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
							pty.Setsize(ptmx, &pty.Winsize{
								Cols: msg.Cols,
								Rows: msg.Rows,
							})
						}
					}
				} else if msgType == gorillaws.BinaryMessage {
					ptmx.Write(data)
				}
			}
		}()

		select {
		case <-done:
		case <-idleTimer.C:
			slog.Info("shell: session idle timeout, disconnecting", "remote", r.RemoteAddr)
			conn.WriteMessage(gorillaws.TextMessage, []byte(`{"type":"error","data":"session timed out (30min idle)"}`))
		}
	}
}

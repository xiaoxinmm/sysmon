package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"sysmon/monitor"

	"github.com/gorilla/websocket"
)

//go:embed web
var webFS embed.FS

// ---------- Config ----------

type Config struct {
	Port            int     `json:"port"`
	RefreshInterval float64 `json:"refresh_interval"`
	MaxProcesses    int     `json:"max_processes"`
	Password        string  `json:"password"`
	HistoryDuration int     `json:"history_duration"`
}

func defaultConfig() Config {
	return Config{
		Port:            8888,
		RefreshInterval: 1.5,
		MaxProcesses:    50,
		Password:        "",
		HistoryDuration: 3600,
	}
}

func loadConfig(path string) Config {
	cfg := defaultConfig()
	if path == "" {
		// try default
		if _, err := os.Stat("/etc/sysmon.json"); err == nil {
			path = "/etc/sysmon.json"
		}
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			json.Unmarshal(data, &cfg)
			log.Printf("loaded config from %s", path)
		}
	}
	// env overrides
	if v := os.Getenv("PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Port = n
		}
	}
	if v := os.Getenv("SYSMON_PASSWORD"); v != "" {
		cfg.Password = v
	}
	if v := os.Getenv("SYSMON_REFRESH"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.RefreshInterval = f
		}
	}
	if v := os.Getenv("SYSMON_MAX_PROCS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxProcesses = n
		}
	}
	if v := os.Getenv("SYSMON_HISTORY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.HistoryDuration = n
		}
	}
	// sanity
	if cfg.Port <= 0 {
		cfg.Port = 8888
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 1.5
	}
	if cfg.MaxProcesses <= 0 {
		cfg.MaxProcesses = 50
	}
	if cfg.HistoryDuration <= 0 {
		cfg.HistoryDuration = 3600
	}
	return cfg
}

// ---------- Auth ----------

var authSecret []byte

func initAuthSecret() {
	authSecret = make([]byte, 32)
	rand.Read(authSecret)
}

func generateToken(password string) string {
	expiry := time.Now().Add(24 * time.Hour).Unix()
	payload := fmt.Sprintf("%s:%d", password, expiry)
	mac := hmac.New(sha256.New, authSecret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%d:%s", expiry, sig)
}

func validateToken(token string, password string) bool {
	// parse "expiry:signature"
	var expiry int64
	var sig string
	n, _ := fmt.Sscanf(token, "%d:%s", &expiry, &sig)
	if n != 2 {
		return false
	}
	if time.Now().Unix() > expiry {
		return false
	}
	payload := fmt.Sprintf("%s:%d", password, expiry)
	mac := hmac.New(sha256.New, authSecret)
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

func isAuthenticated(r *http.Request, password string) bool {
	if password == "" {
		return true
	}
	// check cookie
	cookie, err := r.Cookie("sysmon_token")
	if err == nil && validateToken(cookie.Value, password) {
		return true
	}
	// check query param (for websocket)
	if t := r.URL.Query().Get("token"); t != "" && validateToken(t, password) {
		return true
	}
	return false
}

// ---------- Hub ----------

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

// ---------- WS messages ----------

type wsMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// ---------- Main ----------

func main() {
	configPath := flag.String("config", "", "config file path (JSON)")
	portFlag := flag.Int("port", 0, "listen port (overrides config)")
	flag.Parse()

	cfg := loadConfig(*configPath)
	if *portFlag > 0 {
		cfg.Port = *portFlag
	}
	monitor.SetHistoryCapacity(cfg.HistoryDuration)
	initAuthSecret()

	h := newHub()

	// Serve embedded web files
	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}
	fileServer := http.FileServer(http.FS(webContent))

	// Auth middleware
	authRequired := cfg.Password != ""

	// Login page handler
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			r.ParseForm()
			pw := r.FormValue("password")
			if pw == cfg.Password {
				token := generateToken(cfg.Password)
				http.SetCookie(w, &http.Cookie{
					Name:     "sysmon_token",
					Value:    token,
					Path:     "/",
					MaxAge:   86400,
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
				})
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"token": token})
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"wrong password"}`))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(loginPage))
	})

	// Static files (with auth check)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if authRequired && !isAuthenticated(r, cfg.Password) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	// WebSocket
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if authRequired && !isAuthenticated(r, cfg.Password) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("websocket upgrade error: %v", err)
			return
		}
		h.add(conn)

		// Send initial snapshot
		snap := monitor.Collect(cfg.MaxProcesses)
		initMsg := wsMessage{Type: "snapshot", Payload: snap}
		data, _ := json.Marshal(initMsg)
		conn.WriteMessage(websocket.TextMessage, data)

		// Send history
		history := monitor.GetHistory()
		if len(history) > 0 {
			histMsg := wsMessage{Type: "history", Payload: history}
			data, _ := json.Marshal(histMsg)
			conn.WriteMessage(websocket.TextMessage, data)
		}

		// Read loop to detect disconnect
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

	// API snapshot
	http.HandleFunc("/api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if authRequired && !isAuthenticated(r, cfg.Password) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		snap := monitor.Collect(cfg.MaxProcesses)
		json.NewEncoder(w).Encode(snap)
	})

	// Background system stats broadcaster
	go func() {
		interval := time.Duration(cfg.RefreshInterval * float64(time.Second))
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			snap := monitor.Collect(cfg.MaxProcesses)
			// Record history
			monitor.RecordHistory(snap.CPU.AvgUsage, snap.Memory.UsedPercent)

			msg := wsMessage{Type: "snapshot", Payload: snap}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			h.broadcast(data)
		}
	}()

	// Background docker broadcaster (every 5 seconds)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			containers := monitor.GetDockerContainers()
			if containers == nil {
				continue
			}
			msg := wsMessage{Type: "docker", Payload: containers}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			h.broadcast(data)
		}
	}()

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("sysmon listening on http://0.0.0.0%s", addr)
	if authRequired {
		log.Printf("authentication enabled")
	}
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

// ---------- Login page ----------

const loginPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>sysmon - login</title>
<link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><rect width='100' height='100' rx='12' fill='%230d1117'/><text x='50' y='38' font-family='monospace' font-size='24' fill='%2300ff41' text-anchor='middle'>SYS</text><text x='50' y='62' font-family='monospace' font-size='18' fill='%2358a6ff' text-anchor='middle'>MON</text><rect x='15' y='72' width='70' height='4' rx='2' fill='%2300ff41' opacity='0.6'/><rect x='15' y='80' width='45' height='4' rx='2' fill='%2358a6ff' opacity='0.6'/></svg>">
<style>
  *, *::before, *::after { margin: 0; padding: 0; box-sizing: border-box; }
  :root {
    --bg: #0d1117; --card: #161b22; --border: #21262d;
    --text: #c9d1d9; --text-dim: #8b949e; --green: #00ff41;
    --red: #f85149; --font: 'SF Mono','Cascadia Code','Fira Code','Consolas',monospace;
  }
  body { font-family: var(--font); background: var(--bg); color: var(--text); display: flex; align-items: center; justify-content: center; min-height: 100vh; }
  .login-box { background: var(--card); border: 1px solid var(--border); border-radius: 8px; padding: 32px; width: 360px; max-width: 90vw; }
  .login-box h1 { color: var(--green); font-size: 1.2rem; margin-bottom: 8px; letter-spacing: 2px; }
  .login-box p { color: var(--text-dim); font-size: 0.8rem; margin-bottom: 20px; }
  .login-box input {
    width: 100%; padding: 10px 12px; background: var(--bg); border: 1px solid var(--border);
    border-radius: 4px; color: var(--text); font-family: var(--font); font-size: 0.9rem; outline: none;
  }
  .login-box input:focus { border-color: var(--green); }
  .login-box button {
    width: 100%; padding: 10px; margin-top: 12px; background: rgba(0,255,65,0.1); border: 1px solid var(--green);
    color: var(--green); border-radius: 4px; font-family: var(--font); font-size: 0.85rem; cursor: pointer;
  }
  .login-box button:hover { background: rgba(0,255,65,0.2); }
  .error { color: var(--red); font-size: 0.8rem; margin-top: 8px; display: none; }
</style>
</head>
<body>
<div class="login-box">
  <h1>sysmon</h1>
  <p>Enter password to continue</p>
  <input type="password" id="pw" placeholder="password" autofocus>
  <button id="btn" onclick="doLogin()">Login</button>
  <div class="error" id="err">Wrong password</div>
</div>
<script>
document.getElementById('pw').addEventListener('keydown',function(e){if(e.key==='Enter')doLogin();});
function doLogin(){
  var pw=document.getElementById('pw').value;
  fetch('/login',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'password='+encodeURIComponent(pw)})
  .then(function(r){
    if(r.ok){
      r.json().then(function(d){ localStorage.setItem('sysmon_token',d.token); window.location.href='/'; });
    } else {
      document.getElementById('err').style.display='block';
      document.getElementById('pw').value='';
      document.getElementById('pw').focus();
    }
  });
}
</script>
</body>
</html>`

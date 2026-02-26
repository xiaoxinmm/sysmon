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
	"strings"
	"sync"
	"time"

	"sysmon/monitor"

	"github.com/gorilla/websocket"
)

// TODO: 支持 TOML 配置

// Config 存放所有运行时配置
type Config struct {
	Port            int    `json:"port"`
	RefreshInterval int    `json:"refreshInterval"` // milliseconds
	MaxProcesses    int    `json:"maxProcesses"`
	Password        string `json:"password"`
	HistoryDuration int    `json:"historyDuration"` // seconds
}

func defaultConfig() Config {
	return Config{
		Port:            8888,
		RefreshInterval: 1500,
		MaxProcesses:    50,
		Password:        "",
		HistoryDuration: 3600,
	}
}

func loadConfig(path string) Config {
	cfg := defaultConfig()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("config: failed to read %s, using defaults: %v", path, err)
		} else {
			if err := json.Unmarshal(data, &cfg); err != nil {
				log.Printf("config: failed to parse %s: %v", path, err)
				cfg = defaultConfig()
			}
		}
	}

	// 环境变量覆盖
	if v := os.Getenv("PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Port = n
		}
	}
	if v := os.Getenv("SYSMON_PASSWORD"); v != "" {
		cfg.Password = v
	}
	if v := os.Getenv("SYSMON_REFRESH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RefreshInterval = n
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

	return cfg
}

// --- auth stuff ---

var authSecret []byte

func initAuthSecret() {
	authSecret = make([]byte, 32)
	if _, err := rand.Read(authSecret); err != nil {
		log.Fatal("failed to generate auth secret:", err)
	}
}

func generateToken(password string) string {
	expiry := time.Now().Add(24 * time.Hour).Unix()
	payload := fmt.Sprintf("%d:%s", expiry, password)
	mac := hmac.New(sha256.New, authSecret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%d:%s", expiry, sig)
}

func validateToken(token, password string) bool {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return false
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix() > expiry {
		return false
	}
	payload := fmt.Sprintf("%d:%s", expiry, password)
	mac := hmac.New(sha256.New, authSecret)
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

func isAuthenticated(r *http.Request, password string) bool {
	if password == "" {
		return true
	}
	// check URL query token
	if t := r.URL.Query().Get("token"); t != "" {
		return validateToken(t, password)
	}
	// check cookie
	if c, err := r.Cookie("sysmon_token"); err == nil {
		return validateToken(c.Value, password)
	}
	return false
}

// 嵌入的登录页，懒得拆文件了
const loginPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>sysmon - login</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:'SF Mono','Cascadia Code','Fira Code',Consolas,monospace;
background:#0d1117;color:#c9d1d9;display:flex;justify-content:center;align-items:center;min-height:100vh}
.box{background:#161b22;border:1px solid #21262d;border-radius:8px;padding:32px;width:320px;text-align:center}
h1{font-size:1.1rem;color:#00ff41;margin-bottom:24px;letter-spacing:1px}
input{width:100%;padding:10px 12px;background:#0d1117;border:1px solid #21262d;border-radius:4px;
color:#c9d1d9;font-family:inherit;font-size:0.9rem;margin-bottom:16px;outline:none}
input:focus{border-color:#00ff41}
button{width:100%;padding:10px;background:#238636;border:none;border-radius:4px;
color:#fff;font-family:inherit;font-size:0.9rem;cursor:pointer;font-weight:600}
button:hover{background:#2ea043}
.err{color:#f85149;font-size:0.8rem;margin-top:12px;display:none}
</style>
</head>
<body>
<div class="box">
<h1>🔒 sysmon</h1>
<form id="f">
<input type="password" id="pw" placeholder="password" autofocus>
<button type="submit">login</button>
</form>
<div class="err" id="err">wrong password</div>
</div>
<script>
document.getElementById('f').onsubmit=async function(e){
  e.preventDefault();
  const pw=document.getElementById('pw').value;
  const res=await fetch('/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:pw})});
  if(res.ok){
    const d=await res.json();
    document.cookie='sysmon_token='+d.token+';path=/;max-age=86400';
    location.href='/';
  }else{
    document.getElementById('err').style.display='block';
  }
};
</script>
</body>
</html>`

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
	Timestamp int64                `json:"timestamp"`
	System    monitor.SystemInfo   `json:"system"`
	CPU       monitor.CPUInfo      `json:"cpu"`
	Memory    monitor.MemInfo      `json:"memory"`
	Disks     []monitor.DiskInfo   `json:"disks"`
	Network   []monitor.NetInfo    `json:"network"`
	Load      monitor.LoadInfo     `json:"load"`
	Processes []monitor.ProcessInfo `json:"processes"`
}

func collect(maxProcesses int) Snapshot {
	return Snapshot{
		Timestamp: time.Now().UnixMilli(),
		System:    monitor.GetSystemInfo(),
		CPU:       monitor.GetCPUInfo(),
		Memory:    monitor.GetMemInfo(),
		Disks:     monitor.GetDiskInfo(),
		Network:   monitor.GetNetInfo(),
		Load:      monitor.GetLoadInfo(),
		Processes: monitor.GetProcesses(maxProcesses),
	}
}

func main() {
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	cfg := loadConfig(*configPath)

	initAuthSecret()

	// 设置 history 容量
	monitor.SetHistoryCapacity(cfg.HistoryDuration)

	h := newHub()

	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	// login handler
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var req struct {
				Password string `json:"password"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", 400)
				return
			}
			if req.Password != cfg.Password {
				http.Error(w, "unauthorized", 401)
				return
			}
			token := generateToken(cfg.Password)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"token": token})
			return
		}
		// GET: show login page
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, loginPage)
	})

	// static files with auth
	fileServer := http.FileServer(http.FS(webContent))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !isAuthenticated(r, cfg.Password) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if !isAuthenticated(r, cfg.Password) {
			http.Error(w, "unauthorized", 401)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("websocket upgrade error: %v", err)
			return
		}
		h.add(conn)

		snap := collect(cfg.MaxProcesses)
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
		ticker := time.NewTicker(time.Duration(cfg.RefreshInterval) * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			snap := collect(cfg.MaxProcesses)
			// record history
			monitor.RecordHistory(snap.CPU.AvgUsage, snap.Memory.UsedPercent)

			msg := wsMessage{Type: "snapshot", Payload: snap}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			h.broadcast(data)
		}
	}()

	// docker stats, less frequent
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
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

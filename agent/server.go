package agent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// AgentInfo stores information about a connected agent
type AgentInfo struct {
	Hostname string    `json:"hostname"`
	IP       string    `json:"ip"`
	LastSeen time.Time `json:"last_seen"`
	Snapshot *Snapshot `json:"snapshot"`
	Online   bool      `json:"online"`
}

// AgentServer manages multiple agent connections
type AgentServer struct {
	mu      sync.RWMutex
	agents  map[string]*AgentInfo
	apiKeys map[string]bool
}

// NewAgentServer creates a new agent server
func NewAgentServer(apiKeys []string) *AgentServer {
	keyMap := make(map[string]bool)
	for _, key := range apiKeys {
		if key != "" {
			keyMap[key] = true
		}
	}

	s := &AgentServer{
		agents:  make(map[string]*AgentInfo),
		apiKeys: keyMap,
	}

	// Start background goroutine to mark agents offline
	go s.monitorAgents()

	return s
}

// monitorAgents periodically checks agent status
func (s *AgentServer) monitorAgents() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for hostname, info := range s.agents {
			// Mark offline if no heartbeat for 5 minutes
			if now.Sub(info.LastSeen) > 5*time.Minute {
				if info.Online {
					slog.Warn("agent marked offline", "hostname", hostname)
					info.Online = false
				}
			}
		}
		s.mu.Unlock()
	}
}

// HandleReport handles agent report requests
func (s *AgentServer) HandleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Get headers
	hostname := r.Header.Get("X-Agent-Hostname")
	signature := r.Header.Get("X-Agent-Signature")

	if hostname == "" || signature == "" {
		http.Error(w, "missing required headers", http.StatusBadRequest)
		return
	}

	// Verify signature
	if !s.verifySignature(body, signature) {
		slog.Warn("agent: invalid signature", "hostname", hostname, "ip", r.RemoteAddr)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Parse snapshot
	var snap Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Extract IP address
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = fwd
	}

	// Update agent info
	s.mu.Lock()
	info, exists := s.agents[hostname]
	if !exists {
		info = &AgentInfo{
			Hostname: hostname,
			IP:       ip,
		}
		s.agents[hostname] = info
		slog.Info("agent: new agent registered", "hostname", hostname, "ip", ip)
	}
	info.LastSeen = time.Now()
	info.Snapshot = &snap
	info.Online = true
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleList returns list of all agents
func (s *AgentServer) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	agents := make([]*AgentInfo, 0, len(s.agents))
	for _, info := range s.agents {
		agents = append(agents, info)
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    agents,
	})
}

// verifySignature checks if the signature is valid for any of the configured API keys
func (s *AgentServer) verifySignature(data []byte, signature string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for apiKey := range s.apiKeys {
		h := hmac.New(sha256.New, []byte(apiKey))
		h.Write(data)
		expected := hex.EncodeToString(h.Sum(nil))
		if hmac.Equal([]byte(expected), []byte(signature)) {
			return true
		}
	}
	return false
}

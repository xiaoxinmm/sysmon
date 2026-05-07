package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"
)

// Config stores all runtime configuration
type Config struct {
	// System monitoring
	Port                 int    `json:"port"`
	RefreshInterval      int    `json:"refreshInterval"`        // milliseconds
	MaxProcesses         int    `json:"maxProcesses"`
	Password             string `json:"password"`
	HistoryDuration      int    `json:"historyDuration"`        // seconds
	EnableShell          bool   `json:"enableShell"`
	ShellPassword        string `json:"shell_password"`
	HistoryFile          string `json:"history_file"`           // legacy JSON
	HistoryDB            string `json:"history_db"`             // SQLite path
	HistoryRetentionDays int    `json:"history_retention_days"`
	LogLevel             string `json:"logLevel"`
	LogFormat            string `json:"logFormat"`

	// Traffic monitoring
	EnableTraffic   bool   `json:"enable_traffic"`
	TrafficDB       string `json:"traffic_db"`
	TrafficInterval int    `json:"traffic_interval"` // seconds between snapshots
	TrafficIface    string `json:"traffic_iface"`

	// Agent mode
	AgentMode     string   `json:"agent_mode"`      // "master", "agent", "" (disabled)
	AgentMaster   string   `json:"agent_master"`    // Master URL (for agent mode)
	AgentAPIKey   string   `json:"agent_api_key"`   // API Key
	AgentKeys     []string `json:"agent_keys"`      // Allowed API Keys (for master mode)
	AgentInterval int      `json:"agent_interval"`  // Report interval seconds
}

// ShellEnabled returns true only when shell is explicitly enabled AND shell_password is set
func (c Config) ShellEnabled() bool {
	return c.EnableShell && c.ShellPassword != ""
}

func defaultConfig() Config {
	return Config{
		Port:                 8888,
		RefreshInterval:      1500,
		MaxProcesses:         50,
		Password:             "",
		HistoryDuration:      3600,
		HistoryDB:            "./sysmon.db",
		HistoryRetentionDays: 7,
		LogLevel:             "info",
		LogFormat:            "text",
		EnableTraffic:        false,
		TrafficDB:            "./sysmon.db",
		TrafficInterval:      60,
		AgentMode:            "",
		AgentInterval:        60,
	}
}

// LoadConfig loads config from a JSON file, with environment variable overrides
func LoadConfig(path string) Config {
	cfg := defaultConfig()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("config: failed to read file, using defaults", "path", path, "error", err)
		} else {
			if err := json.Unmarshal(data, &cfg); err != nil {
				slog.Warn("config: failed to parse file, using defaults", "path", path, "error", err)
				cfg = defaultConfig()
			}
		}
	}

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
	if v := os.Getenv("SYSMON_HISTORY_FILE"); v != "" {
		cfg.HistoryFile = v
	}
	if v := os.Getenv("SYSMON_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("SYSMON_LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}

	return cfg
}

// --- runtime config management ---

var (
	currentCfg   Config
	currentCfgMu sync.RWMutex
)

// GetConfig returns a copy of the current runtime configuration
func GetConfig() Config {
	currentCfgMu.RLock()
	defer currentCfgMu.RUnlock()
	return currentCfg
}

// SetConfig replaces the current runtime configuration
func SetConfig(cfg Config) {
	currentCfgMu.Lock()
	defer currentCfgMu.Unlock()
	currentCfg = cfg
}

// RateLimiter tracks login attempts per IP
type RateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{attempts: make(map[string][]time.Time)}
	go rl.cleanupLoop()
	return rl
}

// cleanupLoop periodically removes expired IP entries from the attempts map
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-1 * time.Minute)
		for ip, times := range rl.attempts {
			var fresh []time.Time
			for _, t := range times {
				if t.After(cutoff) {
					fresh = append(fresh, t)
				}
			}
			if len(fresh) == 0 {
				delete(rl.attempts, ip)
			} else {
				rl.attempts[ip] = fresh
			}
		}
		rl.mu.Unlock()
	}
}

// Allow checks if the given IP is allowed to make a request (max 5 per minute)
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)
	times := rl.attempts[ip]
	var fresh []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) >= 5 {
		rl.attempts[ip] = fresh
		return false
	}
	rl.attempts[ip] = append(fresh, now)
	return true
}

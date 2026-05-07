package main

import (
	"crypto/subtle"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"sysmon/agent"
	"sysmon/monitor"
	"sysmon/storage"
	"sysmon/terminal"
	"sysmon/traffic"
	ws "sysmon/websocket"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//go:embed web
var webFS embed.FS

//go:embed web/login.html
var loginPage string

var configFilePath string

func main() {
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()
	configFilePath = *configPath

	cfg := LoadConfig(*configPath)
	SetupLogging(cfg.LogLevel, cfg.LogFormat)
	SetConfig(cfg)

	slog.Info("sysmon starting",
		"port", cfg.Port,
		"log_level", cfg.LogLevel,
		"log_format", cfg.LogFormat,
		"traffic", cfg.EnableTraffic,
		"agent_mode", cfg.AgentMode,
	)

	// Agent mode: start agent client and exit early
	if cfg.AgentMode == "agent" {
		if cfg.AgentMaster == "" || cfg.AgentAPIKey == "" {
			slog.Error("agent mode requires agent_master and agent_api_key")
			os.Exit(1)
		}

		interval := time.Duration(cfg.AgentInterval) * time.Second
		if interval <= 0 {
			interval = 60 * time.Second
		}

		agentClient := agent.NewAgentClient(cfg.AgentMaster, cfg.AgentAPIKey, interval)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Graceful shutdown
		go func() {
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			sig := <-sigCh
			slog.Info("received shutdown signal", "signal", sig)
			cancel()
		}()

		agentClient.Start(ctx)
		slog.Info("agent client stopped")
		return
	}

	InitAuthSecret()
	InitAllowedOrigins()

	monitor.SetHistoryCapacity(cfg.HistoryDuration)

	// Open shared database
	db, err := storage.Open(cfg.HistoryDB)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Initialize system history
	histStore, err := storage.NewHistoryStore(db, cfg.HistoryRetentionDays)
	if err != nil {
		slog.Error("failed to initialize history store", "error", err)
		os.Exit(1)
	}

	// Migrate from old JSON file if exists
	if cfg.HistoryFile != "" {
		histStore.MigrateFromJSON(cfg.HistoryFile)
	}
	histStore.MigrateFromJSON("history.json")

	// Load recent data into memory ring buffer
	capacity := cfg.HistoryDuration
	if capacity <= 0 {
		capacity = 3600
	}
	points, err := histStore.QueryRecent(capacity)
	if err != nil {
		slog.Warn("failed to load recent history from SQLite", "error", err)
	} else if len(points) > 0 {
		monitor.LoadHistory(points)
		slog.Info("loaded recent history from SQLite", "count", len(points))
	}

	// Run initial cleanup
	histStore.Cleanup()

	hub := ws.NewHub()

	upgrader := &websocket.Upgrader{
		CheckOrigin: CheckOrigin,
	}

	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		slog.Error("failed to access embedded web filesystem", "error", err)
		os.Exit(1)
	}

	rl := NewRateLimiter()

	// Agent server (master mode)
	var agentServer *agent.AgentServer
	if cfg.AgentMode == "master" {
		if len(cfg.AgentKeys) == 0 {
			slog.Warn("master mode enabled but no agent_keys configured")
		} else {
			agentServer = agent.NewAgentServer(cfg.AgentKeys)
			slog.Info("agent server initialized", "keys_count", len(cfg.AgentKeys))
		}
	}

	registerRoutes(cfg, hub, rl, webContent, upgrader, agentServer)

	// Background system monitoring broadcaster
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic in system monitoring broadcaster", "panic", r)
			}
		}()
		ticker := time.NewTicker(time.Duration(cfg.RefreshInterval) * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			c := GetConfig()
			snap := Collect(c.MaxProcesses)
			monitor.RecordHistory(snap.CPU.AvgUsage, snap.Memory.UsedPercent)

			UpdateMetricsFromSnapshot(snap)
			UpdateWSConnections(hub.Count())

			msg := WsMessage{Type: "snapshot", Payload: snap}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			hub.Broadcast(data)
		}
	}()

	// Docker stats, less frequent
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic in docker stats goroutine", "panic", r)
			}
		}()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			containers := monitor.GetDockerContainers()
			if containers == nil {
				continue
			}
			UpdateDockerMetric(len(containers))
			msg := WsMessage{Type: "docker", Payload: containers}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			hub.Broadcast(data)
		}
	}()

	// Periodic history persistence
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic in history persistence goroutine", "panic", r)
			}
		}()
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			pts := monitor.GetHistory()
			if len(pts) > 0 {
				latest := pts[len(pts)-1]
				if err := histStore.InsertPoint(latest); err != nil {
					slog.Error("history: failed to insert point", "error", err)
				}
			}
			histStore.Cleanup()
		}
	}()

	// Traffic monitoring (if enabled)
	var trafficStore *storage.TrafficStore
	var dnsCache *traffic.DNSCache
	var anomalyDetector *traffic.AnomalyDetector
	if cfg.EnableTraffic {
		trafficStore, err = storage.NewTrafficStore(db)
		if err != nil {
			slog.Error("failed to initialize traffic store", "error", err)
			os.Exit(1)
		}

		agg := traffic.NewAggregator()
		dnsCache = traffic.NewDNSCache(5 * time.Minute)
		anomalyDetector = traffic.NewAnomalyDetector(10, 100)

		// Get listening ports
		listenPorts, err := traffic.GetListeningPorts()
		if err != nil {
			slog.Warn("failed to get listening ports", "error", err)
			listenPorts = make(map[uint16]bool)
		}
		slog.Info("monitoring listening ports", "count", len(listenPorts))

		var portsMu sync.RWMutex

		// Start packet capture
		iface := cfg.TrafficIface
		if iface == "" {
			iface, err = traffic.GetDefaultInterface()
			if err != nil {
				slog.Error("failed to get default network interface", "error", err)
			}
		}
		if iface != "" {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("panic in packet capture goroutine", "panic", r)
					}
				}()
				slog.Info("starting traffic capture", "interface", iface)
				if err := traffic.StartPacketCapture(iface, agg, &listenPorts, &portsMu); err != nil {
					slog.Error("packet capture failed", "error", err)
				}
			}()
		}

		// Traffic snapshot persistence
		trafficInterval := cfg.TrafficInterval
		if trafficInterval <= 0 {
			trafficInterval = 60
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic in traffic snapshot persistence goroutine", "panic", r)
				}
			}()
			ticker := time.NewTicker(time.Duration(trafficInterval) * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				snapshots := agg.GetSnapshot()
				if len(snapshots) > 0 {
					if err := trafficStore.BatchInsert(snapshots); err != nil {
						slog.Error("traffic: failed to insert port snapshots", "error", err)
					}

					// Detect anomalies in port traffic
					anomalies := anomalyDetector.DetectPortAnomalies(snapshots)
					if len(anomalies) > 0 {
						slog.Warn("traffic anomalies detected", "count", len(anomalies))
						for _, a := range anomalies {
							slog.Warn("anomaly",
								"port", a.Port,
								"source", a.SourceIP,
								"direction", a.Direction,
								"rate", a.Value,
								"threshold", a.Threshold,
								"severity", a.Severity,
							)
						}
					}
				}

				hostSnapshots := agg.GetHostSnapshot()
				if len(hostSnapshots) > 0 {
					if err := trafficStore.BatchInsertHostSnapshots(hostSnapshots); err != nil {
						slog.Error("traffic: failed to insert host snapshots", "error", err)
					}

					// Detect anomalies in host traffic
					hostAnomalies := anomalyDetector.DetectHostAnomalies(hostSnapshots)
					if len(hostAnomalies) > 0 {
						slog.Warn("host traffic anomalies detected", "count", len(hostAnomalies))
						for _, a := range hostAnomalies {
							slog.Warn("host anomaly",
								"host", a.SourceIP,
								"direction", a.Direction,
								"rate", a.Value,
								"threshold", a.Threshold,
								"severity", a.Severity,
							)
						}
					}
				}

				if newPorts, err := traffic.GetListeningPorts(); err == nil {
					portsMu.Lock()
					listenPorts = newPorts
					portsMu.Unlock()
				}
			}
		}()

		// Downsample and cleanup hourly
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic in traffic cleanup goroutine", "panic", r)
				}
			}()
			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()
			if err := trafficStore.DownsampleAndCleanup(); err != nil {
				slog.Error("traffic: initial cleanup failed", "error", err)
			}
			for range ticker.C {
				if err := trafficStore.DownsampleAndCleanup(); err != nil {
					slog.Error("traffic: downsampling failed", "error", err)
				}
			}
		}()

		registerTrafficRoutes(agg, trafficStore, dnsCache, anomalyDetector)
	}

	// SIGHUP config hot-reload
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic in SIGHUP handler goroutine", "panic", r)
			}
		}()
		sigHup := make(chan os.Signal, 1)
		signal.Notify(sigHup, syscall.SIGHUP)
		for range sigHup {
			reloadConfig()
		}
	}()

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: nil,
	}

	// Graceful shutdown
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic in shutdown handler goroutine", "panic", r)
			}
		}()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("received shutdown signal, shutting down...", "signal", sig)

		pts := monitor.GetHistory()
		if len(pts) > 0 {
			latest := pts[len(pts)-1]
			histStore.InsertPoint(latest)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}()

	slog.Info("sysmon listening", "address", "http://0.0.0.0"+addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}

func reloadConfig() {
	slog.Info("SIGHUP received, reloading configuration", "path", configFilePath)
	newCfg := LoadConfig(configFilePath)
	SetupLogging(newCfg.LogLevel, newCfg.LogFormat)
	SetConfig(newCfg)
	slog.Info("configuration reloaded successfully",
		"log_level", newCfg.LogLevel,
		"log_format", newCfg.LogFormat,
		"port", newCfg.Port,
	)
}

func registerRoutes(cfg Config, hub *ws.Hub, rl *RateLimiter, webContent fs.FS, upgrader *websocket.Upgrader, agentServer *agent.AgentServer) {
	fileServer := http.FileServer(http.FS(webContent))

	// Login handler
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		c := GetConfig()
		if r.Method == http.MethodPost {
			ip := r.RemoteAddr
			if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
				ip = strings.Split(fwd, ",")[0]
			}
			if !rl.Allow(ip) {
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			var req struct {
				Password string `json:"password"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", 400)
				return
			}
			if subtle.ConstantTimeCompare([]byte(req.Password), []byte(c.Password)) != 1 {
				slog.Warn("login failed: wrong password", "ip", ip)
				http.Error(w, "unauthorized", 401)
				return
			}
			token := GenerateToken(c.Password)
			http.SetCookie(w, &http.Cookie{
				Name:     "sysmon_token",
				Value:    token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				MaxAge:   86400,
			})
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"token": token})
			slog.Info("login successful", "ip", ip)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, loginPage)
	})

	// Static files with auth
	http.HandleFunc("/", AuthRequired(cfg.Password, func(w http.ResponseWriter, r *http.Request) {
		fileServer.ServeHTTP(w, r)
	}))

	// WebSocket endpoint
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c := GetConfig()
		if !IsAuthenticated(r, c.Password) {
			http.Error(w, "unauthorized", 401)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("websocket upgrade error", "error", err, "remote", r.RemoteAddr)
			return
		}
		n := hub.Add(conn)
		UpdateWSConnections(n)

		snap := Collect(c.MaxProcesses)
		initMsg := WsMessage{Type: "snapshot", Payload: snap}
		data, _ := json.Marshal(initMsg)
		hub.WriteTo(conn, data)

		history := monitor.GetHistory()
		if len(history) > 0 {
			histMsg := WsMessage{Type: "history", Payload: history}
			data, _ := json.Marshal(histMsg)
			hub.WriteTo(conn, data)
		}

		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic in websocket read goroutine", "panic", r)
				}
			}()
			defer func() {
				n := hub.Remove(conn)
				UpdateWSConnections(n)
			}()
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					break
				}
			}
		}()
	})

	// Shell WebSocket endpoint
	http.HandleFunc("/ws/shell", terminal.HandleShell(
		cfg.ShellEnabled(),
		cfg.Password,
		cfg.ShellPassword,
		IsAuthenticated,
		ValidateShellToken,
		upgrader,
	))

	// Health endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// Prometheus metrics
	http.Handle("/metrics", promhttp.Handler())

	// Shell status API
	http.HandleFunc("/api/shell-status", AuthRequired(cfg.Password, func(w http.ResponseWriter, r *http.Request) {
		c := GetConfig()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"enabled": c.ShellEnabled()})
	}))

	// Shell auth API
	http.HandleFunc("/api/shell-auth", AuthRequired(cfg.Password, func(w http.ResponseWriter, r *http.Request) {
		c := GetConfig()
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !c.ShellEnabled() {
			http.Error(w, "shell disabled", http.StatusForbidden)
			return
		}
		var req struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if subtle.ConstantTimeCompare([]byte(req.Password), []byte(c.ShellPassword)) != 1 {
			slog.Warn("shell auth failed: wrong password", "remote", r.RemoteAddr)
			http.Error(w, "wrong password", http.StatusUnauthorized)
			return
		}
		token := GenerateShellToken(c.ShellPassword)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"shell_token": token})
	}))

	// History query API
	http.HandleFunc("/api/history", AuthRequired(cfg.Password, func(w http.ResponseWriter, r *http.Request) {
		history := monitor.GetHistory()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(history)
	}))

	// Agent endpoints (master mode)
	if agentServer != nil {
		http.HandleFunc("/api/agent/report", agentServer.HandleReport)
		http.HandleFunc("/api/agents", AuthRequired(cfg.Password, agentServer.HandleList))
		slog.Info("agent endpoints registered")
	}
}

func registerTrafficRoutes(agg *traffic.Aggregator, store *storage.TrafficStore, dnsCache *traffic.DNSCache, anomalyDetector *traffic.AnomalyDetector) {
	cfg := GetConfig()
	// Active ports
	http.HandleFunc("/api/traffic/ports", AuthRequired(cfg.Password, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		stats := agg.GetRealTimeStats()
		result := make([]map[string]interface{}, 0, len(stats))
		for _, portData := range stats {
			result = append(result, portData)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data":    result,
		})
	}))

	// Port stats (historical)
	http.HandleFunc("/api/traffic/port-stats", AuthRequired(cfg.Password, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		portStr := r.URL.Query().Get("port")
		rangeStr := r.URL.Query().Get("range")

		if portStr == "" || rangeStr == "" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "missing port or range parameter",
			})
			return
		}

		var port uint16
		if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "invalid port number",
			})
			return
		}

		data, err := store.QueryStats(port, rangeStr)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
			return
		}

		var totalBytes, totalPackets uint64
		var peakRate float64
		for _, record := range data {
			if bytes, ok := record["bytes"].(uint64); ok {
				totalBytes += bytes
			}
			if packets, ok := record["packets"].(uint64); ok {
				totalPackets += packets
			}
			if rate, ok := record["peak_rate"].(float64); ok && rate > peakRate {
				peakRate = rate
			}
		}

		avgRate := float64(0)
		if len(data) > 0 {
			avgRate = float64(totalBytes) / float64(len(data))
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"port":          port,
				"range":         rangeStr,
				"total_bytes":   totalBytes,
				"total_packets": totalPackets,
				"peak_rate":     peakRate,
				"average_rate":  avgRate,
				"timeseries":    data,
			},
		})
	}))

	// Active hosts
	http.HandleFunc("/api/traffic/hosts", AuthRequired(cfg.Password, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		stats := agg.GetRealTimeHostStats()
		result := make([]map[string]interface{}, 0, len(stats))
		for _, hostData := range stats {
			result = append(result, hostData)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data":    result,
		})
	}))

	// Host stats (historical)
	http.HandleFunc("/api/traffic/host-stats", AuthRequired(cfg.Password, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		hostIP := r.URL.Query().Get("host_ip")
		rangeStr := r.URL.Query().Get("range")

		if hostIP == "" || rangeStr == "" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "missing host_ip or range parameter",
			})
			return
		}

		data, err := store.QueryHostStats(hostIP, rangeStr)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
			return
		}

		var totalBytes, totalPackets uint64
		var peakRate float64
		for _, record := range data {
			if bytes, ok := record["bytes"].(uint64); ok {
				totalBytes += bytes
			}
			if packets, ok := record["packets"].(uint64); ok {
				totalPackets += packets
			}
			if rate, ok := record["peak_rate"].(float64); ok && rate > peakRate {
				peakRate = rate
			}
		}

		avgRate := float64(0)
		if len(data) > 0 {
			avgRate = float64(totalBytes) / float64(len(data))
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"host_ip":       hostIP,
				"range":         rangeStr,
				"total_bytes":   totalBytes,
				"total_packets": totalPackets,
				"peak_rate":     peakRate,
				"average_rate":  avgRate,
				"timeseries":    data,
			},
		})
	}))

	// DNS resolve endpoint
	http.HandleFunc("/api/traffic/resolve", AuthRequired(cfg.Password, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		ip := r.URL.Query().Get("ip")
		if ip == "" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "missing ip parameter",
			})
			return
		}

		hostname, err := dnsCache.Resolve(ip)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"ip":       ip,
			"hostname": hostname,
		})
	}))

	// Anomalies endpoint
	http.HandleFunc("/api/traffic/anomalies", AuthRequired(cfg.Password, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		limitStr := r.URL.Query().Get("limit")
		limit := 50
		if limitStr != "" {
			if _, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil {
				limit = 50
			}
		}

		anomalies := anomalyDetector.GetRecentAnomalies(limit)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data":    anomalies,
		})
	}))
}

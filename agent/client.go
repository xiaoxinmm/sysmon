package agent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"sysmon/monitor"
)

// Snapshot represents the system metrics collected by the agent
type Snapshot struct {
	System    monitor.SystemInfo   `json:"system"`
	CPU       monitor.CPUInfo      `json:"cpu"`
	Memory    monitor.MemInfo      `json:"memory"`
	Disks     []monitor.DiskInfo   `json:"disks"`
	Network   []monitor.NetInfo    `json:"network"`
	Load      monitor.LoadInfo     `json:"load"`
	Processes []monitor.ProcessInfo `json:"processes"`
	Docker    []monitor.DockerContainer `json:"docker,omitempty"`
}

// AgentClient periodically collects system metrics and reports to master
type AgentClient struct {
	masterURL string
	apiKey    string
	hostname  string
	interval  time.Duration
	client    *http.Client
}

// NewAgentClient creates a new agent client
func NewAgentClient(masterURL, apiKey string, interval time.Duration) *AgentClient {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	return &AgentClient{
		masterURL: masterURL,
		apiKey:    apiKey,
		hostname:  hostname,
		interval:  interval,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Start begins the periodic reporting loop
func (c *AgentClient) Start(ctx context.Context) {
	slog.Info("agent client starting", "master", c.masterURL, "hostname", c.hostname, "interval", c.interval)

	// Initial report
	if err := c.report(); err != nil {
		slog.Error("agent: initial report failed", "error", err)
	}

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("agent client stopping")
			return
		case <-ticker.C:
			if err := c.report(); err != nil {
				slog.Error("agent: report failed", "error", err)
			}
		}
	}
}

// report collects system metrics and sends to master
func (c *AgentClient) report() error {
	// Collect system metrics
	snap := Snapshot{
		System:    monitor.GetSystemInfo(),
		CPU:       monitor.GetCPUInfo(),
		Memory:    monitor.GetMemInfo(),
		Disks:     monitor.GetDiskInfo(),
		Network:   monitor.GetNetInfo(),
		Load:      monitor.GetLoadInfo(),
		Processes: monitor.GetProcesses(10), // Top 10 processes
		Docker:    monitor.GetDockerContainers(),
	}

	// Marshal to JSON
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Create signature
	signature := c.sign(data)

	// Create request
	url := c.masterURL + "/api/agent/report"
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Hostname", c.hostname)
	req.Header.Set("X-Agent-Signature", signature)

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("master returned status %d", resp.StatusCode)
	}

	slog.Debug("agent: report sent successfully", "hostname", c.hostname)
	return nil
}

// sign creates HMAC-SHA256 signature of the data
func (c *AgentClient) sign(data []byte) string {
	h := hmac.New(sha256.New, []byte(c.apiKey))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

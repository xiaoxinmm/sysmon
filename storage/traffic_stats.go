package storage

import (
	"fmt"
	"log/slog"
	"time"

	"sysmon/traffic"
)

// TrafficStore manages traffic statistics in SQLite
type TrafficStore struct {
	db *DB
}

// NewTrafficStore creates a new TrafficStore and initializes tables
func NewTrafficStore(db *DB) (*TrafficStore, error) {
	store := &TrafficStore{db: db}

	if err := store.init(); err != nil {
		return nil, fmt.Errorf("failed to initialize traffic tables: %w", err)
	}

	return store, nil
}

func (s *TrafficStore) init() error {
	schema := `
	CREATE TABLE IF NOT EXISTS traffic_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		port INTEGER NOT NULL,
		source_ip TEXT NOT NULL,
		direction TEXT NOT NULL,
		bytes INTEGER NOT NULL,
		packets INTEGER NOT NULL,
		peak_rate REAL NOT NULL,
		granularity INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_traffic_query
		ON traffic_stats(port, timestamp, granularity);

	CREATE INDEX IF NOT EXISTS idx_traffic_cleanup
		ON traffic_stats(granularity, timestamp);

	CREATE TABLE IF NOT EXISTS host_traffic_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		host_ip TEXT NOT NULL,
		remote_ip TEXT NOT NULL,
		direction TEXT NOT NULL,
		bytes INTEGER NOT NULL,
		packets INTEGER NOT NULL,
		peak_rate REAL NOT NULL,
		granularity INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_host_traffic_query
		ON host_traffic_stats(host_ip, timestamp, granularity);

	CREATE INDEX IF NOT EXISTS idx_host_traffic_cleanup
		ON host_traffic_stats(granularity, timestamp);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	slog.Info("traffic: SQLite tables initialized")
	return nil
}

// BatchInsert inserts port traffic snapshots
func (s *TrafficStore) BatchInsert(snapshots []traffic.TrafficSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO traffic_stats
		(timestamp, port, source_ip, direction, bytes, packets, peak_rate, granularity, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, snap := range snapshots {
		_, err := stmt.Exec(
			snap.Timestamp.Unix(),
			snap.Port,
			snap.SourceIP,
			snap.Direction,
			snap.Bytes,
			snap.Packets,
			snap.Rate,
			now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// BatchInsertHostSnapshots inserts host traffic snapshots
func (s *TrafficStore) BatchInsertHostSnapshots(snapshots []traffic.HostTrafficSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO host_traffic_stats
		(timestamp, host_ip, remote_ip, direction, bytes, packets, peak_rate, granularity, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, snap := range snapshots {
		_, err := stmt.Exec(
			snap.Timestamp.Unix(),
			snap.HostIP,
			snap.RemoteIP,
			snap.Direction,
			snap.Bytes,
			snap.Packets,
			snap.Rate,
			now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// QueryStats queries port traffic statistics for a given range
func (s *TrafficStore) QueryStats(port uint16, rangeStr string) ([]map[string]interface{}, error) {
	duration, granularity, err := parseRange(rangeStr)
	if err != nil {
		return nil, err
	}

	startTime := time.Now().Add(-duration).Unix()

	rows, err := s.db.Query(`
		SELECT
			timestamp,
			source_ip,
			direction,
			SUM(bytes) as total_bytes,
			SUM(packets) as total_packets,
			MAX(peak_rate) as max_rate
		FROM traffic_stats
		WHERE port = ? AND timestamp >= ? AND granularity = ?
		GROUP BY timestamp, source_ip, direction
		ORDER BY timestamp ASC
	`, port, startTime, granularity)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]map[string]interface{}, 0)
	for rows.Next() {
		var timestamp int64
		var sourceIP, direction string
		var bytes, packets uint64
		var maxRate float64

		if err := rows.Scan(&timestamp, &sourceIP, &direction, &bytes, &packets, &maxRate); err != nil {
			return nil, err
		}

		results = append(results, map[string]interface{}{
			"timestamp": timestamp,
			"time":      time.Unix(timestamp, 0).Format("2006-01-02 15:04:05"),
			"source_ip": sourceIP,
			"direction": direction,
			"bytes":     bytes,
			"packets":   packets,
			"peak_rate": maxRate,
		})
	}

	return results, nil
}

// QueryHostStats queries host traffic statistics for a given range
func (s *TrafficStore) QueryHostStats(hostIP string, rangeStr string) ([]map[string]interface{}, error) {
	duration, granularity, err := parseRange(rangeStr)
	if err != nil {
		return nil, err
	}

	startTime := time.Now().Add(-duration).Unix()

	rows, err := s.db.Query(`
		SELECT
			timestamp,
			remote_ip,
			direction,
			SUM(bytes) as total_bytes,
			SUM(packets) as total_packets,
			MAX(peak_rate) as max_rate
		FROM host_traffic_stats
		WHERE host_ip = ? AND timestamp >= ? AND granularity = ?
		GROUP BY timestamp, remote_ip, direction
		ORDER BY timestamp ASC
	`, hostIP, startTime, granularity)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]map[string]interface{}, 0)
	for rows.Next() {
		var timestamp int64
		var remoteIP, direction string
		var bytes, packets uint64
		var maxRate float64

		if err := rows.Scan(&timestamp, &remoteIP, &direction, &bytes, &packets, &maxRate); err != nil {
			return nil, err
		}

		results = append(results, map[string]interface{}{
			"timestamp": timestamp,
			"time":      time.Unix(timestamp, 0).Format("2006-01-02 15:04:05"),
			"remote_ip": remoteIP,
			"direction": direction,
			"bytes":     bytes,
			"packets":   packets,
			"peak_rate": maxRate,
		})
	}

	return results, nil
}

// parseRange converts a range string to duration and granularity
func parseRange(rangeStr string) (time.Duration, int, error) {
	switch rangeStr {
	case "15m":
		return 15 * time.Minute, 0, nil
	case "30m":
		return 30 * time.Minute, 0, nil
	case "60m", "1h":
		return 60 * time.Minute, 0, nil
	case "1d":
		return 24 * time.Hour, 1, nil
	case "3d":
		return 3 * 24 * time.Hour, 1, nil
	case "7d":
		return 7 * 24 * time.Hour, 1, nil
	case "30d":
		return 30 * 24 * time.Hour, 2, nil
	default:
		return 0, 0, fmt.Errorf("invalid range: %s", rangeStr)
	}
}

package storage

import (
	"fmt"
	"log/slog"
	"time"
)

// DownsampleAndCleanup performs traffic data downsampling and cleanup
func (s *TrafficStore) DownsampleAndCleanup() error {
	now := time.Now()

	// 1. Granularity 0 -> 1: Aggregate minute data to hourly for data older than 6 hours
	sixHoursAgo := now.Add(-6 * time.Hour).Unix()

	slog.Debug("downsampling: granularity 0 -> 1")

	_, err := s.db.Exec(`
		INSERT INTO traffic_stats
		(timestamp, port, source_ip, direction, bytes, packets, peak_rate, granularity, created_at)
		SELECT
			(timestamp / 3600) * 3600 as hour_timestamp,
			port,
			source_ip,
			direction,
			SUM(bytes) as total_bytes,
			SUM(packets) as total_packets,
			MAX(peak_rate) as max_peak_rate,
			1 as granularity,
			? as created_at
		FROM traffic_stats
		WHERE granularity = 0 AND timestamp < ?
		GROUP BY hour_timestamp, port, source_ip, direction
	`, now.Unix(), sixHoursAgo)

	if err != nil {
		return fmt.Errorf("failed to downsample to granularity 1: %v", err)
	}

	result, err := s.db.Exec(`
		DELETE FROM traffic_stats
		WHERE granularity = 0 AND timestamp < ?
	`, sixHoursAgo)
	if err != nil {
		return fmt.Errorf("failed to delete old granularity 0 data: %v", err)
	}
	deleted, _ := result.RowsAffected()
	slog.Debug("deleted granularity 0 records", "count", deleted)

	// 2. Granularity 1 -> 2: Aggregate hourly data to daily for data older than 7 days
	sevenDaysAgo := now.Add(-7 * 24 * time.Hour).Unix()

	slog.Debug("downsampling: granularity 1 -> 2")

	_, err = s.db.Exec(`
		INSERT INTO traffic_stats
		(timestamp, port, source_ip, direction, bytes, packets, peak_rate, granularity, created_at)
		SELECT
			(timestamp / 86400) * 86400 as day_timestamp,
			port,
			source_ip,
			direction,
			SUM(bytes) as total_bytes,
			SUM(packets) as total_packets,
			MAX(peak_rate) as max_peak_rate,
			2 as granularity,
			? as created_at
		FROM traffic_stats
		WHERE granularity = 1 AND timestamp < ?
		GROUP BY day_timestamp, port, source_ip, direction
	`, now.Unix(), sevenDaysAgo)

	if err != nil {
		return fmt.Errorf("failed to downsample to granularity 2: %v", err)
	}

	result, err = s.db.Exec(`
		DELETE FROM traffic_stats
		WHERE granularity = 1 AND timestamp < ?
	`, sevenDaysAgo)
	if err != nil {
		return fmt.Errorf("failed to delete old granularity 1 data: %v", err)
	}
	deleted, _ = result.RowsAffected()
	slog.Debug("deleted granularity 1 records", "count", deleted)

	// 3. Delete granularity 2 data older than 30 days
	thirtyDaysAgo := now.Add(-30 * 24 * time.Hour).Unix()

	result, err = s.db.Exec(`
		DELETE FROM traffic_stats
		WHERE granularity = 2 AND timestamp < ?
	`, thirtyDaysAgo)
	if err != nil {
		return fmt.Errorf("failed to delete old granularity 2 data: %v", err)
	}
	deleted, _ = result.RowsAffected()
	slog.Debug("deleted granularity 2 records", "count", deleted)

	// 4. Host traffic downsampling
	slog.Debug("downsampling: host traffic granularity 0 -> 1")

	_, err = s.db.Exec(`
		INSERT INTO host_traffic_stats
		(timestamp, host_ip, remote_ip, direction, bytes, packets, peak_rate, granularity, created_at)
		SELECT
			(timestamp / 3600) * 3600 as hour_timestamp,
			host_ip,
			remote_ip,
			direction,
			SUM(bytes) as total_bytes,
			SUM(packets) as total_packets,
			MAX(peak_rate) as max_peak_rate,
			1 as granularity,
			? as created_at
		FROM host_traffic_stats
		WHERE granularity = 0 AND timestamp < ?
		GROUP BY hour_timestamp, host_ip, remote_ip, direction
	`, now.Unix(), sixHoursAgo)

	if err != nil {
		return fmt.Errorf("failed to downsample host traffic to granularity 1: %v", err)
	}

	result, err = s.db.Exec(`
		DELETE FROM host_traffic_stats
		WHERE granularity = 0 AND timestamp < ?
	`, sixHoursAgo)
	if err != nil {
		return fmt.Errorf("failed to delete old host granularity 0 data: %v", err)
	}
	deleted, _ = result.RowsAffected()
	slog.Debug("deleted host granularity 0 records", "count", deleted)

	slog.Debug("downsampling: host traffic granularity 1 -> 2")

	_, err = s.db.Exec(`
		INSERT INTO host_traffic_stats
		(timestamp, host_ip, remote_ip, direction, bytes, packets, peak_rate, granularity, created_at)
		SELECT
			(timestamp / 86400) * 86400 as day_timestamp,
			host_ip,
			remote_ip,
			direction,
			SUM(bytes) as total_bytes,
			SUM(packets) as total_packets,
			MAX(peak_rate) as max_peak_rate,
			2 as granularity,
			? as created_at
		FROM host_traffic_stats
		WHERE granularity = 1 AND timestamp < ?
		GROUP BY day_timestamp, host_ip, remote_ip, direction
	`, now.Unix(), sevenDaysAgo)

	if err != nil {
		return fmt.Errorf("failed to downsample host traffic to granularity 2: %v", err)
	}

	result, err = s.db.Exec(`
		DELETE FROM host_traffic_stats
		WHERE granularity = 1 AND timestamp < ?
	`, sevenDaysAgo)
	if err != nil {
		return fmt.Errorf("failed to delete old host granularity 1 data: %v", err)
	}
	deleted, _ = result.RowsAffected()
	slog.Debug("deleted host granularity 1 records", "count", deleted)

	result, err = s.db.Exec(`
		DELETE FROM host_traffic_stats
		WHERE granularity = 2 AND timestamp < ?
	`, thirtyDaysAgo)
	if err != nil {
		return fmt.Errorf("failed to delete old host granularity 2 data: %v", err)
	}
	deleted, _ = result.RowsAffected()
	slog.Debug("deleted host granularity 2 records", "count", deleted)

	// 5. Optimize
	if _, err := s.db.Exec("PRAGMA optimize;"); err != nil {
		slog.Warn("failed to optimize database", "error", err)
	}

	slog.Debug("downsampling and cleanup completed")
	return nil
}

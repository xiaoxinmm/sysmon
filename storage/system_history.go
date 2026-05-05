package storage

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sysmon/monitor"
)

// HistoryStore manages SQLite-backed system history persistence
type HistoryStore struct {
	db            *DB
	retentionDays int
}

// NewHistoryStore creates a new HistoryStore
func NewHistoryStore(db *DB, retentionDays int) (*HistoryStore, error) {
	store := &HistoryStore{
		db:            db,
		retentionDays: retentionDays,
	}

	if err := store.init(); err != nil {
		return nil, fmt.Errorf("failed to initialize history table: %w", err)
	}

	return store, nil
}

func (s *HistoryStore) init() error {
	createTable := `
	CREATE TABLE IF NOT EXISTS history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		cpu_avg REAL,
		memory_percent REAL,
		data JSON
	);`

	createIndex := `
	CREATE INDEX IF NOT EXISTS idx_timestamp ON history(timestamp);`

	if _, err := s.db.Exec(createTable); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	if _, err := s.db.Exec(createIndex); err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	slog.Info("history: SQLite table initialized", "retention_days", s.retentionDays)
	return nil
}

// InsertPoint inserts a single history point
func (s *HistoryStore) InsertPoint(point monitor.HistoryPoint) error {
	dataJSON, err := json.Marshal(point)
	if err != nil {
		return fmt.Errorf("failed to marshal point: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO history (timestamp, cpu_avg, memory_percent, data) VALUES (?, ?, ?, ?)`,
		time.Unix(point.Timestamp, 0).UTC(),
		point.CPUAvg,
		point.MemPercent,
		string(dataJSON),
	)
	return err
}

// InsertPoints inserts multiple history points in a transaction
func (s *HistoryStore) InsertPoints(points []monitor.HistoryPoint) error {
	if len(points) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO history (timestamp, cpu_avg, memory_percent, data) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, point := range points {
		dataJSON, err := json.Marshal(point)
		if err != nil {
			return fmt.Errorf("failed to marshal point: %w", err)
		}

		_, err = stmt.Exec(
			time.Unix(point.Timestamp, 0).UTC(),
			point.CPUAvg,
			point.MemPercent,
			string(dataJSON),
		)
		if err != nil {
			return fmt.Errorf("failed to insert point: %w", err)
		}
	}

	return tx.Commit()
}

// QueryRange returns history points within the given time range
func (s *HistoryStore) QueryRange(from, to time.Time) ([]monitor.HistoryPoint, error) {
	rows, err := s.db.Query(
		`SELECT data FROM history WHERE timestamp >= ? AND timestamp <= ? ORDER BY timestamp ASC`,
		from.UTC(),
		to.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query history: %w", err)
	}
	defer rows.Close()

	var points []monitor.HistoryPoint
	for rows.Next() {
		var dataJSON string
		if err := rows.Scan(&dataJSON); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		var point monitor.HistoryPoint
		if err := json.Unmarshal([]byte(dataJSON), &point); err != nil {
			slog.Warn("history: failed to unmarshal point, skipping", "error", err)
			continue
		}
		points = append(points, point)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return points, nil
}

// QueryRecent returns the most recent N history points
func (s *HistoryStore) QueryRecent(limit int) ([]monitor.HistoryPoint, error) {
	rows, err := s.db.Query(
		`SELECT data FROM history ORDER BY timestamp DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent history: %w", err)
	}
	defer rows.Close()

	var points []monitor.HistoryPoint
	for rows.Next() {
		var dataJSON string
		if err := rows.Scan(&dataJSON); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		var point monitor.HistoryPoint
		if err := json.Unmarshal([]byte(dataJSON), &point); err != nil {
			slog.Warn("history: failed to unmarshal point, skipping", "error", err)
			continue
		}
		points = append(points, point)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	// Reverse to chronological order
	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}

	return points, nil
}

// Cleanup removes history data older than the retention period
func (s *HistoryStore) Cleanup() error {
	if s.retentionDays <= 0 {
		return nil
	}

	cutoff := time.Now().AddDate(0, 0, -s.retentionDays).UTC()
	result, err := s.db.Exec(`DELETE FROM history WHERE timestamp < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("failed to cleanup old history: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		slog.Info("history: cleaned up old data", "rows_deleted", rowsAffected, "cutoff", cutoff)
	}

	return nil
}

// Count returns the total number of history records
func (s *HistoryStore) Count() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM history`).Scan(&count)
	return count, err
}

// MigrateFromJSON migrates data from an old JSON history file to SQLite
func (s *HistoryStore) MigrateFromJSON(jsonPath string) error {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read JSON file: %w", err)
	}

	var points []monitor.HistoryPoint
	if err := json.Unmarshal(data, &points); err != nil {
		return fmt.Errorf("failed to parse JSON file: %w", err)
	}

	if len(points) == 0 {
		slog.Info("history: JSON file is empty, nothing to migrate")
		return nil
	}

	count, err := s.Count()
	if err != nil {
		return fmt.Errorf("failed to check database count: %w", err)
	}

	if count > 0 {
		slog.Info("history: SQLite already has data, skipping JSON migration",
			"existing_records", count, "json_points", len(points))
		return nil
	}

	if err := s.InsertPoints(points); err != nil {
		return fmt.Errorf("failed to insert migrated points: %w", err)
	}

	slog.Info("history: migrated data from JSON to SQLite",
		"points", len(points), "source", jsonPath)

	backupPath := jsonPath + ".bak"
	if err := os.Rename(jsonPath, backupPath); err != nil {
		slog.Warn("history: failed to rename old JSON file", "from", jsonPath, "to", backupPath, "error", err)
	} else {
		slog.Info("history: old JSON file renamed to backup", "backup", backupPath)
	}

	return nil
}

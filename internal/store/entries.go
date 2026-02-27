package store

import (
	"database/sql"
	"fmt"
	"time"
)

type Entry struct {
	ID          int
	ClockifyID  string
	ProjectID   string
	ProjectName string
	Description string
	StartTime   time.Time
	EndTime     time.Time
	Minutes     int
	Status      string
	RawInput    string
	CreatedAt   time.Time
}

func (db *DB) InsertEntry(e *Entry) (int64, error) {
	result, err := db.Exec(
		`INSERT INTO entries (clockify_id, project_id, project_name, description, start_time, end_time, minutes, status, raw_input)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ClockifyID, e.ProjectID, e.ProjectName, e.Description,
		e.StartTime.UTC().Format(time.RFC3339),
		e.EndTime.UTC().Format(time.RFC3339),
		e.Minutes, e.Status, e.RawInput,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting entry: %w", err)
	}
	return result.LastInsertId()
}

func (db *DB) UpdateEntryStatus(id int, status, clockifyID string) error {
	_, err := db.Exec(
		"UPDATE entries SET status = ?, clockify_id = ? WHERE id = ?",
		status, clockifyID, id,
	)
	return err
}

func (db *DB) GetTodayEntries() ([]Entry, error) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)

	return db.queryEntries(
		`SELECT id, clockify_id, project_id, project_name, description, start_time, end_time, minutes, status, raw_input, created_at
		 FROM entries
		 WHERE start_time >= ? AND start_time < ?
		 ORDER BY start_time ASC`,
		startOfDay.UTC().Format(time.RFC3339),
		endOfDay.UTC().Format(time.RFC3339),
	)
}

func (db *DB) GetLastEntry() (*Entry, error) {
	entries, err := db.queryEntries(
		`SELECT id, clockify_id, project_id, project_name, description, start_time, end_time, minutes, status, raw_input, created_at
		 FROM entries
		 WHERE status = 'logged'
		 ORDER BY created_at DESC
		 LIMIT 1`,
	)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return &entries[0], nil
}

func (db *DB) GetFailedEntries() ([]Entry, error) {
	return db.queryEntries(
		`SELECT id, clockify_id, project_id, project_name, description, start_time, end_time, minutes, status, raw_input, created_at
		 FROM entries
		 WHERE status = 'failed'
		 ORDER BY created_at ASC`,
	)
}

func (db *DB) queryEntries(query string, args ...interface{}) ([]Entry, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying entries: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var clockifyID, rawInput sql.NullString
		var startStr, endStr, createdStr string

		if err := rows.Scan(
			&e.ID, &clockifyID, &e.ProjectID, &e.ProjectName, &e.Description,
			&startStr, &endStr, &e.Minutes, &e.Status, &rawInput, &createdStr,
		); err != nil {
			return nil, fmt.Errorf("scanning entry: %w", err)
		}

		e.ClockifyID = clockifyID.String
		e.RawInput = rawInput.String

		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			e.StartTime = t
		}
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			e.EndTime = t
		}
		if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
			e.CreatedAt = t
		}

		entries = append(entries, e)
	}

	return entries, rows.Err()
}

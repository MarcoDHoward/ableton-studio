// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// abletonTables are the local tables the Ableton commands own: the change
// journal, note backups, saved mixer states, project snapshots and saved
// device chains. Created lazily so a read-only command never migrates.
var abletonTables = []string{
	`CREATE TABLE IF NOT EXISTS journal (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts TEXT NOT NULL,
		command TEXT NOT NULL,
		method TEXT NOT NULL,
		path TEXT NOT NULL,
		target TEXT NOT NULL,
		property TEXT NOT NULL,
		before_json TEXT,
		after_json TEXT,
		reverted_by INTEGER
	)`,
	`CREATE INDEX IF NOT EXISTS idx_journal_ts ON journal(ts)`,
	`CREATE TABLE IF NOT EXISTS note_backups (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts TEXT NOT NULL,
		track_name TEXT,
		track_index INTEGER,
		slot INTEGER,
		clip_name TEXT,
		reason TEXT,
		notes_json TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS mix_states (
		name TEXT NOT NULL,
		track_name TEXT NOT NULL,
		state_json TEXT NOT NULL,
		saved_at TEXT NOT NULL,
		PRIMARY KEY(name, track_name)
	)`,
	`CREATE TABLE IF NOT EXISTS snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		label TEXT NOT NULL,
		ts TEXT NOT NULL,
		snapshot_json TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS chains (
		name TEXT PRIMARY KEY,
		saved_at TEXT NOT NULL,
		source_track TEXT,
		chain_json TEXT NOT NULL
	)`,
}

// EnsureAbletonTables creates the Ableton tables if they do not exist.
func EnsureAbletonTables(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("nil database")
	}
	for _, stmt := range abletonTables {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("creating Ableton tables: %w", err)
		}
	}
	return nil
}

// JournalRow is one journal entry as stored.
type JournalRow struct {
	ID         int64           `json:"id"`
	TS         string          `json:"ts"`
	Command    string          `json:"command"`
	Method     string          `json:"method"`
	Path       string          `json:"path"`
	Target     string          `json:"target"`
	Property   string          `json:"property"`
	Before     json.RawMessage `json:"before"`
	After      json.RawMessage `json:"after"`
	RevertedBy *int64          `json:"reverted_by"`
}

// InsertJournal appends one journal row (before/after are marshalled to
// JSON) and returns its id. It ensures the tables exist first.
func InsertJournal(ctx context.Context, db *sql.DB, ts time.Time, command, method, path, target, property string, before, after any) (int64, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return 0, err
	}
	b, err := json.Marshal(before)
	if err != nil {
		return 0, fmt.Errorf("encoding before value: %w", err)
	}
	a, err := json.Marshal(after)
	if err != nil {
		return 0, fmt.Errorf("encoding after value: %w", err)
	}
	res, err := db.ExecContext(ctx,
		`INSERT INTO journal (ts, command, method, path, target, property, before_json, after_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ts.UTC().Format(time.RFC3339Nano), command, method, path, target, property, string(b), string(a))
	if err != nil {
		return 0, fmt.Errorf("writing journal: %w", err)
	}
	return res.LastInsertId()
}

// RecentJournal returns the newest journal rows first.
func RecentJournal(ctx context.Context, db *sql.DB, limit int) ([]JournalRow, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `SELECT id, ts, command, method, path, target, property, before_json, after_json, reverted_by FROM journal ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JournalRow
	for rows.Next() {
		var r JournalRow
		var before, after sql.NullString
		var rev sql.NullInt64
		if err := rows.Scan(&r.ID, &r.TS, &r.Command, &r.Method, &r.Path, &r.Target, &r.Property, &before, &after, &rev); err != nil {
			return nil, err
		}
		if before.Valid {
			r.Before = json.RawMessage(before.String)
		}
		if after.Valid {
			r.After = json.RawMessage(after.String)
		}
		if rev.Valid {
			v := rev.Int64
			r.RevertedBy = &v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

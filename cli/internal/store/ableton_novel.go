// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written (slice C): store helpers for the transcendence commands
// (notes backups, mix states, snapshots, device chains, journal queries and
// the local browser index lookup). Every helper creates the Ableton tables
// on first use so a fresh install works without a migration step.

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound is returned when a named or numbered local record is missing.
var ErrNotFound = errors.New("not found")

func nowText(ts time.Time) string { return ts.UTC().Format(time.RFC3339Nano) }

// ---------- note backups ----------

// NoteBackup is one saved copy of a clip's notes.
type NoteBackup struct {
	ID         int64           `json:"id"`
	TS         string          `json:"ts"`
	TrackName  string          `json:"track_name"`
	TrackIndex int             `json:"track_number"`
	Slot       int             `json:"slot"`
	ClipName   string          `json:"clip_name"`
	Reason     string          `json:"reason"`
	NoteCount  int             `json:"note_count"`
	Notes      json.RawMessage `json:"notes,omitempty"`
}

// InsertNoteBackup stores a backup. trackNumber and slot are 1-based.
func InsertNoteBackup(ctx context.Context, db *sql.DB, ts time.Time, trackName string, trackNumber, slot int, clipName, reason string, notesJSON []byte) (int64, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return 0, err
	}
	if !json.Valid(notesJSON) {
		return 0, fmt.Errorf("note backup is not valid JSON")
	}
	res, err := db.ExecContext(ctx,
		`INSERT INTO note_backups (ts, track_name, track_index, slot, clip_name, reason, notes_json) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		nowText(ts), trackName, trackNumber, slot, clipName, reason, string(notesJSON))
	if err != nil {
		return 0, fmt.Errorf("writing note backup: %w", err)
	}
	return res.LastInsertId()
}

func noteCount(raw string) int {
	var arr []json.RawMessage
	if json.Unmarshal([]byte(raw), &arr) != nil {
		return 0
	}
	return len(arr)
}

func scanNoteBackup(sc interface{ Scan(...any) error }, withNotes bool) (NoteBackup, error) {
	var b NoteBackup
	var tname, cname, reason sql.NullString
	var tidx, slot sql.NullInt64
	var notes string
	if err := sc.Scan(&b.ID, &b.TS, &tname, &tidx, &slot, &cname, &reason, &notes); err != nil {
		return b, err
	}
	b.TrackName, b.ClipName, b.Reason = tname.String, cname.String, reason.String
	b.TrackIndex, b.Slot = int(tidx.Int64), int(slot.Int64)
	b.NoteCount = noteCount(notes)
	if withNotes {
		b.Notes = json.RawMessage(notes)
	}
	return b, nil
}

// GetNoteBackup returns one backup including its notes.
func GetNoteBackup(ctx context.Context, db *sql.DB, id int64) (NoteBackup, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return NoteBackup{}, err
	}
	row := db.QueryRowContext(ctx, `SELECT id, ts, track_name, track_index, slot, clip_name, reason, notes_json FROM note_backups WHERE id = ?`, id)
	b, err := scanNoteBackup(row, true)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// ListNoteBackups returns backups newest first, optionally filtered by a
// case-insensitive track-name substring and a 1-based slot.
func ListNoteBackups(ctx context.Context, db *sql.DB, track string, slot, limit int) ([]NoteBackup, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return nil, err
	}
	q := `SELECT id, ts, track_name, track_index, slot, clip_name, reason, notes_json FROM note_backups WHERE 1=1`
	var args []any
	if strings.TrimSpace(track) != "" {
		q += ` AND lower(track_name) LIKE ? ESCAPE '\'`
		args = append(args, "%"+likeEscape(strings.ToLower(strings.TrimSpace(track)))+"%")
	}
	if slot > 0 {
		q += ` AND slot = ?`
		args = append(args, slot)
	}
	if limit <= 0 {
		limit = 50
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NoteBackup{}
	for rows.Next() {
		b, err := scanNoteBackup(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ---------- mix states ----------

// MixRow is one track's saved mixer state inside a named mix.
type MixRow struct {
	Mix      string          `json:"mix"`
	TrackKey string          `json:"track_key"`
	State    json.RawMessage `json:"state"`
	SavedAt  string          `json:"saved_at"`
}

// MixSummary describes one saved mix.
type MixSummary struct {
	Name    string `json:"name"`
	Tracks  int    `json:"tracks"`
	SavedAt string `json:"saved_at"`
}

// SaveMix stores all rows of a mix in one transaction. With replace=false an
// existing mix of the same name is an error.
func SaveMix(ctx context.Context, db *sql.DB, name string, ts time.Time, rows map[string][]byte, replace bool) (bool, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return false, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM mix_states WHERE name = ?`, name).Scan(&n); err != nil {
		return false, err
	}
	existed := n > 0
	if existed && !replace {
		return true, fmt.Errorf("a mix named %q already exists", name)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mix_states WHERE name = ?`, name); err != nil {
		return existed, err
	}
	for key, state := range rows {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mix_states (name, track_name, state_json, saved_at) VALUES (?, ?, ?, ?)`, name, key, string(state), nowText(ts)); err != nil {
			return existed, fmt.Errorf("writing mix state: %w", err)
		}
	}
	return existed, tx.Commit()
}

// LoadMix returns every row of a mix, or ErrNotFound.
func LoadMix(ctx context.Context, db *sql.DB, name string) ([]MixRow, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT name, track_name, state_json, saved_at FROM mix_states WHERE name = ? ORDER BY rowid`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MixRow
	for rows.Next() {
		var r MixRow
		var state string
		if err := rows.Scan(&r.Mix, &r.TrackKey, &state, &r.SavedAt); err != nil {
			return nil, err
		}
		r.State = json.RawMessage(state)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// ListMixes returns one summary per saved mix, newest first.
func ListMixes(ctx context.Context, db *sql.DB) ([]MixSummary, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT name, count(*), max(saved_at) FROM mix_states GROUP BY name ORDER BY max(saved_at) DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MixSummary{}
	for rows.Next() {
		var m MixSummary
		if err := rows.Scan(&m.Name, &m.Tracks, &m.SavedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---------- snapshots ----------

// SnapshotRow is one stored project snapshot.
type SnapshotRow struct {
	ID       int64           `json:"id"`
	Label    string          `json:"label"`
	TS       string          `json:"ts"`
	Bytes    int             `json:"bytes"`
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
}

// SaveSnapshot stores a snapshot under a label. With replace=false an
// existing label is an error; with replace=true older rows with the label
// are removed.
func SaveSnapshot(ctx context.Context, db *sql.DB, label string, ts time.Time, snapshot []byte, replace bool) (int64, bool, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return 0, false, err
	}
	if !json.Valid(snapshot) {
		return 0, false, fmt.Errorf("snapshot is not valid JSON")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM snapshots WHERE label = ?`, label).Scan(&n); err != nil {
		return 0, false, err
	}
	existed := n > 0
	if existed && !replace {
		return 0, true, fmt.Errorf("a snapshot named %q already exists", label)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM snapshots WHERE label = ?`, label); err != nil {
		return 0, existed, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO snapshots (label, ts, snapshot_json) VALUES (?, ?, ?)`, label, nowText(ts), string(snapshot))
	if err != nil {
		return 0, existed, fmt.Errorf("writing snapshot: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, existed, err
	}
	return id, existed, tx.Commit()
}

// GetSnapshot finds a snapshot by exact label (newest wins) or, failing
// that, by numeric id.
func GetSnapshot(ctx context.Context, db *sql.DB, ref string) (SnapshotRow, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return SnapshotRow{}, err
	}
	var r SnapshotRow
	var body string
	err := db.QueryRowContext(ctx, `SELECT id, label, ts, snapshot_json FROM snapshots WHERE label = ? ORDER BY id DESC LIMIT 1`, ref).Scan(&r.ID, &r.Label, &r.TS, &body)
	if errors.Is(err, sql.ErrNoRows) {
		var id int64
		if _, scanErr := fmt.Sscanf(strings.TrimPrefix(strings.TrimSpace(ref), "#"), "%d", &id); scanErr == nil && id > 0 {
			err = db.QueryRowContext(ctx, `SELECT id, label, ts, snapshot_json FROM snapshots WHERE id = ?`, id).Scan(&r.ID, &r.Label, &r.TS, &body)
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.Snapshot = json.RawMessage(body)
	r.Bytes = len(body)
	return r, nil
}

// ListSnapshots returns snapshots newest first, without their bodies.
func ListSnapshots(ctx context.Context, db *sql.DB) ([]SnapshotRow, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id, label, ts, length(snapshot_json) FROM snapshots ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SnapshotRow{}
	for rows.Next() {
		var r SnapshotRow
		if err := rows.Scan(&r.ID, &r.Label, &r.TS, &r.Bytes); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- chains ----------

// ChainRow is one saved device chain.
type ChainRow struct {
	Name        string          `json:"name"`
	SavedAt     string          `json:"saved_at"`
	SourceTrack string          `json:"source_track"`
	Chain       json.RawMessage `json:"chain,omitempty"`
}

// SaveChain stores a chain. With replace=false an existing name is an error.
func SaveChain(ctx context.Context, db *sql.DB, name, sourceTrack string, ts time.Time, chain []byte, replace bool) (bool, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return false, err
	}
	if !json.Valid(chain) {
		return false, fmt.Errorf("chain is not valid JSON")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM chains WHERE name = ?`, name).Scan(&n); err != nil {
		return false, err
	}
	existed := n > 0
	if existed && !replace {
		return true, fmt.Errorf("a chain named %q already exists", name)
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO chains (name, saved_at, source_track, chain_json) VALUES (?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET saved_at = excluded.saved_at, source_track = excluded.source_track, chain_json = excluded.chain_json`,
		name, nowText(ts), sourceTrack, string(chain))
	if err != nil {
		return existed, fmt.Errorf("writing chain: %w", err)
	}
	return existed, nil
}

// GetChain returns one chain or ErrNotFound.
func GetChain(ctx context.Context, db *sql.DB, name string) (ChainRow, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return ChainRow{}, err
	}
	var r ChainRow
	var src sql.NullString
	var body string
	err := db.QueryRowContext(ctx, `SELECT name, saved_at, source_track, chain_json FROM chains WHERE name = ?`, name).Scan(&r.Name, &r.SavedAt, &src, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.SourceTrack = src.String
	r.Chain = json.RawMessage(body)
	return r, nil
}

// ListChains returns every chain (with body), newest first.
func ListChains(ctx context.Context, db *sql.DB) ([]ChainRow, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT name, saved_at, source_track, chain_json FROM chains ORDER BY saved_at DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChainRow{}
	for rows.Next() {
		var r ChainRow
		var src sql.NullString
		var body string
		if err := rows.Scan(&r.Name, &r.SavedAt, &src, &body); err != nil {
			return nil, err
		}
		r.SourceTrack = src.String
		r.Chain = json.RawMessage(body)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- journal queries ----------

// JournalFilter narrows QueryJournal. Zero values mean "no filter".
type JournalFilter struct {
	Since   time.Time
	Command string // case-insensitive substring of the command path
	Target  string // case-insensitive substring of the target
	Limit   int
}

func scanJournal(sc interface{ Scan(...any) error }) (JournalRow, error) {
	var r JournalRow
	var before, after sql.NullString
	var rev sql.NullInt64
	if err := sc.Scan(&r.ID, &r.TS, &r.Command, &r.Method, &r.Path, &r.Target, &r.Property, &before, &after, &rev); err != nil {
		return r, err
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
	return r, nil
}

// QueryJournal returns journal rows newest first.
func QueryJournal(ctx context.Context, db *sql.DB, f JournalFilter) ([]JournalRow, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return nil, err
	}
	q := `SELECT id, ts, command, method, path, target, property, before_json, after_json, reverted_by FROM journal WHERE 1=1`
	var args []any
	if !f.Since.IsZero() {
		// Whole seconds without a zone suffix: RFC3339Nano trims trailing
		// zeros, so comparing full timestamps as text misorders values
		// inside one second; a seconds prefix always sorts at or before them.
		q += ` AND ts >= ?`
		args = append(args, f.Since.UTC().Format("2006-01-02T15:04:05"))
	}
	if s := strings.TrimSpace(f.Command); s != "" {
		q += ` AND lower(command) LIKE ? ESCAPE '\'`
		args = append(args, "%"+likeEscape(strings.ToLower(s))+"%")
	}
	if s := strings.TrimSpace(f.Target); s != "" {
		q += ` AND lower(target) LIKE ? ESCAPE '\'`
		args = append(args, "%"+likeEscape(strings.ToLower(s))+"%")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JournalRow{}
	for rows.Next() {
		r, err := scanJournal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// JournalRevertCommand is the command path recorded on rows written by
// 'journal revert'.
const JournalRevertCommand = "journal revert"

// LatestUnrevertedJournal returns up to n of the newest journal rows that
// have not been reverted and are not themselves reverts, newest first. It
// backs 'journal revert --last N'.
func LatestUnrevertedJournal(ctx context.Context, db *sql.DB, n int) ([]JournalRow, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return nil, err
	}
	if n <= 0 {
		return []JournalRow{}, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, ts, command, method, path, target, property, before_json, after_json, reverted_by FROM journal WHERE reverted_by IS NULL AND command <> ? ORDER BY id DESC LIMIT ?`, JournalRevertCommand, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JournalRow{}
	for rows.Next() {
		r, err := scanJournal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetJournal returns one journal row or ErrNotFound.
func GetJournal(ctx context.Context, db *sql.DB, id int64) (JournalRow, error) {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return JournalRow{}, err
	}
	row := db.QueryRowContext(ctx, `SELECT id, ts, command, method, path, target, property, before_json, after_json, reverted_by FROM journal WHERE id = ?`, id)
	r, err := scanJournal(row)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// MarkJournalReverted links a journal row to the row that reverted it.
func MarkJournalReverted(ctx context.Context, db *sql.DB, id, revertedBy int64) error {
	if err := EnsureAbletonTables(ctx, db); err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `UPDATE journal SET reverted_by = ? WHERE id = ?`, revertedBy, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- browser index ----------

// BrowserItem is one loadable item from the local browser index that
// `sync --resources browser` fills.
type BrowserItem struct {
	URI      string `json:"uri"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Category string `json:"category"`
	IsDevice bool   `json:"is_device"`
}

// LoadableBrowserItems returns every loadable item with a URI. A missing
// browser table (never synced) returns an empty list, not an error.
func LoadableBrowserItems(ctx context.Context, db *sql.DB) ([]BrowserItem, error) {
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'browser'`).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return []BrowserItem{}, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT data FROM "browser" ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrowserItem{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var it struct {
			URI        string `json:"uri"`
			Name       string `json:"name"`
			Path       string `json:"path"`
			Category   string `json:"category"`
			IsDevice   bool   `json:"is_device"`
			IsLoadable *bool  `json:"is_loadable"`
		}
		if json.Unmarshal([]byte(raw), &it) != nil {
			continue
		}
		if strings.TrimSpace(it.URI) == "" || (it.IsLoadable != nil && !*it.IsLoadable) {
			continue
		}
		out = append(out, BrowserItem{URI: it.URI, Name: it.Name, Path: it.Path, Category: it.Category, IsDevice: it.IsDevice})
	}
	return out, rows.Err()
}

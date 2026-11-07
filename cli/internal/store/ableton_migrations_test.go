// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestAbletonJournal(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := EnsureAbletonTables(ctx, s.DB()); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAbletonTables(ctx, s.DB()); err != nil {
		t.Fatalf("second ensure must be a no-op: %v", err)
	}
	for _, table := range []string{"journal", "note_backups", "mix_states", "snapshots", "chains"} {
		var n int
		if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s missing (%v)", table, err)
		}
	}
	id1, err := InsertJournal(ctx, s.DB(), time.Now(), "track mute", "PUT", "/tracks/Bass/mute", `track 2 "Bass"`, "mute", false, true)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := InsertJournal(ctx, s.DB(), time.Now(), "tempo", "PUT", "/song/props/tempo", "song", "tempo", 120.0, 121.5)
	if err != nil {
		t.Fatal(err)
	}
	if id2 <= id1 {
		t.Fatalf("ids not increasing: %d %d", id1, id2)
	}
	rows, err := RecentJournal(ctx, s.DB(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != id2 || string(rows[0].After) != "121.5" || string(rows[1].Before) != "false" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestEnsureAbletonTablesNil(t *testing.T) {
	if err := EnsureAbletonTables(context.Background(), nil); err == nil {
		t.Fatal("nil db must error")
	}
}

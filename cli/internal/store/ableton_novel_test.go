// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAbletonNovelStoreHelpers(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db := s.DB()
	now := time.Now()

	id, err := InsertNoteBackup(ctx, db, now, "Bass", 2, 1, "Line", "notes replace", []byte(`[{"pitch":40}]`))
	if err != nil || id != 1 {
		t.Fatalf("backup: %d %v", id, err)
	}
	if _, err := InsertNoteBackup(ctx, db, now, "Bass", 2, 1, "Line", "x", []byte(`nope`)); err == nil {
		t.Fatal("invalid JSON must be refused")
	}
	b, err := GetNoteBackup(ctx, db, 1)
	if err != nil || b.NoteCount != 1 || string(b.Notes) != `[{"pitch":40}]` {
		t.Fatalf("get backup: %+v %v", b, err)
	}
	if _, err := GetNoteBackup(ctx, db, 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing backup: %v", err)
	}
	if list, _ := ListNoteBackups(ctx, db, "ba", 1, 0); len(list) != 1 || list[0].Notes != nil {
		t.Fatalf("list backups: %+v", list)
	}
	if list, _ := ListNoteBackups(ctx, db, "100%", 0, 0); len(list) != 0 {
		t.Fatalf("LIKE wildcards must be escaped: %+v", list)
	}

	if _, err := SaveMix(ctx, db, "m", now, map[string][]byte{"Bass": []byte(`{}`)}, false); err != nil {
		t.Fatal(err)
	}
	if existed, err := SaveMix(ctx, db, "m", now, map[string][]byte{"Bass": []byte(`{}`)}, false); err == nil || !existed {
		t.Fatal("second save without replace must fail")
	}
	if existed, err := SaveMix(ctx, db, "m", now, map[string][]byte{"A": []byte(`{}`), "B": []byte(`{}`)}, true); err != nil || !existed {
		t.Fatalf("replace: %v", err)
	}
	if rows, err := LoadMix(ctx, db, "m"); err != nil || len(rows) != 2 {
		t.Fatalf("load mix: %+v %v", rows, err)
	}
	if _, err := LoadMix(ctx, db, "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing mix: %v", err)
	}

	sid, _, err := SaveSnapshot(ctx, db, "a", now, []byte(`{"tracks":[]}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := GetSnapshot(ctx, db, "#"+strconv.FormatInt(sid, 10)); err != nil || r.Label != "a" {
		t.Fatalf("snapshot by id: %+v %v", r, err)
	}

	if _, err := SaveChain(ctx, db, "c", "Bass", now, []byte(`{"devices":[]}`), false); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveChain(ctx, db, "c", "Bass", now, []byte(`{}`), false); err == nil {
		t.Fatal("duplicate chain must fail")
	}

	for _, cmd := range []string{"track mute", "mix recall", "mix recall"} {
		if _, err := InsertJournal(ctx, db, now, cmd, "PUT", "/tracks/1/mute", `track 1 "Drums"`, "mute", false, true); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := QueryJournal(ctx, db, JournalFilter{Command: "MIX", Since: now.Add(-time.Minute)})
	if err != nil || len(rows) != 2 || rows[0].ID != 3 {
		t.Fatalf("query journal: %+v %v", rows, err)
	}
	if err := MarkJournalReverted(ctx, db, 1, 3); err != nil {
		t.Fatal(err)
	}
	if r, _ := GetJournal(ctx, db, 1); r.RevertedBy == nil || *r.RevertedBy != 3 {
		t.Fatalf("reverted_by: %+v", r)
	}
	if items, err := LoadableBrowserItems(ctx, db); err != nil || len(items) != 0 {
		t.Fatalf("empty browser index: %v %v", items, err)
	}
}

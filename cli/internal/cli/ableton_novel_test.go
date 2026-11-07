// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written (slice C) behaviour tests for the transcendence commands,
// run as whole CLI invocations against the fake bridges.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"ableton-pp-cli/internal/client"
	"ableton-pp-cli/internal/live"
	"ableton-pp-cli/internal/store"
)

func requireNoWrites(t *testing.T, f *fakeLive, what string) {
	t.Helper()
	if w := f.writes(); len(w) > 0 {
		t.Fatalf("%s sent writes to Live: %v", what, w)
	}
}

// ---------- notes replace / restore / backups ----------

func TestNotesReplaceBackupVerifyAndRestore(t *testing.T) {
	f := withFakeLive(t)
	var res noteWriteResult
	runJSON(t, &res, "notes", "replace", "Bass", "1", "--notes",
		`[{"pitch":45,"start":0,"duration":0.5},{"pitch":47,"start":0.5,"duration":0.5,"velocity":80},{"pitch":48,"start":1,"duration":1}]`)
	if !res.Verified || res.NotesBefore != 2 || res.NotesAfter != 3 || res.BackupID != 1 || res.Track != "Bass" || res.Slot != 1 {
		t.Fatalf("replace result: %+v", res)
	}
	if got := len(f.tracks[1].clips[0].notes); got != 3 {
		t.Fatalf("clip has %d notes, want 3", got)
	}
	var backups []store.NoteBackup
	runJSON(t, &backups, "notes", "backups")
	if len(backups) != 1 || backups[0].NoteCount != 2 || backups[0].TrackName != "Bass" || backups[0].Reason != "notes replace" {
		t.Fatalf("backups: %+v", backups)
	}
	// Tracks move: restore still finds "Bass" by name.
	f.mu.Lock()
	f.tracks[0], f.tracks[1] = f.tracks[1], f.tracks[0]
	f.mu.Unlock()
	runJSON(t, &res, "notes", "restore", "1")
	if res.RestoredFrom == nil || *res.RestoredFrom != 1 || res.NotesAfter != 2 || res.TrackNumber != 1 || res.BackupID != 2 {
		t.Fatalf("restore result: %+v", res)
	}
	notes := f.tracks[0].clips[0].notes
	if len(notes) != 2 || notes[0].Pitch != 40 || notes[1].Pitch != 43 {
		t.Fatalf("restored notes: %+v", notes)
	}
	runJSON(t, &backups, "notes", "backups", "--track", "bass")
	if len(backups) != 2 || !strings.Contains(backups[0].Reason, "restore of backup 1") {
		t.Fatalf("backups after restore: %+v", backups)
	}
}

func TestNotesReplaceDryRunSendsNothing(t *testing.T) {
	f := withFakeLive(t)
	out, err := runCLI(t, "notes", "replace", "Bass", "1", "--notes", `[{"pitch":36,"start":0,"duration":1}]`, "--dry-run", "--json")
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if !strings.Contains(compactJSON(out), `"notes_to_write":1`) || !strings.Contains(compactJSON(out), `"dry_run":true`) {
		t.Fatalf("dry-run output: %s", out)
	}
	if tr := f.traffic(); len(tr) != 0 {
		t.Fatalf("dry run contacted Live: %v", tr)
	}
	// restore dry-run is local only as well
	s := openTestStore(t)
	if _, err := store.InsertNoteBackup(context.Background(), s.DB(), time.Now(), "Bass", 2, 1, "Bassline", "test", []byte(`[]`)); err != nil {
		t.Fatal(err)
	}
	if out, err := runCLI(t, "notes", "restore", "1", "--dry-run", "--json"); err != nil || !strings.Contains(compactJSON(out), `"backup_id":1`) {
		t.Fatalf("restore dry run: %v\n%s", err, out)
	}
	if tr := f.traffic(); len(tr) != 0 {
		t.Fatalf("restore dry run contacted Live: %v", tr)
	}
}

func TestNotesReplaceErrors(t *testing.T) {
	f := withFakeLive(t)
	_, err := runCLI(t, "notes", "replace", "Guitar", "1", "--notes", `[]`)
	if exitCode(err) != 3 {
		t.Fatalf("unknown track: exit %d (%v)", exitCode(err), err)
	}
	f.mu.Lock()
	f.tracks[2].name = "Bass 2"
	f.mu.Unlock()
	_, err = runCLI(t, "notes", "replace", "Bas", "1", "--notes", `[]`)
	if exitCode(err) != 5 || !strings.Contains(err.Error(), `"Bass 2"`) {
		t.Fatalf("ambiguous track: exit %d (%v)", exitCode(err), err)
	}
	f.resetTraffic()
	_, err = runCLI(t, "notes", "replace", "Bass", "1", "--notes", `[{"pitch":36,"start":0,"duration":1},{"pitch":36,"start":0,"duration":2}]`)
	if exitCode(err) != 2 || len(f.traffic()) != 0 {
		t.Fatalf("duplicate notes must be refused before contacting Live: %v %v", err, f.traffic())
	}
	_, err = runCLI(t, "notes", "replace", "Bass", "1", "--notes", `not json`)
	if exitCode(err) != 2 {
		t.Fatalf("bad JSON: %v", err)
	}
	if _, err := runCLI(t, "notes", "restore", "99"); exitCode(err) != 3 {
		t.Fatalf("missing backup: %v", err)
	}
	// empty array clears the clip (backup first)
	var res noteWriteResult
	runJSON(t, &res, "notes", "replace", "Drums", "1", "--notes", `[]`)
	if res.NotesAfter != 0 || res.NotesBefore != 1 || !res.Verified {
		t.Fatalf("clear via replace: %+v", res)
	}
}

// ---------- device add ----------

func seedStandardBrowser(t *testing.T) {
	seedBrowser(t,
		map[string]any{"uri": "query:Synths#Drift", "name": "Drift", "path": "instruments/Drift", "category": "instruments", "is_device": true},
		map[string]any{"uri": "query:AudioFx#Echo", "name": "Echo", "path": "audio_effects/Echo", "category": "audio_effects", "is_device": true},
		map[string]any{"uri": "query:AudioFx#Reverb:Hall", "name": "Reverb Hall.adv", "path": "audio_effects/Reverb/Reverb Hall.adv", "category": "audio_effects"},
		map[string]any{"uri": "query:Sounds#Reverb:Pad", "name": "Reverb Pad.adg", "path": "sounds/Pad/Reverb Pad.adg", "category": "sounds"},
	)
}

func TestDeviceAddByName(t *testing.T) {
	f := withFakeLive(t)
	seedStandardBrowser(t)
	var out map[string]any
	runJSON(t, &out, "device", "add", "keys", "drift")
	if out["uri"] != "query:Synths#Drift" || out["track"] != "Keys" {
		t.Fatalf("device add: %v", out)
	}
	if len(f.tracks[2].devices) != 1 || f.tracks[2].devices[0].name != "Drift" {
		t.Fatalf("Keys devices: %+v", f.tracks[2].devices)
	}
	if after, _ := out["devices_after"].([]any); len(after) != 1 {
		t.Fatalf("devices_after: %v", out["devices_after"])
	}
	// prefix match + dry run: resolves, reads tracks, loads nothing
	f.resetTraffic()
	runJSON(t, &out, "device", "add", "Vox", "Ech", "--dry-run")
	if out["dry_run"] != true || out["uri"] != "query:AudioFx#Echo" {
		t.Fatalf("dry run: %v", out)
	}
	requireNoWrites(t, f, "device add --dry-run")
}

func TestDeviceAddResolutionErrors(t *testing.T) {
	f := withFakeLive(t)
	// empty index explains how to build it
	_, err := runCLI(t, "device", "add", "Bass", "Echo")
	if exitCode(err) != 10 || !strings.Contains(err.Error(), "sync --resources browser") {
		t.Fatalf("empty index: exit %d %v", exitCode(err), err)
	}
	seedStandardBrowser(t)
	_, err = runCLI(t, "device", "add", "Bass", "Reverb")
	if exitCode(err) != 5 || !strings.Contains(err.Error(), "--category") || !strings.Contains(err.Error(), "Reverb Hall.adv") {
		t.Fatalf("ambiguous browser name: exit %d %v", exitCode(err), err)
	}
	var out map[string]any
	runJSON(t, &out, "device", "add", "Bass", "Reverb", "--category", "audio_effects", "--dry-run")
	if out["uri"] != "query:AudioFx#Reverb:Hall" {
		t.Fatalf("--category: %v", out)
	}
	_, err = runCLI(t, "device", "add", "Bass", "Drfit")
	if exitCode(err) != 3 || !strings.Contains(err.Error(), `"Drift"`) {
		t.Fatalf("not found with suggestion: exit %d %v", exitCode(err), err)
	}
	_, err = runCLI(t, "device", "add", "Guitar", "Drift")
	if exitCode(err) != 3 {
		t.Fatalf("unknown track: exit %d %v", exitCode(err), err)
	}
	_, err = runCLI(t, "device", "add", "Vox", "Drift")
	if exitCode(err) != 2 || !strings.Contains(err.Error(), "audio track") {
		t.Fatalf("instrument on audio track: exit %d %v", exitCode(err), err)
	}
	requireNoWrites(t, f, "failed device adds")
}

// ---------- arrangement build ----------

func TestArrangementPlanLayout(t *testing.T) {
	clips := []gridClip{
		{Track: "Drums", TrackNumber: 1, Slot: 1, Name: "A", LengthBeats: 4},
		{Track: "Bass", TrackNumber: 2, Slot: 1, Name: "B", LengthBeats: 6},
		{Track: "Drums", TrackNumber: 1, Slot: 2, Name: "C", LengthBeats: 4},
	}
	specs, err := parsePlanFlag("Intro, Drop:1")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildArrangementPlan(specs, []string{"Intro", "Drop"}, clips, 4, 4, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Intro has no bar count: the longest clip (6 beats) rounds up to 2 bars.
	if plan.Sections[0].Bars != 2 || plan.Sections[0].BarsSource != "longest clip" || plan.Sections[1].StartBeat != 8 || plan.EndBeat != 12 {
		t.Fatalf("sections: %+v end %v", plan.Sections, plan.EndBeat)
	}
	// Drums A at 0,4; Bass B at 0 and 6 (overruns by 4); Drums C at 8.
	if len(plan.Placements) != 5 || plan.Placements[3].OverrunBeats != 4 || plan.Placements[4].At != "3.1" {
		t.Fatalf("placements: %+v", plan.Placements)
	}
	if _, err := buildArrangementPlan([]arrSectionSpec{{Name: "X", Scene: "Nope"}}, []string{"Intro"}, clips, 4, 4, 0, 0); err == nil {
		t.Fatal("unknown scene must fail")
	}
	s, err := parseSectionFlag("Verse:scene=Drop:bars=16")
	if err != nil || s.Name != "Verse" || s.Scene != "Drop" || s.Bars != 16 {
		t.Fatalf("section flag: %+v %v", s, err)
	}
	if _, err := parseSectionFlag("Verse:color=red"); err == nil {
		t.Fatal("unknown key must fail")
	}
}

func TestArrangementBuildDryRunAndBuild(t *testing.T) {
	f := withFakeLive(t)
	var plan arrPlan
	runJSON(t, &plan, "arrangement", "build", "--plan", "Intro:4,Drop:2", "--dry-run")
	if !plan.DryRun || len(plan.Sections) != 2 || plan.TotalBars != 6 || plan.End != "7.1" {
		t.Fatalf("plan: %+v", plan)
	}
	// Intro 16 beats: Drums (4) x4 + Bass (8) x2; Drop 8 beats: Drums B x2.
	if len(plan.Placements) != 8 || len(plan.Locators) != 2 || plan.Locators[1].At != "5.1" {
		t.Fatalf("placements/locators: %+v %+v", plan.Placements, plan.Locators)
	}
	requireNoWrites(t, f, "arrangement build --dry-run")

	var built arrPlan
	runJSON(t, &built, "arrangement", "build", "--plan", "Intro:4,Drop:2")
	if built.Placed != 8 || built.Located != 2 {
		t.Fatalf("build result: %+v", built)
	}
	if len(f.tracks[0].arr) != 6 || len(f.tracks[1].arr) != 2 || f.tracks[0].arr[4].start != 16 || len(f.cues) != 2 || f.cues[1].name != "Drop" {
		t.Fatalf("fake arrangement: drums %+v bass %+v cues %+v", f.tracks[0].arr, f.tracks[1].arr, f.cues)
	}
	// Building again over the same range is a conflict unless --force.
	f.resetTraffic()
	_, err := runCLI(t, "arrangement", "build", "--plan", "Intro:4")
	if exitCode(err) != 5 || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("conflict: exit %d %v", exitCode(err), err)
	}
	requireNoWrites(t, f, "refused build")
	runJSON(t, &plan, "arrangement", "build", "--plan", "Intro:4", "--dry-run")
	if len(plan.Conflicts) == 0 || plan.Locators[0].Replaces != "Intro" {
		t.Fatalf("dry run should list conflicts and the renamed locator: %+v", plan)
	}
	if _, err := runCLI(t, "arrangement", "build", "--plan", "Chorus:4", "--dry-run"); exitCode(err) != 3 {
		t.Fatalf("unknown scene: %v", err)
	}
}

// ---------- mix save / recall / list ----------

func TestMixSaveRecallWithMissingTracks(t *testing.T) {
	f := withFakeLive(t)
	var saved map[string]any
	runJSON(t, &saved, "mix", "save", "rough")
	if saved["tracks"] != float64(4) {
		t.Fatalf("mix save: %v", saved)
	}
	if _, err := runCLI(t, "mix", "save", "rough"); exitCode(err) != 5 {
		t.Fatalf("second save without --force: %v", err)
	}
	// Change the mixer, move tracks, rename Keys to Pad.
	f.mu.Lock()
	bass := f.tracks[1]
	bass.volume, bass.mute, bass.sends[1] = 0.3, true, 0.9
	f.tracks[0], f.tracks[1] = f.tracks[1], f.tracks[0]
	f.tracks[2].name = "Pad"
	f.tracks[3].pan = 0.5
	f.mu.Unlock()

	f.resetTraffic()
	var dry mixRecallResult
	runJSON(t, &dry, "mix", "recall", "rough", "--dry-run")
	if len(dry.Changes) != 4 || len(dry.Missing) != 1 || dry.Missing[0] != "Keys" || len(dry.Extra) != 1 || dry.Extra[0] != "Pad" {
		t.Fatalf("dry recall: %+v", dry)
	}
	requireNoWrites(t, f, "mix recall --dry-run")

	var res mixRecallResult
	runJSON(t, &res, "mix", "recall", "rough")
	if len(res.Applied) != 4 || !res.Success {
		t.Fatalf("recall: %+v", res)
	}
	if !nearlyEqual(bass.volume, 0.7) || bass.mute || !nearlyEqual(bass.sends[1], 0.3) || f.tracks[3].pan != 0 {
		t.Fatalf("mixer not restored: bass %+v vox pan %v", bass, f.tracks[3].pan)
	}
	if f.tracks[2].volume != 0.6 {
		t.Fatalf("Pad (not in the mix) must be left alone")
	}
	var rows []store.JournalRow
	runJSON(t, &rows, "journal", "list", "--command", "mix recall")
	if len(rows) != 4 {
		t.Fatalf("recall changes must be journaled: %+v", rows)
	}
	var mixes []store.MixSummary
	runJSON(t, &mixes, "mix", "list")
	if len(mixes) != 1 || mixes[0].Name != "rough" || mixes[0].Tracks != 4 {
		t.Fatalf("mix list: %+v", mixes)
	}
	if _, err := runCLI(t, "mix", "recall", "nope"); exitCode(err) != 3 {
		t.Fatalf("unknown mix: %v", err)
	}
}

// ---------- journal list / revert ----------

func TestJournalRevertWithConflictCheck(t *testing.T) {
	f := withFakeLive(t)
	if out, err := runCLI(t, "track", "mute", "Drums", "--on", "true", "--json"); err != nil {
		t.Fatalf("mute: %v\n%s", err, out)
	}
	if out, err := runCLI(t, "tempo", "130", "--json"); err != nil {
		t.Fatalf("tempo: %v\n%s", err, out)
	}
	if out, err := runCLI(t, "param", "set", "Bass", "Drift", "Filter", "--value", "0.9", "--json"); err != nil {
		t.Fatalf("param set: %v\n%s", err, out)
	}
	var rows []store.JournalRow
	runJSON(t, &rows, "journal", "list", "--target", "drums", "--since", "1h")
	if len(rows) != 1 || rows[0].Property != "mute" || string(rows[0].After) != "true" {
		t.Fatalf("journal list filter: %+v", rows)
	}
	runJSON(t, &rows, "journal", "list", "--limit", "2")
	if len(rows) != 2 || rows[0].ID != 3 {
		t.Fatalf("journal list limit/order: %+v", rows)
	}

	// dry run: reads only
	f.resetTraffic()
	var dry revertResult
	runJSON(t, &dry, "journal", "revert", "1", "2", "3", "--dry-run")
	if len(dry.Planned) != 3 || dry.Planned[0].ID != 3 {
		t.Fatalf("dry revert plan: %+v", dry)
	}
	requireNoWrites(t, f, "journal revert --dry-run")

	var res revertResult
	runJSON(t, &res, "journal", "revert", "3", "2")
	if len(res.Reverted) != 2 || f.tempo != 120 || !nearlyEqual(f.tracks[1].devices[0].params[1].value, 0.25) {
		t.Fatalf("revert: %+v tempo %v filter %v", res, f.tempo, f.tracks[1].devices[0].params[1].value)
	}
	s := openTestStore(t)
	row, err := store.GetJournal(context.Background(), s.DB(), 2)
	if err != nil || row.RevertedBy == nil {
		t.Fatalf("row 2 must be linked to its revert: %+v %v", row, err)
	}
	link, _ := store.GetJournal(context.Background(), s.DB(), *row.RevertedBy)
	if link.Command != "journal revert" || link.Property != "tempo" || string(link.After) != "120" {
		t.Fatalf("revert row: %+v", link)
	}
	if _, err := runCLI(t, "journal", "revert", "2"); exitCode(err) != 5 || !strings.Contains(err.Error(), "already reverted") {
		t.Fatalf("already reverted: %v", err)
	}

	// Conflict: Drums was unmuted by hand since change 1.
	f.mu.Lock()
	f.tracks[0].mute = false
	f.tracks[0].solo = true
	f.mu.Unlock()
	f.resetTraffic()
	_, err = runCLI(t, "journal", "revert", "1")
	if exitCode(err) != 5 || !strings.Contains(err.Error(), "changed again") {
		t.Fatalf("conflict: exit %d %v", exitCode(err), err)
	}
	requireNoWrites(t, f, "conflicting revert")
	runJSON(t, &res, "journal", "revert", "1", "--force")
	if len(res.Reverted) != 1 || f.tracks[0].mute {
		t.Fatalf("forced revert: %+v", res)
	}
	if _, err := runCLI(t, "journal", "revert", "999"); exitCode(err) != 3 {
		t.Fatalf("missing row: %v", err)
	}
}

func TestJournalRevertLast(t *testing.T) {
	f := withFakeLive(t)
	for _, bpm := range []string{"130", "131"} {
		if out, err := runCLI(t, "tempo", bpm, "--json"); err != nil {
			t.Fatalf("tempo: %v\n%s", err, out)
		}
	}
	if _, err := runCLI(t, "journal", "revert", "1", "--last", "1"); exitCode(err) != 2 {
		t.Fatalf("ids plus --last must be a usage error: %v", err)
	}
	f.resetTraffic()
	var dry revertResult
	runJSON(t, &dry, "journal", "revert", "--last", "1", "--dry-run")
	if len(dry.Planned) != 1 || dry.Planned[0].ID != 2 {
		t.Fatalf("--last 1 dry run must plan row 2: %+v", dry)
	}
	requireNoWrites(t, f, "journal revert --last --dry-run")

	var res revertResult
	runJSON(t, &res, "journal", "revert", "--last", "1")
	if len(res.Reverted) != 1 || f.tempo != 130 {
		t.Fatalf("--last 1: %+v tempo %v", res, f.tempo)
	}
	// The revert row (3) and the reverted row (2) are skipped: next is row 1.
	runJSON(t, &res, "journal", "revert", "--last", "1")
	if len(res.Reverted) != 1 || res.Reverted[0].ID != 1 || f.tempo != 120 {
		t.Fatalf("second --last 1 must revert row 1: %+v tempo %v", res, f.tempo)
	}
	if _, err := runCLI(t, "journal", "revert", "--last", "1"); exitCode(err) != 3 {
		t.Fatalf("nothing left to revert must exit 3: %v", err)
	}
}

func TestJournalRevertFollowsMovedTrack(t *testing.T) {
	f := withFakeLive(t)
	if _, err := runCLI(t, "track", "pan", "Vox", "--value", "0.4", "--json"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.tracks[0], f.tracks[3] = f.tracks[3], f.tracks[0] // Vox moves to track 1
	f.mu.Unlock()
	var res revertResult
	runJSON(t, &res, "journal", "revert", "1")
	if f.tracks[0].name != "Vox" || f.tracks[0].pan != 0 {
		t.Fatalf("revert must follow the track by name: %+v", f.tracks[0])
	}
	if !strings.Contains(res.Reverted[0].Path, "/tracks/1/") {
		t.Fatalf("revert path: %+v", res.Reverted[0])
	}
}

// ---------- snapshot save / list / diff ----------

func TestSnapshotSaveAndDiff(t *testing.T) {
	f := withFakeLive(t)
	// bare `snapshot` lists stored snapshots and never reads the project
	if out, err := runCLI(t, "snapshot", "--json"); err != nil || strings.TrimSpace(lastJSON(out)) != "[]" {
		t.Fatalf("bare snapshot: %v\n%s", err, out)
	}
	if out, err := runCLI(t, "snapshot", "save", "a", "--dry-run", "--json"); err != nil || !strings.Contains(compactJSON(out), `"dry_run":true`) {
		t.Fatalf("save dry run: %v %s", err, out)
	}
	if tr := f.traffic(); len(tr) != 0 {
		t.Fatalf("bare snapshot / dry run contacted Live: %v", tr)
	}
	var saved map[string]any
	runJSON(t, &saved, "snapshot", "save", "a")
	if saved["label"] != "a" || saved["id"] != float64(1) {
		t.Fatalf("save: %v", saved)
	}
	if _, err := runCLI(t, "snapshot", "save", "a"); exitCode(err) != 5 {
		t.Fatalf("duplicate label: %v", err)
	}
	f.mu.Lock()
	f.tracks[1].volume = 0.5                     // mixer
	f.tracks[1].devices[1].params[2].value = 0.9 // parameter
	f.tracks[0].clips[0].notes = append(f.tracks[0].clips[0].notes, live.Note{Pitch: 38, Start: 1, Duration: 0.25, Velocity: 100})
	f.tracks[1].clips[0].notes[0].Velocity = 50 // edit, same count
	f.tracks = append(f.tracks, &fakeTrack{name: "Pad", midi: true, sends: []float64{0, 0}, clips: map[int]*fakeClip{}})
	f.tracks[2].devices = []*fakeDevice{f.templates["query:Synths#Drift"]()} // device added
	f.tempo = 124
	f.mu.Unlock()
	runJSON(t, nil, "snapshot", "save", "b")

	var d snapDiff
	runJSON(t, &d, "snapshot", "diff", "a", "b")
	want := map[string]int{"song": 1, "tracks": 1, "mixer": 1, "devices": 1, "parameters": 1, "notes": 2}
	for k, v := range want {
		if d.Summary[k] != v {
			t.Fatalf("summary %s = %d, want %d\n%+v", k, d.Summary[k], v, d.Changes)
		}
	}
	find := func(sub string) *snapChange {
		for i := range d.Changes {
			if strings.Contains(d.Changes[i].Path, sub) {
				return &d.Changes[i]
			}
		}
		return nil
	}
	if c := find(`track "Pad"`); c == nil || c.Kind != "added" || c.Path != `track "Pad"` {
		t.Fatalf("added track: %+v", c)
	}
	if c := find(`track "Bass" / volume`); c == nil || c.Before != 0.7 || c.After != 0.5 {
		t.Fatalf("volume change: %+v", c)
	}
	if c := find(`param "Dry/Wet"`); c == nil || c.Category != "parameters" {
		t.Fatalf("param change: %+v", c)
	}
	if c := find(`track "Drums" / slot 1 / note_count`); c == nil || c.Before != float64(1) || c.After != float64(2) {
		t.Fatalf("note count: %+v", c)
	}
	if c := find(`track "Bass" / slot 1 / notes`); c == nil || c.Category != "notes" {
		t.Fatalf("same-count note edit: %+v", c)
	}
	var buf bytes.Buffer
	printSnapDiff(&buf, &d)
	human := buf.String()
	for _, s := range []string{"7 changes", "Mixer", `~ "track \"Bass\" / volume": 0.7 -> 0.5`, `+ "track \"Pad\""`, "notes edited (same note count)"} {
		if !strings.Contains(human, s) {
			t.Fatalf("human diff missing %q:\n%s", s, human)
		}
	}
	// against the live set
	f.mu.Lock()
	f.tracks[0].mute = true
	f.mu.Unlock()
	runJSON(t, &d, "snapshot", "diff", "b", "--live")
	if d.Total != 1 || d.Changes[0].Path != `track "Drums" / mute` || d.To.Label != "live set" {
		t.Fatalf("live diff: %+v", d)
	}
	var list []store.SnapshotRow
	runJSON(t, &list, "snapshot", "list")
	if len(list) != 2 || list[0].Label != "b" {
		t.Fatalf("snapshot list: %+v", list)
	}
	if _, err := runCLI(t, "snapshot", "diff", "a", "zzz"); exitCode(err) != 3 {
		t.Fatalf("unknown snapshot: %v", err)
	}
}

func TestSessionSnapshotRouteIsGated(t *testing.T) {
	f := withFakeLive(t)
	c := &client.Client{BaseURL: "osc://127.0.0.1:11000", NoCache: true}
	_, err := c.Get(context.Background(), "/snapshot", nil)
	if err == nil || !strings.Contains(err.Error(), "snapshot save") {
		t.Fatalf("ungated /snapshot must be refused: %v", err)
	}
	for _, s := range f.traffic() {
		if strings.Contains(s, "snapshot") {
			t.Fatalf("get_session_snapshot was sent: %v", f.traffic())
		}
	}
	if _, err := c.Get(client.WithSessionSnapshot(context.Background()), "/snapshot", nil); err != nil {
		t.Fatalf("gated read: %v", err)
	}
}

// ---------- chain save / apply / list ----------

func TestChainSaveApplyList(t *testing.T) {
	f := withFakeLive(t)
	seedStandardBrowser(t)
	var saved map[string]any
	runJSON(t, &saved, "chain", "save", "Bass", "bass-chain")
	if devs, _ := saved["devices"].([]any); len(devs) != 2 {
		t.Fatalf("chain save: %v", saved)
	}
	if _, err := runCLI(t, "chain", "save", "Bass", "bass-chain"); exitCode(err) != 5 {
		t.Fatalf("duplicate chain: %v", err)
	}
	f.resetTraffic()
	var dry chainApplyResult
	runJSON(t, &dry, "chain", "apply", "bass-chain", "Keys", "--dry-run")
	if !dry.DryRun || len(dry.Devices) != 2 || dry.Devices[1].URI != "query:AudioFx#Echo" {
		t.Fatalf("dry apply: %+v", dry)
	}
	requireNoWrites(t, f, "chain apply --dry-run")

	var res chainApplyResult
	runJSON(t, &res, "chain", "apply", "bass-chain", "Keys")
	keys := f.tracks[2]
	if !res.Success || len(keys.devices) != 2 || keys.devices[0].name != "Drift" || keys.devices[1].name != "Echo" {
		t.Fatalf("apply: %+v devices %+v", res, keys.devices)
	}
	if !nearlyEqual(keys.devices[0].params[1].value, 0.25) || keys.devices[0].params[2].value != 4 || !nearlyEqual(keys.devices[1].params[1].value, 0.7) {
		t.Fatalf("parameters not rebuilt: drift %+v echo %+v", keys.devices[0].params, keys.devices[1].params)
	}
	if res.Devices[0].ParamsSet != 2 || res.Devices[0].ParamsSame != 1 || res.Devices[1].ParamsSet != 1 {
		t.Fatalf("param counts: %+v %+v", res.Devices[0], res.Devices[1])
	}
	// Instruments do not fit on an audio track.
	if _, err := runCLI(t, "chain", "apply", "bass-chain", "Vox"); exitCode(err) != 2 {
		t.Fatalf("chain with instrument on audio track: %v", err)
	}
	var list []map[string]any
	runJSON(t, &list, "chain", "list")
	if len(list) != 1 || list[0]["name"] != "bass-chain" {
		t.Fatalf("chain list: %v", list)
	}
}

func TestChainApplyMissingDevice(t *testing.T) {
	f := withFakeLive(t)
	seedBrowser(t, map[string]any{"uri": "query:Synths#Drift", "name": "Drift", "path": "instruments/Drift", "category": "instruments", "is_device": true})
	runJSON(t, nil, "chain", "save", "Bass", "c")
	f.resetTraffic()
	_, err := runCLI(t, "chain", "apply", "c", "Keys")
	if exitCode(err) != 3 || !strings.Contains(err.Error(), "--skip-missing") {
		t.Fatalf("missing device: exit %d %v", exitCode(err), err)
	}
	requireNoWrites(t, f, "refused chain apply")
	var res chainApplyResult
	runJSON(t, &res, "chain", "apply", "c", "Keys", "--skip-missing")
	if len(res.Devices) != 1 || len(res.Skipped) != 1 || len(f.tracks[2].devices) != 1 {
		t.Fatalf("skip-missing: %+v", res)
	}
	// --map points a missing name at an indexed item.
	runJSON(t, &res, "chain", "apply", "c", "Drums", "--map", "Echo=Drift", "--dry-run")
	if len(res.Devices) != 2 || res.Devices[1].URI != "query:Synths#Drift" {
		t.Fatalf("--map: %+v", res)
	}
}

func TestNovelHelpResolves(t *testing.T) {
	for _, args := range [][]string{
		{"notes", "replace"}, {"notes", "restore"}, {"notes", "backups"},
		{"device", "add"}, {"arrangement", "build"},
		{"mix", "save"}, {"mix", "recall"}, {"mix", "list"},
		{"journal", "list"}, {"journal", "revert"},
		{"snapshot"}, {"snapshot", "save"}, {"snapshot", "list"}, {"snapshot", "diff"},
		{"chain", "save"}, {"chain", "apply"}, {"chain", "list"},
	} {
		out, err := runCLI(t, append(args, "--help")...)
		if err != nil || !strings.Contains(out, "Usage:") || !strings.Contains(out, "Do NOT") {
			t.Fatalf("%v --help: %v\n%s", args, err, out)
		}
		cmd, _, ferr := RootCmd().Find(args)
		if ferr != nil || isNovelScaffoldCommand(cmd) {
			t.Fatalf("%v is still a scaffold: %v", args, ferr)
		}
	}
}

func TestStoreJSONShapes(t *testing.T) {
	// The journal row JSON keeps before/after as raw JSON values.
	b, _ := json.Marshal(store.JournalRow{ID: 1, Before: json.RawMessage("false"), After: json.RawMessage("true")})
	if !strings.Contains(string(b), `"before":false`) {
		t.Fatalf("journal row JSON: %s", b)
	}
}

func TestIdempotentDoesNotHideAmbiguousName(t *testing.T) {
	f := withFakeLive(t)
	f.mu.Lock()
	f.tracks[1].name = "Drums 2" // "Drum" now matches "Drums" and "Drums 2"
	f.mu.Unlock()
	f.resetTraffic()
	_, err := runCLI(t, "track", "mute", "Drum", "--on", "true", "--idempotent", "--json")
	if exitCode(err) != 5 || !strings.Contains(err.Error(), "use the number instead") {
		t.Fatalf("ambiguous name under --idempotent must still fail with exit 5: exit %d %v", exitCode(err), err)
	}
	requireNoWrites(t, f, "ambiguous mute")
}

func TestJournalRevertSeek(t *testing.T) {
	withFakeLive(t)
	if out, err := runCLI(t, "song", "seek", "--to", "9.1", "--json"); err != nil {
		t.Fatalf("seek: %v\n%s", err, out)
	}
	var res revertResult
	runJSON(t, &res, "journal", "revert", "--last", "1")
	if len(res.Reverted) != 1 || res.Reverted[0].Path != "/song/seek" || res.Reverted[0].Method != "POST" {
		t.Fatalf("seek revert: %+v", res)
	}
}

func TestNotesRestoreRefusesPrefixMatchedTrack(t *testing.T) {
	f := withFakeLive(t)
	var res noteWriteResult
	runJSON(t, &res, "notes", "replace", "Bass", "1", "--notes", `[{"pitch":45,"start":0,"duration":0.5}]`)
	f.mu.Lock()
	f.tracks[1].name = "Bass Synth" // the original "Bass" is gone
	f.mu.Unlock()
	f.resetTraffic()
	if _, err := runCLI(t, "notes", "restore", "1"); exitCode(err) != 3 {
		t.Fatalf("restore onto a prefix match must exit 3: exit %d %v", exitCode(err), err)
	}
	requireNoWrites(t, f, "restore onto prefix match")
	// An explicit --track is honoured.
	runJSON(t, &res, "notes", "restore", "1", "--track", "Bass Synth")
	if res.Track != "Bass Synth" || res.NotesAfter != 2 {
		t.Fatalf("explicit --track restore: %+v", res)
	}
}

func TestNameKeysAreUnique(t *testing.T) {
	got := nameKeys([]string{"Bass", "Bass #2", "Bass", "Bass", "Pad"})
	want := []string{"Bass", "Bass #2", "Bass #3", "Bass #4", "Pad"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("nameKeys = %q, want %q", got, want)
	}
	if got := nameKeys([]string{"A", "A", "B"}); strings.Join(got, "|") != "A|A #2|B" {
		t.Fatalf("plain repeats: %q", got)
	}
}

// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written (slice C): notes replace / notes restore / notes backups.
// Bridges: AbletonOSC only (through the routed /clips and /notes endpoints).

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ableton-pp-cli/internal/live"
	"ableton-pp-cli/internal/store"
)

type notesReplaceOpts struct {
	file  string
	notes string
	stdin bool
}

// parseNotesInput accepts a bare array of notes, {"notes": [...]}, or the
// CLI's own JSON envelopes ({"results": [...]} / {"data": [...]}).
func parseNotesInput(data []byte) ([]live.Note, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("the notes JSON is empty (use [] to clear the clip)")
	}
	var raw any
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil, fmt.Errorf("the notes are not valid JSON: %w", err)
	}
	if obj, ok := raw.(map[string]any); ok {
		found := false
		for _, k := range []string{"notes", "results", "data"} {
			if v, ok := obj[k]; ok {
				raw, found = v, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("expected a JSON array of notes (or an object with a \"notes\" array)")
		}
	}
	if _, ok := raw.([]any); !ok {
		return nil, fmt.Errorf("expected a JSON array of notes, got %T", raw)
	}
	notes, err := live.ParseNotes(raw)
	if err != nil {
		return nil, err
	}
	seen := map[string]int{}
	for i, n := range notes {
		key := fmt.Sprintf("%d@%g", n.Pitch, n.Start)
		if j, dup := seen[key]; dup {
			return nil, fmt.Errorf("notes %d and %d have the same pitch %d and start %g; Live would merge them, so remove one", j+1, i+1, n.Pitch, n.Start)
		}
		seen[key] = i
	}
	live.SortNotes(notes)
	return notes, nil
}

func notesMatch(want, got []live.Note) bool {
	if len(want) != len(got) {
		return false
	}
	w := append([]live.Note(nil), want...)
	g := append([]live.Note(nil), got...)
	live.SortNotes(w)
	live.SortNotes(g)
	for i := range w {
		a, b := w[i], g[i]
		if a.Pitch != b.Pitch || a.Velocity != b.Velocity || a.Mute != b.Mute || !nearlyEqual(a.Start, b.Start) || !nearlyEqual(a.Duration, b.Duration) {
			return false
		}
	}
	return true
}

type noteWriteResult struct {
	Action       string `json:"action"`
	Target       string `json:"target"`
	Track        string `json:"track"`
	TrackNumber  int    `json:"track_number"`
	Slot         int    `json:"slot"`
	Clip         string `json:"clip"`
	BackupID     int64  `json:"backup_id"`
	RestoredFrom *int64 `json:"restored_from,omitempty"`
	NotesBefore  int    `json:"notes_before"`
	NotesWritten int    `json:"notes_written"`
	NotesAfter   int    `json:"notes_after"`
	Verified     bool   `json:"verified"`
}

type clipInfo struct {
	Track       string `json:"track"`
	TrackNumber int    `json:"track_number"`
	Slot        int    `json:"slot"`
	Name        string `json:"name"`
	Type        string `json:"type"`
}

// writeClipNotes backs up a clip's notes, clears it, writes notes and reads
// the clip back. Any failure after the backup names the backup to restore.
func writeClipNotes(api *liveAPI, db *store.Store, trackIn, slotIn, exactTrack string, notes []live.Note, reason string, restoredFrom *int64) (noteWriteResult, error) {
	var res noteWriteResult
	// Validate before anything is backed up or cleared, so a bad note can
	// never leave the clip empty.
	for i, n := range notes {
		if err := live.ValidateNote(n); err != nil {
			return res, usageErr(fmt.Errorf("note %d: %w; nothing was changed", i+1, err))
		}
	}
	var clip clipInfo
	if err := api.get(livePath("/clips/{track}/{slot}", "track", trackIn, "slot", slotIn), nil, &clip); err != nil {
		return res, err
	}
	// A restore aimed at the backup's own track must land on exactly that
	// track, not on a prefix or substring match such as "Bass Synth".
	if exactTrack != "" && clip.Track != exactTrack {
		return res, notFoundErr(fmt.Errorf("track %q is no longer in the set (the closest match is track %d %q); pass --track to choose where to restore", exactTrack, clip.TrackNumber, clip.Track))
	}
	if clip.Type != "midi" {
		return res, usageErr(fmt.Errorf("clip %d on track %d %q is an audio clip; notes only exist in MIDI clips", clip.Slot, clip.TrackNumber, clip.Track))
	}
	// From here on address the clip by number so every step hits the same clip.
	tn, sn := itoa(clip.TrackNumber), itoa(clip.Slot)
	notesPath := livePath("/notes/{track}/{slot}", "track", tn, "slot", sn)
	var current []live.Note
	if err := api.get(notesPath, nil, &current); err != nil {
		return res, err
	}
	backup, err := json.Marshal(current)
	if err != nil {
		return res, err
	}
	id, err := store.InsertNoteBackup(api.ctx, db.DB(), time.Now(), clip.Track, clip.TrackNumber, clip.Slot, clip.Name, reason, backup)
	if err != nil {
		return res, configErr(fmt.Errorf("could not back up the clip's notes, so nothing was changed: %w", err))
	}
	res = noteWriteResult{
		Action: "replace", Target: fmt.Sprintf("clip %d on track %d %q", clip.Slot, clip.TrackNumber, clip.Track),
		Track: clip.Track, TrackNumber: clip.TrackNumber, Slot: clip.Slot, Clip: clip.Name,
		BackupID: id, RestoredFrom: restoredFrom, NotesBefore: len(current), NotesWritten: len(notes),
	}
	if restoredFrom != nil {
		res.Action = "restore"
	}
	hint := func(err error) error {
		return fmt.Errorf("%w (the clip's previous %d notes are saved as backup %d; put them back with 'ableton-pp-cli notes restore %d')", err, len(current), id, id)
	}
	if err := api.del(notesPath, nil, nil); err != nil {
		return res, hint(err)
	}
	if len(notes) > 0 {
		if err := api.post(notesPath, map[string]any{"notes": notes}, nil); err != nil {
			return res, hint(err)
		}
	}
	var after []live.Note
	if err := api.get(notesPath, nil, &after); err != nil {
		return res, hint(err)
	}
	res.NotesAfter = len(after)
	res.Verified = notesMatch(notes, after)
	if !res.Verified {
		return res, apiErr(hint(fmt.Errorf("read-back check failed: the clip has %d notes that do not match the %d notes sent", len(after), len(notes))))
	}
	return res, nil
}

func emitNoteWrite(cmd *cobra.Command, flags *rootFlags, res noteWriteResult) error {
	return emit(cmd, flags, res, func(w io.Writer) {
		verb := "Replaced"
		if res.RestoredFrom != nil {
			verb = fmt.Sprintf("Restored backup %d to", *res.RestoredFrom)
		}
		fmt.Fprintf(w, "%s %s (%q): %d notes -> %d notes, verified by read-back.\n", verb, res.Target, res.Clip, res.NotesBefore, res.NotesAfter)
		fmt.Fprintf(w, "Previous notes saved as backup %d (undo with: ableton-pp-cli notes restore %d)\n", res.BackupID, res.BackupID)
	})
}

func runNotesReplace(cmd *cobra.Command, flags *rootFlags, args []string, opts notesReplaceOpts) error {
	if planOnly(flags, args, 2) {
		return writeDryRun(cmd.OutOrStdout(), flags, "notes replace")
	}
	if len(args) != 2 {
		return usageErr(fmt.Errorf("give the track (name or number) and the clip slot, e.g. 'notes replace Bass 1 --file bassline.json'"))
	}
	sources := 0
	for _, set := range []bool{opts.file != "", opts.notes != "", opts.stdin} {
		if set {
			sources++
		}
	}
	if sources != 1 {
		return usageErr(fmt.Errorf("give the new notes with exactly one of --file, --notes or --stdin"))
	}
	var data []byte
	var err error
	switch {
	case opts.file != "":
		data, err = os.ReadFile(opts.file)
		if err != nil {
			return usageErr(fmt.Errorf("reading --file: %w", err))
		}
	case opts.notes != "":
		data = []byte(opts.notes)
	default:
		data, err = io.ReadAll(io.LimitReader(cmd.InOrStdin(), 32<<20))
		if err != nil {
			return usageErr(fmt.Errorf("reading stdin: %w", err))
		}
	}
	notes, err := parseNotesInput(data)
	if err != nil {
		return usageErr(err)
	}
	track, slot := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	if dryRunOK(flags) {
		plan := map[string]any{
			"dry_run": true, "success": true, "action": "notes replace",
			"track": track, "slot": slot, "notes_to_write": len(notes),
			"steps": []string{
				"read the clip's current notes",
				"save them as a new note backup in the local store",
				"clear the clip",
				fmt.Sprintf("write %d notes", len(notes)),
				"read the clip back and compare",
			},
			"note": "nothing was sent to Live",
		}
		return emit(cmd, flags, plan, func(w io.Writer) {
			fmt.Fprintf(w, "dry-run: would back up clip %s on track %q, clear it and write %d notes; nothing was sent to Live\n", slot, track, len(notes))
		})
	}
	db, err := openAbletonStore(cmd.Context())
	if err != nil {
		return err
	}
	defer db.Close()
	api, err := newLiveAPI(cmd, flags)
	if err != nil {
		return err
	}
	res, err := writeClipNotes(api, db, track, slot, "", notes, "notes replace", nil)
	if err != nil {
		return liveErr(cmd, flags, err)
	}
	return emitNoteWrite(cmd, flags, res)
}

// ---------- notes restore ----------

func newNotesRestoreCmd(flags *rootFlags) *cobra.Command {
	var flagTrack, flagSlot string
	cmd := &cobra.Command{
		Use:   "restore <backup-id>",
		Short: "Put a saved note backup back into its clip (the clip's current notes are backed up first)",
		Long: "Use this command to put back a clip's notes from a backup made by 'notes replace' or an\n" +
			"earlier restore. Do NOT use it to add or delete a few notes; use 'notes add' / 'notes clear'\n" +
			"instead. Do NOT use it to undo Live's own edits; use 'undo' instead.\n\n" +
			"The backup goes back to the track it came from, found by name (so it still works\n" +
			"after tracks move), and the same slot; --track and --slot send it elsewhere. The\n" +
			"clip's current notes are saved as a new backup first, so a restore can itself be\n" +
			"restored. --dry-run prints the plan without contacting Live.",
		Example: "  ableton-pp-cli notes backups\n" +
			"  ableton-pp-cli notes restore 12\n" +
			"  ableton-pp-cli notes restore 12 --track \"Bass 2\" --slot 3 --json",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if planOnly(flags, args, 1) {
				return writeDryRun(cmd.OutOrStdout(), flags, "notes restore")
			}
			if len(args) != 1 {
				return usageErr(fmt.Errorf("give the backup id to restore (see 'notes backups')"))
			}
			id, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(args[0]), "#"), 10, 64)
			if err != nil || id <= 0 {
				return usageErr(fmt.Errorf("backup id must be a positive number, got %q", args[0]))
			}
			db, err := openAbletonStore(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			b, err := store.GetNoteBackup(cmd.Context(), db.DB(), id)
			if errors.Is(err, store.ErrNotFound) {
				return notFoundErr(fmt.Errorf("note backup %d not found; list backups with 'ableton-pp-cli notes backups'", id))
			}
			if err != nil {
				return configErr(err)
			}
			var notes []live.Note
			if err := json.Unmarshal(b.Notes, &notes); err != nil {
				return configErr(fmt.Errorf("note backup %d is unreadable: %w", id, err))
			}
			track, slot := b.TrackName, itoa(b.Slot)
			if strings.TrimSpace(flagTrack) != "" {
				track = strings.TrimSpace(flagTrack)
			}
			if strings.TrimSpace(flagSlot) != "" {
				slot = strings.TrimSpace(flagSlot)
			}
			if dryRunOK(flags) {
				plan := map[string]any{
					"dry_run": true, "success": true, "action": "notes restore", "backup_id": id,
					"backup": map[string]any{"ts": b.TS, "track": b.TrackName, "slot": b.Slot, "clip": b.ClipName, "note_count": len(notes)},
					"track":  track, "slot": slot, "notes_to_write": len(notes),
					"note": "nothing was sent to Live",
				}
				return emit(cmd, flags, plan, func(w io.Writer) {
					fmt.Fprintf(w, "dry-run: would back up clip %s on track %q and write the %d notes of backup %d; nothing was sent to Live\n", slot, track, len(notes), id)
				})
			}
			api, err := newLiveAPI(cmd, flags)
			if err != nil {
				return err
			}
			exact := ""
			if strings.TrimSpace(flagTrack) == "" {
				exact = b.TrackName
			}
			res, err := writeClipNotes(api, db, track, slot, exact, notes, fmt.Sprintf("before restore of backup %d", id), &id)
			if err != nil {
				return liveErr(cmd, flags, err)
			}
			return emitNoteWrite(cmd, flags, res)
		},
	}
	cmd.Flags().StringVar(&flagTrack, "track", "", "Restore into this track (name or number) instead of the backup's own track")
	cmd.Flags().StringVar(&flagSlot, "slot", "", "Restore into this clip slot instead of the backup's own slot")
	return cmd
}

// ---------- notes backups ----------

func newNotesBackupsCmd(flags *rootFlags) *cobra.Command {
	var flagTrack string
	var flagSlot, flagLimit int
	cmd := &cobra.Command{
		Use:   "backups",
		Short: "List the note backups that 'notes replace' and 'notes restore' saved",
		Long: "Use this command to find a note backup to restore. Do NOT use it to read a clip's\n" +
			"current notes; use 'notes get' instead.\n\n" +
			"Reads only the local store; Live is not contacted.",
		Example:     "  ableton-pp-cli notes backups\n  ableton-pp-cli notes backups --track Bass --json",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.dryRun {
				return writeDryRun(cmd.OutOrStdout(), flags, "notes backups")
			}
			db, err := openAbletonStore(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			rows, err := store.ListNoteBackups(cmd.Context(), db.DB(), flagTrack, flagSlot, flagLimit)
			if err != nil {
				return configErr(err)
			}
			return emit(cmd, flags, rows, func(w io.Writer) {
				if len(rows) == 0 {
					fmt.Fprintln(w, "No note backups yet. 'notes replace' saves one before every overwrite.")
					return
				}
				tw := newTabWriter(w)
				fmt.Fprintln(tw, "ID\tSAVED\tTRACK\tSLOT\tCLIP\tNOTES\tREASON")
				for _, r := range rows {
					fmt.Fprintf(tw, "%d\t%s\t%q\t%d\t%q\t%d\t%s\n", r.ID, r.TS, r.TrackName, r.Slot, r.ClipName, r.NoteCount, r.Reason)
				}
				_ = tw.Flush()
			})
		},
	}
	cmd.Flags().StringVar(&flagTrack, "track", "", "Only backups from tracks whose name contains this text")
	cmd.Flags().IntVar(&flagSlot, "slot", 0, "Only backups from this clip slot")
	cmd.Flags().IntVar(&flagLimit, "limit", 50, "Maximum number of backups to list")
	return cmd
}

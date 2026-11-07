// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written (slice C): snapshot save / snapshot list / snapshot diff, and
// the bare `snapshot` command, which now lists stored snapshots instead of
// reading the whole project.
// Bridges: AbletonMCP get_session_snapshot (through GET /snapshot), sent only
// by 'snapshot save' and 'snapshot diff --live'; the router refuses the route
// for any other caller (client.WithSessionSnapshot).

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ableton-pp-cli/internal/client"
	"ableton-pp-cli/internal/store"
)

func init() {
	registerNovelCommand(rewireBareSnapshot)
}

// rewireBareSnapshot turns the promoted `snapshot` command (generated as a
// direct get_session_snapshot read) into a listing of stored snapshots, so
// the whole-project read only ever happens from 'snapshot save' and
// 'snapshot diff --live'. Done from a hook so the generated file stays
// untouched.
func rewireBareSnapshot(root *cobra.Command, flags *rootFlags) {
	var snap *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "snapshot" {
			snap = c
			break
		}
	}
	if snap == nil {
		return
	}
	snap.Short = "List stored project snapshots; save, list and compare them with the subcommands"
	snap.Long = "Use this command to see the project snapshots stored with 'snapshot save'. Do NOT use it\n" +
		"to read single values from Live; use 'tracks', 'device list', 'param list' or 'notes get'\n" +
		"instead.\n\n" +
		"A snapshot is the whole project's state (tracks, mixer, devices, parameters, clips,\n" +
		"notes, scenes, locators) read through AbletonMCP. It is only read by\n" +
		"'snapshot save <label>' (stores it) and 'snapshot diff <label> --live' (compares\n" +
		"it with a stored one without storing). Bare 'snapshot' is the same as 'snapshot list'."
	snap.Example = "  ableton-pp-cli snapshot\n  ableton-pp-cli snapshot save before-edit\n  ableton-pp-cli snapshot diff before-edit --live"
	snap.Args = cobra.NoArgs
	snap.Annotations = map[string]string{"mcp:read-only": "true", "pp:data-source": "local"}
	for _, name := range []string{"notes", "params"} {
		if f := snap.Flags().Lookup(name); f != nil {
			f.Hidden = true
			f.Usage = "Ignored; use 'snapshot save --" + name + "'"
		}
	}
	snap.RunE = func(cmd *cobra.Command, args []string) error {
		return runSnapshotList(cmd, flags)
	}
}

type snapshotDiffOpts struct {
	live bool
}

// readLiveSnapshot is the only place that asks for get_session_snapshot.
func readLiveSnapshot(cmd *cobra.Command, flags *rootFlags, notes, params bool) (json.RawMessage, error) {
	api, err := newLiveAPI(cmd, flags)
	if err != nil {
		return nil, err
	}
	q := map[string]string{"notes": strconv.FormatBool(notes), "params": strconv.FormatBool(params)}
	raw, err := api.c.Get(client.WithSessionSnapshot(api.ctx), "/snapshot", q)
	if err != nil {
		return nil, err
	}
	var probe map[string]any
	if json.Unmarshal(raw, &probe) != nil || probe["tracks"] == nil {
		return nil, apiErr(fmt.Errorf("AbletonMCP returned an unexpected snapshot (no tracks)"))
	}
	return raw, nil
}

func newSnapshotSaveCmd(flags *rootFlags) *cobra.Command {
	var flagForce, flagNotes, flagParams bool
	cmd := &cobra.Command{
		Use:   "save <label>",
		Short: "Read the whole project now and store it as a named snapshot for 'snapshot diff'",
		Long: "Use this command to keep a named copy of the whole project's state so 'snapshot diff'\n" +
			"can show what changed later, including manual edits. Do NOT use it to keep only the\n" +
			"mixer; use 'mix save' instead. Do NOT use it to back up one clip's notes; 'notes\n" +
			"replace' does that automatically.\n\n" +
			"Reads tracks, mixer, devices, parameters, Session and Arrangement clips, notes,\n" +
			"scenes, return tracks, master and locators through AbletonMCP and stores them\n" +
			"in the local store. An existing label is kept unless --force. --dry-run sends\n" +
			"nothing to Live.",
		Example:     "  ableton-pp-cli snapshot save before-edit\n  ableton-pp-cli snapshot save mix-v2 --notes=false --force --json",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				if len(args) != 1 {
					return writeDryRun(cmd.OutOrStdout(), flags, "snapshot save")
				}
				plan := map[string]any{"dry_run": true, "success": true, "action": "snapshot save", "label": strings.TrimSpace(args[0]),
					"would": "read the whole project through AbletonMCP and store it in the local store", "note": "nothing was sent to Live"}
				return emit(cmd, flags, plan, func(w io.Writer) {
					fmt.Fprintf(w, "dry-run: would read the whole project and store it as snapshot %q; nothing was sent to Live\n", strings.TrimSpace(args[0]))
				})
			}
			if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
				return usageErr(fmt.Errorf("give the snapshot a label, e.g. 'snapshot save before-edit'"))
			}
			label := strings.TrimSpace(args[0])
			if len(label) > 200 {
				return usageErr(fmt.Errorf("snapshot labels are limited to 200 characters"))
			}
			db, err := openAbletonStore(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			if !flagForce {
				if _, err := store.GetSnapshot(cmd.Context(), db.DB(), label); err == nil {
					return apiErr(fmt.Errorf("a snapshot named %q already exists; pass --force to replace it or pick another label", label))
				}
			}
			raw, err := readLiveSnapshot(cmd, flags, flagNotes, flagParams)
			if err != nil {
				return liveErr(cmd, flags, err)
			}
			id, existed, err := store.SaveSnapshot(cmd.Context(), db.DB(), label, time.Now(), raw, flagForce)
			if err != nil {
				return configErr(err)
			}
			sum := summarizeSnapshot(raw)
			out := map[string]any{"id": id, "label": label, "bytes": len(raw), "replaced": existed, "summary": sum}
			return emit(cmd, flags, out, func(w io.Writer) {
				fmt.Fprintf(w, "Saved snapshot %q (#%d, %d KB): %d tracks, %d devices, %d clips, %d notes.\n", label, id, (len(raw)+1023)/1024, sum["tracks"], sum["devices"], sum["clips"], sum["notes"])
			})
		},
	}
	cmd.Flags().BoolVar(&flagForce, "force", false, "Replace an existing snapshot with the same label")
	cmd.Flags().BoolVar(&flagNotes, "notes", true, "Include MIDI notes (needed for note-level diffs)")
	cmd.Flags().BoolVar(&flagParams, "params", true, "Include device parameter values")
	return cmd
}

func summarizeSnapshot(raw []byte) map[string]int {
	var s struct {
		Tracks []struct {
			Devices   []json.RawMessage `json:"devices"`
			ClipSlots []struct {
				HasClip bool `json:"has_clip"`
				Clip    *struct {
					NoteCount int `json:"note_count"`
				} `json:"clip"`
			} `json:"clip_slots"`
			Arr []struct {
				NoteCount int `json:"note_count"`
			} `json:"arrangement_clips"`
		} `json:"tracks"`
	}
	_ = json.Unmarshal(raw, &s)
	out := map[string]int{"tracks": len(s.Tracks)}
	for _, t := range s.Tracks {
		out["devices"] += len(t.Devices)
		for _, c := range t.ClipSlots {
			if c.HasClip && c.Clip != nil {
				out["clips"]++
				out["notes"] += c.Clip.NoteCount
			}
		}
		for _, c := range t.Arr {
			out["clips"]++
			out["notes"] += c.NoteCount
		}
	}
	return out
}

func newSnapshotListCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored project snapshots",
		Long: "Use this command to find a snapshot label for 'snapshot diff'. Do NOT use it to read\n" +
			"the project now; use 'snapshot save' or 'snapshot diff <label> --live' instead.\n\n" +
			"Reads only the local store; Live is not contacted.",
		Example:     "  ableton-pp-cli snapshot list --json",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshotList(cmd, flags)
		},
	}
}

func runSnapshotList(cmd *cobra.Command, flags *rootFlags) error {
	if flags.dryRun {
		return writeDryRun(cmd.OutOrStdout(), flags, "snapshot list")
	}
	db, err := openAbletonStore(cmd.Context())
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := store.ListSnapshots(cmd.Context(), db.DB())
	if err != nil {
		return configErr(err)
	}
	return emit(cmd, flags, rows, func(w io.Writer) {
		if len(rows) == 0 {
			fmt.Fprintln(w, "No stored snapshots yet. Store one with 'ableton-pp-cli snapshot save <label>'.")
			return
		}
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "ID\tLABEL\tSAVED\tSIZE")
		for _, r := range rows {
			fmt.Fprintf(tw, "%d\t%q\t%s\t%d KB\n", r.ID, r.Label, localTime(r.TS), (r.Bytes+1023)/1024)
		}
		_ = tw.Flush()
	})
}

// ---------- diff ----------

type snapSide struct {
	Label string `json:"label"`
	ID    int64  `json:"id,omitempty"`
	TS    string `json:"ts"`
}

type snapChange struct {
	Category string `json:"category"`
	Kind     string `json:"kind"` // added, removed, changed
	Path     string `json:"path"`
	Before   any    `json:"before,omitempty"`
	After    any    `json:"after,omitempty"`
}

type snapDiff struct {
	From    snapSide       `json:"from"`
	To      snapSide       `json:"to"`
	Total   int            `json:"total"`
	Summary map[string]int `json:"summary"`
	Changes []snapChange   `json:"changes"`
}

var snapCategories = []string{"song", "tracks", "mixer", "devices", "parameters", "clips", "notes", "scenes", "locators"}

// flatSnap is a snapshot flattened to name-keyed paths ("track \"Bass\" / volume").
type flatSnap struct {
	keys   []string
	vals   map[string]any
	segs   map[string][]string
	prefix map[string]bool
}

func newFlatSnap() *flatSnap {
	return &flatSnap{vals: map[string]any{}, segs: map[string][]string{}, prefix: map[string]bool{}}
}

func (f *flatSnap) put(path []string, v any) {
	if v == nil {
		return
	}
	key := strings.Join(path, " / ")
	if _, ok := f.vals[key]; !ok {
		f.keys = append(f.keys, key)
	}
	f.vals[key] = v
	f.segs[key] = append([]string(nil), path...)
	for i := 1; i < len(path); i++ {
		f.prefix[strings.Join(path[:i], " / ")] = true
	}
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any         { l, _ := v.([]any); return l }
func asStr(v any) string         { s, _ := v.(string); return s }

func path2(base []string, more ...string) []string {
	out := make([]string, 0, len(base)+len(more))
	out = append(out, base...)
	return append(out, more...)
}

func notesHash(notes []any) string {
	type n struct {
		p, v int
		s, d float64
		m    bool
	}
	list := make([]n, 0, len(notes))
	for _, raw := range notes {
		m := asMap(raw)
		p, _ := m["pitch"].(float64)
		v, _ := m["velocity"].(float64)
		s, _ := m["start_time"].(float64)
		d, _ := m["duration"].(float64)
		mu, _ := m["mute"].(bool)
		list = append(list, n{int(p), int(math.Round(v)), math.Round(s*1e4) / 1e4, math.Round(d*1e4) / 1e4, mu})
	}
	sort.Slice(list, func(a, b int) bool {
		if list[a].s != list[b].s {
			return list[a].s < list[b].s
		}
		return list[a].p < list[b].p
	})
	h := fnv.New64a()
	for _, x := range list {
		fmt.Fprintf(h, "%d|%g|%g|%d|%v;", x.p, x.s, x.d, x.v, x.m)
	}
	return fmt.Sprintf("%016x", h.Sum64())
}

func flattenClip(f *flatSnap, base []string, clip map[string]any) {
	for _, k := range []string{"name", "length", "looping", "loop_start", "loop_end", "warping", "warp_mode", "gain", "pitch_coarse", "pitch_fine", "launch_mode", "color", "start_time", "end_time", "file_path", "warp_marker_count"} {
		f.put(path2(base, k), clip[k])
	}
	if nc, ok := clip["note_count"]; ok {
		f.put(path2(base, "note_count"), nc)
	}
	if notes, ok := clip["notes"].([]any); ok {
		f.put(path2(base, "notes"), notesHash(notes))
	}
}

func flattenDevices(f *flatSnap, base []string, devices []any) {
	names := make([]string, len(devices))
	for i, d := range devices {
		names[i] = asStr(asMap(d)["name"])
	}
	keys := nameKeys(names)
	for i, d := range devices {
		dm := asMap(d)
		db := path2(base, fmt.Sprintf("device %q", keys[i]))
		f.put(path2(db, "position"), float64(i+1))
		f.put(path2(db, "class_name"), dm["class_name"])
		params := asList(dm["parameters"])
		pnames := make([]string, len(params))
		for j, p := range params {
			pnames[j] = asStr(asMap(p)["name"])
		}
		pkeys := nameKeys(pnames)
		for j, p := range params {
			f.put(path2(db, fmt.Sprintf("param %q", pkeys[j]), "value"), asMap(p)["value"])
		}
		chains := asList(dm["chains"])
		cnames := make([]string, len(chains))
		for j, c := range chains {
			cnames[j] = asStr(asMap(c)["chain_name"])
		}
		ckeys := nameKeys(cnames)
		for j, c := range chains {
			cm := asMap(c)
			cb := path2(db, fmt.Sprintf("chain %q", ckeys[j]))
			for _, k := range []string{"mute", "solo", "volume", "panning"} {
				f.put(path2(cb, k), cm[k])
			}
			flattenDevices(f, cb, asList(cm["devices"]))
		}
	}
}

func flattenMixer(f *flatSnap, base []string, t map[string]any) {
	for _, k := range []string{"mute", "solo", "arm", "volume", "panning"} {
		f.put(path2(base, k), t[k])
	}
	for _, s := range asList(t["sends"]) {
		sm := asMap(s)
		idx, ok := sm["index"].(float64)
		if !ok {
			continue
		}
		f.put(path2(base, "send "+sendLetter(int(idx))), sm["value"])
	}
}

func sendLetter(i int) string {
	if i >= 0 && i < 26 {
		return string(rune('A' + i))
	}
	return itoa(i + 1)
}

// flattenSnapshot turns an ableton_mcp_snapshot_v2 document into name-keyed
// scalar paths. Volatile fields (play state, playhead, triggers) are left out.
func flattenSnapshot(raw []byte) (*flatSnap, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("snapshot is not valid JSON: %w", err)
	}
	f := newFlatSnap()
	sess := asMap(doc["session"])
	for _, k := range []string{"tempo", "signature_numerator", "signature_denominator", "loop", "loop_start", "loop_length"} {
		f.put([]string{"song", k}, sess[k])
	}
	tracks := asList(doc["tracks"])
	names := make([]string, len(tracks))
	for i, t := range tracks {
		names[i] = asStr(asMap(t)["name"])
	}
	keys := nameKeys(names)
	for i, t := range tracks {
		tm := asMap(t)
		base := []string{fmt.Sprintf("track %q", keys[i])}
		f.put(path2(base, "position"), float64(i+1))
		kind := "audio"
		if b, _ := tm["is_midi_track"].(bool); b {
			kind = "midi"
		} else if b, _ := tm["is_audio_track"].(bool); !b {
			kind = "group"
		}
		f.put(path2(base, "type"), kind)
		flattenMixer(f, base, tm)
		flattenDevices(f, base, asList(tm["devices"]))
		for _, s := range asList(tm["clip_slots"]) {
			sm := asMap(s)
			if has, _ := sm["has_clip"].(bool); !has || sm["clip"] == nil {
				continue
			}
			idx, _ := sm["index"].(float64)
			flattenClip(f, path2(base, fmt.Sprintf("slot %d", int(idx)+1)), asMap(sm["clip"]))
		}
		for _, c := range asList(tm["arrangement_clips"]) {
			cm := asMap(c)
			st, _ := cm["start_time"].(float64)
			flattenClip(f, path2(base, fmt.Sprintf("arrangement clip @%g", st)), cm)
		}
	}
	for _, s := range asList(doc["scenes"]) {
		sm := asMap(s)
		idx, _ := sm["index"].(float64)
		base := []string{fmt.Sprintf("scene %d", int(idx)+1)}
		f.put(path2(base, "name"), sm["name"])
		f.put(path2(base, "tempo"), sm["tempo"])
	}
	rets := asList(doc["return_tracks"])
	rnames := make([]string, len(rets))
	for i, r := range rets {
		rnames[i] = asStr(asMap(r)["name"])
	}
	rkeys := nameKeys(rnames)
	for i, r := range rets {
		rm := asMap(r)
		base := []string{fmt.Sprintf("return %q", rkeys[i])}
		flattenMixer(f, base, rm)
		flattenDevices(f, base, asList(rm["devices"]))
	}
	if m := asMap(doc["master_track"]); m != nil {
		base := []string{"master"}
		flattenMixer(f, base, m)
		flattenDevices(f, base, asList(m["devices"]))
	}
	cues := asList(doc["cue_points"])
	cnames := make([]string, len(cues))
	for i, c := range cues {
		cnames[i] = asStr(asMap(c)["name"])
	}
	ckeys := nameKeys(cnames)
	for i, c := range cues {
		f.put([]string{fmt.Sprintf("locator %q", ckeys[i]), "time"}, asMap(c)["time"])
	}
	return f, nil
}

func segCategory(seg string) string {
	switch {
	case strings.HasPrefix(seg, "param "):
		return "parameters"
	case strings.HasPrefix(seg, "device ") || strings.HasPrefix(seg, "chain "):
		return "devices"
	case strings.HasPrefix(seg, "slot ") || strings.HasPrefix(seg, "arrangement clip"):
		return "clips"
	case strings.HasPrefix(seg, "scene "):
		return "scenes"
	case strings.HasPrefix(seg, "locator "):
		return "locators"
	case seg == "song":
		return "song"
	}
	return "tracks" // track, return, master
}

// changeCategory classifies a field-level change by its path.
func changeCategory(segs []string) string {
	field := segs[len(segs)-1]
	entity := segs[len(segs)-2]
	switch {
	case field == "note_count" || field == "notes":
		return "notes"
	case strings.HasPrefix(entity, "param "):
		return "parameters"
	case strings.HasPrefix(entity, "slot ") || strings.HasPrefix(entity, "arrangement clip"):
		return "clips"
	case strings.HasPrefix(entity, "device ") || (strings.HasPrefix(entity, "chain ") && (field == "position" || field == "class_name")):
		return "devices"
	case field == "mute" || field == "solo" || field == "arm" || field == "volume" || field == "panning" || strings.HasPrefix(field, "send "):
		return "mixer"
	}
	return segCategory(entity)
}

func valuesEqual(a, b any) bool {
	x, okA := a.(float64)
	y, okB := b.(float64)
	if okA && okB {
		return math.Abs(x-y) <= 1e-6*math.Max(1, math.Max(math.Abs(x), math.Abs(y)))
	}
	return jsonEqual(a, b)
}

// diffSnapshots compares two flattened snapshots. Whole objects that exist
// on one side only are reported once (e.g. one "added track"), not field by
// field.
func diffSnapshots(from, to *flatSnap) []snapChange {
	var out []snapChange
	reported := map[string]bool{}
	entityChange := func(segs []string, other *flatSnap, kind string) bool {
		for i := 1; i < len(segs); i++ {
			p := strings.Join(segs[:i], " / ")
			if !other.prefix[p] {
				if !reported[kind+p] {
					reported[kind+p] = true
					out = append(out, snapChange{Category: segCategory(segs[i-1]), Kind: kind, Path: p})
				}
				return true
			}
		}
		return false
	}
	changedPrefix := map[string]bool{}
	for _, k := range from.keys {
		segs := from.segs[k]
		bv, inTo := to.vals[k]
		if !inTo {
			if !entityChange(segs, to, "removed") {
				out = append(out, snapChange{Category: changeCategory(segs), Kind: "removed", Path: k, Before: from.vals[k]})
			}
			continue
		}
		if !valuesEqual(from.vals[k], bv) {
			if segs[len(segs)-1] == "note_count" {
				changedPrefix[strings.Join(segs[:len(segs)-1], " / ")] = true
			}
			out = append(out, snapChange{Category: changeCategory(segs), Kind: "changed", Path: k, Before: from.vals[k], After: bv})
		}
	}
	for _, k := range to.keys {
		if _, inFrom := from.vals[k]; inFrom {
			continue
		}
		segs := to.segs[k]
		if !entityChange(segs, from, "added") {
			out = append(out, snapChange{Category: changeCategory(segs), Kind: "added", Path: k, After: to.vals[k]})
		}
	}
	// A changed note count already says the notes changed; drop the hash line.
	filtered := out[:0]
	for _, c := range out {
		if c.Kind == "changed" && strings.HasSuffix(c.Path, " / notes") && changedPrefix[strings.TrimSuffix(c.Path, " / notes")] {
			continue
		}
		if c.Kind == "changed" && strings.HasSuffix(c.Path, " / notes") {
			c.Before, c.After = nil, "notes edited (same note count)"
		}
		filtered = append(filtered, c)
	}
	sort.SliceStable(filtered, func(a, b int) bool {
		return catIndex(filtered[a].Category) < catIndex(filtered[b].Category)
	})
	return filtered
}

func catIndex(c string) int {
	for i, x := range snapCategories {
		if x == c {
			return i
		}
	}
	return len(snapCategories)
}

func buildSnapDiff(fromRaw, toRaw []byte, from, to snapSide) (*snapDiff, error) {
	ff, err := flattenSnapshot(fromRaw)
	if err != nil {
		return nil, err
	}
	tf, err := flattenSnapshot(toRaw)
	if err != nil {
		return nil, err
	}
	changes := diffSnapshots(ff, tf)
	d := &snapDiff{From: from, To: to, Total: len(changes), Summary: map[string]int{}, Changes: changes}
	if d.Changes == nil {
		d.Changes = []snapChange{}
	}
	for _, c := range changes {
		d.Summary[c.Category]++
	}
	return d, nil
}

func printSnapDiff(w io.Writer, d *snapDiff) {
	side := func(s snapSide) string {
		if s.ID == 0 {
			return fmt.Sprintf("%s (%s)", s.Label, localTime(s.TS))
		}
		return fmt.Sprintf("%q (#%d, %s)", s.Label, s.ID, localTime(s.TS))
	}
	fmt.Fprintf(w, "Snapshot diff: %s -> %s\n", side(d.From), side(d.To))
	if d.Total == 0 {
		fmt.Fprintln(w, "No differences.")
		return
	}
	parts := []string{}
	for _, c := range snapCategories {
		if n := d.Summary[c]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, c))
		}
	}
	fmt.Fprintf(w, "%d changes: %s\n", d.Total, strings.Join(parts, ", "))
	cur := ""
	for _, c := range d.Changes {
		if c.Category != cur {
			cur = c.Category
			fmt.Fprintf(w, "\n%s\n", strings.ToUpper(cur[:1])+cur[1:])
		}
		switch c.Kind {
		case "added":
			if c.After != nil {
				fmt.Fprintf(w, "  + %s = %s\n", strconv.QuoteToASCII(c.Path), shortValue(c.After))
			} else {
				fmt.Fprintf(w, "  + %s\n", strconv.QuoteToASCII(c.Path))
			}
		case "removed":
			if c.Before != nil {
				fmt.Fprintf(w, "  - %s (was %s)\n", strconv.QuoteToASCII(c.Path), shortValue(c.Before))
			} else {
				fmt.Fprintf(w, "  - %s\n", strconv.QuoteToASCII(c.Path))
			}
		default:
			if c.Before == nil {
				fmt.Fprintf(w, "  ~ %s: %s\n", strconv.QuoteToASCII(c.Path), shortValue(c.After))
			} else {
				fmt.Fprintf(w, "  ~ %s: %s -> %s\n", strconv.QuoteToASCII(c.Path), shortValue(c.Before), shortValue(c.After))
			}
		}
	}
}

func getStoredSnapshot(cmd *cobra.Command, db *store.Store, ref string) (store.SnapshotRow, error) {
	row, err := store.GetSnapshot(cmd.Context(), db.DB(), ref)
	if errors.Is(err, store.ErrNotFound) {
		labels := []string{}
		if all, lerr := store.ListSnapshots(cmd.Context(), db.DB()); lerr == nil {
			for _, r := range all {
				labels = append(labels, r.Label)
			}
		}
		msg := fmt.Sprintf("no stored snapshot named %q", ref)
		if len(labels) > 0 {
			msg += "; stored snapshots: " + joinQuoted(labels)
		} else {
			msg += "; store one with 'ableton-pp-cli snapshot save <label>'"
		}
		return row, notFoundErr(errors.New(msg))
	}
	if err != nil {
		return row, configErr(err)
	}
	return row, nil
}

func runSnapshotDiff(cmd *cobra.Command, flags *rootFlags, args []string, opts snapshotDiffOpts) error {
	need := 2
	if opts.live {
		need = 1
	}
	if planOnly(flags, args, need) {
		return writeDryRun(cmd.OutOrStdout(), flags, "snapshot diff")
	}
	if len(args) != need {
		if opts.live {
			return usageErr(fmt.Errorf("with --live give one stored snapshot label, e.g. 'snapshot diff before-edit --live'"))
		}
		return usageErr(fmt.Errorf("give two stored snapshot labels, or one label and --live"))
	}
	db, err := openAbletonStore(cmd.Context())
	if err != nil {
		return err
	}
	defer db.Close()
	a, err := getStoredSnapshot(cmd, db, strings.TrimSpace(args[0]))
	if err != nil {
		return err
	}
	fromSide := snapSide{Label: a.Label, ID: a.ID, TS: a.TS}
	var toRaw []byte
	var toSide snapSide
	if opts.live {
		var meta struct {
			Notes  *bool `json:"include_notes"`
			Params *bool `json:"include_params"`
		}
		_ = json.Unmarshal(a.Snapshot, &meta)
		notes, params := true, true
		if meta.Notes != nil {
			notes = *meta.Notes
		}
		if meta.Params != nil {
			params = *meta.Params
		}
		raw, err := readLiveSnapshot(cmd, flags, notes, params)
		if err != nil {
			return liveErr(cmd, flags, err)
		}
		toRaw, toSide = raw, snapSide{Label: "live set", TS: time.Now().UTC().Format(time.RFC3339Nano)}
	} else {
		b, err := getStoredSnapshot(cmd, db, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		toRaw, toSide = b.Snapshot, snapSide{Label: b.Label, ID: b.ID, TS: b.TS}
	}
	d, err := buildSnapDiff(a.Snapshot, toRaw, fromSide, toSide)
	if err != nil {
		return configErr(err)
	}
	return emit(cmd, flags, d, func(w io.Writer) { printSnapDiff(w, d) })
}

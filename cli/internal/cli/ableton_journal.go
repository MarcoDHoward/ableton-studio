// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written (slice C): journal list / journal revert.
// Bridges: none for list (local store). Revert reads the current value and
// re-applies the row's before value through the same routed setter that
// made the change (AbletonOSC for most properties; AbletonPPExtras for
// volume_db). Live's own undo history is never used.

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ableton-pp-cli/internal/cliutil"
	"ableton-pp-cli/internal/store"
)

type journalListOpts struct {
	since   string
	command string
	target  string
	limit   int
}

func parseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if d, err := cliutil.ParseDurationLoose(s); err == nil {
		if d < 0 {
			d = -d
		}
		return now.Add(-d), nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("--since must be a duration (30m, 2h, 7d) or a date/time (2026-10-31, 2026-10-31T18:00:00Z), got %q", s)
}

func runJournalList(cmd *cobra.Command, flags *rootFlags, args []string, opts journalListOpts) error {
	if len(args) > 0 {
		return usageErr(fmt.Errorf("journal list takes no arguments; filter with --since, --command, --target and --limit"))
	}
	since, err := parseSince(opts.since, time.Now())
	if err != nil {
		return usageErr(err)
	}
	if opts.limit < 0 {
		return usageErr(fmt.Errorf("--limit must be 0 or more"))
	}
	db, err := openAbletonStore(cmd.Context())
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := store.QueryJournal(cmd.Context(), db.DB(), store.JournalFilter{Since: since, Command: opts.command, Target: opts.target, Limit: opts.limit})
	if err != nil {
		return configErr(err)
	}
	return emit(cmd, flags, rows, func(w io.Writer) {
		if len(rows) == 0 {
			fmt.Fprintln(w, "No journal rows match. Every change this CLI makes in Live is recorded here.")
			return
		}
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "ID\tTIME\tCOMMAND\tTARGET\tPROPERTY\tBEFORE\tAFTER\tREVERTED BY")
		for _, r := range rows {
			rev := ""
			if r.RevertedBy != nil {
				rev = fmt.Sprintf("#%d", *r.RevertedBy)
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%q\t%s\t%s\t%s\n", r.ID, localTime(r.TS), r.Command, strconv.QuoteToASCII(r.Target), r.Property, string(r.Before), string(r.After), rev)
		}
		_ = tw.Flush()
	})
}

func localTime(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ts
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// ---------- revert ----------

var (
	quotedRE  = `"(?:[^"\\]|\\.)*"`
	reTrack   = regexp.MustCompile(`^track (\d+) (` + quotedRE + `)$`)
	reDevice  = regexp.MustCompile(`^track (\d+) (` + quotedRE + `) / device (\d+) (` + quotedRE + `)$`)
	reClip    = regexp.MustCompile(`^clip (\d+) on track (\d+) (` + quotedRE + `)$`)
	reScene   = regexp.MustCompile(`^scene (\d+) (` + quotedRE + `)$`)
	reLocator = regexp.MustCompile(`^locator (\d+) at `)
)

func unq(s string) string {
	if v, err := strconv.Unquote(s); err == nil {
		return v
	}
	return s
}

// revertStep is how one journal row is rolled back: where its current value
// is read, and which routed setter puts the before value back.
type revertStep struct {
	Row       store.JournalRow `json:"-"`
	ID        int64            `json:"id"`
	Target    string           `json:"target"`
	Property  string           `json:"property"`
	Current   any              `json:"current"`
	Expected  any              `json:"expected"`
	SetTo     any              `json:"set_to"`
	Method    string           `json:"method"`
	Path      string           `json:"path"`
	Body      map[string]any   `json:"body"`
	Conflict  bool             `json:"conflict"`
	JournalID *int64           `json:"journal_id,omitempty"`
	stateKey  string
}

type revertError struct{ msg string }

func (e *revertError) Error() string { return e.msg }

func cannotRevert(row store.JournalRow, why string) error {
	return usageErr(&revertError{msg: fmt.Sprintf("journal row %d (%s %s) cannot be reverted automatically: %s", row.ID, row.Target, row.Property, why)})
}

// locateTrack finds the journaled track again: the recorded number if it
// still has the recorded name, otherwise the one track with that name.
func locateTrack(rows []liveTrack, number int, name string) (liveTrack, error) {
	if number >= 1 && number <= len(rows) && rows[number-1].Name == name {
		return rows[number-1], nil
	}
	var hits []liveTrack
	for _, r := range rows {
		if r.Name == name {
			hits = append(hits, r)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return liveTrack{}, notFoundErr(fmt.Errorf("track %q (track %d when the change was made) is no longer in the set", name, number))
	}
	nums := make([]string, len(hits))
	for i, h := range hits {
		nums[i] = itoa(h.Number)
	}
	return liveTrack{}, apiErr(fmt.Errorf("%d tracks are named %q now (numbers %s) and none is at its old position %d; set the value by hand with the track commands", len(hits), name, strings.Join(nums, ", "), number))
}

func locateDevice(devs []liveDevice, number int, name string) (liveDevice, error) {
	if number >= 1 && number <= len(devs) && devs[number-1].Name == name {
		return devs[number-1], nil
	}
	var hits []liveDevice
	for _, d := range devs {
		if d.Name == name {
			hits = append(hits, d)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	if len(hits) == 0 {
		return liveDevice{}, notFoundErr(fmt.Errorf("device %q (device %d when the change was made) is no longer on the track", name, number))
	}
	return liveDevice{}, apiErr(fmt.Errorf("%d devices on the track are named %q and none is at its old position %d", len(hits), name, number))
}

// setText renders a before value for the string-valued setters (song,
// clip and scene properties).
func setText(v any, position bool) (string, bool) {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x), true
	case float64:
		if position {
			return beatsArg(x), true
		}
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case string:
		return x, true
	}
	return "", false
}

// revertPlanner caches reads across the rows of one revert run.
type revertPlanner struct {
	api    *liveAPI
	tracks []liveTrack
}

func (p *revertPlanner) trackRows() ([]liveTrack, error) {
	if p.tracks == nil {
		rows, err := p.api.tracks()
		if err != nil {
			return nil, err
		}
		p.tracks = rows
	}
	return p.tracks, nil
}

func trackField(tr liveTrack, prop string) (any, bool) {
	switch prop {
	case "mute":
		return tr.Mute, true
	case "solo":
		return tr.Solo, true
	case "arm":
		if tr.Arm == nil {
			return nil, true
		}
		return *tr.Arm, true
	case "pan":
		if tr.Pan == nil {
			return nil, true
		}
		return *tr.Pan, true
	case "volume":
		if tr.Volume == nil {
			return nil, true
		}
		return *tr.Volume, true
	case "volume_db":
		if tr.VolumeDB == nil {
			return nil, true
		}
		return *tr.VolumeDB, true
	case "name":
		return tr.Name, true
	}
	if strings.HasPrefix(prop, "send ") {
		v, ok := tr.Sends[strings.TrimPrefix(prop, "send ")]
		if !ok {
			return nil, true
		}
		return v, true
	}
	return nil, false
}

// plan works out how to roll one journal row back and reads the target's
// current value.
func (p *revertPlanner) plan(row store.JournalRow) (*revertStep, error) {
	var before, after any
	if err := json.Unmarshal(row.Before, &before); err != nil || before == nil {
		return nil, cannotRevert(row, "the value before the change was not recorded")
	}
	_ = json.Unmarshal(row.After, &after)
	step := &revertStep{Row: row, ID: row.ID, Target: row.Target, Property: row.Property, Expected: after, SetTo: before, Method: "PUT"}
	path := row.Path
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	segs := strings.Split(strings.Trim(path, "/"), "/")
	isSeek := row.Method == "POST" && path == "/song/seek"
	if (row.Method != "PUT" && !isSeek) || len(segs) == 0 {
		return nil, cannotRevert(row, "only property changes can be reverted")
	}
	prop := row.Property
	switch {
	case segs[0] == "tracks" && len(segs) == 3 && segs[2] != "routing":
		m := reTrack.FindStringSubmatch(row.Target)
		if m == nil {
			return nil, cannotRevert(row, "the target is not a track")
		}
		num, _ := strconv.Atoi(m[1])
		name := unq(m[2])
		if prop == "name" {
			s, ok := after.(string)
			if !ok {
				return nil, cannotRevert(row, "the new name was not recorded")
			}
			name = s
		}
		rows, err := p.trackRows()
		if err != nil {
			return nil, err
		}
		tr, err := locateTrack(rows, num, name)
		if err != nil {
			return nil, err
		}
		cur, ok := trackField(tr, prop)
		if !ok {
			return nil, cannotRevert(row, "the current value of this property cannot be read back")
		}
		step.Current = cur
		tn := itoa(tr.Number)
		switch prop {
		case "mute", "solo", "arm":
			step.Path, step.Body = livePath("/tracks/{track}/"+prop, "track", tn), map[string]any{"on": before}
		case "pan":
			step.Path, step.Body = livePath("/tracks/{track}/pan", "track", tn), map[string]any{"value": before}
		case "volume":
			step.Path, step.Body = livePath("/tracks/{track}/volume", "track", tn), map[string]any{"fader": before}
		case "volume_db":
			step.Path, step.Body = livePath("/tracks/{track}/volume", "track", tn), map[string]any{"db": before}
		case "name":
			step.Path, step.Body = livePath("/tracks/{track}/name", "track", tn), map[string]any{"name": before}
		default:
			return nil, cannotRevert(row, "unsupported track property")
		}
		step.stateKey = "track:" + tn + ":" + prop
	case segs[0] == "tracks" && len(segs) == 4 && segs[2] == "sends":
		m := reTrack.FindStringSubmatch(row.Target)
		if m == nil || !strings.HasPrefix(prop, "send ") {
			return nil, cannotRevert(row, "the target is not a track send")
		}
		num, _ := strconv.Atoi(m[1])
		rows, err := p.trackRows()
		if err != nil {
			return nil, err
		}
		tr, err := locateTrack(rows, num, unq(m[2]))
		if err != nil {
			return nil, err
		}
		step.Current, _ = trackField(tr, prop)
		letter := strings.TrimPrefix(prop, "send ")
		tn := itoa(tr.Number)
		step.Path, step.Body = livePath("/tracks/{track}/sends/{send}", "track", tn, "send", letter), map[string]any{"value": before}
		step.stateKey = "track:" + tn + ":" + prop
	case segs[0] == "tracks" && len(segs) == 3 && segs[2] == "routing":
		m := reTrack.FindStringSubmatch(row.Target)
		if m == nil {
			return nil, cannotRevert(row, "the target is not a track")
		}
		num, _ := strconv.Atoi(m[1])
		rows, err := p.trackRows()
		if err != nil {
			return nil, err
		}
		tr, err := locateTrack(rows, num, unq(m[2]))
		if err != nil {
			return nil, err
		}
		tn := itoa(tr.Number)
		var routing map[string]any
		if err := p.api.get(livePath("/tracks/{track}/routing", "track", tn), nil, &routing); err != nil {
			return nil, err
		}
		cur, ok := routing[prop]
		if !ok {
			return nil, cannotRevert(row, "unknown routing field")
		}
		step.Current = cur
		step.Path, step.Body = livePath("/tracks/{track}/routing", "track", tn), map[string]any{prop: before}
		step.stateKey = "track:" + tn + ":" + prop
	case segs[0] == "tracks" && len(segs) >= 5 && segs[2] == "devices":
		m := reDevice.FindStringSubmatch(row.Target)
		if m == nil {
			return nil, cannotRevert(row, "the target is not a device")
		}
		tnum, _ := strconv.Atoi(m[1])
		dnum, _ := strconv.Atoi(m[3])
		rows, err := p.trackRows()
		if err != nil {
			return nil, err
		}
		tr, err := locateTrack(rows, tnum, unq(m[2]))
		if err != nil {
			return nil, err
		}
		tn := itoa(tr.Number)
		devs, err := p.api.devices(tn)
		if err != nil {
			return nil, err
		}
		dev, err := locateDevice(devs, dnum, unq(m[4]))
		if err != nil {
			return nil, err
		}
		dn := itoa(dev.Number)
		switch {
		case len(segs) == 5 && (segs[4] == "enable" || segs[4] == "disable") && prop == "on":
			b, ok := before.(bool)
			if !ok {
				return nil, cannotRevert(row, "the device state before the change was not recorded")
			}
			if dev.On != nil {
				step.Current = *dev.On
			}
			verb := "disable"
			if b {
				verb = "enable"
			}
			step.Path, step.Body = livePath("/tracks/{track}/devices/{device}/"+verb, "track", tn, "device", dn), map[string]any{}
		case len(segs) == 6 && segs[4] == "params":
			params, err := p.api.params(tn, dn)
			if err != nil {
				return nil, err
			}
			var hits []liveParam
			for _, pr := range params {
				if pr.Name == prop {
					hits = append(hits, pr)
				}
			}
			if len(hits) == 0 {
				return nil, notFoundErr(fmt.Errorf("parameter %q is no longer on device %q", prop, dev.Name))
			}
			if len(hits) > 1 {
				return nil, cannotRevert(row, fmt.Sprintf("the device has %d parameters named %q", len(hits), prop))
			}
			step.Current = hits[0].Value
			step.Path = livePath("/tracks/{track}/devices/{device}/params/{param}", "track", tn, "device", dn, "param", itoa(hits[0].Number))
			step.Body = map[string]any{"value": before}
		default:
			return nil, cannotRevert(row, "unsupported device change")
		}
		step.stateKey = "device:" + tn + ":" + dn + ":" + prop
	case segs[0] == "song" && len(segs) == 3 && segs[1] == "props", segs[0] == "song" && len(segs) == 2 && segs[1] == "seek", segs[0] == "record" && len(segs) == 2 && segs[1] == "session":
		songProp := prop
		var out struct {
			Value any `json:"value"`
		}
		if err := p.api.get(livePath("/song/props/{prop}", "prop", songProp), nil, &out); err != nil {
			return nil, err
		}
		step.Current = out.Value
		position := songProp == "loop_start" || songProp == "current_song_time"
		switch {
		case segs[1] == "seek":
			f, ok := before.(float64)
			if !ok {
				return nil, cannotRevert(row, "the position before the change was not recorded")
			}
			step.Path, step.Body, step.Method = "/song/seek", map[string]any{"to": beatsArg(f)}, "POST"
		case segs[0] == "record":
			step.Path, step.Body = "/record/session", map[string]any{"on": before}
		default:
			text, ok := setText(before, position)
			if !ok {
				return nil, cannotRevert(row, "the value before the change has an unexpected type")
			}
			step.Path, step.Body = livePath("/song/props/{prop}", "prop", songProp), map[string]any{"value": text}
		}
		step.stateKey = "song:" + songProp
	case segs[0] == "clips" && len(segs) == 5 && segs[3] == "props":
		m := reClip.FindStringSubmatch(row.Target)
		if m == nil {
			return nil, cannotRevert(row, "the target is not a clip")
		}
		slot := m[1]
		tnum, _ := strconv.Atoi(m[2])
		rows, err := p.trackRows()
		if err != nil {
			return nil, err
		}
		tr, err := locateTrack(rows, tnum, unq(m[3]))
		if err != nil {
			return nil, err
		}
		tn := itoa(tr.Number)
		var clip map[string]any
		if err := p.api.get(livePath("/clips/{track}/{slot}", "track", tn, "slot", slot), nil, &clip); err != nil {
			return nil, err
		}
		cur, ok := clip[prop]
		if !ok {
			return nil, cannotRevert(row, "this clip property cannot be read back")
		}
		step.Current = cur
		text, ok := setText(before, false)
		if !ok {
			return nil, cannotRevert(row, "the value before the change has an unexpected type")
		}
		step.Path, step.Body = livePath("/clips/{track}/{slot}/props/{prop}", "track", tn, "slot", slot, "prop", prop), map[string]any{"value": text}
		step.stateKey = "clip:" + tn + ":" + slot + ":" + prop
	case segs[0] == "scenes" && len(segs) == 4 && segs[2] == "props":
		m := reScene.FindStringSubmatch(row.Target)
		if m == nil {
			return nil, cannotRevert(row, "the target is not a scene")
		}
		num, _ := strconv.Atoi(m[1])
		name := unq(m[2])
		if prop == "name" {
			if s, ok := after.(string); ok {
				name = s
			}
		}
		var scenes []map[string]any
		if err := p.api.get("/scenes", nil, &scenes); err != nil {
			return nil, err
		}
		idx := -1
		if num >= 1 && num <= len(scenes) && scenes[num-1]["name"] == name {
			idx = num - 1
		} else {
			for i, s := range scenes {
				if s["name"] == name {
					if idx >= 0 {
						return nil, apiErr(fmt.Errorf("several scenes are named %q now; set the value by hand with 'scene set'", name))
					}
					idx = i
				}
			}
		}
		if idx < 0 {
			return nil, notFoundErr(fmt.Errorf("scene %q (scene %d when the change was made) is no longer in the set", name, num))
		}
		cur, ok := scenes[idx][prop]
		if !ok {
			return nil, cannotRevert(row, "this scene property cannot be read back")
		}
		step.Current = cur
		text, ok := setText(before, false)
		if !ok {
			return nil, cannotRevert(row, "the value before the change has an unexpected type")
		}
		sn := itoa(idx + 1)
		step.Path, step.Body = livePath("/scenes/{scene}/props/{prop}", "scene", sn, "prop", prop), map[string]any{"value": text}
		if f, isNum := before.(float64); isNum && prop == "tempo" && f < 0 {
			// Live reports -1 for a scene without its own tempo; going back to
			// that state means switching the scene tempo off.
			step.Path, step.Body = livePath("/scenes/{scene}/props/{prop}", "scene", sn, "prop", "tempo_enabled"), map[string]any{"value": "false"}
		}
		step.stateKey = "scene:" + sn + ":" + prop
	case segs[0] == "locators" && len(segs) == 2 && prop == "name":
		m := reLocator.FindStringSubmatch(row.Target)
		if m == nil {
			return nil, cannotRevert(row, "the target is not a locator")
		}
		num, _ := strconv.Atoi(m[1])
		var locs []locatorRow
		if err := p.api.get("/locators", nil, &locs); err != nil {
			return nil, err
		}
		if num < 1 || num > len(locs) {
			return nil, notFoundErr(fmt.Errorf("locator %d is no longer in the set", num))
		}
		step.Current = locs[num-1].Name
		step.Path, step.Body = livePath("/locators/{locator}", "locator", itoa(num)), map[string]any{"name": before}
		step.stateKey = "locator:" + itoa(num)
	default:
		return nil, cannotRevert(row, "this kind of change has no automatic revert")
	}
	return step, nil
}

func responseJournalID(raw map[string]any) *int64 {
	pick := func(v any) *int64 {
		if f, ok := v.(float64); ok && f > 0 {
			id := int64(f)
			return &id
		}
		return nil
	}
	if id := pick(raw["journal_id"]); id != nil {
		return id
	}
	if ids, ok := raw["journal_ids"].([]any); ok {
		for _, v := range ids {
			if id := pick(v); id != nil {
				return id
			}
		}
	}
	return nil
}

type revertResult struct {
	DryRun   bool          `json:"dry_run,omitempty"`
	Success  bool          `json:"success"`
	Reverted []*revertStep `json:"reverted"`
	Planned  []*revertStep `json:"planned,omitempty"`
}

func newJournalRevertCmd(flags *rootFlags) *cobra.Command {
	var flagForce bool
	var flagLast int
	cmd := &cobra.Command{
		Use:   "revert [id...]",
		Short: "Roll back specific changes this CLI made, by journal id, without touching Live's undo history",
		Long: journalLong + "\n\n" +
			"Each row's 'before' value is set again through the same command path that made\n" +
			"the change, and the revert is journaled too (the original row's reverted_by\n" +
			"points at it). Several ids are reverted newest first. A row is refused when its\n" +
			"target's current value no longer equals the value the change left (it was\n" +
			"changed again since, by hand or by another command); --force sets it back\n" +
			"anyway. Rows that were already reverted are refused unless --force. --dry-run\n" +
			"reads the current values and shows what would be set without changing Live.\n\n" +
			"--last N picks the N newest rows that are not reverts and were not reverted\n" +
			"yet, instead of ids.",
		Example: "  ableton-pp-cli journal list --since 1h\n" +
			"  ableton-pp-cli journal revert 42\n" +
			"  ableton-pp-cli journal revert 42 41 40 --dry-run --json\n" +
			"  ableton-pp-cli journal revert --last 1 --dry-run",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagLast < 0 {
				return usageErr(fmt.Errorf("--last must be a positive number"))
			}
			if flagLast > 0 && len(args) > 0 {
				return usageErr(fmt.Errorf("give journal ids or --last, not both"))
			}
			need := 1
			if flagLast > 0 {
				need = 0
			}
			if planOnly(flags, args, need) {
				return writeDryRun(cmd.OutOrStdout(), flags, "journal revert")
			}
			if len(args) == 0 && flagLast == 0 {
				return usageErr(fmt.Errorf("give the journal id(s) to revert, or --last N (see 'journal list')"))
			}
			var ids []int64
			seen := map[int64]bool{}
			for _, a := range args {
				id, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(a), "#"), 10, 64)
				if err != nil || id <= 0 {
					return usageErr(fmt.Errorf("journal ids are positive numbers, got %q", a))
				}
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
			sort.Slice(ids, func(a, b int) bool { return ids[a] > ids[b] })
			db, err := openAbletonStore(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			if flagLast > 0 {
				latest, err := store.LatestUnrevertedJournal(cmd.Context(), db.DB(), flagLast)
				if err != nil {
					return configErr(err)
				}
				if len(latest) == 0 {
					return notFoundErr(fmt.Errorf("no unreverted changes in the journal; see 'ableton-pp-cli journal list'"))
				}
				for _, r := range latest {
					ids = append(ids, r.ID)
				}
			}
			var rows []store.JournalRow
			for _, id := range ids {
				row, err := store.GetJournal(cmd.Context(), db.DB(), id)
				if errors.Is(err, store.ErrNotFound) {
					return notFoundErr(fmt.Errorf("journal row %d not found; see 'ableton-pp-cli journal list'", id))
				}
				if err != nil {
					return configErr(err)
				}
				if row.RevertedBy != nil && !flagForce {
					return apiErr(fmt.Errorf("journal row %d was already reverted by row %d; pass --force to set its before value again", id, *row.RevertedBy))
				}
				rows = append(rows, row)
			}
			api, err := newLiveAPI(cmd, flags)
			if err != nil {
				return err
			}
			planner := &revertPlanner{api: api}
			res := revertResult{Success: true, Reverted: []*revertStep{}}
			virtual := map[string]any{} // dry run: values earlier steps would have set
			for _, row := range rows {
				step, err := planner.plan(row)
				if err != nil {
					return liveErr(cmd, flags, err)
				}
				if v, ok := virtual[step.stateKey]; ok && dryRunOK(flags) {
					step.Current = v
				}
				step.Conflict = !jsonEqual(step.Current, step.Expected)
				if step.Conflict && !flagForce {
					if dryRunOK(flags) {
						res.Planned = append(res.Planned, step)
						res.Success = false
						continue
					}
					return apiErr(fmt.Errorf("journal row %d: %s %s is now %s, not %s as that change left it (it changed again since); nothing was set. Pass --force to set it back to %s anyway%s",
						row.ID, row.Target, row.Property, shortValue(step.Current), shortValue(step.Expected), shortValue(step.SetTo), revertedSoFar(res)))
				}
				if dryRunOK(flags) {
					virtual[step.stateKey] = step.SetTo
					res.Planned = append(res.Planned, step)
					continue
				}
				var out map[string]any
				send := api.put
				if step.Method == "POST" {
					send = api.post
				}
				if err := send(step.Path, step.Body, &out); err != nil {
					return liveErr(cmd, flags, fmt.Errorf("%w%s", err, revertedSoFar(res)))
				}
				planner.tracks = nil // values changed; re-read for the next row
				step.JournalID = responseJournalID(out)
				if step.JournalID != nil {
					if err := store.MarkJournalReverted(cmd.Context(), db.DB(), row.ID, *step.JournalID); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: row %d was reverted in Live but could not be linked to journal row %d: %v\n", row.ID, *step.JournalID, err)
					}
				} else {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: row %d was reverted in Live but the revert was not journaled\n", row.ID)
				}
				res.Reverted = append(res.Reverted, step)
			}
			if dryRunOK(flags) {
				res.DryRun = true
				if err := emit(cmd, flags, res, func(w io.Writer) { printRevert(w, res) }); err != nil {
					return err
				}
				if !res.Success {
					return apiErr(fmt.Errorf("some rows conflict with the current values in Live; pass --force to revert them anyway"))
				}
				return nil
			}
			return emit(cmd, flags, res, func(w io.Writer) { printRevert(w, res) })
		},
	}
	cmd.Flags().BoolVar(&flagForce, "force", false, "Revert even when the value changed again since, or the row was already reverted")
	cmd.Flags().IntVar(&flagLast, "last", 0, "Revert the N newest changes that are not reverts and were not reverted yet, instead of giving ids")
	return cmd
}

func revertedSoFar(res revertResult) string {
	if len(res.Reverted) == 0 {
		return ""
	}
	ids := make([]string, len(res.Reverted))
	for i, s := range res.Reverted {
		ids[i] = itoa(int(s.ID))
	}
	return fmt.Sprintf(" (rows already reverted in this run: %s)", strings.Join(ids, ", "))
}

func printRevert(w io.Writer, res revertResult) {
	steps := res.Reverted
	if res.DryRun {
		steps = res.Planned
		fmt.Fprintln(w, "dry-run: nothing was changed in Live.")
	}
	for _, s := range steps {
		switch {
		case res.DryRun && s.Conflict:
			fmt.Fprintf(w, "  #%d %s %s: CONFLICT, now %s but the change left %s (needs --force)\n", s.ID, strconv.QuoteToASCII(s.Target), s.Property, shortValue(s.Current), shortValue(s.Expected))
		case res.DryRun:
			fmt.Fprintf(w, "  #%d %s %s: would set %s -> %s\n", s.ID, strconv.QuoteToASCII(s.Target), s.Property, shortValue(s.Current), shortValue(s.SetTo))
		default:
			jid := ""
			if s.JournalID != nil {
				jid = fmt.Sprintf(" (journal #%d)", *s.JournalID)
			}
			fmt.Fprintf(w, "Reverted #%d %s %s: %s -> %s%s\n", s.ID, strconv.QuoteToASCII(s.Target), s.Property, shortValue(s.Current), shortValue(s.SetTo), jid)
		}
	}
}

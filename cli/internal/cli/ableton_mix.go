// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written (slice C): mix save / mix recall / mix list.
// Bridges: AbletonOSC (GET /tracks for the mixer; PUT volume --fader, pan,
// mute, solo and sends for recall, each journaled by the router). AbletonMCP
// get_session_info is used by the router to count return tracks.

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ableton-pp-cli/internal/store"
)

// mixTrackState is one track's saved mixer state.
type mixTrackState struct {
	Key      string             `json:"key"`
	Name     string             `json:"name"`
	Number   int                `json:"number"`
	Volume   *float64           `json:"volume"`
	VolumeDB *float64           `json:"volume_db,omitempty"`
	Pan      *float64           `json:"pan"`
	Mute     bool               `json:"mute"`
	Solo     bool               `json:"solo"`
	Sends    map[string]float64 `json:"sends"`
}

func mixStatesFromTracks(rows []liveTrack) []mixTrackState {
	keys := nameKeys(trackNames(rows))
	out := make([]mixTrackState, len(rows))
	for i, r := range rows {
		sends := r.Sends
		if sends == nil {
			sends = map[string]float64{}
		}
		out[i] = mixTrackState{Key: keys[i], Name: r.Name, Number: r.Number, Volume: r.Volume, VolumeDB: r.VolumeDB, Pan: r.Pan, Mute: r.Mute, Solo: r.Solo, Sends: sends}
	}
	return out
}

func validMixName(name string) error {
	if strings.TrimSpace(name) == "" {
		return usageErr(fmt.Errorf("give the mix a name, e.g. 'mix save dub-version'"))
	}
	if len(name) > 200 {
		return usageErr(fmt.Errorf("mix names are limited to 200 characters"))
	}
	return nil
}

func runMixSave(cmd *cobra.Command, flags *rootFlags, args []string, force bool) error {
	if planOnly(flags, args, 1) {
		return writeDryRun(cmd.OutOrStdout(), flags, "mix save")
	}
	if len(args) != 1 {
		return usageErr(fmt.Errorf("give the mix a name, e.g. 'mix save dub-version'"))
	}
	name := strings.TrimSpace(args[0])
	if err := validMixName(name); err != nil {
		return err
	}
	api, err := newLiveAPI(cmd, flags)
	if err != nil {
		return err
	}
	rows, err := api.tracks()
	if err != nil {
		return liveErr(cmd, flags, err)
	}
	if len(rows) == 0 {
		return usageErr(fmt.Errorf("the set has no tracks to save"))
	}
	states := mixStatesFromTracks(rows)
	out := map[string]any{"mix": name, "tracks": len(states), "states": states}
	if dryRunOK(flags) {
		out["dry_run"] = true
		out["success"] = true
		out["would"] = fmt.Sprintf("save %d tracks as mix %q in the local store", len(states), name)
		return emit(cmd, flags, out, func(w io.Writer) {
			fmt.Fprintf(w, "dry-run: would save %d tracks as mix %q; nothing was stored\n", len(states), name)
		})
	}
	db, err := openAbletonStore(cmd.Context())
	if err != nil {
		return err
	}
	defer db.Close()
	payload := map[string][]byte{}
	for _, s := range states {
		b, err := json.Marshal(s)
		if err != nil {
			return err
		}
		payload[s.Key] = b
	}
	existed, err := store.SaveMix(cmd.Context(), db.DB(), name, time.Now(), payload, force)
	if err != nil {
		if existed && !force {
			return apiErr(fmt.Errorf("a mix named %q already exists; pass --force to replace it or pick another name", name))
		}
		return configErr(err)
	}
	out["replaced"] = existed
	return emit(cmd, flags, out, func(w io.Writer) {
		verb := "Saved"
		if existed {
			verb = "Replaced"
		}
		fmt.Fprintf(w, "%s mix %q: %d tracks (volume, pan, mute, solo, sends).\n", verb, name, len(states))
	})
}

// ---------- recall ----------

type mixChange struct {
	Track       string `json:"track"`
	TrackNumber int    `json:"track_number"`
	Property    string `json:"property"`
	From        any    `json:"from"`
	To          any    `json:"to"`
	path        string
	body        map[string]any
}

type mixRecallResult struct {
	Mix       string       `json:"mix"`
	DryRun    bool         `json:"dry_run,omitempty"`
	Success   bool         `json:"success"`
	Changes   []mixChange  `json:"changes"`
	Applied   []propChange `json:"applied"`
	Unchanged int          `json:"unchanged"`
	Missing   []string     `json:"missing"`
	Extra     []string     `json:"extra"`
	Notes     []string     `json:"notes,omitempty"`
}

// planMixRecall compares a saved mix with the live mixer by track name.
func planMixRecall(saved []mixTrackState, rows []liveTrack) mixRecallResult {
	res := mixRecallResult{Success: true, Changes: []mixChange{}, Applied: []propChange{}, Missing: []string{}, Extra: []string{}}
	liveStates := mixStatesFromTracks(rows)
	byKey := map[string]mixTrackState{}
	for _, s := range liveStates {
		byKey[s.Key] = s
	}
	savedKeys := map[string]bool{}
	for _, s := range saved {
		savedKeys[s.Key] = true
		cur, ok := byKey[s.Key]
		if !ok {
			res.Missing = append(res.Missing, s.Key)
			continue
		}
		tn := itoa(cur.Number)
		add := func(prop, sub string, from, to any, body map[string]any) {
			res.Changes = append(res.Changes, mixChange{Track: cur.Name, TrackNumber: cur.Number, Property: prop, From: from, To: to, path: livePath("/tracks/{track}/"+sub, "track", tn), body: body})
		}
		if s.Volume != nil && cur.Volume != nil {
			if nearlyEqual(*s.Volume, *cur.Volume) {
				res.Unchanged++
			} else {
				add("volume", "volume", *cur.Volume, *s.Volume, map[string]any{"fader": *s.Volume})
			}
		}
		if s.Pan != nil && cur.Pan != nil {
			if nearlyEqual(*s.Pan, *cur.Pan) {
				res.Unchanged++
			} else {
				add("pan", "pan", *cur.Pan, *s.Pan, map[string]any{"value": *s.Pan})
			}
		}
		if s.Mute == cur.Mute {
			res.Unchanged++
		} else {
			add("mute", "mute", cur.Mute, s.Mute, map[string]any{"on": s.Mute})
		}
		if s.Solo == cur.Solo {
			res.Unchanged++
		} else {
			add("solo", "solo", cur.Solo, s.Solo, map[string]any{"on": s.Solo})
		}
		letters := make([]string, 0, len(s.Sends))
		for l := range s.Sends {
			letters = append(letters, l)
		}
		sort.Strings(letters)
		for _, l := range letters {
			v := s.Sends[l]
			now, ok := cur.Sends[l]
			if !ok {
				res.Notes = append(res.Notes, fmt.Sprintf("track %q: send %s no longer exists (the set has fewer return tracks)", cur.Name, l))
				continue
			}
			if nearlyEqual(v, now) {
				res.Unchanged++
				continue
			}
			res.Changes = append(res.Changes, mixChange{Track: cur.Name, TrackNumber: cur.Number, Property: "send " + l, From: now, To: v,
				path: livePath("/tracks/{track}/sends/{send}", "track", tn, "send", l), body: map[string]any{"value": v}})
		}
	}
	for _, s := range liveStates {
		if !savedKeys[s.Key] {
			res.Extra = append(res.Extra, s.Key)
		}
	}
	return res
}

func loadMixStates(cmd *cobra.Command, db *store.Store, name string) ([]mixTrackState, string, error) {
	rows, err := store.LoadMix(cmd.Context(), db.DB(), name)
	if errors.Is(err, store.ErrNotFound) {
		names := []string{}
		if all, lerr := store.ListMixes(cmd.Context(), db.DB()); lerr == nil {
			for _, m := range all {
				names = append(names, m.Name)
			}
		}
		msg := fmt.Sprintf("no saved mix named %q", name)
		if len(names) > 0 {
			msg += "; saved mixes: " + joinQuoted(names)
		} else {
			msg += "; save one with 'ableton-pp-cli mix save <name>'"
		}
		return nil, "", notFoundErr(errors.New(msg))
	}
	if err != nil {
		return nil, "", configErr(err)
	}
	out := make([]mixTrackState, 0, len(rows))
	savedAt := ""
	for _, r := range rows {
		var s mixTrackState
		if err := json.Unmarshal(r.State, &s); err != nil {
			return nil, "", configErr(fmt.Errorf("mix %q has an unreadable row for %q: %w", name, r.TrackKey, err))
		}
		if s.Key == "" {
			s.Key = r.TrackKey
		}
		out = append(out, s)
		savedAt = r.SavedAt
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Number < out[b].Number })
	return out, savedAt, nil
}

func newMixRecallCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recall <name>",
		Short: "Put a saved mix back: every track's volume, pan, mute, solo and sends, matched by track name",
		Long: "Use this command to put a mix saved with 'mix save' back onto the set. Do NOT use it\n" +
			"to change one track's mixer setting; use 'track volume' / 'track pan' / 'track mute' /\n" +
			"'track solo' / 'track send' instead. Do NOT use it to undo single CLI changes; use\n" +
			"'journal revert' instead.\n\n" +
			"Tracks are matched by name (repeated names by their order), so the recall still\n" +
			"works after tracks move. Only values that differ are sent; each change goes\n" +
			"through the same journaled setters as the track commands, so 'journal revert'\n" +
			"can roll any of them back. Saved tracks missing from the set and set tracks that\n" +
			"were not saved are reported, not touched. --dry-run reads the mixer and lists\n" +
			"the changes without sending them.",
		Example:     "  ableton-pp-cli mix recall dub-version --dry-run\n  ableton-pp-cli mix recall dub-version --json",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if planOnly(flags, args, 1) {
				return writeDryRun(cmd.OutOrStdout(), flags, "mix recall")
			}
			if len(args) != 1 {
				return usageErr(fmt.Errorf("give the name of the mix to recall (see 'mix list')"))
			}
			name := strings.TrimSpace(args[0])
			db, err := openAbletonStore(cmd.Context())
			if err != nil {
				return err
			}
			saved, _, err := loadMixStates(cmd, db, name)
			db.Close()
			if err != nil {
				return err
			}
			api, err := newLiveAPI(cmd, flags)
			if err != nil {
				return err
			}
			rows, err := api.tracks()
			if err != nil {
				return liveErr(cmd, flags, err)
			}
			res := planMixRecall(saved, rows)
			res.Mix = name
			if dryRunOK(flags) {
				res.DryRun = true
				return emit(cmd, flags, res, func(w io.Writer) { printMixRecall(w, res) })
			}
			for _, ch := range res.Changes {
				var pc propChange
				if err := api.put(ch.path, ch.body, &pc); err != nil {
					res.Success = false
					return liveErr(cmd, flags, fmt.Errorf("%w (stopped after %d of %d changes; 'journal list --command \"mix recall\"' shows what was applied)", err, len(res.Applied), len(res.Changes)))
				}
				res.Applied = append(res.Applied, pc)
			}
			return emit(cmd, flags, res, func(w io.Writer) { printMixRecall(w, res) })
		},
	}
	return cmd
}

func printMixRecall(w io.Writer, res mixRecallResult) {
	if res.DryRun {
		fmt.Fprintf(w, "dry-run: mix %q would change %d values (%d already match); nothing was sent to Live\n", res.Mix, len(res.Changes), res.Unchanged)
	} else {
		fmt.Fprintf(w, "Recalled mix %q: %d values changed, %d already matched.\n", res.Mix, len(res.Applied), res.Unchanged)
	}
	for _, ch := range res.Changes {
		fmt.Fprintf(w, "  track %d %q %s: %s -> %s\n", ch.TrackNumber, ch.Track, ch.Property, shortValue(ch.From), shortValue(ch.To))
	}
	if len(res.Missing) > 0 {
		fmt.Fprintf(w, "Saved tracks not in the set (skipped): %s\n", joinQuoted(res.Missing))
	}
	if len(res.Extra) > 0 {
		fmt.Fprintf(w, "Tracks in the set that the mix does not cover (left alone): %s\n", joinQuoted(res.Extra))
	}
	for _, n := range res.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
}

func newMixListCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [name]",
		Short: "List saved mixes, or show the tracks stored in one",
		Long: "Use this command to see which mixes 'mix save' stored, or the values inside one.\n" +
			"Do NOT use it to read the current mixer; use 'tracks' instead.\n\n" +
			"Reads only the local store; Live is not contacted.",
		Example:     "  ableton-pp-cli mix list\n  ableton-pp-cli mix list dub-version --json",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageErr(fmt.Errorf("mix list takes at most one mix name"))
			}
			if flags.dryRun {
				return writeDryRun(cmd.OutOrStdout(), flags, "mix list")
			}
			db, err := openAbletonStore(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			if len(args) == 1 {
				states, savedAt, err := loadMixStates(cmd, db, strings.TrimSpace(args[0]))
				if err != nil {
					return err
				}
				out := map[string]any{"mix": strings.TrimSpace(args[0]), "saved_at": savedAt, "tracks": states}
				return emit(cmd, flags, out, func(w io.Writer) {
					fmt.Fprintf(w, "Mix %q (saved %s):\n", strings.TrimSpace(args[0]), savedAt)
					tw := newTabWriter(w)
					fmt.Fprintln(tw, "TRACK\tVOLUME\tPAN\tMUTE\tSOLO\tSENDS")
					for _, s := range states {
						fmt.Fprintf(tw, "%q\t%s\t%s\t%v\t%v\t%s\n", s.Key, optFloat(s.Volume), optFloat(s.Pan), s.Mute, s.Solo, sendsText(s.Sends))
					}
					_ = tw.Flush()
				})
			}
			mixes, err := store.ListMixes(cmd.Context(), db.DB())
			if err != nil {
				return configErr(err)
			}
			return emit(cmd, flags, mixes, func(w io.Writer) {
				if len(mixes) == 0 {
					fmt.Fprintln(w, "No saved mixes yet. Save one with 'ableton-pp-cli mix save <name>'.")
					return
				}
				tw := newTabWriter(w)
				fmt.Fprintln(tw, "NAME\tTRACKS\tSAVED")
				for _, m := range mixes {
					fmt.Fprintf(tw, "%q\t%d\t%s\n", m.Name, m.Tracks, m.SavedAt)
				}
				_ = tw.Flush()
			})
		},
	}
	return cmd
}

func optFloat(f *float64) string {
	if f == nil {
		return "-"
	}
	return fmt.Sprintf("%.3f", *f)
}

func sendsText(s map[string]float64) string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%.3f", k, s[k])
	}
	return strings.Join(parts, " ")
}

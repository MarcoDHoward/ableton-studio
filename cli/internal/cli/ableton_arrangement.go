// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written (slice C): arrangement build.
// Bridges: AbletonOSC (signature, scenes, clip grid, locator list) and
// AbletonMCP (get_arrangement_clips for the conflict check,
// duplicate_session_clip_to_arrangement through POST
// /clips/{track}/{slot}/to-arrangement, create_locator through POST /locators).

package cli

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"ableton-pp-cli/internal/live"
)

type arrBuildOpts struct {
	plan       string
	sections   []string
	allScenes  bool
	bars       float64
	start      string
	noLocators bool
	force      bool
}

// arrSectionSpec is one requested section before it is resolved against Live.
type arrSectionSpec struct {
	Name  string
	Scene string
	Bars  float64 // 0 = the longest clip in the scene row
}

type gridClip struct {
	Track       string  `json:"track"`
	TrackNumber int     `json:"track_number"`
	Slot        int     `json:"slot"`
	Name        string  `json:"name"`
	LengthBeats float64 `json:"length_beats"`
}

type arrSection struct {
	Name        string  `json:"name"`
	Scene       string  `json:"scene"`
	SceneNumber int     `json:"scene_number"`
	StartBeat   float64 `json:"start_beat"`
	Start       string  `json:"start"`
	EndBeat     float64 `json:"end_beat"`
	End         string  `json:"end"`
	Bars        float64 `json:"bars"`
	BarsSource  string  `json:"bars_source"`
	Clips       int     `json:"clips"`
}

type arrPlacement struct {
	Section      string  `json:"section"`
	Track        string  `json:"track"`
	TrackNumber  int     `json:"track_number"`
	Slot         int     `json:"slot"`
	Clip         string  `json:"clip"`
	AtBeat       float64 `json:"at_beat"`
	At           string  `json:"at"`
	LengthBeats  float64 `json:"length_beats"`
	OverrunBeats float64 `json:"overrun_beats,omitempty"`
}

type arrLocator struct {
	Name     string  `json:"name"`
	AtBeat   float64 `json:"at_beat"`
	At       string  `json:"at"`
	Replaces string  `json:"replaces,omitempty"`
}

type arrConflict struct {
	Track       string  `json:"track"`
	TrackNumber int     `json:"track_number"`
	Clip        string  `json:"clip"`
	StartBeat   float64 `json:"start_beat"`
	EndBeat     float64 `json:"end_beat"`
}

type arrPlan struct {
	DryRun     bool           `json:"dry_run,omitempty"`
	Success    bool           `json:"success"`
	Signature  string         `json:"signature"`
	StartBeat  float64        `json:"start_beat"`
	Start      string         `json:"start"`
	EndBeat    float64        `json:"end_beat"`
	End        string         `json:"end"`
	TotalBars  float64        `json:"total_bars"`
	Sections   []arrSection   `json:"sections"`
	Placements []arrPlacement `json:"placements"`
	Locators   []arrLocator   `json:"locators"`
	Conflicts  []arrConflict  `json:"conflicts,omitempty"`
	Warnings   []string       `json:"warnings,omitempty"`
	Placed     int            `json:"placed"`
	Located    int            `json:"locators_added"`
}

const maxArrPlacements = 5000

// parseBarsValue accepts a positive number of bars.
func parseBarsValue(s, ctx string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, fmt.Errorf("%s: bars must be a positive number, got %q", ctx, s)
	}
	if v > 10000 {
		return 0, fmt.Errorf("%s: %v bars is too long", ctx, v)
	}
	return v, nil
}

// parsePlanFlag reads --plan "Intro:4,Drop:8" (bars optional per entry).
func parsePlanFlag(plan string) ([]arrSectionSpec, error) {
	var out []arrSectionSpec
	for _, part := range strings.Split(plan, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		spec := arrSectionSpec{}
		if i := strings.LastIndex(part, ":"); i >= 0 {
			bars, err := parseBarsValue(part[i+1:], fmt.Sprintf("--plan entry %q", part))
			if err != nil {
				return nil, err
			}
			spec.Bars = bars
			part = strings.TrimSpace(part[:i])
		}
		if part == "" {
			return nil, fmt.Errorf("--plan entries need a scene name, e.g. Intro:4")
		}
		spec.Name, spec.Scene = part, part
		out = append(out, spec)
	}
	return out, nil
}

// parseSectionFlag reads Name:scene=Scene:bars=N (or Name:N).
func parseSectionFlag(s string) (arrSectionSpec, error) {
	fields := strings.Split(s, ":")
	spec := arrSectionSpec{Name: strings.TrimSpace(fields[0])}
	if spec.Name == "" {
		return spec, fmt.Errorf("--section %q needs a name first, e.g. Intro:scene=Intro:bars=8", s)
	}
	for _, f := range fields[1:] {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			bars, err := parseBarsValue(f, fmt.Sprintf("--section %q", s))
			if err != nil {
				return spec, err
			}
			spec.Bars = bars
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "scene":
			spec.Scene = strings.TrimSpace(v)
		case "bars":
			bars, err := parseBarsValue(v, fmt.Sprintf("--section %q", s))
			if err != nil {
				return spec, err
			}
			spec.Bars = bars
		default:
			return spec, fmt.Errorf("--section %q: unknown key %q (use scene= and bars=)", s, k)
		}
	}
	if spec.Scene == "" {
		spec.Scene = spec.Name
	}
	return spec, nil
}

// buildArrangementPlan lays the sections out back to back. It is pure so the
// layout can be unit tested without Live.
func buildArrangementPlan(specs []arrSectionSpec, scenes []string, clips []gridClip, num, den int, startBeat, defaultBars float64) (*arrPlan, error) {
	if !live.ValidSignature(num, den) {
		num, den = 4, 4
	}
	bpb := live.BeatsPerBar(num, den)
	pos := func(b float64) string { return live.FormatPosition(b, num, den) }
	byRow := map[int][]gridClip{}
	for _, c := range clips {
		byRow[c.Slot-1] = append(byRow[c.Slot-1], c)
	}
	for k := range byRow {
		sort.SliceStable(byRow[k], func(a, b int) bool { return byRow[k][a].TrackNumber < byRow[k][b].TrackNumber })
	}
	plan := &arrPlan{Success: true, Signature: live.FormatSignature(num, den), StartBeat: startBeat, Start: pos(startBeat), Sections: []arrSection{}, Placements: []arrPlacement{}, Locators: []arrLocator{}}
	at := startBeat
	for _, spec := range specs {
		si, err := live.Resolve("scene", spec.Scene, scenes)
		if err != nil {
			return nil, err
		}
		row := byRow[si]
		sec := arrSection{Name: spec.Name, Scene: scenes[si], SceneNumber: si + 1, StartBeat: at, Start: pos(at), Clips: len(row)}
		bars := spec.Bars
		sec.BarsSource = "given"
		if bars <= 0 && defaultBars > 0 {
			bars, sec.BarsSource = defaultBars, "--bars"
		}
		if bars <= 0 {
			longest := 0.0
			for _, c := range row {
				longest = math.Max(longest, c.LengthBeats)
			}
			if longest <= 0 {
				return nil, fmt.Errorf("scene %d %q has no clips, so its length is unknown; give it a bar count (e.g. %s:4)", si+1, scenes[si], spec.Name)
			}
			bars = math.Ceil(longest/bpb - 1e-9)
			sec.BarsSource = "longest clip"
		}
		sec.Bars = bars
		length := bars * bpb
		sec.EndBeat, sec.End = at+length, pos(at+length)
		if len(row) == 0 {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("section %q: scene %d %q has no clips; the section stays silent", spec.Name, si+1, scenes[si]))
		}
		for _, c := range row {
			if c.LengthBeats <= 0 {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("section %q: clip %q on track %d has no length and is skipped", spec.Name, c.Name, c.TrackNumber))
				continue
			}
			for p := at; p < sec.EndBeat-1e-9; p += c.LengthBeats {
				pl := arrPlacement{Section: spec.Name, Track: c.Track, TrackNumber: c.TrackNumber, Slot: c.Slot, Clip: c.Name, AtBeat: p, At: pos(p), LengthBeats: c.LengthBeats}
				if over := p + c.LengthBeats - sec.EndBeat; over > 1e-9 {
					pl.OverrunBeats = over
					plan.Warnings = append(plan.Warnings, fmt.Sprintf("section %q: the last copy of %q on track %d runs %g beats past the section end (the next section's clips replace the overlap; trim the end of the song by hand if it is the last section)", spec.Name, c.Name, c.TrackNumber, over))
				}
				plan.Placements = append(plan.Placements, pl)
				if len(plan.Placements) > maxArrPlacements {
					return nil, fmt.Errorf("the layout needs more than %d clip copies; use fewer bars or longer clips", maxArrPlacements)
				}
			}
		}
		plan.Locators = append(plan.Locators, arrLocator{Name: spec.Name, AtBeat: at, At: pos(at)})
		plan.Sections = append(plan.Sections, sec)
		plan.TotalBars += bars
		at = sec.EndBeat
	}
	plan.EndBeat, plan.End = at, pos(at)
	return plan, nil
}

type locatorRow struct {
	Number int     `json:"number"`
	Name   string  `json:"name"`
	Beat   float64 `json:"beat"`
}

type arrClipRow struct {
	Name  string  `json:"name"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

func songPropInt(api *liveAPI, prop string) (int, error) {
	var out struct {
		Value any `json:"value"`
	}
	if err := api.get(livePath("/song/props/{prop}", "prop", prop), nil, &out); err != nil {
		return 0, err
	}
	f, _ := out.Value.(float64)
	return int(f), nil
}

func runArrangementBuild(cmd *cobra.Command, flags *rootFlags, args []string, opts arrBuildOpts) error {
	given := 0
	if strings.TrimSpace(opts.plan) != "" {
		given++
	}
	if len(opts.sections) > 0 {
		given++
	}
	if opts.allScenes {
		given++
	}
	need := 0
	if given == 0 {
		need = 1 // nothing to plan: a bare dry run reports the generic plan
	}
	if planOnly(flags, args, need) {
		return writeDryRun(cmd.OutOrStdout(), flags, "arrangement build")
	}
	if len(args) > 0 {
		return usageErr(fmt.Errorf("arrangement build takes no arguments; give the sections with --plan, --section or --all-scenes"))
	}
	if given != 1 {
		return usageErr(fmt.Errorf("give the sections with exactly one of --plan \"Intro:4,Drop:8\", --section Name:scene=Scene:bars=N, or --all-scenes"))
	}
	if opts.bars < 0 || math.IsNaN(opts.bars) || opts.bars > 10000 {
		return usageErr(fmt.Errorf("--bars must be a positive number"))
	}
	var specs []arrSectionSpec
	var err error
	switch {
	case strings.TrimSpace(opts.plan) != "":
		specs, err = parsePlanFlag(opts.plan)
	case len(opts.sections) > 0:
		for _, s := range opts.sections {
			spec, perr := parseSectionFlag(s)
			if perr != nil {
				err = perr
				break
			}
			specs = append(specs, spec)
		}
	}
	if err != nil {
		return usageErr(err)
	}
	api, err := newLiveAPI(cmd, flags)
	if err != nil {
		return err
	}
	num, err := songPropInt(api, "signature_numerator")
	if err != nil {
		return liveErr(cmd, flags, err)
	}
	den, err := songPropInt(api, "signature_denominator")
	if err != nil {
		return liveErr(cmd, flags, err)
	}
	if !live.ValidSignature(num, den) {
		num, den = 4, 4
	}
	startBeat, err := live.ParsePosition(opts.start, num, den)
	if err != nil {
		return usageErr(fmt.Errorf("--start: %w", err))
	}
	var sceneRows []struct {
		Number int    `json:"number"`
		Name   string `json:"name"`
	}
	if err := api.get("/scenes", nil, &sceneRows); err != nil {
		return liveErr(cmd, flags, err)
	}
	scenes := make([]string, len(sceneRows))
	for i, s := range sceneRows {
		scenes[i] = s.Name
	}
	var clips []gridClip
	if err := api.get("/clips", nil, &clips); err != nil {
		return liveErr(cmd, flags, err)
	}
	if opts.allScenes {
		rows := map[int]bool{}
		for _, c := range clips {
			rows[c.Slot] = true
		}
		for i, name := range scenes {
			if !rows[i+1] {
				continue
			}
			label := name
			if strings.TrimSpace(label) == "" {
				label = fmt.Sprintf("Scene %d", i+1)
			}
			specs = append(specs, arrSectionSpec{Name: label, Scene: itoa(i + 1)})
		}
		if len(specs) == 0 {
			return usageErr(fmt.Errorf("no scene row has clips, so there is nothing to lay out"))
		}
	}
	if len(specs) == 0 {
		return usageErr(fmt.Errorf("no sections given"))
	}
	plan, err := buildArrangementPlan(specs, scenes, clips, num, den, startBeat, opts.bars)
	if err != nil {
		var nf *live.NotFoundError
		var amb *live.AmbiguousError
		switch {
		case errors.As(err, &nf):
			return notFoundErr(err)
		case errors.As(err, &amb):
			return apiErr(err)
		}
		return usageErr(err)
	}
	// Existing Arrangement content in the song's range would be overwritten.
	tracksUsed := map[int]string{}
	// A last copy can run past the song end; its tail must be checked too.
	checkEnd := plan.EndBeat
	for _, p := range plan.Placements {
		tracksUsed[p.TrackNumber] = p.Track
		if e := p.AtBeat + p.LengthBeats; e > checkEnd {
			checkEnd = e
		}
	}
	nums := make([]int, 0, len(tracksUsed))
	for n := range tracksUsed {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		var rows []arrClipRow
		if err := api.get(livePath("/arrangement/{track}/clips", "track", itoa(n)), nil, &rows); err != nil {
			return liveErr(cmd, flags, err)
		}
		for _, r := range rows {
			if r.Start < checkEnd-1e-9 && r.End > plan.StartBeat+1e-9 {
				plan.Conflicts = append(plan.Conflicts, arrConflict{Track: tracksUsed[n], TrackNumber: n, Clip: r.Name, StartBeat: r.Start, EndBeat: r.End})
			}
		}
	}
	if !opts.noLocators {
		var locs []locatorRow
		if err := api.get("/locators", nil, &locs); err != nil {
			return liveErr(cmd, flags, err)
		}
		for i := range plan.Locators {
			for _, l := range locs {
				if math.Abs(l.Beat-plan.Locators[i].AtBeat) < 1e-3 {
					plan.Locators[i].Replaces = l.Name
				}
			}
		}
	} else {
		plan.Locators = []arrLocator{}
	}
	if dryRunOK(flags) {
		plan.DryRun = true
		return emit(cmd, flags, plan, func(w io.Writer) { printArrPlan(w, plan, true) })
	}
	if len(plan.Conflicts) > 0 && !opts.force {
		c := plan.Conflicts[0]
		return apiErr(fmt.Errorf("the Arrangement already has %d clip(s) between %s and %s, e.g. %q on track %d at beat %g; move them, choose another --start, or pass --force to overwrite (preview with --dry-run)", len(plan.Conflicts), plan.Start, plan.End, c.Clip, c.TrackNumber, c.StartBeat))
	}
	for _, p := range plan.Placements {
		path := livePath("/clips/{track}/{slot}/to-arrangement", "track", itoa(p.TrackNumber), "slot", itoa(p.Slot))
		if err := api.post(path, map[string]any{"at": beatsArg(p.AtBeat)}, nil); err != nil {
			plan.Success = false
			return liveErr(cmd, flags, fmt.Errorf("%w (stopped after %d of %d clip copies; Live's own undo removes them)", err, plan.Placed, len(plan.Placements)))
		}
		plan.Placed++
	}
	for _, l := range plan.Locators {
		if err := api.post("/locators", map[string]any{"name": l.Name, "at": beatsArg(l.AtBeat)}, nil); err != nil {
			return liveErr(cmd, flags, fmt.Errorf("%w (all %d clip copies were placed; stopped after %d of %d locators)", err, plan.Placed, plan.Located, len(plan.Locators)))
		}
		plan.Located++
	}
	return emit(cmd, flags, plan, func(w io.Writer) { printArrPlan(w, plan, false) })
}

func printArrPlan(w io.Writer, plan *arrPlan, dry bool) {
	if dry {
		fmt.Fprintf(w, "dry-run: nothing was changed in Live. Layout plan (%s, %s to %s, %g bars):\n", plan.Signature, plan.Start, plan.End, plan.TotalBars)
	} else {
		fmt.Fprintf(w, "Built the Arrangement (%s, %s to %s, %g bars): %d clip copies, %d locators.\n", plan.Signature, plan.Start, plan.End, plan.TotalBars, plan.Placed, plan.Located)
	}
	tw := newTabWriter(w)
	fmt.Fprintln(tw, "SECTION\tSCENE\tSTART\tEND\tBARS\tCLIPS")
	for _, s := range plan.Sections {
		fmt.Fprintf(tw, "%q\t%d %q\t%s\t%s\t%g (%s)\t%d\n", s.Name, s.SceneNumber, s.Scene, s.Start, s.End, s.Bars, s.BarsSource, s.Clips)
	}
	_ = tw.Flush()
	if dry {
		fmt.Fprintln(w, "\nClip copies:")
		tw = newTabWriter(w)
		fmt.Fprintln(tw, "SECTION\tTRACK\tSLOT\tCLIP\tAT\tBEATS")
		for _, p := range plan.Placements {
			fmt.Fprintf(tw, "%q\t%d %q\t%d\t%q\t%s\t%g\n", p.Section, p.TrackNumber, p.Track, p.Slot, p.Clip, p.At, p.LengthBeats)
		}
		_ = tw.Flush()
		if len(plan.Locators) > 0 {
			fmt.Fprintln(w, "\nLocators:")
			for _, l := range plan.Locators {
				extra := ""
				if l.Replaces != "" {
					extra = fmt.Sprintf(" (renames existing locator %q)", l.Replaces)
				}
				fmt.Fprintf(w, "  %s  %q%s\n", l.At, l.Name, extra)
			}
		}
		if len(plan.Conflicts) > 0 {
			fmt.Fprintf(w, "\nConflicts (%d existing Arrangement clips in range; the build needs --force):\n", len(plan.Conflicts))
			for _, c := range plan.Conflicts {
				fmt.Fprintf(w, "  track %d %q: %q at beat %g-%g\n", c.TrackNumber, c.Track, c.Clip, c.StartBeat, c.EndBeat)
			}
		}
	}
	for _, warn := range plan.Warnings {
		fmt.Fprintf(w, "warning: %s\n", warn)
	}
}

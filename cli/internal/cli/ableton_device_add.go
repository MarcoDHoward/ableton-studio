// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written (slice C): device add, plus the browser-index and track name
// resolution that chain apply reuses.
// Bridges: local browser index (filled earlier through AbletonMCP), AbletonOSC
// for the track list and device list, AbletonMCP load_instrument_or_effect.

package cli

import (
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"ableton-pp-cli/internal/live"
	"ableton-pp-cli/internal/store"
)

const browserIndexHint = "build it once with 'ableton-pp-cli sync --resources browser' (reads Live's browser through the AbletonMCP script; takes about a minute), then retry"

var browserCategoryNames = []string{"instruments", "sounds", "drums", "audio_effects", "midi_effects", "plugins"}

func normCategory(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "_")
}

func foldName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func stripPresetExt(s string) string {
	ext := strings.ToLower(path.Ext(s))
	switch ext {
	case ".adg", ".adv", ".amxd", ".vst", ".vst3", ".component", ".alc":
		return strings.TrimSuffix(s, s[len(s)-len(ext):])
	}
	return s
}

// loadBrowserIndex reads the local browser index; an empty index is an
// error that says how to build it.
func loadBrowserIndex(cmd *cobra.Command) ([]store.BrowserItem, error) {
	if localDataDisabled() {
		return nil, configErr(fmt.Errorf("loading by name needs the local browser index, which %s=1 disables; use 'device load --uri' instead", noLocalDataEnvVar))
	}
	db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("ableton-pp-cli"))
	if err != nil {
		return nil, configErr(fmt.Errorf("opening the local store: %w", err))
	}
	defer db.Close()
	items, err := store.LoadableBrowserItems(cmd.Context(), db.DB())
	if err != nil {
		return nil, configErr(err)
	}
	if len(items) == 0 {
		return nil, configErr(fmt.Errorf("the local browser index is empty: %s", browserIndexHint))
	}
	return items, nil
}

type browserAmbiguousError struct {
	query string
	hits  []store.BrowserItem
}

func (e *browserAmbiguousError) Error() string {
	parts := []string{}
	for i, h := range e.hits {
		if i == 10 {
			parts = append(parts, fmt.Sprintf("and %d more", len(e.hits)-10))
			break
		}
		parts = append(parts, fmt.Sprintf("%q (%s)", h.Path, h.Category))
	}
	return fmt.Sprintf("browser name %q matches %d items: %s; narrow it with --category or give the path, e.g. %q", e.query, len(e.hits), strings.Join(parts, ", "), e.hits[0].Path)
}

// resolveBrowserItem finds one loadable item by name. Order: an item path
// (when the query contains "/"), exact name (ignoring a preset extension),
// then unique prefix, then unique substring; exactOnly stops after the exact
// stage. Among several hits a single device wins over presets.
func resolveBrowserItem(items []store.BrowserItem, query, category string, exactOnly bool) (store.BrowserItem, error) {
	q := foldName(query)
	if q == "" {
		return store.BrowserItem{}, usageErr(fmt.Errorf("give the browser name to load, e.g. Echo or Drift"))
	}
	cat := normCategory(category)
	if cat != "" {
		ok := false
		for _, c := range browserCategoryNames {
			if c == cat {
				ok = true
			}
		}
		if !ok {
			return store.BrowserItem{}, usageErr(fmt.Errorf("--category must be one of %s", strings.Join(browserCategoryNames, ", ")))
		}
	}
	pool := make([]store.BrowserItem, 0, len(items))
	for _, it := range items {
		if cat == "" || normCategory(it.Category) == cat {
			pool = append(pool, it)
		}
	}
	type stage func(it store.BrowserItem) bool
	var stages []stage
	if strings.Contains(q, "/") {
		qp := strings.Trim(q, "/")
		stages = append(stages, func(it store.BrowserItem) bool {
			p := foldName(it.Path)
			return p == qp || strings.HasSuffix(p, "/"+qp) || foldName(stripPresetExt(it.Path)) == qp || strings.HasSuffix(foldName(stripPresetExt(it.Path)), "/"+qp)
		})
	}
	stages = append(stages, func(it store.BrowserItem) bool {
		n := foldName(it.Name)
		return n == q || foldName(stripPresetExt(it.Name)) == q
	})
	if !exactOnly {
		stages = append(stages,
			func(it store.BrowserItem) bool { return strings.HasPrefix(foldName(it.Name), q) },
			func(it store.BrowserItem) bool { return strings.Contains(foldName(it.Name), q) })
	}
	for _, match := range stages {
		var hits []store.BrowserItem
		seen := map[string]bool{}
		for _, it := range pool {
			if match(it) && !seen[it.URI] {
				seen[it.URI] = true
				hits = append(hits, it)
			}
		}
		if len(hits) == 1 {
			return hits[0], nil
		}
		if len(hits) > 1 {
			var devices []store.BrowserItem
			for _, h := range hits {
				if h.IsDevice {
					devices = append(devices, h)
				}
			}
			if len(devices) == 1 {
				return devices[0], nil
			}
			sort.SliceStable(hits, func(a, b int) bool { return hits[a].Path < hits[b].Path })
			return store.BrowserItem{}, apiErr(&browserAmbiguousError{query: query, hits: hits})
		}
	}
	names := make([]string, 0, len(pool))
	seen := map[string]bool{}
	for _, it := range pool {
		if !seen[it.Name] {
			seen[it.Name] = true
			names = append(names, it.Name)
		}
	}
	msg := fmt.Sprintf("browser name %q not found in the local browser index", query)
	if cat != "" {
		msg += fmt.Sprintf(" (category %s)", cat)
	}
	if sug := live.Suggest(query, names, 3); len(sug) > 0 {
		s := make([]string, len(sug))
		for i, c := range sug {
			s[i] = c.Name
		}
		msg += "; close matches: " + joinQuoted(s)
	}
	msg += "; if it was installed recently, rebuild the index with 'ableton-pp-cli sync --resources browser'"
	return store.BrowserItem{}, notFoundErr(errors.New(msg))
}

// resolveTrackRow matches a track by number or name with the same rules as
// the router (number, exact, unique prefix, unique substring).
func resolveTrackRow(rows []liveTrack, input string) (liveTrack, error) {
	i, err := live.Resolve("track", input, trackNames(rows))
	if err != nil {
		var nf *live.NotFoundError
		if errors.As(err, &nf) {
			return liveTrack{}, notFoundErr(err)
		}
		return liveTrack{}, apiErr(err)
	}
	return rows[i], nil
}

// checkDeviceFits refuses combinations Live would silently ignore.
func checkDeviceFits(item store.BrowserItem, tr liveTrack) error {
	cat := normCategory(item.Category)
	switch {
	case (cat == "instruments" || cat == "drums" || cat == "sounds" || cat == "midi_effects") && tr.Type == "audio":
		return usageErr(fmt.Errorf("%q is in %s and needs a MIDI track; track %d %q is an audio track", item.Name, cat, tr.Number, tr.Name))
	case cat != "audio_effects" && cat != "plugins" && tr.Type == "group":
		return usageErr(fmt.Errorf("track %d %q is a group track, which only takes audio effects", tr.Number, tr.Name))
	}
	return nil
}

func runDeviceAdd(cmd *cobra.Command, flags *rootFlags, args []string, category string) error {
	if planOnly(flags, args, 2) {
		return writeDryRun(cmd.OutOrStdout(), flags, "device add")
	}
	if len(args) != 2 {
		return usageErr(fmt.Errorf("give the track and the browser name, e.g. 'device add Bass Drift'"))
	}
	items, err := loadBrowserIndex(cmd)
	if err != nil {
		return err
	}
	item, err := resolveBrowserItem(items, args[1], category, false)
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
	tr, err := resolveTrackRow(rows, args[0])
	if err != nil {
		return err
	}
	if err := checkDeviceFits(item, tr); err != nil {
		return err
	}
	out := map[string]any{
		"track": tr.Name, "track_number": tr.Number,
		"device": item.Name, "uri": item.URI, "path": item.Path, "category": item.Category,
	}
	if dryRunOK(flags) {
		out["dry_run"] = true
		out["success"] = true
		out["would"] = fmt.Sprintf("load %q (%s) onto track %d through AbletonMCP", item.Name, item.URI, tr.Number)
		return emit(cmd, flags, out, func(w io.Writer) {
			fmt.Fprintf(w, "dry-run: would load %q (%s, %s) onto track %d %q; nothing was loaded\n", item.Name, item.Path, item.URI, tr.Number, tr.Name)
		})
	}
	tn := itoa(tr.Number)
	if err := api.post(livePath("/tracks/{track}/devices", "track", tn), map[string]any{"uri": item.URI}, nil); err != nil {
		return liveErr(cmd, flags, err)
	}
	devs, err := api.devices(tn)
	if err != nil {
		return liveErr(cmd, flags, err)
	}
	names := make([]string, len(devs))
	for i, d := range devs {
		names[i] = d.Name
	}
	out["devices_after"] = names
	return emit(cmd, flags, out, func(w io.Writer) {
		fmt.Fprintf(w, "Loaded %q onto track %d %q. Devices now: %s\n", item.Name, tr.Number, tr.Name, joinQuoted(names))
	})
}

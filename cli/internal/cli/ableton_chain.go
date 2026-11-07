// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written (slice C): chain save / chain apply / chain list.
// Bridges: AbletonOSC (device list, parameter list, parameter set through the
// journaled PUT .../params/{param}), AbletonMCP load_instrument_or_effect
// through POST /tracks/{track}/devices, and the local browser index.

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ableton-pp-cli/internal/cliutil"
	"ableton-pp-cli/internal/store"
)

type chainParam struct {
	Name      string  `json:"name"`
	Value     float64 `json:"value"`
	Display   *string `json:"display,omitempty"`
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
	Quantized bool    `json:"quantized"`
}

type chainDevice struct {
	Name      string       `json:"name"`
	ClassName string       `json:"class_name"`
	Type      string       `json:"type"`
	On        *bool        `json:"on,omitempty"`
	Params    []chainParam `json:"params"`
}

type savedChain struct {
	SourceTrack       string        `json:"source_track"`
	SourceTrackNumber int           `json:"source_track_number"`
	Devices           []chainDevice `json:"devices"`
}

func runChainSave(cmd *cobra.Command, flags *rootFlags, args []string, force bool) error {
	if planOnly(flags, args, 2) {
		return writeDryRun(cmd.OutOrStdout(), flags, "chain save")
	}
	if len(args) != 2 {
		return usageErr(fmt.Errorf("give the track and a name for the chain, e.g. 'chain save \"Dub Bus\" dub-chain'"))
	}
	name := strings.TrimSpace(args[1])
	if name == "" || len(name) > 200 {
		return usageErr(fmt.Errorf("the chain name must be 1-200 characters"))
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
	tn := itoa(tr.Number)
	devs, err := api.devices(tn)
	if err != nil {
		return liveErr(cmd, flags, err)
	}
	if len(devs) == 0 {
		return usageErr(fmt.Errorf("track %d %q has no devices to save", tr.Number, tr.Name))
	}
	chain := savedChain{SourceTrack: tr.Name, SourceTrackNumber: tr.Number, Devices: []chainDevice{}}
	for _, d := range devs {
		params, err := api.params(tn, itoa(d.Number))
		if err != nil {
			return liveErr(cmd, flags, err)
		}
		cd := chainDevice{Name: d.Name, ClassName: d.ClassName, Type: d.Type, On: d.On, Params: make([]chainParam, len(params))}
		for i, p := range params {
			cd.Params[i] = chainParam{Name: p.Name, Value: p.Value, Display: p.Display, Min: p.Min, Max: p.Max, Quantized: p.Quantized}
		}
		chain.Devices = append(chain.Devices, cd)
	}
	out := map[string]any{"chain": name, "source_track": tr.Name, "source_track_number": tr.Number, "devices": chain.Devices}
	if dryRunOK(flags) {
		out["dry_run"] = true
		out["success"] = true
		out["would"] = fmt.Sprintf("store %d devices as chain %q in the local store", len(chain.Devices), name)
		return emit(cmd, flags, out, func(w io.Writer) {
			fmt.Fprintf(w, "dry-run: would save %d devices from track %d %q as chain %q; nothing was stored\n", len(chain.Devices), tr.Number, tr.Name, name)
		})
	}
	body, err := json.Marshal(chain)
	if err != nil {
		return err
	}
	db, err := openAbletonStore(cmd.Context())
	if err != nil {
		return err
	}
	defer db.Close()
	existed, err := store.SaveChain(cmd.Context(), db.DB(), name, tr.Name, time.Now(), body, force)
	if err != nil {
		if existed && !force {
			return apiErr(fmt.Errorf("a chain named %q already exists; pass --force to replace it or pick another name", name))
		}
		return configErr(err)
	}
	out["replaced"] = existed
	return emit(cmd, flags, out, func(w io.Writer) {
		names := make([]string, len(chain.Devices))
		params := 0
		for i, d := range chain.Devices {
			names[i] = d.Name
			params += len(d.Params)
		}
		fmt.Fprintf(w, "Saved chain %q from track %d %q: %s (%d parameter values).\n", name, tr.Number, tr.Name, joinQuoted(names), params)
	})
}

// chainClassBrowserNames maps Live device class names to browser names, used
// when a device was renamed and its display name is not in the index.
var chainClassBrowserNames = map[string]string{
	"Eq8": "EQ Eight", "Compressor2": "Compressor", "StereoGain": "Utility", "AutoFilter": "Auto Filter",
	"Reverb": "Reverb", "Delay": "Delay", "Echo": "Echo", "Operator": "Operator", "Drift": "Drift",
	"InstrumentVector": "Wavetable", "UltraAnalog": "Analog", "OriginalSimpler": "Simpler",
	"MultiSampler": "Sampler", "Saturator": "Saturator", "GlueCompressor": "Glue Compressor",
	"Limiter": "Limiter", "Gate": "Gate", "Redux2": "Redux", "Overdrive": "Overdrive", "Erosion": "Erosion",
	"FilterDelay": "Filter Delay", "GrainDelay": "Grain Delay", "BeatRepeat": "Beat Repeat",
	"Vinyl": "Vinyl Distortion", "Corpus": "Corpus", "Amp": "Amp", "Cabinet": "Cabinet", "Pedal": "Pedal",
	"DrumBuss": "Drum Buss", "ChannelEq": "Channel EQ", "Collision": "Collision", "LoungeLizard": "Electric",
	"StringStudio": "Tension", "Tuner": "Tuner", "MidiArpeggiator": "Arpeggiator", "MidiChord": "Chord",
	"MidiScale": "Scale", "MidiNoteLength": "Note Length", "MidiPitcher": "Pitch", "MidiRandom": "Random",
	"MidiVelocity": "Velocity", "Meld": "Meld", "Roar": "Roar",
}

func typeCategory(t string) string {
	switch t {
	case "instrument":
		return "instruments"
	case "audio_effect":
		return "audio_effects"
	case "midi_effect":
		return "midi_effects"
	}
	return ""
}

// resolveChainDevice finds the browser item that rebuilds a saved device:
// an explicit --map entry, the device's own name, then its class name. Exact
// names only; when several items share the name, the category that fits the
// device type wins.
func resolveChainDevice(items []store.BrowserItem, d chainDevice, mapping map[string]string) (store.BrowserItem, error) {
	var candidates []string
	if m, ok := mapping[foldName(d.Name)]; ok {
		candidates = append(candidates, m)
	}
	candidates = append(candidates, d.Name)
	if n, ok := chainClassBrowserNames[d.ClassName]; ok {
		candidates = append(candidates, n)
	}
	var lastErr error
	for _, c := range candidates {
		it, err := resolveBrowserItem(items, c, "", true)
		if err == nil {
			return it, nil
		}
		var amb *browserAmbiguousError
		if errors.As(err, &amb) {
			if cat := typeCategory(d.Type); cat != "" {
				if it, err2 := resolveBrowserItem(items, c, cat, true); err2 == nil {
					return it, nil
				}
			}
		}
		lastErr = err
	}
	return store.BrowserItem{}, lastErr
}

type chainApplyDevice struct {
	Name         string           `json:"name"`
	URI          string           `json:"uri"`
	BrowserPath  string           `json:"browser_path"`
	DeviceNumber int              `json:"device_number,omitempty"`
	LoadedAs     string           `json:"loaded_as,omitempty"`
	SavedParams  int              `json:"saved_params"`
	ParamsSet    int              `json:"params_set"`
	ParamsSame   int              `json:"params_unchanged"`
	ParamsFailed []map[string]any `json:"params_failed,omitempty"`
	saved        chainDevice
}

type chainApplyResult struct {
	Chain       string              `json:"chain"`
	DryRun      bool                `json:"dry_run,omitempty"`
	Success     bool                `json:"success"`
	Track       string              `json:"track"`
	TrackNumber int                 `json:"track_number"`
	Devices     []*chainApplyDevice `json:"devices"`
	Skipped     []map[string]any    `json:"skipped,omitempty"`
}

// findLoadedDevice locates the device a load added (or replaced) by
// comparing the device lists before and after. When an instrument replaced
// an identical one (same name and class), the lists look the same; then the
// single device of the wanted class is the one that was loaded.
func findLoadedDevice(before, after []liveDevice, wantClass string) (liveDevice, bool) {
	if len(after) == len(before)+1 || len(after) == len(before) {
		for i := range after {
			if i >= len(before) || after[i].Name != before[i].Name || after[i].ClassName != before[i].ClassName {
				return after[i], true
			}
		}
	}
	if len(after) == len(before) && wantClass != "" {
		var hit []liveDevice
		for _, d := range after {
			if d.ClassName == wantClass {
				hit = append(hit, d)
			}
		}
		if len(hit) == 1 {
			return hit[0], true
		}
	}
	return liveDevice{}, false
}

func paramDiffers(saved, now liveParam) bool {
	span := math.Max(math.Abs(now.Max-now.Min), 1)
	return math.Abs(saved.Value-now.Value) > 1e-6*span
}

// applyChainParams sets every saved parameter that differs on the new device,
// matched by name (repeated names by order) and addressed by number.
func applyChainParams(api *liveAPI, tn, dn string, dev *chainApplyDevice) error {
	params, err := api.params(tn, dn)
	if err != nil {
		return err
	}
	liveNames := make([]string, len(params))
	for i, p := range params {
		liveNames[i] = p.Name
	}
	liveKeys := nameKeys(liveNames)
	byKey := map[string]liveParam{}
	for i, p := range params {
		byKey[liveKeys[i]] = p
	}
	savedNames := make([]string, len(dev.saved.Params))
	for i, p := range dev.saved.Params {
		savedNames[i] = p.Name
	}
	savedKeys := nameKeys(savedNames)
	for i, sp := range dev.saved.Params {
		now, ok := byKey[savedKeys[i]]
		if !ok {
			dev.ParamsFailed = append(dev.ParamsFailed, map[string]any{"param": savedKeys[i], "error": "not on the loaded device"})
			continue
		}
		v := sp.Value
		if now.Quantized {
			v = math.Round(v)
		}
		if !paramDiffers(liveParam{Value: v}, now) {
			dev.ParamsSame++
			continue
		}
		if v < now.Min || v > now.Max {
			dev.ParamsFailed = append(dev.ParamsFailed, map[string]any{"param": savedKeys[i], "error": fmt.Sprintf("saved value %v is outside %v..%v", v, now.Min, now.Max)})
			continue
		}
		path := livePath("/tracks/{track}/devices/{device}/params/{param}", "track", tn, "device", dn, "param", itoa(now.Number))
		if err := api.put(path, map[string]any{"value": v}, nil); err != nil {
			dev.ParamsFailed = append(dev.ParamsFailed, map[string]any{"param": savedKeys[i], "error": err.Error()})
			continue
		}
		dev.ParamsSet++
	}
	return nil
}

func loadChain(cmd *cobra.Command, db *store.Store, name string) (savedChain, error) {
	var chain savedChain
	row, err := store.GetChain(cmd.Context(), db.DB(), name)
	if errors.Is(err, store.ErrNotFound) {
		names := []string{}
		if all, lerr := store.ListChains(cmd.Context(), db.DB()); lerr == nil {
			for _, c := range all {
				names = append(names, c.Name)
			}
		}
		msg := fmt.Sprintf("no saved chain named %q", name)
		if len(names) > 0 {
			msg += "; saved chains: " + joinQuoted(names)
		} else {
			msg += "; save one with 'ableton-pp-cli chain save <track> <name>'"
		}
		return chain, notFoundErr(errors.New(msg))
	}
	if err != nil {
		return chain, configErr(err)
	}
	if err := json.Unmarshal(row.Chain, &chain); err != nil {
		return chain, configErr(fmt.Errorf("chain %q is unreadable: %w", name, err))
	}
	return chain, nil
}

func newChainApplyCmd(flags *rootFlags) *cobra.Command {
	var flagSkipMissing bool
	var flagMap []string
	cmd := &cobra.Command{
		Use:   "apply <name> <track>",
		Short: "Rebuild a saved device chain on a track: load each device by browser name, then set its parameters",
		Long: chainLong + "\n\n" +
			"Each saved device is found in the local browser index by its exact name (then\n" +
			"by its device class, e.g. a renamed EQ Eight), loaded onto the end of the\n" +
			"track through AbletonMCP like 'device load', and every saved parameter that\n" +
			"differs is set by name through the journaled 'param set' path. All devices\n" +
			"are resolved before anything loads: a device missing from the index stops the\n" +
			"run unless --skip-missing. --map \"Old Name=Browser Name\" overrides a lookup.\n" +
			"Parameters that cannot be set are reported and the command exits 6. --dry-run\n" +
			"resolves everything (reading the track list from Live) and loads nothing.",
		Example: "  ableton-pp-cli chain apply dub-chain \"Vox Bus\" --dry-run\n" +
			"  ableton-pp-cli chain apply dub-chain 4 --json\n" +
			"  ableton-pp-cli chain apply dub-chain Keys --map \"My Space=Reverb\" --skip-missing",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if planOnly(flags, args, 2) {
				return writeDryRun(cmd.OutOrStdout(), flags, "chain apply")
			}
			if len(args) != 2 {
				return usageErr(fmt.Errorf("give the chain name and the target track, e.g. 'chain apply dub-chain \"Vox Bus\"'"))
			}
			mapping := map[string]string{}
			for _, m := range flagMap {
				k, v, ok := strings.Cut(m, "=")
				if !ok || strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
					return usageErr(fmt.Errorf("--map takes \"Saved Name=Browser Name\", got %q", m))
				}
				mapping[foldName(k)] = strings.TrimSpace(v)
			}
			db, err := openAbletonStore(cmd.Context())
			if err != nil {
				return err
			}
			chain, err := loadChain(cmd, db, strings.TrimSpace(args[0]))
			db.Close()
			if err != nil {
				return err
			}
			items, err := loadBrowserIndex(cmd)
			if err != nil {
				return err
			}
			res := chainApplyResult{Chain: strings.TrimSpace(args[0]), Success: true, Devices: []*chainApplyDevice{}}
			for _, d := range chain.Devices {
				it, err := resolveChainDevice(items, d, mapping)
				if err != nil {
					if !flagSkipMissing {
						return fmt.Errorf("chain %q, device %q: %w (nothing was loaded; use --map or --skip-missing)", res.Chain, d.Name, err)
					}
					res.Skipped = append(res.Skipped, map[string]any{"device": d.Name, "class_name": d.ClassName, "reason": err.Error()})
					continue
				}
				res.Devices = append(res.Devices, &chainApplyDevice{Name: d.Name, URI: it.URI, BrowserPath: it.Path, SavedParams: len(d.Params), saved: d})
			}
			if len(res.Devices) == 0 {
				return usageErr(fmt.Errorf("none of the chain's devices could be found in the browser index"))
			}
			api, err := newLiveAPI(cmd, flags)
			if err != nil {
				return err
			}
			rows, err := api.tracks()
			if err != nil {
				return liveErr(cmd, flags, err)
			}
			tr, err := resolveTrackRow(rows, args[1])
			if err != nil {
				return err
			}
			res.Track, res.TrackNumber = tr.Name, tr.Number
			if tr.Type == "audio" || tr.Type == "group" {
				for _, d := range res.Devices {
					if d.saved.Type == "instrument" || d.saved.Type == "midi_effect" {
						return usageErr(fmt.Errorf("the chain has %s %q, which needs a MIDI track; track %d %q is a %s track", strings.ReplaceAll(d.saved.Type, "_", " "), d.Name, tr.Number, tr.Name, tr.Type))
					}
				}
			}
			if dryRunOK(flags) {
				res.DryRun = true
				return emit(cmd, flags, res, func(w io.Writer) { printChainApply(w, res) })
			}
			tn := itoa(tr.Number)
			for _, d := range res.Devices {
				before, err := api.devices(tn)
				if err != nil {
					return liveErr(cmd, flags, err)
				}
				if err := api.post(livePath("/tracks/{track}/devices", "track", tn), map[string]any{"uri": d.URI}, nil); err != nil {
					res.Success = false
					return liveErr(cmd, flags, fmt.Errorf("loading %q: %w", d.Name, err))
				}
				after, err := api.devices(tn)
				if err != nil {
					return liveErr(cmd, flags, err)
				}
				nd, ok := findLoadedDevice(before, after, d.saved.ClassName)
				if !ok {
					return apiErr(fmt.Errorf("loaded %q but could not tell which device on track %d it became; its parameters were not set", d.Name, tr.Number))
				}
				d.DeviceNumber, d.LoadedAs = nd.Number, nd.Name
				if err := applyChainParams(api, tn, itoa(nd.Number), d); err != nil {
					return liveErr(cmd, flags, err)
				}
				if len(d.ParamsFailed) > 0 {
					res.Success = false
				}
			}
			if err := emit(cmd, flags, res, func(w io.Writer) { printChainApply(w, res) }); err != nil {
				return err
			}
			if !res.Success {
				return partialFailureErr(fmt.Errorf("some parameters could not be set; see params_failed"))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagSkipMissing, "skip-missing", false, "Skip devices that are not in the browser index instead of stopping")
	cmd.Flags().StringArrayVar(&flagMap, "map", nil, "Override a lookup as \"Saved Name=Browser Name\" (repeatable)")
	return cmd
}

func printChainApply(w io.Writer, res chainApplyResult) {
	if res.DryRun {
		fmt.Fprintf(w, "dry-run: would rebuild chain %q on track %d %q; nothing was loaded\n", res.Chain, res.TrackNumber, res.Track)
		for i, d := range res.Devices {
			fmt.Fprintf(w, "  %d. %q from %q (%q), up to %d parameter values\n", i+1, d.Name, d.BrowserPath, d.URI, d.SavedParams)
		}
	} else {
		fmt.Fprintf(w, "Rebuilt chain %q on track %d %q:\n", res.Chain, res.TrackNumber, res.Track)
		for _, d := range res.Devices {
			fmt.Fprintf(w, "  device %d %q: %d parameters set, %d already matched, %d failed\n", d.DeviceNumber, d.LoadedAs, d.ParamsSet, d.ParamsSame, len(d.ParamsFailed))
			for _, f := range d.ParamsFailed {
				fmt.Fprintf(w, "    %q: %s\n", f["param"], cliutil.EscapeTerminalControls(fmt.Sprint(f["error"])))
			}
		}
	}
	for _, s := range res.Skipped {
		fmt.Fprintf(w, "skipped %q: %s\n", s["device"], cliutil.EscapeTerminalControls(fmt.Sprint(s["reason"])))
	}
}

func newChainListCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list [name]",
		Short: "List saved device chains, or show the devices and parameter values in one",
		Long: "Use this command to see which chains 'chain save' stored. Do NOT use it to read a\n" +
			"track's current devices; use 'device list' instead.\n\n" +
			"Reads only the local store; Live is not contacted.",
		Example:     "  ableton-pp-cli chain list\n  ableton-pp-cli chain list dub-chain --json",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageErr(fmt.Errorf("chain list takes at most one chain name"))
			}
			if flags.dryRun {
				return writeDryRun(cmd.OutOrStdout(), flags, "chain list")
			}
			db, err := openAbletonStore(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			if len(args) == 1 {
				chain, err := loadChain(cmd, db, strings.TrimSpace(args[0]))
				if err != nil {
					return err
				}
				return emit(cmd, flags, chain, func(w io.Writer) {
					fmt.Fprintf(w, "Chain %q (from track %q):\n", strings.TrimSpace(args[0]), chain.SourceTrack)
					for i, d := range chain.Devices {
						fmt.Fprintf(w, "  %d. %q (%s, %s): %d parameters\n", i+1, d.Name, d.ClassName, d.Type, len(d.Params))
					}
				})
			}
			rows, err := store.ListChains(cmd.Context(), db.DB())
			if err != nil {
				return configErr(err)
			}
			type summary struct {
				Name        string   `json:"name"`
				SavedAt     string   `json:"saved_at"`
				SourceTrack string   `json:"source_track"`
				Devices     []string `json:"devices"`
			}
			out := make([]summary, 0, len(rows))
			for _, r := range rows {
				var c savedChain
				_ = json.Unmarshal(r.Chain, &c)
				names := make([]string, len(c.Devices))
				for i, d := range c.Devices {
					names[i] = d.Name
				}
				out = append(out, summary{Name: r.Name, SavedAt: r.SavedAt, SourceTrack: r.SourceTrack, Devices: names})
			}
			return emit(cmd, flags, out, func(w io.Writer) {
				if len(out) == 0 {
					fmt.Fprintln(w, "No saved chains yet. Save one with 'ableton-pp-cli chain save <track> <name>'.")
					return
				}
				tw := newTabWriter(w)
				fmt.Fprintln(tw, "NAME\tFROM TRACK\tSAVED\tDEVICES")
				for _, s := range out {
					fmt.Fprintf(tw, "%q\t%q\t%s\t%s\n", s.Name, s.SourceTrack, localTime(s.SavedAt), joinQuoted(s.Devices))
				}
				_ = tw.Flush()
			})
		},
	}
}

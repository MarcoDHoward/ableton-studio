// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ableton-pp-cli/internal/client"
	"ableton-pp-cli/internal/cliutil"
	"ableton-pp-cli/internal/live"
)

// watchEvent is one NDJSON line.
type watchEvent struct {
	TS     string `json:"ts"`
	Event  string `json:"event"`
	Target string `json:"target"`
	Value  any    `json:"value"`
}

// watchTopic is one AbletonOSC listener.
type watchTopic struct {
	start, stop string
	args        []any
}

var watchTrackProps = map[string]string{"volume": "volume", "pan": "panning", "mute": "mute", "solo": "solo", "arm": "arm"}

func newWatchCmd(flags *rootFlags) *cobra.Command {
	var forStr, trackIn, deviceIn, paramIn string
	cmd := &cobra.Command{
		Use:   "watch <what>...",
		Short: "Stream live changes (tempo, playing, beat, selection, a track's mixer, a parameter) as NDJSON",
		Long: strings.Trim(`
Stream changes from Live as NDJSON lines: {"ts","event","target","value"}.
Each listener first reports the current value, then every change.

What to watch:
  tempo       song tempo
  playing     transport play/stop
  beat        every beat while playing
  selection   the selected track
  mixer       --track's volume, pan, mute, solo and arm
  volume, pan, mute, solo, arm   one mixer property of --track
  param       --track --device --param value (plus its display text)

Runs until Ctrl+C, or for --for (e.g. 30s, 5m). Listeners are removed on exit.
`, "\n"),
		Example: strings.Trim(`
  ableton-pp-cli watch tempo playing --for 30s
  ableton-pp-cli watch mixer --track Bass
  ableton-pp-cli watch param --track 1 --device 1 --param Cutoff
`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "watch")
			}
			if len(args) == 0 {
				return usageErr(fmt.Errorf("name what to watch: tempo, playing, beat, selection, mixer, volume, pan, mute, solo, arm or param"))
			}
			var dur time.Duration
			if strings.TrimSpace(forStr) != "" {
				d, err := cliutil.ParseDurationLoose(forStr)
				if err != nil || d <= 0 {
					return usageErr(fmt.Errorf("--for must be a duration such as 30s or 5m, got %q", forStr))
				}
				dur = d
			}
			if cliutil.IsAnyHarness() || cliutil.IsMCPChildEnv() {
				if dur == 0 || dur > 2*time.Second {
					dur = 2 * time.Second
				}
			}
			whats := map[string]bool{}
			needTrack, needParam := false, false
			for _, a := range args {
				for _, w := range strings.Split(a, ",") {
					w = strings.ToLower(strings.TrimSpace(w))
					switch w {
					case "":
						continue
					case "tempo", "playing", "beat", "selection":
					case "is_playing":
						w = "playing"
					case "selected_track":
						w = "selection"
					case "mixer", "track", "volume", "pan", "mute", "solo", "arm":
						needTrack = true
					case "param", "parameter":
						w = "param"
						needTrack, needParam = true, true
					default:
						return usageErr(fmt.Errorf("cannot watch %q: use tempo, playing, beat, selection, mixer, volume, pan, mute, solo, arm or param", w))
					}
					whats[w] = true
				}
			}
			if needTrack && strings.TrimSpace(trackIn) == "" {
				return usageErr(fmt.Errorf("--track is required to watch a track's mixer or a parameter"))
			}
			if needParam && (strings.TrimSpace(deviceIn) == "" || strings.TrimSpace(paramIn) == "") {
				return usageErr(fmt.Errorf("watching a parameter needs --track, --device and --param"))
			}
			if cliutil.IsVerifyEnv() {
				if c, err := flags.newClient(); err == nil && !c.IsAbletonVirtual() {
					fmt.Fprintln(cmd.ErrOrStderr(), "verify harness: watch needs a running AbletonOSC; nothing to stream against a mock server")
					return nil
				}
			}
			return runWatch(cmd, flags, whats, trackIn, deviceIn, paramIn, dur)
		},
	}
	cmd.Flags().StringVar(&forStr, "for", "", "Stop after this long (e.g. 30s, 5m); default: until Ctrl+C")
	cmd.Flags().StringVar(&trackIn, "track", "", "Track name or 1-based number (for mixer, volume, pan, mute, solo, arm, param)")
	cmd.Flags().StringVar(&deviceIn, "device", "", "Device name or 1-based number on --track (for param)")
	cmd.Flags().StringVar(&paramIn, "param", "", "Parameter name or 1-based number on --device (for param)")
	return cmd
}

func watchErr(err error) error {
	var nf *live.NotFoundError
	var amb *live.AmbiguousError
	var ue *live.UnavailableError
	var pe *live.PortInUseError
	switch {
	case errors.As(err, &nf):
		return notFoundErr(err)
	case errors.As(err, &amb):
		return usageErr(err)
	case errors.As(err, &ue):
		return apiErr(fmt.Errorf("%s. %s", strings.TrimRight(err.Error(), "."), client.AbletonBridgeHelp(ue.Bridge)))
	case errors.As(err, &pe):
		return apiErr(err)
	}
	return apiErr(err)
}

func oscNames(ctx context.Context, o *live.OSCConn, address string, echo int, args ...any) ([]string, error) {
	vals, err := o.Query(ctx, address, echo, args...)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i], _ = live.AsString(v)
	}
	return out, nil
}

func runWatch(cmd *cobra.Command, flags *rootFlags, whats map[string]bool, trackIn, deviceIn, paramIn string, dur time.Duration) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	o, err := live.DialOSC(live.OSCReplyAddr, live.OSCSendAddr)
	if err != nil {
		return watchErr(err)
	}
	defer o.Close()

	trackNames, err := oscNames(ctx, o, "/live/song/get/track_names", 0)
	if err != nil {
		return watchErr(err)
	}
	t, d, p := -1, -1, -1
	tname, dname, pname := "", "", ""
	if strings.TrimSpace(trackIn) != "" {
		if t, err = live.Resolve("track", trackIn, trackNames); err != nil {
			return watchErr(err)
		}
		tname = trackNames[t]
	}
	if whats["param"] {
		devs, err := oscNames(ctx, o, "/live/track/get/devices/name", 1, int32(t))
		if err != nil {
			return watchErr(err)
		}
		if d, err = live.Resolve("device", deviceIn, devs); err != nil {
			return watchErr(err)
		}
		dname = devs[d]
		params, err := oscNames(ctx, o, "/live/device/get/parameters/name", 2, int32(t), int32(d))
		if err != nil {
			return watchErr(err)
		}
		if p, err = live.Resolve("parameter", paramIn, params); err != nil {
			return watchErr(err)
		}
		pname = params[p]
	}

	var topics []watchTopic
	song := func(prop string) {
		topics = append(topics, watchTopic{"/live/song/start_listen/" + prop, "/live/song/stop_listen/" + prop, nil})
	}
	trackProp := func(osc string) {
		topics = append(topics, watchTopic{"/live/track/start_listen/" + osc, "/live/track/stop_listen/" + osc, []any{int32(t)}})
	}
	if whats["tempo"] {
		song("tempo")
	}
	if whats["playing"] {
		song("is_playing")
	}
	if whats["beat"] {
		song("beat")
	}
	if whats["selection"] {
		topics = append(topics, watchTopic{"/live/view/start_listen/selected_track", "/live/view/stop_listen/selected_track", nil})
	}
	for _, prop := range []string{"volume", "pan", "mute", "solo", "arm"} {
		if whats[prop] || whats["mixer"] || whats["track"] {
			if prop == "arm" && !whats["arm"] {
				can, err := o.Query(ctx, "/live/track/get/can_be_armed", 1, int32(t))
				if err != nil || len(can) == 0 {
					continue
				}
				if ok, _ := live.AsBool(can[0]); !ok {
					continue
				}
			}
			trackProp(watchTrackProps[prop])
		}
	}
	if whats["param"] {
		topics = append(topics, watchTopic{"/live/device/start_listen/parameter/value", "/live/device/stop_listen/parameter/value", []any{int32(t), int32(d), int32(p)}})
	}

	started := 0
	defer func() {
		for _, tp := range topics[:started] {
			_ = o.Send(tp.stop, tp.args...)
		}
	}()
	for _, tp := range topics {
		if err := o.Send(tp.start, tp.args...); err != nil {
			return watchErr(err)
		}
		started++
	}

	// --for counts from when the listeners are running, not from setup.
	if dur > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, dur)
		defer cancel()
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	emit := func(event, target string, value any) error {
		return enc.Encode(watchEvent{TS: time.Now().UTC().Format(time.RFC3339Nano), Event: event, Target: target, Value: value})
	}
	trackTarget := fmt.Sprintf("track %d %q", t+1, tname)
	paramTarget := fmt.Sprintf("%s / %q / %q", trackTarget, dname, pname)
	isOurs := func(args []any, n int, want ...int) bool {
		if len(args) < n {
			return false
		}
		for i, w := range want {
			if v, ok := live.AsInt(args[i]); !ok || v != w {
				return false
			}
		}
		return true
	}
	for {
		msg, err := o.Recv(ctx, time.Now().Add(time.Hour))
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return watchErr(err)
		}
		if msg == nil {
			continue
		}
		a := msg.Args
		var werr error
		switch msg.Address {
		case "/live/song/get/tempo":
			werr = emit("tempo", "song", live.JSONValue(first(a)))
		case "/live/song/get/is_playing":
			v, _ := live.AsBool(first(a))
			werr = emit("playing", "song", v)
		case "/live/song/get/beat":
			werr = emit("beat", "song", live.JSONValue(first(a)))
		case "/live/view/get/selected_track":
			idx, ok := live.AsInt(first(a))
			val := map[string]any{"number": nil, "name": nil}
			if ok && idx >= 0 && idx < len(trackNames) {
				val = map[string]any{"number": idx + 1, "name": trackNames[idx]}
			}
			werr = emit("selected_track", "view", val)
		case "/live/device/get/parameter/value":
			if isOurs(a, 4, t, d, p) {
				werr = emit("param", paramTarget, live.JSONValue(a[3]))
			}
		case "/live/device/get/parameter/value_string":
			if isOurs(a, 4, t, d, p) {
				werr = emit("param_display", paramTarget, live.JSONValue(a[3]))
			}
		case "/live/error":
			fmt.Fprintf(cmd.ErrOrStderr(), "AbletonOSC error: %v\n", fmt.Sprint(a...))
		default:
			for prop, osc := range watchTrackProps {
				if msg.Address == "/live/track/get/"+osc && isOurs(a, 2, t) {
					v := live.JSONValue(a[1])
					if prop == "mute" || prop == "solo" || prop == "arm" {
						v, _ = live.AsBool(a[1])
					}
					werr = emit(prop, trackTarget, v)
				}
			}
		}
		if werr != nil {
			return werr
		}
	}
}

func first(a []any) any {
	if len(a) == 0 {
		return nil
	}
	return a[0]
}

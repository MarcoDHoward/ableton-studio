// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written (slice C) test harness: an in-memory Live set served over fake
// AbletonOSC (UDP), AbletonMCP (TCP) and AbletonPPExtras (TCP) bridges on
// ephemeral loopback ports. Whole CLI commands run against it through the
// real router, so write paths are exercised without touching a real set.

package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"testing"

	"ableton-pp-cli/internal/client"
	"ableton-pp-cli/internal/cliutil/testenv"
	"ableton-pp-cli/internal/live"
	"ableton-pp-cli/internal/store"
)

type fakeParam struct {
	name           string
	value, min, mx float64
	quantized      bool
}

type fakeDevice struct {
	name, class string
	typ         int
	params      []*fakeParam
}

type fakeClip struct {
	name   string
	length float64
	midi   bool
	notes  []live.Note
}

type fakeArrClip struct {
	name       string
	start, end float64
}

type fakeTrack struct {
	name            string
	midi            bool
	volume, pan     float64
	mute, solo, arm bool
	sends           []float64
	devices         []*fakeDevice
	clips           map[int]*fakeClip // 0-based slot
	arr             []fakeArrClip
}

type fakeCue struct {
	name string
	time float64
}

type fakeLive struct {
	mu        sync.Mutex
	tracks    []*fakeTrack
	scenes    []string
	tempo     float64
	songTime  float64
	sigNum    int
	cues      []fakeCue
	templates map[string]func() *fakeDevice
	log       []string
}

// newFakeLive builds a small set: Drums (midi), Bass (midi, Drift + Echo),
// Keys (midi), Vox (audio); two scenes; two return tracks.
func newFakeLive() *fakeLive {
	drift := func() *fakeDevice {
		return &fakeDevice{name: "Drift", class: "Drift", typ: 1, params: []*fakeParam{
			{name: "Device On", value: 1, min: 0, mx: 1, quantized: true},
			{name: "Filter", value: 0.5, min: 0, mx: 1},
			{name: "Voices", value: 2, min: 1, mx: 8, quantized: true},
		}}
	}
	echo := func() *fakeDevice {
		return &fakeDevice{name: "Echo", class: "Echo", typ: 2, params: []*fakeParam{
			{name: "Device On", value: 1, min: 0, mx: 1, quantized: true},
			{name: "Feedback", value: 0.4, min: 0, mx: 1},
			{name: "Dry/Wet", value: 0.5, min: 0, mx: 1},
		}}
	}
	bassDrift := drift()
	bassDrift.params[1].value = 0.25
	bassDrift.params[2].value = 4
	bassEcho := echo()
	bassEcho.params[1].value = 0.7
	return &fakeLive{
		tempo:  120,
		sigNum: 4,
		scenes: []string{"Intro", "Drop"},
		tracks: []*fakeTrack{
			{name: "Drums", midi: true, volume: 0.85, sends: []float64{0, 0}, clips: map[int]*fakeClip{
				0: {name: "Beat A", length: 4, midi: true, notes: []live.Note{{Pitch: 36, Start: 0, Duration: 0.25, Velocity: 100}}},
				1: {name: "Beat B", length: 4, midi: true},
			}},
			{name: "Bass", midi: true, volume: 0.7, pan: -0.2, sends: []float64{0.1, 0.3}, devices: []*fakeDevice{bassDrift, bassEcho}, clips: map[int]*fakeClip{
				0: {name: "Bassline", length: 8, midi: true, notes: []live.Note{{Pitch: 40, Start: 0, Duration: 1, Velocity: 90}, {Pitch: 43, Start: 1, Duration: 1, Velocity: 90}}},
			}},
			{name: "Keys", midi: true, volume: 0.6, sends: []float64{0, 0}, clips: map[int]*fakeClip{}},
			{name: "Vox", midi: false, volume: 0.8, sends: []float64{0.5, 0}, clips: map[int]*fakeClip{}},
		},
		templates: map[string]func() *fakeDevice{
			"query:Synths#Drift": drift,
			"query:AudioFx#Echo": echo,
		},
	}
}

func (f *fakeLive) record(s string) {
	f.log = append(f.log, s)
}

// traffic returns a copy of every request the fakes received.
func (f *fakeLive) traffic() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

func (f *fakeLive) resetTraffic() {
	f.mu.Lock()
	f.log = nil
	f.mu.Unlock()
}

// writes filters traffic to requests that change Live.
func (f *fakeLive) writes() []string {
	var out []string
	for _, s := range f.traffic() {
		switch {
		case strings.Contains(s, "/set/"), strings.Contains(s, "/add/"), strings.Contains(s, "/remove/"),
			strings.HasPrefix(s, "mcp:load"), strings.HasPrefix(s, "mcp:duplicate"), strings.HasPrefix(s, "mcp:create"),
			strings.HasPrefix(s, "ppx:set"):
			out = append(out, s)
		}
	}
	return out
}

func i32(v any) int {
	n, _ := live.AsInt(v)
	return n
}

func f64(v any) float64 {
	x, _ := live.AsFloat(v)
	return x
}

func (f *fakeLive) handleOSC(addr string, args []any) (string, []any, bool) {
	f.record(addr)
	errOut := func(msg string) (string, []any, bool) {
		return "/live/error", []any{"Error handling OSC message: " + msg}, true
	}
	reply := func(vals ...any) (string, []any, bool) { return addr, vals, true }
	track := func(i int) *fakeTrack {
		if i < 0 || i >= len(f.tracks) {
			return nil
		}
		return f.tracks[i]
	}
	switch {
	case addr == "/live/application/get/version":
		return reply(int32(12), int32(4))
	case addr == "/live/song/get/track_names":
		vals := []any{}
		for _, t := range f.tracks {
			vals = append(vals, t.name)
		}
		return reply(vals...)
	case addr == "/live/song/get/num_tracks":
		return reply(int32(len(f.tracks)))
	case addr == "/live/song/get/track_data":
		lo, hi := i32(args[0]), i32(args[1])
		if hi < 0 || hi > len(f.tracks) {
			hi = len(f.tracks)
		}
		var vals []any
		for i := lo; i < hi; i++ {
			t := f.tracks[i]
			for _, p := range args[2:] {
				switch p {
				case "track.name":
					vals = append(vals, t.name)
				case "track.mute":
					vals = append(vals, t.mute)
				case "track.solo":
					vals = append(vals, t.solo)
				case "track.has_midi_input":
					vals = append(vals, t.midi)
				case "track.has_audio_input":
					vals = append(vals, !t.midi)
				case "track.can_be_armed":
					vals = append(vals, true)
				default:
					vals = append(vals, false)
				}
			}
		}
		return reply(vals...)
	case addr == "/live/song/get/num_scenes":
		return reply(int32(len(f.scenes)))
	case addr == "/live/song/get/scenes/name":
		vals := []any{}
		for _, s := range f.scenes {
			vals = append(vals, s)
		}
		return reply(vals...)
	case strings.HasPrefix(addr, "/live/scene/get/"):
		i := i32(args[0])
		if i >= len(f.scenes) {
			return errOut("list index out of range")
		}
		switch strings.TrimPrefix(addr, "/live/scene/get/") {
		case "name":
			return reply(int32(i), f.scenes[i])
		case "tempo":
			return reply(int32(i), float32(-1))
		case "time_signature_numerator", "time_signature_denominator":
			return reply(int32(i), int32(4))
		case "color_index":
			return reply(int32(i), int32(0))
		}
		return reply(int32(i), false)
	case addr == "/live/song/get/signature_numerator":
		return reply(int32(f.sigNum))
	case addr == "/live/song/get/signature_denominator":
		return reply(int32(4))
	case addr == "/live/song/get/tempo":
		return reply(float32(f.tempo))
	case addr == "/live/song/set/tempo":
		f.tempo = f64(args[0])
		return "", nil, false
	case addr == "/live/song/get/current_song_time":
		return reply(float32(f.songTime))
	case addr == "/live/song/set/current_song_time":
		f.songTime = f64(args[0])
		return "", nil, false
	case addr == "/live/song/get/cue_points":
		var vals []any
		for _, c := range f.cues {
			vals = append(vals, c.name, float32(c.time))
		}
		return reply(vals...)
	case strings.HasPrefix(addr, "/live/track/get/devices/"):
		t := track(i32(args[0]))
		if t == nil {
			return errOut("list index out of range")
		}
		vals := []any{int32(i32(args[0]))}
		for _, d := range t.devices {
			switch strings.TrimPrefix(addr, "/live/track/get/devices/") {
			case "name":
				vals = append(vals, d.name)
			case "class_name":
				vals = append(vals, d.class)
			case "type":
				vals = append(vals, int32(d.typ))
			}
		}
		return reply(vals...)
	case strings.HasPrefix(addr, "/live/track/get/clips/"):
		ti := i32(args[0])
		t := track(ti)
		if t == nil {
			return errOut("list index out of range")
		}
		vals := []any{int32(ti)}
		for s := range f.scenes {
			c := t.clips[s]
			switch {
			case c == nil:
				vals = append(vals, nil)
			case strings.HasSuffix(addr, "/name"):
				vals = append(vals, c.name)
			default:
				vals = append(vals, float32(c.length))
			}
		}
		return reply(vals...)
	case strings.HasPrefix(addr, "/live/track/get/"):
		ti := i32(args[0])
		t := track(ti)
		if t == nil {
			return errOut("list index out of range")
		}
		switch strings.TrimPrefix(addr, "/live/track/get/") {
		case "volume":
			return reply(int32(ti), float32(t.volume))
		case "panning":
			return reply(int32(ti), float32(t.pan))
		case "mute":
			return reply(int32(ti), t.mute)
		case "solo":
			return reply(int32(ti), t.solo)
		case "arm":
			return reply(int32(ti), t.arm)
		case "can_be_armed":
			return reply(int32(ti), true)
		case "send":
			s := i32(args[1])
			if s >= len(t.sends) {
				return errOut("list index out of range")
			}
			return reply(int32(ti), int32(s), float32(t.sends[s]))
		case "name":
			return reply(int32(ti), t.name)
		}
		return errOut("unknown track property")
	case strings.HasPrefix(addr, "/live/track/set/"):
		t := track(i32(args[0]))
		if t == nil {
			return errOut("list index out of range")
		}
		switch strings.TrimPrefix(addr, "/live/track/set/") {
		case "volume":
			t.volume = f64(args[1])
		case "panning":
			t.pan = f64(args[1])
		case "mute":
			t.mute, _ = live.AsBool(args[1])
		case "solo":
			t.solo, _ = live.AsBool(args[1])
		case "arm":
			t.arm, _ = live.AsBool(args[1])
		case "name":
			t.name, _ = live.AsString(args[1])
		case "send":
			t.sends[i32(args[1])] = f64(args[2])
		}
		return "", nil, false
	case strings.HasPrefix(addr, "/live/device/get/parameters/"):
		t := track(i32(args[0]))
		if t == nil || i32(args[1]) >= len(t.devices) {
			return errOut("list index out of range")
		}
		d := t.devices[i32(args[1])]
		vals := []any{int32(i32(args[0])), int32(i32(args[1]))}
		for _, p := range d.params {
			switch strings.TrimPrefix(addr, "/live/device/get/parameters/") {
			case "name":
				vals = append(vals, p.name)
			case "value":
				vals = append(vals, float32(p.value))
			case "min":
				vals = append(vals, float32(p.min))
			case "max":
				vals = append(vals, float32(p.mx))
			case "is_quantized":
				vals = append(vals, p.quantized)
			}
		}
		return reply(vals...)
	case strings.HasPrefix(addr, "/live/device/get/parameter/"):
		t := track(i32(args[0]))
		if t == nil || i32(args[1]) >= len(t.devices) || i32(args[2]) >= len(t.devices[i32(args[1])].params) {
			return errOut("list index out of range")
		}
		p := t.devices[i32(args[1])].params[i32(args[2])]
		head := []any{int32(i32(args[0])), int32(i32(args[1])), int32(i32(args[2]))}
		switch strings.TrimPrefix(addr, "/live/device/get/parameter/") {
		case "name":
			return reply(append(head, p.name)...)
		case "value":
			return reply(append(head, float32(p.value))...)
		case "value_string":
			return reply(append(head, fmt.Sprintf("%.2f", p.value))...)
		}
		return errOut("unknown parameter property")
	case addr == "/live/device/set/parameter/value":
		t := track(i32(args[0]))
		if t != nil && i32(args[1]) < len(t.devices) && i32(args[2]) < len(t.devices[i32(args[1])].params) {
			t.devices[i32(args[1])].params[i32(args[2])].value = f64(args[3])
		}
		return "", nil, false
	case addr == "/live/clip_slot/get/has_clip":
		t := track(i32(args[0]))
		if t == nil {
			return errOut("list index out of range")
		}
		return reply(int32(i32(args[0])), int32(i32(args[1])), t.clips[i32(args[1])] != nil)
	case strings.HasPrefix(addr, "/live/clip/"):
		t := track(i32(args[0]))
		if t == nil || t.clips[i32(args[1])] == nil {
			return errOut("no clip in slot")
		}
		c := t.clips[i32(args[1])]
		head := []any{int32(i32(args[0])), int32(i32(args[1]))}
		switch addr {
		case "/live/clip/get/notes":
			return reply(append(head, live.FlattenNotes(c.notes)...)...)
		case "/live/clip/remove/notes":
			c.notes = nil
			return "", nil, false
		case "/live/clip/add/notes":
			ns, _ := live.NotesFromFlat(args[2:])
			c.notes = append(c.notes, ns...)
			return "", nil, false
		case "/live/clip/get/name":
			return reply(append(head, c.name)...)
		case "/live/clip/get/length":
			return reply(append(head, float32(c.length))...)
		case "/live/clip/get/is_midi_clip":
			return reply(append(head, c.midi)...)
		case "/live/clip/get/is_audio_clip":
			return reply(append(head, !c.midi)...)
		}
		if strings.HasPrefix(addr, "/live/clip/get/") {
			return reply(append(head, int32(0))...)
		}
		return "", nil, false
	}
	if strings.Contains(addr, "/get/") {
		return errOut("fake AbletonOSC does not know " + addr)
	}
	return "", nil, false
}

func (f *fakeLive) serveOSC(t *testing.T) string {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 65536)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			addr, args, err := live.DecodeOSC(buf[:n])
			if err != nil {
				continue
			}
			f.mu.Lock()
			raddr, rargs, ok := f.handleOSC(addr, args)
			f.mu.Unlock()
			if ok {
				pkt, err := live.EncodeOSC(raddr, rargs...)
				if err == nil {
					_, _ = pc.WriteTo(pkt, from)
				}
			}
		}
	}()
	return pc.LocalAddr().String()
}

func (f *fakeLive) snapshotDoc() map[string]any {
	tracks := []any{}
	for ti, t := range f.tracks {
		sends := []any{}
		for i, v := range t.sends {
			sends = append(sends, map[string]any{"index": i, "value": v, "name": fmt.Sprintf("Send %d", i)})
		}
		slots := []any{}
		for s := range f.scenes {
			c := t.clips[s]
			var clip any
			if c != nil {
				notes := []any{}
				for _, n := range c.notes {
					notes = append(notes, map[string]any{"pitch": n.Pitch, "start_time": n.Start, "duration": n.Duration, "velocity": n.Velocity, "mute": n.Mute})
				}
				clip = map[string]any{"name": c.name, "length": c.length, "is_midi_clip": c.midi, "notes": notes, "note_count": len(notes)}
			}
			slots = append(slots, map[string]any{"index": s, "has_clip": c != nil, "clip": clip})
		}
		devs := []any{}
		for di, d := range t.devices {
			params := []any{}
			for pi, p := range d.params {
				params = append(params, map[string]any{"index": pi, "name": p.name, "value": p.value, "min": p.min, "max": p.mx})
			}
			devs = append(devs, map[string]any{"index": di, "name": d.name, "class_name": d.class, "type": "x", "parameters": params})
		}
		arr := []any{}
		for _, a := range t.arr {
			arr = append(arr, map[string]any{"name": a.name, "start_time": a.start, "end_time": a.end, "length": a.end - a.start})
		}
		tracks = append(tracks, map[string]any{
			"index": ti, "name": t.name, "is_midi_track": t.midi, "is_audio_track": !t.midi,
			"mute": t.mute, "solo": t.solo, "arm": t.arm, "volume": t.volume, "panning": t.pan,
			"sends": sends, "clip_slots": slots, "devices": devs, "arrangement_clips": arr,
		})
	}
	scenes := []any{}
	for i, s := range f.scenes {
		scenes = append(scenes, map[string]any{"index": i, "name": s, "tempo": nil, "is_triggered": false})
	}
	cues := []any{}
	for _, c := range f.cues {
		cues = append(cues, map[string]any{"name": c.name, "time": c.time})
	}
	return map[string]any{
		"schema":  "ableton_mcp_snapshot_v2",
		"session": map[string]any{"tempo": f.tempo, "signature_numerator": f.sigNum, "signature_denominator": 4, "is_playing": false},
		"tracks":  tracks, "scenes": scenes, "return_tracks": []any{}, "cue_points": cues,
		"include_notes": true, "include_params": true,
	}
}

func (f *fakeLive) handleTCP(prefix, typ string, params map[string]any) (any, string) {
	f.record(prefix + ":" + typ)
	idx := func(k string) int { v, _ := params[k].(float64); return int(v) }
	switch prefix + ":" + typ {
	case "mcp:get_session_info":
		return map[string]any{"tempo": f.tempo, "track_count": len(f.tracks), "return_track_count": 2}, ""
	case "mcp:load_instrument_or_effect":
		ti := idx("track_index")
		uri, _ := params["uri"].(string)
		mk, ok := f.templates[uri]
		if !ok || ti >= len(f.tracks) {
			return nil, "Browser item not found"
		}
		d := mk()
		f.tracks[ti].devices = append(f.tracks[ti].devices, d)
		return map[string]any{"loaded": true, "item_name": d.name, "track_name": f.tracks[ti].name, "uri": uri}, ""
	case "mcp:duplicate_session_clip_to_arrangement":
		ti, ci := idx("track_index"), idx("clip_index")
		at, _ := params["destination_time"].(float64)
		c := f.tracks[ti].clips[ci]
		if c == nil {
			return nil, "No clip in slot"
		}
		f.tracks[ti].arr = append(f.tracks[ti].arr, fakeArrClip{name: c.name, start: at, end: at + c.length})
		return map[string]any{"success": true}, ""
	case "mcp:create_locator":
		name, _ := params["name"].(string)
		at, _ := params["time"].(float64)
		for i, c := range f.cues {
			if math.Abs(c.time-at) < 1e-6 {
				f.cues[i].name = name
				return map[string]any{"ok": true}, ""
			}
		}
		f.cues = append(f.cues, fakeCue{name: name, time: at})
		return map[string]any{"ok": true}, ""
	case "mcp:get_arrangement_clips":
		ti := idx("track_index")
		clips := []any{}
		for _, a := range f.tracks[ti].arr {
			clips = append(clips, map[string]any{"name": a.name, "start_time": a.start, "end_time": a.end, "is_midi_clip": true})
		}
		return map[string]any{"clips": clips}, ""
	case "mcp:get_session_snapshot":
		return f.snapshotDoc(), ""
	case "ppx:mixer_db":
		rows := []any{}
		for i, t := range f.tracks {
			rows = append(rows, map[string]any{"track": i, "name": t.name, "volume": t.volume, "db": fmt.Sprintf("%.1f dB", 40*math.Log10(math.Max(t.volume, 1e-4)/0.85))})
		}
		return rows, ""
	}
	return nil, "unknown command " + typ
}

func (f *fakeLive) serveTCP(t *testing.T, prefix string) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var req struct {
					Type   string         `json:"type"`
					Params map[string]any `json:"params"`
				}
				if err := json.NewDecoder(bufio.NewReader(c)).Decode(&req); err != nil {
					return
				}
				f.mu.Lock()
				res, msg := f.handleTCP(prefix, req.Type, req.Params)
				f.mu.Unlock()
				var out map[string]any
				if msg != "" {
					out = map[string]any{"status": "error", "message": msg, "code": "bad_request"}
				} else {
					out = map[string]any{"status": "success", "result": res}
				}
				_ = json.NewEncoder(c).Encode(out)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// withFakeLive sandboxes HOME, starts the fake bridges and points the router
// at them.
func withFakeLive(t *testing.T) *fakeLive {
	t.Helper()
	testenv.Isolate(t)
	t.Setenv("ABLETON_BASE_URL", "")
	t.Setenv("ABLETON_NO_LOCAL_DATA", "")
	f := newFakeLive()
	osc := f.serveOSC(t)
	mcp := f.serveTCP(t, "mcp")
	ppx := f.serveTCP(t, "ppx")
	restore, err := client.SetAbletonBridgesForTest("127.0.0.1:0", osc, mcp, ppx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	return f
}

// runCLI executes the command tree with args and returns stdout+stderr.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := RootCmd()
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	return out.String(), err
}

// runJSON runs a command with --json and decodes stdout into v.
func runJSON(t *testing.T, v any, args ...string) {
	t.Helper()
	out, err := runCLI(t, append(args, "--json")...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	if v == nil {
		return
	}
	if err := json.Unmarshal([]byte(lastJSON(out)), v); err != nil {
		t.Fatalf("%v: cannot decode output: %v\n%s", args, err, out)
	}
}

// lastJSON returns the last top-level JSON value in s (warnings may precede it).
func lastJSON(s string) string {
	s = strings.TrimSpace(s)
	for i := 0; i < len(s); i++ {
		if s[i] != '{' && s[i] != '[' {
			continue
		}
		if i > 0 && s[i-1] != '\n' {
			continue
		}
		if json.Valid([]byte(s[i:])) {
			return s[i:]
		}
	}
	return s
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(defaultDBPath("ableton-pp-cli"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedBrowser(t *testing.T, items ...map[string]any) {
	t.Helper()
	s := openTestStore(t)
	for _, it := range items {
		it["is_loadable"] = true
		it["is_folder"] = false
		b, _ := json.Marshal(it)
		if err := s.UpsertBrowser(b); err != nil {
			t.Fatal(err)
		}
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	return ExitCode(err)
}

// compactJSON removes the indentation from the last JSON value in s.
func compactJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(lastJSON(s))); err != nil {
		return s
	}
	return buf.String()
}

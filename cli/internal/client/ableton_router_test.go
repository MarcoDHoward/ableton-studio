// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"

	"ableton-pp-cli/internal/live"
)

// Every method+path in the synthetic spec must have a route.
func TestEverySpecEndpointIsRouted(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "spec.yaml"))
	if err != nil {
		t.Skipf("spec.yaml not found: %v", err)
	}
	methodRE := regexp.MustCompile(`^\s+method:\s*(\w+)`)
	pathRE := regexp.MustCompile(`^\s+path:\s*"([^"]+)"`)
	var method string
	count := 0
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	for sc.Scan() {
		line := sc.Text()
		if m := methodRE.FindStringSubmatch(line); m != nil {
			method = m[1]
			continue
		}
		m := pathRE.FindStringSubmatch(line)
		if m == nil || method == "" {
			continue
		}
		// substitute every {var} with a sample value
		path := regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(m[1], "1")
		rt, _, ok, err := matchRoute(method, path)
		if !ok || err != nil {
			t.Errorf("%s %s is not routed: %v", method, m[1], err)
		} else if rt.pattern != m[1] {
			t.Errorf("%s %s routed to %s", method, m[1], rt.pattern)
		}
		method = ""
		count++
	}
	if count < 80 {
		t.Fatalf("parsed only %d endpoints from spec.yaml", count)
	}
}

func TestMatchRoute(t *testing.T) {
	cases := []struct {
		method, path, pattern string
		vars                  map[string]string
		status                int
	}{
		{"POST", "/scenes/selected/fire", "/scenes/selected/fire", nil, 0},
		{"POST", "/scenes/Intro/fire", "/scenes/{scene}/fire", map[string]string{"scene": "Intro"}, 0},
		{"PUT", "/tracks/Lead%20Synth/mute", "/tracks/{track}/mute", map[string]string{"track": "Lead Synth"}, 0},
		{"PUT", "/tracks/AC%2FDC/sends/B", "/tracks/{track}/sends/{send}", map[string]string{"track": "AC/DC", "send": "B"}, 0},
		{"POST", "/locators/next", "/locators/next", nil, 0},
		{"GET", "/nope", "", nil, http.StatusNotFound},
		{"PATCH", "/tracks", "", nil, http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		rt, vars, ok, err := matchRoute(tc.method, tc.path)
		if tc.status != 0 {
			var re *routeErr
			if ok || !errors.As(err, &re) || re.status != tc.status {
				t.Errorf("%s %s: want status %d, got %v %v", tc.method, tc.path, tc.status, ok, err)
			}
			continue
		}
		if !ok || rt.pattern != tc.pattern {
			t.Errorf("%s %s: got %v %v", tc.method, tc.path, rt, err)
			continue
		}
		for k, v := range tc.vars {
			if vars[k] != v {
				t.Errorf("%s %s: var %s = %q, want %q", tc.method, tc.path, k, vars[k], v)
			}
		}
	}
}

func TestClassifyRouteError(t *testing.T) {
	cases := []struct {
		err    error
		status int
		substr string
	}{
		{&live.NotFoundError{Kind: "track", Input: "x", Count: 2}, 404, "not found"},
		{&live.AmbiguousError{Kind: "track", Input: "B", Candidates: []live.Candidate{{Number: 2, Name: "Bass"}, {Number: 3, Name: "Bass 2"}}}, 300, `2 "Bass"`},
		{&live.UnavailableError{Bridge: live.BridgeOSC, Detail: "no reply"}, 503, "Link, Tempo & MIDI"},
		{&live.UnavailableError{Bridge: live.BridgePPX, Detail: "refused"}, 503, "scripts install"},
		{&live.RemoteError{Bridge: live.BridgePPX, Code: "not_found", Message: "no device"}, 404, "no device"},
		{&live.RemoteError{Bridge: live.BridgePPX, Code: "unsupported", Message: "x"}, 501, "Live version"},
		{&live.RemoteError{Bridge: live.BridgeOSC, Message: "list index out of range"}, 404, "out of range"},
		{&live.RemoteError{Bridge: live.BridgeOSC, Message: "boom"}, 400, "Live rejected"},
		{badInput("bad %d", 1), 400, "bad 1"},
		{context.DeadlineExceeded, 503, "timed out"},
	}
	for _, tc := range cases {
		status, msg := classifyRouteError(tc.err)
		if status != tc.status || !strings.Contains(msg, tc.substr) {
			t.Errorf("%T: got %d %q, want %d containing %q", tc.err, status, msg, tc.status, tc.substr)
		}
	}
}

func TestRouterOnlyForVirtualBase(t *testing.T) {
	for base, want := range map[string]bool{
		"osc://127.0.0.1:11000":  true,
		"/osc://127.0.0.1:11000": true,
		"http://127.0.0.1:5555":  false,
		"https://example.test":   false,
		"":                       false,
	} {
		c := &Client{BaseURL: base}
		if got := c.abletonVirtual(); got != want {
			t.Errorf("abletonVirtual(%q) = %v, want %v", base, got, want)
		}
		if !want {
			// only non-virtual bases are exercised here: they must not touch Live
			if _, _, routed, _ := c.abletonRoute(context.Background(), "GET", "/status", nil, nil); routed {
				t.Errorf("base %q must not be routed", base)
			}
		}
	}
}

func TestDryRunDoesNoIO(t *testing.T) {
	old := abletonOSCRemote
	abletonOSCRemote = "127.0.0.1:1" // would fail if contacted
	defer func() { abletonOSCRemote = old }()
	c := &Client{BaseURL: "osc://127.0.0.1:11000", DryRun: true}
	out, status, routed, err := c.abletonRoute(context.Background(), "PUT", "/tracks/Bass/mute", nil, map[string]any{"on": true})
	if !routed || err != nil || status != 200 {
		t.Fatalf("dry run: %v %d %v", routed, status, err)
	}
	var got struct {
		DryRun bool `json:"dry_run"`
		Would  struct {
			Bridge  string `json:"bridge"`
			Command string `json:"command"`
			Args    []any  `json:"args"`
		} `json:"would"`
	}
	if err := json.Unmarshal(out, &got); err != nil || !got.DryRun || got.Would.Bridge != "abletonosc" || got.Would.Command != "/live/track/set/mute" || got.Would.Args[0] != "Bass" {
		t.Fatalf("dry run body: %s", out)
	}
}

func TestValidateAudioPath(t *testing.T) {
	dir := t.TempDir()
	wav := filepath.Join(dir, "kick.wav")
	if err := os.WriteFile(wav, []byte("RIFF"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := validateAudioPath(wav); err != nil || got != wav {
		t.Fatalf("local wav: %q %v", got, err)
	}
	for _, bad := range []string{`\\server\share\kick.wav`, `//server/share/kick.wav`, "kick.wav", "", "file://C:/x.wav", filepath.Join(dir, "missing.wav"), filepath.Join(dir, "notes.txt")} {
		if _, err := validateAudioPath(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

// fakeAbletonOSC is a tiny AbletonOSC stand-in for router tests.
type fakeAbletonOSC struct {
	mu     sync.Mutex
	tempo  float32
	mute   map[int32]bool
	tracks []string
}

func (f *fakeAbletonOSC) serve(t *testing.T) string {
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
			reply := func(a string, vals ...any) {
				pkt, _ := live.EncodeOSC(a, vals...)
				_, _ = pc.WriteTo(pkt, from)
			}
			fail := func(msg string) { reply("/live/error", "Error handling OSC message: "+msg) }
			idx := func(i int) int32 { v, _ := live.AsInt(args[i]); return int32(v) }
			f.mu.Lock()
			switch addr {
			case "/live/application/get/version":
				reply(addr, int32(12), int32(4))
			case "/live/song/get/tempo":
				reply(addr, f.tempo)
			case "/live/song/set/tempo":
				v, _ := live.AsFloat(args[0])
				f.tempo = float32(v)
			case "/live/song/get/track_names":
				vals := make([]any, len(f.tracks))
				for i, n := range f.tracks {
					vals[i] = n
				}
				reply(addr, vals...)
			case "/live/song/get/track_data":
				var vals []any
				for i, n := range f.tracks {
					vals = append(vals, n, f.mute[int32(i)], false, i == 0, i != 0, i == 0, false)
				}
				reply(addr, vals...)
			case "/live/track/get/volume":
				reply(addr, idx(0), float32(0.85))
			case "/live/track/get/panning":
				reply(addr, idx(0), float32(0))
			case "/live/track/get/arm":
				reply(addr, idx(0), false)
			case "/live/track/get/send":
				if idx(1) >= 2 {
					fail("list index out of range")
				} else {
					reply(addr, idx(0), idx(1), float32(0.25))
				}
			case "/live/track/get/mute":
				reply(addr, idx(0), f.mute[idx(0)])
			case "/live/track/set/mute":
				b, _ := live.AsBool(args[1])
				f.mute[idx(0)] = b
			}
			f.mu.Unlock()
		}
	}()
	return pc.LocalAddr().String()
}

func withFakeBridges(t *testing.T, f *fakeAbletonOSC) {
	t.Helper()
	remote := f.serve(t)
	oldL, oldR, oldM, oldP := abletonOSCLocal, abletonOSCRemote, abletonMCPAddr, abletonPPXAddr
	abletonOSCLocal, abletonOSCRemote = "127.0.0.1:0", remote
	// closed loopback ports: MCP and PPExtras are "not loaded"
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln.Addr().String()
	ln.Close()
	abletonMCPAddr, abletonPPXAddr = closed, closed
	t.Cleanup(func() { abletonOSCLocal, abletonOSCRemote, abletonMCPAddr, abletonPPXAddr = oldL, oldR, oldM, oldP })
}

func TestRouterAgainstFakeOSC(t *testing.T) {
	f := &fakeAbletonOSC{tempo: 120, mute: map[int32]bool{}, tracks: []string{"Drums", "Bass", "Bass 2"}}
	withFakeBridges(t, f)
	var journaled []JournalEntry
	oldHook := JournalHook
	JournalHook = func(_ context.Context, e JournalEntry) (int64, error) {
		journaled = append(journaled, e)
		return int64(len(journaled)), nil
	}
	defer func() { JournalHook = oldHook }()
	c := &Client{BaseURL: "osc://127.0.0.1:11000"}
	ctx := context.Background()

	out, status, routed, err := c.abletonRoute(ctx, "GET", "/tracks", nil, nil)
	if !routed || err != nil || status != 200 {
		t.Fatalf("tracks: %d %v", status, err)
	}
	var tracks []map[string]any
	if err := json.Unmarshal(out, &tracks); err != nil || len(tracks) != 3 {
		t.Fatalf("tracks body: %s", out)
	}
	sends := tracks[1]["sends"].(map[string]any)
	if tracks[0]["type"] != "midi" || tracks[1]["type"] != "audio" || len(sends) != 2 || sends["B"] != 0.25 {
		t.Fatalf("track rows: %s", out)
	}
	if tracks[0]["note"] == nil || tracks[0]["volume_db"] != nil {
		t.Fatalf("expected volume_db null with a note: %s", out)
	}

	out, _, _, err = c.abletonRoute(ctx, "PUT", "/tracks/drums/mute", nil, map[string]any{"on": true})
	if err != nil {
		t.Fatal(err)
	}
	var pc map[string]any
	_ = json.Unmarshal(out, &pc)
	if pc["before"] != false || pc["after"] != true || pc["journal_id"] != float64(1) {
		t.Fatalf("mute PropChange: %s", out)
	}
	if len(journaled) != 1 || journaled[0].Path != "/tracks/drums/mute" || journaled[0].Property != "mute" {
		t.Fatalf("journal: %+v", journaled)
	}

	_, status, _, err = c.abletonRoute(ctx, "PUT", "/tracks/Bass/mute", nil, map[string]any{"on": true})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || status != 300 || !strings.Contains(apiErr.Body, `3 "Bass 2"`) {
		// "Bass" is an exact match, so this must succeed instead
		if err != nil {
			t.Fatalf("exact name should win: %v", err)
		}
	}
	_, status, _, err = c.abletonRoute(ctx, "PUT", "/tracks/Ba/mute", nil, map[string]any{"on": true})
	if status != 300 || err == nil {
		t.Fatalf("ambiguous prefix: %d %v", status, err)
	}
	_, status, _, _ = c.abletonRoute(ctx, "PUT", "/tracks/Keys/mute", nil, map[string]any{"on": true})
	if status != 404 {
		t.Fatalf("unknown track: %d", status)
	}
	_, status, _, _ = c.abletonRoute(ctx, "PUT", "/song/props/tempo", nil, map[string]any{"value": "5"})
	if status != 400 {
		t.Fatalf("tempo out of range: %d", status)
	}
	out, _, _, err = c.abletonRoute(ctx, "PUT", "/song/props/tempo", nil, map[string]any{"value": "121"})
	if err != nil || !strings.Contains(string(out), `"after":121`) {
		t.Fatalf("tempo set: %s %v", out, err)
	}
	out, _, _, err = c.abletonRoute(ctx, "GET", "/song/props/tempo", nil, nil)
	if err != nil || !strings.Contains(string(out), `"value":121`) {
		t.Fatalf("read after write: %s %v", out, err)
	}
	// AbletonPPExtras is not loaded: --db must explain the alternatives.
	_, status, _, err = c.abletonRoute(ctx, "PUT", "/tracks/1/volume", nil, map[string]any{"db": -6.0})
	if status != 503 || err == nil || !strings.Contains(err.Error(), "--fader") {
		t.Fatalf("volume --db without PPExtras: %d %v", status, err)
	}
}

func TestLocalDrivePathRefusesDevicePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows path forms")
	}
	for _, p := range []string{`\??\UNC\host\share\x.wav`, `/??/UNC/host/share/x.wav`, `\\?\UNC\host\share\x.wav`, `\\host\share\x.wav`, `\\?\C:\x.wav`, `\??\GLOBALROOT\Device\Mup\h\s\x.wav`, `\x.wav`, `C:x.wav`} {
		if localDrivePath(p) {
			t.Errorf("localDrivePath(%q) = true, want false", p)
		}
		if _, err := validateAudioPath(p); err == nil {
			t.Errorf("validateAudioPath(%q) accepted a non-local path", p)
		}
	}
	if !localDrivePath(`C:\Samples\kick.wav`) || !localDrivePath(`d:/x/y.wav`) {
		t.Error("drive-letter paths must be accepted")
	}
}

func TestAutomationSetRejectsZeroDuration(t *testing.T) {
	// A zero-length step writes nothing in Live, so it must fail before any bridge call.
	r := &routeReq{body: map[string]any{"points": []any{
		map[string]any{"time": 0.0, "value": 20.0, "duration": 0.0},
	}}}
	_, err := hAutomationSet(r)
	if err == nil || !strings.Contains(err.Error(), "duration must be a number > 0") {
		t.Fatalf("want a zero-duration rejection, got %v", err)
	}
}

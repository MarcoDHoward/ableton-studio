// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package client

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"ableton-pp-cli/internal/live"
)

// hSongMethod fires a parameterless AbletonOSC song/scene method.
func hSongMethod(address, action string) routeHandler {
	return func(r *routeReq) (any, error) {
		if err := r.act(address); err != nil {
			return nil, err
		}
		return ack(action, "song", ""), nil
	}
}

func hStatus(r *routeReq) (any, error) {
	res, err := r.qm([]live.Request{
		live.Q("/live/song/get/tempo", 0),
		live.Q("/live/song/get/signature_numerator", 0),
		live.Q("/live/song/get/signature_denominator", 0),
		live.Q("/live/song/get/is_playing", 0),
		live.Q("/live/song/get/current_song_time", 0),
		live.Q("/live/song/get/loop", 0),
		live.Q("/live/song/get/num_scenes", 0),
		live.Q("/live/application/get/version", 0),
	})
	if err != nil {
		return nil, err
	}
	num, den := firstInt(res[1]), firstInt(res[2])
	if live.ValidSignature(num, den) {
		r.sigNum, r.sigDen = num, den
	}
	beats := firstFloat(res[4])
	version := ""
	if len(res[7]) >= 2 {
		version = fmt.Sprintf("%d.%d", firstInt(res[7][:1]), firstInt(res[7][1:2]))
	}
	tracks, note, returns, err := r.loadTracks(0, -1)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"tempo":        firstFloat(res[0]),
		"signature":    live.FormatSignature(num, den),
		"is_playing":   firstBool(res[3]),
		"position":     r.formatPos(beats),
		"beat":         beats,
		"loop":         firstBool(res[5]),
		"track_count":  len(tracks),
		"scene_count":  firstInt(res[6]),
		"return_count": returns,
		"live_version": version,
		"tracks":       tracks,
	}
	if note != "" {
		out["note"] = note
	}
	return out, nil
}

func hStopClips(r *routeReq) (any, error) {
	in := strings.TrimSpace(r.params["track"])
	if in == "" {
		if err := r.act("/live/song/stop_all_clips"); err != nil {
			return nil, err
		}
		return ack("stop clips", "all tracks", ""), nil
	}
	t, name, err := r.resolveTrack(in)
	if err != nil {
		return nil, err
	}
	if err := r.act("/live/track/stop_all_clips", int32(t)); err != nil {
		return nil, err
	}
	return ack("stop clips", trackTarget(t, name), ""), nil
}

func hSeek(r *routeReq) (any, error) {
	to, hasTo := r.bodyString("to")
	by, hasBy, err := r.bodyFloat("by")
	if err != nil {
		return nil, err
	}
	hasTo = hasTo && strings.TrimSpace(to) != ""
	if hasTo == hasBy {
		return nil, badInput("give exactly one of --to (beats or bar.beat) or --by (beats)")
	}
	before, err := r.q("/live/song/get/current_song_time", 0)
	if err != nil {
		return nil, err
	}
	if hasTo {
		beats, err := r.position(to)
		if err != nil {
			return nil, err
		}
		if err := r.send("/live/song/set/current_song_time", float32(beats)); err != nil {
			return nil, err
		}
	} else {
		if math.IsNaN(by) || math.IsInf(by, 0) {
			return nil, badInput("--by must be a number of beats")
		}
		if err := r.send("/live/song/jump_by", float32(by)); err != nil {
			return nil, err
		}
	}
	after, err := r.q("/live/song/get/current_song_time", 0)
	if err != nil {
		return nil, err
	}
	b, a := firstFloat(before), firstFloat(after)
	return r.propChange("song", "current_song_time", b, a, map[string]any{
		"before_position": r.formatPos(b),
		"after_position":  r.formatPos(a),
	}), nil
}

func songProp(r *routeReq) (string, propSpec, error) {
	prop := strings.TrimSpace(r.vars["prop"])
	spec, ok := songProps[prop]
	if !ok {
		return "", propSpec{}, badInput("unknown song property %q: use one of %s", prop, propNames(songProps))
	}
	return prop, spec, nil
}

func hSongGet(r *routeReq) (any, error) {
	prop, spec, err := songProp(r)
	if err != nil {
		return nil, err
	}
	vals, err := r.q("/live/song/get/"+prop, 0)
	if err != nil {
		return nil, err
	}
	v := spec.display(first(vals))
	out := map[string]any{"property": prop, "value": v, "text": fmtValue(v)}
	if spec.kind == "position" {
		out["position"] = r.formatPos(firstFloat(vals))
	}
	return out, nil
}

// setSongProp reads, sets and reads back one song property.
func setSongProp(r *routeReq, prop string, spec propSpec, raw string) (map[string]any, error) {
	if spec.readOnly {
		return nil, badInput("%s is read-only", prop)
	}
	arg, err := spec.parse(r, prop, raw)
	if err != nil {
		return nil, err
	}
	before, err := r.q("/live/song/get/"+prop, 0)
	if err != nil {
		return nil, err
	}
	if err := r.send("/live/song/set/"+prop, arg); err != nil {
		return nil, err
	}
	after, err := r.q("/live/song/get/"+prop, 0)
	if err != nil {
		return nil, err
	}
	extra := map[string]any{"requested": raw}
	if spec.kind == "position" {
		extra["after_position"] = r.formatPos(firstFloat(after))
	}
	return r.propChange("song", prop, spec.display(first(before)), spec.display(first(after)), extra), nil
}

func hSongSet(r *routeReq) (any, error) {
	prop, spec, err := songProp(r)
	if err != nil {
		return nil, err
	}
	raw, ok := r.bodyString("value")
	if !ok {
		return nil, badInput("value is required")
	}
	return setSongProp(r, prop, spec, raw)
}

func hRecordSession(r *routeReq) (any, error) {
	on, err := r.requireBodyBool("on")
	if err != nil {
		return nil, err
	}
	return setSongProp(r, "session_record", songProps["session_record"], fmt.Sprint(on))
}

// ---------- locators ----------

type cuePoint struct {
	name string
	time float64
}

func (r *routeReq) cues() ([]cuePoint, error) {
	vals, err := r.q("/live/song/get/cue_points", 0)
	if err != nil {
		return nil, err
	}
	var out []cuePoint
	for i := 0; i+1 < len(vals); i += 2 {
		name, _ := live.AsString(vals[i])
		t, _ := live.AsFloat(vals[i+1])
		out = append(out, cuePoint{name: name, time: t})
	}
	return out, nil
}

func (r *routeReq) locatorRow(i int, c cuePoint) map[string]any {
	return map[string]any{"number": i + 1, "name": c.name, "beat": c.time, "position": r.formatPos(c.time)}
}

func (r *routeReq) resolveLocator(input string) (int, []cuePoint, error) {
	cues, err := r.cues()
	if err != nil {
		return -1, nil, err
	}
	names := make([]string, len(cues))
	for i, c := range cues {
		names[i] = c.name
	}
	i, err := live.Resolve("locator", input, names)
	if err != nil {
		return -1, nil, err
	}
	return i, cues, nil
}

func hLocatorList(r *routeReq) (any, error) {
	cues, err := r.cues()
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(cues))
	for i, c := range cues {
		rows = append(rows, r.locatorRow(i, c))
	}
	return rows, nil
}

func hLocatorAdd(r *routeReq) (any, error) {
	name, err := r.requireBodyString("name")
	if err != nil {
		return nil, err
	}
	at, err := r.requireBodyString("at")
	if err != nil {
		return nil, err
	}
	beats, err := r.position(at)
	if err != nil {
		return nil, err
	}
	if _, err := r.mcp("create_locator", map[string]any{"name": name, "time": beats}, 10*time.Second); err != nil {
		return nil, err
	}
	cues, err := r.cues()
	if err != nil {
		return nil, err
	}
	for i, c := range cues {
		if math.Abs(c.time-beats) < 1e-3 {
			return r.locatorRow(i, c), nil
		}
	}
	return nil, &routeErr{status: http.StatusInternalServerError, msg: fmt.Sprintf("AbletonMCP reported success but no locator is at %s", r.formatPos(beats))}
}

func hLocatorJump(r *routeReq) (any, error) {
	i, cues, err := r.resolveLocator(r.vars["locator"])
	if err != nil {
		return nil, err
	}
	if err := r.act("/live/song/cue_point/jump", int32(i)); err != nil {
		return nil, err
	}
	return ack("jump", fmt.Sprintf("locator %d %q", i+1, cues[i].name), r.formatPos(cues[i].time)), nil
}

func hLocatorRename(r *routeReq) (any, error) {
	name, err := r.requireBodyString("name")
	if err != nil {
		return nil, err
	}
	i, cues, err := r.resolveLocator(r.vars["locator"])
	if err != nil {
		return nil, err
	}
	if err := r.send("/live/song/cue_point/set/name", int32(i), name); err != nil {
		return nil, err
	}
	after, err := r.cues()
	if err != nil {
		return nil, err
	}
	if i >= len(after) {
		return nil, &routeErr{status: http.StatusInternalServerError, msg: "locator list changed while renaming"}
	}
	row := r.locatorRow(i, after[i])
	row["journal_id"] = r.journal(fmt.Sprintf("locator %d at %s", i+1, r.formatPos(cues[i].time)), "name", cues[i].name, after[i].name)
	return row, nil
}

func hLocatorDelete(r *routeReq) (any, error) {
	i, cues, err := r.resolveLocator(r.vars["locator"])
	if err != nil {
		return nil, err
	}
	playing, err := r.q("/live/song/get/is_playing", 0)
	if err != nil {
		return nil, err
	}
	if firstBool(playing) {
		return nil, badInput("stop playback first: deleting a locator moves the playhead onto it, which cannot be done reliably while Live is playing")
	}
	target := cues[i]
	orig, err := r.q("/live/song/get/current_song_time", 0)
	if err != nil {
		return nil, err
	}
	if err := r.send("/live/song/set/current_song_time", float32(target.time)); err != nil {
		return nil, err
	}
	if err := r.send("/live/song/cue_point/add_or_delete"); err != nil {
		return nil, err
	}
	after, err := r.cues()
	_ = r.send("/live/song/set/current_song_time", float32(firstFloat(orig)))
	if err != nil {
		return nil, err
	}
	stillThere := false
	for _, c := range after {
		if math.Abs(c.time-target.time) < 1e-3 {
			stillThere = true
		}
	}
	if len(after) > len(cues) {
		// The toggle added a locator instead of removing one: undo it.
		_ = r.send("/live/song/set/current_song_time", float32(target.time))
		_ = r.send("/live/song/cue_point/add_or_delete")
		_ = r.send("/live/song/set/current_song_time", float32(firstFloat(orig)))
		_, _ = r.cues()
		return nil, &routeErr{status: http.StatusInternalServerError, msg: "Live added a locator instead of deleting one; the extra locator was removed again"}
	}
	if stillThere || len(after) != len(cues)-1 {
		return nil, &routeErr{status: http.StatusInternalServerError, msg: fmt.Sprintf("locator %q is still there after the delete request", target.name)}
	}
	return ack("delete", fmt.Sprintf("locator %d %q", i+1, target.name), "was at "+r.formatPos(target.time)), nil
}

// ---------- misc ----------

func hMessage(r *routeReq) (any, error) {
	text, err := r.requireBodyString("text")
	if err != nil {
		return nil, err
	}
	if len(text) > 500 {
		return nil, badInput("message text is limited to 500 characters")
	}
	if err := r.act("/live/api/show_message", text); err != nil {
		return nil, err
	}
	return ack("message", "status bar", text), nil
}

func hSnapshot(r *routeReq) (any, error) {
	// Slice C: get_session_snapshot is only sent for 'snapshot save' and
	// 'snapshot diff --live', which mark their context (ableton_snapshot_gate.go).
	if !sessionSnapshotAllowed(r.ctx) {
		return nil, badInput("the whole-project snapshot is only read by 'ableton-pp-cli snapshot save <label>' and 'ableton-pp-cli snapshot diff <label> --live'")
	}
	notes, err := r.paramBool("notes", true)
	if err != nil {
		return nil, err
	}
	params, err := r.paramBool("params", true)
	if err != nil {
		return nil, err
	}
	raw, err := r.mcp("get_session_snapshot", map[string]any{"include_notes": notes, "include_params": params}, 60*time.Second)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func hHealth(r *routeReq) (any, error) {
	out := map[string]any{}
	oscOK := false
	o, err := r.conn()
	if err == nil {
		o.Timeout = 1500 * time.Millisecond
		var vals []any
		vals, err = o.Query(r.ctx, "/live/application/get/version", 0)
		if err == nil {
			oscOK = true
			v := "unknown"
			if len(vals) >= 2 {
				v = fmt.Sprintf("%d.%d", firstInt(vals[:1]), firstInt(vals[1:2]))
			}
			out[live.BridgeOSC] = "ok (Live " + v + ")"
		}
		o.Timeout = live.DefaultOSCTimeout
	}
	if !oscOK {
		out[live.BridgeOSC] = "not reachable: " + err.Error()
	}
	if raw, err := live.TCPCommand(r.ctx, live.BridgeMCP, abletonMCPAddr, "get_script_info", nil, live.TCPOptions{DialTimeout: time.Second, ReadTimeout: 3 * time.Second}); err == nil {
		var info struct {
			ScriptVersion   string `json:"script_version"`
			ProtocolVersion any    `json:"protocol_version"`
		}
		_ = json.Unmarshal(raw, &info)
		out[live.BridgeMCP] = fmt.Sprintf("ok (script %s, protocol %v)", info.ScriptVersion, info.ProtocolVersion)
	} else {
		out[live.BridgeMCP] = "not reachable: " + err.Error()
	}
	if raw, err := live.TCPCommand(r.ctx, live.BridgePPX, abletonPPXAddr, "version", nil, live.TCPOptions{DialTimeout: 600 * time.Millisecond, ReadTimeout: 3 * time.Second, HalfClose: true}); err == nil {
		var info struct {
			ScriptVersion   string `json:"script_version"`
			ProtocolVersion any    `json:"protocol_version"`
		}
		_ = json.Unmarshal(raw, &info)
		out[live.BridgePPX] = fmt.Sprintf("ok (script %s, protocol %v)", info.ScriptVersion, info.ProtocolVersion)
	} else {
		out[live.BridgePPX] = "not reachable: " + err.Error() + " (optional; install with 'ableton-pp-cli scripts install')"
	}
	if !oscOK {
		return nil, &routeErr{status: http.StatusServiceUnavailable, msg: fmt.Sprintf("AbletonOSC is not answering. %s | abletonosc: %v | abletonmcp: %v | abletonppextras: %v",
			helpOSC, out[live.BridgeOSC], out[live.BridgeMCP], out[live.BridgePPX])}
	}
	return out, nil
}

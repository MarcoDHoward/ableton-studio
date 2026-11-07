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

const maxSends = 12 // Live allows at most 12 return tracks

const volumeNote = "volume_db needs the AbletonPPExtras Remote Script (install it with 'ableton-pp-cli scripts install'); until then volume is the raw 0-1 fader value (0.85 = 0 dB)"

type mixerDBRow struct {
	Track  int     `json:"track"`
	Name   string  `json:"name"`
	Volume float64 `json:"volume"`
	DB     string  `json:"db"`
}

// mixerDB asks AbletonPPExtras for every track's volume in dB. It fails
// fast when the script is not installed.
func (r *routeReq) mixerDB() (map[int]mixerDBRow, error) {
	raw, err := live.TCPCommand(r.ctx, live.BridgePPX, abletonPPXAddr, "mixer_db", map[string]any{}, live.TCPOptions{DialTimeout: 300 * time.Millisecond, ReadTimeout: 3 * time.Second, HalfClose: true})
	if err != nil {
		return nil, err
	}
	var rows []mixerDBRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("unexpected mixer_db reply: %w", err)
	}
	out := map[int]mixerDBRow{}
	for _, row := range rows {
		out[row.Track] = row
	}
	return out, nil
}

func trackType(midi, audio, foldable bool) string {
	switch {
	case midi:
		return "midi"
	case audio:
		return "audio"
	case foldable:
		return "group"
	}
	return "other"
}

// returnCount is the number of return tracks (= sends per track). The
// AbletonMCP session info answers without touching Live's main thread; when
// it is not loaded, track t is probed send by send (each rejected index
// costs AbletonOSC a whole update tick, so the probe stops at the first).
func (r *routeReq) returnCount(t int) (int, error) {
	if r.returns >= 0 {
		return r.returns, nil
	}
	raw, err := live.TCPCommand(r.ctx, live.BridgeMCP, abletonMCPAddr, "get_session_info", nil, live.TCPOptions{DialTimeout: 300 * time.Millisecond, ReadTimeout: 2 * time.Second})
	if err == nil {
		var info struct {
			Returns *int `json:"return_track_count"`
		}
		if json.Unmarshal(raw, &info) == nil && info.Returns != nil {
			r.returns = *info.Returns
			return r.returns, nil
		}
	}
	if t < 0 {
		return 0, nil
	}
	n := 0
	for ; n < maxSends; n++ {
		_, errs, err := r.qmTolerant([]live.Request{live.Q("/live/track/get/send", 2, int32(t), int32(n))})
		if err != nil {
			return 0, err
		}
		if errs[0] != nil {
			break
		}
	}
	r.returns = n
	return n, nil
}

// loadTracks returns Track rows for tracks [min, max) (max -1 = all), the
// one-time note about volume_db, and the number of return tracks (-1 when
// there are no tracks to probe).
func (r *routeReq) loadTracks(min, max int) ([]map[string]any, string, int, error) {
	dbCh := make(chan map[int]mixerDBRow, 1)
	go func() {
		rows, err := r.mixerDB()
		if err != nil {
			rows = nil
		}
		dbCh <- rows
	}()
	props := []string{"track.name", "track.mute", "track.solo", "track.has_midi_input", "track.has_audio_input", "track.can_be_armed", "track.is_foldable"}
	args := []any{int32(min), int32(max)}
	for _, p := range props {
		args = append(args, p)
	}
	vals, err := r.q("/live/song/get/track_data", 0, args...)
	if err != nil {
		<-dbCh
		return nil, "", -1, err
	}
	n := len(vals) / len(props)
	type base struct {
		name                            string
		mute, solo, midi, audio, arm, f bool
	}
	bases := make([]base, n)
	for i := 0; i < n; i++ {
		v := vals[i*len(props):]
		name, _ := live.AsString(v[0])
		bases[i] = base{name: name, mute: firstBool(v[1:2]), solo: firstBool(v[2:3]), midi: firstBool(v[3:4]), audio: firstBool(v[4:5]), arm: firstBool(v[5:6]), f: firstBool(v[6:7])}
	}
	probe := -1
	if n > 0 {
		probe = min
	}
	returns, err := r.returnCount(probe)
	if err != nil {
		<-dbCh
		return nil, "", -1, err
	}
	sendVals := make([][]float64, n)
	// per-track mixer values
	var reqs []live.Request
	type slot struct{ track, kind, send int }
	var slots []slot
	for i := 0; i < n; i++ {
		t := int32(min + i)
		reqs = append(reqs, live.Q("/live/track/get/volume", 1, t), live.Q("/live/track/get/panning", 1, t))
		slots = append(slots, slot{i, 0, 0}, slot{i, 1, 0})
		if bases[i].arm {
			reqs = append(reqs, live.Q("/live/track/get/arm", 1, t))
			slots = append(slots, slot{i, 2, 0})
		}
		for s := 0; s < returns; s++ {
			reqs = append(reqs, live.Q("/live/track/get/send", 2, t, int32(s)))
			slots = append(slots, slot{i, 3, s})
		}
	}
	res, errs, err := r.qmTolerant(reqs)
	if err != nil {
		<-dbCh
		return nil, "", -1, err
	}
	volume := make([]any, n)
	pan := make([]any, n)
	arm := make([]any, n)
	for i := 0; i < n; i++ {
		sendVals[i] = make([]float64, returns)
	}
	for k, sl := range slots {
		if errs[k] != nil {
			continue
		}
		f := firstFloat(res[k])
		switch sl.kind {
		case 0:
			volume[sl.track] = f
		case 1:
			pan[sl.track] = f
		case 2:
			arm[sl.track] = firstBool(res[k])
		case 3:
			sendVals[sl.track][sl.send] = f
		}
	}
	dbRows := <-dbCh
	note := ""
	if dbRows == nil {
		note = volumeNote
	}
	rows := make([]map[string]any, n)
	for i := 0; i < n; i++ {
		idx := min + i
		sends := map[string]any{}
		for s, v := range sendVals[i] {
			sends[live.SendLetter(s)] = v
		}
		var volDB any
		row := map[string]any{
			"number": idx + 1,
			"name":   bases[i].name,
			"type":   trackType(bases[i].midi, bases[i].audio, bases[i].f),
			"volume": volume[i],
			"pan":    pan[i],
			"mute":   bases[i].mute,
			"solo":   bases[i].solo,
			"arm":    arm[i],
			"sends":  sends,
		}
		if dbRows != nil {
			if d, ok := dbRows[idx]; ok {
				row["volume_text"] = d.DB
				if f, ok := live.ParseDBText(d.DB); ok {
					volDB = f
				}
			}
		}
		row["volume_db"] = volDB
		rows[i] = row
	}
	return rows, note, returns, nil
}

func hTracks(r *routeReq) (any, error) {
	rows, note, _, err := r.loadTracks(0, -1)
	if err != nil {
		return nil, err
	}
	if note != "" && len(rows) > 0 {
		rows[0]["note"] = note
	}
	return rows, nil
}

// loadDevices lists a track's devices with type and on/off state.
func (r *routeReq) loadDevices(t int) ([]map[string]any, error) {
	res, err := r.qm([]live.Request{
		live.Q("/live/track/get/devices/name", 1, int32(t)),
		live.Q("/live/track/get/devices/type", 1, int32(t)),
		live.Q("/live/track/get/devices/class_name", 1, int32(t)),
	})
	if err != nil {
		return nil, err
	}
	names, types, classes := res[0], res[1], res[2]
	var reqs []live.Request
	for d := range names {
		reqs = append(reqs,
			live.Q("/live/device/get/parameter/name", 3, int32(t), int32(d), int32(0)),
			live.Q("/live/device/get/parameter/value", 3, int32(t), int32(d), int32(0)))
	}
	pres, perrs, err := r.qmTolerant(reqs)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(names))
	for d := range names {
		name, _ := live.AsString(names[d])
		class := ""
		if d < len(classes) {
			class, _ = live.AsString(classes[d])
		}
		code := 0
		if d < len(types) {
			code, _ = live.AsInt(types[d])
		}
		var on any
		if perrs[2*d] == nil && perrs[2*d+1] == nil && strings.EqualFold(firstString(pres[2*d]), "Device On") {
			on = firstFloat(pres[2*d+1]) >= 0.5
		}
		rows = append(rows, map[string]any{
			"number": d + 1, "name": name, "class_name": class,
			"type": live.DeviceTypeName(code), "type_code": code, "on": on,
		})
	}
	return rows, nil
}

func (r *routeReq) loadTrackClips(t int) ([]map[string]any, error) {
	res, err := r.qm([]live.Request{
		live.Q("/live/track/get/clips/name", 1, int32(t)),
		live.Q("/live/track/get/clips/length", 1, int32(t)),
	})
	if err != nil {
		return nil, err
	}
	num, den, err := r.signature()
	if err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	for s, v := range res[0] {
		if v == nil {
			continue
		}
		name, _ := live.AsString(v)
		length := 0.0
		if s < len(res[1]) {
			length, _ = live.AsFloat(res[1][s])
		}
		rows = append(rows, map[string]any{
			"slot": s + 1, "name": name, "length_beats": length,
			"bars": length / live.BeatsPerBar(num, den),
		})
	}
	return rows, nil
}

func hTrackGet(r *routeReq) (any, error) {
	t, _, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	rows, note, _, err := r.loadTracks(t, t+1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, notFound("track %d disappeared", t+1)
	}
	out := rows[0]
	if note != "" {
		out["note"] = note
	}
	if out["devices"], err = r.loadDevices(t); err != nil {
		return nil, err
	}
	if out["clips"], err = r.loadTrackClips(t); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *routeReq) numTracks() (int, error) {
	vals, err := r.q("/live/song/get/num_tracks", 0)
	if err != nil {
		return 0, err
	}
	return firstInt(vals), nil
}

func hTrackCreate(r *routeReq) (any, error) {
	typ, err := r.requireBodyString("type")
	if err != nil {
		return nil, err
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	name, hasName := r.bodyString("name")
	index, hasIndex, err := r.bodyInt("index")
	if err != nil {
		return nil, err
	}
	switch typ {
	case "return":
		if err := r.act("/live/song/create_return_track"); err != nil {
			return nil, err
		}
		out := map[string]any{"type": "return", "ok": true, "name": nil}
		if hasName || hasIndex {
			out["note"] = "return tracks are always added at the end and cannot be named through AbletonOSC; rename it in Live"
		}
		return out, nil
	case "midi", "audio":
	default:
		return nil, badInput("type must be midi, audio or return, got %q", typ)
	}
	before, err := r.numTracks()
	if err != nil {
		return nil, err
	}
	idx := -1
	if hasIndex {
		if index < 1 || index > before+1 {
			return nil, badInput("index must be between 1 and %d", before+1)
		}
		idx = index - 1
	}
	if err := r.act("/live/song/create_"+typ+"_track", int32(idx)); err != nil {
		return nil, err
	}
	after, err := r.numTracks()
	if err != nil {
		return nil, err
	}
	if after != before+1 {
		return nil, &routeErr{status: http.StatusInternalServerError, msg: fmt.Sprintf("Live has %d tracks after the create request (expected %d)", after, before+1)}
	}
	newIdx := idx
	if newIdx < 0 {
		newIdx = before
	}
	if hasName && name != "" {
		if err := r.send("/live/track/set/name", int32(newIdx), name); err != nil {
			return nil, err
		}
	}
	r.trackNames = nil
	rows, note, _, err := r.loadTracks(newIdx, newIdx+1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, &routeErr{status: http.StatusInternalServerError, msg: "the new track could not be read back"}
	}
	if note != "" {
		rows[0]["note"] = note
	}
	return rows[0], nil
}

func hTrackDelete(r *routeReq) (any, error) {
	t, name, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	before, err := r.numTracks()
	if err != nil {
		return nil, err
	}
	if err := r.act("/live/song/delete_track", int32(t)); err != nil {
		return nil, err
	}
	after, err := r.numTracks()
	if err != nil {
		return nil, err
	}
	if after != before-1 {
		return nil, &routeErr{status: http.StatusInternalServerError, msg: "Live did not delete the track"}
	}
	return ack("delete", trackTarget(t, name), ""), nil
}

func hTrackDuplicate(r *routeReq) (any, error) {
	t, name, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	before, err := r.numTracks()
	if err != nil {
		return nil, err
	}
	if err := r.act("/live/song/duplicate_track", int32(t)); err != nil {
		return nil, err
	}
	after, err := r.numTracks()
	if err != nil {
		return nil, err
	}
	if after != before+1 {
		return nil, &routeErr{status: http.StatusInternalServerError, msg: "Live did not duplicate the track"}
	}
	return ack("duplicate", trackTarget(t, name), fmt.Sprintf("copy is track %d", t+2)), nil
}

// setTrack reads, sets and reads back one AbletonOSC track property.
func (r *routeReq) setTrack(t int, name, oscProp, property string, arg any, disp func(any) any) (map[string]any, error) {
	before, err := r.q("/live/track/get/"+oscProp, 1, int32(t))
	if err != nil {
		return nil, err
	}
	if err := r.send("/live/track/set/"+oscProp, int32(t), arg); err != nil {
		return nil, err
	}
	after, err := r.q("/live/track/get/"+oscProp, 1, int32(t))
	if err != nil {
		return nil, err
	}
	return r.propChange(trackTarget(t, name), property, disp(first(before)), disp(first(after)), nil), nil
}

func dispBool(v any) any   { b, _ := live.AsBool(v); return b }
func dispFloat(v any) any  { f, _ := live.AsFloat(v); return f }
func dispInt(v any) any    { i, _ := live.AsInt(v); return i }
func dispString(v any) any { s, _ := live.AsString(v); return s }

func hTrackBool(prop string) routeHandler {
	return func(r *routeReq) (any, error) {
		on, err := r.requireBodyBool("on")
		if err != nil {
			return nil, err
		}
		t, name, err := r.trackVar()
		if err != nil {
			return nil, err
		}
		if prop == "arm" {
			can, err := r.q("/live/track/get/can_be_armed", 1, int32(t))
			if err != nil {
				return nil, err
			}
			if !firstBool(can) {
				return nil, badInput("%s cannot be armed (group tracks and tracks without input cannot record)", trackTarget(t, name))
			}
		}
		return r.setTrack(t, name, prop, prop, on, dispBool)
	}
}

func hTrackRename(r *routeReq) (any, error) {
	newName, err := r.requireBodyString("name")
	if err != nil {
		return nil, err
	}
	t, name, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	return r.setTrack(t, name, "name", "name", newName, dispString)
}

func hTrackColor(r *routeReq) (any, error) {
	idx, ok, err := r.bodyInt("index")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, badInput("index is required (0-69)")
	}
	if idx < 0 || idx > 69 {
		return nil, badInput("color index must be between 0 and 69, got %d", idx)
	}
	t, name, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	return r.setTrack(t, name, "color_index", "color_index", int32(idx), dispInt)
}

func hTrackPan(r *routeReq) (any, error) {
	v, err := r.requireBodyFloat("value")
	if err != nil {
		return nil, err
	}
	if v < -1 || v > 1 || math.IsNaN(v) {
		return nil, badInput("pan must be between -1 (left) and 1 (right), got %v", v)
	}
	t, name, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	return r.setTrack(t, name, "panning", "pan", float32(v), dispFloat)
}

func hTrackVolume(r *routeReq) (any, error) {
	db, hasDB, err := r.bodyFloat("db")
	if err != nil {
		return nil, err
	}
	fader, hasFader, err := r.bodyFloat("fader")
	if err != nil {
		return nil, err
	}
	if hasDB == hasFader {
		return nil, badInput("give exactly one of --db (e.g. -6) or --fader (0-1, 0.85 = 0 dB)")
	}
	t, name, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	if hasFader {
		if fader < 0 || fader > 1 || math.IsNaN(fader) {
			return nil, badInput("fader must be between 0 and 1 (0.85 = 0 dB), got %v", fader)
		}
		return r.setTrack(t, name, "volume", "volume", float32(fader), dispFloat)
	}
	if math.IsNaN(db) || db > 6 || db < -70 {
		return nil, badInput("db must be between -70 and +6, got %v", db)
	}
	raw, err := r.ppx("set_volume_db", map[string]any{"track": t, "db": db})
	if err != nil {
		if ue, ok := err.(*live.UnavailableError); ok {
			return nil, &routeErr{status: http.StatusServiceUnavailable, msg: "setting volume in dB needs the AbletonPPExtras Remote Script (" + strings.TrimRight(ue.Detail, ".") + "). Install it with 'ableton-pp-cli scripts install' and select AbletonPPExtras as a Control Surface in Live > Settings > Link, Tempo & MIDI (Input/Output None), or use --fader 0-1 (0.85 = 0 dB) instead."}
		}
		return nil, err
	}
	var res struct {
		BeforeDB string  `json:"before_db"`
		AfterDB  string  `json:"after_db"`
		Value    float64 `json:"value"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("unexpected set_volume_db reply: %w", err)
	}
	dbVal := func(s string) any {
		if f, ok := live.ParseDBText(s); ok {
			return f
		}
		return s
	}
	return r.propChange(trackTarget(t, name), "volume_db", dbVal(res.BeforeDB), dbVal(res.AfterDB), map[string]any{
		"before_text": res.BeforeDB, "after_text": res.AfterDB, "fader": res.Value,
	}), nil
}

func hTrackSend(r *routeReq) (any, error) {
	v, err := r.requireBodyFloat("value")
	if err != nil {
		return nil, err
	}
	if v < 0 || v > 1 || math.IsNaN(v) {
		return nil, badInput("send level must be between 0 and 1, got %v", v)
	}
	t, name, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	count, err := r.returnCount(t)
	if err != nil {
		return nil, err
	}
	s, err := live.ResolveSend(r.vars["send"], count)
	if err != nil {
		return nil, err
	}
	before, err := r.q("/live/track/get/send", 2, int32(t), int32(s))
	if err != nil {
		return nil, err
	}
	if err := r.send("/live/track/set/send", int32(t), int32(s), float32(v)); err != nil {
		return nil, err
	}
	after, err := r.q("/live/track/get/send", 2, int32(t), int32(s))
	if err != nil {
		return nil, err
	}
	return r.propChange(trackTarget(t, name), "send "+live.SendLetter(s), firstFloat(before), firstFloat(after), nil), nil
}

// ---------- routing ----------

var routingKeys = []string{
	"input_routing_type", "input_routing_channel", "output_routing_type", "output_routing_channel",
	"current_monitoring_state", "available_input_routing_types", "available_input_routing_channels",
	"available_output_routing_types", "available_output_routing_channels",
}

func (r *routeReq) loadRouting(t int, name string) (map[string]any, error) {
	reqs := make([]live.Request, len(routingKeys))
	for i, k := range routingKeys {
		reqs[i] = live.Q("/live/track/get/"+k, 1, int32(t))
	}
	res, errs, err := r.qmTolerant(reqs)
	if err != nil {
		return nil, err
	}
	str := func(i int) any {
		if errs[i] != nil || len(res[i]) == 0 || res[i][0] == nil {
			return nil
		}
		s, _ := live.AsString(res[i][0])
		return s
	}
	list := func(i int) []string {
		out := []string{}
		if errs[i] != nil {
			return out
		}
		for _, v := range res[i] {
			if s, ok := live.AsString(v); ok {
				out = append(out, s)
			}
		}
		return out
	}
	var monitoring any
	if errs[4] == nil && len(res[4]) > 0 && res[4][0] != nil {
		if code, ok := live.AsInt(res[4][0]); ok {
			monitoring = live.Monitoring.Name(code)
		}
	}
	return map[string]any{
		"track":                     trackTarget(t, name),
		"input_type":                str(0),
		"input_channel":             str(1),
		"output_type":               str(2),
		"output_channel":            str(3),
		"monitoring":                monitoring,
		"available_inputs":          list(5),
		"available_input_channels":  list(6),
		"available_outputs":         list(7),
		"available_output_channels": list(8),
	}, nil
}

func hRoutingGet(r *routeReq) (any, error) {
	t, name, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	return r.loadRouting(t, name)
}

func pickName(kind, input string, options []string) (string, error) {
	for _, o := range options {
		if strings.EqualFold(strings.TrimSpace(o), strings.TrimSpace(input)) {
			return o, nil
		}
	}
	return "", badInput("%s %q is not available; choose one of: %s", kind, input, strings.Join(options, ", "))
}

func hRoutingSet(r *routeReq) (any, error) {
	t, name, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	fields := []string{"input_type", "input_channel", "output_type", "output_channel", "monitoring"}
	given := map[string]string{}
	for _, f := range fields {
		if v, ok := r.bodyString(f); ok && strings.TrimSpace(v) != "" {
			given[f] = v
		}
	}
	if len(given) == 0 {
		return nil, badInput("give at least one of --input-type, --input-channel, --output-type, --output-channel, --monitoring")
	}
	before, err := r.loadRouting(t, name)
	if err != nil {
		return nil, err
	}
	var ids []any
	apply := func(field, oscProp, availKey string) error {
		v, ok := given[field]
		if !ok {
			return nil
		}
		cur, err := r.loadRouting(t, name)
		if err != nil {
			return err
		}
		choice, err := pickName(strings.ReplaceAll(field, "_", " "), v, cur[availKey].([]string))
		if err != nil {
			return err
		}
		if err := r.send("/live/track/set/"+oscProp, int32(t), choice); err != nil {
			return err
		}
		after, err := r.q("/live/track/get/"+oscProp, 1, int32(t))
		if err != nil {
			return err
		}
		ids = append(ids, r.journal(trackTarget(t, name), field, cur[field], firstString(after)))
		return nil
	}
	if err := apply("input_type", "input_routing_type", "available_inputs"); err != nil {
		return nil, err
	}
	if err := apply("input_channel", "input_routing_channel", "available_input_channels"); err != nil {
		return nil, err
	}
	if err := apply("output_type", "output_routing_type", "available_outputs"); err != nil {
		return nil, err
	}
	if err := apply("output_channel", "output_routing_channel", "available_output_channels"); err != nil {
		return nil, err
	}
	if v, ok := given["monitoring"]; ok {
		code, err := live.Monitoring.Parse(v)
		if err != nil {
			return nil, badInput("%v", err)
		}
		if err := r.send("/live/track/set/current_monitoring_state", int32(t), int32(code)); err != nil {
			return nil, err
		}
		after, err := r.q("/live/track/get/current_monitoring_state", 1, int32(t))
		if err != nil {
			return nil, err
		}
		ids = append(ids, r.journal(trackTarget(t, name), "monitoring", before["monitoring"], live.Monitoring.Name(firstInt(after))))
	}
	out, err := r.loadRouting(t, name)
	if err != nil {
		return nil, err
	}
	out["journal_ids"] = ids
	return out, nil
}

func hMeters(r *routeReq) (any, error) {
	names, err := r.tracks()
	if err != nil {
		return nil, err
	}
	var reqs []live.Request
	for t := range names {
		reqs = append(reqs,
			live.Q("/live/track/get/output_meter_level", 1, int32(t)),
			live.Q("/live/track/get/output_meter_left", 1, int32(t)),
			live.Q("/live/track/get/output_meter_right", 1, int32(t)))
	}
	res, errs, err := r.qmTolerant(reqs)
	if err != nil {
		return nil, err
	}
	val := func(i int) any {
		if errs[i] != nil || len(res[i]) == 0 || res[i][0] == nil {
			return nil
		}
		return firstFloat(res[i])
	}
	rows := make([]map[string]any, len(names))
	for t, name := range names {
		rows[t] = map[string]any{"number": t + 1, "name": name, "level": val(3 * t), "left": val(3*t + 1), "right": val(3*t + 2)}
	}
	return rows, nil
}

// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package client

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"ableton-pp-cli/internal/live"
)

func clipTarget(t int, tname string, s int) string {
	return fmt.Sprintf("clip %d on track %d %q", s+1, t+1, tname)
}

// clipVars resolves {track} and {slot}.
func (r *routeReq) clipVars() (int, string, int, error) {
	t, name, err := r.trackVar()
	if err != nil {
		return -1, "", -1, err
	}
	s, err := r.slotVar("slot")
	if err != nil {
		return -1, "", -1, err
	}
	return t, name, s, nil
}

func (r *routeReq) hasClip(t, s int) (bool, error) {
	vals, err := r.q("/live/clip_slot/get/has_clip", 2, int32(t), int32(s))
	if err != nil {
		return false, err
	}
	return firstBool(vals), nil
}

func (r *routeReq) requireClip(t int, tname string, s int) error {
	has, err := r.hasClip(t, s)
	if err != nil {
		return err
	}
	if !has {
		return notFound("slot %d on track %d %q is empty", s+1, t+1, tname)
	}
	return nil
}

func (r *routeReq) requireMidiClip(t int, tname string, s int) error {
	if err := r.requireClip(t, tname, s); err != nil {
		return err
	}
	vals, err := r.q("/live/clip/get/is_midi_clip", 2, int32(t), int32(s))
	if err != nil {
		return err
	}
	if !firstBool(vals) {
		return badInput("%s is an audio clip; notes only exist in MIDI clips", clipTarget(t, tname, s))
	}
	return nil
}

func hClipGrid(r *routeReq) (any, error) {
	names, err := r.tracks()
	if err != nil {
		return nil, err
	}
	num, den, err := r.signature()
	if err != nil {
		return nil, err
	}
	var reqs []live.Request
	for t := range names {
		reqs = append(reqs, live.Q("/live/track/get/clips/name", 1, int32(t)), live.Q("/live/track/get/clips/length", 1, int32(t)))
	}
	res, err := r.qm(reqs)
	if err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	for t, tname := range names {
		cn, cl := res[2*t], res[2*t+1]
		for s, v := range cn {
			if v == nil {
				continue
			}
			name, _ := live.AsString(v)
			length := 0.0
			if s < len(cl) {
				length, _ = live.AsFloat(cl[s])
			}
			rows = append(rows, map[string]any{
				"track": tname, "track_number": t + 1, "slot": s + 1, "name": name,
				"length_beats": length, "bars": length / live.BeatsPerBar(num, den),
			})
		}
	}
	return rows, nil
}

var clipGetProps = []string{
	"name", "length", "loop_start", "loop_end", "is_playing", "muted", "is_midi_clip", "is_audio_clip",
	"color_index", "launch_mode", "launch_quantization", "legato", "looping", "start_marker", "end_marker",
	"velocity_amount", "is_recording", "warping", "warp_mode", "gain", "pitch_coarse", "pitch_fine", "ram_mode", "file_path",
}

func (r *routeReq) loadClip(t int, tname string, s int) (map[string]any, error) {
	if err := r.requireClip(t, tname, s); err != nil {
		return nil, err
	}
	reqs := make([]live.Request, len(clipGetProps))
	for i, p := range clipGetProps {
		reqs[i] = live.Q("/live/clip/get/"+p, 2, int32(t), int32(s))
	}
	res, errs, err := r.qmTolerant(reqs)
	if err != nil {
		return nil, err
	}
	raw := map[string]any{}
	for i, p := range clipGetProps {
		if errs[i] == nil && len(res[i]) > 0 {
			raw[p] = res[i][0]
		}
	}
	num, den, err := r.signature()
	if err != nil {
		return nil, err
	}
	length, _ := live.AsFloat(raw["length"])
	isMidi, _ := live.AsBool(raw["is_midi_clip"])
	out := map[string]any{
		"track":        tname,
		"track_number": t + 1,
		"slot":         s + 1,
		"name":         dispString(raw["name"]),
		"type":         map[bool]string{true: "midi", false: "audio"}[isMidi],
		"length_beats": length,
		"bars":         length / live.BeatsPerBar(num, den),
	}
	for _, p := range []string{"loop_start", "loop_end", "start_marker", "end_marker", "velocity_amount"} {
		if v, ok := raw[p]; ok && v != nil {
			out[p] = dispFloat(v)
		}
	}
	for _, p := range []string{"is_playing", "muted", "legato", "looping", "is_recording"} {
		if v, ok := raw[p]; ok && v != nil {
			out[p] = dispBool(v)
		}
	}
	if v, ok := raw["color_index"]; ok && v != nil {
		out["color_index"] = dispInt(v)
	}
	if v, ok := live.AsInt(raw["launch_mode"]); ok {
		out["launch_mode"] = live.LaunchMode.Name(v)
	}
	if v, ok := live.AsInt(raw["launch_quantization"]); ok {
		out["launch_quantization"] = live.ClipLaunchQuantization.Name(v)
	}
	if !isMidi {
		if v, ok := raw["warping"]; ok && v != nil {
			out["warping"] = dispBool(v)
		}
		if v, ok := live.AsInt(raw["warp_mode"]); ok {
			out["warp_mode"] = live.WarpMode.Name(v)
		}
		for _, p := range []string{"gain", "pitch_fine"} {
			if v, ok := raw[p]; ok && v != nil {
				out[p] = dispFloat(v)
			}
		}
		if v, ok := raw["pitch_coarse"]; ok && v != nil {
			out["pitch_coarse"] = dispInt(v)
		}
		if v, ok := raw["ram_mode"]; ok && v != nil {
			out["ram_mode"] = dispBool(v)
		}
		if v, ok := raw["file_path"]; ok && v != nil {
			out["file_path"] = dispString(v)
		}
	}
	return out, nil
}

func hClipGet(r *routeReq) (any, error) {
	t, name, s, err := r.clipVars()
	if err != nil {
		return nil, err
	}
	return r.loadClip(t, name, s)
}

func hClipCreate(r *routeReq) (any, error) {
	bars, hasBars, err := r.bodyFloat("bars")
	if err != nil {
		return nil, err
	}
	if !hasBars {
		bars = 4
	}
	if bars <= 0 || bars > 1024 || math.IsNaN(bars) {
		return nil, badInput("bars must be more than 0 and at most 1024, got %v", bars)
	}
	clipName, hasName := r.bodyString("name")
	t, tname, s, err := r.clipVars()
	if err != nil {
		return nil, err
	}
	midi, err := r.q("/live/track/get/has_midi_input", 1, int32(t))
	if err != nil {
		return nil, err
	}
	if !firstBool(midi) {
		return nil, badInput("track %d %q is not a MIDI track; 'clip create' makes empty MIDI clips (use 'clip import-audio' for audio)", t+1, tname)
	}
	has, err := r.hasClip(t, s)
	if err != nil {
		return nil, err
	}
	if has {
		return nil, conflict("slot %d on track %d %q already has a clip", s+1, t+1, tname)
	}
	num, den, err := r.signature()
	if err != nil {
		return nil, err
	}
	beats := live.BarsToBeats(bars, num, den)
	if err := r.act("/live/clip_slot/create_clip", int32(t), int32(s), float32(beats)); err != nil {
		return nil, err
	}
	if hasName && clipName != "" {
		if err := r.send("/live/clip/set/name", int32(t), int32(s), clipName); err != nil {
			return nil, err
		}
	}
	return r.loadClip(t, tname, s)
}

func hClipDelete(r *routeReq) (any, error) {
	t, tname, s, err := r.clipVars()
	if err != nil {
		return nil, err
	}
	if err := r.requireClip(t, tname, s); err != nil {
		return nil, err
	}
	nameVals, _ := r.q("/live/clip/get/name", 2, int32(t), int32(s))
	if err := r.send("/live/clip_slot/delete_clip", int32(t), int32(s)); err != nil {
		return nil, err
	}
	has, err := r.hasClip(t, s)
	if err != nil {
		return nil, err
	}
	if has {
		return nil, &routeErr{status: http.StatusInternalServerError, msg: "Live did not delete the clip"}
	}
	return ack("delete", clipTarget(t, tname, s), fmt.Sprintf("deleted %q", firstString(nameVals))), nil
}

func hClipSet(r *routeReq) (any, error) {
	prop := strings.TrimSpace(r.vars["prop"])
	spec, ok := clipProps[prop]
	if !ok {
		return nil, badInput("unknown clip property %q: use one of %s", prop, propNames(clipProps))
	}
	raw, ok := r.bodyString("value")
	if !ok {
		return nil, badInput("value is required")
	}
	t, tname, s, err := r.clipVars()
	if err != nil {
		return nil, err
	}
	if err := r.requireClip(t, tname, s); err != nil {
		return nil, err
	}
	arg, err := spec.parse(r, prop, raw)
	if err != nil {
		return nil, err
	}
	before, err := r.q("/live/clip/get/"+prop, 2, int32(t), int32(s))
	if err != nil {
		return nil, err
	}
	if err := r.send("/live/clip/set/"+prop, int32(t), int32(s), arg); err != nil {
		return nil, err
	}
	after, err := r.q("/live/clip/get/"+prop, 2, int32(t), int32(s))
	if err != nil {
		return nil, err
	}
	return r.propChange(clipTarget(t, tname, s), prop, spec.display(first(before)), spec.display(first(after)), map[string]any{"requested": raw}), nil
}

func hClipDuplicate(r *routeReq) (any, error) {
	toSlot, ok, err := r.bodyInt("to_slot")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, badInput("to_slot is required (a 1-based empty slot)")
	}
	t, tname, s, err := r.clipVars()
	if err != nil {
		return nil, err
	}
	if err := r.requireClip(t, tname, s); err != nil {
		return nil, err
	}
	tt, ttname := t, tname
	if v, ok := r.bodyString("to_track"); ok && strings.TrimSpace(v) != "" {
		if tt, ttname, err = r.resolveTrack(v); err != nil {
			return nil, err
		}
	}
	ts, err := r.checkSlot(fmt.Sprint(toSlot))
	if err != nil {
		return nil, err
	}
	if tt == t && ts == s {
		return nil, badInput("source and destination are the same slot")
	}
	has, err := r.hasClip(tt, ts)
	if err != nil {
		return nil, err
	}
	if has {
		return nil, conflict("slot %d on track %d %q already has a clip; pick an empty slot", ts+1, tt+1, ttname)
	}
	if err := r.act("/live/clip_slot/duplicate_clip_to", int32(t), int32(s), int32(tt), int32(ts)); err != nil {
		return nil, err
	}
	if has, err = r.hasClip(tt, ts); err != nil {
		return nil, err
	}
	if !has {
		return nil, &routeErr{status: http.StatusInternalServerError, msg: "Live did not copy the clip (the tracks may be of different types)"}
	}
	return ack("duplicate", clipTarget(t, tname, s), "copied to "+clipTarget(tt, ttname, ts)), nil
}

func hClipMethod(address, action string) routeHandler {
	return func(r *routeReq) (any, error) {
		t, tname, s, err := r.clipVars()
		if err != nil {
			return nil, err
		}
		if err := r.requireClip(t, tname, s); err != nil {
			return nil, err
		}
		if err := r.act(address, int32(t), int32(s)); err != nil {
			return nil, err
		}
		return ack(action, clipTarget(t, tname, s), ""), nil
	}
}

func hClipToArrangement(r *routeReq) (any, error) {
	at, err := r.requireBodyString("at")
	if err != nil {
		return nil, err
	}
	t, tname, s, err := r.clipVars()
	if err != nil {
		return nil, err
	}
	if err := r.requireClip(t, tname, s); err != nil {
		return nil, err
	}
	beats, err := r.position(at)
	if err != nil {
		return nil, err
	}
	if _, err := r.mcp("duplicate_session_clip_to_arrangement", map[string]any{"track_index": t, "clip_index": s, "destination_time": beats}, 10*time.Second); err != nil {
		return nil, err
	}
	return ack("to-arrangement", clipTarget(t, tname, s), fmt.Sprintf("placed at %s (beat %v)", r.formatPos(beats), beats)), nil
}

var audioExts = map[string]bool{".wav": true, ".aif": true, ".aiff": true, ".mp3": true, ".flac": true}

// validateAudioPath accepts only an existing local absolute file. UNC and
// other network paths are refused before anything is sent to Live, since
// Live would open them and an SMB path can leak NTLM credentials.
func validateAudioPath(p string) (string, error) {
	path := strings.TrimSpace(p)
	if path == "" {
		return "", badInput("file is required")
	}
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, `//`) || strings.HasPrefix(path, `\/`) || strings.HasPrefix(path, `/\`) {
		return "", badInput("network paths are refused (%s): copy the file to a local drive first", path)
	}
	if strings.Contains(strings.ToLower(path), "://") {
		return "", badInput("URLs are refused: give a local file path")
	}
	if !filepath.IsAbs(path) {
		return "", badInput("file must be an absolute local path, got %q", path)
	}
	if vol := filepath.VolumeName(path); strings.HasPrefix(vol, `\\`) || strings.HasPrefix(vol, `//`) {
		return "", badInput("network paths are refused (%s): copy the file to a local drive first", path)
	}
	clean := filepath.Clean(path)
	if !localDrivePath(clean) {
		return "", badInput("file must be on a local drive (e.g. C:\\Samples\\kick.wav), got %q", path)
	}
	if !audioExts[strings.ToLower(filepath.Ext(clean))] {
		return "", badInput("file must be a .wav, .aif, .aiff, .mp3 or .flac file")
	}
	// A symlink or junction could still lead to a network share.
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		if !localDrivePath(resolved) {
			return "", badInput("network paths are refused (%s resolves to %s): copy the file to a local drive first", clean, resolved)
		}
		clean = resolved
	}
	st, err := os.Stat(clean)
	if err != nil {
		return "", notFound("file %s not found: %v", clean, err)
	}
	if !st.Mode().IsRegular() {
		return "", badInput("%s is not a regular file", clean)
	}
	return clean, nil
}

// localDrivePath reports whether p is an absolute path on a local drive:
// "X:\..." on Windows (no UNC, \\?\ or \??\ device paths), "/..." but not
// "//..." elsewhere.
func localDrivePath(p string) bool {
	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(p)
		if len(vol) != 2 || vol[1] != ':' {
			return false
		}
		c := vol[0] | 0x20
		return c >= 'a' && c <= 'z' && len(p) > 2 && (p[2] == '\\' || p[2] == '/')
	}
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//")
}

func hClipImportAudio(r *routeReq) (any, error) {
	raw, _ := r.bodyString("file")
	file, err := validateAudioPath(raw)
	if err != nil {
		return nil, err
	}
	t, tname, s, err := r.clipVars()
	if err != nil {
		return nil, err
	}
	res, err := r.qm([]live.Request{live.Q("/live/track/get/has_midi_input", 1, int32(t)), live.Q("/live/track/get/has_audio_input", 1, int32(t))})
	if err != nil {
		return nil, err
	}
	if firstBool(res[0]) || !firstBool(res[1]) {
		return nil, badInput("track %d %q is not an audio track", t+1, tname)
	}
	has, err := r.hasClip(t, s)
	if err != nil {
		return nil, err
	}
	if has {
		return nil, conflict("slot %d on track %d %q already has a clip", s+1, t+1, tname)
	}
	if _, err := r.mcp("create_audio_clip", map[string]any{"track_index": t, "clip_index": s, "path": file}, 65*time.Second); err != nil {
		return nil, err
	}
	return ack("import-audio", clipTarget(t, tname, s), file), nil
}

// ---------- notes ----------

func (r *routeReq) readNotes(t, s int) ([]live.Note, error) {
	vals, err := r.q("/live/clip/get/notes", 2, int32(t), int32(s))
	if err != nil {
		return nil, err
	}
	return live.NotesFromFlat(vals)
}

func hNotesGet(r *routeReq) (any, error) {
	t, tname, s, err := r.clipVars()
	if err != nil {
		return nil, err
	}
	if err := r.requireMidiClip(t, tname, s); err != nil {
		return nil, err
	}
	return r.readNotes(t, s)
}

func hNotesAdd(r *routeReq) (any, error) {
	rawNotes, ok := r.body["notes"]
	if !ok || rawNotes == nil {
		return nil, badInput("notes is required: a JSON array of {pitch, start, duration, velocity, mute}")
	}
	notes, err := live.ParseNotes(rawNotes)
	if err != nil {
		return nil, badInput("%v", err)
	}
	if len(notes) == 0 {
		return nil, badInput("notes is empty")
	}
	t, tname, s, err := r.clipVars()
	if err != nil {
		return nil, err
	}
	if err := r.requireMidiClip(t, tname, s); err != nil {
		return nil, err
	}
	before, err := r.readNotes(t, s)
	if err != nil {
		return nil, err
	}
	for _, chunk := range live.ChunkNotes(notes, 64) {
		args := append([]any{int32(t), int32(s)}, live.FlattenNotes(chunk)...)
		if err := r.send("/live/clip/add/notes", args...); err != nil {
			return nil, err
		}
	}
	after, err := r.readNotes(t, s)
	if err != nil {
		return nil, err
	}
	out := ack("add notes", clipTarget(t, tname, s), fmt.Sprintf("sent %d notes; clip has %d notes (was %d)", len(notes), len(after), len(before)))
	out["added"] = len(after) - len(before)
	return out, nil
}

func hNotesClear(r *routeReq) (any, error) {
	pf, hasPF, err := r.paramFloat("pitch_from")
	if err != nil {
		return nil, err
	}
	pt, hasPT, err := r.paramFloat("pitch_to")
	if err != nil {
		return nil, err
	}
	tf, hasTF, err := r.paramFloat("time_from")
	if err != nil {
		return nil, err
	}
	tt, hasTT, err := r.paramFloat("time_to")
	if err != nil {
		return nil, err
	}
	ranged := hasPF || hasPT || hasTF || hasTT
	if !hasPF {
		pf = 0
	}
	if !hasPT {
		pt = 127
	}
	if pf < 0 || pt > 127 || pf > pt || pf != math.Trunc(pf) || pt != math.Trunc(pt) {
		return nil, badInput("pitch range must be whole numbers with 0 <= pitch_from <= pitch_to <= 127")
	}
	timeStart, timeSpan := -8192.0, 16384.0
	if hasTF || hasTT {
		if !hasTF {
			tf = 0
		}
		if tf < 0 {
			return nil, badInput("time_from must be 0 or more beats")
		}
		timeStart = tf
		if hasTT {
			if tt <= tf {
				return nil, badInput("time_to must be after time_from")
			}
			timeSpan = tt - tf
		}
	}
	t, tname, s, err := r.clipVars()
	if err != nil {
		return nil, err
	}
	if err := r.requireMidiClip(t, tname, s); err != nil {
		return nil, err
	}
	before, err := r.readNotes(t, s)
	if err != nil {
		return nil, err
	}
	if ranged {
		err = r.send("/live/clip/remove/notes", int32(t), int32(s), int32(pf), int32(pt-pf+1), float32(timeStart), float32(timeSpan))
	} else {
		err = r.send("/live/clip/remove/notes", int32(t), int32(s))
	}
	if err != nil {
		return nil, err
	}
	after, err := r.readNotes(t, s)
	if err != nil {
		return nil, err
	}
	out := ack("clear notes", clipTarget(t, tname, s), fmt.Sprintf("removed %d notes; %d left", len(before)-len(after), len(after)))
	out["removed"] = len(before) - len(after)
	return out, nil
}

// ---------- scenes ----------

func (r *routeReq) sceneRows(indexes []int) ([]map[string]any, error) {
	names, err := r.scenes()
	if err != nil {
		return nil, err
	}
	props := []string{"tempo", "tempo_enabled", "time_signature_numerator", "time_signature_denominator", "time_signature_enabled", "color_index"}
	var reqs []live.Request
	for _, i := range indexes {
		for _, p := range props {
			reqs = append(reqs, live.Q("/live/scene/get/"+p, 1, int32(i)))
		}
	}
	res, errs, err := r.qmTolerant(reqs)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(indexes))
	for k, i := range indexes {
		get := func(j int) any {
			n := k*len(props) + j
			if errs[n] != nil || len(res[n]) == 0 {
				return nil
			}
			return res[n][0]
		}
		name := ""
		if i < len(names) {
			name = names[i]
		}
		num, _ := live.AsInt(get(2))
		den, _ := live.AsInt(get(3))
		sig := ""
		if live.ValidSignature(num, den) {
			sig = live.FormatSignature(num, den)
		}
		var tempo any
		if f, ok := live.AsFloat(get(0)); ok && f > 0 {
			tempo = f // Live reports -1 when the scene has no tempo of its own
		}
		rows = append(rows, map[string]any{
			"number":            i + 1,
			"name":              name,
			"tempo":             tempo,
			"tempo_enabled":     dispBool(get(1)),
			"signature":         sig,
			"signature_enabled": dispBool(get(4)),
			"color_index":       dispInt(get(5)),
		})
	}
	return rows, nil
}

func hScenes(r *routeReq) (any, error) {
	names, err := r.scenes()
	if err != nil {
		return nil, err
	}
	idx := make([]int, len(names))
	for i := range idx {
		idx[i] = i
	}
	return r.sceneRows(idx)
}

func hSceneFire(r *routeReq) (any, error) {
	i, name, err := r.resolveScene(r.vars["scene"])
	if err != nil {
		return nil, err
	}
	if err := r.act("/live/scene/fire", int32(i)); err != nil {
		return nil, err
	}
	return ack("fire", fmt.Sprintf("scene %d %q", i+1, name), ""), nil
}

func hSceneCreate(r *routeReq) (any, error) {
	name, hasName := r.bodyString("name")
	index, hasIndex, err := r.bodyInt("index")
	if err != nil {
		return nil, err
	}
	before, err := r.numScenes()
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
	if err := r.act("/live/song/create_scene", int32(idx)); err != nil {
		return nil, err
	}
	after, err := r.numScenes()
	if err != nil {
		return nil, err
	}
	if after != before+1 {
		return nil, &routeErr{status: http.StatusInternalServerError, msg: "Live did not create the scene"}
	}
	newIdx := idx
	if newIdx < 0 {
		newIdx = before
	}
	if hasName && name != "" {
		if err := r.send("/live/scene/set/name", int32(newIdx), name); err != nil {
			return nil, err
		}
	}
	r.sceneNames = nil
	rows, err := r.sceneRows([]int{newIdx})
	if err != nil {
		return nil, err
	}
	return rows[0], nil
}

func hSceneDelete(r *routeReq) (any, error) {
	i, name, err := r.resolveScene(r.vars["scene"])
	if err != nil {
		return nil, err
	}
	before := len(r.sceneNames)
	if err := r.act("/live/song/delete_scene", int32(i)); err != nil {
		return nil, err
	}
	r.sceneNames = nil
	after, err := r.numScenes()
	if err != nil {
		return nil, err
	}
	if after != before-1 {
		return nil, &routeErr{status: http.StatusInternalServerError, msg: "Live did not delete the scene (a set always keeps at least one)"}
	}
	return ack("delete", fmt.Sprintf("scene %d %q", i+1, name), ""), nil
}

func hSceneDuplicate(r *routeReq) (any, error) {
	i, name, err := r.resolveScene(r.vars["scene"])
	if err != nil {
		return nil, err
	}
	before := len(r.sceneNames)
	if err := r.act("/live/song/duplicate_scene", int32(i)); err != nil {
		return nil, err
	}
	r.sceneNames = nil
	after, err := r.numScenes()
	if err != nil {
		return nil, err
	}
	if after != before+1 {
		return nil, &routeErr{status: http.StatusInternalServerError, msg: "Live did not duplicate the scene"}
	}
	return ack("duplicate", fmt.Sprintf("scene %d %q", i+1, name), fmt.Sprintf("copy is scene %d", i+2)), nil
}

func hSceneSet(r *routeReq) (any, error) {
	prop := strings.TrimSpace(r.vars["prop"])
	spec, ok := sceneProps[prop]
	if !ok {
		return nil, badInput("unknown scene property %q: use one of %s", prop, propNames(sceneProps))
	}
	raw, ok := r.bodyString("value")
	if !ok {
		return nil, badInput("value is required")
	}
	arg, err := spec.parse(r, prop, raw)
	if err != nil {
		return nil, err
	}
	i, name, err := r.resolveScene(r.vars["scene"])
	if err != nil {
		return nil, err
	}
	before, err := r.q("/live/scene/get/"+prop, 1, int32(i))
	if err != nil {
		return nil, err
	}
	if err := r.send("/live/scene/set/"+prop, int32(i), arg); err != nil {
		return nil, err
	}
	after, err := r.q("/live/scene/get/"+prop, 1, int32(i))
	if err != nil {
		return nil, err
	}
	return r.propChange(fmt.Sprintf("scene %d %q", i+1, name), prop, spec.display(first(before)), spec.display(first(after)), map[string]any{"requested": raw}), nil
}

// ---------- view ----------

func hViewGet(r *routeReq) (any, error) {
	res, errs, err := r.qmTolerant([]live.Request{
		live.Q("/live/view/get/selected_track", 0),
		live.Q("/live/view/get/selected_scene", 0),
		live.Q("/live/view/get/selected_device", 0),
	})
	if err != nil {
		return nil, err
	}
	out := map[string]any{"track": nil, "track_number": nil, "scene": nil, "scene_number": nil, "clip": nil, "device": nil, "device_number": nil}
	t, s := -1, -1
	if errs[0] == nil && len(res[0]) > 0 {
		t = firstInt(res[0])
		if names, err := r.tracks(); err == nil && t >= 0 && t < len(names) {
			out["track"], out["track_number"] = names[t], t+1
		}
	} else {
		out["track"] = "(return or main track)"
	}
	if errs[1] == nil && len(res[1]) > 0 {
		s = firstInt(res[1])
		if names, err := r.scenes(); err == nil && s >= 0 && s < len(names) {
			out["scene"], out["scene_number"] = names[s], s+1
		}
	}
	if t >= 0 && s >= 0 {
		cres, cerrs, err := r.qmTolerant([]live.Request{live.Q("/live/clip_slot/get/has_clip", 2, int32(t), int32(s))})
		if err == nil && cerrs[0] == nil && firstBool(cres[0]) {
			if v, err := r.q("/live/clip/get/name", 2, int32(t), int32(s)); err == nil {
				out["clip"] = firstString(v)
			}
		}
	}
	if errs[2] == nil && len(res[2]) >= 2 {
		dt, d := firstInt(res[2][:1]), firstInt(res[2][1:2])
		if names, err := r.deviceNames(dt); err == nil && d >= 0 && d < len(names) {
			out["device"], out["device_number"] = names[d], d+1
		}
	}
	return out, nil
}

func hViewSelect(r *routeReq) (any, error) {
	trackIn, hasTrack := r.bodyString("track")
	sceneIn, hasScene := r.bodyString("scene")
	slot, hasSlot, err := r.bodyInt("slot")
	if err != nil {
		return nil, err
	}
	devIn, hasDev := r.bodyString("device")
	hasTrack = hasTrack && strings.TrimSpace(trackIn) != ""
	hasScene = hasScene && strings.TrimSpace(sceneIn) != ""
	hasDev = hasDev && strings.TrimSpace(devIn) != ""
	if !hasTrack && !hasScene && !hasSlot && !hasDev {
		return nil, badInput("give at least one of --track, --scene, --slot (with --track) or --device (with --track)")
	}
	if (hasSlot || hasDev) && !hasTrack {
		return nil, badInput("--slot and --device need --track")
	}
	t := -1
	if hasTrack {
		if t, _, err = r.resolveTrack(trackIn); err != nil {
			return nil, err
		}
		if err := r.send("/live/view/set/selected_track", int32(t)); err != nil {
			return nil, err
		}
	}
	if hasScene {
		i, _, err := r.resolveScene(sceneIn)
		if err != nil {
			return nil, err
		}
		if err := r.send("/live/view/set/selected_scene", int32(i)); err != nil {
			return nil, err
		}
	}
	if hasSlot {
		s, err := r.checkSlot(fmt.Sprint(slot))
		if err != nil {
			return nil, err
		}
		if err := r.send("/live/view/set/selected_clip", int32(t), int32(s)); err != nil {
			return nil, err
		}
	}
	if hasDev {
		d, _, err := r.resolveDevice(t, devIn)
		if err != nil {
			return nil, err
		}
		if err := r.send("/live/view/set/selected_device", int32(t), int32(d)); err != nil {
			return nil, err
		}
	}
	if _, err := r.q("/live/application/get/version", 0); err != nil {
		return nil, err
	}
	return hViewGet(r)
}

func hViewShow(r *routeReq) (any, error) {
	view, err := r.requireBodyString("view")
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(view)) {
	case "arrangement", "arranger":
		if _, err := r.mcp("switch_to_arrangement_view", map[string]any{}, 10*time.Second); err != nil {
			return nil, err
		}
		return ack("show", "main window", "Arrangement view"), nil
	case "session":
		return nil, badInput("only switching to the Arrangement view is supported by the Remote Scripts; press Tab in Live to go back to the Session view")
	}
	return nil, badInput("view must be arrangement or session, got %q", view)
}

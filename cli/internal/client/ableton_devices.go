// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package client

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"ableton-pp-cli/internal/cliutil"
	"ableton-pp-cli/internal/live"
)

func deviceTarget(t int, tname string, d int, dname string) string {
	return fmt.Sprintf("track %d %q / device %d %q", t+1, tname, d+1, dname)
}

// deviceVars resolves {track} and {device}.
func (r *routeReq) deviceVars() (int, string, int, string, error) {
	t, tname, err := r.trackVar()
	if err != nil {
		return -1, "", -1, "", err
	}
	d, dname, err := r.resolveDevice(t, r.vars["device"])
	if err != nil {
		return -1, "", -1, "", err
	}
	return t, tname, d, dname, nil
}

func hDeviceList(r *routeReq) (any, error) {
	t, _, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	return r.loadDevices(t)
}

func hDeviceLoad(r *routeReq) (any, error) {
	uri, err := r.requireBodyString("uri")
	if err != nil {
		return nil, err
	}
	t, tname, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	raw, err := r.mcp("load_instrument_or_effect", map[string]any{"track_index": t, "uri": uri}, 20*time.Second)
	if err != nil {
		return nil, err
	}
	var res struct {
		Loaded   bool   `json:"loaded"`
		ItemName string `json:"item_name"`
	}
	_ = json.Unmarshal(raw, &res)
	if !res.Loaded {
		return nil, &routeErr{status: http.StatusInternalServerError, msg: fmt.Sprintf("AbletonMCP did not confirm loading %s", uri)}
	}
	return ack("load", trackTarget(t, tname), fmt.Sprintf("loaded %q", res.ItemName)), nil
}

func hDeviceOn(on bool) routeHandler {
	return func(r *routeReq) (any, error) {
		t, tname, d, dname, err := r.deviceVars()
		if err != nil {
			return nil, err
		}
		res, err := r.qm([]live.Request{
			live.Q("/live/device/get/parameter/name", 3, int32(t), int32(d), int32(0)),
			live.Q("/live/device/get/parameter/value", 3, int32(t), int32(d), int32(0)),
		})
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(firstString(res[0]), "Device On") {
			return nil, badInput("%s has no Device On switch", deviceTarget(t, tname, d, dname))
		}
		v := float32(0)
		if on {
			v = 1
		}
		if err := r.send("/live/device/set/parameter/value", int32(t), int32(d), int32(0), v); err != nil {
			return nil, err
		}
		after, err := r.q("/live/device/get/parameter/value", 3, int32(t), int32(d), int32(0))
		if err != nil {
			return nil, err
		}
		return r.propChange(deviceTarget(t, tname, d, dname), "on", firstFloat(res[1]) >= 0.5, firstFloat(after) >= 0.5, nil), nil
	}
}

func hDeviceDelete(r *routeReq) (any, error) {
	t, tname, d, dname, err := r.deviceVars()
	if err != nil {
		return nil, err
	}
	raw, err := r.ppx("delete_device", map[string]any{"track": t, "device": d})
	if err != nil {
		return nil, err
	}
	var res struct {
		Deleted string `json:"deleted"`
	}
	_ = json.Unmarshal(raw, &res)
	return ack("delete", deviceTarget(t, tname, d, dname), fmt.Sprintf("deleted %q", res.Deleted)), nil
}

func hParamList(r *routeReq) (any, error) {
	t, _, d, _, err := r.deviceVars()
	if err != nil {
		return nil, err
	}
	keys := []string{"name", "value", "min", "max", "is_quantized"}
	reqs := make([]live.Request, len(keys))
	for i, k := range keys {
		reqs[i] = live.Q("/live/device/get/parameters/"+k, 2, int32(t), int32(d))
	}
	res, err := r.qm(reqs)
	if err != nil {
		return nil, err
	}
	names := res[0]
	sreqs := make([]live.Request, len(names))
	for p := range names {
		sreqs[p] = live.Q("/live/device/get/parameter/value_string", 3, int32(t), int32(d), int32(p))
	}
	sres, serrs, err := r.qmTolerant(sreqs)
	if err != nil {
		return nil, err
	}
	at := func(list []any, i int) any {
		if i < len(list) {
			return list[i]
		}
		return nil
	}
	rows := make([]map[string]any, len(names))
	for p := range names {
		var display any
		if serrs[p] == nil && len(sres[p]) > 0 {
			display = firstString(sres[p])
		}
		rows[p] = map[string]any{
			"number":    p + 1,
			"name":      dispString(names[p]),
			"value":     dispFloat(at(res[1], p)),
			"display":   display,
			"min":       dispFloat(at(res[2], p)),
			"max":       dispFloat(at(res[3], p)),
			"quantized": dispBool(at(res[4], p)),
		}
	}
	return rows, nil
}

func hParamSet(r *routeReq) (any, error) {
	v, err := r.requireBodyFloat("value")
	if err != nil {
		return nil, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, badInput("value must be a number")
	}
	t, tname, d, dname, err := r.deviceVars()
	if err != nil {
		return nil, err
	}
	p, pname, err := r.resolveParam(t, d, r.vars["param"])
	if err != nil {
		return nil, err
	}
	res, err := r.qm([]live.Request{
		live.Q("/live/device/get/parameters/min", 2, int32(t), int32(d)),
		live.Q("/live/device/get/parameters/max", 2, int32(t), int32(d)),
		live.Q("/live/device/get/parameters/is_quantized", 2, int32(t), int32(d)),
		live.Q("/live/device/get/parameter/value", 3, int32(t), int32(d), int32(p)),
		live.Q("/live/device/get/parameter/value_string", 3, int32(t), int32(d), int32(p)),
	})
	if err != nil {
		return nil, err
	}
	if p >= len(res[0]) || p >= len(res[1]) {
		return nil, notFound("parameter %d disappeared", p+1)
	}
	lo, _ := live.AsFloat(res[0][p])
	hi, _ := live.AsFloat(res[1][p])
	quantized := false
	if p < len(res[2]) {
		quantized, _ = live.AsBool(res[2][p])
	}
	if v < lo || v > hi {
		return nil, badInput("%s accepts %v to %v, got %v (value unchanged)", pname, lo, hi, v)
	}
	if quantized && v != math.Trunc(v) {
		return nil, badInput("%s is a stepped parameter: use a whole number between %v and %v", pname, lo, hi)
	}
	if err := r.send("/live/device/set/parameter/value", int32(t), int32(d), int32(p), float32(v)); err != nil {
		return nil, err
	}
	after, err := r.qm([]live.Request{
		live.Q("/live/device/get/parameter/value", 3, int32(t), int32(d), int32(p)),
		live.Q("/live/device/get/parameter/value_string", 3, int32(t), int32(d), int32(p)),
	})
	if err != nil {
		return nil, err
	}
	return r.propChange(deviceTarget(t, tname, d, dname), pname, firstFloat(res[3]), firstFloat(after[0]), map[string]any{
		"parameter_number": p + 1,
		"before_display":   firstString(res[4]),
		"after_display":    firstString(after[1]),
		"min":              lo,
		"max":              hi,
	}), nil
}

func hMidiMap(r *routeReq) (any, error) {
	trackIn, err := r.requireBodyString("track")
	if err != nil {
		return nil, err
	}
	devIn, err := r.requireBodyString("device")
	if err != nil {
		return nil, err
	}
	paramIn, err := r.requireBodyString("param")
	if err != nil {
		return nil, err
	}
	ch, ok, err := r.bodyInt("channel")
	if err != nil {
		return nil, err
	}
	if !ok || ch < 1 || ch > 16 {
		return nil, badInput("channel is required and must be 1-16")
	}
	cc, ok, err := r.bodyInt("cc")
	if err != nil {
		return nil, err
	}
	if !ok || cc < 0 || cc > 127 {
		return nil, badInput("cc is required and must be 0-127")
	}
	t, tname, err := r.resolveTrack(trackIn)
	if err != nil {
		return nil, err
	}
	d, dname, err := r.resolveDevice(t, devIn)
	if err != nil {
		return nil, err
	}
	p, pname, err := r.resolveParam(t, d, paramIn)
	if err != nil {
		return nil, err
	}
	if err := r.act("/live/midimap/map_cc", int32(t), int32(d), int32(p), int32(ch-1), int32(cc)); err != nil {
		return nil, err
	}
	return ack("midimap", deviceTarget(t, tname, d, dname)+fmt.Sprintf(" / %q", pname), fmt.Sprintf("CC %d on channel %d", cc, ch)), nil
}

// ---------- browser ----------

type browserNode struct {
	Name       string        `json:"name"`
	URI        *string       `json:"uri"`
	IsFolder   bool          `json:"is_folder"`
	IsDevice   bool          `json:"is_device"`
	IsLoadable bool          `json:"is_loadable"`
	HasMore    bool          `json:"has_more"`
	Children   []browserNode `json:"children"`
}

type browserListing struct {
	Path                string        `json:"path"`
	Name                string        `json:"name"`
	URI                 *string       `json:"uri"`
	IsFolder            bool          `json:"is_folder"`
	IsLoadable          bool          `json:"is_loadable"`
	Items               []browserNode `json:"items"`
	Error               string        `json:"error"`
	AvailableCategories []string      `json:"available_categories"`
}

var browserCategories = []string{"instruments", "sounds", "drums", "audio_effects", "midi_effects", "plugins"}

func categoryKey(display string) string {
	switch strings.ToLower(display) {
	case "audio effects":
		return "audio_effects"
	case "midi effects":
		return "midi_effects"
	}
	return strings.ToLower(strings.ReplaceAll(display, " ", "_"))
}

func browserRow(n browserNode, path, category string) map[string]any {
	uri := ""
	if n.URI != nil {
		uri = *n.URI
	}
	return map[string]any{
		"uri": uri, "name": n.Name, "path": path, "category": category,
		"is_folder": n.IsFolder, "is_loadable": n.IsLoadable, "is_device": n.IsDevice,
	}
}

func (r *routeReq) browserItems(path string) (*browserListing, error) {
	raw, err := r.mcp("get_browser_items_at_path", map[string]any{"path": path}, 20*time.Second)
	if err != nil {
		return nil, err
	}
	var res browserListing
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("unexpected browser reply: %w", err)
	}
	if res.Error != "" {
		msg := fmt.Sprintf("browser path %q: %s", path, res.Error)
		if len(res.AvailableCategories) > 0 {
			msg += "; start with one of: " + strings.Join(browserCategories, ", ")
		}
		return nil, notFound("%s", msg)
	}
	return &res, nil
}

func hBrowserTree(r *routeReq) (any, error) {
	cat := strings.TrimSpace(r.params["category"])
	if cat == "" {
		cat = "all"
	}
	if cat != "all" {
		ok := false
		for _, c := range browserCategories {
			if c == cat {
				ok = true
			}
		}
		if !ok {
			return nil, badInput("category must be all or one of %s", strings.Join(browserCategories, ", "))
		}
	}
	raw, err := r.mcp("get_browser_tree", map[string]any{"category_type": cat}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	var res struct {
		Categories []browserNode `json:"categories"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("unexpected browser tree reply: %w", err)
	}
	rows := []map[string]any{}
	for _, c := range res.Categories {
		key := categoryKey(c.Name)
		row := browserRow(c, key, key)
		row["has_more"] = c.HasMore
		rows = append(rows, row)
		for _, ch := range c.Children {
			cr := browserRow(ch, key+"/"+ch.Name, key)
			cr["has_more"] = ch.HasMore
			rows = append(rows, cr)
		}
	}
	return rows, nil
}

func hBrowserItems(r *routeReq) (any, error) {
	path := strings.Trim(strings.TrimSpace(r.params["path"]), "/")
	if path == "" {
		return nil, badInput("path is required, e.g. instruments, audio_effects or drums/Kits")
	}
	res, err := r.browserItems(path)
	if err != nil {
		return nil, err
	}
	cat := strings.ToLower(strings.SplitN(path, "/", 2)[0])
	rows := make([]map[string]any, 0, len(res.Items))
	for _, it := range res.Items {
		rows = append(rows, browserRow(it, path+"/"+it.Name, cat))
	}
	return rows, nil
}

// hBrowserIndex flattens loadable items two levels deep for sync and
// 'device add'. Under the dogfood harness only the first category is read.
func hBrowserIndex(r *routeReq) (any, error) {
	cats := browserCategories
	if cliutil.IsDogfoodEnv() {
		cats = cats[:1]
	}
	seen := map[string]bool{}
	rows := []map[string]any{}
	add := func(n browserNode, path, cat string) {
		if !n.IsLoadable || n.URI == nil || *n.URI == "" || seen[*n.URI] {
			return
		}
		seen[*n.URI] = true
		row := browserRow(n, path, cat)
		row["is_folder"] = false
		rows = append(rows, row)
	}
	for _, cat := range cats {
		top, err := r.browserItems(cat)
		if err != nil {
			var re *routeErr
			if ok := asRouteErr(err, &re); ok && re.status == http.StatusNotFound {
				continue // category not offered by this Live (e.g. no plugins)
			}
			return nil, err
		}
		for _, it := range top.Items {
			add(it, cat+"/"+it.Name, cat)
			if !it.IsFolder || strings.Contains(it.Name, "/") {
				continue
			}
			sub, err := r.browserItems(cat + "/" + it.Name)
			if err != nil {
				var re *routeErr
				if ok := asRouteErr(err, &re); ok && re.status == http.StatusNotFound {
					continue
				}
				return nil, err
			}
			for _, s := range sub.Items {
				add(s, cat+"/"+it.Name+"/"+s.Name, cat)
			}
		}
	}
	return rows, nil
}

func asRouteErr(err error, target **routeErr) bool {
	re, ok := err.(*routeErr)
	if ok {
		*target = re
	}
	return ok
}

// ---------- drums, racks ----------

func hDrumsLoad(r *routeReq) (any, error) {
	kit, err := r.requireBodyString("kit")
	if err != nil {
		return nil, err
	}
	kit = strings.Trim(strings.TrimSpace(kit), "/")
	if !strings.HasPrefix(strings.ToLower(kit), "drums/") && !strings.EqualFold(kit, "drums") {
		kit = "drums/" + kit
	}
	rackURI, _ := r.bodyString("rack_uri")
	t, tname, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	res, err := r.browserItems(kit)
	if err != nil {
		return nil, err
	}
	kitURI, kitName := "", ""
	if res.IsLoadable && res.URI != nil && *res.URI != "" {
		kitURI, kitName = *res.URI, res.Name
	} else {
		for _, it := range res.Items {
			if it.IsLoadable && it.URI != nil && *it.URI != "" {
				kitURI, kitName = *it.URI, it.Name
				break
			}
		}
	}
	if kitURI == "" {
		return nil, notFound("no loadable drum kit at %q; list kits with 'browser list --path drums'", kit)
	}
	detail := ""
	if strings.TrimSpace(rackURI) != "" {
		if _, err := r.mcp("load_browser_item", map[string]any{"track_index": t, "item_uri": strings.TrimSpace(rackURI)}, 20*time.Second); err != nil {
			return nil, err
		}
		detail = "loaded rack " + rackURI + ", then "
	}
	if _, err := r.mcp("load_browser_item", map[string]any{"track_index": t, "item_uri": kitURI}, 20*time.Second); err != nil {
		return nil, err
	}
	return ack("load drum kit", trackTarget(t, tname), fmt.Sprintf("%sloaded kit %q", detail, kitName)), nil
}

func hDrumPads(r *routeReq) (any, error) {
	t, _, d, _, err := r.deviceVars()
	if err != nil {
		return nil, err
	}
	raw, err := r.ppx("drum_pads", map[string]any{"track": t, "device": d})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func hRackInspect(r *routeReq) (any, error) {
	t, _, d, _, err := r.deviceVars()
	if err != nil {
		return nil, err
	}
	raw, err := r.mcp("inspect_rack", map[string]any{"track_index": t, "device_index": d}, 10*time.Second)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("unexpected inspect_rack reply: %w", err)
	}
	out["rack"] = out["rack_name"]
	return out, nil
}

func hRackChains(r *routeReq) (any, error) {
	t, _, d, _, err := r.deviceVars()
	if err != nil {
		return nil, err
	}
	raw, err := r.ppx("rack_chains", map[string]any{"track": t, "device": d})
	if err != nil {
		return nil, err
	}
	var chains []map[string]any
	if err := json.Unmarshal(raw, &chains); err != nil {
		return nil, fmt.Errorf("unexpected rack_chains reply: %w", err)
	}
	for _, c := range chains {
		if idx, ok := c["index"].(float64); ok {
			c["number"] = int(idx) + 1
		}
		if text, ok := c["volume_db"].(string); ok {
			c["volume_text"] = text
			if f, ok := live.ParseDBText(text); ok {
				c["volume_db"] = f
			} else {
				c["volume_db"] = nil
			}
		}
	}
	return chains, nil
}

func hRackMapMacro(r *routeReq) (any, error) {
	name, ok := r.bodyString("name")
	if !ok || strings.TrimSpace(name) == "" {
		name = "Magnitude"
	}
	t, tname, d, dname, err := r.deviceVars()
	if err != nil {
		return nil, err
	}
	raw, err := r.mcp("map_rack_magnitude", map[string]any{"track_index": t, "device_index": d, "macro_name": name}, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var res struct {
		Mapped  []any `json:"mapped"`
		Skipped []any `json:"skipped"`
	}
	_ = json.Unmarshal(raw, &res)
	out := ack("map-macro", deviceTarget(t, tname, d, dname), fmt.Sprintf("Macro 1 %q controls %d parameters (%d devices skipped)", name, len(res.Mapped), len(res.Skipped)))
	out["mapped"], out["skipped"] = res.Mapped, res.Skipped
	return out, nil
}

// ---------- arrangement ----------

func (r *routeReq) arrRow(i int, name string, start, end float64, midi bool) map[string]any {
	return map[string]any{
		"number": i + 1, "name": name, "start": start, "end": end, "length": end - start,
		"position": r.formatPos(start), "end_position": r.formatPos(end),
		"type": map[bool]string{true: "midi", false: "audio"}[midi],
	}
}

func hArrClips(r *routeReq) (any, error) {
	t, _, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	raw, err := r.mcp("get_arrangement_clips", map[string]any{"track_index": t}, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var res struct {
		Clips []struct {
			Name      string  `json:"name"`
			StartTime float64 `json:"start_time"`
			EndTime   float64 `json:"end_time"`
			IsMidi    bool    `json:"is_midi_clip"`
		} `json:"clips"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("unexpected get_arrangement_clips reply: %w", err)
	}
	rows := make([]map[string]any, len(res.Clips))
	for i, c := range res.Clips {
		rows[i] = r.arrRow(i, c.Name, c.StartTime, c.EndTime, c.IsMidi)
	}
	return rows, nil
}

func hArrAddClip(r *routeReq) (any, error) {
	at, err := r.requireBodyString("at")
	if err != nil {
		return nil, err
	}
	bars, hasBars, err := r.bodyFloat("bars")
	if err != nil {
		return nil, err
	}
	if !hasBars {
		bars = 4
	}
	if bars <= 0 || bars > 1024 {
		return nil, badInput("bars must be more than 0 and at most 1024")
	}
	t, _, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	start, err := r.position(at)
	if err != nil {
		return nil, err
	}
	num, den, _ := r.signature()
	raw, err := r.ppx("arrangement_add_clip", map[string]any{"track": t, "start": start, "length": live.BarsToBeats(bars, num, den)})
	if err != nil {
		return nil, err
	}
	var res struct {
		Index int     `json:"index"`
		Name  string  `json:"name"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("unexpected arrangement_add_clip reply: %w", err)
	}
	return r.arrRow(res.Index, res.Name, res.Start, res.End, true), nil
}

func hArrDeleteClip(r *routeReq) (any, error) {
	t, tname, err := r.trackVar()
	if err != nil {
		return nil, err
	}
	c, err := live.ParseSlot("clip", r.vars["clip"])
	if err != nil {
		return nil, badInput("%v", err)
	}
	raw, err := r.ppx("arrangement_delete_clip", map[string]any{"track": t, "clip": c})
	if err != nil {
		return nil, err
	}
	var res struct {
		Deleted string  `json:"deleted"`
		Start   float64 `json:"start"`
	}
	_ = json.Unmarshal(raw, &res)
	return ack("delete", fmt.Sprintf("arrangement clip %d on %s", c+1, trackTarget(t, tname)), fmt.Sprintf("deleted %q at %s", res.Deleted, r.formatPos(res.Start))), nil
}

// ---------- automation ----------

func (r *routeReq) automationVars() (map[string]any, string, error) {
	t, tname, err := r.trackVar()
	if err != nil {
		return nil, "", err
	}
	s, err := r.slotVar("slot")
	if err != nil {
		return nil, "", err
	}
	d, dname, err := r.resolveDevice(t, r.vars["device"])
	if err != nil {
		return nil, "", err
	}
	p, pname, err := r.resolveParam(t, d, r.vars["param"])
	if err != nil {
		return nil, "", err
	}
	target := fmt.Sprintf("%s / %q in clip %d", deviceTarget(t, tname, d, dname), pname, s+1)
	return map[string]any{"track": t, "slot": s, "device": d, "param": p}, target, nil
}

func hAutomationGet(r *routeReq) (any, error) {
	step, hasStep, err := r.paramFloat("step")
	if err != nil {
		return nil, err
	}
	if !hasStep {
		step = 0.25
	}
	if step <= 0 {
		return nil, badInput("step must be more than 0 beats")
	}
	params, _, err := r.automationVars()
	if err != nil {
		return nil, err
	}
	params["step"] = step
	raw, err := r.ppx("automation_get", params)
	if err != nil {
		return nil, err
	}
	var res struct {
		Points []map[string]any `json:"points"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("unexpected automation_get reply: %w", err)
	}
	if res.Points == nil {
		res.Points = []map[string]any{}
	}
	return res.Points, nil
}

func hAutomationSet(r *routeReq) (any, error) {
	rawPoints, ok := r.body["points"].([]any)
	if !ok || len(rawPoints) == 0 {
		return nil, badInput("points is required: a JSON array of {time, value, duration}")
	}
	points := make([]map[string]any, 0, len(rawPoints))
	for i, rp := range rawPoints {
		m, ok := rp.(map[string]any)
		if !ok {
			return nil, badInput("point %d must be an object", i+1)
		}
		tm, ok1 := m["time"].(float64)
		val, ok2 := m["value"].(float64)
		if !ok1 || !ok2 || tm < 0 {
			return nil, badInput("point %d needs a numeric time (>= 0) and value", i+1)
		}
		pt := map[string]any{"time": tm, "value": val}
		if dv, ok := m["duration"]; ok && dv != nil {
			dur, ok := dv.(float64)
			// Live writes a flat step of this length; a zero-length step writes nothing.
			if !ok || dur <= 0 {
				return nil, badInput("point %d: duration must be a number > 0 (leave it out to hold the value until the next point)", i+1)
			}
			pt["duration"] = dur
		}
		points = append(points, pt)
	}
	params, target, err := r.automationVars()
	if err != nil {
		return nil, err
	}
	params["points"] = points
	raw, err := r.ppx("automation_set", params)
	if err != nil {
		return nil, err
	}
	var res struct {
		Written int `json:"written"`
	}
	_ = json.Unmarshal(raw, &res)
	return ack("automation set", target, fmt.Sprintf("wrote %d points", res.Written)), nil
}

func hAutomationClear(r *routeReq) (any, error) {
	params, target, err := r.automationVars()
	if err != nil {
		return nil, err
	}
	raw, err := r.ppx("automation_clear", params)
	if err != nil {
		return nil, err
	}
	var res struct {
		Cleared bool `json:"cleared"`
	}
	_ = json.Unmarshal(raw, &res)
	detail := "no envelope to clear"
	if res.Cleared {
		detail = "envelope cleared"
	}
	return ack("automation clear", target, detail), nil
}

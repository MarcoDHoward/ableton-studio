// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package client

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"ableton-pp-cli/internal/live"
)

// propSpec describes one settable/readable Live property.
type propSpec struct {
	kind     string // float, int, bool, enum, string, root, position, signature_den
	enum     *live.Enum
	min, max float64
	ranged   bool
	readOnly bool
}

func fRange(lo, hi float64) propSpec { return propSpec{kind: "float", min: lo, max: hi, ranged: true} }
func iRange(lo, hi float64) propSpec { return propSpec{kind: "int", min: lo, max: hi, ranged: true} }

var (
	pBool   = propSpec{kind: "bool"}
	pString = propSpec{kind: "string"}
	pFloat  = propSpec{kind: "float"}
	pPos    = propSpec{kind: "position"}
	pRO     = func(p propSpec) propSpec { p.readOnly = true; return p }
)

var songProps = map[string]propSpec{
	"tempo":                       fRange(20, 999),
	"metronome":                   pBool,
	"loop":                        pBool,
	"loop_start":                  pPos,
	"loop_length":                 fRange(0.0625, 1e6),
	"signature_numerator":         iRange(1, 99),
	"signature_denominator":       {kind: "signature_den"},
	"groove_amount":               fRange(0, 1.3125),
	"clip_trigger_quantization":   {kind: "enum", enum: &live.SongClipTriggerQuantization},
	"midi_recording_quantization": {kind: "enum", enum: &live.MidiRecordingQuantization},
	"record_mode":                 pBool,
	"arrangement_overdub":         pBool,
	"session_record":              pBool,
	"punch_in":                    pBool,
	"punch_out":                   pBool,
	"back_to_arranger":            pBool,
	"nudge_up":                    pBool,
	"nudge_down":                  pBool,
	"current_song_time":           pPos,
	"song_length":                 pRO(pFloat),
	"is_playing":                  pRO(pBool),
	"can_undo":                    pRO(pBool),
	"can_redo":                    pRO(pBool),
	"root_note":                   {kind: "root"},
	"scale_name":                  pString,
}

var clipProps = map[string]propSpec{
	"name":                pString,
	"color_index":         iRange(0, 69),
	"gain":                fRange(0, 1),
	"pitch_coarse":        iRange(-48, 48),
	"pitch_fine":          fRange(-50, 49),
	"loop_start":          pFloat,
	"loop_end":            pFloat,
	"start_marker":        pFloat,
	"end_marker":          pFloat,
	"warping":             pBool,
	"warp_mode":           {kind: "enum", enum: &live.WarpMode},
	"launch_mode":         {kind: "enum", enum: &live.LaunchMode},
	"launch_quantization": {kind: "enum", enum: &live.ClipLaunchQuantization},
	"legato":              pBool,
	"muted":               pBool,
	"ram_mode":            pBool,
	"velocity_amount":     fRange(0, 1),
}

var sceneProps = map[string]propSpec{
	"name":                       pString,
	"color_index":                iRange(0, 69),
	"tempo":                      fRange(20, 999),
	"tempo_enabled":              pBool,
	"time_signature_numerator":   iRange(1, 99),
	"time_signature_denominator": {kind: "signature_den"},
	"time_signature_enabled":     pBool,
}

func propNames(m map[string]propSpec) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	return strings.Join(sortStrings(names), ", ")
}

func sortStrings(s []string) []string {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s
}

// parse turns a user value into the OSC argument Live expects.
func (p propSpec) parse(r *routeReq, name, raw string) (any, error) {
	if p.readOnly {
		return nil, badInput("%s is read-only", name)
	}
	s := strings.TrimSpace(raw)
	switch p.kind {
	case "bool":
		b, err := live.ParseBool(s)
		if err != nil {
			return nil, badInput("%s: %v", name, err)
		}
		return b, nil
	case "string":
		if strings.ContainsRune(raw, 0) {
			return nil, badInput("%s must not contain NUL bytes", name)
		}
		return raw, nil
	case "enum":
		v, err := p.enum.Parse(s)
		if err != nil {
			return nil, badInput("%s: %v", name, err)
		}
		return int32(v), nil
	case "root":
		v, err := live.ParseRootNote(s)
		if err != nil {
			return nil, badInput("%s: %v", name, err)
		}
		return int32(v), nil
	case "signature_den":
		v, err := strconv.Atoi(s)
		if err != nil || !live.ValidSignature(4, v) {
			return nil, badInput("%s must be 1, 2, 4, 8 or 16, got %q", name, raw)
		}
		return int32(v), nil
	case "position":
		b, err := r.position(s)
		if err != nil {
			return nil, err
		}
		return float32(b), nil
	case "int":
		v, err := strconv.Atoi(s)
		if err != nil {
			return nil, badInput("%s must be a whole number, got %q", name, raw)
		}
		if p.ranged && (float64(v) < p.min || float64(v) > p.max) {
			return nil, badInput("%s must be between %v and %v, got %d", name, p.min, p.max, v)
		}
		return int32(v), nil
	default: // float
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, badInput("%s must be a number, got %q", name, raw)
		}
		if p.ranged && (v < p.min || v > p.max) {
			return nil, badInput("%s must be between %v and %v, got %v", name, p.min, p.max, v)
		}
		return float32(v), nil
	}
}

// display turns a value read from Live into a JSON value for output.
func (p propSpec) display(v any) any {
	switch p.kind {
	case "bool":
		b, ok := live.AsBool(v)
		if !ok {
			return nil
		}
		return b
	case "enum":
		i, ok := live.AsInt(v)
		if !ok {
			return live.JSONValue(v)
		}
		return p.enum.Name(i)
	case "root":
		i, ok := live.AsInt(v)
		if !ok {
			return live.JSONValue(v)
		}
		return live.RootNoteName(i)
	case "int", "signature_den":
		i, ok := live.AsInt(v)
		if !ok {
			return live.JSONValue(v)
		}
		return i
	case "string":
		s, ok := live.AsString(v)
		if !ok {
			return nil
		}
		return s
	}
	return live.JSONValue(v)
}

// fmtValue renders a display value as text (PropValue.value is a string).
func fmtValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if x {
			return "on"
		}
		return "off"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package live

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Enum maps Live's integer codes to user-facing names.
type Enum struct {
	Kind    string
	Names   map[int]string
	Aliases map[string]int
}

func normEnum(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	r := strings.NewReplacer(" ", "", "-", "", "_", "")
	return r.Replace(s)
}

// Name returns the display name for a code, or the number itself.
func (e Enum) Name(v int) string {
	if n, ok := e.Names[v]; ok {
		return n
	}
	return strconv.Itoa(v)
}

// Parse accepts a name (case and space insensitive), an alias or the code.
func (e Enum) Parse(s string) (int, error) {
	key := normEnum(s)
	for code, name := range e.Names {
		if normEnum(name) == key {
			return code, nil
		}
	}
	if code, ok := e.Aliases[key]; ok {
		return code, nil
	}
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		if _, ok := e.Names[n]; ok {
			return n, nil
		}
	}
	return 0, fmt.Errorf("invalid %s %q: use one of %s", e.Kind, s, strings.Join(e.choices(), ", "))
}

func (e Enum) choices() []string {
	codes := make([]int, 0, len(e.Names))
	for c := range e.Names {
		codes = append(codes, c)
	}
	sort.Ints(codes)
	out := make([]string, len(codes))
	for i, c := range codes {
		out[i] = e.Names[c]
	}
	return out
}

// ClipLaunchQuantization is Clip.launch_quantization (AbletonOSC README).
var ClipLaunchQuantization = Enum{
	Kind: "launch quantization",
	Names: map[int]string{
		0: "global", 1: "none", 2: "8 bars", 3: "4 bars", 4: "2 bars", 5: "1 bar",
		6: "1/2", 7: "1/2t", 8: "1/4", 9: "1/4t", 10: "1/8", 11: "1/8t",
		12: "1/16", 13: "1/16t", 14: "1/32",
	},
	Aliases: map[string]int{"8bar": 2, "4bar": 3, "2bar": 4, "1bars": 5, "bar": 5},
}

// SongClipTriggerQuantization is Song.clip_trigger_quantization (no Global).
var SongClipTriggerQuantization = Enum{
	Kind: "clip trigger quantization",
	Names: map[int]string{
		0: "none", 1: "8 bars", 2: "4 bars", 3: "2 bars", 4: "1 bar", 5: "1/2",
		6: "1/2t", 7: "1/4", 8: "1/4t", 9: "1/8", 10: "1/8t", 11: "1/16",
		12: "1/16t", 13: "1/32",
	},
	Aliases: map[string]int{"8bar": 1, "4bar": 2, "2bar": 3, "1bars": 4, "bar": 4},
}

// MidiRecordingQuantization is Song.midi_recording_quantization.
var MidiRecordingQuantization = Enum{
	Kind: "MIDI recording quantization",
	Names: map[int]string{
		0: "none", 1: "1/4", 2: "1/8", 3: "1/8t", 4: "1/8+1/8t",
		5: "1/16", 6: "1/16t", 7: "1/16+1/16t", 8: "1/32",
	},
}

// WarpMode is Clip.warp_mode (5 is Live's "invalid" marker).
var WarpMode = Enum{
	Kind:    "warp mode",
	Names:   map[int]string{0: "beats", 1: "tones", 2: "texture", 3: "repitch", 4: "complex", 6: "pro"},
	Aliases: map[string]int{"repitched": 3, "complexpro": 6},
}

// LaunchMode is Clip.launch_mode.
var LaunchMode = Enum{
	Kind:  "launch mode",
	Names: map[int]string{0: "trigger", 1: "gate", 2: "toggle", 3: "repeat"},
}

// Monitoring is Track.current_monitoring_state.
var Monitoring = Enum{
	Kind:  "monitoring",
	Names: map[int]string{0: "in", 1: "auto", 2: "off"},
}

// DeviceTypeName maps Device.type to a name. Live's LOM numbering is
// 1 = instrument, 2 = audio_effect, 4 = midi_effect (0 = undefined).
func DeviceTypeName(code int) string {
	switch code {
	case 1:
		return "instrument"
	case 2:
		return "audio_effect"
	case 4:
		return "midi_effect"
	}
	return "unknown"
}

// ParseBool accepts on/off, true/false, yes/no and 1/0.
func ParseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "yes", "1", "y", "t":
		return true, nil
	case "off", "false", "no", "0", "n", "f":
		return false, nil
	}
	return false, fmt.Errorf("invalid on/off value %q: use on, off, true, false, 1 or 0", s)
}

var noteNames = []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// NoteName renders a MIDI pitch the way Live does (60 = C3).
func NoteName(pitch int) string {
	if pitch < 0 || pitch > 127 {
		return strconv.Itoa(pitch)
	}
	return fmt.Sprintf("%s%d", noteNames[pitch%12], pitch/12-2)
}

// RootNoteName renders Song.root_note (0 = C ... 11 = B).
func RootNoteName(n int) string {
	if n < 0 || n > 11 {
		return strconv.Itoa(n)
	}
	return noteNames[n]
}

// ParseRootNote accepts C, C#, Db, ... B or 0-11.
func ParseRootNote(s string) (int, error) {
	in := strings.TrimSpace(s)
	if n, err := strconv.Atoi(in); err == nil {
		if n >= 0 && n <= 11 {
			return n, nil
		}
		return 0, fmt.Errorf("root note %d out of range 0-11", n)
	}
	flats := map[string]int{"DB": 1, "EB": 3, "GB": 6, "AB": 8, "BB": 10, "CB": 11, "FB": 4, "E#": 5, "B#": 0}
	up := strings.ToUpper(in)
	for i, n := range noteNames {
		if up == n {
			return i, nil
		}
	}
	if n, ok := flats[up]; ok {
		return n, nil
	}
	return 0, fmt.Errorf("invalid root note %q: use C, C#, Db, ... B or 0-11", s)
}

// ParseDBText reads AbletonPPExtras' str_for_value text ("-6.0 dB",
// "0.00 dB", "-inf dB"). ok is false for -inf or unparseable text.
func ParseDBText(s string) (float64, bool) {
	t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "dB"))
	if t == "" || strings.Contains(strings.ToLower(t), "inf") {
		return 0, false
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

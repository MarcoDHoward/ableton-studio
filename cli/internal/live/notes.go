// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package live

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// Note is one MIDI note in a clip. Start and Duration are in beats.
type Note struct {
	Pitch    int     `json:"pitch"`
	Start    float64 `json:"start"`
	Duration float64 `json:"duration"`
	Velocity int     `json:"velocity"`
	Mute     bool    `json:"mute"`
}

// NotesFromFlat decodes AbletonOSC's flattened note list
// (pitch, start, duration, velocity, mute, pitch, ...).
func NotesFromFlat(vals []any) ([]Note, error) {
	if len(vals)%5 != 0 {
		return nil, fmt.Errorf("note list has %d values, not a multiple of 5", len(vals))
	}
	notes := make([]Note, 0, len(vals)/5)
	for i := 0; i < len(vals); i += 5 {
		pitch, ok1 := AsFloat(vals[i])
		start, ok2 := AsFloat(vals[i+1])
		dur, ok3 := AsFloat(vals[i+2])
		vel, ok4 := AsFloat(vals[i+3])
		mute, ok5 := AsBool(vals[i+4])
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
			return nil, fmt.Errorf("note %d has unexpected value types", i/5+1)
		}
		notes = append(notes, Note{
			Pitch:    int(math.Round(pitch)),
			Start:    start,
			Duration: dur,
			Velocity: int(math.Round(vel)),
			Mute:     mute,
		})
	}
	SortNotes(notes)
	return notes, nil
}

// SortNotes orders notes by start time, then pitch.
func SortNotes(notes []Note) {
	sort.SliceStable(notes, func(a, b int) bool {
		if notes[a].Start != notes[b].Start {
			return notes[a].Start < notes[b].Start
		}
		return notes[a].Pitch < notes[b].Pitch
	})
}

// FlattenNotes encodes notes as AbletonOSC /live/clip/add/notes arguments.
func FlattenNotes(notes []Note) []any {
	out := make([]any, 0, len(notes)*5)
	for _, n := range notes {
		out = append(out, int32(n.Pitch), float32(n.Start), float32(n.Duration), float32(n.Velocity), n.Mute)
	}
	return out
}

// ChunkNotes splits notes into groups of at most size (one OSC message each,
// keeping every datagram well under the UDP limit).
func ChunkNotes(notes []Note, size int) [][]Note {
	if size < 1 {
		size = 64
	}
	var out [][]Note
	for i := 0; i < len(notes); i += size {
		end := i + size
		if end > len(notes) {
			end = len(notes)
		}
		out = append(out, notes[i:end])
	}
	return out
}

// ValidateNote checks the ranges Live accepts.
func ValidateNote(n Note) error {
	switch {
	case n.Pitch < 0 || n.Pitch > 127:
		return fmt.Errorf("pitch %d out of range 0-127", n.Pitch)
	case n.Start < 0 || math.IsNaN(n.Start) || math.IsInf(n.Start, 0):
		return fmt.Errorf("start %v must be 0 or more beats", n.Start)
	case n.Duration <= 0 || math.IsNaN(n.Duration) || math.IsInf(n.Duration, 0):
		return fmt.Errorf("duration %v must be more than 0 beats", n.Duration)
	case n.Velocity < 1 || n.Velocity > 127:
		return fmt.Errorf("velocity %d out of range 1-127", n.Velocity)
	}
	return nil
}

// ParseNotes decodes a JSON array of {pitch, start, duration, velocity, mute}
// objects (velocity defaults to 100, mute to false) and validates each note.
func ParseNotes(raw any) ([]Note, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var items []map[string]any
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, fmt.Errorf("notes must be a JSON array of objects: %w", err)
	}
	notes := make([]Note, 0, len(items))
	for i, it := range items {
		num := func(key string, def float64, required bool) (float64, error) {
			v, ok := it[key]
			if !ok || v == nil {
				if required {
					return 0, fmt.Errorf("note %d: %q is required", i+1, key)
				}
				return def, nil
			}
			f, ok := v.(float64)
			if !ok {
				return 0, fmt.Errorf("note %d: %q must be a number", i+1, key)
			}
			return f, nil
		}
		pitch, err := num("pitch", 0, true)
		if err != nil {
			return nil, err
		}
		start, err := num("start", 0, true)
		if err != nil {
			return nil, err
		}
		dur, err := num("duration", 0, true)
		if err != nil {
			return nil, err
		}
		vel, err := num("velocity", 100, false)
		if err != nil {
			return nil, err
		}
		mute := false
		if v, ok := it["mute"]; ok && v != nil {
			bv, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("note %d: \"mute\" must be true or false", i+1)
			}
			mute = bv
		}
		if pitch != math.Trunc(pitch) || vel != math.Trunc(vel) {
			return nil, fmt.Errorf("note %d: pitch and velocity must be whole numbers", i+1)
		}
		n := Note{Pitch: int(pitch), Start: start, Duration: dur, Velocity: int(vel), Mute: mute}
		if err := ValidateNote(n); err != nil {
			return nil, fmt.Errorf("note %d: %w", i+1, err)
		}
		notes = append(notes, n)
	}
	return notes, nil
}

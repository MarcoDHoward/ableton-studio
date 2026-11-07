// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package live

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestOSCRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		args []any
		want []any
	}{
		{"none", nil, []any{}},
		{"int", []any{3}, []any{int32(3)}},
		{"int64", []any{int64(1) << 40}, []any{int64(1) << 40}},
		{"float", []any{120.5}, []any{float32(120.5)}},
		{"string", []any{"Bass"}, []any{"Bass"}},
		{"string pad 4", []any{"abcd"}, []any{"abcd"}},
		{"bools nil", []any{true, false, nil}, []any{true, false, nil}},
		{"blob", []any{Blob{1, 2, 3}}, []any{Blob{1, 2, 3}}},
		{"mixed", []any{int32(0), "Lead Synth", float32(0.85), true}, []any{int32(0), "Lead Synth", float32(0.85), true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkt, err := EncodeOSC("/live/track/set/name", tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if len(pkt)%4 != 0 {
				t.Fatalf("packet length %d not 4-aligned", len(pkt))
			}
			addr, vals, err := DecodeOSC(pkt)
			if err != nil {
				t.Fatal(err)
			}
			if addr != "/live/track/set/name" {
				t.Fatalf("addr = %q", addr)
			}
			if !reflect.DeepEqual(vals, tc.want) {
				t.Fatalf("vals = %#v, want %#v", vals, tc.want)
			}
		})
	}
}

func TestOSCDecodeDoubleAndMalformed(t *testing.T) {
	// /x ,d 1.5
	pkt := []byte{'/', 'x', 0, 0, ',', 'd', 0, 0}
	pkt = append(pkt, 0x3f, 0xf8, 0, 0, 0, 0, 0, 0)
	_, vals, err := DecodeOSC(pkt)
	if err != nil || len(vals) != 1 || vals[0].(float64) != 1.5 {
		t.Fatalf("double decode: %v %v", vals, err)
	}
	bad := [][]byte{
		nil,
		[]byte("nope"),
		{'/', 'x', 0, 0, ',', 'i', 0, 0, 0}, // truncated int
		{'/', 'x', 0, 0, ',', 'b', 0, 0, 0, 0, 0, 9}, // blob past end
		{'/', 'x', 0, 0, 'i', 0, 0, 0},               // no comma
	}
	for i, b := range bad {
		if _, _, err := DecodeOSC(b); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
	if _, err := EncodeOSC("no-slash"); err == nil {
		t.Error("expected address error")
	}
	if _, err := EncodeOSC("/x", struct{}{}); err == nil {
		t.Error("expected type error")
	}
}

func TestValueHelpers(t *testing.T) {
	if v, _ := AsFloat(float32(0.85)); v != 0.85 {
		t.Errorf("Clean32 = %v", v)
	}
	if v, ok := AsInt(float32(3)); !ok || v != 3 {
		t.Errorf("AsInt float = %v %v", v, ok)
	}
	if _, ok := AsInt(float32(3.5)); ok {
		t.Error("AsInt 3.5 should fail")
	}
	if b, ok := AsBool(int32(1)); !ok || !b {
		t.Error("AsBool(1)")
	}
	if s, ok := AsString(nil); ok || s != "" {
		t.Error("AsString(nil)")
	}
}

func TestParsePosition(t *testing.T) {
	cases := []struct {
		in       string
		num, den int
		want     float64
		wantErr  bool
	}{
		{"64", 4, 4, 64, false},
		{"0", 4, 4, 0, false},
		{"66.5b", 4, 4, 66.5, false},
		{"1.1", 4, 4, 0, false},
		{"17.1", 4, 4, 64, false},
		{"17.3", 4, 4, 66, false},
		{"17.1.2", 4, 4, 64.25, false},
		{"2.1", 3, 4, 3, false},
		{"2.2", 6, 8, 3.5, false},
		{"2.2.2", 6, 8, 3.75, false},
		{"1.5", 4, 4, 0, true},
		{"0.1", 4, 4, 0, true},
		{"1.1.5", 4, 4, 0, true},
		{"1.1.3", 6, 8, 0, true},
		{"-4", 4, 4, 0, true},
		{"abc", 4, 4, 0, true},
		{"", 4, 4, 0, true},
		{"1.2.3.4", 4, 4, 0, true},
		{"4", 4, 3, 0, true},
	}
	for _, tc := range cases {
		got, err := ParsePosition(tc.in, tc.num, tc.den)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParsePosition(%q,%d/%d) err = %v, wantErr %v", tc.in, tc.num, tc.den, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("ParsePosition(%q,%d/%d) = %v, want %v", tc.in, tc.num, tc.den, got, tc.want)
		}
	}
}

func TestFormatPosition(t *testing.T) {
	cases := []struct {
		beats    float64
		num, den int
		want     string
	}{
		{0, 4, 4, "1.1"},
		{64, 4, 4, "17.1"},
		{66, 4, 4, "17.3"},
		{64.25, 4, 4, "17.1.2"},
		{64.3, 4, 4, "17.1.2"},
		{3, 3, 4, "2.1"},
		{3.5, 6, 8, "2.2"},
		{3.75, 6, 8, "2.2.2"},
		{7.999999999, 4, 4, "3.1"},
	}
	for _, tc := range cases {
		if got := FormatPosition(tc.beats, tc.num, tc.den); got != tc.want {
			t.Errorf("FormatPosition(%v,%d/%d) = %q, want %q", tc.beats, tc.num, tc.den, got, tc.want)
		}
		// round trip for on-grid positions
		if back, err := ParsePosition(FormatPosition(tc.beats, tc.num, tc.den), tc.num, tc.den); err != nil || back > tc.beats+1e-6 {
			t.Errorf("round trip %v -> %v (%v)", tc.beats, back, err)
		}
	}
	if got := BarsToBeats(4, 6, 8); got != 12 {
		t.Errorf("BarsToBeats(4, 6/8) = %v", got)
	}
}

func TestResolve(t *testing.T) {
	names := []string{"Drums", "Bass", "Bass 2", "Lead Synth", "Pad", "12", "dub siren"}
	cases := []struct {
		in        string
		want      int
		ambiguous bool
		notFound  bool
	}{
		{"1", 0, false, false},
		{"7", 6, false, false},
		{"bass", 1, false, false}, // exact beats prefix
		{"BASS 2", 2, false, false},
		{"lead", 3, false, false},  // unique prefix
		{"synth", 3, false, false}, // unique substring
		{"Ba", -1, true, false},    // ambiguous prefix
		{"a", -1, true, false},     // ambiguous substring
		{"12", 5, false, false},    // out-of-range number falls back to name
		{"0", -1, false, true},
		{"Guitar", -1, false, true},
		{"", -1, false, true},
	}
	for _, tc := range cases {
		got, err := Resolve("track", tc.in, names)
		var amb *AmbiguousError
		var nf *NotFoundError
		switch {
		case tc.ambiguous:
			if !errors.As(err, &amb) || len(amb.Candidates) < 2 {
				t.Errorf("Resolve(%q) = %d, %v; want ambiguous", tc.in, got, err)
			}
		case tc.notFound:
			if !errors.As(err, &nf) {
				t.Errorf("Resolve(%q) = %d, %v; want not found", tc.in, got, err)
			}
		default:
			if err != nil || got != tc.want {
				t.Errorf("Resolve(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
			}
		}
	}
	_, err := Resolve("track", "Bas", []string{"Drums", "Keys"})
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want not found, got %v", err)
	}
	if _, err := Resolve("track", "Basss", names); err == nil {
		t.Fatal("expected not found")
	} else if errors.As(err, &nf) && len(nf.Suggestions) == 0 {
		t.Errorf("expected suggestions for a near miss: %v", err)
	}
}

func TestSendsAndSlots(t *testing.T) {
	cases := []struct {
		in    string
		count int
		want  int
		ok    bool
	}{
		{"A", 2, 0, true},
		{"b", 2, 1, true},
		{"2", 2, 1, true},
		{"C", 2, -1, false},
		{"3", 2, -1, false},
	}
	for _, tc := range cases {
		got, err := ResolveSend(tc.in, tc.count)
		if (err == nil) != tc.ok || (tc.ok && got != tc.want) {
			t.Errorf("ResolveSend(%q,%d) = %d, %v", tc.in, tc.count, got, err)
		}
	}
	if SendLetter(0) != "A" || SendLetter(25) != "Z" || SendLetter(26) != "AA" {
		t.Error("SendLetter")
	}
	if i, err := ParseSlot("slot", "3"); err != nil || i != 2 {
		t.Error("ParseSlot 3")
	}
	if _, err := ParseSlot("slot", "0"); err == nil {
		t.Error("ParseSlot 0 should fail")
	}
}

func TestEnums(t *testing.T) {
	cases := []struct {
		e    Enum
		in   string
		want int
		ok   bool
	}{
		{ClipLaunchQuantization, "1/4", 8, true},
		{ClipLaunchQuantization, "1/8T", 11, true},
		{ClipLaunchQuantization, "Global", 0, true},
		{ClipLaunchQuantization, "1 bar", 5, true},
		{ClipLaunchQuantization, "3", 3, true},
		{SongClipTriggerQuantization, "1 Bar", 4, true},
		{SongClipTriggerQuantization, "global", 0, false},
		{MidiRecordingQuantization, "1/16", 5, true},
		{WarpMode, "complex pro", 6, true},
		{WarpMode, "Re-Pitch", 3, true},
		{WarpMode, "5", 0, false},
		{LaunchMode, "gate", 1, true},
		{Monitoring, "off", 2, true},
		{Monitoring, "loud", 0, false},
	}
	for _, tc := range cases {
		got, err := tc.e.Parse(tc.in)
		if (err == nil) != tc.ok || (tc.ok && got != tc.want) {
			t.Errorf("%s.Parse(%q) = %d, %v", tc.e.Kind, tc.in, got, err)
		}
	}
	if ClipLaunchQuantization.Name(8) != "1/4" || WarpMode.Name(5) != "5" {
		t.Error("Enum.Name")
	}
	for in, want := range map[string]bool{"on": true, "OFF": false, "1": true, "false": false} {
		if got, err := ParseBool(in); err != nil || got != want {
			t.Errorf("ParseBool(%q)", in)
		}
	}
	if _, err := ParseBool("maybe"); err == nil {
		t.Error("ParseBool(maybe)")
	}
	if NoteName(60) != "C3" || NoteName(36) != "C1" || NoteName(0) != "C-2" {
		t.Error("NoteName")
	}
	if n, err := ParseRootNote("Eb"); err != nil || n != 3 {
		t.Error("ParseRootNote Eb")
	}
	if DeviceTypeName(1) != "instrument" || DeviceTypeName(2) != "audio_effect" || DeviceTypeName(4) != "midi_effect" {
		t.Error("DeviceTypeName")
	}
	for in, want := range map[string]float64{"-6.0 dB": -6, "0.00 dB": 0, "3.5dB": 3.5} {
		if got, ok := ParseDBText(in); !ok || got != want {
			t.Errorf("ParseDBText(%q) = %v %v", in, got, ok)
		}
	}
	if _, ok := ParseDBText("-inf dB"); ok {
		t.Error("ParseDBText(-inf)")
	}
}

func TestNotes(t *testing.T) {
	flat := []any{int32(62), float32(1), float32(0.5), float32(100), false, int32(60), float32(0), float32(1), float32(90.0), true}
	notes, err := NotesFromFlat(flat)
	if err != nil {
		t.Fatal(err)
	}
	want := []Note{{60, 0, 1, 90, true}, {62, 1, 0.5, 100, false}}
	if !reflect.DeepEqual(notes, want) {
		t.Fatalf("notes = %+v", notes)
	}
	if _, err := NotesFromFlat(flat[:4]); err == nil {
		t.Error("expected error for partial note")
	}
	back, err := NotesFromFlat(FlattenNotes(want))
	if err != nil || !reflect.DeepEqual(back, want) {
		t.Fatalf("flatten round trip: %+v %v", back, err)
	}
	many := make([]Note, 130)
	chunks := ChunkNotes(many, 64)
	if len(chunks) != 3 || len(chunks[2]) != 2 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	parsed, err := ParseNotes([]any{map[string]any{"pitch": 60.0, "start": 0.0, "duration": 1.0}})
	if err != nil || parsed[0].Velocity != 100 {
		t.Fatalf("ParseNotes default velocity: %+v %v", parsed, err)
	}
	bad := []any{
		[]any{map[string]any{"pitch": 128.0, "start": 0.0, "duration": 1.0}},
		[]any{map[string]any{"pitch": 60.0, "start": 0.0, "duration": 0.0}},
		[]any{map[string]any{"pitch": 60.0, "duration": 1.0}},
		[]any{map[string]any{"pitch": 60.5, "start": 0.0, "duration": 1.0}},
		"not an array",
	}
	for i, b := range bad {
		if _, err := ParseNotes(b); err == nil {
			t.Errorf("bad case %d accepted", i)
		}
	}
}

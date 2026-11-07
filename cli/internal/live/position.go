// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package live

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Live measures song time in beats, where one beat is a quarter note no
// matter what the time signature says. Bar.beat positions follow Live's
// display: the beat inside a bar is one denominator note long (an eighth
// note in 6/8), and the optional third field counts sixteenth notes.

const posEpsilon = 1e-6

// ValidSignature reports whether num/den is a time signature Live accepts.
func ValidSignature(num, den int) bool {
	if num < 1 || num > 99 {
		return false
	}
	switch den {
	case 1, 2, 4, 8, 16:
		return true
	}
	return false
}

// BeatsPerBar is the length of one bar in quarter-note beats.
func BeatsPerBar(num, den int) float64 {
	return float64(num) * 4 / float64(den)
}

// BarsToBeats converts a length in bars to quarter-note beats.
func BarsToBeats(bars float64, num, den int) float64 {
	return bars * BeatsPerBar(num, den)
}

// ParsePosition turns "64" (beats), "66.5b" (fractional beats) or
// "17.1" / "17.1.2" (bar.beat[.sixteenth], all 1-based) into beats.
func ParsePosition(s string, num, den int) (float64, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return 0, fmt.Errorf("empty position: use beats (64) or bar.beat (17.1)")
	}
	if !ValidSignature(num, den) {
		return 0, fmt.Errorf("invalid time signature %d/%d", num, den)
	}
	lower := strings.ToLower(raw)
	if strings.HasSuffix(lower, "b") {
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(lower, "b")), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return 0, fmt.Errorf("invalid beat position %q: use a non-negative number of beats such as 66.5b", raw)
		}
		return v, nil
	}
	if !strings.Contains(raw, ".") {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid position %q: use beats (64), fractional beats (66.5b) or bar.beat (17.1)", raw)
		}
		return float64(n), nil
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("invalid position %q: use bar.beat (17.1) or bar.beat.sixteenth (17.1.3)", raw)
	}
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 1 {
			return 0, fmt.Errorf("invalid position %q: bar, beat and sixteenth are 1-based whole numbers", raw)
		}
		nums[i] = n
	}
	bar, beat := nums[0], nums[1]
	if beat > num {
		return 0, fmt.Errorf("invalid position %q: beat %d does not exist in %d/%d", raw, beat, num, den)
	}
	beatLen := 4 / float64(den)
	beats := float64(bar-1)*BeatsPerBar(num, den) + float64(beat-1)*beatLen
	if len(nums) == 3 {
		six := nums[2]
		maxSix := int(math.Round(beatLen / 0.25))
		if maxSix < 1 {
			maxSix = 1
		}
		if six > maxSix {
			return 0, fmt.Errorf("invalid position %q: a %d/%d beat has %d sixteenths", raw, num, den, maxSix)
		}
		beats += float64(six-1) * 0.25
	}
	return beats, nil
}

// FormatPosition renders beats as "bar.beat", or "bar.beat.sixteenth" when
// the position is not on a beat. Fractions smaller than a sixteenth are
// dropped from the display (the raw beat value is always reported too).
func FormatPosition(beats float64, num, den int) string {
	if !ValidSignature(num, den) || math.IsNaN(beats) || math.IsInf(beats, 0) {
		return strconv.FormatFloat(beats, 'f', -1, 64)
	}
	neg := beats < 0
	if neg {
		beats = -beats
	}
	bpb := BeatsPerBar(num, den)
	bar := math.Floor(beats/bpb + posEpsilon)
	rem := beats - bar*bpb
	if rem < 0 {
		rem = 0
	}
	beatLen := 4 / float64(den)
	beat := math.Floor(rem/beatLen + posEpsilon)
	rem2 := rem - beat*beatLen
	prefix := ""
	if neg {
		prefix = "-"
	}
	if rem2 < posEpsilon {
		return fmt.Sprintf("%s%d.%d", prefix, int(bar)+1, int(beat)+1)
	}
	six := math.Floor(rem2/0.25 + posEpsilon)
	return fmt.Sprintf("%s%d.%d.%d", prefix, int(bar)+1, int(beat)+1, int(six)+1)
}

// FormatSignature renders a time signature as "4/4".
func FormatSignature(num, den int) string {
	return fmt.Sprintf("%d/%d", num, den)
}

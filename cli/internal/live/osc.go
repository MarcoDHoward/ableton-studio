// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Package live holds the pure logic and the loopback transports used to talk
// to Ableton Live's Remote Scripts: an OSC 1.0 codec, bar.beat position math,
// name/number resolution, enum tables, note (de)flattening and the UDP/TCP
// clients for AbletonOSC, AbletonMCP and AbletonPPExtras.
//
// Names that come back from Live (tracks, clips, devices, browser items) are
// untrusted data. Nothing in this package interprets them.
package live

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// Blob is an OSC 'b' argument.
type Blob []byte

func oscPad(b []byte) []byte {
	b = append(b, 0)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func oscPadBlob(b []byte) []byte {
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// EncodeOSC builds one OSC 1.0 message. Supported argument types:
// int/int32 (i), int64 (h), float32/float64 (f; AbletonOSC reads every float
// as a Python float), string (s), bool (T/F), nil (N) and Blob/[]byte (b).
func EncodeOSC(address string, args ...any) ([]byte, error) {
	if len(address) == 0 || address[0] != '/' {
		return nil, fmt.Errorf("OSC address must start with '/': %q", address)
	}
	if bytes.IndexByte([]byte(address), 0) >= 0 {
		return nil, errors.New("OSC address contains a NUL byte")
	}
	var out, payload bytes.Buffer
	out.Write(oscPad([]byte(address)))
	tags := []byte{','}
	for _, a := range args {
		switch v := a.(type) {
		case int:
			if v < math.MinInt32 || v > math.MaxInt32 {
				tags = append(tags, 'h')
				_ = binary.Write(&payload, binary.BigEndian, int64(v))
			} else {
				tags = append(tags, 'i')
				_ = binary.Write(&payload, binary.BigEndian, int32(v))
			}
		case int32:
			tags = append(tags, 'i')
			_ = binary.Write(&payload, binary.BigEndian, v)
		case int64:
			tags = append(tags, 'h')
			_ = binary.Write(&payload, binary.BigEndian, v)
		case float32:
			tags = append(tags, 'f')
			_ = binary.Write(&payload, binary.BigEndian, v)
		case float64:
			tags = append(tags, 'f')
			_ = binary.Write(&payload, binary.BigEndian, float32(v))
		case string:
			if bytes.IndexByte([]byte(v), 0) >= 0 {
				return nil, errors.New("OSC string argument contains a NUL byte")
			}
			tags = append(tags, 's')
			payload.Write(oscPad([]byte(v)))
		case bool:
			if v {
				tags = append(tags, 'T')
			} else {
				tags = append(tags, 'F')
			}
		case nil:
			tags = append(tags, 'N')
		case Blob:
			tags = append(tags, 'b')
			_ = binary.Write(&payload, binary.BigEndian, int32(len(v)))
			payload.Write(oscPadBlob(append([]byte(nil), v...)))
		case []byte:
			tags = append(tags, 'b')
			_ = binary.Write(&payload, binary.BigEndian, int32(len(v)))
			payload.Write(oscPadBlob(append([]byte(nil), v...)))
		default:
			return nil, fmt.Errorf("unsupported OSC argument type %T", a)
		}
	}
	out.Write(oscPad(tags))
	out.Write(payload.Bytes())
	return out.Bytes(), nil
}

func readOSCString(b []byte, pos int) (string, int, error) {
	if pos > len(b) {
		return "", 0, errors.New("OSC string past end of packet")
	}
	end := bytes.IndexByte(b[pos:], 0)
	if end < 0 {
		return "", 0, errors.New("unterminated OSC string")
	}
	s := string(b[pos : pos+end])
	next := pos + end + 1
	for next%4 != 0 {
		next++
	}
	return s, next, nil
}

// DecodeOSC parses one OSC 1.0 message. It never panics on malformed input.
// Decoded types: i -> int32, h -> int64, f -> float32, d -> float64,
// s -> string, T/F -> bool, N -> nil, b -> Blob.
func DecodeOSC(b []byte) (string, []any, error) {
	addr, pos, err := readOSCString(b, 0)
	if err != nil {
		return "", nil, err
	}
	if len(addr) == 0 || addr[0] != '/' {
		return "", nil, fmt.Errorf("not an OSC message (address %q)", addr)
	}
	if pos >= len(b) {
		return addr, nil, nil
	}
	tags, pos, err := readOSCString(b, pos)
	if err != nil {
		return addr, nil, err
	}
	if len(tags) == 0 || tags[0] != ',' {
		return addr, nil, errors.New("bad OSC type tag string")
	}
	need := func(n int) error {
		if pos+n > len(b) {
			return errors.New("OSC argument past end of packet")
		}
		return nil
	}
	vals := make([]any, 0, len(tags)-1)
	for _, t := range tags[1:] {
		switch t {
		case 'i':
			if err := need(4); err != nil {
				return addr, vals, err
			}
			vals = append(vals, int32(binary.BigEndian.Uint32(b[pos:])))
			pos += 4
		case 'f':
			if err := need(4); err != nil {
				return addr, vals, err
			}
			vals = append(vals, math.Float32frombits(binary.BigEndian.Uint32(b[pos:])))
			pos += 4
		case 'h':
			if err := need(8); err != nil {
				return addr, vals, err
			}
			vals = append(vals, int64(binary.BigEndian.Uint64(b[pos:])))
			pos += 8
		case 'd':
			if err := need(8); err != nil {
				return addr, vals, err
			}
			vals = append(vals, math.Float64frombits(binary.BigEndian.Uint64(b[pos:])))
			pos += 8
		case 's', 'S':
			var s string
			s, pos, err = readOSCString(b, pos)
			if err != nil {
				return addr, vals, err
			}
			vals = append(vals, s)
		case 'b':
			if err := need(4); err != nil {
				return addr, vals, err
			}
			n := int(int32(binary.BigEndian.Uint32(b[pos:])))
			pos += 4
			if n < 0 {
				return addr, vals, errors.New("negative OSC blob size")
			}
			if err := need(n); err != nil {
				return addr, vals, err
			}
			vals = append(vals, Blob(append([]byte(nil), b[pos:pos+n]...)))
			pos += n
			for pos%4 != 0 {
				pos++
			}
		case 'T':
			vals = append(vals, true)
		case 'F':
			vals = append(vals, false)
		case 'N':
			vals = append(vals, nil)
		default:
			return addr, vals, fmt.Errorf("unsupported OSC type tag %q", t)
		}
	}
	return addr, vals, nil
}

// Clean32 turns a float32 that came off the wire into the float64 a person
// would write (0.85, not 0.8500000238418579).
func Clean32(f float32) float64 {
	v, err := strconv.ParseFloat(strconv.FormatFloat(float64(f), 'g', -1, 32), 64)
	if err != nil {
		return float64(f)
	}
	return v
}

// AsFloat converts a decoded OSC value to float64.
func AsFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float32:
		return Clean32(x), true
	case float64:
		return x, true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// AsInt converts a decoded OSC value to int (floats are truncated only when
// they hold an integral value).
func AsInt(v any) (int, bool) {
	switch x := v.(type) {
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case int:
		return x, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case float32:
		if float32(int(x)) == x {
			return int(x), true
		}
	case float64:
		if float64(int(x)) == x {
			return int(x), true
		}
	}
	return 0, false
}

// AsBool converts a decoded OSC value (T/F or 0/1) to bool.
func AsBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case int32:
		return x != 0, true
	case int64:
		return x != 0, true
	case int:
		return x != 0, true
	case float32:
		return x != 0, true
	case float64:
		return x != 0, true
	}
	return false, false
}

// AsString converts a decoded OSC value to a string (nil -> "", ok=false).
func AsString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case nil:
		return "", false
	}
	return fmt.Sprint(v), true
}

// JSONValue maps a decoded OSC value onto a JSON-friendly Go value.
func JSONValue(v any) any {
	switch x := v.(type) {
	case float32:
		return Clean32(x)
	case int32:
		return int(x)
	case int64:
		return x
	case Blob:
		return []byte(x)
	}
	return v
}

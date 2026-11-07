// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package live

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

// fakeOSC answers on an ephemeral UDP port: /live/song/get/tempo -> 120,
// /live/track/get/mute t -> (t, false) except track 9 which errors, and a
// stray listener packet before every reply.
func fakeOSC(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 65536)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			addr, args, err := DecodeOSC(buf[:n])
			if err != nil {
				continue
			}
			reply := func(a string, vals ...any) {
				pkt, _ := EncodeOSC(a, vals...)
				_, _ = pc.WriteTo(pkt, from)
			}
			reply("/live/song/get/beat", int32(7)) // stray listener event
			switch addr {
			case "/live/song/get/tempo":
				reply(addr, float32(120))
			case "/live/track/get/mute":
				if v, _ := AsInt(args[0]); v == 9 {
					reply("/live/error", "Error handling OSC message: list index out of range")
				} else {
					reply(addr, args[0], false)
				}
			case "/live/silent":
				// never answers
			}
		}
	}()
	return pc.LocalAddr().String()
}

func TestOSCConnQuery(t *testing.T) {
	remote := fakeOSC(t)
	o, err := DialOSC("127.0.0.1:0", remote)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	o.Timeout = 500 * time.Millisecond
	ctx := context.Background()

	vals, err := o.Query(ctx, "/live/song/get/tempo", 0)
	if err != nil || len(vals) != 1 {
		t.Fatalf("tempo: %v %v", vals, err)
	}
	if f, _ := AsFloat(vals[0]); f != 120 {
		t.Fatalf("tempo = %v", f)
	}

	reqs := []Request{Q("/live/track/get/mute", 1, 0), Q("/live/track/get/mute", 1, 9), Q("/live/track/get/mute", 1, 2)}
	res, errs, err := o.QueryMany(ctx, reqs, true)
	if err != nil {
		t.Fatal(err)
	}
	if errs[0] != nil || errs[1] == nil || errs[2] != nil || len(res[2]) != 1 {
		t.Fatalf("tolerant batch: %v %v", res, errs)
	}
	if _, _, err := o.QueryMany(ctx, reqs, false); err == nil {
		t.Fatal("strict batch should fail on /live/error")
	} else {
		var re *RemoteError
		if !errors.As(err, &re) {
			t.Fatalf("want RemoteError, got %T", err)
		}
	}
	// drain anything left from the strict batch
	time.Sleep(50 * time.Millisecond)

	_, err = o.Query(ctx, "/live/silent", 0)
	var ue *UnavailableError
	if !errors.As(err, &ue) || ue.Bridge != BridgeOSC {
		t.Fatalf("silent: want UnavailableError, got %v", err)
	}
}

func TestOSCPortInUse(t *testing.T) {
	a, err := DialOSC("127.0.0.1:0", "127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	BindWait = 200 * time.Millisecond
	defer func() { BindWait = 2 * time.Second }()
	_, err = DialOSC(a.LocalAddr(), "127.0.0.1:9")
	var pe *PortInUseError
	if !errors.As(err, &pe) {
		t.Fatalf("want PortInUseError, got %v", err)
	}
}

func TestTCPCommand(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var req struct {
					Type   string         `json:"type"`
					Params map[string]any `json:"params"`
				}
				if err := json.NewDecoder(c).Decode(&req); err != nil {
					return
				}
				switch req.Type {
				case "ping":
					_, _ = c.Write([]byte(`{"status":"success","result":{"ok":true}}` + "\n"))
				default:
					_, _ = c.Write([]byte(`{"status":"error","message":"no such device","code":"not_found"}`))
				}
			}(c)
		}
	}()
	addr := ln.Addr().String()
	ctx := context.Background()
	res, err := TCPCommand(ctx, BridgePPX, addr, "ping", nil, TCPOptions{HalfClose: true})
	if err != nil || string(res) != `{"ok":true}` {
		t.Fatalf("ping: %s %v", res, err)
	}
	_, err = TCPCommand(ctx, BridgePPX, addr, "delete_device", map[string]any{"track": 0}, TCPOptions{})
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != "not_found" {
		t.Fatalf("want RemoteError not_found, got %v", err)
	}
	_, err = TCPCommand(ctx, BridgeMCP, "10.0.0.1:9877", "ping", nil, TCPOptions{})
	if err == nil {
		t.Fatal("non-loopback address must be refused")
	}
	// closed port -> unavailable
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln2.Addr().String()
	ln2.Close()
	_, err = TCPCommand(ctx, BridgeMCP, closed, "ping", nil, TCPOptions{DialTimeout: 300 * time.Millisecond})
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("want UnavailableError, got %v", err)
	}
}

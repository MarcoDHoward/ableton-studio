// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package live

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Loopback endpoints of the three Remote Scripts. Nothing in this CLI talks
// to any other host.
const (
	OSCSendAddr  = "127.0.0.1:11000"
	OSCReplyAddr = "127.0.0.1:11001"
	MCPAddr      = "127.0.0.1:9877"
	PPXAddr      = "127.0.0.1:9878"
)

// Bridge names used in errors, dry-run output and health.
const (
	BridgeOSC = "abletonosc"
	BridgeMCP = "abletonmcp"
	BridgePPX = "abletonppextras"
)

// BindWait is how long DialOSC retries when the reply port is busy.
var BindWait = 2 * time.Second

// DefaultOSCTimeout is how long to wait for AbletonOSC before giving up.
const DefaultOSCTimeout = 3 * time.Second

// UnavailableError: a bridge did not answer (not loaded, Live closed, or
// AbletonOSC paused because a Live menu is open).
type UnavailableError struct {
	Bridge string
	Detail string
}

func (e *UnavailableError) Error() string {
	return e.Detail
}

// PortInUseError: the AbletonOSC reply port could not be bound.
type PortInUseError struct {
	Addr string
	Err  error
}

func (e *PortInUseError) Error() string {
	return fmt.Sprintf("cannot listen for AbletonOSC replies on %s: another program (another ableton-pp-cli watch, TouchOSC, pylive) is using that port; close it and retry (%v)", e.Addr, e.Err)
}

// RemoteError: a bridge answered with an error.
type RemoteError struct {
	Bridge  string
	Code    string // AbletonPPExtras error code, if any
	Message string
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("%s: %s", e.Bridge, e.Message)
}

// Message is one decoded OSC packet.
type Message struct {
	Address string
	Args    []any
}

// OSCConn is one UDP socket bound to the AbletonOSC reply port. AbletonOSC
// answers every request on port 11001 of the sender's IP, so the socket that
// sends must be the one bound there.
type OSCConn struct {
	conn    *net.UDPConn
	remote  *net.UDPAddr
	Timeout time.Duration
	buf     []byte
}

// DialOSC binds localAddr and targets remoteAddr (both loopback in
// production; tests pass ephemeral ports).
func DialOSC(localAddr, remoteAddr string) (*OSCConn, error) {
	laddr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		return nil, err
	}
	raddr, err := net.ResolveUDPAddr("udp", remoteAddr)
	if err != nil {
		return nil, err
	}
	if !laddr.IP.IsLoopback() || !raddr.IP.IsLoopback() {
		return nil, fmt.Errorf("refusing OSC address %s -> %s: only 127.0.0.1 is allowed", localAddr, remoteAddr)
	}
	// Another short-lived ableton-pp-cli invocation may hold the reply port
	// for a moment; wait briefly before reporting it as taken.
	deadline := time.Now().Add(BindWait)
	var conn *net.UDPConn
	for {
		conn, err = net.ListenUDP("udp", laddr)
		if err == nil {
			break
		}
		if !time.Now().Before(deadline) {
			return nil, &PortInUseError{Addr: localAddr, Err: err}
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = conn.SetReadBuffer(4 << 20)
	return &OSCConn{conn: conn, remote: raddr, Timeout: DefaultOSCTimeout, buf: make([]byte, 65536)}, nil
}

// LocalAddr is the bound address (useful when tests bind port 0).
func (o *OSCConn) LocalAddr() string { return o.conn.LocalAddr().String() }

// Close releases the socket.
func (o *OSCConn) Close() error { return o.conn.Close() }

// Send fires one message without waiting.
func (o *OSCConn) Send(address string, args ...any) error {
	pkt, err := EncodeOSC(address, args...)
	if err != nil {
		return err
	}
	_, err = o.conn.WriteToUDP(pkt, o.remote)
	return err
}

// Recv waits for the next decodable message until deadline or ctx ends.
// It returns (nil, nil) on a plain read timeout.
func (o *OSCConn) Recv(ctx context.Context, deadline time.Time) (*Message, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d := deadline
		// Wake up regularly so a cancelled context is noticed promptly.
		if wake := time.Now().Add(250 * time.Millisecond); wake.Before(d) {
			d = wake
		}
		_ = o.conn.SetReadDeadline(d)
		n, from, err := o.conn.ReadFromUDP(o.buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				if !time.Now().Before(deadline) {
					return nil, nil
				}
				continue
			}
			return nil, err
		}
		// Only AbletonOSC's own socket may answer: another local process
		// sending to the reply port could otherwise forge track names.
		if from == nil || from.Port != o.remote.Port || !from.IP.IsLoopback() {
			continue
		}
		addr, vals, derr := DecodeOSC(o.buf[:n])
		if derr != nil {
			continue
		}
		return &Message{Address: addr, Args: vals}, nil
	}
}

// Request is one getter in a QueryMany batch. Echo is how many leading
// arguments AbletonOSC repeats in its reply (track/clip/device indexes).
type Request struct {
	Address string
	Echo    int
	Args    []any
}

// Q builds a Request whose leading Echo args are repeated in the reply.
func Q(address string, echo int, args ...any) Request {
	return Request{Address: address, Echo: echo, Args: args}
}

func sameArg(a, b any) bool {
	if fa, ok := AsFloat(a); ok {
		if fb, ok := AsFloat(b); ok {
			return fa == fb
		}
		return false
	}
	sa, oka := a.(string)
	sb, okb := b.(string)
	return oka && okb && sa == sb
}

func (r Request) matches(m *Message) bool {
	if m.Address != r.Address || len(m.Args) < r.Echo {
		return false
	}
	for i := 0; i < r.Echo && i < len(r.Args); i++ {
		if !sameArg(r.Args[i], m.Args[i]) {
			return false
		}
	}
	return true
}

func (o *OSCConn) noReply(address string) error {
	return &UnavailableError{Bridge: BridgeOSC, Detail: fmt.Sprintf("no reply from AbletonOSC for %s within %s", address, o.Timeout)}
}

// Query sends one getter and returns the reply values after the echoed
// prefix. A /live/error that arrives first is returned as a RemoteError.
func (o *OSCConn) Query(ctx context.Context, address string, echo int, args ...any) ([]any, error) {
	res, errs, err := o.QueryMany(ctx, []Request{{Address: address, Echo: echo, Args: args}}, false)
	if err != nil {
		return nil, err
	}
	if errs[0] != nil {
		return nil, errs[0]
	}
	return res[0], nil
}

// queryWindow bounds in-flight requests so a big batch (every parameter of a
// plugin) cannot overflow the socket buffer.
const queryWindow = 64

// QueryMany pipelines getters over the socket. AbletonOSC handles messages
// strictly in order and answers each getter with exactly one reply or one
// /live/error. In strict mode the first /live/error fails the batch; in
// tolerant mode it is assigned to the oldest unanswered request and the
// batch carries on (used for properties that raise on some tracks).
func (o *OSCConn) QueryMany(ctx context.Context, reqs []Request, tolerant bool) ([][]any, []error, error) {
	results := make([][]any, len(reqs))
	errs := make([]error, len(reqs))
	done := make([]bool, len(reqs))
	sent, finished := 0, 0
	// Stray packets (listener events, late replies) do not count as
	// progress; only an answered request extends the deadline.
	progress := time.Now().Add(o.Timeout)
	for finished < len(reqs) {
		for sent < len(reqs) && sent-finished < queryWindow {
			if err := o.Send(reqs[sent].Address, reqs[sent].Args...); err != nil {
				return nil, nil, err
			}
			sent++
		}
		msg, err := o.Recv(ctx, progress)
		if err != nil {
			return nil, nil, err
		}
		if msg == nil {
			first := 0
			for first < len(reqs) && done[first] {
				first++
			}
			return nil, nil, o.noReply(reqs[first].Address)
		}
		if msg.Address == "/live/error" {
			text := "unknown error"
			if len(msg.Args) > 0 {
				text = fmt.Sprint(msg.Args...)
			}
			rerr := &RemoteError{Bridge: BridgeOSC, Message: strings.TrimSpace(text)}
			if !tolerant {
				return nil, nil, rerr
			}
			for i := 0; i < sent; i++ {
				if !done[i] {
					done[i] = true
					errs[i] = rerr
					finished++
					progress = time.Now().Add(o.Timeout)
					break
				}
			}
			continue
		}
		for i := 0; i < sent; i++ {
			if done[i] || !reqs[i].matches(msg) {
				continue
			}
			done[i] = true
			finished++
			results[i] = msg.Args[reqs[i].Echo:]
			progress = time.Now().Add(o.Timeout)
			break
		}
	}
	return results, errs, nil
}

// tcpResponse is the reply envelope of AbletonMCP and AbletonPPExtras.
type tcpResponse struct {
	Status  string          `json:"status"`
	Result  json.RawMessage `json:"result"`
	Message string          `json:"message"`
	Code    string          `json:"code"`
}

// TCPOptions tune one TCP JSON command.
type TCPOptions struct {
	DialTimeout time.Duration
	ReadTimeout time.Duration
	HalfClose   bool // AbletonPPExtras: one request per connection, then FIN
}

// TCPCommand sends {"type","params"} to a loopback Remote Script and reads
// one complete JSON reply.
func TCPCommand(ctx context.Context, bridge, addr, cmdType string, params map[string]any, opt TCPOptions) (json.RawMessage, error) {
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		return nil, fmt.Errorf("refusing non-loopback bridge address %s", addr)
	}
	if opt.DialTimeout <= 0 {
		opt.DialTimeout = 2 * time.Second
	}
	if opt.ReadTimeout <= 0 {
		opt.ReadTimeout = 10 * time.Second
	}
	d := net.Dialer{Timeout: opt.DialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, &UnavailableError{Bridge: bridge, Detail: fmt.Sprintf("cannot reach %s on %s: %v", bridge, addr, err)}
	}
	defer conn.Close()
	if params == nil {
		params = map[string]any{}
	}
	req, err := json.Marshal(map[string]any{"type": cmdType, "params": params})
	if err != nil {
		return nil, err
	}
	if opt.HalfClose {
		req = append(req, '\n')
	}
	deadline := time.Now().Add(opt.ReadTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(req); err != nil {
		return nil, &UnavailableError{Bridge: bridge, Detail: fmt.Sprintf("sending to %s failed: %v", bridge, err)}
	}
	if opt.HalfClose {
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}
	dec := json.NewDecoder(bufio.NewReader(conn))
	var resp tcpResponse
	if err := dec.Decode(&resp); err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, &UnavailableError{Bridge: bridge, Detail: fmt.Sprintf("%s did not answer %s within %s (Live may be busy or a dialog is open)", bridge, cmdType, opt.ReadTimeout)}
		}
		return nil, &UnavailableError{Bridge: bridge, Detail: fmt.Sprintf("reading %s reply: %v", bridge, err)}
	}
	if resp.Status != "success" {
		msg := resp.Message
		if msg == "" {
			msg = "unknown error"
		}
		return nil, &RemoteError{Bridge: bridge, Code: resp.Code, Message: msg}
	}
	return resp.Result, nil
}

// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written: routes the synthetic spec's virtual REST paths to the local
// Ableton Live Remote Scripts instead of HTTP:
//   - AbletonOSC (OSC over UDP, send 127.0.0.1:11000, replies on 11001)
//   - AbletonMCP Remote Script (JSON over TCP 127.0.0.1:9877)
//   - AbletonPPExtras, our own script (JSON over TCP 127.0.0.1:9878)
//
// doInternal calls abletonRoute right after rejectUnresolvedPathParams. When
// the base URL is the virtual osc:// scheme every request is answered here;
// when tests or verify point the base URL at an http(s) mock server the
// router steps aside and the normal HTTP path runs.
//
// Security: only loopback endpoints are ever contacted; drain_passive_events
// and prompt text are never sent; get_session_snapshot is only called by the
// /snapshot route, and that route refuses requests whose context was not
// marked by WithSessionSnapshot (snapshot save / snapshot diff --live);
// names returned by Live are treated as data.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"ableton-pp-cli/internal/live"
)

// Bridge endpoints. Package variables only so tests can point them at
// fakes; production always uses the loopback constants.
var (
	abletonOSCLocal  = live.OSCReplyAddr
	abletonOSCRemote = live.OSCSendAddr
	abletonMCPAddr   = live.MCPAddr
	abletonPPXAddr   = live.PPXAddr
)

// JournalEntry is one change this CLI made in Live. before/after are JSON
// values. Read by the journal/revert commands.
type JournalEntry struct {
	Command  string
	Method   string
	Path     string
	Target   string
	Property string
	Before   any
	After    any
}

// JournalHook persists a JournalEntry and returns its row id. The cli
// package installs it at startup (it owns the local store); a nil hook means
// changes are not journaled.
var JournalHook func(ctx context.Context, e JournalEntry) (int64, error)

// JournalCommand is the CLI command path being run, recorded in each entry.
var JournalCommand string

// abletonVirtual reports whether requests target the virtual osc:// scheme.
func (c *Client) abletonVirtual() bool {
	if c == nil {
		return false
	}
	base := strings.ToLower(strings.TrimSpace(c.BaseURL))
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		return false
	}
	return strings.Contains(base, "osc://")
}

// IsAbletonVirtual exposes abletonVirtual to hand-written commands.
func (c *Client) IsAbletonVirtual() bool { return c.abletonVirtual() }

type routeHandler func(r *routeReq) (any, error)

type route struct {
	method  string
	pattern string
	bridge  string
	command string
	handler routeHandler
	segs    []string
}

type routeErr struct {
	status int
	msg    string
}

func (e *routeErr) Error() string { return e.msg }

func badInput(format string, args ...any) error {
	return &routeErr{status: http.StatusBadRequest, msg: fmt.Sprintf(format, args...)}
}

func notFound(format string, args ...any) error {
	return &routeErr{status: http.StatusNotFound, msg: fmt.Sprintf(format, args...)}
}

func conflict(format string, args ...any) error {
	return &routeErr{status: http.StatusConflict, msg: fmt.Sprintf(format, args...)}
}

const (
	helpOSC = "Check that Live is open and that AbletonOSC is selected as a Control Surface in Live > Settings > Link, Tempo & MIDI (Input: None, Output: None). AbletonOSC also pauses while a Live menu or dialog is open: close it and retry."
	helpMCP = "Select AbletonMCP as a Control Surface in Live > Settings > Link, Tempo & MIDI (Input: None, Output: None)."
	helpPPX = "This command needs the AbletonPPExtras Remote Script: run 'ableton-pp-cli scripts install', then select AbletonPPExtras as a Control Surface in Live > Settings > Link, Tempo & MIDI (Input: None, Output: None)."
)

func bridgeHelp(bridge string) string {
	switch bridge {
	case live.BridgeMCP:
		return helpMCP
	case live.BridgePPX:
		return helpPPX
	}
	return helpOSC
}

// classifyRouteError maps an error to the HTTP-style status the generated
// commands turn into typed exit codes (404 -> 3, others -> 5).
func classifyRouteError(err error) (int, string) {
	var re *routeErr
	var nf *live.NotFoundError
	var amb *live.AmbiguousError
	var pe *live.PortInUseError
	var ue *live.UnavailableError
	var rem *live.RemoteError
	switch {
	case errors.As(err, &re):
		return re.status, re.msg
	case errors.As(err, &nf):
		return http.StatusNotFound, nf.Error()
	case errors.As(err, &amb):
		// 300, not 409: under --idempotent the generated commands treat 409 as
		// "already exists" and exit 0, which would hide an ambiguous name.
		return http.StatusMultipleChoices, amb.Error()
	case errors.As(err, &pe):
		return http.StatusServiceUnavailable, pe.Error()
	case errors.As(err, &ue):
		return http.StatusServiceUnavailable, strings.TrimRight(ue.Error(), ".") + ". " + bridgeHelp(ue.Bridge)
	case errors.As(err, &rem):
		msg := rem.Message
		if rem.Bridge == live.BridgePPX {
			switch rem.Code {
			case "not_found":
				return http.StatusNotFound, "AbletonPPExtras: " + msg
			case "unsupported":
				return http.StatusNotImplemented, "AbletonPPExtras: " + msg + " (this Live version does not offer the API this command needs)"
			case "internal":
				return http.StatusInternalServerError, "AbletonPPExtras: " + msg
			}
			return http.StatusBadRequest, "AbletonPPExtras: " + msg
		}
		lower := strings.ToLower(msg)
		if strings.Contains(lower, "out of range") {
			return http.StatusNotFound, fmt.Sprintf("Live rejected the request (%s): %s", rem.Bridge, msg)
		}
		return http.StatusBadRequest, fmt.Sprintf("Live rejected the request (%s): %s", rem.Bridge, msg)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return http.StatusServiceUnavailable, "timed out talking to Live: " + err.Error()
	}
	return http.StatusInternalServerError, err.Error()
}

var abletonRoutes []*route

func init() {
	abletonRoutes = buildAbletonRoutes()
	for _, rt := range abletonRoutes {
		rt.segs = strings.Split(strings.Trim(rt.pattern, "/"), "/")
	}
}

// matchRoute finds the first route whose method and pattern fit. Literal
// routes are listed before their {var} siblings in the table.
func matchRoute(method, path string) (*route, map[string]string, bool, error) {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	pathKnown := false
	for _, rt := range abletonRoutes {
		if len(rt.segs) != len(segs) {
			continue
		}
		vars := map[string]string{}
		ok := true
		for i, p := range rt.segs {
			if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
				v, err := url.PathUnescape(segs[i])
				if err != nil {
					return nil, nil, false, badInput("bad path segment %q: %v", segs[i], err)
				}
				vars[p[1:len(p)-1]] = v
				continue
			}
			if p != segs[i] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		pathKnown = true
		if rt.method == method {
			return rt, vars, true, nil
		}
	}
	if pathKnown {
		return nil, nil, false, &routeErr{status: http.StatusMethodNotAllowed, msg: fmt.Sprintf("%s is not supported on %s", method, path)}
	}
	return nil, nil, false, notFound("no Ableton route for %s %s", method, path)
}

// abletonRoute answers a request from Live when the client targets the
// virtual osc:// base URL. routed=false hands the request back to HTTP.
func (c *Client) abletonRoute(ctx context.Context, method, path string, params map[string]string, body any) (json.RawMessage, int, bool, error) {
	if !c.abletonVirtual() {
		return nil, 0, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fail := func(err error) (json.RawMessage, int, bool, error) {
		status, msg := classifyRouteError(err)
		return nil, status, true, &APIError{Method: method, Path: path, StatusCode: status, Body: msg}
	}
	merged := map[string]string{}
	for k, v := range params {
		merged[k] = v
	}
	rawPath := path
	if i := strings.IndexByte(rawPath, '?'); i >= 0 {
		if q, err := url.ParseQuery(rawPath[i+1:]); err == nil {
			for k, vs := range q {
				if len(vs) > 0 {
					merged[k] = vs[0]
				}
			}
		}
		rawPath = rawPath[:i]
	}
	rt, vars, _, err := matchRoute(method, rawPath)
	if err != nil {
		return fail(err)
	}
	bodyMap, err := normalizeBody(body)
	if err != nil {
		return fail(err)
	}
	if c.IsDryRun() {
		out, _ := json.Marshal(map[string]any{
			"dry_run": true,
			"would":   dryRunPlan(rt, vars, merged, bodyMap),
		})
		return out, http.StatusOK, true, nil
	}
	r := &routeReq{ctx: ctx, c: c, method: method, path: rawPath, vars: vars, params: merged, body: bodyMap, returns: -1}
	defer r.close()
	out, err := rt.handler(r)
	if err != nil {
		return fail(err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return fail(err)
	}
	return b, http.StatusOK, true, nil
}

func dryRunPlan(rt *route, vars map[string]string, params map[string]string, body map[string]any) map[string]any {
	args := []any{}
	for _, seg := range rt.segs {
		if strings.HasPrefix(seg, "{") {
			args = append(args, vars[seg[1:len(seg)-1]])
		}
	}
	if len(params) > 0 {
		clean := map[string]string{}
		for k, v := range params {
			if v != "" {
				clean[k] = v
			}
		}
		if len(clean) > 0 {
			args = append(args, clean)
		}
	}
	if len(body) > 0 {
		args = append(args, body)
	}
	return map[string]any{"bridge": rt.bridge, "command": rt.command, "args": args}
}

func normalizeBody(body any) (map[string]any, error) {
	out := map[string]any{}
	if body == nil {
		return out, nil
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, badInput("request body is not JSON: %v", err)
	}
	if string(b) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, badInput("request body must be a JSON object: %v", err)
	}
	return out, nil
}

// routeReq carries one routed request and its lazily opened bridges.
type routeReq struct {
	ctx    context.Context
	c      *Client
	method string
	path   string
	vars   map[string]string
	params map[string]string
	body   map[string]any

	osc        *live.OSCConn
	trackNames []string
	sceneNames []string
	sigNum     int
	sigDen     int
	returns    int // -1 until known
}

func (r *routeReq) close() {
	if r.osc != nil {
		_ = r.osc.Close()
		r.osc = nil
	}
}

// conn opens the AbletonOSC socket once per routed request.
func (r *routeReq) conn() (*live.OSCConn, error) {
	if r.osc != nil {
		return r.osc, nil
	}
	o, err := live.DialOSC(abletonOSCLocal, abletonOSCRemote)
	if err != nil {
		return nil, err
	}
	r.osc = o
	return o, nil
}

func (r *routeReq) q(address string, echo int, args ...any) ([]any, error) {
	o, err := r.conn()
	if err != nil {
		return nil, err
	}
	return o.Query(r.ctx, address, echo, args...)
}

func (r *routeReq) qm(reqs []live.Request) ([][]any, error) {
	if len(reqs) == 0 {
		return nil, nil
	}
	o, err := r.conn()
	if err != nil {
		return nil, err
	}
	res, _, err := o.QueryMany(r.ctx, reqs, false)
	return res, err
}

func (r *routeReq) qmTolerant(reqs []live.Request) ([][]any, []error, error) {
	if len(reqs) == 0 {
		return nil, nil, nil
	}
	o, err := r.conn()
	if err != nil {
		return nil, nil, err
	}
	return o.QueryMany(r.ctx, reqs, true)
}

func (r *routeReq) send(address string, args ...any) error {
	o, err := r.conn()
	if err != nil {
		return err
	}
	return o.Send(address, args...)
}

// act fires an AbletonOSC method (which never replies) and then asks for
// Live's version: the answer proves AbletonOSC is alive, and a /live/error
// raised by the method arrives first and fails the request.
func (r *routeReq) act(address string, args ...any) error {
	if err := r.send(address, args...); err != nil {
		return err
	}
	_, err := r.q("/live/application/get/version", 0)
	return err
}

func (r *routeReq) mcp(cmd string, params map[string]any, readTimeout time.Duration) (json.RawMessage, error) {
	return live.TCPCommand(r.ctx, live.BridgeMCP, abletonMCPAddr, cmd, params, live.TCPOptions{DialTimeout: 2 * time.Second, ReadTimeout: readTimeout})
}

func (r *routeReq) ppx(cmd string, params map[string]any) (json.RawMessage, error) {
	return live.TCPCommand(r.ctx, live.BridgePPX, abletonPPXAddr, cmd, params, live.TCPOptions{DialTimeout: 1500 * time.Millisecond, ReadTimeout: 15 * time.Second, HalfClose: true})
}

// ---------- name resolution ----------

func (r *routeReq) tracks() ([]string, error) {
	if r.trackNames != nil {
		return r.trackNames, nil
	}
	vals, err := r.q("/live/song/get/track_names", 0)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(vals))
	for i, v := range vals {
		names[i], _ = live.AsString(v)
	}
	r.trackNames = names
	return names, nil
}

func (r *routeReq) resolveTrack(input string) (int, string, error) {
	names, err := r.tracks()
	if err != nil {
		return -1, "", err
	}
	i, err := live.Resolve("track", input, names)
	if err != nil {
		return -1, "", err
	}
	return i, names[i], nil
}

// trackVar resolves the {track} path variable.
func (r *routeReq) trackVar() (int, string, error) {
	return r.resolveTrack(r.vars["track"])
}

func (r *routeReq) scenes() ([]string, error) {
	if r.sceneNames != nil {
		return r.sceneNames, nil
	}
	vals, err := r.q("/live/song/get/scenes/name", 0)
	if err != nil {
		var rem *live.RemoteError
		if !errors.As(err, &rem) {
			return nil, err
		}
		// Older AbletonOSC without the bulk getter: one name per scene.
		n, err := r.numScenes()
		if err != nil {
			return nil, err
		}
		reqs := make([]live.Request, n)
		for i := range reqs {
			reqs[i] = live.Q("/live/scene/get/name", 1, i)
		}
		res, err := r.qm(reqs)
		if err != nil {
			return nil, err
		}
		vals = make([]any, n)
		for i, v := range res {
			if len(v) > 0 {
				vals[i] = v[0]
			}
		}
	}
	names := make([]string, len(vals))
	for i, v := range vals {
		names[i], _ = live.AsString(v)
	}
	r.sceneNames = names
	return names, nil
}

func (r *routeReq) numScenes() (int, error) {
	if r.sceneNames != nil {
		return len(r.sceneNames), nil
	}
	vals, err := r.q("/live/song/get/num_scenes", 0)
	if err != nil {
		return 0, err
	}
	if len(vals) == 0 {
		return 0, nil
	}
	n, _ := live.AsInt(vals[0])
	return n, nil
}

func (r *routeReq) resolveScene(input string) (int, string, error) {
	names, err := r.scenes()
	if err != nil {
		return -1, "", err
	}
	i, err := live.Resolve("scene", input, names)
	if err != nil {
		return -1, "", err
	}
	return i, names[i], nil
}

func (r *routeReq) deviceNames(track int) ([]string, error) {
	vals, err := r.q("/live/track/get/devices/name", 1, track)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(vals))
	for i, v := range vals {
		names[i], _ = live.AsString(v)
	}
	return names, nil
}

func (r *routeReq) resolveDevice(track int, input string) (int, string, error) {
	names, err := r.deviceNames(track)
	if err != nil {
		return -1, "", err
	}
	i, err := live.Resolve("device", input, names)
	if err != nil {
		return -1, "", err
	}
	return i, names[i], nil
}

func (r *routeReq) paramNames(track, device int) ([]string, error) {
	vals, err := r.q("/live/device/get/parameters/name", 2, track, device)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(vals))
	for i, v := range vals {
		names[i], _ = live.AsString(v)
	}
	return names, nil
}

func (r *routeReq) resolveParam(track, device int, input string) (int, string, error) {
	names, err := r.paramNames(track, device)
	if err != nil {
		return -1, "", err
	}
	i, err := live.Resolve("parameter", input, names)
	if err != nil {
		return -1, "", err
	}
	return i, names[i], nil
}

// slotVar parses a 1-based {slot} and checks it against the scene count.
func (r *routeReq) slotVar(key string) (int, error) {
	return r.checkSlot(r.vars[key])
}

func (r *routeReq) checkSlot(input string) (int, error) {
	s, err := live.ParseSlot("slot", input)
	if err != nil {
		return -1, badInput("%v", err)
	}
	n, err := r.numScenes()
	if err != nil {
		return -1, err
	}
	if s >= n {
		return -1, notFound("slot %d does not exist (the set has %d scene rows)", s+1, n)
	}
	return s, nil
}

func (r *routeReq) signature() (int, int, error) {
	if r.sigNum > 0 {
		return r.sigNum, r.sigDen, nil
	}
	res, err := r.qm([]live.Request{live.Q("/live/song/get/signature_numerator", 0), live.Q("/live/song/get/signature_denominator", 0)})
	if err != nil {
		return 0, 0, err
	}
	num, den := 4, 4
	if len(res[0]) > 0 {
		num, _ = live.AsInt(res[0][0])
	}
	if len(res[1]) > 0 {
		den, _ = live.AsInt(res[1][0])
	}
	if !live.ValidSignature(num, den) {
		num, den = 4, 4
	}
	r.sigNum, r.sigDen = num, den
	return num, den, nil
}

func (r *routeReq) position(input string) (float64, error) {
	num, den, err := r.signature()
	if err != nil {
		return 0, err
	}
	b, err := live.ParsePosition(input, num, den)
	if err != nil {
		return 0, badInput("%v", err)
	}
	return b, nil
}

func (r *routeReq) formatPos(beats float64) string {
	num, den, err := r.signature()
	if err != nil {
		num, den = 4, 4
	}
	return live.FormatPosition(beats, num, den)
}

// ---------- body / query helpers ----------

func (r *routeReq) bodyString(key string) (string, bool) {
	v, ok := r.body[key]
	if !ok || v == nil {
		return "", false
	}
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(x), true
	}
	b, _ := json.Marshal(v)
	return string(b), true
}

func (r *routeReq) bodyFloat(key string) (float64, bool, error) {
	v, ok := r.body[key]
	if !ok || v == nil {
		return 0, false, nil
	}
	switch x := v.(type) {
	case float64:
		return x, true, nil
	case string:
		if strings.TrimSpace(x) == "" {
			return 0, false, nil
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, true, badInput("%s must be a number, got %q", key, x)
		}
		return f, true, nil
	}
	return 0, true, badInput("%s must be a number", key)
}

func (r *routeReq) bodyInt(key string) (int, bool, error) {
	f, ok, err := r.bodyFloat(key)
	if err != nil || !ok {
		return 0, ok, err
	}
	if f != float64(int(f)) {
		return 0, true, badInput("%s must be a whole number, got %v", key, f)
	}
	return int(f), true, nil
}

func (r *routeReq) bodyBool(key string) (bool, bool, error) {
	v, ok := r.body[key]
	if !ok || v == nil {
		return false, false, nil
	}
	switch x := v.(type) {
	case bool:
		return x, true, nil
	case string:
		b, err := live.ParseBool(x)
		if err != nil {
			return false, true, badInput("%s: %v", key, err)
		}
		return b, true, nil
	case float64:
		return x != 0, true, nil
	}
	return false, true, badInput("%s must be true or false", key)
}

func (r *routeReq) requireBodyString(key string) (string, error) {
	s, ok := r.bodyString(key)
	if !ok || strings.TrimSpace(s) == "" {
		return "", badInput("%s is required", key)
	}
	return s, nil
}

func (r *routeReq) requireBodyFloat(key string) (float64, error) {
	f, ok, err := r.bodyFloat(key)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, badInput("%s is required", key)
	}
	return f, nil
}

func (r *routeReq) requireBodyBool(key string) (bool, error) {
	b, ok, err := r.bodyBool(key)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, badInput("%s is required (true or false)", key)
	}
	return b, nil
}

func (r *routeReq) paramFloat(key string) (float64, bool, error) {
	s := strings.TrimSpace(r.params[key])
	if s == "" {
		return 0, false, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, true, badInput("%s must be a number, got %q", key, s)
	}
	return f, true, nil
}

func (r *routeReq) paramBool(key string, def bool) (bool, error) {
	s := strings.TrimSpace(r.params[key])
	if s == "" {
		return def, nil
	}
	b, err := live.ParseBool(s)
	if err != nil {
		return false, badInput("%s: %v", key, err)
	}
	return b, nil
}

// ---------- results ----------

func ack(action, target, detail string) map[string]any {
	return map[string]any{"ok": true, "action": action, "target": target, "detail": detail}
}

func trackTarget(i int, name string) string {
	return fmt.Sprintf("track %d %q", i+1, name)
}

// journal records one change; failures only warn (the change in Live has
// already happened and must not be reported as failed).
func (r *routeReq) journal(target, property string, before, after any) *int64 {
	if JournalHook == nil || r.c.IsDryRun() {
		return nil
	}
	cmd := JournalCommand
	if cmd == "" {
		cmd = "ableton-pp-cli"
	}
	id, err := JournalHook(r.ctx, JournalEntry{
		Command: cmd, Method: r.method, Path: r.path, Target: target,
		Property: property, Before: before, After: after,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: the change was made in Live but could not be journaled: %v\n", err)
		return nil
	}
	if id <= 0 {
		return nil
	}
	return &id
}

// propChange builds the PropChange response and journals it.
func (r *routeReq) propChange(target, property string, before, after any, extra map[string]any) map[string]any {
	out := map[string]any{
		"target":     target,
		"property":   property,
		"before":     before,
		"after":      after,
		"journal_id": r.journal(target, property, before, after),
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func first(vals []any) any {
	if len(vals) == 0 {
		return nil
	}
	return vals[0]
}

func firstFloat(vals []any) float64 {
	f, _ := live.AsFloat(first(vals))
	return f
}

func firstInt(vals []any) int {
	i, ok := live.AsInt(first(vals))
	if !ok {
		f, _ := live.AsFloat(first(vals))
		return int(f)
	}
	return i
}

func firstBool(vals []any) bool {
	b, _ := live.AsBool(first(vals))
	return b
}

func firstString(vals []any) string {
	s, _ := live.AsString(first(vals))
	return s
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// AbletonBridgeHelp is the setup hint for a bridge (abletonosc,
// abletonmcp, abletonppextras), for hand-written commands that talk to the
// bridges directly.
func AbletonBridgeHelp(bridge string) string { return bridgeHelp(bridge) }

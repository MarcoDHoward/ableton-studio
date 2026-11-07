// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written (slice C): shared plumbing for the transcendence commands
// (notes replace/restore/backups, device add, arrangement build, mix,
// journal, snapshot save/list/diff, chain). These commands compose the
// routed endpoints that slice A built (the same virtual REST paths the
// generated commands call), so every write goes through the router's
// read-set-readback path and lands in the journal.
//
// Names returned by Live (tracks, clips, devices, parameters, scenes) are
// untrusted data: they are only ever rendered quoted (%q) or as JSON values
// and are never interpolated into anything that is executed.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"ableton-pp-cli/internal/client"
	"ableton-pp-cli/internal/store"
)

// liveAPI wraps one client for a composite command. Dry runs are handled by
// the commands themselves (they read Live to build a plan and then skip
// every write), so the client never runs in the router's own dry-run mode.
type liveAPI struct {
	c   *client.Client
	ctx context.Context
}

func newLiveAPI(cmd *cobra.Command, flags *rootFlags) (*liveAPI, error) {
	c, err := flags.newClient()
	if err != nil {
		return nil, err
	}
	// Composite commands build their own plan under --dry-run and never call
	// a write path in that mode; reads must reach Live to build the plan.
	c.DryRun = false
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return &liveAPI{c: c, ctx: ctx}, nil
}

func decodeInto(raw json.RawMessage, out any) error {
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("unexpected reply from Live: %w", err)
	}
	return nil
}

func (a *liveAPI) get(path string, params map[string]string, out any) error {
	raw, err := a.c.Get(a.ctx, path, params)
	if err != nil {
		return err
	}
	return decodeInto(raw, out)
}

func (a *liveAPI) put(path string, body any, out any) error {
	raw, _, err := a.c.Put(a.ctx, path, body)
	if err != nil {
		return err
	}
	return decodeInto(raw, out)
}

func (a *liveAPI) post(path string, body any, out any) error {
	raw, _, err := a.c.Post(a.ctx, path, body)
	if err != nil {
		return err
	}
	return decodeInto(raw, out)
}

func (a *liveAPI) del(path string, params map[string]string, out any) error {
	raw, _, err := a.c.DeleteWithParams(a.ctx, path, params)
	if err != nil {
		return err
	}
	return decodeInto(raw, out)
}

// livePath fills {name} placeholders pairwise: livePath("/tracks/{track}", "track", "Bass").
func livePath(pattern string, kv ...string) string {
	for i := 0; i+1 < len(kv); i += 2 {
		pattern = replacePathParam(pattern, kv[i], kv[i+1])
	}
	return pattern
}

// planOnly reports whether a --dry-run must fall back to the generic
// "would run" report: required arguments are missing, a Printing Press
// harness waived required input, or the client is not pointed at Live (mock
// verification), so there is nothing real to plan against.
func planOnly(flags *rootFlags, args []string, need int) bool {
	if !dryRunOK(flags) {
		return false
	}
	if len(args) < need || requiredInputWaived() {
		return true
	}
	c, err := flags.newClient()
	if err != nil {
		return true
	}
	return !c.IsAbletonVirtual()
}

// liveErr classifies a client error into the CLI's typed exit codes and
// writes the JSON error envelope when --json is set.
func liveErr(cmd *cobra.Command, flags *rootFlags, err error) error {
	if err == nil {
		return nil
	}
	return classifyAPIError(cmd.OutOrStdout(), err, flags)
}

// emit prints v as JSON (piped, --json, --agent ...) or calls human for a
// terminal.
func emit(cmd *cobra.Command, flags *rootFlags, v any, human func(w io.Writer)) error {
	w := cmd.OutOrStdout()
	if human != nil && wantsHumanTable(w, flags) {
		human(w)
		return nil
	}
	return printJSONFiltered(w, v, flags)
}

// openAbletonStore opens the local store and makes sure the Ableton tables
// exist. The ABLETON_NO_LOCAL_DATA kill switch disables these commands.
func openAbletonStore(ctx context.Context) (*store.Store, error) {
	if localDataDisabled() {
		return nil, configErr(fmt.Errorf("this command keeps its data in the local store, which %s=1 disables", noLocalDataEnvVar))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s, err := store.OpenWithContext(ctx, defaultDBPath("ableton-pp-cli"))
	if err != nil {
		return nil, configErr(fmt.Errorf("opening the local store: %w", err))
	}
	if err := store.EnsureAbletonTables(ctx, s.DB()); err != nil {
		_ = s.Close()
		return nil, configErr(err)
	}
	return s, nil
}

// ---------- Live data shapes (as returned by the routed endpoints) ----------

type liveTrack struct {
	Number   int                `json:"number"`
	Name     string             `json:"name"`
	Type     string             `json:"type"`
	Volume   *float64           `json:"volume"`
	VolumeDB *float64           `json:"volume_db"`
	Pan      *float64           `json:"pan"`
	Mute     bool               `json:"mute"`
	Solo     bool               `json:"solo"`
	Arm      *bool              `json:"arm"`
	Sends    map[string]float64 `json:"sends"`
}

type liveDevice struct {
	Number    int    `json:"number"`
	Name      string `json:"name"`
	ClassName string `json:"class_name"`
	Type      string `json:"type"`
	On        *bool  `json:"on"`
}

type liveParam struct {
	Number    int     `json:"number"`
	Name      string  `json:"name"`
	Value     float64 `json:"value"`
	Display   *string `json:"display"`
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
	Quantized bool    `json:"quantized"`
}

// propChange is the router's response to every journaled setter.
type propChange struct {
	Target    string `json:"target"`
	Property  string `json:"property"`
	Before    any    `json:"before"`
	After     any    `json:"after"`
	JournalID *int64 `json:"journal_id"`
}

func (a *liveAPI) tracks() ([]liveTrack, error) {
	var rows []liveTrack
	if err := a.get("/tracks", nil, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (a *liveAPI) devices(track string) ([]liveDevice, error) {
	var rows []liveDevice
	if err := a.get(livePath("/tracks/{track}/devices", "track", track), nil, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (a *liveAPI) params(track, device string) ([]liveParam, error) {
	var rows []liveParam
	if err := a.get(livePath("/tracks/{track}/devices/{device}/params", "track", track, "device", device), nil, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func trackNames(rows []liveTrack) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}

// nameKeys gives every entry a stable, unique key for name-based matching:
// the name itself for its first occurrence, "name #2", "name #3" ... for
// repeats. A generated key never collides with a literal name in the list
// (a track really called "Bass #2"): the counter skips taken keys.
func nameKeys(names []string) []string {
	taken := map[string]bool{}
	for _, n := range names {
		taken[n] = true
	}
	seen := map[string]int{}
	used := map[string]bool{}
	out := make([]string, len(names))
	for i, n := range names {
		seen[n]++
		if seen[n] == 1 && !used[n] {
			out[i] = n
			used[n] = true
			continue
		}
		k := seen[n]
		if k < 2 {
			k = 2
		}
		key := fmt.Sprintf("%s #%d", n, k)
		for taken[key] || used[key] {
			k++
			key = fmt.Sprintf("%s #%d", n, k)
		}
		seen[n] = k
		out[i] = key
		used[key] = true
	}
	return out
}

func itoa(i int) string { return strconv.Itoa(i) }

// beatsArg renders a beat position for the router's position parser: an
// explicit "b" suffix so 66.5 means beats, never bar 66 beat 5.
func beatsArg(beats float64) string {
	return strconv.FormatFloat(beats, 'f', -1, 64) + "b"
}

func nearlyEqual(a, b float64) bool {
	return math.Abs(a-b) <= 1e-4*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

// jsonEqual compares two JSON values, allowing float32 rounding on numbers.
func jsonEqual(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && nearlyEqual(x, y)
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if !jsonEqual(v, y[k]) {
				return false
			}
		}
		return true
	}
	return false
}

// shortValue renders a JSON value for human output.
func shortValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return strconv.Quote(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func joinQuoted(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = strconv.Quote(n)
	}
	return strings.Join(parts, ", ")
}

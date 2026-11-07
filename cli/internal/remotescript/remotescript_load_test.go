// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package remotescript

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stubControlSurface mirrors the parts of Live's _Framework.ControlSurface the
// script touches. _tasks is a read-only property there, so assigning
// self._tasks in a subclass fails at load time (seen in Live 12.4.16).
const stubControlSurface = `
class _Song(object):
    tracks = []

class ControlSurface(object):
    def __init__(self, c_instance):
        self._c_instance = c_instance
        self.messages = []
    @property
    def _tasks(self):
        return None
    def song(self):
        return _Song()
    def show_message(self, text):
        self.messages.append(text)
    def log_message(self, text):
        pass
    def update_display(self):
        pass
    def disconnect(self):
        pass
`

// loadScript constructs the script the way Live does, serves one request over
// its socket while pumping update_display, then disconnects.
const loadScript = `
import json, socket, sys, threading
sys.path.insert(0, sys.argv[1])
import AbletonPPExtras as m
m.PORT = 0  # never collide with a running Live on 9878
s = m.create_instance(object())
assert s._pp_server is not None, s.messages
port = s._pp_server._sock.getsockname()[1]
out = {}
def ask():
    c = socket.create_connection(("127.0.0.1", port), timeout=5)
    c.sendall(json.dumps({"type": "version", "params": {}}).encode() + b"\n")
    out["reply"] = c.makefile().readline()
    c.close()
t = threading.Thread(target=ask); t.start()
while t.is_alive():
    s.update_display()
    t.join(0.05)
s.disconnect()
print(out["reply"].strip())
`

// automationScript drives automation_set through dispatch against fake Live
// objects and prints each case's reply plus the steps written to the envelope.
const automationScript = `
import json, sys
sys.path.insert(0, sys.argv[1])
import AbletonPPExtras as m

class Env(object):
    def __init__(self): self.steps = []
    def insert_step(self, start, length, value): self.steps.append([start, length, value])
class Clip(object):
    length = 16.0
    def __init__(self): self.env = None
    def automation_envelope(self, p): return self.env
    def create_automation_envelope(self, p):
        self.env = Env(); return self.env
class Param(object):
    name, min, max = "Macro 1", 0.0, 127.0
class Device(object):
    name, parameters = "Rack", [Param(), Param()]
class Slot(object):
    has_clip = True
    def __init__(self): self.clip = Clip()
class Track(object):
    name = "T"
    def __init__(self): self.clip_slots, self.devices = [Slot()], [Device()]
class Song(object):
    def __init__(self): self.tracks = [Track()]

cases = {
    "held": [{"time": 0, "value": 20}, {"time": 8, "value": 100}],
    "mixed": [{"time": 4, "value": 10, "duration": 2}, {"time": 0, "value": 5}],
    "zero": [{"time": 0, "value": 20, "duration": 0}],
    "at_end": [{"time": 16, "value": 20}],
}
out = {}
for name, points in cases.items():
    song = Song()
    raw = json.dumps({"type": "automation_set", "params": {
        "track": 0, "slot": 0, "device": 0, "param": 1, "points": points}}).encode()
    _, reply = m.dispatch(raw, lambda fn: fn(), m.Commands(lambda: "test"), lambda: song)
    env = song.tracks[0].clip_slots[0].clip.env
    out[name] = {"status": reply["status"], "steps": env.steps if env else None}
print(json.dumps(out, sort_keys=True))
`

func TestAutomationSetHoldsUntilNextPoint(t *testing.T) {
	got := runScriptPython(t, automationScript)
	want := `{"at_end": {"status": "error", "steps": null}, ` +
		`"held": {"status": "success", "steps": [[0.0, 8.0, 20.0], [8.0, 8.0, 100.0]]}, ` +
		`"mixed": {"status": "success", "steps": [[0.0, 4.0, 5.0], [4.0, 2.0, 10.0]]}, ` +
		`"zero": {"status": "error", "steps": null}}`
	if strings.TrimSpace(got) != want {
		t.Fatalf("automation_set steps:\n got %s\nwant %s", strings.TrimSpace(got), want)
	}
}

func TestScriptLoadsUnderControlSurface(t *testing.T) {
	reply := runScriptPython(t, loadScript)
	if !strings.Contains(reply, `"status":"success"`) || !strings.Contains(reply, `"script_version":"`+ScriptVersion+`"`) {
		t.Fatalf("unexpected version reply: %s", reply)
	}
}

// runScriptPython writes the embedded script, a stub _Framework and driver
// into a temp dir, runs the driver with that dir as argv[1], and returns stdout.
func runScriptPython(t *testing.T, driver string) string {
	t.Helper()
	py, err := exec.LookPath("python")
	if err != nil {
		if py, err = exec.LookPath("python3"); err != nil {
			t.Skip("python not on PATH")
		}
	}
	files, err := ScriptFiles()
	if err != nil {
		t.Fatalf("ScriptFiles: %v", err)
	}
	dir := t.TempDir()
	write := func(rel string, body []byte) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range files {
		write(filepath.Join("AbletonPPExtras", name), body)
	}
	write(filepath.Join("_Framework", "__init__.py"), nil)
	write(filepath.Join("_Framework", "ControlSurface.py"), []byte(stubControlSurface))
	write("driver.py", []byte(driver))

	cmd := exec.Command(py, "-B", filepath.Join(dir, "driver.py"), dir)
	cmd.Dir = dir
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script driver failed: %v\n%s", err, got)
	}
	return string(got)
}

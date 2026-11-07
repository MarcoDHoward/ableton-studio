// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package remotescript

import (
	"strings"
	"testing"
)

func TestEmbeddedScriptIsLockedDown(t *testing.T) {
	files, err := ScriptFiles()
	if err != nil {
		t.Fatalf("ScriptFiles: %v", err)
	}
	src, ok := files["__init__.py"]
	if !ok {
		t.Fatalf("embedded __init__.py missing; got %d files", len(files))
	}
	s := string(src)

	mustContain := []string{
		`SCRIPT_VERSION = "` + ScriptVersion + `"`,
		`HOST = "127.0.0.1"`,
		`PORT = 9878`,
		`MAX_REQUEST_BYTES = 1024 * 1024`,
		`def create_instance(c_instance):`,
		`from _Framework.ControlSurface import ControlSurface`,
	}
	for _, want := range mustContain {
		if !strings.Contains(s, want) {
			t.Errorf("script should contain %q", want)
		}
	}

	forbidden := []string{
		"eval(", "exec(", "compile(", "open(", "__import__",
		"subprocess", "urllib", "requests", "http.client",
		"0.0.0.0", "add_value_listener", "_listener(",
		"os.system", "import os", "pickle", "marshal",
	}
	for _, bad := range forbidden {
		if strings.Contains(s, bad) {
			t.Errorf("script must not contain %q", bad)
		}
	}
}

func TestEmbeddedScriptImportsOnlyAllowlist(t *testing.T) {
	files, err := ScriptFiles()
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"import json": true, "import math": true, "import queue": true,
		"import socket": true, "import threading": true, "import time": true,
		"from _Framework.ControlSurface import ControlSurface":           true,
		"import Live  # Live's own API module, only present inside Live": true,
	}
	for _, line := range strings.Split(string(files["__init__.py"]), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ") {
			if !allowed[trimmed] {
				t.Errorf("unexpected import line: %q", trimmed)
			}
		}
	}
}

func TestEmbeddedScriptCommandAllowlist(t *testing.T) {
	files, err := ScriptFiles()
	if err != nil {
		t.Fatal(err)
	}
	s := string(files["__init__.py"])
	start := strings.Index(s, "COMMANDS = (")
	if start < 0 {
		t.Fatal("COMMANDS tuple not found")
	}
	end := strings.Index(s[start:], "\n)")
	if end < 0 {
		t.Fatal("COMMANDS tuple not closed")
	}
	var got []string
	for _, line := range strings.Split(s[start:start+end], "\n")[1:] {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
		if line != "" {
			got = append(got, strings.Trim(line, `"`))
		}
	}
	want := []string{
		"ping", "version", "delete_device", "rack_chains", "drum_pads",
		"arrangement_add_clip", "arrangement_delete_clip",
		"automation_get", "automation_set", "automation_clear",
		"mixer_db", "set_volume_db",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("COMMANDS = %v, want %v (the contract in ppextras-protocol.md)", got, want)
	}
	// The spike's security rules: the script never offers AbletonMCP's
	// passive-event drain or whole-session snapshot, and never dials out.
	for _, bad := range []string{"drain_passive_events", "get_session_snapshot", ".connect(", "create_connection"} {
		if strings.Contains(s, bad) {
			t.Errorf("script must not contain %q", bad)
		}
	}
	if n := strings.Count(s, ".bind("); n != 1 {
		t.Errorf("script should bind exactly one socket, found %d bind calls", n)
	}
}

func TestEmbeddedScriptIsASCII(t *testing.T) {
	files, err := ScriptFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		t.Errorf("embedded folder should hold only __init__.py, got %v", names)
	}
	for name, data := range files {
		for i, b := range data {
			if b > 0x7f {
				t.Fatalf("%s: non-ASCII byte 0x%02x at offset %d (Live's Python may not decode it)", name, b, i)
			}
		}
	}
}

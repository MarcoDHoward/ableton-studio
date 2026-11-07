// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Package remotescript embeds AbletonPPExtras, the small Ableton Live MIDI
// Remote Script that ableton-pp-cli ships for capabilities Live's other
// control surfaces do not expose (rack chains, drum pads, arrangement clips,
// clip automation, dB-accurate volume). The script is written from scratch,
// binds 127.0.0.1:9878 only, and accepts a fixed command allowlist.
package remotescript

import (
	"embed"
	"io/fs"
)

// ScriptVersion is the version string embedded in AbletonPPExtras/__init__.py
// (SCRIPT_VERSION). Bump both together.
const ScriptVersion = "1.0.2"

// FolderName is the Remote Scripts subfolder Live loads the script from.
const FolderName = "AbletonPPExtras"

// Files holds the script folder (AbletonPPExtras/*).
//
//go:embed AbletonPPExtras/*
var Files embed.FS

// ScriptFiles returns the embedded script files as name -> contents, keyed by
// their base name inside the AbletonPPExtras folder.
func ScriptFiles() (map[string][]byte, error) {
	entries, err := fs.ReadDir(Files, FolderName)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := fs.ReadFile(Files, FolderName+"/"+e.Name())
		if err != nil {
			return nil, err
		}
		out[e.Name()] = data
	}
	return out, nil
}

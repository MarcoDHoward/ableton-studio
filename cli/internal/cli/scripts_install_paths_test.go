// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUserLibraryFromCfgUnescapesAndRefusesNetworkPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows Library.cfg paths")
	}
	cfg := func(v string) []byte {
		return []byte(`<Ableton><UserLibrary><LibraryProject><ProjectPath Value="` + v + `" /></LibraryProject></UserLibrary></Ableton>`)
	}
	if got := userLibraryFromCfg(cfg(`F:\Drum &amp; Bass`)); got != `F:\Drum & Bass\User Library` {
		t.Errorf("entity not unescaped: %q", got)
	}
	for _, bad := range []string{`\\server\share`, `\??\UNC\server\share`, `relative\dir`, `\\?\C:\x`} {
		if got := userLibraryFromCfg(cfg(bad)); got != "" {
			t.Errorf("userLibraryFromCfg(%q) = %q, want refusal", bad, got)
		}
	}
}

func TestBackupPathNeverOverwritesADifferentBackup(t *testing.T) {
	dir := t.TempDir()
	if got := backupPathFor(dir, []byte("v1")); filepath.Base(got) != "__init__.py.bak" {
		t.Fatalf("first backup: %s", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "__init__.py.bak"), []byte("user edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := backupPathFor(dir, []byte("v2")); !strings.HasPrefix(filepath.Base(got), "__init__.py.bak-") {
		t.Fatalf("an existing different .bak must be kept: %s", got)
	}
	if got := backupPathFor(dir, []byte("user edit")); filepath.Base(got) != "__init__.py.bak" {
		t.Fatalf("identical content may reuse .bak: %s", got)
	}
}

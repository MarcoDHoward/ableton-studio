// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ableton-pp-cli/internal/cliutil/testenv"
	"ableton-pp-cli/internal/remotescript"
)

func TestUserLibraryFromCfg(t *testing.T) {
	cfg := []byte(`<Ableton><ContentLibrary><UserLibrary><LibraryProject Id="1">
<ProjectName Value="User Library" /><ProjectPath Value="F:/Ableton" /></LibraryProject></UserLibrary>
<SliceInfoList><LibrarySliceInfo Path="F:\Other" /></SliceInfoList></ContentLibrary></Ableton>`)
	got := userLibraryFromCfg(cfg)
	want := filepath.Join("F:"+string(filepath.Separator)+"Ableton", "User Library")
	if got != want {
		t.Fatalf("userLibraryFromCfg = %q, want %q", got, want)
	}
	if userLibraryFromCfg([]byte(`<ProjectPath Value="X" />`)) != "" {
		t.Fatal("ProjectPath outside <UserLibrary> must be ignored")
	}
}

func TestLocateUserLibraryPrefersNewestPlainLiveFolder(t *testing.T) {
	appData := t.TempDir()
	write := func(dir, root string) {
		p := filepath.Join(appData, "Ableton", dir, "Preferences")
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		body := `<UserLibrary><LibraryProject><ProjectPath Value="` + root + `" /></LibraryProject></UserLibrary>`
		if err := os.WriteFile(filepath.Join(p, "Library.cfg"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Live 12.4.7", "C:/old")
	write("Live 12.4.16", "C:/new")
	write("Live 12.4.16 - Copy", "C:/copy")
	write("Live 10.1.43", "C:/ancient")
	got, src := locateUserLibrary(appData, "")
	if !strings.Contains(got, "new") || !strings.Contains(src, "Live 12.4.16"+string(filepath.Separator)) {
		t.Fatalf("locateUserLibrary = %q (%s), want the Live 12.4.16 library", got, src)
	}
	got, src = locateUserLibrary(t.TempDir(), `C:\Users\someone`)
	if src != "default" || !strings.HasSuffix(got, filepath.Join("Ableton", "User Library")) {
		t.Fatalf("fallback = %q (%s)", got, src)
	}
}

func runScriptsCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := RootCmd()
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	return out.String(), err
}

func TestScriptsInstallPlanThenYesWithBackup(t *testing.T) {
	testenv.Isolate(t)
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	lib := t.TempDir()
	dest := filepath.Join(lib, "Remote Scripts", "AbletonPPExtras")

	// Default: plan only, nothing written.
	out, err := runScriptsCmd(t, "scripts", "install", "--target", lib, "--json")
	if err != nil {
		t.Fatalf("plan: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("plan mode must not write; stat err = %v", statErr)
	}

	// Pre-existing different script -> backed up.
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	old := []byte("# old script\n")
	if err := os.WriteFile(filepath.Join(dest, "__init__.py"), old, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runScriptsCmd(t, "scripts", "install", "--target", lib, "--yes", "--json")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	var plan scriptInstallPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if !plan.Written || !plan.BackedUp || plan.SourceVersion != remotescript.ScriptVersion {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	bak, _ := os.ReadFile(filepath.Join(dest, "__init__.py.bak"))
	if !bytes.Equal(bak, old) {
		t.Fatalf("backup content = %q", bak)
	}
	files, _ := remotescript.ScriptFiles()
	got, _ := os.ReadFile(filepath.Join(dest, "__init__.py"))
	if !bytes.Equal(got, files["__init__.py"]) {
		t.Fatal("installed script does not match embedded copy")
	}

	out, err = runScriptsCmd(t, "scripts", "status", "--target", lib, "--json")
	var status scriptInstallPlan
	if err != nil || json.Unmarshal([]byte(out), &status) != nil || !status.UpToDate || !status.Installed {
		t.Fatalf("status after install: %v\n%s", err, out)
	}
}

func TestScriptsInstallNeverWritesUnderHarness(t *testing.T) {
	testenv.Isolate(t)
	t.Setenv("PRINTING_PRESS_VERIFY", "1")
	lib := t.TempDir()
	out, err := runScriptsCmd(t, "scripts", "install", "--target", lib, "--yes", "--json")
	if err != nil {
		t.Fatalf("harness install: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(lib, "Remote Scripts")); !os.IsNotExist(statErr) {
		t.Fatalf("harness run wrote files: %v", statErr)
	}
}

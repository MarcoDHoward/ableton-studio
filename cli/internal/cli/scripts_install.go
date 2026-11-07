// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source computed

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"ableton-pp-cli/internal/cliutil"
	"ableton-pp-cli/internal/remotescript"

	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newScriptsCmd(flags))
		var scripts *cobra.Command
		for _, c := range root.Commands() {
			if c.Name() == "scripts" {
				scripts = c
				break
			}
		}
		if scripts == nil {
			return
		}
		addNovelCommandIfAbsent(scripts, newScriptsInstallCmd(flags))
		addNovelCommandIfAbsent(scripts, newScriptsStatusCmd(flags))
	})
}

var scriptsAnnotations = map[string]string{
	"mcp:hidden":     "true",
	"mcp:read-only":  "false",
	"pp:data-source": "computed",
}

func newScriptsCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "scripts",
		Short: "Install or check the AbletonPPExtras Live Remote Script",
		Long: "AbletonPPExtras is a small Remote Script written for this CLI. It listens on\n" +
			"127.0.0.1:9878 only and accepts a fixed list of commands (rack chains, drum\n" +
			"pads, arrangement clips, clip automation, dB volume).",
		Example:     "  ableton-pp-cli scripts install\n  ableton-pp-cli scripts install --yes\n  ableton-pp-cli scripts status --json",
		Annotations: map[string]string{"mcp:hidden": "true", "pp:data-source": "computed", "mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}
}

// scriptInstallPlan describes what `scripts install` will do or did.
type scriptInstallPlan struct {
	Script            string   `json:"script"`
	SourceVersion     string   `json:"source_version"`
	UserLibrary       string   `json:"user_library"`
	UserLibrarySource string   `json:"user_library_source"`
	UserLibraryExists bool     `json:"user_library_exists"`
	Destination       string   `json:"destination"`
	Installed         bool     `json:"installed"`
	InstalledVersion  string   `json:"installed_version,omitempty"`
	UpToDate          bool     `json:"up_to_date"`
	WouldOverwrite    bool     `json:"would_overwrite"`
	BackupPath        string   `json:"backup_path,omitempty"`
	Files             []string `json:"files"`
	Written           bool     `json:"written"`
	BackedUp          bool     `json:"backed_up,omitempty"`
	Note              string   `json:"note,omitempty"`
	NextSteps         []string `json:"next_steps,omitempty"`
}

var scriptNextSteps = []string{
	"Restart Ableton Live (or, if Live is open, re-select the script in the Control Surface list).",
	"Open Settings > Link, Tempo & MIDI. In a free Control Surface row choose AbletonPPExtras, and set its Input and Output to None.",
	"Leave the MiniLab_3 row exactly as it is.",
	"Live's status bar should show: AbletonPPExtras: listening on 127.0.0.1:9878",
}

func newScriptsInstallCmd(flags *rootFlags) *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Copy the AbletonPPExtras Remote Script into your Ableton User Library",
		Long: "Shows the install plan by default and writes nothing. Pass --yes to copy\n" +
			"AbletonPPExtras into <User Library>/Remote Scripts/AbletonPPExtras. An existing\n" +
			"__init__.py that differs is saved as __init__.py.bak first.\n\n" +
			"The User Library is read from the newest Live Preferences/Library.cfg under\n" +
			"%APPDATA%\\Ableton, falling back to Documents\\Ableton\\User Library.",
		Example:     "  ableton-pp-cli scripts install\n  ableton-pp-cli scripts install --yes\n  ableton-pp-cli scripts install --target \"D:\\Music\\User Library\" --yes",
		Annotations: scriptsAnnotations,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "scripts install")
			}
			plan, err := buildScriptInstallPlan(target)
			if err != nil {
				return err
			}
			if !flags.yes {
				plan.Note = "plan only; nothing written. Re-run with --yes to install."
				return writeScriptPlan(cmd.OutOrStdout(), flags, plan)
			}
			if cliutil.IsAnyHarness() {
				plan.Note = "running under a Printing Press harness; nothing written."
				return writeScriptPlan(cmd.OutOrStdout(), flags, plan)
			}
			if !plan.UserLibraryExists {
				return configErr(fmt.Errorf("user library %q does not exist; pass --target <User Library folder>", plan.UserLibrary))
			}
			if err := applyScriptInstall(plan); err != nil {
				return err
			}
			plan.NextSteps = scriptNextSteps
			return writeScriptPlan(cmd.OutOrStdout(), flags, plan)
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "User Library folder to install into (overrides Library.cfg discovery)")
	return cmd
}

func newScriptsStatusCmd(flags *rootFlags) *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:         "status",
		Short:       "Report whether the installed AbletonPPExtras matches the version bundled with this CLI",
		Example:     "  ableton-pp-cli scripts status\n  ableton-pp-cli scripts status --json",
		Annotations: map[string]string{"mcp:hidden": "true", "pp:data-source": "computed", "mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, err := buildScriptInstallPlan(target)
			if err != nil {
				return err
			}
			switch {
			case !plan.Installed:
				plan.Note = "not installed; run `ableton-pp-cli scripts install --yes`."
			case plan.UpToDate:
				plan.Note = "installed copy matches the bundled version."
			default:
				plan.Note = "installed copy differs from the bundled version; run `ableton-pp-cli scripts install --yes` to update."
			}
			return writeScriptPlan(cmd.OutOrStdout(), flags, plan)
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "User Library folder to check (overrides Library.cfg discovery)")
	return cmd
}

func buildScriptInstallPlan(target string) (*scriptInstallPlan, error) {
	files, err := remotescript.ScriptFiles()
	if err != nil {
		return nil, fmt.Errorf("reading embedded script: %w", err)
	}
	lib, source := "", ""
	if strings.TrimSpace(target) != "" {
		lib, source = filepath.Clean(target), "--target"
	} else {
		lib, source = locateUserLibrary(os.Getenv("APPDATA"), os.Getenv("USERPROFILE"))
	}
	if lib == "" {
		return nil, configErr(errors.New("could not determine the Ableton User Library; pass --target <User Library folder>"))
	}
	dest := filepath.Join(lib, "Remote Scripts", remotescript.FolderName)
	plan := &scriptInstallPlan{
		Script:            remotescript.FolderName,
		SourceVersion:     remotescript.ScriptVersion,
		UserLibrary:       lib,
		UserLibrarySource: source,
		UserLibraryExists: isDir(lib),
		Destination:       dest,
		UpToDate:          true,
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	plan.Files = names
	for _, name := range names {
		existing, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil {
			plan.UpToDate = false
			continue
		}
		if name == "__init__.py" {
			plan.Installed = true
			plan.InstalledVersion = scriptVersionOf(existing)
		}
		if !bytes.Equal(existing, files[name]) {
			plan.UpToDate = false
			plan.WouldOverwrite = true
			if name == "__init__.py" {
				plan.BackupPath = backupPathFor(dest, existing)
			}
		}
	}
	return plan, nil
}

func applyScriptInstall(plan *scriptInstallPlan) error {
	files, err := remotescript.ScriptFiles()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(plan.Destination, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", plan.Destination, err)
	}
	// Never write through a symlink or junction planted in the script folder.
	for _, p := range append([]string{plan.Destination, plan.BackupPath}, joinAll(plan.Destination, plan.Files)...) {
		if p == "" {
			continue
		}
		if st, err := os.Lstat(p); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through the link %s", p)
		}
	}
	if plan.BackupPath != "" {
		existing, err := os.ReadFile(filepath.Join(plan.Destination, "__init__.py"))
		if err != nil {
			return fmt.Errorf("reading existing script for backup: %w", err)
		}
		if err := os.WriteFile(plan.BackupPath, existing, 0o644); err != nil {
			return fmt.Errorf("writing backup %s: %w", plan.BackupPath, err)
		}
		plan.BackedUp = true
	}
	for _, name := range plan.Files {
		if err := os.WriteFile(filepath.Join(plan.Destination, name), files[name], 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}
	plan.Written = true
	plan.Installed = true
	plan.InstalledVersion = remotescript.ScriptVersion
	plan.UpToDate = true
	plan.Note = "installed."
	return nil
}

var (
	userLibraryBlockRe = regexp.MustCompile(`(?s)<UserLibrary>(.*?)</UserLibrary>`)
	projectPathRe      = regexp.MustCompile(`<ProjectPath\s+Value="([^"]*)"`)
	liveDirVersionRe   = regexp.MustCompile(`^Live (\d+(?:\.\d+)*)$`)
	scriptVersionRe    = regexp.MustCompile(`SCRIPT_VERSION = "([^"]+)"`)
)

// locateUserLibrary returns the User Library folder and where it came from.
// It prefers the highest-versioned "Live X.Y.Z" preferences folder (folders
// with extra suffixes such as "- Copy" are only used if no plain one has a
// readable Library.cfg), then falls back to Documents\Ableton\User Library.
func locateUserLibrary(appData, userProfile string) (string, string) {
	if appData != "" {
		for _, cfg := range libraryCfgCandidates(filepath.Join(appData, "Ableton")) {
			data, err := os.ReadFile(cfg)
			if err != nil {
				continue
			}
			if p := userLibraryFromCfg(data); p != "" {
				return p, "Library.cfg " + cfg
			}
		}
	}
	if userProfile != "" {
		return filepath.Join(userProfile, "Documents", "Ableton", "User Library"), "default"
	}
	return "", ""
}

func libraryCfgCandidates(abletonDir string) []string {
	matches, _ := filepath.Glob(filepath.Join(abletonDir, "Live *", "Preferences", "Library.cfg"))
	type cand struct {
		path    string
		plain   bool
		version []int
		mtime   int64
	}
	cands := make([]cand, 0, len(matches))
	for _, m := range matches {
		c := cand{path: m}
		dir := filepath.Base(filepath.Dir(filepath.Dir(m)))
		if sm := liveDirVersionRe.FindStringSubmatch(dir); sm != nil {
			c.plain = true
			for _, part := range strings.Split(sm[1], ".") {
				n, _ := strconv.Atoi(part)
				c.version = append(c.version, n)
			}
		}
		if st, err := os.Stat(m); err == nil {
			c.mtime = st.ModTime().UnixNano()
		}
		cands = append(cands, c)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.plain != b.plain {
			return a.plain
		}
		if cmp := compareVersions(a.version, b.version); cmp != 0 {
			return cmp > 0
		}
		return a.mtime > b.mtime
	})
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.path
	}
	return out
}

func compareVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

// userLibraryFromCfg extracts <UserLibrary>...<ProjectPath Value="X"> and
// appends "User Library" (Live stores the folder that contains it).
func userLibraryFromCfg(data []byte) string {
	block := userLibraryBlockRe.FindSubmatch(data)
	if block == nil {
		return ""
	}
	pp := projectPathRe.FindSubmatch(block[1])
	if pp == nil {
		return ""
	}
	root := strings.TrimSpace(string(pp[1]))
	if root == "" {
		return ""
	}
	root = html.UnescapeString(root) // Library.cfg is XML: "Drum &amp; Bass"
	root = strings.ReplaceAll(root, "/", string(filepath.Separator))
	root = strings.ReplaceAll(root, "\\", string(filepath.Separator))
	if !localAbsDir(root) {
		return "" // relative, UNC or device paths are not trusted; fall back
	}
	return filepath.Join(root, "User Library")
}

// localAbsDir reports whether p is an absolute path on a local drive
// ("X:\..." on Windows, "/..." elsewhere), never a UNC or \\?\ path.
func localAbsDir(p string) bool {
	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(p)
		if len(vol) != 2 || vol[1] != ':' {
			return false
		}
		c := vol[0] | 0x20
		return c >= 'a' && c <= 'z' && len(p) > 2 && (p[2] == '\\' || p[2] == '/')
	}
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//")
}

// backupPathFor names the backup of the current __init__.py. The first
// backup is __init__.py.bak; an existing, different .bak is never
// overwritten, so later backups get a timestamp.
func backupPathFor(dest string, current []byte) string {
	bak := filepath.Join(dest, "__init__.py.bak")
	prev, err := os.ReadFile(bak)
	if err != nil || bytes.Equal(prev, current) {
		return bak
	}
	return filepath.Join(dest, "__init__.py.bak-"+time.Now().Format("20060102-150405"))
}

func joinAll(dir string, names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(dir, n))
	}
	return out
}

func scriptVersionOf(src []byte) string {
	if m := scriptVersionRe.FindSubmatch(src); m != nil {
		return string(m[1])
	}
	return ""
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func writeScriptPlan(w io.Writer, flags *rootFlags, plan *scriptInstallPlan) error {
	if !wantsHumanTable(w, flags) {
		return printJSONFiltered(w, plan, flags)
	}
	yesNo := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	fmt.Fprintf(w, "Script:            %s %s (bundled)\n", plan.Script, plan.SourceVersion)
	fmt.Fprintf(w, "User Library:      %s (from %s)\n", plan.UserLibrary, plan.UserLibrarySource)
	if !plan.UserLibraryExists {
		fmt.Fprintf(w, "                   warning: this folder does not exist\n")
	}
	fmt.Fprintf(w, "Destination:       %s\n", plan.Destination)
	installed := yesNo(plan.Installed)
	if plan.Installed && plan.InstalledVersion != "" {
		installed += " (version " + plan.InstalledVersion + ")"
	}
	fmt.Fprintf(w, "Installed:         %s\n", installed)
	fmt.Fprintf(w, "Up to date:        %s\n", yesNo(plan.UpToDate))
	if !plan.Written {
		fmt.Fprintf(w, "Would overwrite:   %s\n", yesNo(plan.WouldOverwrite))
	}
	if plan.BackupPath != "" {
		fmt.Fprintf(w, "Backup:            %s\n", plan.BackupPath)
	}
	fmt.Fprintf(w, "Files:             %s\n", strings.Join(plan.Files, ", "))
	if plan.Note != "" {
		fmt.Fprintf(w, "\n%s\n", plan.Note)
	}
	if len(plan.NextSteps) > 0 {
		fmt.Fprintln(w, "\nNext steps:")
		for i, s := range plan.NextSteps {
			fmt.Fprintf(w, "  %d. %s\n", i+1, s)
		}
	}
	return nil
}

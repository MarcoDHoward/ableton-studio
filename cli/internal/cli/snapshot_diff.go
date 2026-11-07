// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source local
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

// Implemented in slice C; the logic lives in ableton_snapshot.go.
func newNovelSnapshotDiffCmd(flags *rootFlags) *cobra.Command {
	var opts snapshotDiffOpts

	cmd := &cobra.Command{
		Use:   "diff <snapshot> [other-snapshot]",
		Short: "Compare two saved project snapshots, or a snapshot against the live set: tracks, mixer, devices, parameters",
		Long: "Use this command to see every difference between two points in time, including manual edits. Do NOT use it to list or revert changes this CLI made; use 'journal list' / 'journal revert' instead.\n\n" +
			"Snapshots are stored with 'snapshot save <label>' and named by label (or id).\n" +
			"Give two labels to compare them, or one label and --live to compare it with the\n" +
			"set as it is now (read through AbletonMCP and not stored). The diff covers song\n" +
			"settings, tracks (added, removed, moved), mixer values and sends, devices,\n" +
			"device parameters, Session and Arrangement clips, note counts (and note edits\n" +
			"that keep the count), scenes, return tracks, the master track and locators.\n" +
			"Objects are matched by name, so a renamed track shows as removed plus added.",
		Example: "  ableton-pp-cli snapshot save before-edit\n" +
			"  ableton-pp-cli snapshot diff before-edit --live --agent\n" +
			"  ableton-pp-cli snapshot diff verse-v1 verse-v2 --json",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshotDiff(cmd, flags, args, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.live, "live", false, "Compare the snapshot with the set as it is now (reads Live; nothing is stored)")
	return cmd
}

// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source local
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

const journalLong = "Use this command to list or roll back specific changes made through this CLI. Do NOT use it to undo Live's whole edit history (including mouse edits); use 'undo' instead. Do NOT use it to compare whole-set states; use 'snapshot diff' instead."

// Implemented in slice C; the logic lives in ableton_journal.go.
func newNovelJournalListCmd(flags *rootFlags) *cobra.Command {
	var opts journalListOpts

	cmd := &cobra.Command{
		Use:   "list",
		Short: "See every change this CLI made, with before and after values; roll back specific ones with journal revert.",
		Long: journalLong + "\n\n" +
			"Every setter this CLI runs (mute, solo, volume, pan, sends, tempo and other song\n" +
			"properties, device parameters, clip and scene properties, renames, routing)\n" +
			"records the value before and after the change. Rows are listed newest first.\n" +
			"Roll one back with 'journal revert <id>'. Reads only the local store.",
		Example: "  ableton-pp-cli journal list --since 1h --agent\n" +
			"  ableton-pp-cli journal list --command \"mix recall\" --limit 20\n" +
			"  ableton-pp-cli journal list --target Bass --json",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJournalList(cmd, flags, args, opts)
		},
	}
	cmd.Flags().StringVar(&opts.since, "since", "", "Only changes newer than this: a duration (30m, 2h, 7d) or a date/time (2026-10-31 or RFC 3339)")
	cmd.Flags().StringVar(&opts.command, "command", "", "Only changes made by commands whose path contains this text, e.g. \"track mute\" or \"mix recall\"")
	cmd.Flags().StringVar(&opts.target, "target", "", "Only changes whose target contains this text, e.g. a track name")
	cmd.Flags().IntVar(&opts.limit, "limit", 50, "Maximum number of rows")
	return cmd
}

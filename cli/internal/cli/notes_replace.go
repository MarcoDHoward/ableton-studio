// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source live
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

// Implemented in slice C; the logic lives in ableton_notes.go.
func newNovelNotesReplaceCmd(flags *rootFlags) *cobra.Command {
	var opts notesReplaceOpts

	cmd := &cobra.Command{
		Use:   "replace <track> <slot>",
		Short: "Overwrite a clip's MIDI notes from JSON with an automatic backup, and restore any earlier version.",
		Long: "Use this command to overwrite a clip's notes with an automatic backup. Do NOT use it to append or delete a few notes; use 'notes add' / 'notes clear' instead.\n\n" +
			"Before anything changes, the clip's current notes are saved to the local store\n" +
			"(list them with 'notes backups'). The clip is then cleared, the new notes are\n" +
			"written, and the clip is read back and compared with what was sent.\n" +
			"'notes restore <backup-id>' puts any backup back. The notes JSON is an array of\n" +
			"{pitch, start, duration, velocity, mute} objects (start and duration in beats),\n" +
			"the same shape 'notes get' prints. An empty array clears the clip.\n" +
			"--dry-run validates the notes and prints the steps without contacting Live.",
		Example: "  ableton-pp-cli notes replace Bass 1 --file bassline.json --agent\n" +
			"  ableton-pp-cli notes replace 2 3 --notes '[{\"pitch\":36,\"start\":0,\"duration\":1}]'\n" +
			"  ableton-pp-cli notes replace Bass 1 --file bassline.json --dry-run",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotesReplace(cmd, flags, args, opts)
		},
	}
	cmd.Flags().StringVar(&opts.file, "file", "", "JSON file with the new notes: an array of {pitch, start, duration, velocity, mute}")
	cmd.Flags().StringVar(&opts.notes, "notes", "", "The new notes as inline JSON (same shape as --file)")
	cmd.Flags().BoolVar(&opts.stdin, "stdin", false, "Read the new notes JSON from stdin")
	return cmd
}

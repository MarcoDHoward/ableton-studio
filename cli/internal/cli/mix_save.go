// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source live
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

// Implemented in slice C; the logic lives in ableton_mix.go.
func newNovelMixSaveCmd(flags *rootFlags) *cobra.Command {
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "save <name>",
		Short: "Save every track's volume, pan, mute, solo and sends under a name and recall them later.",
		Long: "Use this command to keep a named copy of the whole mixer (every track's fader,\n" +
			"pan, mute, solo and send levels) so 'mix recall <name>' can put it back later.\n" +
			"Do NOT use it to change one track's mixer setting; use 'track volume', 'track pan',\n" +
			"'track mute', 'track solo' or 'track send' instead. Do NOT use it to compare whole\n" +
			"project states; use 'snapshot diff' instead.\n\n" +
			"Tracks are stored by name, so a recall still finds them after they move. Reads\n" +
			"Live through AbletonOSC and writes only the local store. An existing mix of the\n" +
			"same name is kept unless --force. --dry-run reads the mixer and shows what would\n" +
			"be saved without storing it.",
		Example: "  ableton-pp-cli mix save dub-version --agent\n" +
			"  ableton-pp-cli mix save rough-1 --force\n" +
			"  ableton-pp-cli mix list",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMixSave(cmd, flags, args, flagForce)
		},
	}
	cmd.Flags().BoolVar(&flagForce, "force", false, "Replace an existing mix with the same name")
	return cmd
}

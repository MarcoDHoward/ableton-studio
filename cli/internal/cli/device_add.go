// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source live
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

// Implemented in slice C; the logic lives in ableton_device_add.go.
func newNovelDeviceAddCmd(flags *rootFlags) *cobra.Command {
	var flagCategory string

	cmd := &cobra.Command{
		Use:   "add <track> <name>",
		Short: "Load an instrument or effect onto a track by its browser name, such as Echo or Drift. Needs the browser index (run sync --resources browser once) and the AbletonMCP script.",
		Long: "Use this command to load an instrument or effect by its browser name. Do NOT use it when you already have a URI; use 'device load' instead. Do NOT use it to rebuild a multi-device chain with settings; use 'chain apply' instead.\n\n" +
			"The name is looked up in the local browser index (build it once with\n" +
			"'ableton-pp-cli sync --resources browser'): an exact name wins, then a unique\n" +
			"prefix, then a unique substring. Several matches are an error that lists them;\n" +
			"narrow it with --category or a path such as audio_effects/Echo. The track is\n" +
			"matched by name or number the same way. The device is then loaded through\n" +
			"AbletonMCP by URI, exactly like 'device load'. --dry-run resolves both names\n" +
			"(reading the track list from Live) and loads nothing.",
		Example: "  ableton-pp-cli device add Bass Drift --agent\n" +
			"  ableton-pp-cli device add \"Dub Bass\" Echo --category audio_effects\n" +
			"  ableton-pp-cli device add 3 audio_effects/Reverb --dry-run",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceAdd(cmd, flags, args, flagCategory)
		},
	}
	cmd.Flags().StringVar(&flagCategory, "category", "", "Only match browser items in this category: instruments, sounds, drums, audio_effects, midi_effects or plugins")
	return cmd
}

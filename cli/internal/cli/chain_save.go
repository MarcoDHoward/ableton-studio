// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source live
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

const chainLong = "Use this command to rebuild a saved multi-device chain with its parameter values. Do NOT use it to load a single device; use 'device add' instead."

// Implemented in slice C; the logic lives in ableton_chain.go.
func newNovelChainSaveCmd(flags *rootFlags) *cobra.Command {
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "save <track> <name>",
		Short: "Save a track's chain of devices with their settings; chain apply rebuilds it on another track (needs the browser index and the AbletonMCP script).",
		Long: "Use this command to save a track's multi-device chain with its parameter values, so 'chain apply' can rebuild it. Do NOT use it to load a single device; use 'device add' instead.\n\n" +
			"'chain save' stores the track's devices in order (name, class, type, on/off) and\n" +
			"every parameter value in the local store; 'chain apply <name> <track>' rebuilds\n" +
			"it. Reads Live through AbletonOSC; writes only the local store. An existing\n" +
			"chain of the same name is kept unless --force. --dry-run reads the devices and\n" +
			"shows what would be stored without storing it.",
		Example: "  ableton-pp-cli chain save Dub-Echo-Bus dub-chain --agent\n" +
			"  ableton-pp-cli chain apply dub-chain \"Vox Bus\" --dry-run\n" +
			"  ableton-pp-cli chain list",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChainSave(cmd, flags, args, flagForce)
		},
	}
	cmd.Flags().BoolVar(&flagForce, "force", false, "Replace an existing chain with the same name")
	return cmd
}

// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source auto
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

func newNovelChainCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "chain",
		Short:       "Work with chain",
		Example:     "  ableton-pp-cli chain list\n  ableton-pp-cli chain save Dub-Echo-Bus dub-chain --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "auto", "pp:typed-exit-codes": "0,2"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newNovelChainSaveCmd(flags))
	cmd.AddCommand(newChainApplyCmd(flags))
	cmd.AddCommand(newChainListCmd(flags))
	return cmd
}

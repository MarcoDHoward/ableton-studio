// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source auto
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

func newNovelJournalCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "journal",
		Short:       "Work with journal",
		Example:     "  ableton-pp-cli journal list --since 1h --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "auto", "pp:typed-exit-codes": "0,2"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newNovelJournalListCmd(flags))
	cmd.AddCommand(newJournalRevertCmd(flags))
	return cmd
}

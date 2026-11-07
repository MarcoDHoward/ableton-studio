// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ableton-pp-cli/internal/client"
	"ableton-pp-cli/internal/store"
)

// Wires the Ableton router's change journal to the local store, records the
// running command path for each journal row, and registers the hand-written
// top-level commands (tempo, watch).
func init() {
	client.JournalHook = writeAbletonJournal
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		prev := root.PersistentPreRunE
		root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
			client.JournalCommand = strings.TrimSpace(strings.TrimPrefix(cmd.CommandPath(), root.Name()))
			if prev != nil {
				return prev(cmd, args)
			}
			return nil
		}
		addNovelCommandIfAbsent(root, newTempoCmd(flags))
		addNovelCommandIfAbsent(root, newWatchCmd(flags))
	})
}

// writeAbletonJournal appends one journal row. The operator's local-data
// kill switch disables journaling too.
func writeAbletonJournal(ctx context.Context, e client.JournalEntry) (int64, error) {
	if localDataDisabled() {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	db, err := store.OpenWithContext(ctx, defaultDBPath("ableton-pp-cli"))
	if err != nil {
		return 0, err
	}
	defer db.Close()
	return store.InsertJournal(ctx, db.DB(), time.Now(), e.Command, e.Method, e.Path, e.Target, e.Property, e.Before, e.After)
}

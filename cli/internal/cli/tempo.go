// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// newTempoCmd: `tempo` prints the song tempo; `tempo 124` sets it through
// the same router path as `song set tempo` (read-back and journaled).
func newTempoCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tempo [bpm]",
		Short: "Show the tempo, or set it to a BPM between 20 and 999",
		Long: strings.Trim(`
Show Live's song tempo, or set it. Setting reads the old value, changes it,
reads it back and records the change in the local journal.
`, "\n"),
		Example: strings.Trim(`
  ableton-pp-cli tempo
  ableton-pp-cli tempo 124
  ableton-pp-cli tempo 92.5 --json
`, "\n"),
		Annotations: map[string]string{"pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "tempo")
			}
			if len(args) > 1 {
				return usageErr(fmt.Errorf("tempo takes at most one argument (the BPM), got %d", len(args)))
			}
			var bpm float64
			setting := len(args) == 1
			if setting {
				v, err := strconv.ParseFloat(strings.TrimSpace(args[0]), 64)
				if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
					return usageErr(fmt.Errorf("tempo must be a number of BPM, got %q", args[0]))
				}
				if v < 20 || v > 999 {
					return usageErr(fmt.Errorf("tempo must be between 20 and 999 BPM, got %v", v))
				}
				bpm = v
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			path := "/song/props/tempo"
			var data json.RawMessage
			if setting {
				data, _, err = c.Put(cmd.Context(), path, map[string]any{"value": strconv.FormatFloat(bpm, 'f', -1, 64)})
			} else {
				data, err = c.Get(cmd.Context(), path, nil)
			}
			if err != nil {
				return classifyAPIError(cmd.OutOrStdout(), err, flags)
			}
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				var out struct {
					Value  any `json:"value"`
					Before any `json:"before"`
					After  any `json:"after"`
					ID     any `json:"journal_id"`
				}
				if json.Unmarshal(data, &out) == nil {
					if setting && out.After != nil {
						_, perr := fmt.Fprintf(cmd.OutOrStdout(), "Tempo %v -> %v BPM\n", out.Before, out.After)
						return perr
					}
					if !setting && out.Value != nil {
						_, perr := fmt.Fprintf(cmd.OutOrStdout(), "%v BPM\n", out.Value)
						return perr
					}
				}
			}
			return printJSONFiltered(cmd.OutOrStdout(), data, flags)
		},
	}
	return cmd
}

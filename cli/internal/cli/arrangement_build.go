// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source live
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

// Implemented in slice C; the logic lives in ableton_arrangement.go.
//
// pp:client-call runArrangementBuild reaches Live through newLiveAPI
// (flags.newClient -> client.Get/Post on the routed /song/props, /scenes,
// /clips, /arrangement/{track}/clips, /clips/{track}/{slot}/to-arrangement and /locators
// endpoints), the same client path the generated commands use.
func newNovelArrangementBuildCmd(flags *rootFlags) *cobra.Command {
	var opts arrBuildOpts

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Lay out a whole song in the Arrangement from Session scenes, with a named locator at each section. Needs the AbletonMCP script.",
		Long: "Use this command to lay out whole sections from scenes. Do NOT use it to place one clip at one beat; use 'clip to-arrangement' instead.\n\n" +
			"Each section plays one scene for a number of bars. Sections follow each other\n" +
			"from --start (default 1.1). Every clip in the scene's row is copied into the\n" +
			"Arrangement on its own track (through AbletonMCP, like 'clip to-arrangement'),\n" +
			"repeated back to back until the section is full; a section without a bar count\n" +
			"lasts as long as the longest clip in its row, rounded up to whole bars. A named\n" +
			"locator marks each section start (--no-locators skips them).\n\n" +
			"Give the sections with --plan \"Intro:4,Drop:8\" (scene:bars, in order), with\n" +
			"repeated --section Name:scene=Scene:bars=N, or with --all-scenes (every scene\n" +
			"row that has clips, top to bottom; --bars sets one length for all).\n\n" +
			"Existing Arrangement clips inside the new song's range are a conflict unless\n" +
			"--force. --dry-run reads the scenes and clips from Live and prints the full\n" +
			"layout (sections, every clip copy, locators) without changing anything.",
		Example: "  ableton-pp-cli arrangement build --plan \"Intro:4,Drop:8,Outro:4\" --dry-run\n" +
			"  ableton-pp-cli arrangement build --section Intro:scene=Intro:bars=8 --section Verse:scene=Verse:bars=16 --dry-run\n" +
			"  ableton-pp-cli arrangement build --all-scenes --bars 8 --start 17.1 --json",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArrangementBuild(cmd, flags, args, opts)
		},
	}
	cmd.Flags().StringVar(&opts.plan, "plan", "", "Sections in order as Scene:bars pairs, e.g. \"Intro:4,Drop:8\" (bars optional)")
	cmd.Flags().StringArrayVar(&opts.sections, "section", nil, "One section as Name:scene=Scene:bars=N (repeatable; scene defaults to Name, bars to the scene's clip length)")
	cmd.Flags().BoolVar(&opts.allScenes, "all-scenes", false, "Use every scene row that has clips, top to bottom")
	cmd.Flags().Float64Var(&opts.bars, "bars", 0, "Bars for every section that does not give its own length")
	cmd.Flags().StringVar(&opts.start, "start", "1.1", "Where the song starts: bar.beat (17.1) or beats (64)")
	cmd.Flags().BoolVar(&opts.noLocators, "no-locators", false, "Do not add a locator at each section start")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Build even when Arrangement clips already exist in the song's range (they get overwritten)")
	return cmd
}

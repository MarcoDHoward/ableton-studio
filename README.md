# ableton-studio

Claude as a co-producer in Ableton Live 12. It writes music into a scratch set, listens to the result, scores it against sound thumbprints of my own tracks, and iterates.

- `cli/`: `ableton-pp-cli`, the command line that controls Live through AbletonOSC, AbletonMCP and our own AbletonPPExtras Remote Script
- `analysis/`: set and stem analysis (`.als` extraction, STFT, thumbprints, findings page)
- `engine/`: music engine: theory, rhythm and parts, plus a critic
- `harness/`: the agentic loop and the scratch-set guard
- `thumbprints/`: thumbprints of my tracks (the first is ProgAcidAI)

See `CLAUDE.md` for the rules Claude follows here, and `docs/` for the bridge protocol and research.

## Setup

1. Install AbletonOSC and the AbletonMCP Remote Script into the Live User Library. Select them in Control Surface slots 2 and 3 with Input/Output set to None. Slot 1 stays MiniLab_3.
2. Build the CLI: `scripts/setup.ps1`. Install AbletonPPExtras with `cli/ableton-pp-cli.exe scripts install --yes`, then select it in slot 4.
3. Save an empty set as `live/SCRATCH Claude.als`. Claude only writes freely in sets whose name starts with `SCRATCH`.
4. Install the analysis packages: `pip install numpy scipy matplotlib`.

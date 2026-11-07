---
name: pp-ableton
description: "Control Ableton Live from the terminal by track and device name, with backups for note edits and a journal of every change. Trigger phrases: `set the tempo in Ableton`, `mute the drums in Live`, `load an instrument on this track`, `put these notes in the clip`, `lay out the arrangement from my scenes`, `use ableton-pp-cli`, `run the ableton cli`."
author: "Marco"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - ableton-pp-cli
    install:
      - kind: go
        bins: [ableton-pp-cli]
        module: github.com/mvanhorn/printing-press-library/library/media-and-entertainment/ableton/cmd/ableton-pp-cli
---

# Ableton Live — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `ableton-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer. It defaults binaries to `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows:
   ```bash
   npx -y @mvanhorn/printing-press-library install ableton --cli-only
   ```
2. Verify: `ableton-pp-cli --version`
3. Ensure the reported install directory is on `$PATH` for the agent/runtime that will invoke this skill.

If the `npx` install fails (no Node, offline, etc.), fall back to a direct Go install (requires Go 1.26.10 or newer). This installs into `$GOPATH/bin` (default `$HOME/go/bin`), so add that directory to `$PATH` instead:

```bash
go install github.com/mvanhorn/printing-press-library/library/media-and-entertainment/ableton/cmd/ableton-pp-cli@latest
```

If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

Drives Live through the AbletonOSC and AbletonMCP Remote Scripts plus a small companion script, so transport, mixer, clips, notes, devices, the browser and the Arrangement are all scriptable. `notes replace` backs up a clip before overwriting it, `journal revert` rolls back the CLI's own changes, and `arrangement build` lays out a song from scenes. Nothing leaves your machine.

## When to Use This CLI

Use this CLI when an agent or script needs to read or change a running Ableton Live set: transport, tempo, mixer, clips, MIDI notes, devices and parameters, scenes, the browser, locators and the Arrangement. Prefer `notes replace` over clearing and re-adding notes, and check `journal list` to report what you changed.

## Anti-triggers

Do not use this CLI for:
- Do not use this CLI to render, bounce or export audio; Live's export dialog is the only way to do that.
- Do not use it to edit Live set files (.als) on disk when Live is closed; it only talks to a running Live.
- Do not use it to compose music on its own; it writes the notes it is given and has no groove or chord generator.
- Do not use it to change Live's preferences, audio driver or MIDI port settings.

## Unique Capabilities

### Safe edits you can take back
- **`notes replace`** — Overwrite a clip's MIDI notes from JSON with an automatic backup, and restore any earlier version.

  _Use this whenever you write generated notes into an existing clip, so the user's original part can always be restored._

  ```bash
  ableton-pp-cli notes replace Bass 1 --file bassline.json --agent
  ```
- **`mix save`** — Save every track's volume, pan, mute, solo and sends under a name and recall them later.

  _Use this to switch between a full mix and a dub version, or to snapshot the mix before changing it._

  ```bash
  ableton-pp-cli mix save dub-version --agent
  ```
- **`journal list`** — See every change this CLI made, with before and after values; roll back specific ones with journal revert.

  _Use this to report exactly what you changed in the set, and to undo only your own changes._

  ```bash
  ableton-pp-cli journal list --since 1h --agent
  ```
- **`snapshot diff`** — Compare two saved project snapshots, or a snapshot against the live set: tracks, mixer, devices, parameters, clips and notes. Reading the live set needs the AbletonMCP script.

  _Run snapshot save before-edit before a task, then use this to check what changed in the set, including the user's own manual edits._

  ```bash
  ableton-pp-cli snapshot diff before-edit --live --agent
  ```

### Building a song
- **`device add`** — Load an instrument or effect onto a track by its browser name, such as Echo or Drift. Needs the browser index (run sync --resources browser once) and the AbletonMCP script.

  _Reach for this instead of browsing for URIs whenever the user names an instrument or effect._

  ```bash
  ableton-pp-cli device add Bass Drift --agent
  ```
- **`arrangement build`** — Lay out a whole song in the Arrangement from Session scenes, with a named locator at each section. Needs the AbletonMCP script.

  _Use this to turn a jammed Session into a song structure in one step instead of placing clips one at a time._

  ```bash
  ableton-pp-cli arrangement build --section Intro:scene=Intro:bars=8 --section Verse:scene=Verse:bars=16 --dry-run
  ```
- **`chain save`** — Save a track's chain of devices with their settings; chain apply rebuilds it on another track (needs the browser index and the AbletonMCP script).

  _Use this to reuse a favorite effect chain on a new track or project._

  ```bash
  ableton-pp-cli chain save Dub-Echo-Bus dub-chain --agent
  ```

## Command Reference

**arrangement** — Clips on the Arrangement timeline

- `ableton-pp-cli arrangement add-clip` — Create an empty MIDI clip on the Arrangement timeline (needs AbletonPPExtras)
- `ableton-pp-cli arrangement clips` — List a track's Arrangement clips with start, end and bar.beat positions
- `ableton-pp-cli arrangement delete-clip` — Delete an Arrangement clip (needs AbletonPPExtras)

**automation** — Clip automation envelopes (needs AbletonPPExtras)

- `ableton-pp-cli automation clear` — Clear a clip's automation envelope for a device parameter
- `ableton-pp-cli automation get` — Read a clip's automation envelope for a device parameter
- `ableton-pp-cli automation set` — Write automation points into a clip's envelope from JSON

**browser** — Live's browser (instruments, sounds, drums, effects, plugins)

- `ableton-pp-cli browser index` — Every loadable browser item (two levels deep), for 'sync' and 'device add'
- `ableton-pp-cli browser list` — List the items inside a browser folder, e.g. instruments or drums/Kits
- `ableton-pp-cli browser tree` — Show the browser's top-level categories and their folders

**clip** — Work with Session clips

- `ableton-pp-cli clip create` — Create an empty MIDI clip in a slot
- `ableton-pp-cli clip delete` — Delete the clip in a slot
- `ableton-pp-cli clip duplicate` — Copy a clip to an empty slot
- `ableton-pp-cli clip duplicate-loop` — Double a clip's loop, copying its notes into the new half
- `ableton-pp-cli clip fire` — Launch a clip
- `ableton-pp-cli clip get` — Show all properties of a Session clip
- `ableton-pp-cli clip import-audio` — Create an audio clip from a local audio file (Live 12.0.5 or newer)
- `ableton-pp-cli clip set` — Change one clip property (the change is journaled)
- `ableton-pp-cli clip stop` — Stop a playing clip
- `ableton-pp-cli clip to-arrangement` — Copy a Session clip onto the same track in the Arrangement at a position

**clips** — The Session clip grid

- `ableton-pp-cli clips` — Show every track's clip slots with clip names and lengths

**device** — Devices on a track

- `ableton-pp-cli device delete` — Delete a device from a track (needs the AbletonPPExtras script)
- `ableton-pp-cli device disable` — Turn a device off (bypass)
- `ableton-pp-cli device enable` — Turn a device on
- `ableton-pp-cli device list` — List a track's devices with their type and class
- `ableton-pp-cli device load` — Load an instrument, effect or plugin onto a track by its browser URI

**drums** — Drum Racks

- `ableton-pp-cli drums load` — Load a Drum Rack and a kit from the browser onto a track
- `ableton-pp-cli drums pads` — List a Drum Rack's filled pads (needs the AbletonPPExtras script)

**health** — Remote Script health

- `ableton-pp-cli health` — Check which Remote Scripts answer and their versions

**locator** — Arrangement locators (cue points)

- `ableton-pp-cli locator add` — Add a named locator at a beat or bar.beat (renames an existing one at the same spot)
- `ableton-pp-cli locator delete` — Delete a locator
- `ableton-pp-cli locator jump` — Move the playhead to a locator, by name or number
- `ableton-pp-cli locator list` — List locators with their beat and bar.beat positions
- `ableton-pp-cli locator next` — Jump to the next locator
- `ableton-pp-cli locator prev` — Jump to the previous locator
- `ableton-pp-cli locator rename` — Rename a locator

**message** — Show a message in Live's status bar

- `ableton-pp-cli message` — Show a short message in Live's status bar

**midimap** — Map a MIDI CC to a device parameter

- `ableton-pp-cli midimap` — Make a MIDI CC control a device parameter

**notes** — MIDI notes in Session clips

- `ableton-pp-cli notes add` — Add notes to a MIDI clip from JSON (keeps existing notes)
- `ableton-pp-cli notes clear` — Remove notes from a MIDI clip, optionally only within a pitch and time range
- `ableton-pp-cli notes get` — Read every note in a MIDI clip as JSON (pitch, start, duration, velocity, mute)

**param** — Device parameters

- `ableton-pp-cli param list` — List a device's parameters with value, range and display text
- `ableton-pp-cli param set` — Set a device parameter by name or number (range-checked and journaled)

**play** — Start playback

- `ableton-pp-cli play` — Start Live's transport from the current position

**rack** — Instrument, Audio Effect and Drum Racks

- `ableton-pp-cli rack chains` — List a rack's chains with volume, pan, mute and solo (needs AbletonPPExtras)
- `ableton-pp-cli rack inspect` — Show a rack's nested devices and which macros are mapped
- `ableton-pp-cli rack map-macro` — Map every nested device's Dry/Wet (or Mix/Amount) to Macro 1 and name it

**record** — Recording controls

- `ableton-pp-cli record capture` — Capture MIDI you just played into a new clip
- `ableton-pp-cli record session` — Turn Session record on or off
- `ableton-pp-cli record trigger` — Trigger Session record (like pressing the Session record button)

**redo** — Redo the last undone change in Live

- `ableton-pp-cli redo` — Redo the last undone operation

**scene** — Session scenes

- `ableton-pp-cli scene create` — Create a scene
- `ableton-pp-cli scene delete` — Delete a scene
- `ableton-pp-cli scene duplicate` — Duplicate a scene with its clips
- `ableton-pp-cli scene fire` — Launch a scene
- `ableton-pp-cli scene fire-selected` — Launch the selected scene and select the next one
- `ableton-pp-cli scene list` — List scenes with name, tempo, time signature and color
- `ableton-pp-cli scene set` — Change a scene's name, color, tempo or time signature

**snapshot** — Whole-project snapshots

- `ableton-pp-cli snapshot` — List stored project snapshots (same as 'snapshot list'); Live is not contacted. 'snapshot save' reads the set

**song** — Song-level settings and transport actions

- `ableton-pp-cli song continue` — Resume playback from where it stopped
- `ableton-pp-cli song get` — Read a song property
- `ableton-pp-cli song seek` — Move the playhead to a beat or bar.beat position, or jump by a number of beats
- `ableton-pp-cli song set` — Change a song property (the change is journaled)
- `ableton-pp-cli song stop-clips` — Stop all playing clips, or only one track's clips with --track
- `ableton-pp-cli song tap-tempo` — Tap the Tap Tempo button once

**status** — Session overview: tempo, signature, transport and every track

- `ableton-pp-cli status` — Show tempo, time signature, play state, loop and every track with its mixer state

**stop** — Stop playback

- `ableton-pp-cli stop` — Stop Live's transport

**track** — Work with one track: mixer, routing, create, delete

- `ableton-pp-cli track arm` — Arm or disarm a track for recording
- `ableton-pp-cli track color` — Set a track's color from Live's palette (0-69)
- `ableton-pp-cli track create` — Create a MIDI, audio or return track
- `ableton-pp-cli track delete` — Delete a track
- `ableton-pp-cli track duplicate` — Duplicate a track with its devices and clips
- `ableton-pp-cli track get` — Show a track's mixer state, devices and clip slots
- `ableton-pp-cli track meter` — Current output levels for every track
- `ableton-pp-cli track mute` — Mute or unmute a track
- `ableton-pp-cli track pan` — Set a track's pan from -1 (left) to 1 (right)
- `ableton-pp-cli track rename` — Rename a track
- `ableton-pp-cli track routing` — Show a track's input/output routing, the available routes and its monitoring state
- `ableton-pp-cli track send` — Set a send level (send A = 1) as a 0-1 knob value
- `ableton-pp-cli track set-routing` — Change a track's input/output routing or monitoring
- `ableton-pp-cli track solo` — Solo or unsolo a track
- `ableton-pp-cli track volume` — Set a track's volume as a 0-1 fader value, or in dB (-inf to +6; --db needs AbletonPPExtras)

**tracks** — Every track with its mixer state

- `ableton-pp-cli tracks` — List every track: number, name, type, volume (0-1 fader, plus dB with AbletonPPExtras), pan, mute, solo, arm and sends

**undo** — Undo the last change in Live

- `ableton-pp-cli undo` — Undo the last operation in Live's undo history (includes your own mouse edits)

**view** — What is selected and shown in Live

- `ableton-pp-cli view get` — Show the selected track, scene, clip and device
- `ableton-pp-cli view select` — Select a track, scene, clip or device
- `ableton-pp-cli view show` — Switch Live's main window to the Arrangement view (switching to Session is not supported)


**arrangement**


- `ableton-pp-cli arrangement build` — Lay out a whole song in the Arrangement from Session scenes, with a named locator at each section. Needs the AbletonMCP script.


**chain**


- `ableton-pp-cli chain save` — Save a track's chain of devices with their settings; chain apply rebuilds it on another track (needs the browser index and the AbletonMCP script).


**device**


- `ableton-pp-cli device add` — Load an instrument or effect onto a track by its browser name, such as Echo or Drift. Needs the browser index (run sync --resources browser once) and the AbletonMCP script.


**journal**


- `ableton-pp-cli journal list` — See every change this CLI made, with before and after values; roll back specific ones with journal revert.


**mix**


- `ableton-pp-cli mix save` — Save every track's volume, pan, mute, solo and sends under a name and recall them later.


**notes**


- `ableton-pp-cli notes replace` — Overwrite a clip's MIDI notes from JSON with an automatic backup, and restore any earlier version.


**snapshot**


- `ableton-pp-cli snapshot diff` — Compare two saved project snapshots, or a snapshot against the live set: tracks, mixer, devices, parameters, clips and notes. Reading the live set needs the AbletonMCP script.




### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
ableton-pp-cli which "load a synth on a track"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query. `--json` (and other machine formats) keep that exit-2 contract and write `{"matches":[]}` on stdout so agents can inspect the envelope without treating a miss as success.

## Hand-written Extensions

These commands are hand-written for this CLI and fully wired. This list is a compact signature reference; run `ableton-pp-cli <command> --help` for flags.

- `ableton-pp-cli tempo [bpm]` — Show the tempo, or set it to a BPM between 20 and 999
- `ableton-pp-cli watch <what>...` — Stream live changes (tempo, playing, beat, selection, a track's mixer, a parameter) as NDJSON
- `ableton-pp-cli notes replace <track> <slot>` — Overwrite a clip's notes from JSON with an automatic backup and read-back check
- `ableton-pp-cli notes restore <backup-id>` — Put back a clip's notes from a backup (see notes backups); --track/--slot send it elsewhere
- `ableton-pp-cli notes backups` — List saved note backups (filter with --track, --slot, --limit)
- `ableton-pp-cli device add <track> <name>` — Load an instrument or effect by its browser name, no URI needed
- `ableton-pp-cli arrangement build` — Lay out a song in the Arrangement from Session scenes, with a locator per section
- `ableton-pp-cli mix save <name>` — Save every track's volume, pan, mute, solo and sends under a name
- `ableton-pp-cli mix recall <name>` — Restore a saved mixer state by track name
- `ableton-pp-cli mix list` — List saved mixer states
- `ableton-pp-cli journal list` — List changes this CLI made, with before and after values
- `ableton-pp-cli journal revert [id...]` — Roll back specific changes this CLI made, by id or with --last N
- `ableton-pp-cli snapshot save <label>` — Save the whole project's state locally under a label
- `ableton-pp-cli snapshot list` — List saved project snapshots
- `ableton-pp-cli snapshot diff <a> [b]` — Compare two snapshots, or a snapshot against the open set with --live
- `ableton-pp-cli chain save <track> <name>` — Save a track's device chain with its parameter values
- `ableton-pp-cli chain apply <name> <track>` — Rebuild a saved device chain on a track
- `ableton-pp-cli chain list` — List saved device chains
- `ableton-pp-cli scripts install` — Show the plan for installing the AbletonPPExtras Remote Script into your Ableton User Library; --yes copies it (a differing __init__.py is kept as .bak)

## Recipes

### Mixer state of every track

```bash
ableton-pp-cli tracks --agent --select name,volume,volume_db,mute,solo,arm
```

Lists every track's name and mixer state, trimmed to the fields an agent needs. volume is the 0-1 fader (0.85 is 0 dB); volume_db is filled only when AbletonPPExtras is installed.

### Replace a bassline safely

```bash
ableton-pp-cli notes replace Bass 1 --file bassline.json
```

Backs up the clip's current notes, writes the new ones, and verifies them; `notes backups --track Bass` lists the backups and `notes restore <backup-id>` brings the old part back.

### Save and recall a dub mix

```bash
ableton-pp-cli mix save full-mix
```

Saves the current mixer; after riding mutes and sends, `mix recall full-mix` puts everything back.

### Lay out a song from scenes

```bash
ableton-pp-cli arrangement build --section Intro:scene=Intro:bars=8 --section Verse:scene=Verse:bars=16 --dry-run
```

Previews the Arrangement layout; run it again without --dry-run to place the clips and locators.

### Undo only the CLI's last change

```bash
ableton-pp-cli journal revert --last 1
```

Writes back the previous value of the last change made through this CLI, leaving your own edits alone.

## Auth Setup

No account or API key. The CLI talks only to Remote Scripts running inside Live on your own computer: AbletonOSC on UDP port 11000 (replies on 11001), the AbletonMCP script on TCP port 9877, and AbletonPPExtras on TCP port 9878. AbletonOSC and AbletonMCP are installed from their own projects into your User Library's Remote Scripts folder. AbletonPPExtras is optional (dB volume, rack chains, drum pads, automation, Arrangement clip add/delete and device delete use it); install it with `ableton-pp-cli scripts install --yes` and restart Live. Select each script in Live's Settings > Link, Tempo & MIDI with Input and Output set to None.

Run `ableton-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color`.

Global format flags share one contract on promoted, novel, sync, and `--deliver` paths:

- `--json` — one JSON document on stdout (sync progress events go to stderr)
- `--compact` — keep identity/status/timestamp fields; does not change the document vs stream shape
- `--csv` / `--plain` — tabular rows (collection envelopes unwrap to the row array). `--csv` prefixes `'` on a cell or header key that starts with `=`, `+`, `-`, `@`, tab, CR, or LF (fullwidth `＝＋－＠` too) so a spreadsheet does not run it as a formula; a plain signed number such as `-5` or `+2.5e3` is unchanged.
- `--quiet` — one identity value per row, no envelope

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on large sets:

  ```bash
  ableton-pp-cli tracks --agent --select name,volume,mute,solo
  ```
- **Previewable** — `--dry-run` never changes Live. Single-endpoint commands send nothing; composite commands (`arrangement build`, `mix save`/`recall`, `device add`, `chain save`/`apply`, `journal revert`) read Live to build their plan but never write. A completed dry run exits 0 with `"success": true` and `"dry_run": true`; validation failures still exit non-zero with `"success": false`.
- **Offline-friendly** — sync/search commands can use the local SQLite store when available
- **Non-interactive** — never prompts, every input is a flag
- **Explicit confirmation** — `--agent` does not imply `--yes`; pass `--yes` separately only after the target, arguments, and side effects are clear
- **No blind retries** — leave `--idempotent` and `--ignore-missing` off: in this CLI a 404 is usually a misspelled track, device or scene, and `--ignore-missing` turns it into a silent success on deletes. Check `journal list` instead of retrying a write

### Response envelope

Commands that read from the local store or the API wrap output in a provenance envelope:

```json
{
  "meta": {"source": "live" | "local", "synced_at": "...", "reason": "..."},
  "results": <data>
}
```

Parse `.results` for data and `.meta.source` to know whether it's live or local. A human-readable `N results (live)` summary is printed to stderr only when stdout is a terminal AND no machine-format flag (`--json`, `--csv`, `--compact`, `--quiet`, `--plain`, `--select`) is set — piped/agent consumers and explicit-format runs get pure JSON on stdout.

## Paths and state

Agents should treat the CLI's path resolver as part of the runtime contract:

- Use `--home <dir>` for one invocation, or set `ABLETON_HOME=<dir>` to relocate all four path kinds under one root.
- Use per-kind env vars only when a specific kind must diverge: `ABLETON_CONFIG_DIR`, `ABLETON_DATA_DIR`, `ABLETON_STATE_DIR`, `ABLETON_CACHE_DIR`.
- Resolution order is per-kind env var, `--home`, `ABLETON_HOME`, XDG (`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`), then platform defaults.
- `config` contains settings like `config.toml` and profiles. `data` contains durable local data such as `data.db`. `state` contains persisted queries, jobs, and `teach.log`. `cache` is unused: Live reads are never cached.
- Run `ableton-pp-cli doctor --fail-on warn` to surface path warnings. `agent-context` exposes a schema v4 `paths` block for agents that need the resolved dirs.
- For MCP, pass relocation through the MCP host config. The MCP binary does not inherit CLI flags:

  ```json
  {
    "mcpServers": {
      "ableton": {
        "command": "ableton-pp-mcp",
        "env": {
          "ABLETON_HOME": "/srv/ableton"
        }
      }
    }
  }
  ```

Fleet precedence: an inherited per-kind env var overrides an explicit `--home` for that kind. Use `ABLETON_HOME` or per-kind vars as durable fleet levers, and use `--home` only for a single invocation. Relocation is not reversible by unsetting env vars; move files manually before clearing `ABLETON_HOME`, or `doctor` will not find files left under the former root.

Local data is unencrypted and relies on OS account permissions plus disk encryption. Reads in the default `--data-source auto` mode are written to the SQLite store; Live reads are never cached, because Live's state changes constantly. `local-data purge` previews what it would erase (store files, response cache, learn journal) and deletes only with `--yes`; it is not exposed over MCP. `ABLETON_NO_LOCAL_DATA=1` disables the response cache and store writes, turns `auto` reads into live reads, and blocks `sync` and `workflow archive`; `--data-source local` still reads an existing store, and `ABLETON_DATA_SOURCE` sets the `--data-source` default.

## Automatic learning

This CLI ships a self-capturing learning loop. The CLI does its own bookkeeping: every invocation is journaled locally, a failed flag followed by a corrected retry auto-derives a `flag_alias` candidate, and a `teach` on a query family without a playbook auto-synthesizes a `playbook_candidate` from the session's journal. Your job is judgment only: `recall` first, act on surfaced candidates, `teach` the final answer, `playbook amend` when you observe a correction. You never record failures by hand.

### Step 1: `recall` before any discovery

Before list/search/drill commands on a new user question, pass the question as an argv or MCP tool argument to `recall --agent`. Do not interpolate user-controlled text into a shell command line.

Quoted `recall "<question>"` breaks on an apostrophe, which is ordinary English. A quoted heredoc breaks when a body line equals the delimiter, and that delimiter is published in these docs. Write the question with a non-shell file-writing tool, then read it back as data:

```bash
# Write the question verbatim with your file-writing tool (no shell involved).
# Command substitution on a file only ever yields data — the shell never
# parses the file's bytes as syntax.
QUERY=$(cat /path/to/question.txt)
ableton-pp-cli recall "$QUERY" --agent
```

Prefer MCP: pass the question as the tool's query argument. `"$QUERY"` after a file read is argv-safe; putting the question itself in the command text is not.

The response envelope:

```json
{
  "query": "...",
  "normalized": "<normalized form>",
  "query_entities": ["..."],
  "found": true | false,
  "match_score": 0.0,
  "results": [
    { "resource_id": "...", "resource_type": "...", "venue": "...",
      "confidence": 2, "entity_match": "exact|partial|unknown",
      "source": "taught|preseed|pattern", "warnings": ["..."] }
  ],
  "mismatches": [ /* only when --debug-mismatches */ ],
  "warnings": [ /* top-level */ ],
  "candidates": [
    { "id": 12, "class": "flag_alias | playbook_candidate",
      "summary": "...", "sightings": 3, "last_seen": "...",
      "rationale": "...",
      "next_action": ["<trial command>", "ableton-pp-cli learnings confirm 12"] }
  ],
  "playbook": {
    "query_family": "...",
    "playbook": {
      "steps": [ { "cmd": "<command with {slot} substitution>", "purpose": "..." } ],
      "entity_slots": ["$ENTITY"],
      "expected_tool_calls": 3
    },
    "slots_resolved": { "$ENTITY": { "token": "<live token>", "canonical": "<canonical>" } },
    "notes": "<workarounds + gotchas for this query family>"
  },
  "notes": "<duplicate surface for non-playbook callers>"
}
```

Empty-store short-circuit: if the store has no learnings, playbooks, or candidates yet (recall finds nothing and `learnings list` and `learnings candidates` are both empty), skip recall for the rest of this session instead of taxing every query; resume recall-first once something has been taught.

### Step 2: decision tree

Read `candidates`, `playbook`, `notes`, `results[0]`, and warnings in that order:

```
if Candidates present (warnings include "candidates_present"):
    -> candidates are try-then-confirm, never facts. Follow each candidate's
       two-step next_action verbatim: run the trial command first, then run
       `learnings confirm <id>` only after the trial verified the behavior.
       Reject a wrong candidate with `learnings reject <id>`.
    -> NEVER re-teach something recall surfaced as a candidate; confirm or
       reject that candidate instead of teaching a duplicate.
    -> candidates ride alongside playbooks and resource hits, not instead of
       them; continue with the branches below after acting on them.

if Playbook present:
    -> READ Playbook.notes verbatim FIRST (workarounds + gotchas the CLI surface doesn't expose)
    -> replay Playbook.steps in order, substituting Playbook.slots_resolved entries
       for the entity slot tokens. If a step's slot is unresolved, fall back to
       discovery for that step only.
    -> the Playbook's expected_tool_calls is a budget; if you find yourself running
       materially more, record the divergence via `ableton-pp-cli playbook amend`
       at end-of-session.

elif Notes present (no Playbook):
    -> read Notes verbatim before any discovery step; they carry known gotchas
       for this query family even when no structured choreography exists yet.

elif Found AND Results[0].EntityMatch == "exact" AND Results[0].Confidence >= 2:
    -> skip discovery; fetch live data for Results[*].ResourceID in parallel

elif Found AND Results[0].EntityMatch == "partial":
    -> candidate hint, NOT a hit; read the resource title to validate before trusting

elif (any row in Mismatches[] when --debug-mismatches was passed):
    -> treat as cold start; the stored learning is for a different entity
       (different canonical resolved from query_entities)

else:  // Found == false, no playbook, no notes
    -> cold start; run discovery normally; teach the answer afterward (Step 4).
       If the family has no playbook yet, that teach auto-synthesizes a
       playbook candidate from this session's journal - you do not need to
       record one by hand.
```

Playbook and Notes are orthogonal to the per-resource path. A recall response can carry both a Playbook AND a `Results[]` hit - use both: the Playbook tells you which choreography to run; the resource hits short-circuit specific steps. Default to skipping `mismatches`; pass `--debug-mismatches` only when investigating cold-start surprises.

Candidate judgment details: `learnings confirm <id>` prints the candidate's full payload before materializing it - check that the printed payload matches the behavior you verified. `learnings reject <id>` tombstones the derivation signature so the same candidate does not resurface. The envelope carries only the few candidates worth acting on now; `ableton-pp-cli learnings candidates` lists the full open set.

Graceful degradation: if `learnings confirm` is an unknown command, you are driving an older binary - ignore the candidates guidance and follow the rest of the protocol.

### Step 3: always read `warnings`

- `low_confidence`: row exists at `confidence<2`. Treat as a hint, not a skip-discovery hit.
- `resource_not_in_store`: the local store doesn't have the resource the learning points at. The match validator couldn't classify entities — direct-fetch and re-evaluate.
- `cross_alias_match` (per-result): the row was taught under a different alias and matched the live query's canonical via `entity_lookups` (e.g., an "eq8" teach satisfying an "EQ Eight" recall). Trust the resource_id.
- `similar_shape_different_entity:<canonical>` (top-level): a structurally matching row exists but its canonical entity differs from the live query's. Treated as cold start; the warning carries the conflicting canonical as a hint, but the row is NOT promoted into Results.
- `ambiguous_alias` (top-level): a single query entity resolved to multiple canonicals (e.g., "comp" → Compressor + Glue Compressor). Surface the ambiguity from context before committing to a resource.
- `candidates_present` (top-level): the envelope carries a `candidates` section. Handle it via the candidates branch in Step 2 before anything else.
- `lookup_refresh_available` (top-level): an entity in the query has no lookup row yet, but synced data could provide one. Run `ableton-pp-cli sync` to refresh entity lookups.
- Top-level `no_learnings_for_query_family`: the table had no rows above the Jaccard floor. Pure cold start.

### Step 4: `teach &` after finalizing your response - always

Teaching is unconditional. After resolving a query the store could not answer, background-teach the final resource mapping - no call-count threshold, no judging whether it was "worth" learning. The teach is the anchor of the loop: it triggers playbook synthesis for a family without a playbook, and same-referent phrasings fold into one family so near-duplicate teaches do not fragment the store. Fire it after assembling your user-facing response but BEFORE emitting it, with a shell `&` so the call returns immediately. Pass the query the same way as recall — argv/MCP, or file-then-`$QUERY`. Do not splice the question into the command text:

```bash
QUERY=$(cat /path/to/question.txt)
ableton-pp-cli teach --query "$QUERY" --resource-type <type> --resource <id1> --resource <id2>
# (append shell `&` to background it)
```

Silent on success. Errors only land in `teach.log` under the resolved state dir. Teach the **most specific** resource - if the user asked a broad question and you walked through parent records to find the specific answer, teach the leaf id, not the parent. The CLI uses seeded `entity_lookups` for cross-alias resolution at recall time, so a teach under one alias (e.g., "dub echo") satisfies future queries under another alias (e.g., "tape echo", "Echo") automatically.

PII rule: teach the structural question with identifiers stripped - never include names, emails, phone numbers, account ids, or other personal identifiers in taught queries or notes. The CLI scans teach queries for obvious email/phone shapes and warns, but does not block; strip before teaching rather than relying on the warning.

### Step 5: playbooks - optional flags, automatic synthesis

You do not need to decide whether a session "deserves" a playbook: a teach on a family without one auto-synthesizes a `playbook_candidate` from the session's journal, and the next session judges it via confirm/reject. Attach explicit playbook flags only when you already hold choreography worth recording verbatim - workarounds the CLI didn't surface (silently-dropped flags, undocumented params, pagination tricks, payload gotchas). Prefer the **integrated one-call form** - record the resource learning and the playbook in the same `teach` invocation:

```bash
# Common case: record both the resource learning AND the playbook in one call.
QUERY=$(cat /path/to/question.txt)
ableton-pp-cli teach \
  --query "$QUERY" \
  --resource <id> \
  --playbook-file ~/playbooks/<shape>.json \
  --playbook-notes-file ~/playbooks/<shape>-notes.md
# (append shell `&` to background it)

# Alternate: playbook-only (no resource to record alongside).
QUERY=$(cat /path/to/question.txt)
ableton-pp-cli teach-playbook \
  --query "$QUERY" \
  --playbook-file ~/playbooks/<shape>.json \
  --notes-file ~/playbooks/<shape>-notes.md
```

Playbook files are JSON with `steps`, `entity_slots`, `expected_tool_calls`. Notes files are markdown carrying the gotchas verbatim. File-free callers (MCP-only agents) pass the same content inline: `--playbook-json` and `--playbook-notes` on the integrated `teach` form, `--playbook-json` and `--notes` on `teach-playbook`. On the integrated `teach` form, the playbook flags are optional - omit them entirely for a resource-only teach. On the standalone `teach-playbook` form, at least one of the playbook and notes flags must be set; both empty is rejected. Playbooks are keyed on the structural query family (entities stripped) so a recipe taught from one entity-shaped query applies to every other query of the same shape, with `slots_resolved` binding the live query's canonical at recall time.

When you DO find a playbook on a future recall, treat it as ground truth: replay the steps with `slots_resolved` substitutions, skip the discovery that the choreography already documents, and read `notes` before any step.

### Step 6: `playbook amend &` when your debug response identifies a correction

If your debug-protocol response identifies a concrete correction the notes or playbook should know — a workaround, an undocumented endpoint shape, a stale field name, observed schema drift, an empty-payload fallback — fire `playbook amend` BEFORE emitting your user-facing response. Same fire-and-forget posture as `teach`. Pass the query and note as argv/MCP arguments, or write each with a non-shell file tool and read them back (`QUERY=$(cat ...)`, `NOTE=$(cat ...)`). Do not interpolate either string into the command text:

```bash
QUERY=$(cat /path/to/question.txt)
NOTE=$(cat /path/to/note.txt)
ableton-pp-cli playbook amend \
  --query "$QUERY" \
  --add-note "$NOTE"
# (append shell `&` to background it)
```

What counts as worth amending: a behavior you OBSERVED this session that future-you would benefit from knowing. Examples worth amending:

- A workaround for a CLI surface that silently drops or misorders a flag.
- An undocumented endpoint shape (response wrapped in `{meta, results}`, payload nested two levels deeper than the docs claim).
- Observed schema drift (a field renamed, an index that shifted between seasons, a category label that the API now returns lower-cased).

What does NOT belong in notes:

- The year-specific or entity-specific answer to the user's question. That's the response, not a learning.
- Per-team / per-athlete / per-row data the playbook already retrieves at runtime.
- Statements that paraphrase what the existing notes already say.

The amend command appends to the family's existing notes with a timestamped marker (`[amend YYYY-MM-DDTHH:MMZ]: <text>`). Multiple amends accumulate; the audit trail is visible. If no playbook exists yet for the family, amend creates a notes-only one (so cold-start corrections still land).

#### PII discipline for amend notes

`playbook amend` notes are designed to potentially flow upstream as shared knowledge in future versions of the Printing Press. Keep them clean of user-identifying content so the upstream-contribution path stays open without retroactive scrubbing:

- **Do NOT embed** paths to user filesystems, personal API keys or tokens, user email addresses, user GitHub handles, or specific query histories tied to a single user.
- **Acceptable**: endpoint shapes, undocumented field names, API gotchas, observed schema drift, workarounds for CLI surfaces, generalizable pagination or retry tactics.

If a correction is only meaningful with user-specific context, it belongs in a personal note, not in the playbook amend.

### Measuring the loop

`ableton-pp-cli learnings stats` reports recall hit rate, teach-to-reuse, playbook resolution rate, and candidate confirm/reject counts from the local `learn_events` table. Rates are null until they have a denominator; everything stays on this machine. Use it to check whether the loop is earning its keep for this CLI.

### Disabling learning

- `--no-learn` on a single command short-circuits both `recall` and the `teach` write path. Use for deterministic agent flows or tests that must not be affected by accumulated learnings.
- `ABLETON_NO_LEARN=true` in the environment globally disables the pipeline.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
ableton-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
ableton-pp-cli feedback --stdin < notes.txt
ableton-pp-cli feedback list --json --limit 10
```

Entries are stored locally as `feedback.jsonl` under the resolved data dir. They are never POSTed unless `ABLETON_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `ABLETON_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

Write what *surprised* you, not a bug report. Short, specific, one line: that is the part that compounds.

## Output Delivery

Every command accepts `--deliver <sink>`. The output goes to the named sink in addition to (or instead of) stdout, so agents can route command results without hand-piping. Three sinks are supported:

| Sink | Effect |
|------|--------|
| `stdout` | Default; write to stdout only |
| `file:<path>` | Atomically write output to `<path>` as a private 0600 file (exclusive randomly named temp in the destination directory, then rename; a planted symlink is not followed). Binary-response commands write decoded payload bytes (not the base64 JSON envelope) and print a small JSON receipt on stdout; `--json`/`--csv` do not refuse when this sink is set. |
| `webhook:<url>` | POST the output body to the URL (`application/json`). Only `127.0.0.1`, `::1` or `localhost` URLs are accepted: this CLI never sends data to other hosts. Redirects are not followed; a 3xx response fails the delivery. |

Unknown schemes are refused with a structured error naming the supported set. Webhook failures, including redirects, return non-zero and log the URL and HTTP status on stderr.

## Named Profiles

A profile is a saved set of flag values, reused across invocations. Use it when a scheduled or recurring agent reuses the same saved flags while providing different input each run.

```
ableton-pp-cli profile save briefing --json
ableton-pp-cli --profile briefing tracks
ableton-pp-cli profile list --json
ableton-pp-cli profile show briefing
ableton-pp-cli profile delete briefing --yes
```

Explicit flags always win over profile values; profile values win over defaults. `agent-context` lists all available profiles under `available_profiles` so introspecting agents discover them at runtime.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage error (wrong arguments) |
| 3 | Resource not found |
| 5 | Live or Remote Script error: Live rejected the input, an ambiguous track/device name (candidates listed; use the number), a slot already holds a clip, AbletonPPExtras does not support the call, or a Remote Script is unreachable (the message names the script to select) |
| 6 | Partial failure (e.g. `chain apply` set only some parameters) |
| 10 | Config error |

## Argument Parsing

Parse `$ARGUMENTS`:

1. **Empty, `help`, or `--help`** → show `ableton-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

1. Install the MCP server:
   ```bash
   go install github.com/mvanhorn/printing-press-library/library/media-and-entertainment/ableton/cmd/ableton-pp-mcp@latest
   ```
2. Register with Claude Code:
   ```bash
   claude mcp add ableton-pp-mcp -- ableton-pp-mcp
   ```
3. Verify: `claude mcp list`

For an HTTP MCP client on this machine, `ableton-pp-mcp --transport http` requires `ABLETON_MCP_HTTP_TOKEN` set to at least 32 characters (callers send `Authorization: Bearer <token>`; `openssl rand -hex 32` prints 64 hex characters and meets that minimum), binds `127.0.0.1:7777` by default, and refuses any non-loopback `--addr`. Requests with no `Origin` header are accepted. Loopback origins are accepted; any other `Origin` is refused unless listed in `ABLETON_MCP_HTTP_ALLOWED_ORIGINS` (comma-separated `scheme://host[:port]` values). `ABLETON_MCP_READ_ONLY=1` drops mirrored write commands (local-write commands that only touch the CLI's own learn store stay) and makes the raw execute tool refuse state-changing calls.

## Direct Use

1. Check if installed: `which ableton-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   ableton-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `ableton-pp-cli <command> --help`.

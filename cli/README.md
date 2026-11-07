# Ableton Live CLI

**Control Ableton Live from the terminal by track and device name, with backups for note edits and a journal of every change.**

Drives Live through the AbletonOSC and AbletonMCP Remote Scripts plus a small companion script, so transport, mixer, clips, notes, devices, the browser and the Arrangement are all scriptable. `notes replace` backs up a clip before overwriting it, `journal revert` rolls back the CLI's own changes, and `arrangement build` lays out a song from scenes. Nothing leaves your machine.

## Install

The recommended path installs both the `ableton-pp-cli` binary and the `pp-ableton` agent skill (Claude Code, Codex, Cursor, Gemini CLI, GitHub Copilot, and other agents supported by the upstream [`skills`](https://github.com/vercel-labs/skills) CLI) in one shot:

```bash
npx -y @mvanhorn/printing-press-library install ableton
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install ableton --cli-only
```

For skill only — installs the skill into the same agents as the default command above, but skips the CLI binary (use this to update or reinstall just the skill):

```bash
npx -y @mvanhorn/printing-press-library install ableton --skill-only
```

To constrain the skill install to one or more specific agents (repeatable — agent names match the [`skills`](https://github.com/vercel-labs/skills) CLI):

```bash
npx -y @mvanhorn/printing-press-library install ableton --agent claude-code
npx -y @mvanhorn/printing-press-library install ableton --agent claude-code --agent codex
```

### Without Node (Go fallback)

If `npx` isn't available (no Node, offline), install the CLI directly via Go (requires Go 1.26.10 or newer):

```bash
go install github.com/mvanhorn/printing-press-library/library/media-and-entertainment/ableton/cmd/ableton-pp-cli@latest
```

This installs the CLI only — no skill.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/ableton-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

Install the CLI binary first. The installer writes binaries to a per-user managed bin directory by default: `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows.

```bash
npx -y @mvanhorn/printing-press-library install ableton --cli-only
```

Then install the focused Hermes skill.

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-ableton --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-ableton --force
```

Restart the Hermes session or gateway if the newly installed skill is not visible immediately.

## Install for OpenClaw
Install both the CLI binary and the focused OpenClaw skill. The installer defaults binaries to a per-user bin directory (`$HOME/.local/bin` on macOS/Linux, `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows):

```bash
npx -y @mvanhorn/printing-press-library install ableton --agent openclaw
```

Restart the OpenClaw session or gateway if the newly installed skill is not visible immediately.

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/ableton-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


```bash
go install github.com/mvanhorn/printing-press-library/library/media-and-entertainment/ableton/cmd/ableton-pp-mcp@latest
```

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS, `%APPDATA%\Claude\claude_desktop_config.json` on Windows):

```json
{
  "mcpServers": {
    "ableton": {
      "command": "ableton-pp-mcp"
    }
  }
}
```

</details>

### Remote MCP over HTTP

`ableton-pp-mcp --transport http` (or `PP_MCP_TRANSPORT=http`) serves streamable HTTP at `http://127.0.0.1:7777/mcp`. It refuses to start until `ABLETON_MCP_HTTP_TOKEN` is set to at least 32 characters, and every request must send `Authorization: Bearer <token>`. The command below prints 64 hex characters and meets that minimum. Plain HTTP is allowed only on a loopback `--addr`; any other bind, including `:7777` (every interface), also needs `--tls-cert` and `--tls-key`. Requests with no `Origin` header are accepted, as are loopback origins (`localhost`, `127.0.0.1`, or `[::1]`, any port). Any other `Origin` is refused unless it is listed in `ABLETON_MCP_HTTP_ALLOWED_ORIGINS`, a comma-separated list of `scheme://host[:port]` values with no path. That list is not a CORS policy; the server does not emit CORS headers.

```bash
export ABLETON_MCP_HTTP_TOKEN="$(openssl rand -hex 32)"
ableton-pp-mcp --transport http
```

Set `ABLETON_MCP_READ_ONLY=1` (exactly `1`) to drop mirrored commands that are not marked read-only and make the raw execute tool refuse state-changing calls. Commands marked local-write, which write only this CLI's own learn store, stay available.

## Authentication

No account or API key. The CLI talks only to Remote Scripts running inside Live on your own computer: AbletonOSC on UDP port 11000 (replies on 11001), the AbletonMCP script on TCP port 9877, and AbletonPPExtras on TCP port 9878. AbletonOSC and AbletonMCP are installed from their own projects into your User Library's Remote Scripts folder. AbletonPPExtras is optional (dB volume, rack chains, drum pads, automation, Arrangement clip add/delete and device delete use it); install it with `ableton-pp-cli scripts install --yes` and restart Live. Select each script in Live's Settings > Link, Tempo & MIDI with Input and Output set to None.

## Quick Start

```bash
# Checks the CLI itself before Live is involved.
ableton-pp-cli doctor --dry-run

# Confirms each Remote Script is loaded in Live and answering.
ableton-pp-cli doctor

# Shows tempo, transport and every track with its mixer state.
ableton-pp-cli status --agent

# Sets the song tempo to 92 BPM.
ableton-pp-cli tempo 92

# Reads the MIDI notes in the first clip on the Bass track.
ableton-pp-cli notes get Bass 1 --agent

```

## Unique Features

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

## Usage

Run `ableton-pp-cli --help` for the full command reference and flag list.

## Paths & environment variables

This CLI separates local files into four path kinds:

| Kind | Contents |
|------|----------|
| `config` | User-editable settings such as `config.toml` and saved profiles |
| `data` | Durable local data such as `data.db` |
| `state` | Runtime state such as persisted queries, jobs, and `teach.log` |
| `cache` | Unused: Live reads are never cached |

Each kind resolves independently. The ladder is:

1. Per-kind env var: `ABLETON_CONFIG_DIR`, `ABLETON_DATA_DIR`, `ABLETON_STATE_DIR`, or `ABLETON_CACHE_DIR`
2. `--home <dir>` for this invocation
3. `ABLETON_HOME` for a flat relocated root
4. XDG env vars: `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`
5. Platform defaults matching existing installs

For containers and agent sandboxes, prefer a single relocated root:

```bash
export ABLETON_HOME=/srv/ableton
ableton-pp-cli doctor
```

Under `ABLETON_HOME=/srv/ableton`, the four dirs resolve to `/srv/ableton/config`, `/srv/ableton/data`, `/srv/ableton/state`, and `/srv/ableton/cache`.

On Windows, set the same variable in PowerShell:

```powershell
$env:ABLETON_HOME = "C:\srv\ableton"
ableton-pp-cli doctor
```

Under `$env:ABLETON_HOME = "C:\srv\ableton"`, the four dirs resolve to `C:\srv\ableton\config`, `C:\srv\ableton\data`, `C:\srv\ableton\state`, and `C:\srv\ableton\cache`.

MCP servers do not receive CLI flags from the host. Put relocation in the host `env` block:

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

Precedence matters in fleets: an ambient per-kind variable such as `ABLETON_DATA_DIR` overrides an explicit `--home` for that kind. Use `ABLETON_HOME` or the per-kind variables for durable fleet relocation; treat `--home` as the weaker per-invocation lever.

Relocation is one-way. Unsetting `ABLETON_HOME` does not move files back to platform defaults, and `doctor` cannot find files left under a former root. Move the files manually before unsetting relocation variables.

Existing installs keep working because the platform-default rung matches the legacy layout. Run `ableton-pp-cli doctor --fail-on warn` to check path warnings in automation.

### Local data

Reads are saved locally by default: in `--data-source auto` mode every successful read is also written to the SQLite store (`data.db`) in the data directory. Live reads are never cached, because Live's state changes constantly. The learn journal lives under `learn/` in the state directory. `ableton-pp-cli doctor` prints the resolved paths, every store file, and the response-cache size.

None of it is encrypted. New files are created owner-only; beyond that the data relies on OS account permissions and full-disk encryption (FileVault, BitLocker, LUKS).

- **Erase it:** `ableton-pp-cli local-data purge` lists the store files, response cache, and learn journal it would delete; add `--yes` to delete them, or `--only store|cache|journal` to narrow it. Config and credentials are kept.
- **Run without a local copy:** set `ABLETON_NO_LOCAL_DATA=1`. Reads go to Live (`--data-source live`; `--data-source local` still reads an existing store), nothing new is written to the store, and `sync` and `workflow archive` refuse to run. Set it in an MCP host's `env` block and every command the server runs inherits it.
- **Choose the read source without flags:** `ABLETON_DATA_SOURCE=auto|live|local` sets the `--data-source` default; an explicit flag wins.

## Commands

### arrangement

Clips on the Arrangement timeline

- **`ableton-pp-cli arrangement add-clip`** - Create an empty MIDI clip on the Arrangement timeline (needs AbletonPPExtras)
- **`ableton-pp-cli arrangement clips`** - List a track's Arrangement clips with start, end and bar.beat positions
- **`ableton-pp-cli arrangement delete-clip`** - Delete an Arrangement clip (needs AbletonPPExtras)

### automation

Clip automation envelopes (needs AbletonPPExtras)

- **`ableton-pp-cli automation clear`** - Clear a clip's automation envelope for a device parameter
- **`ableton-pp-cli automation get`** - Read a clip's automation envelope for a device parameter
- **`ableton-pp-cli automation set`** - Write automation points into a clip's envelope from JSON

### browser

Live's browser (instruments, sounds, drums, effects, plugins)

- **`ableton-pp-cli browser index`** - Every loadable browser item (two levels deep), for 'sync' and 'device add'
- **`ableton-pp-cli browser list`** - List the items inside a browser folder, e.g. instruments or drums/Kits
- **`ableton-pp-cli browser tree`** - Show the browser's top-level categories and their folders

### clip

Work with Session clips

- **`ableton-pp-cli clip create`** - Create an empty MIDI clip in a slot
- **`ableton-pp-cli clip delete`** - Delete the clip in a slot
- **`ableton-pp-cli clip duplicate`** - Copy a clip to an empty slot
- **`ableton-pp-cli clip duplicate-loop`** - Double a clip's loop, copying its notes into the new half
- **`ableton-pp-cli clip fire`** - Launch a clip
- **`ableton-pp-cli clip get`** - Show all properties of a Session clip
- **`ableton-pp-cli clip import-audio`** - Create an audio clip from a local audio file (Live 12.0.5 or newer)
- **`ableton-pp-cli clip set`** - Change one clip property (the change is journaled)
- **`ableton-pp-cli clip stop`** - Stop a playing clip
- **`ableton-pp-cli clip to-arrangement`** - Copy a Session clip onto the same track in the Arrangement at a position

### clips

The Session clip grid

- **`ableton-pp-cli clips`** - Show every track's clip slots with clip names and lengths

### device

Devices on a track

- **`ableton-pp-cli device delete`** - Delete a device from a track (needs the AbletonPPExtras script)
- **`ableton-pp-cli device disable`** - Turn a device off (bypass)
- **`ableton-pp-cli device enable`** - Turn a device on
- **`ableton-pp-cli device list`** - List a track's devices with their type and class
- **`ableton-pp-cli device load`** - Load an instrument, effect or plugin onto a track by its browser URI

### drums

Drum Racks

- **`ableton-pp-cli drums load`** - Load a Drum Rack and a kit from the browser onto a track
- **`ableton-pp-cli drums pads`** - List a Drum Rack's filled pads (needs the AbletonPPExtras script)

### health

Remote Script health

- **`ableton-pp-cli health`** - Check which Remote Scripts answer and their versions

### locator

Arrangement locators (cue points)

- **`ableton-pp-cli locator add`** - Add a named locator at a beat or bar.beat (renames an existing one at the same spot)
- **`ableton-pp-cli locator delete`** - Delete a locator
- **`ableton-pp-cli locator jump`** - Move the playhead to a locator, by name or number
- **`ableton-pp-cli locator list`** - List locators with their beat and bar.beat positions
- **`ableton-pp-cli locator next`** - Jump to the next locator
- **`ableton-pp-cli locator prev`** - Jump to the previous locator
- **`ableton-pp-cli locator rename`** - Rename a locator

### message

Show a message in Live's status bar

- **`ableton-pp-cli message`** - Show a short message in Live's status bar

### midimap

Map a MIDI CC to a device parameter

- **`ableton-pp-cli midimap`** - Make a MIDI CC control a device parameter

### notes

MIDI notes in Session clips

- **`ableton-pp-cli notes add`** - Add notes to a MIDI clip from JSON (keeps existing notes)
- **`ableton-pp-cli notes clear`** - Remove notes from a MIDI clip, optionally only within a pitch and time range
- **`ableton-pp-cli notes get`** - Read every note in a MIDI clip as JSON (pitch, start, duration, velocity, mute)

### param

Device parameters

- **`ableton-pp-cli param list`** - List a device's parameters with value, range and display text
- **`ableton-pp-cli param set`** - Set a device parameter by name or number (range-checked and journaled)

### play

Start playback

- **`ableton-pp-cli play`** - Start Live's transport from the current position

### rack

Instrument, Audio Effect and Drum Racks

- **`ableton-pp-cli rack chains`** - List a rack's chains with volume, pan, mute and solo (needs AbletonPPExtras)
- **`ableton-pp-cli rack inspect`** - Show a rack's nested devices and which macros are mapped
- **`ableton-pp-cli rack map-macro`** - Map every nested device's Dry/Wet (or Mix/Amount) to Macro 1 and name it

### record

Recording controls

- **`ableton-pp-cli record capture`** - Capture MIDI you just played into a new clip
- **`ableton-pp-cli record session`** - Turn Session record on or off
- **`ableton-pp-cli record trigger`** - Trigger Session record (like pressing the Session record button)

### redo

Redo the last undone change in Live

- **`ableton-pp-cli redo`** - Redo the last undone operation

### scene

Session scenes

- **`ableton-pp-cli scene create`** - Create a scene
- **`ableton-pp-cli scene delete`** - Delete a scene
- **`ableton-pp-cli scene duplicate`** - Duplicate a scene with its clips
- **`ableton-pp-cli scene fire`** - Launch a scene
- **`ableton-pp-cli scene fire-selected`** - Launch the selected scene and select the next one
- **`ableton-pp-cli scene list`** - List scenes with name, tempo, time signature and color
- **`ableton-pp-cli scene set`** - Change a scene's name, color, tempo or time signature

### snapshot

Whole-project snapshots

- **`ableton-pp-cli snapshot`** - List stored project snapshots (same as 'snapshot list'); Live is not contacted. 'snapshot save' reads the set

### song

Song-level settings and transport actions

- **`ableton-pp-cli song continue`** - Resume playback from where it stopped
- **`ableton-pp-cli song get`** - Read a song property
- **`ableton-pp-cli song seek`** - Move the playhead to a beat or bar.beat position, or jump by a number of beats
- **`ableton-pp-cli song set`** - Change a song property (the change is journaled)
- **`ableton-pp-cli song stop-clips`** - Stop all playing clips, or only one track's clips with --track
- **`ableton-pp-cli song tap-tempo`** - Tap the Tap Tempo button once

### status

Session overview: tempo, signature, transport and every track

- **`ableton-pp-cli status`** - Show tempo, time signature, play state, loop and every track with its mixer state

### stop

Stop playback

- **`ableton-pp-cli stop`** - Stop Live's transport

### track

Work with one track: mixer, routing, create, delete

- **`ableton-pp-cli track arm`** - Arm or disarm a track for recording
- **`ableton-pp-cli track color`** - Set a track's color from Live's palette (0-69)
- **`ableton-pp-cli track create`** - Create a MIDI, audio or return track
- **`ableton-pp-cli track delete`** - Delete a track
- **`ableton-pp-cli track duplicate`** - Duplicate a track with its devices and clips
- **`ableton-pp-cli track get`** - Show a track's mixer state, devices and clip slots
- **`ableton-pp-cli track meter`** - Current output levels for every track
- **`ableton-pp-cli track mute`** - Mute or unmute a track
- **`ableton-pp-cli track pan`** - Set a track's pan from -1 (left) to 1 (right)
- **`ableton-pp-cli track rename`** - Rename a track
- **`ableton-pp-cli track routing`** - Show a track's input/output routing, the available routes and its monitoring state
- **`ableton-pp-cli track send`** - Set a send level (send A = 1) as a 0-1 knob value
- **`ableton-pp-cli track set-routing`** - Change a track's input/output routing or monitoring
- **`ableton-pp-cli track solo`** - Solo or unsolo a track
- **`ableton-pp-cli track volume`** - Set a track's volume as a 0-1 fader value, or in dB (-inf to +6; --db needs AbletonPPExtras)

### tracks

Every track with its mixer state

- **`ableton-pp-cli tracks`** - List every track: number, name, type, volume (0-1 fader, plus dB with AbletonPPExtras), pan, mute, solo, arm and sends

### undo

Undo the last change in Live

- **`ableton-pp-cli undo`** - Undo the last operation in Live's undo history (includes your own mouse edits)

### view

What is selected and shown in Live

- **`ableton-pp-cli view get`** - Show the selected track, scene, clip and device
- **`ableton-pp-cli view select`** - Select a track, scene, clip or device
- **`ableton-pp-cli view show`** - Switch Live's main window to the Arrangement view (switching to Session is not supported)


### Safety and composite commands

Hand-written commands that combine several Live calls, keep local backups and history, or stream changes.

**Arrangement**
- **`ableton-pp-cli arrangement build`** - Lay out a whole song in the Arrangement from Session scenes (`--plan`, repeated `--section`, or `--all-scenes`), with a named locator at each section. Needs the AbletonMCP script.

**Device chains**
- **`ableton-pp-cli chain save <track> <name>`** - Save a track's chain of devices with their settings (needs the browser index and the AbletonMCP script)
- **`ableton-pp-cli chain apply <name> <track>`** - Rebuild a saved device chain on a track
- **`ableton-pp-cli chain list [name]`** - List saved device chains

**Devices**
- **`ableton-pp-cli device add <track> <name>`** - Load an instrument or effect onto a track by its browser name, such as Echo or Drift. Needs the browser index (run `sync --resources browser` once) and the AbletonMCP script.

**Journal**
- **`ableton-pp-cli journal list`** - See every change this CLI made, with before and after values
- **`ableton-pp-cli journal revert [id...]`** - Roll back specific changes this CLI made, by id or with `--last N`

**Mixer scenes**
- **`ableton-pp-cli mix save <name>`** - Save every track's volume, pan, mute, solo and sends under a name
- **`ableton-pp-cli mix recall <name>`** - Restore a saved mixer state by track name
- **`ableton-pp-cli mix list [name]`** - List saved mixer states

**Notes**
- **`ableton-pp-cli notes replace <track> <slot>`** - Overwrite a clip's MIDI notes from JSON with an automatic backup
- **`ableton-pp-cli notes restore <backup-id>`** - Put back a clip's notes from a backup (see `notes backups`); `--track`/`--slot` send it elsewhere
- **`ableton-pp-cli notes backups`** - List saved note backups (filter with `--track`, `--slot`, `--limit`)

**Remote Script**
- **`ableton-pp-cli scripts install`** - Show the plan for installing the AbletonPPExtras Remote Script into your Ableton User Library; `--yes` copies it (a differing `__init__.py` is kept as `.bak`)

**Snapshots**
- **`ableton-pp-cli snapshot save <label>`** - Save the whole project's state locally under a label
- **`ableton-pp-cli snapshot diff <snapshot> [other-snapshot]`** - Compare two saved snapshots, or a snapshot against the live set with `--live`: tracks, mixer, devices, parameters, clips and notes. Reading the live set needs the AbletonMCP script.
- **`ableton-pp-cli snapshot list`** - List saved project snapshots

**Tempo and monitoring**
- **`ableton-pp-cli tempo [bpm]`** - Show the tempo, or set it to a BPM between 20 and 999
- **`ableton-pp-cli watch <what>...`** - Stream live changes (tempo, playing, beat, selection, a track's mixer, a parameter) as NDJSON


### Self-learning loop

This CLI caches per-question discovery so repeat queries skip the walk and structurally similar queries get answered via entity substitution. The loop also self-captures: every invocation is journaled locally, and failed-flag corrections plus fresh teaches surface as candidates on the next `recall` for confirm/reject judgment. Agents call `recall` before discovery and fire `teach &` after answering. See the `## Automatic learning` section in `SKILL.md` for the full protocol.

- **`ableton-pp-cli recall <query>`** - Look up cached resources for a query before running discovery
- **`ableton-pp-cli teach`** - Record a query -> resource mapping (silent on success, safe to background with `&`)
- **`ableton-pp-cli learnings list`** - Inspect taught rows
- **`ableton-pp-cli learnings forget <query>`** - Undo a teach
- **`ableton-pp-cli learnings candidates`** - List auto-captured candidates awaiting confirm/reject
- **`ableton-pp-cli learnings stats`** - Local loop metrics: recall hit rate, teach-to-reuse, playbook resolution, candidate counts
- **`ableton-pp-cli teach-pattern`** - Install a query/resource template up front
- **`ableton-pp-cli teach-lookup`** - Add an entity mapping (e.g. "eq8" for EQ Eight) for pattern substitution

Pass `--no-learn` or set `ABLETON_NO_LEARN=true` to disable the loop for deterministic flows.

The local store's schema version stamp is one-way: once this version of `ableton-pp-cli` opens the database, older binaries refuse it with a version error — upgrade the binary rather than downgrading.

## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
ableton-pp-cli tracks

# JSON for scripting and agents
ableton-pp-cli tracks --json
# Filter to specific fields
ableton-pp-cli tracks --json --select name,volume,mute

# Dry run — show what would change without touching Live
ableton-pp-cli tempo 120 --dry-run

# Agent mode — JSON + compact + no prompts in one flag
ableton-pp-cli tracks --agent
```

`--csv` prefixes a leading `'` on cells and header keys that start with `=`, `+`, `-`, `@`, a tab, or a line break (including fullwidth `＝＋－＠`) so a spreadsheet does not run them as formulas. Plain signed numbers such as `-5` and `+2.5e3` are left unchanged.

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select <field>[,<field>...]` returns only fields you need
- **Previewable** - `--dry-run` never changes Live; composite commands (arrangement build, mix, device add, chain, journal revert) read Live to build the plan
- **No blind retries** - leave `--ignore-missing` off: a 404 here is usually a misspelled track or device name. Check `journal list` before retrying a write
- **Explicit confirmation** - `--agent` does not imply `--yes`; pass `--yes` separately only after the target, arguments, and side effects are clear
- **Piped input** - write commands can accept structured input when their help lists `--stdin`
- **Offline-friendly** - sync/search commands can use the local SQLite store when available
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `2` usage error, `3` not found (close matches listed), `5` Live or Remote Script error (rejected input, ambiguous name, occupied slot, Remote Script unreachable), `6` partial failure, `10` config error (e.g. empty browser index).

## Health Check

```bash
ableton-pp-cli doctor
```

Checks the local configuration and whether AbletonOSC, AbletonMCP and AbletonPPExtras answer on 127.0.0.1.

## Configuration

Run `ableton-pp-cli doctor` to see the resolved config, data, state, and cache directories. The platform-default config path is `~/.config/ableton-pp-cli/config.toml` on macOS/Linux and `%USERPROFILE%\.config\ableton-pp-cli\config.toml` on Windows; `--home`, `ABLETON_HOME`, and per-kind env vars can relocate it.

## Troubleshooting
**Not found errors (exit code 3)**
- Check the track, device, scene or clip name or number; the error lists close matches
- `ableton-pp-cli tracks`, `scene list`, `device list <track>` and `clips` show what exists

### API-specific
- **no reply from AbletonOSC** — Open Live, then select AbletonOSC as a Control Surface in Settings > Link, Tempo & MIDI with Input and Output set to None. Close any open Live menu; AbletonOSC pauses while a menu is open.
- **cannot listen on 127.0.0.1:11001** — Another OSC app (TouchOSC, a pylive script) is using the reply port. Close it, then run ableton-pp-cli doctor.
- **cannot reach the AbletonMCP Remote Script on 127.0.0.1:9877** — Select AbletonMCP as a Control Surface in Live's settings. The browser, device load/add, drums load, rack inspect/map-macro, arrangement clips/build, clip to-arrangement/import-audio, locator add, view show, snapshot save/diff --live and chain apply need it.
- **cannot reach AbletonPPExtras on 127.0.0.1:9878** — Run ableton-pp-cli scripts install, restart Live, and select AbletonPPExtras as a Control Surface.
- **track name matches more than one track** — Use the track number instead, for example 3, or rename one of the tracks with ableton-pp-cli track rename.

## Sources & Inspiration

This CLI was built by studying these projects and resources:

- [**ableton-mcp**](https://github.com/ahujasid/ableton-mcp) — Python (3280 stars)
- [**AbletonOSC**](https://github.com/ideoforms/AbletonOSC) — Python (839 stars)
- [**ableton-live-mcp-server**](https://github.com/Simon-Kansara/ableton-live-mcp-server) — Python (396 stars)
- [**ableton-mcp-extended**](https://github.com/uisato/ableton-mcp-extended) — Python (280 stars)
- [**pylive**](https://github.com/ideoforms/pylive) — Python
- [**ableton-osc-mcp**](https://github.com/nozomi-koborinai/ableton-osc-mcp) — Go

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)

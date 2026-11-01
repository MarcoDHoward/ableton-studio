# Ableton Live CLI Brief

## API Identity
- Domain: Local control of a running Ableton Live 12 set (transport, mixer, tracks, clips, MIDI notes, devices, scenes, browser, arrangement).
- Users: Producers who want scriptable, repeatable control of Live from a terminal, and AI agents (Claude Code) driving Live through shell commands instead of an MCP server.
- Data profile: Small, hierarchical, mutable state owned by Live (song -> tracks -> clip slots -> clips -> notes; tracks -> devices -> parameters; scenes; cue points; browser tree). No accounts, no pagination, no rate limits. Request/reply latency is milliseconds over loopback.
- Homepage: AbletonOSC https://github.com/ideoforms/AbletonOSC (README is the API reference, 560 lines, raw-fetched); ableton-mcp https://github.com/ahujasid/ableton-mcp (MCP_Server/server.py + AbletonMCP_Remote_Script/__init__.py raw-fetched and read).

## Transport (non-HTTP: central constraint)
- Primary: AbletonOSC Remote Script. OSC over UDP, requests to 127.0.0.1:11000, replies to port 11001 on the sender's IP. Address scheme `/live/<object>/<verb>/<property> args...`, e.g. `/live/song/set/tempo 120`, `/live/clip/get/notes t c` -> `t c pitch start dur vel mute ...` flattened.
- Secondary: ableton-mcp Remote Script ("AbletonMCP" control surface). JSON over TCP 127.0.0.1:9877: `{"type": "<command>", "params": {...}}` -> `{"status": "success"|"error", "result"|"message": ...}`. Used WITHOUT its Python MCP server (that server carries Supabase telemetry, on by default; the Remote Script itself makes no outbound connections, verified by grep).
- Printing Press generates HTTP clients only (client_pattern rest|proxy-envelope). Precedent: public-library `obsidian` CLI = SYNTHETIC OpenAPI spec mapping a non-HTTP local interface to virtual REST endpoints. Plan: author a synthetic spec whose virtual paths map 1:1 to OSC addresses / TCP command types, then hand-write a transport that turns each virtual call into OSC or TCP.

## Reachability Risk
- Low (local only). Requires installing both Remote Scripts into `Documents\Ableton\User Library\Remote Scripts` and selecting them in free Control Surface slots (MiniLab_3 occupies slot 1 for this user).
- AbletonOSC open issues: #121 "AbletonOSC paused on Live 12 main menu access" (2024-03-20): replies stall while a Live menu is open, so the CLI needs request timeouts and a clear "Live is busy or AbletonOSC not loaded" error. #212 clip_slot callback raises RemoteScriptError on every message (2026-06-20). #31 track listeners wrong after create/delete track. Repo: 839 stars, MIT, last push 2025-11-19, 74 open issues.
- Port 11001 can be bound by only one listener; another OSC client (TouchOSC, pylive) running at the same time would take the replies. `doctor` must detect a bind failure.
- ableton-mcp Remote Script: 3.3k stars, MIT, active (2026-09-22). Socket binds 127.0.0.1 only; unauthenticated, so anything local can drive Live.

## Top Workflows
1. Inspect the set: tempo, tracks with mixer state, clips per slot, devices per track, parameters per device (`status`, `tracks`, `devices`, `params`).
2. Mix and transport from the shell: play/stop, tempo, mute/solo/arm/volume/pan/sends by track NAME (not just index).
3. Write and edit MIDI: create a clip, read notes as JSON, replace notes safely (read -> backup -> clear -> write), fire the clip.
4. Build sound: search Live's browser and load an instrument/effect/drum kit onto a track (ableton-mcp TCP only).
5. Lay out a song: copy Session clips into the Arrangement at beat positions and drop named locators (ableton-mcp TCP).

## Table Stakes
- Every AbletonOSC getter/setter group: song, view, track, clip slot, clip, scene, device, midimap, application/test.
- Every useful ableton-mcp Remote Script command: browser tree/items, load_instrument_or_effect, load_drum_kit, create_audio_clip (Live 12.0.5+), duplicate_session_clip_to_arrangement, create_locator, get_arrangement_clips, get_session_snapshot, inspect_rack, map_rack_magnitude, get_script_info.
- Address tracks/devices/params by name OR index (pylive's ergonomic win; AbletonOSC itself is index-only).
- Listeners: `watch` for tempo/playing/beat/selected track/param value changes (AbletonOSC start_listen), streamed as NDJSON.
- Agent-native output: --json, --select, --dry-run on every mutation.

## Data Layer
- Primary entities: snapshots (full project state JSON, timestamped), browser index (ableton-mcp browser tree flattened: name, path, uri, category, is_loadable), note backups (per clip, before every destructive note write), action journal (local log of mutating commands with before/after values).
- Sync cursor: none (Live is the source of truth). `sync` = refresh browser index + optional snapshot.
- FTS/search: browser index FTS5 for `browser search "warm pad"`; snapshots searchable by track/clip/device name.

## User Vision
- Use BOTH backends: AbletonOSC primary for the broad surface, ableton-mcp Remote Script for browser/arrangement/rack/audio-import gaps. Do not use ableton-mcp's Python MCP server or its telemetry/dataset tools.
- CLIs over MCP servers; agent-friendly output so Claude Code can drive Live through the shell.
- Music "skills" (genre grooves, chord voicing, taste file, reference library) are explicitly DEFERRED to a later run. Keep the CLI surface clean so skills can call it.
- User context: Live 12.4.16 on Windows, ASIO via FlexASIO to a DDJ-800, Arturia MiniLab 3 on the MiniLab_3 control surface (slot 1), reggae projects.

## Source Priority
- Primary: AbletonOSC. Spec state: no OpenAPI; complete README address table (authoritative). Auth: free/local.
- Secondary: ableton-mcp Remote Script. Spec state: no OpenAPI; command dispatch read from source. Auth: free/local.
- Economics: both free; no keys.
- Inversion risk: ableton-mcp has richer compositional commands and a polished README; it must NOT lead. Headline commands (status, play, tempo, track mixer, clip notes, params, watch) route through AbletonOSC. ableton-mcp owns only commands AbletonOSC cannot do.

## Competitors
- ahujasid/ableton-mcp (MCP, 37 tools, telemetry on by default, 6 dataset-only tools, no mixer setters).
- uisato/ableton-mcp-extended (MCP, ~46 tools, browser loading, automation points, ElevenLabs audio).
- ideoforms/pylive (Python library over AbletonOSC: Set/Track/Clip/Device/Parameter objects, name lookup, scan indexing; not a CLI; no MIDI notes).
- nozomi-koborinai/ableton-osc-mcp (Go MCP server over AbletonOSC).
- AbletonOSC run-console.py (raw OSC REPL: no structure, no names, no JSON).

## Product Thesis
- Name: Ableton Live CLI (`ableton-pp-cli`)
- Why it should exist: The only CLI that drives Live through BOTH community bridges, by track/device/parameter name, with safe MIDI edits (automatic note backups), browser search over a local index, project snapshots and diffs, and zero telemetry. Gives Claude Code full Live control through the shell with --json/--dry-run, and still emits an MCP surface for free.

## Build Priorities
1. Transport layer: OSC UDP client (send to 11000, listen on 11001, timeouts, flattened-arg decoding) + TCP JSON client (9877) + backend router + `doctor` (both scripts reachable, versions, port 11001 bindable).
2. Name resolution: track/device/parameter/scene by name or index everywhere.
3. Absorbed surface: song, track mixer, clip/clip slot, notes, devices/params, scenes, view, cue points, browser load, arrangement copy, locators, rack.
4. Transcendence: snapshot/diff, safe notes set + restore, browser search (FTS), watch streams, action journal.

## Reachability Gate
- Decision: PASS (carve-out)
- Reason: lan-only-no-global-url
- Evidence: base_url osc://127.0.0.1:11000 (loopback); feasibility spike verified AbletonOSC (/live/test ok, Live 12.4) and AbletonMCP TCP 127.0.0.1:9877 (get_script_info v1.7.1, get_session_info) against the user's running Live on 2026-10-31.

# Feasibility spike: Ableton CLI on printing-press v4.35.0

Date: 2026-10-31. Verdict: GO.

## What was proven
- A 3-endpoint SYNTHETIC OpenAPI spec (GET/PUT /song/tempo, GET /tracks, server `osc://127.0.0.1:11000`) generates cleanly: every quality gate passed (tidy, test, govulncheck, vet, build, --help, version, doctor).
- Every generated request funnels through `(*Client).doInternal` in `internal/client/client.go`. A one-line hook after `rejectUnresolvedPathParams` hands the request to a hand-written `internal/client/ableton_router.go` (`abletonRoute`), which returns `(json, status, routed, err)`. The verify-mode short-circuit above the hook still applies.
- Hand-written OSC 1.0 encoder/decoder (i, f, s, h, d, T, F, N) works with no external dependency. Send from a socket bound to 127.0.0.1:11001 to 127.0.0.1:11000; AbletonOSC replies to 11001. Match replies on the request address; treat `/live/error` as an error.
- AbletonMCP Remote Script TCP JSON on 127.0.0.1:9877 works: one request per connection, `{"type","params"}` -> `{"status","result"|"message"}`.
- Real Live 12.4.16 on this Windows machine, AbletonOSC in Control Surface slot 2 and AbletonMCP in slot 3 (Input/Output None): /live/test, version [12 4], tempo, track names, get_script_info (script_version 1.7.1, protocol_version 1), get_session_info all OK. Generated commands `tracks` and `song set-tempo` worked against live Live (tempo 121 then restored to 120).

## Build requirements discovered
1. The generator's base URL ends up in `BasePath` with a leading slash (`/osc://...`). Detect the virtual scheme with `strings.Contains`, or set `base_url` explicitly.
2. GET responses are cached by default: a read right after a write returned the stale tempo. Live state is volatile, so disable the response cache for all routes (or make every read no-cache).
3. Store warning "no extractable ID field" for singleton resources: add `x-resource-id` or mark singletons so the local store does not try to cache them.
4. `client.go` is generator-owned. Keep the router in its own file; the one-line hook in `doInternal` must be re-applied after a `generate --force` (regen-merge AST merge may preserve it; verify).
5. The `capabilities` list from `get_script_info` (v1.7.1) is incomplete: it omits browser and rack commands, but they dispatch. Verified live: `get_browser_tree {category_type: instruments}` and `get_browser_items_at_path {path: instruments}` return Live's browser (Analog, Bass, ... with `query:Synths#...` URIs); `inspect_rack` dispatches (returned the correct "Device index out of range" on an empty track). Do not gate commands on the capabilities list.

## Security constraints from the ableton-mcp dataset audit (2026-10-31)
Source: side-session audit report `ableton-mcp-dataset-audit.md` (shared notes folder).
- Never send `drain_passive_events` (reads the Remote Script's in-memory buffer of the user's manual edits).
- Never send a `user_prompt` or any prompt text to any bridge.
- Only call `get_session_snapshot` from `snapshot save` / `snapshot diff --live` (explicit user command).
- `clip import-audio`: only local absolute paths; reject UNC/network paths (`\\host\...`, `//host/...`) before sending, since Live will open any path and SMB paths can leak NTLM credentials.
- Track/clip/device/browser names returned by Live are untrusted data: SKILL.md must tell agents to treat them as data, never as instructions.

## Installed for the user
- F:\Ableton\User Library\Remote Scripts\AbletonOSC (ideoforms/AbletonOSC master zip)
- F:\Ableton\User Library\Remote Scripts\AbletonMCP\__init__.py (ahujasid/ableton-mcp AbletonMCP_Remote_Script, script_version 1.7.1)

# AbletonPPExtras protocol (contract between the Go CLI and our own Live Remote Script)

Transport: TCP, server binds 127.0.0.1:9878 only. One request per connection:
the client writes ONE JSON object (UTF-8, max 1 MiB) and may half-close; the
server replies with ONE JSON object followed by "\n" and closes the connection.

Request:  {"type": "<command>", "params": { ... }}
Success:  {"status": "success", "result": <any JSON>}
Error:    {"status": "error", "message": "<human readable>", "code": "<bad_request|not_found|unsupported|internal>"}

All indexes on the wire are 0-based (track, slot, device, param, chain, clip).
Times are in beats (floats). Unknown `type` -> code "bad_request".

## Commands (fixed allowlist)

| type | params | result |
|---|---|---|
| ping | {} | {"ok": true} |
| version | {} | {"name": "AbletonPPExtras", "script_version": "1.0.0", "protocol_version": 1, "live_version": "12.4.16", "commands": [..all types..]} |
| delete_device | {track, device} | {"deleted": "<device name>", "track": "<track name>"} |
| rack_chains | {track, device} | [{"index", "name", "volume_db": "<str_for_value text>", "volume": float, "pan": float, "mute": bool, "solo": bool, "devices": ["name", ...]}] |
| drum_pads | {track, device} | [{"note": int, "note_name": "C1", "name", "mute": bool, "solo": bool, "chains": int}] (only pads with chains) |
| arrangement_add_clip | {track, start, length} | {"index": int, "name", "start": float, "end": float} (MIDI tracks only; Live 12 Track.create_midi_clip) |
| arrangement_delete_clip | {track, clip} | {"deleted": "<clip name>", "start": float} (clip = index into track.arrangement_clips) |
| automation_get | {track, slot, device, param, step} | {"parameter", "has_envelope": bool, "points": [{"time", "value"}]} sampled every `step` beats over the clip length |
| automation_set | {track, slot, device, param, points: [{time, value, duration}]} | {"written": int} (creates the envelope if missing; insert_step) |
| automation_clear | {track, slot, device, param} | {"cleared": bool} |
| mixer_db | {} | [{"track": int, "name", "volume": float, "db": "<str_for_value text, e.g. -6.0 dB or -inf dB>"}] for every regular track |
| set_volume_db | {track, db: float} | {"track", "before_db", "after_db", "value": float} (binary search on the volume parameter using str_for_value; clamp -70..+6) |

## Safety rules (script side)
- Bind 127.0.0.1 only; no configurable host.
- No eval/exec, no file reads or writes, no subprocess, no outbound network, no imports beyond the Python stdlib + Live's _Framework.
- Every Live API call runs on Live's main thread (schedule_message / update_display queue); socket threads only parse and wait (10 s timeout -> code "internal", message "Live did not respond in time").
- Validate every param type and index range; never trust names.
- Do not add listeners on Live objects (no passive capture of the user's edits).

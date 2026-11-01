# Ableton Live CLI: Absorb Manifest

Sources (confirmed priority): 1) AbletonOSC (OSC/UDP 11000, primary), 2) AbletonMCP Remote Script by ahujasid (JSON/TCP 9877, secondary), 3) our own Remote Script `AbletonPPExtras`, written from scratch for this CLI (JSON/TCP 127.0.0.1:9878), only for capabilities neither installed script provides. User decision 2026-10-31: keep both installed scripts; build any NEW capability ourselves instead of installing more third-party code (no uisato script).

Tools surveyed: ahujasid/ableton-mcp (37 tools, source read), uisato/ableton-mcp-extended (46 tools, source read), ideoforms/AbletonOSC README (all address tables), ideoforms/pylive, nozomi-koborinai/ableton-osc-mcp, ableton-osc-mcp (PyPI, 49 tools), Simon-Kansara/ableton-live-mcp-server (1 tool + OSC daemon), remix-mcp, abletonosc-client (PyPI), AbletonOSC run-console.py, glincker/ableton-skills (skills, deferred).

Deliberately excluded: ahujasid dataset/telemetry tools (set_dataset_consent, submit_intent, rate_last_action, prefer_candidate, reject_last_action, record_audition) and the `user_prompt` argument; uisato ElevenLabs audio generation (external paid service); uisato navigate_device_preset (no clean Live API; would need UI scripting); uisato XY mouse controller (UI automation).

## Absorb Manifest

### Absorbed (match or beat everything that exists)
| # | Feature | Best Source | Our Implementation | Added Value |
|---|---------|-------------|-------------------|-------------|
| 1 | Bridge health check | ahujasid get_script_info; AbletonOSC /live/test | ableton-pp-cli doctor | Checks all three scripts, versions, port 11001 bindability, MiniLab slot untouched |
| 2 | Session overview | ahujasid get_session_info | ableton-pp-cli status | Tempo, signature, transport, tracks with mixer state in one call; --json |
| 3 | Play / stop / continue | AbletonOSC song start/stop/continue; ahujasid start/stop_playback | ableton-pp-cli play | Also `stop`, `song continue`; --dry-run |
| 4 | Stop all clips | AbletonOSC /live/song/stop_all_clips | ableton-pp-cli song stop-clips | Per-track variant via --track |
| 5 | Tempo get/set, tap tempo | AbletonOSC song tempo, tap_tempo; ahujasid set_tempo | ableton-pp-cli tempo | Range-validated, journaled |
| 6 | Song properties get/set (metronome, loop, loop start/length, time signature, groove, clip trigger quantization, MIDI recording quantization, record mode, overdub, punch in/out, back to arranger, nudge, root note, scale) | AbletonOSC song get/set | ableton-pp-cli song get | `song set <prop> <value>`; enum names instead of raw ints |
| 7 | Seek / jump by beats | AbletonOSC current_song_time, jump_by; uisato set_song_time | ableton-pp-cli song seek | Accepts beats or bar.beat |
| 8 | Undo / redo | AbletonOSC song undo/redo, can_undo/can_redo | ableton-pp-cli undo | `redo`; reports can_undo |
| 9 | Recording: session record, trigger session record, capture MIDI | AbletonOSC | ableton-pp-cli record | `record session on/off`, `record trigger`, `record capture` |
| 10 | Locators list | AbletonOSC get/cue_points; uisato get_cue_points | ableton-pp-cli locator list | Bar.beat display |
| 11 | Locator add at beat | ahujasid create_locator; uisato create_cue_point | ableton-pp-cli locator add | Idempotent (renames existing at same beat) |
| 12 | Locator jump / next / prev / rename / delete | AbletonOSC cue_point jump, jump_to_next/prev_cue, set/name, add_or_delete; uisato delete/jump | ableton-pp-cli locator jump | `locator next/prev/rename/delete`; delete seeks then toggles |
| 13 | Tracks list with mixer state | AbletonOSC song track_data bulk query | ableton-pp-cli tracks | One bulk call; name, type, volume dB, pan, mute, solo, arm, sends |
| 14 | Track detail (devices, clip slots) | ahujasid get_track_info | ableton-pp-cli track get | By name or index |
| 15 | Create MIDI / audio / return track | AbletonOSC song create_*; ahujasid create tracks | ableton-pp-cli track create | --name, --index |
| 16 | Delete / duplicate track | AbletonOSC song delete_track, duplicate_track; uisato delete_track | ableton-pp-cli track delete | `track duplicate`; delete requires --yes |
| 17 | Rename / color track | AbletonOSC track set name/color/color_index; ahujasid set_track_name | ableton-pp-cli track rename | `track color` |
| 18 | Mixer: volume, pan, mute, solo, arm, sends by name | AbletonOSC track setters; uisato set_track_volume/panning | ableton-pp-cli track volume | `track pan/mute/solo/arm/send`; dB or 0-1 input |
| 19 | Routing and monitoring | AbletonOSC track routing types/channels, monitoring state | ableton-pp-cli track routing | Lists available routes |
| 20 | Output meters | AbletonOSC track output_meter_* | ableton-pp-cli track meter | All tracks at once |
| 21 | Clip create / delete / duplicate to slot | AbletonOSC clip_slot create/delete/duplicate_clip_to; ahujasid | ableton-pp-cli clip create | `clip delete/duplicate` |
| 22 | Fire / stop clip, fire slot | AbletonOSC clip fire/stop, clip_slot fire; ahujasid | ableton-pp-cli clip fire | `clip stop` |
| 23 | Clip properties get/set (name, color, gain, pitch coarse/fine, loop start/end, start/end marker, warping, warp mode, launch mode, launch quantization, legato, muted, RAM mode, velocity amount, length, file path, playing position) | AbletonOSC clip get/set | ableton-pp-cli clip get | `clip set <prop> <value>`; enum names |
| 24 | Duplicate clip loop | AbletonOSC clip duplicate_loop | ableton-pp-cli clip duplicate-loop | |
| 25 | Session clip grid | AbletonOSC track clips/name, clips/length; track_data | ableton-pp-cli clips | Track x slot grid, names and lengths |
| 26 | MIDI notes get / add / remove range | AbletonOSC clip get/add/remove notes; ahujasid get/add/clear notes | ableton-pp-cli notes get | `notes add` (JSON in), `notes clear --pitch/--time ranges` |
| 27 | Scenes list / fire / fire selected / create / delete / duplicate | AbletonOSC scene fire*, song create/delete/duplicate_scene | ableton-pp-cli scene list | `scene fire/create/delete/duplicate` |
| 28 | Scene properties (name, color, tempo, time signature) | AbletonOSC scene get/set | ableton-pp-cli scene set | |
| 29 | View: selected track/scene/clip/device get/set | AbletonOSC view | ableton-pp-cli view | `view select ...` |
| 30 | Switch to Arrangement / Session view | ahujasid switch_to_arrangement_view; uisato set_ableton_view | ableton-pp-cli view show | `view show arrangement|session` |
| 31 | Devices on track | AbletonOSC track devices/name,type,class_name; ahujasid | ableton-pp-cli device list | Type names (instrument/audio_effect/midi_effect) |
| 32 | Parameters list (value, min, max, quantized, display string) | AbletonOSC device parameters/*; ahujasid get_device_parameters | ableton-pp-cli param list | By device name |
| 33 | Set parameter by name or index | AbletonOSC device set parameter/value; ahujasid set_device_parameter | ableton-pp-cli param set | Name match, range check, journaled |
| 34 | Device enable / disable | uisato enable/disable_device | ableton-pp-cli device enable | `device disable`; via "Device On" parameter over OSC |
| 35 | MIDI CC map to parameter | AbletonOSC midimap map_cc | ableton-pp-cli midimap | Channel 1-16 input (0-based on the wire) |
| 36 | Browser tree / items at path (instruments, sounds, drums, audio_effects, midi_effects, plugins) | ahujasid get_browser_tree/items_at_path; uisato list_external_plugins | ableton-pp-cli browser tree | `browser ls <path>`; synced to local FTS index for `search --type browser_items` |
| 37 | Load instrument / effect / plugin by URI | ahujasid load_instrument_or_effect; uisato load_external_plugin | ableton-pp-cli device load | Track by name |
| 38 | Load drum kit | ahujasid load_drum_kit | ableton-pp-cli drums load | |
| 39 | Arrangement clips list | ahujasid get_arrangement_clips; AbletonOSC arrangement_clips/*; uisato get_arrangement_info | ableton-pp-cli arrangement clips | Bar.beat positions |
| 40 | Copy Session clip into Arrangement at beat | ahujasid duplicate_session_clip_to_arrangement; uisato duplicate_clip_to_arrangement | ableton-pp-cli clip to-arrangement | --beat or --bar |
| 41 | Import audio file as clip | ahujasid create_audio_clip | ableton-pp-cli clip import-audio | Path validated locally first |
| 42 | Rack inspect | ahujasid inspect_rack | ableton-pp-cli rack inspect | |
| 43 | Map nested Dry/Wet to a macro | ahujasid map_rack_magnitude | ableton-pp-cli rack map-macro | --name for the macro label |
| 44 | Full project snapshot | ahujasid get_session_snapshot | ableton-pp-cli snapshot save | Stored locally with a label |
| 45 | Watch changes (tempo, playing, beat, track props, param value, selection, clip position) | AbletonOSC start_listen/stop_listen | ableton-pp-cli watch | NDJSON stream, --for duration |
| 46 | Status bar message | AbletonOSC api show_message | ableton-pp-cli message | |
| 47 | AbletonOSC log level | AbletonOSC api get/set log_level | (behavior in ableton-pp-cli doctor) --osc-log-level sets AbletonOSC's log level | |
| 48 | Delete device | uisato delete_device | ableton-pp-cli device delete | NEW in AbletonPPExtras: Track.delete_device; requires --yes |
| 49 | Rack chains and nested devices | uisato get_chain_info; ahujasid inspect_rack (partial) | ableton-pp-cli rack chains | NEW in AbletonPPExtras: RackDevice.chains, chain mixer (volume, pan, mute, solo) |
| 50 | Drum Rack pads | uisato get_drum_pad_info | ableton-pp-cli drums pads | NEW in AbletonPPExtras: DrumRack.drum_pads (note, name, chains, mute, solo) |
| 51 | Create empty MIDI clip in the Arrangement | uisato create_arrangement_midi_clip | ableton-pp-cli arrangement add-clip | NEW in AbletonPPExtras: Track.create_midi_clip(start, length) (Live 12) |
| 52 | Delete Arrangement clip | uisato delete_arrangement_clip | ableton-pp-cli arrangement delete-clip | NEW in AbletonPPExtras: Track.delete_clip (Live 12); requires --yes |
| 53 | Clip automation envelopes (read, write points, clear) | uisato manage_clip_automation | ableton-pp-cli automation set | NEW in AbletonPPExtras: Clip.automation_envelope / create_automation_envelope / insert_step / clear_envelope; also `automation get`, `automation clear` |

### Transcendence (only possible with our approach)
| # | Feature | Command | Buildability | Score | Why Only We Can Do This | Long Description |
|---|---------|---------|--------------|-------|------------------------|------------------|
| 1 | Safe note rewrite with backups | notes replace | hand-code | 10/10 | Backs up every clip's notes to the local store before an overwrite, then verifies the write by reading it back; restore re-applies any backup | Use this command to overwrite a clip's notes with an automatic backup. Do NOT use it to append or delete a few notes; use 'notes add' / 'notes clear' instead. |
| 2 | Load device by name | device add | hand-code | 9/10 | Joins the local browser index (from the TCP script) with track names (from OSC) so "load Echo on Dub Bass" works without a URI | Use this command to load an instrument or effect by its browser name. Do NOT use it when you already have a URI; use 'device load' instead. Do NOT use it to rebuild a multi-device chain with settings; use 'chain apply' instead. |
| 3 | Arrangement from scenes | arrangement build | hand-code | 9/10 | Combines OSC scene and clip lengths with TCP Arrangement copying and locators to lay out a whole song from scenes in one command | Use this command to lay out whole sections from scenes. Do NOT use it to place one clip at one beat; use 'clip to-arrangement' instead. |
| 4 | Mixer scenes | mix save | hand-code | 8/10 | Saves every track's volume, pan, mute, solo and sends by track name and recalls them later, even after tracks move | none |
| 5 | Action journal with targeted revert | journal list | hand-code | 7/10 | Records before/after values for every change the CLI makes so specific changes can be rolled back without touching Live's mixed undo history | Use this command to list or roll back specific changes made through this CLI. Do NOT use it to undo Live's whole edit history (including mouse edits); use 'undo' instead. Do NOT use it to compare whole-set states; use 'snapshot diff' instead. |
| 6 | Snapshot diff | snapshot diff | hand-code | 7/10 | Compares stored whole-project snapshots (or a snapshot against the live set) by name: tracks, mixer, devices, parameters, clips, note counts | Use this command to see every difference between two points in time, including manual edits. Do NOT use it to list or revert changes this CLI made; use 'journal list' / 'journal revert' instead. |
| 7 | Device chain save/apply | chain save | hand-code | 6/10 | Saves a track's device chain with parameter values and rebuilds it on another track by loading through the browser index and setting parameters by name | Use this command to rebuild a saved multi-device chain with its parameter values. Do NOT use it to load a single device; use 'device add' instead. |

Subcommands of transcendence rows (same features): notes restore, notes backups; arrangement build --dry-run; mix recall, mix list; journal revert; chain apply, chain list.

### Non-Go deliverable (hand-code, outside the generator)
- `remote-script/AbletonPPExtras/__init__.py`: our own Live Remote Script, written from scratch (no code copied from other projects). JSON over TCP bound to 127.0.0.1:9878 only, fixed command allowlist (rows 48-53 plus `ping`/`version`), no file-system writes, no network calls, no code evaluation, all Live API calls marshalled onto Live's main thread. Installed by `ableton-pp-cli scripts install` (copies only on explicit user request, with --dry-run preview).

## Source tally (priority inversion check)
- AbletonOSC (primary): rows 3-10, 12-13, 15-29, 31-35, 45-47 = 38 rows, plus transcendence 1, 4, 5 (OSC-led).
- AbletonMCP script (secondary): rows 2, 11, 14, 30, 36-44 = 13 rows, plus transcendence 2, 3, 6, 7 (cross-bridge).
- AbletonPPExtras (ours): rows 48-53 = 6 rows.
- No inversion: primary leads.

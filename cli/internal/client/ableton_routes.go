// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package client

import "ableton-pp-cli/internal/live"

const (
	bOSC = live.BridgeOSC
	bMCP = live.BridgeMCP
	bPPX = live.BridgePPX
)

// buildAbletonRoutes is the routing contract: one entry per endpoint in the
// synthetic spec. Literal paths come before {var} siblings.
func buildAbletonRoutes() []*route {
	return []*route{
		// status, transport, song
		{method: "GET", pattern: "/status", bridge: bOSC, command: "/live/song/get/track_data", handler: hStatus},
		{method: "POST", pattern: "/transport/play", bridge: bOSC, command: "/live/song/start_playing", handler: hSongMethod("/live/song/start_playing", "play")},
		{method: "POST", pattern: "/transport/stop", bridge: bOSC, command: "/live/song/stop_playing", handler: hSongMethod("/live/song/stop_playing", "stop")},
		{method: "POST", pattern: "/song/undo", bridge: bOSC, command: "/live/song/undo", handler: hSongMethod("/live/song/undo", "undo")},
		{method: "POST", pattern: "/song/redo", bridge: bOSC, command: "/live/song/redo", handler: hSongMethod("/live/song/redo", "redo")},
		{method: "POST", pattern: "/song/continue", bridge: bOSC, command: "/live/song/continue_playing", handler: hSongMethod("/live/song/continue_playing", "continue")},
		{method: "POST", pattern: "/song/tap-tempo", bridge: bOSC, command: "/live/song/tap_tempo", handler: hSongMethod("/live/song/tap_tempo", "tap-tempo")},
		{method: "POST", pattern: "/song/stop-clips", bridge: bOSC, command: "/live/song/stop_all_clips", handler: hStopClips},
		{method: "POST", pattern: "/song/seek", bridge: bOSC, command: "/live/song/set/current_song_time", handler: hSeek},
		{method: "GET", pattern: "/song/props/{prop}", bridge: bOSC, command: "/live/song/get/{prop}", handler: hSongGet},
		{method: "PUT", pattern: "/song/props/{prop}", bridge: bOSC, command: "/live/song/set/{prop}", handler: hSongSet},

		// record
		{method: "PUT", pattern: "/record/session", bridge: bOSC, command: "/live/song/set/session_record", handler: hRecordSession},
		{method: "POST", pattern: "/record/trigger", bridge: bOSC, command: "/live/song/trigger_session_record", handler: hSongMethod("/live/song/trigger_session_record", "record trigger")},
		{method: "POST", pattern: "/record/capture", bridge: bOSC, command: "/live/song/capture_midi", handler: hSongMethod("/live/song/capture_midi", "capture midi")},

		// locators
		{method: "GET", pattern: "/locators", bridge: bOSC, command: "/live/song/get/cue_points", handler: hLocatorList},
		{method: "POST", pattern: "/locators", bridge: bMCP, command: "create_locator", handler: hLocatorAdd},
		{method: "POST", pattern: "/locators/next", bridge: bOSC, command: "/live/song/jump_to_next_cue", handler: hSongMethod("/live/song/jump_to_next_cue", "next locator")},
		{method: "POST", pattern: "/locators/prev", bridge: bOSC, command: "/live/song/jump_to_prev_cue", handler: hSongMethod("/live/song/jump_to_prev_cue", "previous locator")},
		{method: "POST", pattern: "/locators/{locator}/jump", bridge: bOSC, command: "/live/song/cue_point/jump", handler: hLocatorJump},
		{method: "PUT", pattern: "/locators/{locator}", bridge: bOSC, command: "/live/song/cue_point/set/name", handler: hLocatorRename},
		{method: "DELETE", pattern: "/locators/{locator}", bridge: bOSC, command: "/live/song/cue_point/add_or_delete", handler: hLocatorDelete},

		// tracks
		{method: "GET", pattern: "/tracks", bridge: bOSC, command: "/live/song/get/track_data", handler: hTracks},
		{method: "POST", pattern: "/tracks", bridge: bOSC, command: "/live/song/create_<type>_track", handler: hTrackCreate},
		{method: "GET", pattern: "/meters", bridge: bOSC, command: "/live/track/get/output_meter_level", handler: hMeters},
		{method: "GET", pattern: "/tracks/{track}", bridge: bOSC, command: "/live/track/get/*", handler: hTrackGet},
		{method: "DELETE", pattern: "/tracks/{track}", bridge: bOSC, command: "/live/song/delete_track", handler: hTrackDelete},
		{method: "POST", pattern: "/tracks/{track}/duplicate", bridge: bOSC, command: "/live/song/duplicate_track", handler: hTrackDuplicate},
		{method: "PUT", pattern: "/tracks/{track}/name", bridge: bOSC, command: "/live/track/set/name", handler: hTrackRename},
		{method: "PUT", pattern: "/tracks/{track}/color", bridge: bOSC, command: "/live/track/set/color_index", handler: hTrackColor},
		{method: "PUT", pattern: "/tracks/{track}/volume", bridge: bOSC, command: "/live/track/set/volume (or AbletonPPExtras set_volume_db)", handler: hTrackVolume},
		{method: "PUT", pattern: "/tracks/{track}/pan", bridge: bOSC, command: "/live/track/set/panning", handler: hTrackPan},
		{method: "PUT", pattern: "/tracks/{track}/mute", bridge: bOSC, command: "/live/track/set/mute", handler: hTrackBool("mute")},
		{method: "PUT", pattern: "/tracks/{track}/solo", bridge: bOSC, command: "/live/track/set/solo", handler: hTrackBool("solo")},
		{method: "PUT", pattern: "/tracks/{track}/arm", bridge: bOSC, command: "/live/track/set/arm", handler: hTrackBool("arm")},
		{method: "PUT", pattern: "/tracks/{track}/sends/{send}", bridge: bOSC, command: "/live/track/set/send", handler: hTrackSend},
		{method: "GET", pattern: "/tracks/{track}/routing", bridge: bOSC, command: "/live/track/get/*_routing_*", handler: hRoutingGet},
		{method: "PUT", pattern: "/tracks/{track}/routing", bridge: bOSC, command: "/live/track/set/*_routing_*", handler: hRoutingSet},

		// clips
		{method: "GET", pattern: "/clips", bridge: bOSC, command: "/live/track/get/clips/name", handler: hClipGrid},
		{method: "GET", pattern: "/clips/{track}/{slot}", bridge: bOSC, command: "/live/clip/get/*", handler: hClipGet},
		{method: "POST", pattern: "/clips/{track}/{slot}", bridge: bOSC, command: "/live/clip_slot/create_clip", handler: hClipCreate},
		{method: "DELETE", pattern: "/clips/{track}/{slot}", bridge: bOSC, command: "/live/clip_slot/delete_clip", handler: hClipDelete},
		{method: "PUT", pattern: "/clips/{track}/{slot}/props/{prop}", bridge: bOSC, command: "/live/clip/set/{prop}", handler: hClipSet},
		{method: "POST", pattern: "/clips/{track}/{slot}/duplicate", bridge: bOSC, command: "/live/clip_slot/duplicate_clip_to", handler: hClipDuplicate},
		{method: "POST", pattern: "/clips/{track}/{slot}/fire", bridge: bOSC, command: "/live/clip/fire", handler: hClipMethod("/live/clip/fire", "fire")},
		{method: "POST", pattern: "/clips/{track}/{slot}/stop", bridge: bOSC, command: "/live/clip/stop", handler: hClipMethod("/live/clip/stop", "stop")},
		{method: "POST", pattern: "/clips/{track}/{slot}/duplicate-loop", bridge: bOSC, command: "/live/clip/duplicate_loop", handler: hClipMethod("/live/clip/duplicate_loop", "duplicate loop")},
		{method: "POST", pattern: "/clips/{track}/{slot}/to-arrangement", bridge: bMCP, command: "duplicate_session_clip_to_arrangement", handler: hClipToArrangement},
		{method: "POST", pattern: "/clips/{track}/{slot}/import-audio", bridge: bMCP, command: "create_audio_clip", handler: hClipImportAudio},

		// notes
		{method: "GET", pattern: "/notes/{track}/{slot}", bridge: bOSC, command: "/live/clip/get/notes", handler: hNotesGet},
		{method: "POST", pattern: "/notes/{track}/{slot}", bridge: bOSC, command: "/live/clip/add/notes", handler: hNotesAdd},
		{method: "DELETE", pattern: "/notes/{track}/{slot}", bridge: bOSC, command: "/live/clip/remove/notes", handler: hNotesClear},

		// scenes
		{method: "GET", pattern: "/scenes", bridge: bOSC, command: "/live/song/get/scenes/name", handler: hScenes},
		{method: "POST", pattern: "/scenes", bridge: bOSC, command: "/live/song/create_scene", handler: hSceneCreate},
		{method: "POST", pattern: "/scenes/selected/fire", bridge: bOSC, command: "/live/scene/fire_selected", handler: hSongMethod("/live/scene/fire_selected", "fire selected scene")},
		{method: "POST", pattern: "/scenes/{scene}/fire", bridge: bOSC, command: "/live/scene/fire", handler: hSceneFire},
		{method: "DELETE", pattern: "/scenes/{scene}", bridge: bOSC, command: "/live/song/delete_scene", handler: hSceneDelete},
		{method: "POST", pattern: "/scenes/{scene}/duplicate", bridge: bOSC, command: "/live/song/duplicate_scene", handler: hSceneDuplicate},
		{method: "PUT", pattern: "/scenes/{scene}/props/{prop}", bridge: bOSC, command: "/live/scene/set/{prop}", handler: hSceneSet},

		// view
		{method: "GET", pattern: "/view/selection", bridge: bOSC, command: "/live/view/get/*", handler: hViewGet},
		{method: "PUT", pattern: "/view/selection", bridge: bOSC, command: "/live/view/set/*", handler: hViewSelect},
		{method: "PUT", pattern: "/view/main", bridge: bMCP, command: "switch_to_arrangement_view", handler: hViewShow},

		// devices and parameters
		{method: "GET", pattern: "/tracks/{track}/devices", bridge: bOSC, command: "/live/track/get/devices/*", handler: hDeviceList},
		{method: "POST", pattern: "/tracks/{track}/devices", bridge: bMCP, command: "load_instrument_or_effect", handler: hDeviceLoad},
		{method: "PUT", pattern: "/tracks/{track}/devices/{device}/enable", bridge: bOSC, command: "/live/device/set/parameter/value (Device On)", handler: hDeviceOn(true)},
		{method: "PUT", pattern: "/tracks/{track}/devices/{device}/disable", bridge: bOSC, command: "/live/device/set/parameter/value (Device On)", handler: hDeviceOn(false)},
		{method: "DELETE", pattern: "/tracks/{track}/devices/{device}", bridge: bPPX, command: "delete_device", handler: hDeviceDelete},
		{method: "GET", pattern: "/tracks/{track}/devices/{device}/params", bridge: bOSC, command: "/live/device/get/parameters/*", handler: hParamList},
		{method: "PUT", pattern: "/tracks/{track}/devices/{device}/params/{param}", bridge: bOSC, command: "/live/device/set/parameter/value", handler: hParamSet},
		{method: "POST", pattern: "/midimap", bridge: bOSC, command: "/live/midimap/map_cc", handler: hMidiMap},

		// browser
		{method: "GET", pattern: "/browser/tree", bridge: bMCP, command: "get_browser_tree", handler: hBrowserTree},
		{method: "GET", pattern: "/browser/items", bridge: bMCP, command: "get_browser_items_at_path", handler: hBrowserItems},
		{method: "GET", pattern: "/browser/index", bridge: bMCP, command: "get_browser_items_at_path", handler: hBrowserIndex},

		// drums, racks
		{method: "POST", pattern: "/tracks/{track}/drum-kit", bridge: bMCP, command: "load_browser_item", handler: hDrumsLoad},
		{method: "GET", pattern: "/tracks/{track}/devices/{device}/pads", bridge: bPPX, command: "drum_pads", handler: hDrumPads},
		{method: "GET", pattern: "/tracks/{track}/devices/{device}/rack", bridge: bMCP, command: "inspect_rack", handler: hRackInspect},
		{method: "GET", pattern: "/tracks/{track}/devices/{device}/chains", bridge: bPPX, command: "rack_chains", handler: hRackChains},
		{method: "POST", pattern: "/tracks/{track}/devices/{device}/rack/map-macro", bridge: bMCP, command: "map_rack_magnitude", handler: hRackMapMacro},

		// arrangement
		{method: "GET", pattern: "/arrangement/{track}/clips", bridge: bMCP, command: "get_arrangement_clips", handler: hArrClips},
		{method: "POST", pattern: "/arrangement/{track}/clips", bridge: bPPX, command: "arrangement_add_clip", handler: hArrAddClip},
		{method: "DELETE", pattern: "/arrangement/{track}/clips/{clip}", bridge: bPPX, command: "arrangement_delete_clip", handler: hArrDeleteClip},

		// automation
		{method: "GET", pattern: "/automation/{track}/{slot}/{device}/{param}", bridge: bPPX, command: "automation_get", handler: hAutomationGet},
		{method: "PUT", pattern: "/automation/{track}/{slot}/{device}/{param}", bridge: bPPX, command: "automation_set", handler: hAutomationSet},
		{method: "DELETE", pattern: "/automation/{track}/{slot}/{device}/{param}", bridge: bPPX, command: "automation_clear", handler: hAutomationClear},

		// misc
		{method: "GET", pattern: "/snapshot", bridge: bMCP, command: "get_session_snapshot", handler: hSnapshot},
		{method: "POST", pattern: "/message", bridge: bOSC, command: "/live/api/show_message", handler: hMessage},
		{method: "GET", pattern: "/health", bridge: bOSC, command: "/live/application/get/version + get_script_info + version", handler: hHealth},
	}
}

---
name: analyse-set
description: Analyse one of the user's Ableton Live sets end to end (MIDI, automation, devices, Analog Lab presets, STFT of every stem) and build sound thumbprints plus a findings page. Use when the user asks to analyse, fingerprint, thumbprint or critique a set or track, or wants a new reference for sound design.
---

# Analyse a set

Produces `thumbprints/<Set>/` with `set.json`, `midi_analysis.json`, `audio_analysis.json`, `thumbprints.json` and `ANALYSIS.md`, plus an optional findings page. ProgAcidAI is the worked example: `thumbprints/ProgAcidAI/ANALYSIS.md`.

All scripts are read-only on the `.als`. Never save over the user's set.

## 1. Find the set file
- Ask for the folder if you don't know it. Otherwise the open set's name is in Live's window title (`python harness/liveguard.py status`).
- Find `<Set>.als` in the project folder. Its modified time shows whether it's saved; unsaved changes in Live are not in the file, so say so if the title has `*`.

## 2. Extract (no Live needed)
```bash
mkdir -p "thumbprints/<Set>"
python analysis/extract_als.py "<path>/<Set>.als" "thumbprints/<Set>/set.json"
python analysis/plugin_state.py "<path>/<Set>.als" "thumbprints/<Set>/set.json"
python analysis/analyze_midi.py "thumbprints/<Set>/set.json" "thumbprints/<Set>/midi_analysis.json"
```
- `extract_als.py`: tracks, mixer, device trees with parameters, every clip and note, audio file references, automation resolved to parameter names. Drum pads are stored as 128 − MIDI note; the script converts them.
- `plugin_state.py`: decodes Analog Lab V state (preset, pack, author, tags, description, macros).
- `analyze_midi.py`: per-track MIDI features, the song key from MIDI, the 160-bar activity map and 4-bar sections.

## 3. Render stems (track design, so offline export)
Follow the `listen` skill: **Export all stems**. Always offline export, never resampling.

## 4. Analyse audio and build thumbprints
```bash
python analysis/analyze_audio.py "<stems folder>" <tempo> "thumbprints/<Set>/audio_analysis.json" "thumbprints/<Set>/png"
python analysis/build_thumbprints.py thumbprints/<Set>/set.json thumbprints/<Set>/midi_analysis.json thumbprints/<Set>/audio_analysis.json thumbprints/<Set>/thumbprints.json
```
This takes a few minutes for about 25 stems; run it in the background. `png/` is not committed; regenerate it when needed.

## 5. Check the numbers before writing anything
- Silent stems (tracks with no arrangement clips) come back `{"silent": true}`. Call them unused; don't fingerprint them.
- Per-track key estimates are biased by overtones. Report the strongest pitch classes per track, and take the key from the full-mix stem (`<Set> Stems.wav`). Cross-check it with the MIDI key.
- Look at one or two spectrograms. Low frequencies use nearest-bin filling, so horizontal stripes there would mean a bug.
- The Auto Filter type order in Live 12 is 0 lowpass, 1 highpass, 2 bandpass, 3 notch, 4 morph, 5 DJ. Check it against the audio: energy sitting above the cutoff means it's a high-pass.

## 6. Write it up
- `thumbprints/<Set>/ANALYSIS.md`: the song at a glance, the arrangement table, track-by-track tables, the production signature (habits that repeat across tracks), how to use the thumbprints, and the method and its limits. Every claim must come from the JSON; re-check any number you quote.
- Optional findings page: run `python analysis/build_report_data.py "<stems folder>" report_data.json` from inside `thumbprints/<Set>/`. Then fill `analysis/report_template.html`, replacing `/*DATA*/` with the JSON (escape `</` as `<\/`) and rewriting the "production signature" cards for this set. Publish it as an artifact if the user wants it.
- Commit the JSON and markdown. Never commit WAVs.

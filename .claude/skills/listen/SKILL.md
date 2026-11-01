---
name: listen
description: Get audio out of Ableton Live so Claude can hear its work. Resampling for sound design (short, automatic) and offline stem export through screen control for track design (whole song, glitch-free). Use whenever a loop round needs audio, or the user asks to render, bounce, export or listen.
---

# Listen

Pick the method by the job. They are not interchangeable.

| Job | Method | Time | Screen control |
|---|---|---|---|
| Sound design: one instrument, 1–4 bars | **Resample** | bars × 240 / BPM seconds, plus setup | no |
| Track design: whole arrangement, all stems | **Export all stems** | about 3 min for a 5-minute song with Analog Lab | yes, Live hands-off |

Never use resampling for track design: it records through the live audio engine, so CPU spikes and dropouts land in the audio. Export renders inside Live and cannot glitch.

## Resample (sound design)
Only in a `SCRATCH` set. The Live guard asks otherwise.

1. `ableton-pp-cli resample <track> --bars <n> --json`. *(This command is still being built; see `CLAUDE.md` and the plan. Until it exists, ask the user to drag the clip to a resampling track, or use export.)*
2. Check the recording before scoring it:
   ```bash
   python harness/glitch.py "<wav>" --expect-seconds <bars*240/BPM> --json
   ```
   Exit 2 means a dropout, click, wrong length or silence. Re-record up to twice. If it still fails, report the glitch instead of scoring bad audio.
3. Score it: `python harness/score.py "<wav>" --target "<Set>:<Track>" --tempo <BPM>` (see the `iterate` skill).
4. Delete the recorded files from the scratch set's `Samples/Recorded` folder when the loop ends.

## Export all stems (track design)
Use screen control (computer-use) on **Ableton Live 12 Suite**.

1. Tell the user first: **"Hands off Live for about 3 minutes while I export stems."** If screen control is unavailable, or the user is away and hasn't allowed it this session, stop and ask. Don't fall back to resampling.
2. Make sure transport is stopped. Focus Live:
   - Windows may report the desktop shell as frontmost. Click empty space in Live's device view, the bottom pane's "Drop Audio Effects Here" area, to focus it.
   - The title bar opens the window's system menu instead; press Escape if that happens.
3. Press **Ctrl+Shift+R** to open Export Audio/Video. Set:
   - Rendered Track: **All Individual Tracks**.
   - Render Start: double-click the bar field, type `1`, Enter, giving 1.1.1.
   - Render Length: double-click the bar field, type the song length in bars (from `midi_analysis.json` `bars`, or the last arrangement clip end ÷ 4), Enter.
   - Include Return and Main Effects: **Off**, so stems are the tracks' own sound.
   - Sample rate 44100, WAV, 16-bit. Leave the rest as it is.
4. Click Export. The save dialog opens in the set's project folder. Type the name **`<Set> Stems`** and click Save.
5. Live renders, then writes `.asd` analysis files; total about 3 min. Wait until the count of `"<Set> Stems*.wav"` in the project folder stops growing for about 25 s.
6. Live writes the stems loose in the project folder as `<Set> Stems <Track>.wav`, with the main mix as `<Set> Stems.wav`. Move them and their `.asd` files into a `<Set> Stems\` subfolder.
7. Tell the user that **Live is theirs again**.

Stems stay next to the Live project and are never committed.

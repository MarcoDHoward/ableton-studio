---
name: fixphase
description: Make a mix mono-safe while keeping it wide. Measures stereo correlation, mid/side and mono-fold loss per stem and band, simulates width / Bass Mono / polarity fixes on rendered stems, optimizes the smallest change against agreed targets, applies it in Live, verifies with a full render and writes a before/after report. Use when the user asks to fix phase, check mono compatibility, phase cancellation, correlation, stereo width, "thin in mono", or wants a phase report on a set.
---

# Fix phase

The worked example is ReggaeBuilder (2026-11-08): `runs/2026-11-08/phase/` for the numbers,
`docs/examples/phase-report-reggaebuilder.html` for the report page. Read that page once before starting;
it explains every metric and every step below.

**Done means:** a measured baseline, an agreed list of settings and targets, the settings applied in Live,
a full-render before/after, and a report (markdown in `runs/`, plus an HTML page if the user wants one).

## Rules for this loop
- All Live reads and writes go through `cli/ableton-pp-cli.exe`. Writes are free only when Live's title
  starts with `SCRATCH`. In any other set, ask before each write.
- Run `ableton-pp-cli snapshot save phase-<date>` before the first write, in every set. If Live crashes,
  `ableton-pp-cli snapshot diff phase-<date> --live` shows what recovery lost.
- **Agree the search space and the targets with the user before optimizing.** The user expects this.
- Track design, so audio comes from offline export (`listen` skill, **Export all stems**). Never resample.
- Never touch the kick, toms or drum buses, faders, pans, sends or Analog Lab internals unless the user adds them.
- Say "hands off Live for ~3 minutes" before each export and say when Live is free again.

Work in `runs/<date>/phase/` (not in git). `R` below means that folder.

## 1. Set up
```bash
python harness/liveguard.py status                         # title must start with SCRATCH for free writes
python analysis/extract_als.py "<project>/<Set>.als" R/set.json   # read-only; locators, groups, returns
cli/ableton-pp-cli.exe snapshot save phase-<date>
```
The `.als` on disk lacks unsaved changes. If the title has `*`, tell the user the extraction may be stale.

## 2. Render the baseline (R0)
`listen` skill, Export all stems: All Individual Tracks, 1.1.1, the whole song length, Return and Main
Effects **Off**, 44.1 kHz, 16-bit WAV. Name it **`PhaseR0`**. Live writes `PhaseR0 <Track>.wav` loose in the
project folder and the main mix as `PhaseR0.wav`. Move them all (and the `.asd` files) into `PhaseR0\`.
Rename any stem whose name picked up a device suffix (a return shows as `B-Delay | Utility` → `B-Delay`).

## 3. Measure
```bash
python analysis/phase_meter.py "<project>/PhaseR0" --tempo <BPM> --set R/set.json --json R/r0_meter.json --md R/r0_meter.md
python analysis/phase_meter.py "<project>/PhaseR0" --tempo <BPM> --set R/set.json --bars A-B --json R/r0_meter_bA-B.json   # a weak section
```
Main mix and every stem get broadband and per-band ρ, side level, width %, mono loss, L/R balance,
short-term ρ (median, p5, % of time below 0), low-end (<150 Hz) ρ and L/R lag, per-bar figures and flags.
Low-end stem pairs get a sum-vs-power check with the best lag within ±10 ms.

Reading it:
- ρ = 0 folds down **3 dB** and is not cancellation. Cancellation is ρ < 0 or loss beyond −3 dB.
- With balanced L/R, mono loss = 10·log10((1+ρ)/2): ρ 0.71 → −0.69 dB, ρ 0.84 → −0.36 dB.
- The song average hides sections. Always look at `per_bar` and `worst_bars_mono`.
- Energy matters more than correlation: a wide quiet stem is harmless; a moderately wide loud one is not.

## 4. Diagnose
| Finding | Likely cause | Lever |
|---|---|---|
| Low-end ρ < 0.98 or a non-zero lag | wide sub, Haas delay, chorus on a bass | Bass Mono on that stem or Main; chorus HP |
| Pair sum well below the power sum, with a lag | kick/bass timing or polarity | nudge or flip polarity on one of the pair (rare; prove it with the pair check first) |
| Stem ρ near 0 or below, loud in 150 Hz–4 kHz | stereo synth, ping-pong delay, wide return | Utility Width on that stem or return |
| Stem side louder than mid | inverted channel, M/S trick | Left Inv (only when S ≥ M) |
| One section bad, song average fine | an instrument or return that only plays there | section targets (step 6) |

What is usually **not** the cause:
- Filters and EQs that process L and R identically at 100% Dry/Wet. They rotate phase equally in both channels.
- Time-sliding tracks, unless the pair check shows a lag.
- Auto Filter **LFO Phase** (stereo mode Phase) does offset the L and R LFOs; **Offset** only moves the cycle
  start. Check only the filters with a non-zero LFO Phase.

Report the measured problems to the user before proposing any fix.

## 5. Agree the search space and targets
Propose, then wait for the user's OK (AskUserQuestion is fine). Write them to `R/space.json` and `R/targets.json`.

- **Space**: `{track: [kinds]}`. Kinds:
  - `width`: Utility Stereo Width, 50–120%.
  - `bass_mono`: Utility Bass Mono at 100–300 Hz.
  - `left_inv`: Utility Left Inv.
  - `polarity`: keep or remove an existing L+R inversion.
  - `chorus_hp`: Chorus-Ensemble high-pass.
  - `low_cut`: reverb input low cut.
  - Main's Bass Mono at 120–180 Hz is always in the space unless frozen.
  Include only wide stems and returns that hold energy where the problem is.
- **Targets** (ReggaeBuilder defaults, `TARGETS` in `analysis/phase_optimize.py`):
  - below 150 Hz ρ ≥ 0.98;
  - 150–300 Hz ρ ≥ 0.85;
  - 300 Hz–1 kHz ρ ≥ 0.6;
  - per-bar ρ ≥ 0.05;
  - mono loss ≥ −0.8 dB;
  - no stem band holding more than 2% of its energy loses more than 3 dB;
  - 1–10 kHz side energy drops at most 2 dB;
  - no synth below 50% width;
  - smallest change wins.

## 6. Optimize
```bash
python -u analysis/phase_optimize.py "<project>/PhaseR0" --set R/set.json --tempo <BPM> --space R/space.json --targets R/targets.json --out R/r0_plan.json
python -u analysis/phase_optimize.py ... --section-bars 8 --out R/plan_section8.json     # targets per 8-bar section
python analysis/phase_optimize.py ... --section-bars 8 --eval "Acid Bass=0.65,Lead=0.7,B-Delay=0.6" --eval "Acid Bass=0.5,Lead=0.5,B-Delay=0.6"
```
- The first run builds the Gram tensor (about 25 s for 25 stems) and caches it as `phase_gram_v2.npz` in the
  stems folder. After that every run is seconds. Use `python -u`, and run it in the background if the space is large.
- L-BFGS-B, at most 60 iterations per switch combination, and 2^switches combinations. Switches cost 0.3 each in
  the objective, so subtract 0.3 per switch to see what one does to the metrics.
- `--freeze "B-Delay,Main"` leaves tracks out (for example, ones you can't reach).
- Use `--eval` to score hand-picked options. Present 2–4 options to the user as a table: what changes, J,
  300 Hz–1 kHz and 150–300 Hz ρ, worst section, and the 1–10 kHz side drop (the width you give up).
  Let them pick.
- Trust: the simulator matched real renders within 0.04 ρ and 0.1 dB, slightly pessimistic. It can't see a
  return narrowing because its input got narrower, or time-varying effects (phasers, filter LFOs, free-running synths).

## 7. Apply
On a track (journaled, revertible with `journal revert`):
```bash
cli/ableton-pp-cli.exe device add "<Track>" Utility --category audio_effects --json     # lands at the end of the chain
cli/ableton-pp-cli.exe param set "<Track>" Utility "Stereo Width" --value <sqrt(width)> --json
cli/ableton-pp-cli.exe param set "<Track>" Utility "Bass Mono" --value 1 --json
cli/ableton-pp-cli.exe param set "<Track>" Utility "Bass Freq" --value <log10(f/50)> --json
```
- **Stereo Width raw = √(width) on a 0–2 range.** Raw 0.9 shows 81%, and 0.806 shows 65%. Check `after_display`.
- Bass Freq raw = log10(f / 50) over 50–500 Hz.
- If the track already has a Utility, the name is ambiguous and the CLI refuses it. Address the new one by its
  number from `device list` (for example `param set "Lead" 8 "Stereo Width" ...`).

Return tracks and Main can't be addressed by the CLI. Use screen control:
1. Click the return's title in the Arrangement to select it.
2. In the browser, open Audio Effects, scroll to **Utility** and double-click it. It loads at the end of the
   selected track's chain; a fast drag only selects it.
3. Click the Width knob, type the percent (for example `60`) and press Enter. Zoom in to confirm.

## 8. Verify
- **Middle rounds (optional):** render a fragment twice, once with the new devices bypassed and once on
  (`device disable` / `enable`), and measure the same bars with `--start-bar`. **Only compare a fragment with a
  fragment.** Analog Lab LFOs, arps and free-running state restart at the render start, so a fragment doesn't
  match the same bars of a full render; one level shifted 2.5 dB that way.
- **Final:** a full render named `PhaseFinal` with exactly the R0 settings, moved into `PhaseFinal\`. Then:
  ```bash
  python analysis/phase_meter.py "<project>/PhaseFinal" --tempo <BPM> --set R/set.json --json R/final_meter.json --md R/final_meter.md
  python analysis/phase_report_data.py --before "<project>/PhaseR0" --after "<project>/PhaseFinal" --set R/set.json \
      --before-meter R/r0_meter.json --after-meter R/final_meter.json --chosen "<the --eval spec you applied>" \
      --tempo <BPM> --out R/report_data.json
  ```
  `report_data.json` holds:
  - the per-band, per-bar and per-section ρ before and after, plus the simulator's prediction;
  - each stem's share of the side energy;
  - absolute mid/side/stereo level changes (`levels`). Use these for the width-budget target; the meter's
    `side_db` is relative to mid.

## 9. Report
- `R/final_report.md`: the changes (track, device, value, journal id), every target with limit, before, after
  and status, the main band table, the stems that changed, incidents and open items.
- HTML page (when the user wants one): start with Artifact `quickstart`, then build from
  `docs/examples/phase-report-reggaebuilder.html`. Reuse its CSS and chart functions and swap `const D = {...}`
  for this run's `report_data.json`. The band rows, switch table, stem table, share sections and every caption
  are written for ReggaeBuilder, so regenerate the tables from the data and rewrite the text.
- **Check every sentence against the numbers before publishing.** In the example, three claims were wrong on
  first draft:
  - "ρ = 1.00 in every bar" (the late bars dipped to 0.84);
  - "the polarity flip mattered most" (its score was exactly the switch penalty);
  - "at the width limit" (relative side_db was used instead of the absolute side drop).
- Copy the report to the shared notes folder if this runs as a side session.

## 10. Clean up
- Ask whether to keep `PhaseR0\` and `PhaseFinal\` for listening (about 1.4 GB each; F: is nearly full). Delete
  fragment renders straight away. If renders are kept, save a project memory reminding the user to delete them.
- Tell the user the set is unsaved, if it is. Never save over a user's set yourself.

## Gotchas from the first run
- **Live can crash during export.** Recovery may keep some new devices and reset others (the B-Delay Utility
  came back at 100%). Run `snapshot diff phase-<date> --live`, then fix and re-export.
- After a crash, values read from Live are at the playhead. Automated parameters (Delay Feedback, device on/off,
  volumes) will look changed when the playhead isn't at bar 1. Check automation before calling it drift.
- The Export dialog can reset Rendered Track to **Main** after a crash. Check every field each time.
- An unidentified window can cover the Export dialog in screenshots. Drag the dialog aside by its title bar.
- If Windows reports the desktop shell as frontmost, click Live's empty device area ("Drop Audio Effects Here").
- The simulator's 300 Hz–1 kHz baseline was 0.05 too high until the untouched part of the mix was modelled as
  top-level stems (group buses included) minus the adjustable stems. Keep it that way.
- Left Inv only helps when a stem's side is louder than its mid. Flipping both channels of a stem that is
  uncorrelated with the rest changes nothing in the sum.

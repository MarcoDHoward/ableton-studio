# ProgAcidAI: set analysis and sound thumbprint

Source: `F:\Ableton\ReggaeBuilder Project\ProgAcidAI.als` (Live 12.4.16), saved 2026-10-31 19:18.
Stems: `ProgAcidAI Stems\` (25 WAVs, 44.1 kHz/16-bit, bars 1–160, rendered without return/main FX).
Machine-readable thumbprints: `thumbprints.json` (schema `sound-thumbprint/1`).

## The song at a glance

| | |
|---|---|
| Tempo / length | 121 BPM, 4/4, 160 bars, about 5:17 |
| Key | **A minor** (audio of the full mix: 0.88 confidence; MIDI: A holds 63 % of note time, then D 22 %, G 8 %, E 5 %) |
| Harmony style | Pedal-tone writing. Long held notes, often whole bars or 8 bars, with the motion coming from preset sequencers, Live's Arpeggiator and filter automation |
| Mix balance (full mix, before the Main chain) | sub 3.5 % · bass 18 % · low-mid 34 % · mid 42 % · high-mid 2 % · air 0.7 %. Warm and low-mid-centred; spectral centroid 690 Hz (range 290 Hz – 2.1 kHz) |
| Level (before the Main chain) | RMS −19 dBFS, peaks at 0.0 dBFS, crest 19 dB. Your Main chain (Stereo Gain → Glue → Color Limiter) does the final catching |
| Width (mix) | side −5.5 dB under mid, L/R correlation 0.56 |
| Rhythmic pulse (mix) | 16ths dominate the loudness modulation, quarters are 10 dB below |
| Locators | "Tribal Tom in" (bar 21), "First BreakDown" (bar 53) |

### Arrangement (4-bar blocks, by which tracks play)

| Bars | What happens |
|---|---|
| 1–8 | Kick, toms, closed hats, Shimmer Melody (Berlin Sequence) |
| 9–20 | Claps, High Keys and Moody Melody join; crash FX into bar 17; 4-bar drop-out of the kick at 17–20 with Hat Rolls |
| 21–48 | Main groove: shaker, Lead (7 Osc Chord Rave Seq) and **Acid Bass** enter at bar 21; claps back at 37 |
| 49–56 | Peak and build: crash FX, Snare Roll and Aliens downsweep; everything cuts at bar 54 |
| 57–60 | Breather: pads, Lead and acid without drums |
| 61–84 | Second groove: cowbell, Hat Rolls throws, Shaker 2 loop; no claps |
| 85–116 | **Breakdown**: drums out; Moody, Shimmer, Lead and acid carry it; claps and snare roll rebuild from 105 |
| 117–136 | Final full section (11 tracks) |
| 137–160 | Outro: layers peel off until only Acid Bass is left |

Two tracks never play in the arrangement: **Decorations** (a drum rack of Splice fills) and **22-Analog Lab V** (preset "Blue Universe", a bowed-strings pad). They are in the set but have no clips.

## Track by track

Frequencies are medians over the parts where the track is playing. Attack and decay come from the loudness envelope (5.8 ms resolution). Width is side-to-mid energy (0 dB = very wide, −30 dB or below = mono).

### Drums (group: Glue Compressor)

| Track | Source | Pattern (16 steps per bar) | Sound |
|---|---|---|---|
| Kick | Drum Rack, `STCR2_MHPT2_Kick_Disturbing_A` → EQ Eight | four-on-the-floor, flat velocity 99 | 60 Hz centre, 80 % of energy in 60–250 Hz, 17 ms attack, 170 ms decay, mono |
| Toms | Drum Rack of 15 toms (subby, natural, synth) → Glue | off-beat 8ths (the "and"s), velocity 61 ± 37, very dynamic | 110 Hz, all in the bass band, mono-ish: the tribal low end |
| Claps | Rack: Clap Analog 4, DS Clap, 505 Classic (and a reversed copy), wasted clap | beats 2 and 4, plus a ghost 16th before beat 4 | 2.2 kHz, 6 ms attack, 75 ms decay, mono |
| Closed HH | Acoustified hat → AutoFilter | every 16th, accented (velocity 82 ± 14) | 8 kHz centre, 74 % air, 23 ms decay, mono |
| Shaker | Afoxé Percussion kit (cabasa, agogô, pandeiro, surdo, tamborim) → AutoPan | Brazilian 16th pattern, with rests on steps 6, 10 and 14 and doubles on 3 and 13 | 11 kHz, panned wide (correlation 0.01) |
| Cowbell | Cowbell A 2 | straight 8ths, all at velocity 127 | 1 kHz, narrow mid band |
| Shaker 2 | Splice loop `BOS_SKE_110_Percussion_Shakers_Loop_Selassie` → AutoFilter → Delay | audio loop | 9 kHz |
| Hat Rolls | reversed `STCR2_MHPT2_Hat_Momentous` → Delay → Phaser-Flanger | 64 short clips, **1/4-triplet** feel | send and delay-on automation toggled 49 times: rhythmic FX throws |
| Snare Roll / Aliens / crash | Splice build snare, reversed downsweep, reversed crash | transitions only | filter 20 Hz ↔ 20 kHz sweeps; Echo feedback up to 150 %; delay throws |

### Synths (all Arturia Analog Lab V, each followed by a long chain of Live effects)

| Track | Preset (pack, author) | Chain after the synth | What the audio shows | Movement (automation) |
|---|---|---|---|---|
| **Acid Bass** | *Tranceport*: Bass Line, Factory, Sami Rabia ("trance bass…, Timbre controls sub level"). Brightness 0.41, Timbre 0.76, Movement 1.0 | Compressor ×2 → **HP filter 148 Hz** → Overdrive → Chorus → EQ Eight → **HP filter 56 Hz** → Glue | 43 % low-mid + 52 % mid, only 6 % bass band. 17 ms attack, 70 ms decay, pulsing on 16ths (the preset's sequencer; the MIDI is whole-bar held notes) | **761 hand-drawn cutoff points (20 Hz → 8.4 kHz) and 420 resonance points (0 → 0.72) on the second, high-pass filter, bars 1–143.** The acid squelch is a resonant high-pass riding under the line, not a low-pass. The spectrogram shows the bottom edge moving |
| Lead | *7 Osc Chord Rave Seq*: Pigments 3, Maxime Audfray (7 oscillators, Timbre morphs triangle → saw) | Pedal → EQ Eight → Compressor → AutoPan → HP filter 1 kHz → DJ filter | 620 Hz – 2 kHz, **correlation −0.07** (very wide, phasey), 64 ms decay | Pedal drive 0 → 1, Timbre 0 → 1, Delay level −70 → +12 dB, HP sweep 20 Hz → 1 kHz, DJ-filter sweep, send-B throws |
| Moody Melody | *Funky Lead*: Factory, Jean-Michel Blanchet (via MIDI Pitcher + Arpeggiator) | Compressor ×2 → Cabinet → **band-pass 537 Hz** → Channel EQ → HP filter 879 Hz | 0.7 – 2.9 kHz, ~1 s tails, quarter-note pulse | two filter sweeps (band-pass 256 Hz → 8.8 kHz, HP 20 Hz → 9.7 kHz); Analog Lab Master Bass EQ cut 0 → −10 dB |
| Shimmer Melody | *Berlin Sequence*: Factory, Victor Morello ("gentle percolating sequence, stacked saws") | Sidechain comp → Compressor → HP 1.15 kHz → Echo → LFO (M4L) → Erosion → HP 2.4 kHz → AutoPan | 1.4 – 2.3 kHz, quarter-note pulse (sidechain), intervals of fifths | 11 lanes: HP sweep up to 3.7 kHz, reverb −70 → −6 dB, pan sweeps −1 ↔ +1, Brightness 0 → 0.48, Erosion noise |
| High Keys | *Classic Jun Keys*: AL Play Factory, Solidtrax (a Juno-style bright/mellow key) | MIDI Pitcher → HP 540 Hz → HP 723 Hz | 97 % energy in 500 Hz – 2 kHz: thinned to a narrow mid band, 700 ms tails | HP cutoff automated 20 → 723 Hz: the keys start full and get thinned |

Returns: **A** = Bright Long Verb (≈1.3 s tails, 4 kHz centre); **B** = Delay, fed mostly by the Lead (its thumbprint is 0.93 similar to the Lead's).

## Your production signature (what the thumbprints say you do)

1. **Groove**: a straight four-on-the-floor kick, with **off-beat tribal toms** carrying the bounce, flat 16th hats, Brazilian (afoxé) shaker patterns and 8th cowbell. Prog/tribal house at 121.
2. **Harmony**: A-minor pedal. You hold long notes and let **preset sequencers and arps** generate the notes; intervals move in 4ths, 5ths and octaves.
3. **Sound sources**: Analog Lab V presets in the Factory and Pigments styles: Bass Line, Solo Lead, Melodic Sequence, Synth Keys. Every synth is followed by **compression → filtering → character (Pedal, Overdrive, Cabinet, Erosion) → movement (AutoPan, LFO)**, and almost always **two Auto Filters, nearly all of them high-pass**.
4. **You carve from below.** Every synth is high-passed (56 Hz on the acid, 540–720 Hz on the keys, 880 Hz – 2.4 kHz on the melodies and lead), so nothing competes with the kick and toms. Brightness changes come from moving those high-pass cutoffs, plus band-pass and DJ filters, rather than from closing low-pass filters.
5. **Movement is automation**: every synth has filter automation, the acid is a hand-drawn resonant high-pass performance, and transitions use FX throws (delay on/off, send spikes, Echo feedback runaway).
6. **Mix space**: low end is kick and toms only, in mono; the acid sits in the low-mids; synths are wide (correlation 0 – 0.5); the top end is reserved for hats and shakers; air stays restrained (high-mid plus air is under 3 % of the mix). The overall mix is warm and low-mid-heavy.

## Using the thumbprints to make new sounds

Every entry in `thumbprints.json` has:
- `instrument_chain`: the preset, pack, tags, macro values and the full device order, so the chain can be rebuilt.
- `design_hints`: filter cutoff range (10th to 90th percentile of the spectral centroid), amp attack and decay in ms, spectral tilt, noise layer yes/no, stereo treatment, register, velocity feel.
- `descriptors`: plain-language tags.
- `vector`: 48 numbers (31 third-octave bands, 12 MFCCs, centroid, flatness, attack, decay, width). Cosine similarity on this finds "something that sounds like my Acid Bass" in any future render, and `most_similar` lists the nearest tracks in this set.

Example: a new acid line in this style would be a monophonic saw or square on a 16th sequence. Run it through a high-pass at about 150 Hz, then Overdrive and Chorus, then a second, resonant high-pass (resonance 0.25 – 0.7) whose cutoff you draw by hand. Use 15–20 ms attack and about 70 ms decay, and keep the spectral centre between roughly 450 Hz and 1.4 kHz, with the low end left to the toms.

## Files

| File | What it is |
|---|---|
| `set.json` | Full extraction: tracks, mixer, nested device trees and parameters, every clip with its notes, audio file references, automation resolved to parameter names, decoded Analog Lab presets |
| `midi_analysis.json` | Per-track MIDI features, song key, 160-bar activity timeline, sections |
| `audio_analysis.json` | Per-stem STFT features, including spectral centroid per bar (the filter-sweep curves) |
| `thumbprints.json` | The merged thumbprints and similarity results |
| `png/` | Log-frequency spectrogram of every stem |
| `extract_als.py`, `plugin_state.py`, `analyze_midi.py`, `analyze_audio.py`, `build_thumbprints.py` | The pipeline. It is read-only on the .als and can be rerun on any set |

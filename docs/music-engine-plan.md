# Music Engine: plan

A deterministic "physics engine for music". Claude makes the creative decisions; the engine guarantees the theory, timing and structure are right, then writes MIDI into Ableton Live or to .mid files. First genres: progressive house and techno. Reggae later.

## What the research says (and what we take from it)

**AI models compose poorly on their own** (ComposerX, arXiv 2407.21531, LilyBench 2026). They miscount bars, drift out of key and lose long-range structure. What works is a split: the AI plans with high-level attributes, a symbolic engine renders the notes, an automatic critic scores the result, and the AI revises a few times (MuseCoco, ByteComposer, Libretto 2026). Libretto's loop raised full-piece pass rates from 10/16 to 15/16, and showing reference pieces first helped further.

**Rule:** Claude never does bar math, timing or pitch spelling. The engine owns all of it.

**Representation:** a grid of integer steps grouped by bar (Libretto-style). Not ABC notation and not float beats. Melodic notes are written as scale degrees or chord tones, so a wrong-key note cannot be expressed.

**Libraries:** no runtime dependencies are needed.

- Write our own MIDI file writer (about 200 lines of standard Python).
- Write our own theory core as data tables, porting ideas from Tonal (MIT) and pychord (MIT).
- Model patterns on Scribbletune and isobar (both MIT).
- music21 (BSD) and mido (MIT) are used in tests only, as answer-checkers.
- Copy nothing from GPL/AGPL projects (mingus, SCAMP, TidalCycles, Strudel, musicpy). TidalCycles' license even covers "using the source as an algorithm reference", so its ideas are re-derived from its documentation only.

**Techniques worth implementing**, ranked by the research:

1. Repeatable randomness: `rng(seed, bar, step, lane)`, as in TidalCycles.
2. Step patterns with per-step velocity, probability and conditions (Elektron-style: "1st of every 4 loops", "fill only", "if previous played"). This is the best variation tool for techno.
3. Euclidean rhythms E(k, n, rotation).
4. A groove layer kept separate from the notes: swing % (MPC meaning) plus per-step velocity and timing templates (the GrooVAE idea).
5. A section and energy planner: per-bar energy drives layers, density, velocity and filter automation (the MorpheuS idea).
6. A theory core plus chord-derived bass and arps: offbeat, 3-3-2 syncopated, rolling 16ths, acid 303 (odd 13/15-step lengths, accents, slides).
7. Bounded mutation (Hamming-limited) and transforms: rotate, degrade, every-N.
8. Later: a TidalCycles-style mini-notation as a shorthand for Claude.

**Vocabulary:** name parameters after Live 12's MIDI Tools (Rhythm, Seed, Euclidean, Stacks, Ornament, Recombine) so they feel familiar.

## Genre specs (v1 style packs)

| | Progressive house | Peak-time techno | Melodic techno |
|---|---|---|---|
| Tempo | 122-128 | 128-135 | 122-124 |
| Drums | 4/4 kick, clap 2+4, offbeat hats + shaker, 3-step perc | 4/4 kick (+ ghost kicks), clap 2+4 + ghost claps, offbeat + 16th hats, open-hat variations | short 4/4 kick, clap 2+4, minimal hats |
| Bass | 3/16+3/16+2/16 syncopation, sidechained | rumble or rolling 16ths; acid 303 option | rolling 16th saw on the root |
| Harmony | minor key, open voicings, filter-opening plucks | stabs or drones, few chord changes | arp in the bass key, leads in 5ths/3rds |
| Phrases | 8/16/32 bars | 8/16 bars | 8/16/32 bars |
| Form (bars) | intro 16-32, breakdown 16-32, build 8-16, drop 32, outro 16-32 | e.g. 16 intro / 16 build / 32 drop / 16 break / 32 drop / outro | long breakdown with arp, big fills in the build |

Some details are marked unverified in the research (common keys, minimal-techno specifics). The packs are data files, so they're easy to correct by ear.

## Architecture

```
Claude (+ genre skill files)
  writes:  plan.json   key, BPM, sections {bars, energy, active roles}, chords per bar, per-role attributes, optional seed patterns
     |
Music Engine (Python 3.11+, standard library only)
  validate -> render N candidates -> critic -> feedback in musician terms
     |                                   ^
     |                                   +-- Claude revises (max K rounds), engine keeps the best so far
     v
  outputs: notes.json (bar/step + beats), .mid files, automation lanes (filter curves), explain text
     |
ableton-pp-cli  notes replace (with backup) / arrangement build / device add  -> Ableton Live
```

**Modules:**

- `rng`: seeded and position-keyed.
- `theory`: pitches, scales/modes, chords, voicings, Roman numerals, voice leading.
- `rhythm`: grid, step strings, euclid, probability/conditions, swing/groove templates.
- `parts`: kick, hats, clap, perc, bass archetypes, stabs/chords, arp, simple lead.
- `planner`: sections, energy curve, layer schedule, automation lanes.
- `transforms`: rotate, degrade, mutate, every-N, transpose.
- `critic`: gates and metrics.
- `io`: JSON schemas, MIDI file writer, Live note dicts.
- `analyze`: fingerprint MIDI or Live clips.

**Style packs** are TOML data files (`styles/prog_house.toml`, `styles/techno_peak.toml`, `styles/techno_melodic.toml`).

**CLI commands:**

- `engine validate plan.json`
- `engine render plan.json --candidates 4 --out song/`
- `engine part bass --style techno_peak --key Am --bars 8 --seed 7`
- `engine check notes.json`
- `engine analyze clip.mid`
- `engine explain`

Every command has `--json`.

## Critic (self-checks before anything reaches Live)

- **Hard gates:**
  - schema valid
  - bar count matches the plan
  - no notes past the clip end
  - pitch range per role
  - 100% in scale unless deliberately chromatic
- **Tonal:** scale consistency, pitch-class entropy, chord-tone ratio on strong beats, pitch classes used.
- **Rhythm:** empty-beat rate, groove consistency (bar-to-bar onset similarity), density compared with the planned energy curve.
- **Structure:** repetition ratio, novelty at section boundaries, copy-risk against reference clips.
- **Bands:** each metric is compared with percentile bands from a reference corpus (your own clips once we have them). Use several metrics together, not one.

## Your taste, without training a model

- `engine analyze` builds fingerprints of clips you like, and those become the target bands.
- The nearest reference clips are shown to Claude as examples before it writes a plan.
- Groove templates are extracted from your references and applied by the engine.
- Candidates are ranked by similarity to your corpus.
- A local accept/reject log re-weights the targets over time.

## Milestones

1. **v0.1, something to hear:**
   - rng, grid, step patterns with probability/conditions, euclid, swing
   - peak-time techno drums + rolling bass + one stab
   - the MIDI file writer
   - output: one 8-bar loop as .mid
   - tests: mido round-trip; music21 checks once the theory core exists
2. **v0.2, theory and prog house:**
   - scales, chords, voicings, voice leading, Roman numerals
   - chord-derived bass and arps
   - prog house pack
   - critic with the gates and core metrics
3. **v0.3, whole tracks:**
   - planner and energy curve, automation lanes
   - render to Live through ableton-pp-cli (`notes replace`, `arrangement build`)
4. **v0.4, taste:**
   - `analyze`, reference corpus bands, groove extraction, ranking, accept/reject log
   - genre skill files for Claude
   - reggae pack

## Things to decide

- Name and project folder (it needs a real folder, not this session's temporary one).
- How note probability reaches Live: Live 11+ supports per-note probability, but the bridges may not pass it through. Fallback: the engine renders the probability out into longer, varied clips.
- Whether v0.1 targets .mid files only (simplest) or also writes straight into Live through the scripts already installed.

## Sources (selected)

- Libraries: pypi.org JSON pages for music21, mido, pychord, musicpy, isobar; github.com/tonaljs/tonal; github.com/scribbletune/scribbletune; github.com/ideoforms/isobar
- Techniques: Toussaint, "The Euclidean Algorithm Generates Traditional Musical Rhythms"; strudel.cc/learn/mini-notation; tidalcycles.org/docs/reference/randomness; Elektronauts trig-condition threads; Attack Magazine swing articles; Herremans and Chew, MorpheuS; Yadati et al., ISMIR 2014; ableton.com/en/manual/midi-tools
- Genres: Attack Magazine "Beat Dissected" (rolling, thumping, dark techno); edmprod.com progressive house; loststoriesacademy.com DJ-friendly structure; theproducerschool.com melodic techno; musictech.com acid basslines
- AI composition: arxiv 2407.21531; ComposerX (arxiv 2404.18081); MuseCoco (arxiv 2306.00110); ByteComposer (arxiv 2402.17785); Libretto (arxiv 2606.22708); LilyBench (arxiv 2601.22764); MusPy metrics; Jazz Transformer metrics (arxiv 2008.01307); StyleRank (ISMIR 2019)
- Agent tools to learn from: leadsheet (PyPI, AGPL: ideas only), github.com/deer/music.build (Apache-2.0), github.com/ptaczek/daw-mcp (MIT)

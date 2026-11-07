# music-engine

A deterministic "physics engine for music". Claude makes the creative decisions; the engine owns timing, pitch and structure and renders them to MIDI. Standard-library Python 3.11+, no runtime dependencies, nothing leaves the machine.

The plan and research are in [docs/music-engine-plan.md](../docs/music-engine-plan.md). This is milestone **v0.1**.

## Use

Run `engine.cmd` from this folder (it uses `.venv` if present, otherwise `python`), or `python -m music_engine` with this folder on `PYTHONPATH`. Every command takes `--json`.

```
engine render --seed 7                       # 8 bars of peak-time techno -> out\techno_peak-Am-s7\
engine render --seed 8 --key F#m --bpm 130   # another variation
engine render plan.json                      # same settings from a file: style, key, bars, seed, bpm, swing, fill_every, parts
engine render --seed 7 --live                # also add the parts to Live as new tracks (SCRATCH sets only)
engine part drums --seed 7                   # print a step view of one part
engine part drums.kick --bars 4              # ...or one lane
engine euclid 5 13 --rotation 2              # print E(5,13) rotated
engine styles                                # list style packs
```

`render` writes `loop.mid` (one track per part; drop it into Live and each part becomes a track), `drums.mid`, `bass.mid`, `stab.mid`, and `notes.json` (every note with bar, step, tick and beat).

Same arguments, same bytes. Changing `--bars` keeps earlier bars the same, because every random choice is keyed by `(seed, bar, step, lane)`.

### Writing into Live

`--live` talks to the AbletonMCP Remote Script on `127.0.0.1:9877`. It only appends new MIDI tracks with one clip each; it never edits or deletes anything, and changes the tempo only with `--set-tempo`. It refuses unless the open set's name starts with `SCRATCH` (`--any-set` overrides). It never sends `drain_passive_events` or `get_session_snapshot`. The tracks arrive empty of instruments: drop a Drum Rack on the drums track and a synth on bass and stab. From v0.3 this goes through `ableton-pp-cli` instead.

Live's per-note Chance is not passed through this bridge. The engine rolls every chance itself, so the clip holds concrete notes; render more bars or other seeds for more variation.

## How a style is written

Style packs are TOML in `music_engine/styles/`. Each part has lanes; each lane is a step cycle:

- `pattern = "X... X... X... X..."`: 16ths, `X` accent, `x` normal, `o` ghost, `.` rest. Or `euclid = { density = 5, steps = 13, rotation = 2 }` (Live's Euclidean names). Cycles of any length drift against the bar.
- `steps = [{ step = 7, chance = 0.4 }, { step = 15, condition = "!pre" }]`: per-step `velocity`, `chance` (Live's name for probability) and `condition`:
  `A:B` (loop A of every B), `!A:B`, `fill`, `!fill`, `pre`, `!pre`, `first`, `!first`.
- Pitch: `pitch = 36` (drums), `degrees = [[1, 0.8], [8, 0.1]]` (weighted scale degrees) or `chord = [1, 3, 5, 7]`. Degrees can't leave the key.
- `groove` (0-1) scales the style's MPC swing for that lane; `velocity_deviation` adds seeded velocity variation.

Unknown keys are errors, so typos fail loudly.

## Tests

```
.venv\Scripts\python.exe -m unittest discover -s tests -t .
```

`mido` (MIT) is installed in `.venv` for one thing only: reading back the `.mid` files we write, as an independent check. The engine itself never imports it.

## Layout

| Module | What it does |
|---|---|
| `rng.py` | `rng(seed, bar, step, lane)`: position-keyed randomness (blake2b) |
| `grid.py` | integer step grid grouped by bar; ticks and beats; `Note` |
| `rhythm.py` | step strings, Euclid (Bjorklund), conditions, chance |
| `groove.py` | MPC swing as a separate timing layer |
| `pitch.py` | keys and scale degrees (the full theory core is v0.2) |
| `parts.py` | lane specs -> notes; the no-overlap / no-past-the-end guarantees |
| `render.py` | style + settings -> `Song`; text step view |
| `midi.py` | Standard MIDI File writer (format 1) |
| `live.py` | AbletonMCP bridge (v0.1 only) |
| `cli.py` | the `engine` command |

## Licences of ideas

No code is copied from anywhere. Ideas were taken only from MIT/BSD projects (Tonal, pychord, Scribbletune, isobar) and from published descriptions (Toussaint's Euclidean paper, Elektron trig conditions, MPC swing). Nothing from GPL/AGPL projects (mingus, SCAMP, TidalCycles, Strudel, musicpy, leadsheet).

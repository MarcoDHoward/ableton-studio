"""engine: the music engine's command line. Every command takes --json."""
import argparse
import json
import os
import sys

from . import __version__, live, midi, styles
from .render import grid_view, render
from .rhythm import euclid_string

SONG_KEYS = ('style', 'key', 'bars', 'seed', 'bpm', 'swing', 'fill_every', 'parts')


def _song_args(p):
    p.add_argument('--style', help='style name or .toml path (default techno_peak)')
    p.add_argument('--key', help='key, e.g. Am, F#m, C (default: the style\'s)')
    p.add_argument('--bars', type=int, help='bars to render (default 8)')
    p.add_argument('--seed', type=int, help='seed; same seed, same notes (default 1)')
    p.add_argument('--bpm', type=float, help='tempo (default: the style\'s)')
    p.add_argument('--swing', type=float, help='MPC swing %%, 50-75 (default: the style\'s)')
    p.add_argument('--fill-every', type=int, dest='fill_every', help='make the last bar of every N a fill bar; 0 = none')


def _settings(args, plan=None):
    s = {'style': 'techno_peak', 'bars': 8, 'seed': 1}
    if plan:
        unknown = set(plan) - set(SONG_KEYS)
        if unknown:
            raise ValueError(f'plan: unknown key(s) {", ".join(sorted(unknown))} (allowed: {", ".join(SONG_KEYS)})')
        s.update(plan)
    for k in SONG_KEYS:
        v = getattr(args, k, None)
        if v is not None:
            s[k] = v
    return s


def _render(s, parts=None):
    return render(s['style'], key=s.get('key'), bars=s['bars'], seed=s['seed'], bpm=s.get('bpm'),
                  swing=s.get('swing'), fill_every=s.get('fill_every'), parts=parts or s.get('parts'))


def _regen_command(song, out=None):
    cmd = f'engine render --style {song.style} --key {song.key} --bars {song.grid.bars} --bpm {song.bpm:g} --seed {song.seed}'
    return cmd + (f' --out "{out}"' if out else '')


def cmd_styles(args):
    out = []
    for name in styles.available():
        st = styles.load(name)
        out.append({'name': name, 'title': st.get('title', ''), 'key': st.get('key'),
                    'tempo': st.get('tempo', {}), 'parts': {p: list(v.get('lanes', {})) for p, v in st.get('parts', {}).items()}})
    if args.json:
        return out
    for s in out:
        t = s['tempo']
        print(f'{s["name"]:<14} {s["title"]}  ({t.get("min")}-{t.get("max")} BPM, default key {s["key"]})')
        for p, lanes in s['parts'].items():
            print(f'  {p}: {", ".join(lanes)}')


def cmd_euclid(args):
    pattern = euclid_string(args.density, args.steps, args.rotation)
    if args.json:
        return {'density': args.density, 'steps': args.steps, 'rotation': args.rotation, 'pattern': pattern,
                'hits': [i for i, c in enumerate(pattern) if c == 'x']}
    print(pattern)


def cmd_part(args):
    s = _settings(args)
    part, _, lane = args.role.partition('.')
    song = _render(s, parts=[part])
    if lane:
        if lane not in song.lanes(part):
            raise ValueError(f'part {part!r} has no lane {lane!r}')
        song.notes = [n for n in song.notes if n.lane == lane]
    written = None
    if args.out:
        os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
        midi.write_song(args.out, song)
        written = os.path.abspath(args.out)
    if args.json:
        d = song.to_dict()
        d['role'] = args.role
        d['file'] = written
        return d
    print(f'{song.style} {args.role}  key {song.key}  {song.bpm:g} BPM  seed {song.seed}  {song.grid.bars} bars  '
          f'{len(song.notes)} notes')
    print(grid_view(song, part, lane or None))
    if written:
        print(f'wrote {written}')


def cmd_render(args):
    plan = None
    if args.plan:
        with open(args.plan, encoding='utf-8') as fh:
            plan = json.load(fh)
    s = _settings(args, plan)
    song = _render(s)
    out = os.path.abspath(args.out or os.path.join('out', f'{song.style}-{song.key}-s{song.seed}'))
    files = {'loop': os.path.join(out, 'loop.mid'), 'notes': os.path.join(out, 'notes.json')}
    for name, _ in song.parts:
        files[name] = os.path.join(out, f'{name}.mid')
    result = {**song.summary(), 'out': out, 'files': files, 'dry_run': args.dry_run,
              'regenerate': _regen_command(song)}

    if args.dry_run:
        if args.live:
            result['live_commands'] = [(i, k) for i, k, _ in live.plan_commands(song, 'dry-run')]
    else:
        if args.live:
            live.require_scratch(args.any_set)  # refuse before writing anything
        os.makedirs(out, exist_ok=True)
        midi.write_song(files['loop'], song)
        for name, _ in song.parts:
            midi.write_song(files[name], song, parts=[name])
        with open(files['notes'], 'w', encoding='utf-8') as fh:
            json.dump(song.to_dict(), fh, indent=1)
        if args.live:
            result['live'] = live.write_song(song, set_tempo=args.set_tempo, any_set=args.any_set)

    if args.json:
        return result
    verb = 'would write' if args.dry_run else 'wrote'
    print(f'{song.style}  key {song.key}  {song.bpm:g} BPM  swing {song.swing:g}%  seed {song.seed}  '
          f'{song.grid.bars} bars  fills in bar(s) {result["fill_bars"] or "none"}')
    for name, count in result['parts'].items():
        print(f'  {name:<6} {count} notes')
    print(f'{verb} {files["loop"]}  (all parts, one track each)')
    print(f'{verb} ' + ', '.join(os.path.basename(files[n]) for n, _ in song.parts) + ', notes.json')
    if args.live and not args.dry_run:
        for w in result['live']:
            print(f'Live: new track {w["track_index"] + 1} "{w["part"]}" with {w["notes"]} notes')
    elif args.live:
        print(f'would add {len(song.parts)} new track(s) to Live')
    print(f'regenerate: {result["regenerate"]}')


def build_parser():
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument('--json', action='store_true', help='print machine-readable JSON')
    p = argparse.ArgumentParser(prog='engine', parents=[common],
                                description='Music engine: renders rhythm, pitch and structure into MIDI, deterministically.')
    p.add_argument('--version', action='version', version=f'music-engine {__version__}')
    sub = p.add_subparsers(dest='command', required=True)

    sp = sub.add_parser('styles', parents=[common], help='list style packs')
    sp.set_defaults(fn=cmd_styles)

    sp = sub.add_parser('euclid', parents=[common], help='print a Euclidean rhythm E(density, steps, rotation)')
    sp.add_argument('density', type=int)
    sp.add_argument('steps', type=int)
    sp.add_argument('--rotation', type=int, default=0, help='shift later by this many steps')
    sp.set_defaults(fn=cmd_euclid)

    sp = sub.add_parser('part', parents=[common], help='render one part (drums, bass, stab) or lane (drums.kick)')
    sp.add_argument('role')
    _song_args(sp)
    sp.add_argument('--out', help='also write this .mid file')
    sp.set_defaults(fn=cmd_part)

    sp = sub.add_parser('render', parents=[common], help='render every part to .mid files (and optionally into Live)')
    sp.add_argument('plan', nargs='?', help='optional plan.json with any of: ' + ', '.join(SONG_KEYS))
    _song_args(sp)
    sp.add_argument('--out', help='output folder (default out/<style>-<key>-s<seed>)')
    sp.add_argument('--live', action='store_true', help='also add the parts to Live as new tracks (AbletonMCP, port 9877)')
    sp.add_argument('--set-tempo', action='store_true', help='with --live: set Live\'s tempo to the render\'s')
    sp.add_argument('--any-set', action='store_true', help='with --live: allow a set whose name does not start with SCRATCH')
    sp.add_argument('--dry-run', action='store_true', help='show what would be written; write nothing')
    sp.set_defaults(fn=cmd_render)
    return p


def main(argv=None):
    args = build_parser().parse_args(argv)
    try:
        result = args.fn(args)
    except (ValueError, KeyError, OSError, live.LiveError) as e:
        msg = str(e) if not isinstance(e, KeyError) else f'missing {e}'
        if args.json:
            print(json.dumps({'error': msg}))
        else:
            print(f'engine: error: {msg}', file=sys.stderr)
        return 1
    if args.json:
        print(json.dumps(result, indent=1))
    return 0

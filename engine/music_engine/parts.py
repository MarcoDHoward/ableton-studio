"""Turn a style's lane specs into notes.

Every part is a set of lanes and every lane is a step cycle (see rhythm.py).
What differs is where a lane's pitch comes from:
  pitch = 36             a fixed pitch (drums)
  degrees = [[1, 0.8]]   one scale degree per step, chosen by weight (bass)
  chord = [1, 3, 5, 7]   several scale degrees at once (stabs)
"""
from .grid import Note
from .groove import Groove
from .rhythm import Step, check_condition, euclid, parse_steps, play
from .rng import rng, weighted

LANE_KEYS = {'pitch', 'pattern', 'euclid', 'char', 'chance', 'condition', 'velocity', 'duration',
             'groove', 'velocity_deviation', 'steps', 'register_low', 'degrees', 'chord'}
STEP_KEYS = {'step', 'char', 'velocity', 'chance', 'condition', 'degree'}
EUCLID_KEYS = {'density', 'steps', 'rotation'}


def _check_keys(where, got, allowed):
    unknown = set(got) - allowed
    if unknown:
        raise ValueError(f'{where}: unknown key(s) {", ".join(sorted(unknown))} (allowed: {", ".join(sorted(allowed))})')


def _velocity(v, where):
    if not isinstance(v, int) or not 1 <= v <= 127:
        raise ValueError(f'{where}: velocity {v!r} must be an integer 1-127')
    return v


def build_steps(spec, where):
    """A lane spec -> its step cycle [Step | None, ...]."""
    if ('pattern' in spec) == ('euclid' in spec):
        raise ValueError(f'{where}: give exactly one of pattern or euclid')
    if 'pattern' in spec:
        steps = parse_steps(spec['pattern'])
    else:
        e = spec['euclid']
        _check_keys(where + '.euclid', e, EUCLID_KEYS)
        hits = euclid(e['density'], e['steps'], e.get('rotation', 0))
        steps = [Step(char=spec.get('char', 'x')) if hit else None for hit in hits]
    for s in steps:
        if s is not None:
            s.chance = spec.get('chance', 1.0)
            s.condition = spec.get('condition')
    for i, o in enumerate(spec.get('steps', [])):
        w = f'{where}.steps[{i}]'
        _check_keys(w, o, STEP_KEYS)
        if 'step' not in o:
            raise ValueError(f'{w}: missing step')
        targets = o['step'] if isinstance(o['step'], list) else [o['step']]
        for t in targets:
            if not 0 <= t < len(steps):
                raise ValueError(f'{w}: step {t} is outside the {len(steps)}-step cycle')
            if steps[t] is None:
                steps[t] = Step(char=o.get('char', 'x'), chance=spec.get('chance', 1.0), condition=spec.get('condition'))
            s = steps[t]
            if 'char' in o:
                s.char = o['char']
            if 'velocity' in o:
                s.velocity = _velocity(o['velocity'], w)
            if 'chance' in o:
                s.chance = float(o['chance'])
            if 'condition' in o:
                s.condition = o['condition']
            if 'degree' in o:
                s.extra['degree'] = o['degree']
    for s in steps:
        if s is not None:
            check_condition(s.condition)
            if not 0 <= s.chance <= 1:
                raise ValueError(f'{where}: chance {s.chance} must be between 0 and 1')
    return steps


def render_lane(part, lane, spec, ctx):
    """Notes for one lane. `ctx` carries grid, key, seed, groove, velocity map and fill bars."""
    where = f'parts.{part}.lanes.{lane}'
    _check_keys(where, spec, LANE_KEYS)
    sources = [k for k in ('pitch', 'degrees', 'chord') if k in spec]
    if len(sources) != 1:
        raise ValueError(f'{where}: give exactly one of pitch, degrees or chord')
    grid, key, seed = ctx['grid'], ctx['key'], ctx['seed']
    name = f'{part}.{lane}'
    steps = build_steps(spec, where)
    vel_map = {**ctx['velocity'], **spec.get('velocity', {})}
    deviation = spec.get('velocity_deviation', 0)
    duration = max(1, round(spec.get('duration', 1) * grid.step_ticks))
    groove = ctx['groove'].scaled(spec.get('groove', 1.0))
    root = key.root_at_or_above(spec.get('register_low', 36))

    notes = []
    for bar, step, s in play(steps, grid.total_steps, grid.steps_per_bar, seed, name, ctx['fill_bars']):
        velocity = s.velocity if s.velocity is not None else vel_map[s.char]
        if deviation:
            velocity += round((rng(seed, bar, step, name + '.velocity') * 2 - 1) * deviation)
        velocity = max(1, min(127, velocity))
        if 'pitch' in spec:
            pitches = [spec['pitch']]
        elif 'degrees' in spec:
            degree = s.extra.get('degree') or weighted(seed, bar, step, name + '.degree', spec['degrees'])
            pitches = [key.degree(degree, root)]
        else:
            pitches = [key.degree(d, root) for d in spec['chord']]
        offset = groove.offset(step, grid)
        for p in pitches:
            if not 0 <= p <= 127:
                raise ValueError(f'{where}: pitch {p} is outside MIDI 0-127')
            notes.append(Note(part, lane, bar, step, p, velocity, duration, offset))
    return notes


def tidy(notes, grid):
    """Hard guarantees: no note past the clip end, no overlapping notes of the same pitch in a part."""
    by_voice = {}
    for n in notes:
        by_voice.setdefault((n.part, n.pitch), []).append(n)
    keep = []
    for voice in by_voice.values():
        voice.sort(key=lambda n: n.start(grid))
        for i, n in enumerate(voice):
            start = n.start(grid)
            limit = grid.end_tick - start
            if i + 1 < len(voice):
                limit = min(limit, voice[i + 1].start(grid) - start)
            if limit <= 0:
                continue  # a duplicate at the same instant
            n.duration = min(n.duration, limit)
            keep.append(n)
    keep.sort(key=lambda n: (n.start(grid), n.part, n.lane, n.pitch))
    return keep


def make_groove(style, swing=None):
    g = style.get('groove', {})
    return Groove(swing=float(swing if swing is not None else g.get('swing', 50)), swing_grid=g.get('swing_grid', 16))

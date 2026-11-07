"""Render a style into a Song: the one place that turns musical choices into notes."""
from dataclasses import dataclass, field

from . import styles
from .grid import Grid
from .parts import make_groove, render_lane, tidy
from .pitch import name as pitch_name, parse_key


@dataclass
class Song:
    style: str
    title: str
    key: object
    bpm: float
    seed: int
    swing: float
    grid: Grid
    parts: list  # [(name, channel), ...] in render order
    fill_bars: list
    lane_order: dict  # {part: [lane, ...]} in the style's order
    notes: list = field(default_factory=list)

    def part_notes(self, part):
        return [n for n in self.notes if n.part == part]

    def lanes(self, part):
        return list(self.lane_order.get(part, []))

    def summary(self):
        return {
            'style': self.style,
            'key': str(self.key),
            'bpm': self.bpm,
            'seed': self.seed,
            'swing': self.swing,
            'bars': self.grid.bars,
            'beats_per_bar': self.grid.beats_per_bar,
            'steps_per_bar': self.grid.steps_per_bar,
            'length_beats': self.grid.length_beats,
            'fill_bars': [b + 1 for b in self.fill_bars],
            'parts': {name: len(self.part_notes(name)) for name, _ in self.parts},
        }

    def to_dict(self):
        d = self.summary()
        d['ppq'] = self.grid.ppq
        d['notes'] = [n.to_dict(self.grid) for n in self.notes]
        return d


def render(style='techno_peak', *, key=None, bars=8, seed=1, bpm=None, swing=None, fill_every=None, parts=None):
    """Render `bars` bars of a style. Same arguments, same notes, every time."""
    spec = styles.load(style) if isinstance(style, str) else style
    grid = Grid(bars=bars)
    k = parse_key(key or spec.get('key', 'Am'))
    tempo = spec.get('tempo', {})
    bpm = float(bpm if bpm is not None else tempo.get('default', 120))
    if not 20 <= bpm <= 999:
        raise ValueError(f'bpm {bpm} is out of range')
    groove = make_groove(spec, swing)
    every = spec.get('fill_every', 0) if fill_every is None else fill_every
    fill_bars = [b for b in range(bars) if every and (b + 1) % every == 0]
    all_parts = spec.get('parts', {})
    wanted = parts or list(all_parts)
    for p in wanted:
        if p not in all_parts:
            raise ValueError(f'style {spec.get("name")!r} has no part {p!r} (has: {", ".join(all_parts)})')

    ctx = {'grid': grid, 'key': k, 'seed': int(seed), 'groove': groove,
           'velocity': spec.get('velocity', {'X': 120, 'x': 100, 'o': 64}), 'fill_bars': frozenset(fill_bars)}
    notes = []
    for p in wanted:
        for lane, lane_spec in all_parts[p].get('lanes', {}).items():
            notes += render_lane(p, lane, lane_spec, ctx)
    return Song(style=spec.get('name', 'custom'), title=spec.get('title', ''), key=k, bpm=bpm, seed=int(seed),
                swing=groove.swing, grid=grid, parts=[(p, all_parts[p].get('channel', 1)) for p in wanted],
                fill_bars=fill_bars, lane_order={p: list(all_parts[p].get('lanes', {})) for p in wanted},
                notes=tidy(notes, grid))


def grid_view(song, part, only_lane=None):
    """A text step view, one row per lane per bar: X loud, x medium, o soft, . rest. Chords show their top velocity."""
    steps = song.grid.steps_per_bar
    rows = []
    for lane in song.lanes(part):
        if only_lane and lane != only_lane:
            continue
        cells = {}
        for n in song.part_notes(part):
            if n.lane == lane:
                cells[(n.bar, n.step)] = max(cells.get((n.bar, n.step), 0), n.velocity)
        for bar in range(song.grid.bars):
            line = ''
            for s in range(steps):
                v = cells.get((bar, s))
                line += '.' if v is None else 'X' if v >= 105 else 'x' if v >= 75 else 'o'
                if s % 4 == 3 and s < steps - 1:
                    line += ' '
            fill = '  fill' if bar in song.fill_bars else ''
            rows.append(f'{lane:<11} {bar + 1:>3}  {line}{fill}')
        counts = {}
        for n in song.part_notes(part):
            if n.lane == lane:
                counts[n.pitch] = counts.get(n.pitch, 0) + 1
        if len(counts) > 1:
            rows.append(f'{"":<11} pitches  ' + ', '.join(f'{pitch_name(p)} x{c}' for p, c in sorted(counts.items())))
    return '\n'.join(rows)

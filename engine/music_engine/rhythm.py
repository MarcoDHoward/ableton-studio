"""Step patterns: Euclidean rhythms, step strings, chance and trig conditions.

A lane is a cycle of steps. Each step that can fire carries a velocity, a chance
(probability, Live's "Chance") and an optional condition (Elektron-style trig
conditions). The cycle may be any length, so a 13-step lane drifts against the
16-step bar (polymeter).

Conditions:
  A:B    play on loop A of every B loops (loops count from 1), e.g. 1:4
  !A:B   every loop except that one
  fill   only in fill bars;          !fill  never in fill bars
  pre    only if the previous conditional step in this lane played;  !pre  only if it did not
  first  only on the first loop;     !first never on the first loop

A step's condition is checked first, then its chance. A step "is conditional"
when it has a condition or a chance below 1; only those set what `pre` sees.
"""
import re
from dataclasses import dataclass, field

from .rng import chance as roll

REST = set('.-_')
IGNORED = set(' |')
_AB = re.compile(r'^(!?)(\d+):(\d+)$')
_NAMED = {'fill', 'pre', 'first'}


def euclid(density, steps, rotation=0):
    """E(k, n, rotation) by Bjorklund's algorithm: `density` hits spread as evenly as possible over `steps`.

    Rotation moves the pattern later by that many steps (wrapping), so E(3,8,1) = '.x..x..x'.
    """
    if steps < 1:
        raise ValueError('steps must be at least 1')
    if not 0 <= density <= steps:
        raise ValueError(f'density must be between 0 and steps ({steps})')
    if density == 0:
        seq = [False] * steps
    else:
        a = [[True] for _ in range(density)]
        b = [[False] for _ in range(steps - density)]
        while len(b) > 1:
            m = min(len(a), len(b))
            rest = a[m:] if len(a) > m else b[m:]
            a = [a[i] + b[i] for i in range(m)]
            b = rest
        seq = [x for group in a + b for x in group]
    r = rotation % steps
    return seq[-r:] + seq[:-r] if r else seq


def euclid_string(density, steps, rotation=0):
    return ''.join('x' if hit else '.' for hit in euclid(density, steps, rotation))


def check_condition(text):
    """Raise ValueError if `text` is not a known condition."""
    if text is None:
        return
    body = text[1:] if text.startswith('!') else text
    m = _AB.match(text)
    if m:
        a, b = int(m.group(2)), int(m.group(3))
        if not 1 <= a <= b:
            raise ValueError(f'condition {text!r}: need 1 <= A <= B')
        return
    if body not in _NAMED:
        raise ValueError(f'unknown condition {text!r} (use A:B, !A:B, fill, pre, first, or ! before a name)')


def condition_met(text, loop, is_fill, pre):
    """Is condition `text` true on 0-based `loop`? `pre` is the previous conditional result in the lane (None if none yet)."""
    if text is None:
        return True
    m = _AB.match(text)
    if m:
        hit = loop % int(m.group(3)) == int(m.group(2)) - 1
        return not hit if m.group(1) else hit
    negate = text.startswith('!')
    name = text[1:] if negate else text
    if name == 'fill':
        hit = is_fill
    elif name == 'pre':
        hit = bool(pre)
    elif name == 'first':
        hit = loop == 0
    else:
        raise ValueError(f'unknown condition {text!r}')
    return not hit if negate else hit


@dataclass
class Step:
    char: str = 'x'  # velocity class: X accent, x normal, o ghost (looked up in the lane's velocity map)
    velocity: int = None  # explicit velocity; overrides the char
    chance: float = 1.0
    condition: str = None
    extra: dict = field(default_factory=dict)  # role-specific fields, e.g. {'degree': 1}

    @property
    def conditional(self):
        return self.condition is not None or self.chance < 1


def parse_steps(text, char_ok='Xxo'):
    """'X...x.o.' -> [Step('X'), None, None, None, Step('x'), ...]. Spaces and | are ignored, so 'X... X...' reads well."""
    out = []
    for ch in text:
        if ch in IGNORED:
            continue
        if ch in REST:
            out.append(None)
        elif ch in char_ok:
            out.append(Step(char=ch))
        else:
            raise ValueError(f'pattern {text!r}: {ch!r} is not one of {char_ok!r} or a rest (. - _)')
    if not out:
        raise ValueError('pattern is empty')
    return out


def play(steps, total_steps, steps_per_bar, seed, lane, fill_bars=frozenset()):
    """Run a lane's step cycle over `total_steps` grid steps.

    Returns [(bar, step_in_bar, Step), ...] for every step that fires, in time order.
    The cycle restarts every len(steps) steps; that count is the loop number conditions use.
    """
    length = len(steps)
    pre = None
    fired = []
    for g in range(total_steps):
        s = steps[g % length]
        if s is None:
            continue
        bar, step = divmod(g, steps_per_bar)
        loop = g // length
        ok = condition_met(s.condition, loop, bar in fill_bars, pre)
        if ok:
            ok = roll(seed, bar, step, lane + '.chance', s.chance)
        if s.conditional:
            pre = ok
        if ok:
            fired.append((bar, step, s))
    return fired

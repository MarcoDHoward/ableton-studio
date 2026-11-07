"""Just enough pitch to place parts in a key. The full theory core (chords, voicings, Roman numerals) is v0.2.

Melodic material is written as scale degrees (1 = root, 8 = root an octave up),
so an out-of-key note cannot be expressed. Note names follow Live: C3 = MIDI 60.
"""
import re
from dataclasses import dataclass

NAMES = ['C', 'C#', 'D', 'D#', 'E', 'F', 'F#', 'G', 'G#', 'A', 'A#', 'B']
_PC = {'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11}
SCALES = {
    'major': (0, 2, 4, 5, 7, 9, 11),
    'minor': (0, 2, 3, 5, 7, 8, 10),  # natural minor (aeolian)
}
_KEY = re.compile(r'^([A-Ga-g])([#b]?)\s*(m|min|minor|maj|major)?$')


def name(pitch):
    """60 -> 'C3' (Live's octave numbering)."""
    return f'{NAMES[pitch % 12]}{pitch // 12 - 2}'


@dataclass(frozen=True)
class Key:
    root: int  # pitch class 0-11
    mode: str  # 'major' or 'minor'

    @property
    def scale(self):
        return SCALES[self.mode]

    def __str__(self):
        return NAMES[self.root] + ('m' if self.mode == 'minor' else '')

    def root_at_or_above(self, low):
        """The lowest root pitch >= `low`."""
        return low + (self.root - low) % 12

    def degree(self, degree, root_pitch):
        """Scale degree (1-based; 8 = octave, 9 = 2nd an octave up) above `root_pitch`."""
        if degree < 1:
            raise ValueError('scale degrees start at 1')
        octave, index = divmod(degree - 1, 7)
        return root_pitch + 12 * octave + self.scale[index]

    def contains(self, pitch):
        return (pitch - self.root) % 12 in self.scale


def parse_key(text):
    """'Am', 'A minor', 'F#m', 'Bb', 'C major' -> Key."""
    m = _KEY.match(text.strip())
    if not m:
        raise ValueError(f'cannot read key {text!r} (try Am, F#m, Bb, C major)')
    letter, acc, quality = m.groups()
    root = _PC[letter.upper()] + {'#': 1, 'b': -1, '': 0}[acc]
    mode = 'minor' if quality in ('m', 'min', 'minor') else 'major'
    return Key(root % 12, mode)

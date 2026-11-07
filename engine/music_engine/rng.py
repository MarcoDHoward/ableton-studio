"""Position-keyed, seeded randomness.

Every random decision is a pure function of (seed, bar, step, lane), so:
  * the same seed always renders the same notes, on any machine and Python version;
  * rendering 4 bars gives exactly the first 4 bars of the 8-bar render;
  * changing one lane never reshuffles another lane.

Python's hash() is salted per process and random.Random's stream depends on call
order, so neither is used. blake2b over a fixed text key is stable everywhere.
"""
import hashlib


def rng(seed, bar, step, lane):
    """A float in [0, 1) fixed by its position. `lane` names the decision, e.g. 'drums.kick.chance'."""
    key = f'{int(seed)}|{int(bar)}|{int(step)}|{lane}'.encode('utf-8')
    digest = hashlib.blake2b(key, digest_size=8).digest()
    return int.from_bytes(digest, 'big') / 2.0 ** 64


def chance(seed, bar, step, lane, p):
    """True with probability p. p >= 1 is always True and p <= 0 always False, without drawing."""
    if p >= 1:
        return True
    if p <= 0:
        return False
    return rng(seed, bar, step, lane) < p


def rng_int(seed, bar, step, lane, lo, hi):
    """An int in [lo, hi], inclusive."""
    return lo + int(rng(seed, bar, step, lane) * (hi - lo + 1))


def weighted(seed, bar, step, lane, choices):
    """Pick from [(value, weight), ...] by weight."""
    total = sum(w for _, w in choices)
    if total <= 0:
        raise ValueError('weights must sum to more than 0')
    r = rng(seed, bar, step, lane) * total
    for value, w in choices:
        r -= w
        if r < 0:
            return value
    return choices[-1][0]

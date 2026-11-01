"""Glitch guard for real-time recordings (resampling).

Offline export can't glitch; real-time recording can. Before a resampled round is scored, check it for:
  * dropouts: runs of digital silence (|x| < 1e-6 on both channels) of 3 ms or more *inside* the sound,
    i.e. between the first and last moment the recording is above -50 dBFS;
  * clicks: single-sample jumps far larger than the signal's own local movement;
  * wrong length: more than 5 % away from the expected duration.

    python harness/glitch.py <wav> [--expect-seconds 3.97] [--json]
"""
import argparse
import json
import os
import sys

import numpy as np

sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), 'analysis'))
from analyze_audio import load  # noqa: E402

DROPOUT_MS = 3.0
CLICK_RATIO = 25.0       # jump vs local mean absolute step
CLICK_MIN = 0.2          # and at least this much of full scale


def check_signal(x, sr, expect_seconds=None):
    """x: float array (n, channels). Returns {'ok': bool, 'problems': [...]}."""
    problems = []
    seconds = len(x) / sr
    if expect_seconds and abs(seconds - expect_seconds) > 0.05 * expect_seconds:
        problems.append({'kind': 'length', 'detail': '%.2f s recorded, %.2f s expected' % (seconds, expect_seconds)})
    mono = np.max(np.abs(x), axis=1)
    loud = np.where(mono > 10 ** (-50 / 20))[0]
    if len(loud) == 0:
        problems.append({'kind': 'silent', 'detail': 'nothing above -50 dBFS'})
        return {'ok': False, 'seconds': round(seconds, 3), 'problems': problems}
    a, b = loud[0], loud[-1]
    inner = mono[a:b + 1] < 1e-6
    # runs of exact silence inside the sound
    if inner.any():
        edges = np.diff(np.concatenate(([0], inner.astype(np.int8), [0])))
        starts, ends = np.where(edges == 1)[0], np.where(edges == -1)[0]
        lengths = ends - starts
        min_len = int(sr * DROPOUT_MS / 1000)
        for s, n in zip(starts, lengths):
            if n >= min_len:
                problems.append({'kind': 'dropout', 'at_s': round((a + s) / sr, 3), 'ms': round(n / sr * 1000, 1)})
    # clicks: compare each step with the mean step size around it (±2 ms)
    step = np.abs(np.diff(x, axis=0)).max(axis=1)
    w = max(3, int(sr * 0.002))
    local = np.convolve(step, np.ones(2 * w + 1) / (2 * w + 1), mode='same')
    local_excl = (local * (2 * w + 1) - step) / (2 * w)          # leave the step itself out
    cand = np.where((step > CLICK_MIN) & (step > CLICK_RATIO * (local_excl + 1e-6)))[0]
    for i in cand[:20]:
        problems.append({'kind': 'click', 'at_s': round(i / sr, 3), 'jump': round(float(step[i]), 3)})
    return {'ok': not problems, 'seconds': round(seconds, 3), 'problems': problems[:20]}


def check_file(path, expect_seconds=None):
    sr, x = load(path)
    return check_signal(x, sr, expect_seconds)


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('wav')
    ap.add_argument('--expect-seconds', type=float)
    ap.add_argument('--json', action='store_true')
    a = ap.parse_args(argv)
    r = check_file(a.wav, a.expect_seconds)
    if a.json:
        print(json.dumps(r, indent=1))
    else:
        print('clean' if r['ok'] else 'GLITCHES: ' + '; '.join(
            '%s %s' % (p['kind'], {k: v for k, v in p.items() if k != 'kind'}) for p in r['problems']))
    return 0 if r['ok'] else 2


if __name__ == '__main__':
    sys.exit(main())

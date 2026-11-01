"""Tests for the resampling glitch guard. Run: python -m unittest harness/test_glitch.py"""
import os
import sys
import unittest

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from glitch import check_signal

SR = 44100


def saw_bass(seconds=4.0, f=55.0):
    """A plucky 16th-note saw line, like a resampled bass round."""
    t = np.arange(int(SR * seconds)) / SR
    saw = 2 * ((t * f) % 1.0) - 1
    env = np.exp(-((t % 0.124) / 0.04))           # 16ths at 121 BPM, short decay
    x = 0.5 * saw * env
    return np.stack([x, x], 1)


def hats(seconds=4.0):
    rng = np.random.default_rng(7)
    t = np.arange(int(SR * seconds)) / SR
    x = 0.4 * rng.standard_normal(len(t)) * np.exp(-((t % 0.124) / 0.01))
    return np.stack([x, x * 0.9], 1)


class Glitch(unittest.TestCase):
    def test_clean_material_passes(self):
        for sig in (saw_bass(), hats()):
            self.assertTrue(check_signal(sig, SR, expect_seconds=4.0)['ok'])

    def test_dropout_found(self):
        x = saw_bass()
        x[SR:SR + int(0.01 * SR)] = 0.0
        kinds = [p['kind'] for p in check_signal(x, SR)['problems']]
        self.assertIn('dropout', kinds)

    def test_short_gap_is_not_a_dropout(self):
        x = saw_bass()
        x[SR:SR + int(0.001 * SR)] = 0.0              # 1 ms: zero crossings and note gaps look like this
        self.assertNotIn('dropout', [p['kind'] for p in check_signal(x, SR)['problems']])

    def test_click_found(self):
        x = hats()
        x[2 * SR] += 0.7
        self.assertIn('click', [p['kind'] for p in check_signal(x, SR)['problems']])

    def test_wrong_length(self):
        self.assertIn('length', [p['kind'] for p in check_signal(saw_bass(3.0), SR, expect_seconds=4.0)['problems']])

    def test_silence(self):
        r = check_signal(np.zeros((SR, 2)), SR)
        self.assertFalse(r['ok'])
        self.assertEqual(r['problems'][0]['kind'], 'silent')

    def test_silence_before_and_after_is_fine(self):
        x = np.concatenate([np.zeros((SR // 2, 2)), saw_bass(2.0), np.zeros((SR // 2, 2))])
        self.assertTrue(check_signal(x, SR)['ok'])


if __name__ == '__main__':
    unittest.main()

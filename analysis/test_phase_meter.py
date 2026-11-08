"""Tests for the phase meter. Run: python -m unittest analysis/test_phase_meter.py"""
import os
import sys
import tempfile
import unittest

import numpy as np
from scipy.io import wavfile
from scipy.signal import butter, sosfilt

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import phase_meter as pm  # noqa: E402

SR = 44100
RNG = np.random.default_rng(3)


def noise(seconds=6.0):
    return (0.2 * RNG.standard_normal(int(SR * seconds))).astype(np.float32)


def lowpassed(x, hz=120):
    return sosfilt(butter(4, hz, 'low', fs=SR, output='sos'), x).astype(np.float32)


def kick(seconds=6.0, f=55.0):
    t = np.arange(int(SR * seconds)) / SR
    env = np.exp(-(t % 0.496) / 0.12)
    return (0.6 * np.sin(2 * np.pi * f * (t % 0.496)) * env).astype(np.float32)


class Meter(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()

    def wav(self, name, left, right):
        path = os.path.join(self.dir, name + '.wav')
        wavfile.write(path, SR, np.stack([left, right], 1).astype(np.float32))
        return path

    def test_mono_is_correlated_and_lossless(self):
        x = noise()
        r, _ = pm.measure(self.wav('mono', x, x))
        self.assertGreater(r['broadband']['corr'], 0.99)
        self.assertGreater(r['broadband']['mono_loss_db'], -0.05)
        self.assertEqual(r['flags'], [])

    def test_uncorrelated_loses_3db(self):
        r, _ = pm.measure(self.wav('wide', noise(), noise()))
        self.assertLess(abs(r['broadband']['corr']), 0.05)
        self.assertAlmostEqual(r['broadband']['mono_loss_db'], -3.0, delta=0.2)
        self.assertAlmostEqual(r['broadband']['width_pct'], 50, delta=3)

    def test_antiphase_is_flagged(self):
        x = noise()
        r, _ = pm.measure(self.wav('inv', x, -x))
        self.assertLess(r['broadband']['corr'], -0.99)
        self.assertLess(r['broadband']['mono_loss_db'], -30)
        self.assertTrue(any('anti-phase' in f for f in r['flags']))

    def test_haas_delay_in_bass_shows_lag_and_low_corr(self):
        x = lowpassed(noise())
        d = int(0.003 * SR)                       # 3 ms Haas offset on the right channel
        r, _ = pm.measure(self.wav('haas', x, np.r_[np.zeros(d, np.float32), x[:-d]]))
        self.assertLess(r['low']['corr'], 0.9)
        self.assertAlmostEqual(abs(r['low']['lag_ms']), 3.0, delta=0.1)
        self.assertTrue(any('low end not mono' in f for f in r['flags']))

    def test_bands_split_wide_top_from_mono_bass(self):
        b = lowpassed(noise(), 100)
        hp = butter(6, 1000, 'high', fs=SR, output='sos')
        top_l, top_r = sosfilt(hp, noise()), sosfilt(hp, noise())
        r, _ = pm.measure(self.wav('split', b + 0.3 * top_l, b + 0.3 * top_r))
        self.assertGreater(r['bands']['40-60']['corr'], 0.98)
        self.assertLess(abs(r['bands']['4k-10k']['corr']), 0.1)

    def test_pair_check_finds_kick_bass_cancellation(self):
        k = kick()
        d = os.path.join(self.dir, 'Song Stems')
        os.makedirs(d)
        for name, sig in (('Kick', k), ('Bass', -0.8 * k)):
            wavfile.write(os.path.join(d, 'Song Stems %s.wav' % name), SR, np.stack([sig, sig], 1))
        res = pm.main(d)
        p = res['pairs'][0]
        self.assertLess(p['corr'], -0.9)
        self.assertLess(p['sum_gain_db'], -6)
        self.assertLess(p['corr_at_best_lag'], -0.9)

    def test_tempo_gives_bars(self):
        x = noise()
        r, _ = pm.measure(self.wav('bars', x, x), tempo=120)
        self.assertEqual(len(r['per_bar']), 3)

    def test_bar_window_and_start_bar(self):
        x = noise()                                # 6 s at 120 BPM = 3 bars
        wide = np.r_[x[:SR * 4], noise()[:SR * 2]]  # bars 1-2 mono, bar 3 uncorrelated
        r, _ = pm.measure(self.wav('win', x, wide), tempo=120, bars=(12, 12), start_bar=10)
        self.assertLess(abs(r['broadband']['corr']), 0.1)
        self.assertEqual([b['bar'] for b in r['per_bar']], [12])


if __name__ == '__main__':
    unittest.main()

"""Tests for the stem-sum simulator. Run: python -m unittest analysis/test_phase_optimize.py"""
import os
import sys
import tempfile
import unittest

import numpy as np
from scipy.io import wavfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import phase_meter as pm  # noqa: E402
import phase_optimize as po  # noqa: E402

SR = 44100
RNG = np.random.default_rng(5)


def to16(x):
    return (np.clip(x, -1, 1) * 32767).astype(np.int16)


class Simulator(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        d = tempfile.mkdtemp()
        n = SR * 8
        mono = 0.2 * RNG.standard_normal(n)
        wide_l, wide_r = 0.2 * RNG.standard_normal(n), 0.2 * RNG.standard_normal(n)
        cls.stems = {'Kick': (mono, mono), 'Lead': (wide_l, wide_r), 'Pad': (0.5 * wide_l, -0.5 * wide_l + 0.1 * wide_r)}
        cls.files = []
        for name, (l, r) in cls.stems.items():
            p = os.path.join(d, 'S %s.wav' % name)
            wavfile.write(p, SR, np.stack([to16(l), to16(r)], 1)); cls.files.append(p)
        tot_l = sum(l for l, _ in cls.stems.values()); tot_r = sum(r for _, r in cls.stems.values())
        cls.sum_path = os.path.join(d, 'sum.wav')
        wavfile.write(cls.sum_path, SR, np.stack([to16(tot_l), to16(tot_r)], 1))
        cls.G = po.build_gram(cls.files, 120)
        cls.model = po.Model(cls.G, list(cls.stems), ['Lead', 'Pad'],
                             space={'Lead': ['width', 'bass_mono'], 'Pad': ['width', 'left_inv']})
        cls.x0 = np.array([{'width': 1.0, 'bass_mono': 60.0, 'main_bass': 120.0}.get(k, 20.0) for _, k in cls.model.cont])

    def p(self, over):
        x = self.x0.copy()
        for i, (n, k) in enumerate(self.model.cont):
            if (n, k) in over: x[i] = over[(n, k)]
        return x

    def test_identity_matches_meter_on_the_real_sum(self):
        st = self.model.stats(self.x0, {})
        r, _ = pm.measure(self.sum_path)
        self.assertAlmostEqual(st['all']['corr'], r['broadband']['corr'], delta=0.02)
        self.assertAlmostEqual(st['bands']['1k-4k']['mono_loss_db'], r['bands']['1k-4k']['mono_loss_db'], delta=0.1)

    def test_half_width_cuts_side_by_6db(self):
        a = self.model.stats(self.x0, {})['stems']['Lead']['1k-4k']
        st = self.model.stats(self.p({('Lead', 'width'): 0.5}), {})
        b = st['stems']['Lead']['1k-4k']
        # uncorrelated noise: loss -3 dB; with side halved: mono / (mono + side/4) = 1/1.25 -> -0.97 dB
        self.assertAlmostEqual(a[1], -3.0, delta=0.2)
        self.assertAlmostEqual(b[1], -0.97, delta=0.2)

    def test_left_inv_turns_side_heavy_stem_mid_heavy(self):
        loss0 = self.model.stats(self.x0, {})['stems']['Pad']['1k-4k'][1]
        loss1 = self.model.stats(self.x0, {('Pad', 'left_inv'): True})['stems']['Pad']['1k-4k'][1]
        self.assertLess(loss0, -6)
        self.assertGreater(loss1, -1)

    def test_eval_spec_matches_direct_parameters(self):
        self.assertEqual(po.parse_spec('Lead=0.5, Pad:left_inv=1, Main:main_bass=150'),
                         {('Lead', 'width'): 0.5, ('Pad', 'left_inv'): 1.0, ('Main', 'main_bass'): 150.0})
        p = self.x0.copy(); p[self.model.cont.index(('Lead', 'width'))] = 0.5
        direct, _ = po.objective(self.model, p, {}, 0)
        via_spec, _ = po.evaluate(self.model, 'Lead=0.5', 0)
        self.assertAlmostEqual(direct, via_spec, places=9)
        with self.assertRaises(SystemExit):
            po.evaluate(self.model, 'Kick=0.5', 0)       # Kick is not in the search space


if __name__ == '__main__':
    unittest.main()

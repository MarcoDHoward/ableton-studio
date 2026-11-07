"""rng, grid, rhythm (euclid, step strings, conditions, chance) and groove."""
import unittest

from music_engine.grid import Grid
from music_engine.groove import Groove
from music_engine.pitch import name, parse_key
from music_engine.rhythm import Step, condition_met, check_condition, euclid, euclid_string, parse_steps, play
from music_engine.rng import chance, rng, rng_int, weighted


class RngTest(unittest.TestCase):
    def test_pinned_values(self):
        # If these change, every seed the user has saved renders differently. Do not update them casually.
        self.assertEqual(rng(7, 0, 0, 'drums.kick.chance'), 0.8279290506823362)
        self.assertEqual(rng(1, 3, 5, 'bass.rolling.degree'), 0.09848144133601125)

    def test_range_and_position_keying(self):
        values = {rng(1, b, s, 'x') for b in range(8) for s in range(16)}
        self.assertEqual(len(values), 128)
        self.assertTrue(all(0 <= v < 1 for v in values))
        self.assertNotEqual(rng(1, 0, 0, 'a'), rng(1, 0, 0, 'b'))
        self.assertNotEqual(rng(1, 0, 0, 'a'), rng(2, 0, 0, 'a'))
        self.assertNotEqual(rng(1, 0, 1, 'a'), rng(1, 1, 0, 'a'))

    def test_roughly_uniform(self):
        n = 4000
        hits = sum(chance(3, i // 16, i % 16, 'u', 0.25) for i in range(n))
        self.assertAlmostEqual(hits / n, 0.25, delta=0.03)

    def test_chance_edges_and_helpers(self):
        self.assertTrue(chance(1, 0, 0, 'a', 1))
        self.assertFalse(chance(1, 0, 0, 'a', 0))
        self.assertTrue(all(2 <= rng_int(1, 0, s, 'i', 2, 4) <= 4 for s in range(200)))
        self.assertEqual(weighted(1, 0, 0, 'w', [('only', 1)]), 'only')
        with self.assertRaises(ValueError):
            weighted(1, 0, 0, 'w', [('a', 0)])


class GridTest(unittest.TestCase):
    def test_ticks(self):
        g = Grid(bars=8)
        self.assertEqual((g.steps_per_bar, g.step_ticks, g.total_steps), (16, 240, 128))
        self.assertEqual(g.tick(1, 4), (16 + 4) * 240)
        self.assertEqual(g.end_tick, 8 * 4 * 960)
        self.assertEqual(len(list(g.positions())), 128)
        with self.assertRaises(ValueError):
            Grid(bars=0)


class EuclidTest(unittest.TestCase):
    def test_known_rhythms(self):
        # Toussaint, "The Euclidean Algorithm Generates Traditional Musical Rhythms"
        self.assertEqual(euclid_string(3, 8), 'x..x..x.')  # tresillo
        self.assertEqual(euclid_string(5, 8), 'x.xx.xx.')  # cinquillo
        self.assertEqual(euclid_string(2, 5), 'x.x..')
        self.assertEqual(euclid_string(4, 16), 'x...x...x...x...')
        self.assertEqual(euclid_string(0, 4), '....')
        self.assertEqual(euclid_string(4, 4), 'xxxx')

    def test_maximally_even(self):
        for n in range(1, 25):
            for k in range(1, n + 1):
                hits = [i for i, h in enumerate(euclid(k, n)) if h]
                self.assertEqual(len(hits), k)
                gaps = [(hits[(i + 1) % k] - hits[i]) % n or n for i in range(k)]
                self.assertLessEqual(max(gaps) - min(gaps), 1, (k, n))

    def test_rotation(self):
        self.assertEqual(euclid_string(3, 8, 1), '.x..x..x')
        self.assertEqual(euclid_string(3, 8, 8), euclid_string(3, 8))
        self.assertEqual(euclid_string(3, 8, -1), '..x..x.x')

    def test_bad_args(self):
        with self.assertRaises(ValueError):
            euclid(5, 4)
        with self.assertRaises(ValueError):
            euclid(1, 0)


class StepPatternTest(unittest.TestCase):
    def test_parse(self):
        steps = parse_steps('X..o x-_. |')
        self.assertEqual([s.char if s else None for s in steps], ['X', None, None, 'o', 'x', None, None, None])
        with self.assertRaises(ValueError):
            parse_steps('X..?')
        with self.assertRaises(ValueError):
            parse_steps('  ')

    def test_conditions(self):
        self.assertEqual([condition_met('1:4', loop, False, None) for loop in range(8)],
                         [True, False, False, False, True, False, False, False])
        self.assertEqual([condition_met('!3:4', loop, False, None) for loop in range(4)], [True, True, False, True])
        self.assertTrue(condition_met('fill', 0, True, None))
        self.assertFalse(condition_met('fill', 0, False, None))
        self.assertTrue(condition_met('!fill', 0, False, None))
        self.assertTrue(condition_met('pre', 3, False, True))
        self.assertFalse(condition_met('pre', 3, False, None))
        self.assertTrue(condition_met('!pre', 3, False, False))
        self.assertTrue(condition_met('first', 0, False, None))
        self.assertFalse(condition_met('first', 1, False, None))
        for bad in ('0:4', '5:4', 'sometimes', '!'):
            with self.assertRaises(ValueError):
                check_condition(bad)

    def test_play_conditions_by_loop(self):
        steps = [Step('x', condition='2:4')] + [None] * 15
        fired = play(steps, 16 * 8, 16, seed=1, lane='t')
        self.assertEqual([bar for bar, _, _ in fired], [1, 5])

    def test_play_fill(self):
        steps = [Step('x', condition='fill'), Step('x', condition='!fill')] + [None] * 14
        fired = play(steps, 16 * 4, 16, seed=1, lane='t', fill_bars={3})
        self.assertEqual([(b, s) for b, s, _ in fired], [(0, 1), (1, 1), (2, 1), (3, 0)])

    def test_play_pre_alternates(self):
        steps = [None] * 16
        steps[7] = Step('o', chance=0.5)
        steps[15] = Step('o', condition='!pre')
        fired = play(steps, 16 * 32, 16, seed=9, lane='t')
        by_bar = {}
        for bar, step, _ in fired:
            by_bar.setdefault(bar, []).append(step)
        self.assertEqual(len(by_bar), 32)  # exactly one of the two plays in every bar
        self.assertTrue(all(len(v) == 1 for v in by_bar.values()))
        self.assertEqual({tuple(v) for v in by_bar.values()}, {(7,), (15,)})

    def test_chance_is_deterministic_and_prefix_stable(self):
        steps = [Step('x', chance=0.5) for _ in range(16)]
        a = play(steps, 16 * 8, 16, seed=4, lane='t')
        b = play(steps, 16 * 8, 16, seed=4, lane='t')
        short = play(steps, 16 * 4, 16, seed=4, lane='t')
        self.assertEqual([(x, y) for x, y, _ in a], [(x, y) for x, y, _ in b])
        self.assertEqual([(x, y) for x, y, _ in short], [(x, y) for x, y, _ in a if x < 4])
        self.assertTrue(30 < len(a) < 98)

    def test_polymeter(self):
        steps = parse_steps('x' + '.' * 12)  # a 13-step cycle
        fired = play(steps, 16 * 4, 16, seed=1, lane='t')
        self.assertEqual([b * 16 + s for b, s, _ in fired], [0, 13, 26, 39, 52])


class GrooveTest(unittest.TestCase):
    def setUp(self):
        self.g = Grid(bars=1)

    def test_straight_is_zero(self):
        self.assertTrue(all(Groove(50).offset(s, self.g) == 0 for s in range(16)))

    def test_mpc_16th_swing(self):
        tri = Groove(200 / 3)  # 66.67%: triplet feel, the off 16th lands a third of a 16th late
        self.assertEqual([tri.offset(s, self.g) for s in range(4)], [0, 80, 0, 80])
        self.assertEqual(Groove(54).offset(1, self.g), round(480 * 0.54 - 240))
        self.assertEqual(Groove(75).offset(3, self.g), 120)

    def test_8th_swing_warps_16ths(self):
        g8 = Groove(75, swing_grid=8)  # the off 8th (step 2) moves a 16th later; step 1 moves half that
        self.assertEqual([g8.offset(s, self.g) for s in range(4)], [0, 120, 240, 120])

    def test_amount_and_limits(self):
        self.assertEqual(Groove(75).scaled(0.5).offset(1, self.g), 60)
        self.assertEqual(Groove(75).scaled(0).offset(1, self.g), 0)
        for bad in (dict(swing=49), dict(swing=76), dict(swing_grid=4), dict(amount=2)):
            with self.assertRaises(ValueError):
                Groove(**{'swing': 50, **bad})


class PitchTest(unittest.TestCase):
    def test_keys(self):
        am = parse_key('Am')
        self.assertEqual((am.root, am.mode), (9, 'minor'))
        self.assertEqual(parse_key('F#m').root, 6)
        self.assertEqual(parse_key('Bb').root, 10)
        self.assertEqual(parse_key('C major').mode, 'major')
        self.assertEqual(str(parse_key('a minor')), 'Am')
        with self.assertRaises(ValueError):
            parse_key('H')

    def test_degrees(self):
        am = parse_key('Am')
        root = am.root_at_or_above(31)
        self.assertEqual(root, 33)
        self.assertEqual([am.degree(d, root) for d in (1, 3, 5, 7, 8, 10)], [33, 36, 40, 43, 45, 48])
        self.assertEqual(name(60), 'C3')
        self.assertEqual(name(33), 'A0')
        with self.assertRaises(ValueError):
            am.degree(0, root)


if __name__ == '__main__':
    unittest.main()

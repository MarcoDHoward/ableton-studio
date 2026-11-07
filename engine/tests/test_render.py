"""The peak-time techno render: style rules, hard guarantees and determinism."""
import unittest

from music_engine import midi
from music_engine.parts import build_steps
from music_engine.pitch import parse_key
from music_engine.render import render


def at(song, part, lane=None):
    return [n for n in song.part_notes(part) if lane is None or n.lane == lane]


class TechnoPeakTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.songs = [render('techno_peak', bars=8, seed=s) for s in (1, 7, 42, 1234)]

    def test_hard_gates(self):
        for song in self.songs:
            g = song.grid
            self.assertEqual(g.bars, 8)
            self.assertEqual(g.length_beats, 32)
            for n in song.notes:
                self.assertGreaterEqual(n.start(g), 0)
                self.assertLessEqual(n.start(g) + n.duration, g.end_tick, 'no note past the clip end')
                self.assertTrue(1 <= n.velocity <= 127)
                self.assertTrue(0 <= n.step < 16 and 0 <= n.bar < 8)
            voices = {}
            for n in song.notes:
                voices.setdefault((n.part, n.pitch), []).append((n.start(g), n.start(g) + n.duration))
            for spans in voices.values():
                spans.sort()
                for (s1, e1), (s2, _) in zip(spans, spans[1:]):
                    self.assertLessEqual(e1, s2, 'no overlapping notes of the same pitch')

    def test_four_on_the_floor(self):
        for song in self.songs:
            loud = {(n.bar, n.step) for n in at(song, 'drums', 'kick') if n.velocity >= 100}
            self.assertEqual(loud, {(b, s) for b in range(8) for s in (0, 4, 8, 12)})
            for n in at(song, 'drums', 'kick'):
                self.assertEqual(n.offset, 0, 'kick stays on the grid')

    def test_ghost_kick_once_per_bar(self):
        for song in self.songs:
            ghosts = [(n.bar, n.step) for n in at(song, 'drums', 'kick') if n.velocity < 100]
            self.assertEqual(sorted(b for b, _ in ghosts), list(range(8)))
            self.assertTrue(all(s in (7, 15) for _, s in ghosts))

    def test_clap_on_two_and_four_with_fill(self):
        for song in self.songs:
            claps = {(n.bar, n.step) for n in at(song, 'drums', 'clap') if n.velocity >= 100}
            for b in range(8):
                self.assertIn((b, 4), claps)
                self.assertIn((b, 12), claps)
            self.assertEqual(song.fill_bars, [7])
            roll = {n.step for n in at(song, 'drums', 'clap') if n.bar == 7}
            self.assertTrue({13, 14, 15} <= roll)
            self.assertFalse(any(n.step in (13, 14, 15) for n in at(song, 'drums', 'clap') if n.bar < 7))

    def test_hats(self):
        for song in self.songs:
            opens = {(n.bar, n.step) for n in at(song, 'drums', 'hat_open')}
            for b in range(8):
                want = {2, 6, 10, 15} if b % 4 == 3 else {2, 6, 10, 14}
                self.assertEqual({s for bb, s in opens if bb == b}, want)
            closed = at(song, 'drums', 'hat_closed')
            self.assertTrue(all(n.step % 2 == 0 for n in closed if n.velocity > 90))
            self.assertGreater(len(closed), 100)
            swung = [n for n in closed if n.step % 2 == 1]
            self.assertTrue(swung and all(n.offset > 0 for n in swung), 'off 16ths are swung')

    def test_bass_rolls_in_key_around_the_kick(self):
        am = parse_key('Am')
        for song in self.songs:
            bass = at(song, 'bass')
            self.assertEqual(len(bass), 8 * 12)
            self.assertFalse(any(n.step % 4 == 0 for n in bass), 'bass leaves room for the kick')
            self.assertTrue(all(am.contains(n.pitch) for n in bass))
            self.assertTrue(all(31 <= n.pitch <= 31 + 24 for n in bass))
            self.assertTrue(all(n.pitch == 33 for n in bass if n.step % 4 == 1), 'root after every kick')
            self.assertGreater(sum(n.pitch == 33 for n in bass) / len(bass), 0.6)

    def test_stab_is_an_in_key_chord(self):
        am = parse_key('Am')
        for song in self.songs:
            stab = at(song, 'stab')
            self.assertTrue(all(am.contains(n.pitch) for n in stab))
            by_pos = {}
            for n in stab:
                by_pos.setdefault((n.bar, n.step), []).append(n.pitch)
            self.assertTrue(all(sorted(p) == [57, 60, 64, 67] for p in by_pos.values()))
            self.assertEqual({b for b, s in by_pos if s == 6}, set(range(8)))
            self.assertEqual({b for b, s in by_pos if s == 11}, {1, 3, 5, 7})

    def test_other_keys_stay_in_key(self):
        for key in ('F#m', 'C', 'Ebm', 'G minor'):
            song = render('techno_peak', bars=4, seed=5, key=key)
            k = parse_key(key)
            self.assertTrue(all(k.contains(n.pitch) for n in song.notes if n.part != 'drums'), key)

    def test_deterministic_and_seed_sensitive(self):
        a = midi.smf_bytes(midi.song_tracks(render('techno_peak', seed=7)), bpm=132, ppq=960)
        b = midi.smf_bytes(midi.song_tracks(render('techno_peak', seed=7)), bpm=132, ppq=960)
        c = midi.smf_bytes(midi.song_tracks(render('techno_peak', seed=8)), bpm=132, ppq=960)
        self.assertEqual(a, b)
        self.assertNotEqual(a, c)

    def test_shorter_render_is_a_prefix(self):
        long = render('techno_peak', bars=8, seed=11, fill_every=0)
        short = render('techno_peak', bars=4, seed=11, fill_every=0)
        key = lambda n: (n.part, n.lane, n.bar, n.step, n.pitch, n.velocity, n.offset)
        self.assertEqual([key(n) for n in short.notes], [key(n) for n in long.notes if n.bar < 4])

    def test_options(self):
        song = render('techno_peak', bars=16, seed=1, bpm=130, swing=50, parts=['drums'])
        self.assertEqual(song.fill_bars, [7, 15])
        self.assertEqual([p for p, _ in song.parts], ['drums'])
        self.assertTrue(all(n.offset == 0 for n in song.notes))
        with self.assertRaises(ValueError):
            render('techno_peak', parts=['lead'])
        with self.assertRaises(ValueError):
            render('no_such_style')


class LaneSpecTest(unittest.TestCase):
    def test_typos_are_caught(self):
        with self.assertRaises(ValueError):
            build_steps({'pattern': 'x...', 'steps': [{'step': 1, 'chanse': 0.5}]}, 'lane')
        with self.assertRaises(ValueError):
            build_steps({'pattern': 'x...', 'steps': [{'step': 9}]}, 'lane')
        with self.assertRaises(ValueError):
            build_steps({'pattern': 'x...', 'euclid': {'density': 1, 'steps': 4}}, 'lane')
        with self.assertRaises(ValueError):
            build_steps({'pattern': 'x...', 'condition': 'maybe'}, 'lane')

    def test_euclid_lane(self):
        steps = build_steps({'euclid': {'density': 3, 'steps': 8, 'rotation': 1}, 'char': 'o'}, 'lane')
        self.assertEqual(''.join(s.char if s else '.' for s in steps), '.o..o..o')


if __name__ == '__main__':
    unittest.main()

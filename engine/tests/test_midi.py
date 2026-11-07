"""The Standard MIDI File writer, checked byte by byte and by reading it back with mido (tests only)."""
import os
import tempfile
import unittest

from music_engine import midi
from music_engine.render import render

try:
    import mido
except ImportError:  # the engine never needs mido; only this answer-checker does
    mido = None


class VlqTest(unittest.TestCase):
    def test_spec_examples(self):
        # From the SMF spec's table of variable-length quantities.
        cases = {0: '00', 0x40: '40', 0x7F: '7F', 0x80: '8100', 0x2000: 'C000', 0x3FFF: 'FF7F',
                 0x4000: '818000', 0x100000: 'C08000', 0x1FFFFF: 'FFFF7F', 0x200000: '81808000',
                 0x0FFFFFFF: 'FFFFFF7F'}
        for n, hexed in cases.items():
            self.assertEqual(midi.vlq(n), bytes.fromhex(hexed), hex(n))
        with self.assertRaises(ValueError):
            midi.vlq(-1)


class BytesTest(unittest.TestCase):
    def test_header_and_tracks(self):
        data = midi.smf_bytes([('one', 1, [(0, 240, 60, 100)])], bpm=120, ppq=960, title='t', end_tick=3840)
        self.assertEqual(data[:14], b'MThd' + bytes.fromhex('00000006 0001 0002 03C0'))
        self.assertEqual(data.count(b'MTrk'), 2)
        self.assertIn(bytes.fromhex('FF5103 07A120'), data)  # 500000 us per quarter = 120 BPM
        self.assertIn(bytes.fromhex('FF5804 04021808'), data)  # 4/4
        self.assertTrue(data.endswith(b'\xff\x2f\x00'))

    def test_repeated_note_retriggers(self):
        # A note ending exactly where the next same-pitch note starts: off must come before on.
        data = midi.smf_bytes([('r', 1, [(0, 240, 60, 100), (240, 240, 60, 90)])], bpm=120, ppq=960)
        body = data[data.rindex(b'MTrk') + 8:]
        i_off = body.index(bytes([0x80, 60, 0]))
        i_on2 = body.index(bytes([0x90, 60, 90]))
        self.assertLess(i_off, i_on2)

    def test_rejects_bad_notes(self):
        for bad in [(0, 0, 60, 100), (0, 10, 128, 100), (0, 10, 60, 0), (-1, 10, 60, 100)]:
            with self.assertRaises(ValueError):
                midi.smf_bytes([('b', 1, [bad])], bpm=120, ppq=960)
        with self.assertRaises(ValueError):
            midi.smf_bytes([('b', 17, [])], bpm=120, ppq=960)


@unittest.skipIf(mido is None, 'mido not installed (pip install mido into .venv to run the round-trip)')
class MidoRoundTripTest(unittest.TestCase):
    def test_song_round_trip(self):
        song = render('techno_peak', bars=8, seed=7)
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, 'loop.mid')
            midi.write_song(path, song)
            mf = mido.MidiFile(path)

        self.assertEqual(mf.type, 1)
        self.assertEqual(mf.ticks_per_beat, song.grid.ppq)
        self.assertEqual(len(mf.tracks), 1 + len(song.parts))

        meta = {m.type: m for m in mf.tracks[0] if m.is_meta}
        self.assertAlmostEqual(mido.tempo2bpm(meta['set_tempo'].tempo), song.bpm, places=2)
        self.assertEqual((meta['time_signature'].numerator, meta['time_signature'].denominator), (4, 4))

        for track, (part, channel) in zip(mf.tracks[1:], song.parts):
            self.assertEqual(track.name, part)
            now, open_notes, got = 0, {}, []
            for m in track:
                now += m.time
                if m.type == 'note_on' and m.velocity > 0:
                    self.assertEqual(m.channel, channel - 1)
                    open_notes[m.note] = (now, m.velocity)
                elif m.type == 'note_off' or (m.type == 'note_on' and m.velocity == 0):
                    start, vel = open_notes.pop(m.note)
                    got.append((start, now - start, m.note, vel))
            self.assertEqual(open_notes, {}, 'every note is closed')
            self.assertEqual(now, song.grid.end_tick, 'track ends exactly at the clip end')
            want = [(n.start(song.grid), n.duration, n.pitch, n.velocity) for n in song.part_notes(part)]
            self.assertEqual(sorted(got), sorted(want))

    def test_single_part_file(self):
        song = render('techno_peak', bars=2, seed=3)
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, 'bass.mid')
            midi.write_song(path, song, parts=['bass'])
            mf = mido.MidiFile(path)
        self.assertEqual([t.name for t in mf.tracks[1:]], ['bass'])
        self.assertAlmostEqual(mf.length, 2 * 4 * 60 / song.bpm, places=3)


if __name__ == '__main__':
    unittest.main()

"""The Live bridge against a fake AbletonMCP server, and the CLI end to end."""
import contextlib
import io
import json
import os
import socket
import tempfile
import threading
import unittest

from music_engine import live
from music_engine.cli import main
from music_engine.render import render


class FakeLive:
    """Speaks the AbletonMCP Remote Script's protocol: one JSON command in, one JSON reply out."""

    def __init__(self, track_count=3):
        self.track_count = track_count
        self.received = []
        self.server = socket.socket()
        self.server.bind(('127.0.0.1', 0))
        self.server.listen(1)
        self.port = self.server.getsockname()[1]
        threading.Thread(target=self._serve, daemon=True).start()

    def _serve(self):
        conn, _ = self.server.accept()
        buf = b''
        with conn:
            while True:
                data = conn.recv(8192)
                if not data:
                    return
                buf += data
                try:
                    cmd = json.loads(buf)
                except ValueError:
                    continue
                buf = b''
                self.received.append(cmd)
                result = {}
                if cmd['type'] == 'get_session_info':
                    result = {'track_count': self.track_count, 'tempo': 120.0}
                elif cmd['type'] == 'create_midi_track':
                    self.track_count += 1
                    result = {'index': self.track_count - 1}
                conn.sendall(json.dumps({'status': 'success', 'result': result}).encode())

    def close(self):
        self.server.close()


def run_cli(*argv):
    out = io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(io.StringIO()):
        code = main(list(argv))
    return code, out.getvalue()


class LiveBridgeTest(unittest.TestCase):
    def test_writes_new_tracks_only(self):
        song = render('techno_peak', bars=2, seed=7)
        fake = FakeLive(track_count=3)
        try:
            written = live.write_song(song, port=fake.port, title_fn=lambda: 'SCRATCH Claude - Ableton Live 12 Suite')
        finally:
            fake.close()
        kinds = [c['type'] for c in fake.received]
        self.assertTrue(set(kinds) <= live.ALLOWED)
        self.assertNotIn('set_tempo', kinds)
        self.assertEqual(kinds.count('create_midi_track'), 3)
        self.assertTrue(all(c['params'].get('index') == -1 for c in fake.received if c['type'] == 'create_midi_track'))
        self.assertEqual([w['track_index'] for w in written], [3, 4, 5])
        clips = [c['params'] for c in fake.received if c['type'] == 'create_clip']
        self.assertEqual([c['length'] for c in clips], [8.0] * 3)
        adds = [c['params'] for c in fake.received if c['type'] == 'add_notes_to_clip']
        self.assertEqual([a['track_index'] for a in adds], [3, 4, 5])
        kick = [n for n in adds[0]['notes'] if n['pitch'] == 36 and n['velocity'] >= 100]
        self.assertEqual([n['start_time'] for n in kick], [0, 1, 2, 3, 4, 5, 6, 7])
        self.assertTrue(all(n['start_time'] + n['duration'] <= 8 for a in adds for n in a['notes']))

    def test_set_tempo_on_request(self):
        song = render('techno_peak', bars=1, seed=1, parts=['bass'])
        fake = FakeLive()
        try:
            live.write_song(song, port=fake.port, set_tempo=True, any_set=True)
        finally:
            fake.close()
        self.assertIn({'type': 'set_tempo', 'params': {'tempo': 132.0}}, fake.received)

    def test_scratch_guard(self):
        live.require_scratch(title_fn=lambda: 'SCRATCH Claude* - Ableton Live 12 Suite')
        live.require_scratch(any_set=True, title_fn=lambda: 'My Song - Ableton Live 12 Suite')
        for title in ('My Song - Ableton Live 12 Suite', 'Ableton Live 12 Suite', None):
            with self.assertRaises(live.LiveError):
                live.require_scratch(title_fn=lambda: title)
        self.assertEqual(live.set_name('SCRATCH Claude* - Ableton Live 12 Suite'), 'SCRATCH Claude')

    def test_refuses_other_commands_and_hosts(self):
        with self.assertRaises(live.LiveError):
            live.Client(host='192.168.1.5')
        c = live.Client()
        for forbidden in ('drain_passive_events', 'get_session_snapshot', 'delete_clip'):
            with self.assertRaises(live.LiveError):
                c.send(forbidden)

    def test_unreachable(self):
        s = socket.socket()
        s.bind(('127.0.0.1', 0))
        port = s.getsockname()[1]
        s.close()
        with self.assertRaises(live.LiveError):
            with live.Client(port=port, timeout=1):
                pass


class CliTest(unittest.TestCase):
    def test_render_writes_files(self):
        with tempfile.TemporaryDirectory() as d:
            code, out = run_cli('render', '--seed', '7', '--out', d, '--json')
            self.assertEqual(code, 0)
            r = json.loads(out)
            self.assertEqual(r['bars'], 8)
            self.assertEqual(set(r['parts']), {'drums', 'bass', 'stab'})
            for path in r['files'].values():
                self.assertTrue(os.path.isfile(path), path)
            with open(r['files']['notes'], encoding='utf-8') as fh:
                notes = json.load(fh)
            self.assertEqual(len(notes['notes']), sum(r['parts'].values()))
            self.assertIn('--seed 7', r['regenerate'])
            with open(r['files']['loop'], 'rb') as fh:
                first = fh.read()
            run_cli('render', '--seed', '7', '--out', d)
            with open(r['files']['loop'], 'rb') as fh:
                self.assertEqual(fh.read(), first, 'same seed, same bytes')

    def test_render_plan_file_and_dry_run(self):
        with tempfile.TemporaryDirectory() as d:
            plan = os.path.join(d, 'plan.json')
            with open(plan, 'w') as fh:
                json.dump({'key': 'F#m', 'bars': 4, 'seed': 3, 'bpm': 130}, fh)
            out_dir = os.path.join(d, 'o')
            code, out = run_cli('render', plan, '--seed', '9', '--out', out_dir, '--dry-run', '--live', '--json')
            self.assertEqual(code, 0)
            r = json.loads(out)
            self.assertEqual((r['key'], r['bars'], r['seed'], r['bpm']), ('F#m', 4, 9, 130.0))
            self.assertFalse(os.path.exists(out_dir))
            self.assertEqual(len(r['live_commands']), 15)

    def test_part_and_errors(self):
        code, out = run_cli('part', 'drums.kick', '--bars', '2')
        self.assertEqual(code, 0)
        self.assertIn('X... X...', out)
        code, out = run_cli('part', 'bass', '--json', '--bars', '1')
        self.assertEqual(json.loads(out)['parts'], {'bass': 12})
        code, out = run_cli('part', 'lead', '--json')
        self.assertEqual(code, 1)
        self.assertIn('error', json.loads(out))
        code, _ = run_cli('render', '--swing', '90', '--dry-run')
        self.assertEqual(code, 1)

    def test_euclid_and_styles(self):
        code, out = run_cli('euclid', '5', '8', '--json')
        self.assertEqual(json.loads(out)['pattern'], 'x.xx.xx.')
        code, out = run_cli('styles', '--json')
        self.assertIn('techno_peak', [s['name'] for s in json.loads(out)])


if __name__ == '__main__':
    unittest.main()

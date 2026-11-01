"""Tests for the scratch-set guard. Run: python -m unittest harness/test_liveguard.py"""
import io
import json
import os
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import liveguard as g

TOOLS = [  # a slice of the real tools-manifest.json shape
    {'name': 'tracks_list', 'method': 'GET'}, {'name': 'status_get', 'method': 'GET'},
    {'name': 'song_get', 'method': 'GET'}, {'name': 'track_get', 'method': 'GET'},
    {'name': 'notes_get', 'method': 'GET'}, {'name': 'scene_list', 'method': 'GET'},
    {'name': 'snapshot_get', 'method': 'GET'}, {'name': 'scene_fire', 'method': 'POST'},
    {'name': 'track_set', 'method': 'PUT'}, {'name': 'notes_add', 'method': 'POST'},
]


_MANIFEST = tempfile.NamedTemporaryFile('w', suffix='.json', delete=False, encoding='utf-8')
json.dump({'tools': TOOLS}, _MANIFEST)
_MANIFEST.close()
_REAL_READS = g.read_only_commands


def commands():
    with mock.patch.object(g, 'MANIFEST', _MANIFEST.name):
        return _REAL_READS()


class Classify(unittest.TestCase):
    def test_cases(self):
        R = commands()
        cases = [
            (['tracks'], 'read'), (['tracks', '--json'], 'read'), (['status'], 'read'), (['song', 'get'], 'read'),
            (['tempo'], 'read'), (['tempo', '122'], 'write'), (['track', 'get', 'Kick'], 'read'),
            (['track', 'set', 'Kick', 'mute', 'on'], 'write'), (['notes', 'replace', 'Bass', '1'], 'write'),
            (['notes', 'get', 'Bass', '1'], 'read'), (['scripts', 'install', '--yes'], 'ask'), (['snapshot'], 'read'),
            (['mix', 'recall', 'x'], 'write'), (['weird-new-cmd'], 'write'), (['scene', 'fire', '1'], 'write'),
            (['scene', 'list'], 'read'), (['device', 'add', 'Echo'], 'write'), (['notes', 'replace', 'B', '--dry-run'], 'read'),
        ]
        for args, want in cases:
            self.assertEqual(g.classify(args, R), want, args)

    def test_no_manifest_fails_closed(self):
        with mock.patch.object(g, 'MANIFEST', os.path.join(tempfile.gettempdir(), 'no-such-manifest.json')):
            R = _REAL_READS()
        self.assertEqual(g.classify(['tracks'], R), 'write')
        self.assertEqual(g.classify(['snapshot', 'save', 'x'], R), 'read')  # hand-written reads still known


class Parse(unittest.TestCase):
    def test_finds_every_invocation(self):
        with mock.patch.object(g, 'read_only_commands', commands):
            acts = list(g.command_actions('cd cli && ./ableton-pp-cli.exe tracks --json; ableton-pp-cli tempo 125'))
        self.assertEqual([k for k, _ in acts], ['read', 'write'])

    def test_loop_writes(self):
        with mock.patch.object(g, 'read_only_commands', commands):
            self.assertEqual([k for k, _ in g.command_actions('python harness/loop.py listen --bars 2')], ['write'])
            self.assertEqual(list(g.command_actions('python harness/loop.py score run1')), [])


class SetName(unittest.TestCase):
    def test_titles(self):
        self.assertEqual(g.set_name('ProgAcidAI* - Ableton Live 12 Suite'), 'ProgAcidAI')
        self.assertEqual(g.set_name('SCRATCH Claude - Ableton Live 12 Suite'), 'SCRATCH Claude')
        self.assertEqual(g.set_name('Ableton Live 12 Suite'), 'Untitled')
        self.assertTrue(g.is_scratch('SCRATCH Claude'))
        self.assertTrue(g.is_scratch('scratch acid'))
        self.assertFalse(g.is_scratch('ProgAcidAI'))
        self.assertFalse(g.is_scratch('Untitled'))


class Hook(unittest.TestCase):
    def run_hook(self, command, title):
        out = io.StringIO()
        with mock.patch.object(g, 'read_only_commands', commands), \
             mock.patch.object(g, 'live_window_title', return_value=title), \
             mock.patch('sys.stdin', io.StringIO(json.dumps({'tool_input': {'command': command}}))), \
             mock.patch('sys.stdout', out):
            g.hook()
        return out.getvalue()

    def test_write_in_real_set_asks(self):
        out = json.loads(self.run_hook('ableton-pp-cli tempo 122', 'ProgAcidAI* - Ableton Live 12 Suite'))
        self.assertEqual(out['hookSpecificOutput']['permissionDecision'], 'ask')
        self.assertIn('ProgAcidAI', out['hookSpecificOutput']['permissionDecisionReason'])

    def test_write_in_scratch_passes(self):
        self.assertEqual(self.run_hook('ableton-pp-cli tempo 122', 'SCRATCH Claude - Ableton Live 12 Suite'), '')

    def test_read_passes(self):
        self.assertEqual(self.run_hook('ableton-pp-cli tracks', 'ProgAcidAI* - Ableton Live 12 Suite'), '')

    def test_live_not_running_passes(self):
        self.assertEqual(self.run_hook('ableton-pp-cli tempo 122', None), '')

    def test_install_always_asks(self):
        out = json.loads(self.run_hook('ableton-pp-cli scripts install --yes', 'SCRATCH Claude - Ableton Live 12 Suite'))
        self.assertEqual(out['hookSpecificOutput']['permissionDecision'], 'ask')

    def test_unrelated_command_ignored(self):
        self.assertEqual(self.run_hook('git status', 'ProgAcidAI - Ableton Live 12 Suite'), '')


if __name__ == '__main__':
    unittest.main()

"""Write a rendered Song into Ableton Live through the AbletonMCP Remote Script (TCP 127.0.0.1:9877).

This is the v0.1 bridge. From v0.3 the engine writes through ableton-pp-cli instead,
so keep everything Live-specific in this file.

What it does, and only this: add one new MIDI track per part at the end of the set,
create an empty clip in its first slot, and put the notes in it. It never edits or
deletes existing tracks or clips, and changes the tempo only when asked.

Safety:
  * loopback only; nothing leaves the machine;
  * never sends drain_passive_events or get_session_snapshot (they read what you do in Live);
  * writes only when the open set's name starts with SCRATCH, unless any_set=True.
Live's per-note Chance is not passed through by this bridge; the engine has already
rolled every chance, so the clip holds the concrete notes.
"""
import json
import os
import re
import socket

HOST = '127.0.0.1'
PORT = 9877
ALLOWED = {'get_session_info', 'create_midi_track', 'set_track_name', 'create_clip',
           'add_notes_to_clip', 'set_clip_name', 'set_tempo'}


class LiveError(Exception):
    pass


class Client:
    """One JSON request, one JSON reply, over a single TCP connection."""

    def __init__(self, host=HOST, port=PORT, timeout=15.0):
        if host not in ('127.0.0.1', 'localhost', '::1'):
            raise LiveError('the Live bridge is loopback only')
        self.host, self.port, self.timeout = host, port, timeout
        self.sock = None

    def __enter__(self):
        try:
            self.sock = socket.create_connection((self.host, self.port), timeout=self.timeout)
        except OSError as e:
            raise LiveError(f'cannot reach the AbletonMCP Remote Script on {self.host}:{self.port} '
                            f'(is Live open with it selected as a Control Surface?): {e}') from None
        return self

    def __exit__(self, *exc):
        if self.sock:
            self.sock.close()

    def send(self, kind, **params):
        if kind not in ALLOWED:
            raise LiveError(f'{kind} is not a command this engine sends')
        self.sock.sendall(json.dumps({'type': kind, 'params': params}).encode('utf-8'))
        buf = b''
        while True:
            chunk = self.sock.recv(65536)
            if not chunk:
                raise LiveError(f'{kind}: Live closed the connection')
            buf += chunk
            try:
                reply = json.loads(buf.decode('utf-8'))
                break
            except (ValueError, UnicodeDecodeError):
                continue  # not all here yet
        if reply.get('status') != 'success':
            raise LiveError(f'{kind}: {reply.get("message", reply)}')
        return reply.get('result', {})


def live_window_title():
    """Title of Live's main window, e.g. 'SCRATCH Claude* - Ableton Live 12 Suite'; None if not found or not Windows."""
    if os.name != 'nt':
        return None
    import ctypes
    from ctypes import wintypes
    user32 = ctypes.windll.user32
    found = []

    @ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)
    def visit(hwnd, _):
        if user32.IsWindowVisible(hwnd):
            n = user32.GetWindowTextLengthW(hwnd)
            if n:
                buf = ctypes.create_unicode_buffer(n + 1)
                user32.GetWindowTextW(hwnd, buf, n + 1)
                if re.search(r' - Ableton Live \d', buf.value) or buf.value.startswith('Ableton Live '):
                    found.append(buf.value)
                    return False
        return True

    user32.EnumWindows(visit, 0)
    return found[0] if found else None


def set_name(title):
    """'SCRATCH Claude* - Ableton Live 12 Suite' -> 'SCRATCH Claude'; a bare 'Ableton Live 12 Suite' -> 'Untitled'."""
    if title is None:
        return None
    m = re.match(r'^(.*?)\*?\s+-\s+Ableton Live', title)
    return m.group(1).strip() if m else 'Untitled'


def require_scratch(any_set=False, title_fn=live_window_title):
    """Raise unless the open set is a SCRATCH set (or the caller passed any_set)."""
    if any_set:
        return
    name = set_name(title_fn())
    if name is None:
        raise LiveError('cannot find Live\'s window; is Live running?')
    if not name.upper().startswith('SCRATCH'):
        raise LiveError(f'the open set is {name!r}. The engine writes only into sets whose name starts with '
                        f'SCRATCH; pass --any-set to add tracks to this set anyway')


def plan_commands(song, label):
    """The writes for a song, in order, with track indexes relative to the first new track (0, 1, ...)."""
    g = song.grid
    cmds = []
    for i, (part, _) in enumerate(song.parts):
        notes = [{'pitch': n.pitch, 'start_time': round(n.start(g) / g.ppq, 6),
                  'duration': round(n.duration / g.ppq, 6), 'velocity': n.velocity, 'mute': False}
                 for n in song.part_notes(part)]
        cmds.append((i, 'create_midi_track', {}))
        cmds.append((i, 'set_track_name', {'name': f'{part} {label}'}))
        cmds.append((i, 'create_clip', {'clip_index': 0, 'length': float(g.length_beats)}))
        cmds.append((i, 'add_notes_to_clip', {'clip_index': 0, 'notes': notes}))
        cmds.append((i, 'set_clip_name', {'clip_index': 0, 'name': f'{part} {label}'}))
    return cmds


def write_song(song, *, set_tempo=False, any_set=False, port=PORT, title_fn=live_window_title):
    """Add the song to Live as new tracks. Returns [{'part', 'track_index', 'notes'}]."""
    require_scratch(any_set, title_fn)
    label = f'{song.style} {song.key} s{song.seed}'
    written = []
    with Client(port=port) as live:
        info = live.send('get_session_info')
        first = int(info['track_count'])
        if set_tempo:
            live.send('set_tempo', tempo=song.bpm)
        for i, kind, params in plan_commands(song, label):
            if kind == 'create_midi_track':
                live.send(kind, index=-1)
                continue
            live.send(kind, track_index=first + i, **params)
            if kind == 'add_notes_to_clip':
                written.append({'part': song.parts[i][0], 'track_index': first + i, 'notes': len(params['notes'])})
    return written

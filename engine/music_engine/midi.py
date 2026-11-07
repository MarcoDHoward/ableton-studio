"""A Standard MIDI File writer (format 1), standard library only.

Layout: track 0 holds the tempo map (name, time signature, tempo); each part gets
its own track after it. Live turns each track of a dropped multi-track file into
its own MIDI track.

Spec: the MIDI 1.0 "Standard MIDI Files" document (MThd/MTrk chunks, variable-length delta times).
"""
import struct

NOTE_ON = 0x90
NOTE_OFF = 0x80


def vlq(n):
    """Variable-length quantity: 7 bits per byte, high bit set on every byte but the last."""
    if n < 0:
        raise ValueError('delta times cannot be negative')
    out = [n & 0x7F]
    n >>= 7
    while n:
        out.append(0x80 | (n & 0x7F))
        n >>= 7
    return bytes(reversed(out))


def _meta(kind, data):
    return bytes([0xFF, kind]) + vlq(len(data)) + data


def _chunk(kind, body):
    return kind + struct.pack('>I', len(body)) + body


def _track(events, end_tick):
    """events: [(tick, order, bytes)]. order breaks ties: lower first. Ends with End of Track at end_tick or later."""
    events = sorted(events, key=lambda e: (e[0], e[1]))
    body = bytearray()
    now = 0
    for tick, _, data in events:
        body += vlq(tick - now) + data
        now = tick
    body += vlq(max(0, end_tick - now)) + _meta(0x2F, b'')
    return _chunk(b'MTrk', bytes(body))


def tempo_track(name, bpm, numerator, denominator, end_tick):
    mpqn = round(60_000_000 / bpm)  # microseconds per quarter note
    events = [
        (0, 0, _meta(0x03, name.encode('utf-8'))),
        (0, 1, _meta(0x58, bytes([numerator, denominator.bit_length() - 1, 24, 8]))),
        (0, 2, _meta(0x51, mpqn.to_bytes(3, 'big'))),
    ]
    return _track(events, end_tick)


def note_track(name, channel, notes, end_tick):
    """notes: [(start_tick, duration_ticks, pitch, velocity)]. channel is 1-16."""
    if not 1 <= channel <= 16:
        raise ValueError('channel must be 1-16')
    ch = channel - 1
    events = [(0, 0, _meta(0x03, name.encode('utf-8')))]
    for start, duration, pitch, velocity in notes:
        if not (0 <= pitch <= 127 and 1 <= velocity <= 127 and duration > 0 and start >= 0):
            raise ValueError(f'bad note {(start, duration, pitch, velocity)}')
        # At the same tick, note-offs (order 1) go before note-ons (order 2) so repeated notes retrigger.
        events.append((start, 2, bytes([NOTE_ON | ch, pitch, velocity])))
        events.append((start + duration, 1, bytes([NOTE_OFF | ch, pitch, 0])))
    return _track(events, end_tick)


def smf_bytes(tracks, *, bpm, ppq, title='', numerator=4, denominator=4, end_tick=0):
    """tracks: [(name, channel, notes)] as for note_track. Returns the file's bytes."""
    if denominator & (denominator - 1):
        raise ValueError('time signature denominator must be a power of two')
    chunks = [tempo_track(title, bpm, numerator, denominator, end_tick)]
    chunks += [note_track(name, channel, notes, end_tick) for name, channel, notes in tracks]
    header = _chunk(b'MThd', struct.pack('>HHH', 1, len(chunks), ppq))
    return header + b''.join(chunks)


def song_tracks(song, parts=None):
    """A Song's parts as tracks for smf_bytes."""
    g = song.grid
    out = []
    for name, channel in song.parts:
        if parts and name not in parts:
            continue
        notes = [(n.start(g), n.duration, n.pitch, n.velocity) for n in song.part_notes(name)]
        out.append((name, channel, notes))
    return out


def write_song(path, song, parts=None):
    data = smf_bytes(song_tracks(song, parts), bpm=song.bpm, ppq=song.grid.ppq,
                     title=f'{song.style} {song.key} seed {song.seed}',
                     numerator=song.grid.beats_per_bar, denominator=4, end_tick=song.grid.end_tick)
    with open(path, 'wb') as fh:
        fh.write(data)
    return len(data)

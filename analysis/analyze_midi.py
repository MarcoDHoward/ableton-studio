"""Musical analysis of set.json: per-track MIDI + automation features and a song timeline.

usage: python analyze_midi.py set.json midi_analysis.json
"""
import json, math, sys
from collections import Counter, defaultdict
import numpy as np

NAMES = ['C', 'C#', 'D', 'D#', 'E', 'F', 'F#', 'G', 'G#', 'A', 'A#', 'B']
def nn(p): return '%s%d' % (NAMES[p % 12], p // 12 - 2)  # Ableton: C3 = 60

# Krumhansl-Kessler key profiles
MAJ = np.array([6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88])
MIN = np.array([6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17])
def key_estimate(pc):
    pc = np.asarray(pc, float)
    if pc.sum() == 0: return None
    best = []
    for r in range(12):
        for prof, mode in ((MAJ, 'major'), (MIN, 'minor')):
            best.append((np.corrcoef(pc, np.roll(prof, r))[0, 1], '%s %s' % (NAMES[r], mode)))
    best.sort(reverse=True)
    return {'key': best[0][1], 'confidence': round(best[0][0], 3), 'runner_up': best[1][1]}

CHORDS = {(0, 4, 7): '', (0, 3, 7): 'm', (0, 3, 6): 'dim', (0, 4, 8): 'aug', (0, 2, 7): 'sus2',
          (0, 5, 7): 'sus4', (0, 4, 7, 10): '7', (0, 4, 7, 11): 'maj7', (0, 3, 7, 10): 'm7',
          (0, 7): '5', (0, 3, 7, 10, 14): 'm9', (0, 4, 7, 11, 14): 'maj9'}
def chord_name(pitches):
    pcs = sorted({p % 12 for p in pitches})
    if len(pcs) < 2: return None
    for root in pcs:
        shape = tuple(sorted((p - root) % 12 for p in pcs))
        if shape in CHORDS: return NAMES[root] + CHORDS[shape]
    return '{' + ' '.join(NAMES[p] for p in pcs) + '}'

def arrangement_notes(clips):
    """Place clip contents on the song timeline (beats), honouring loops."""
    out = []
    for c in clips:
        if c['kind'] != 'midi' or c.get('disabled'): continue
        t0, t1 = c['start'], c['end']
        lp = c['loop']; ls, le = lp['start'] or 0.0, lp['end'] or 0.0
        notes = c['notes']
        if lp['on'] and le > ls:
            L = le - ls; k = 0
            while t0 + k * L < t1:
                for p, t, d, v in notes:
                    if ls <= t < le:
                        at = t0 + k * L + (t - ls)
                        if t0 <= at < t1: out.append((p, at, min(d, t1 - at), v))
                k += 1
        else:
            for p, t, d, v in notes:
                at = t0 + (t - ls)
                if t0 <= at < t1: out.append((p, at, d, v))
    out.sort(key=lambda n: (n[1], n[0]))
    return out

def midi_features(notes, bars, pad_names=None):
    if not notes: return None
    P = np.array([n[0] for n in notes]); T = np.array([n[1] for n in notes])
    D = np.array([n[2] for n in notes]); V = np.array([n[3] for n in notes])
    f = {'notes': len(notes)}
    f['pitch'] = {'min': nn(P.min()), 'max': nn(P.max()), 'median': nn(int(np.median(P))),
                  'range_semitones': int(P.max() - P.min())}
    used = Counter(P.tolist())
    f['pitches_used'] = {(pad_names.get(p, nn(p)) if pad_names else nn(p)): c for p, c in used.most_common(16)}
    pc = np.zeros(12)
    for p, d in zip(P, D): pc[p % 12] += d
    f['pitch_class_weight'] = {NAMES[i]: round(float(pc[i] / pc.sum()), 3) for i in range(12) if pc[i]}
    f['key_estimate'] = None if pad_names else key_estimate(pc)
    # rhythm on a 16-step bar grid
    step = np.round((T % 4) * 4).astype(int) % 16
    grid = np.bincount(step, minlength=16)
    f['step_histogram_16'] = grid.tolist()
    off = (T * 4) - np.round(T * 4)
    f['grid_deviation_16th'] = round(float(np.mean(np.abs(off))), 3)  # 0 = fully quantised
    odd = np.round(T * 4).astype(int) % 2 == 1
    f['swing_ms_hint'] = round(float(np.mean(off[odd])) if odd.any() else 0.0, 3)
    f['offbeat_ratio'] = round(float(np.mean(step % 4 != 0)), 3)
    onsets = np.unique(np.round(T, 3))
    active_bars = len(set((onsets // 4).astype(int).tolist()))
    f['active_bars'] = active_bars
    f['notes_per_active_bar'] = round(len(notes) / max(active_bars, 1), 2)
    f['duration_beats'] = {'median': round(float(np.median(D)), 3), 'mean': round(float(D.mean()), 3)}
    ioi = np.diff(onsets)
    f['ioi_beats_median'] = round(float(np.median(ioi)), 3) if len(ioi) else None
    f['velocity'] = {'mean': round(float(V.mean()), 1), 'std': round(float(V.std()), 1),
                     'min': int(V.min()), 'max': int(V.max())}
    # polyphony + chords
    groups = defaultdict(list)
    for p, t in zip(P, T): groups[round(t, 2)].append(int(p))
    sizes = np.array([len(set(g)) for g in groups.values()])
    f['polyphony'] = {'max': int(sizes.max()), 'mean': round(float(sizes.mean()), 2)}
    chords = Counter(c for c in (chord_name(g) for g in groups.values() if len(set(g)) > 1) if c)
    if chords: f['chords'] = dict(chords.most_common(8))
    if sizes.mean() < 1.3 and not pad_names:
        mel = [max(groups[k]) for k in sorted(groups)]
        iv = np.diff(mel)
        f['melodic_intervals'] = dict(Counter(int(x) for x in iv).most_common(10))
        f['contour'] = {'up': int((iv > 0).sum()), 'down': int((iv < 0).sum()), 'repeat': int((iv == 0).sum())}
    # repetition: how similar is each active bar's onset pattern to the most common one
    bar_pat = defaultdict(set)
    for t, p in zip(T, P): bar_pat[int(t // 4)].add((round((t % 4) * 4) % 16, int(p)))
    pats = Counter(frozenset(v) for v in bar_pat.values())
    f['distinct_bar_patterns'] = len(pats)
    f['top_pattern_share'] = round(pats.most_common(1)[0][1] / len(bar_pat), 3)
    return f

def automation_features(env, tempo):
    ev = [e for e in env['events'] if e[0] > -1e6]  # first event is the pre-song default
    vals = [e[1] for e in ev] or [e[1] for e in env['events']]
    times = [e[0] for e in ev]
    shape = 'static'
    if len(ev) >= 2:
        if set(vals) <= {0.0, 1.0}: shape = 'switch'
        elif len(ev) > 80: shape = 'drawn/dense'
        elif vals[-1] > vals[0] * 1.05 + 1e-9: shape = 'rise'
        elif vals[-1] < vals[0] * 0.95 - 1e-9: shape = 'fall'
        else: shape = 'move-and-return'
    return {'target': env['owner'], 'param': env['param'], 'points': len(env['events']),
            'span_bars': [round(min(times) / 4, 1), round(max(times) / 4, 1)] if times else None,
            'min': round(min(vals), 4) if vals else None, 'max': round(max(vals), 4) if vals else None,
            'shape': shape}

def main(src, dst):
    o = json.load(open(src, encoding='utf-8'))
    tempo = o['tempo']
    song_end = max([c['end'] for t in o['tracks'] for c in t['arrangement_clips']] + [0])
    bars = int(math.ceil(song_end / 4))
    out = {'tempo': tempo, 'bars': bars, 'minutes': round(song_end / tempo, 2),
           'locators': sorted(o['locators'], key=lambda l: l['time']), 'tracks': {}}
    all_pc = np.zeros(12)
    timeline = {}
    for t in o['tracks']:
        rec = {'type': t['type'], 'group': t['group']}
        pads = {}
        for d in t['devices']:
            for ch in d.get('chains', []):
                if ch.get('note') is not None: pads[ch['note']] = ch['name']
        notes = arrangement_notes(t['arrangement_clips'])
        rec['arrangement_notes'] = len(notes)
        if notes:
            rec['midi'] = midi_features(notes, bars, pads or None)
            if not pads:
                for p, _, d, _ in notes: all_pc[p % 12] += d
        # activity per bar (MIDI notes or audio clips)
        act = np.zeros(bars, int)
        for p, at, d, v in notes: act[min(int(at // 4), bars - 1):min(int(math.ceil((at + d) / 4)), bars)] = 1
        for c in t['arrangement_clips']:
            if c['kind'] == 'audio' and not c.get('disabled'):
                act[int(c['start'] // 4):int(math.ceil(c['end'] / 4))] = 1
        if act.any(): timeline[t['name']] = act.tolist()
        # loops used in session view
        sess = [c for c in t['session_clips'] if c['kind'] == 'midi']
        if sess:
            rec['session_clip_notes'] = {c['name'] or 'slot%d' % c['slot']: len(c['notes']) for c in sess}
        rec['automation'] = [automation_features(e, tempo) for e in t['automation'] if e['param'] != '?']
        clip_env = [automation_features(e, tempo) for c in t['arrangement_clips'] + t['session_clips']
                    for e in c.get('envelopes', []) if e.get('param', '?') != '?']
        if clip_env: rec['clip_envelopes'] = clip_env
        out['tracks'][t['name']] = rec
    out['song_key_estimate'] = key_estimate(all_pc)
    out['song_pitch_class_weight'] = {NAMES[i]: round(float(all_pc[i] / all_pc.sum()), 3) for i in range(12) if all_pc[i]}
    out['timeline_active_bars'] = timeline
    # section boundaries: bars where the set of active tracks changes a lot
    names = list(timeline)
    M = np.array([timeline[n] for n in names])
    changes = []
    for b in range(1, bars):
        diff = int(np.sum(M[:, b] != M[:, b - 1]))
        if diff >= 2: changes.append({'bar': b + 1, 'tracks_changed': diff,
                                      'in': [names[i] for i in range(len(names)) if M[i, b] and not M[i, b - 1]],
                                      'out': [names[i] for i in range(len(names)) if M[i, b - 1] and not M[i, b]]})
    out['section_changes'] = changes
    blocks = []
    for b0 in range(0, bars, 4):
        on = tuple(names[i] for i in range(len(names)) if M[i, b0:b0 + 4].any())
        if blocks and blocks[-1]['tracks'] == list(on): blocks[-1]['bars'][1] = b0 + 4
        else: blocks.append({'bars': [b0 + 1, b0 + 4], 'tracks': list(on)})
    out['sections_4bar'] = blocks
    out['density_per_bar'] = M.sum(axis=0).tolist()
    json.dump(out, open(dst, 'w', encoding='utf-8'), indent=1)
    return out

if __name__ == '__main__':
    o = main(sys.argv[1], sys.argv[2])
    print('song key', o['song_key_estimate'], o['bars'], 'bars', o['minutes'], 'min')
    for n, r in o['tracks'].items():
        m = r.get('midi')
        if m:
            print('%-15s n=%-4d %s..%s key=%-10s poly=%-4s vel=%-5s step16=%s chords=%s' % (
                n[:15], m['notes'], m['pitch']['min'], m['pitch']['max'],
                (m['key_estimate'] or {}).get('key'), m['polyphony']['mean'], m['velocity']['mean'],
                ''.join('x' if v > max(m['step_histogram_16']) * 0.25 else '.' for v in m['step_histogram_16']),
                list(m.get('chords', {}).items())[:4]))
    print('pitch classes', o['song_pitch_class_weight'])
    print('sections (4-bar blocks):')
    for b in o['sections_4bar']:
        print('  bars %3d-%-3d %2d tracks  %s' % (b['bars'][0], b['bars'][1], len(b['tracks']), ', '.join(x[:10] for x in b['tracks'])))

"""Merge set.json + midi_analysis.json + audio_analysis.json into sound thumbprints.

Each thumbprint = identity (instrument, preset, device chain), musical role (MIDI),
movement (automation), measured sound (STFT features), plain-language descriptors,
sound-design starting points, and a numeric vector for similarity search.

usage: python build_thumbprints.py set.json midi_analysis.json audio_analysis.json thumbprints.json
"""
import json, math, re, sys
import numpy as np

AUTOFILTER_TYPES = ['lowpass', 'highpass', 'bandpass', 'notch', 'morph', 'dj']  # Live 12 Auto Filter order

def chain_summary(devs, depth=0):
    out = []
    for d in devs:
        item = {'device': d['name'], 'type': d['type'], 'on': d.get('on', True)}
        if d['type'] == 'AutoFilter2':
            p = d.get('params', {}); ft = p.get('Filter_Type')
            item['filter'] = {'type': AUTOFILTER_TYPES[int(ft)] if ft is not None and int(ft) < 6 else 'type %s' % ft,
                              'cutoff_hz': round(p.get('Filter_Frequency') or 0), 'resonance': round(p.get('Filter_Resonance') or 0, 3)}
        st = d.get('state')
        if st:
            item.update({'preset': st.get('preset'), 'pack': st.get('pack'), 'author': st.get('author'),
                         'category': '/'.join(x for x in (st['meta'].get('Type'), st['meta'].get('Subtype')) if x),
                         'tags': st.get('tags'), 'description': st.get('description'),
                         'macros': {k: round(v, 3) for k, v in d.get('params', {}).items()
                                    if v is not None and not k.startswith('param')} or None})
        if d.get('samples'): item['samples'] = [s.replace('\\', '/').split('/')[-1] for s in d['samples']][:6]
        if d.get('chains') and depth < 2:
            item['chains'] = [{'name': c['name'], 'note': c.get('note'),
                               'devices': [x['name'] for x in c['devices']]} for c in d['chains']][:24]
        out.append(item)
    return out

def descriptors(a):
    if not a or 'spectral' not in a: return []
    d = []
    c = a['spectral']['centroid_hz']['median']
    d.append('sub-heavy' if c < 200 else 'dark/warm' if c < 700 else 'mid-focused' if c < 1800
             else 'bright' if c < 4000 else 'airy/sizzly')
    fl = a['spectral']['flatness']
    d.append('noisy' if fl > 0.15 else 'textured' if fl > 0.02 else 'tonal' if fl > 0.002 else 'very pure/tonal')
    atk = a['envelope']['attack_ms_median']; dec = a['envelope']['decay20db_ms_median']
    if atk is not None:
        d.append('transient/percussive attack' if atk < 12 else 'snappy attack' if atk < 40
                 else 'soft attack' if atk < 150 else 'slow swell')
    if dec is not None:
        d.append('very short' if dec < 120 else 'short' if dec < 300 else 'medium tail' if dec < 800 else 'long/sustained')
    w = a['stereo']['side_to_mid_db']
    d.append('wide stereo' if w > -6 else 'moderate width' if w > -15 else 'near-mono')
    m = a['spectral'].get('centroid_motion_ratio') or 1
    if m > 4: d.append('heavy filter/brightness movement')
    elif m > 2: d.append('noticeable brightness movement')
    if a.get('rhythmic_pulse') and not a['rhythmic_pulse'].startswith('free'):
        d.append('pulses on ' + a['rhythmic_pulse'])
    return d

def design_hints(a, midi):
    if not a or 'spectral' not in a: return {}
    s = a['spectral']; be = a['band_energy_pct']
    h = {
        'filter_cutoff_range_hz': [s['centroid_hz']['p10'], s['centroid_hz']['p90']],
        'spectral_tilt': 'low-heavy' if be['sub'] + be['bass'] > 60 else 'balanced' if be['mid'] > 25 else 'top-heavy',
        'amp_envelope_ms': {'attack': a['envelope']['attack_ms_median'], 'decay_to_-20dB': a['envelope']['decay20db_ms_median']},
        'noise_component': 'add noise/texture layer' if s['flatness'] > 0.02 else 'keep oscillators clean',
        'stereo': 'mono below ~150 Hz, widen top' if a['stereo']['side_to_mid_db'] > -10 else 'keep mostly mono',
    }
    if a.get('chroma_top'): h['strongest_pitch_classes'] = a['chroma_top']
    if a.get('dominant_peak_note'): h['dominant_low_peak'] = '%s (%.0f Hz)' % (a['dominant_peak_note'], a['dominant_peak_hz'])
    if midi:
        h['register'] = '%s..%s' % (midi['pitch']['min'], midi['pitch']['max'])
        h['velocity_feel'] = 'flat/programmed' if midi['velocity']['std'] < 4 else 'dynamic'
    return h

def vector(a):
    if not a or 'spectral' not in a: return None
    third = np.array(list(a['third_octave_db'].values())) / 80.0           # 31 dims, ~-1..0
    mf = np.array(a['mfcc_mean'][1:]) / 100.0                               # 12 dims
    s = a['spectral']
    atk = a['envelope']['attack_ms_median'] or 50; dec = a['envelope']['decay20db_ms_median'] or 300
    scal = np.array([math.log10(max(s['centroid_hz']['median'], 20)) / 4.3, s['flatness'],
                     math.log10(max(atk, 1)) / 3, math.log10(max(dec, 1)) / 3.5,
                     (a['stereo']['side_to_mid_db'] + 40) / 40])
    return np.concatenate([third, mf, scal]).round(4).tolist()

def match_stem(track, stems):
    norm = lambda s: re.sub(r'[^a-z0-9]', '', s.lower())
    cands = [k for k in stems if norm(k).endswith(norm(track))]
    return min(cands, key=len) if cands else None

def main(set_p, midi_p, audio_p, dst):
    o = json.load(open(set_p, encoding='utf-8'))
    mi = json.load(open(midi_p, encoding='utf-8'))
    au = json.load(open(audio_p, encoding='utf-8')) if audio_p != '-' else {}
    group_names = {t['id']: t['name'] for t in o['tracks'] if t['type'] == 'GroupTrack'}
    prints = {}
    for t in o['tracks']:
        if t['type'] == 'GroupTrack': continue
        m = mi['tracks'].get(t['name'], {})
        stem = match_stem(t['name'], au)
        a = au.get(stem) if stem else None
        if a and a.get('silent'): a = None; stem = stem + ' (silent: no clips in arrangement)'
        midi = m.get('midi')
        role = ('drums/percussion' if any(d['type'] == 'DrumGroupDevice' for d in t['devices'])
                else 'audio loop/fx' if t['type'] == 'AudioTrack'
                else 'return fx' if t['type'] == 'ReturnTrack' else 'instrument')
        tp = {
            'track': t['name'], 'kind': t['type'], 'role': role, 'group': group_names.get(t['group']),
            'stem_file': stem,
            'instrument_chain': chain_summary(t['devices']),
            'audio_sources': sorted({(c.get('file') or '').replace('\\', '/').split('/')[-1]
                                     for c in t['arrangement_clips'] + t['session_clips'] if c.get('file')}) or None,
            'mixer': t['mixer'],
            'music': midi and {k: midi[k] for k in ('notes', 'pitch', 'pitches_used', 'key_estimate', 'step_histogram_16',
                                                    'offbeat_ratio', 'notes_per_active_bar', 'duration_beats', 'velocity',
                                                    'polyphony', 'chords', 'melodic_intervals', 'distinct_bar_patterns',
                                                    'top_pattern_share') if k in midi},
            'movement': m.get('automation', []) + m.get('clip_envelopes', []),
            'active_bars': sum(mi['timeline_active_bars'].get(t['name'], [])),
            'sound': a and {k: a[k] for k in a if k not in ('mfcc_mean', 'mfcc_std')},
            'descriptors': descriptors(a),
            'design_hints': design_hints(a, midi),
            'vector': vector(a),
        }
        if a and 'spectral' in a:
            tp['sound']['spectral'] = {k: v for k, v in a['spectral'].items() if k != 'centroid_per_bar_hz'}
            tp['brightness_per_bar_hz'] = a['spectral']['centroid_per_bar_hz']
        prints[t['name']] = tp
    # similarity between every pair of thumbprints (cosine on the vector)
    names = [n for n in prints if prints[n]['vector']]
    V = np.array([prints[n]['vector'] for n in names]) if names else np.zeros((0, 1))
    if len(names):
        Vc = V - V.mean(0)
        Vn = Vc / (np.linalg.norm(Vc, axis=1, keepdims=True) + 1e-9)
        S = Vn @ Vn.T
        for i, n in enumerate(names):
            order = [j for j in np.argsort(-S[i]) if j != i][:3]
            prints[n]['most_similar'] = [{'track': names[j], 'similarity': round(float(S[i, j]), 3)} for j in order]
    out = {
        'schema': 'sound-thumbprint/1',
        'source_set': o['source'], 'tempo': o['tempo'], 'bars': mi['bars'],
        'song': {'key_from_midi': mi['song_key_estimate'], 'pitch_class_weight': mi['song_pitch_class_weight'],
                 'sections_4bar': mi['sections_4bar'], 'locators': mi['locators']},
        'vector_layout': '31 x third-octave dB/80 (20 Hz..20 kHz) | 12 x MFCC1..12/100 | log centroid, flatness, log attack, log decay, width',
        'thumbprints': prints,
    }
    json.dump(out, open(dst, 'w', encoding='utf-8'), indent=1, default=float)
    return out

if __name__ == '__main__':
    r = main(*sys.argv[1:5])
    for n, tp in r['thumbprints'].items():
        print('%-26s %-16s stem=%-5s %s' % (n[:26], tp['role'], bool(tp['stem_file']), ', '.join(tp['descriptors'])))

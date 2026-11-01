"""Compact data for the findings page: per-stem log spectrograms (uint8, base64) plus
summaries from thumbprints.json, the arrangement timeline and the acid automation.

usage: python build_report_data.py <stems_dir> report_data.json
"""
import base64, json, os, sys
import numpy as np
from scipy.signal import stft
from analyze_audio import load

COLS, BANDS, FMIN, FMAX = 360, 72, 30.0, 16000.0

def spectrogram(path):
    sr, x = load(path)
    mid = x.mean(1)
    f, t, Z = stft(mid, sr, window='hann', nperseg=4096, noverlap=2048, boundary=None)
    P = np.abs(Z) ** 2
    edges = np.geomspace(FMIN, FMAX, BANDS + 1)
    centres = np.sqrt(edges[:-1] * edges[1:])
    rows = np.zeros((BANDS, P.shape[1]))
    idx = np.digitize(f, edges) - 1
    for b in range(BANDS):
        sel = idx == b
        rows[b] = P[sel].mean(0) if sel.any() else P[int(np.argmin(np.abs(f - centres[b])))]
    cols = np.array_split(np.arange(P.shape[1]), COLS)
    S = np.stack([rows[:, c].mean(1) for c in cols], 1)
    D = 10 * np.log10(np.maximum(S, 1e-14))
    D -= D.max()
    q = np.clip((D + 72) / 72 * 255, 0, 255).astype(np.uint8)   # 72 dB range
    return base64.b64encode(q.tobytes()).decode(), round(len(mid) / sr, 2)

def main(stems, dst):
    tp = json.load(open('thumbprints.json', encoding='utf-8'))
    mi = json.load(open('midi_analysis.json', encoding='utf-8'))
    au = json.load(open('audio_analysis.json', encoding='utf-8'))
    st = json.load(open('set.json', encoding='utf-8'))
    groups = {t['id']: t['name'] for t in st['tracks'] if t['type'] == 'GroupTrack'}
    tracks = []
    for t in st['tracks']:
        if t['type'] == 'GroupTrack': continue
        p = tp['thumbprints'].get(t['name'], {})
        s = p.get('sound') or {}
        inst = next((d for d in p.get('instrument_chain', []) if d.get('preset')), None)
        filt = [d['filter'] for d in p.get('instrument_chain', []) if d.get('filter')]
        rec = {
            'name': t['name'], 'kind': t['type'], 'role': p.get('role'),
            'group': groups.get(t['group']) or ('Returns' if t['type'] == 'ReturnTrack' else 'Ungrouped'),
            'preset': inst and {k: inst.get(k) for k in ('preset', 'pack', 'author', 'category', 'description')},
            'chain': [d['device'] for d in p.get('instrument_chain', [])],
            'filters': filt,
            'samples': (p.get('instrument_chain') or [{}])[0].get('chains') and
                       [c['name'] for c in p['instrument_chain'][0]['chains']][:8],
            'audio_sources': p.get('audio_sources'),
            'descriptors': p.get('descriptors'),
            'similar': p.get('most_similar'),
            'active_bars': p.get('active_bars'),
            'silent': not s,
        }
        if s:
            rec.update({
                'bands': s['band_energy_pct'], 'centroid': s['spectral']['centroid_hz'],
                'flatness': s['spectral']['flatness'], 'attack_ms': s['envelope']['attack_ms_median'],
                'decay_ms': s['envelope']['decay20db_ms_median'], 'width_db': s['stereo']['side_to_mid_db'],
                'corr': s['stereo']['lr_correlation'], 'pulse': s.get('rhythmic_pulse'),
                'rms': s['level']['rms_dbfs'], 'pitch_classes': s.get('chroma_top'),
            })
        m = p.get('music')
        if m:
            rec['midi'] = {k: m.get(k) for k in ('notes', 'step_histogram_16', 'velocity', 'pitch', 'duration_beats')}
        rec['automation'] = [{'device': a['target'].split('/')[-1].split(':')[-1], 'param': a['param'],
                              'min': a['min'], 'max': a['max'], 'points': a['points'], 'shape': a['shape']}
                             for a in p.get('movement', []) if a['points'] > 2]
        tracks.append(rec)
    # spectrograms
    specs = {}
    for fn in sorted(os.listdir(stems)):
        if not fn.endswith('.wav'): continue
        key = fn[len('ProgAcidAI Stems'):-4].strip() or 'Full mix'
        if au.get(fn[:-4], {}).get('silent'): continue
        print('spectrogram', key, flush=True)
        specs[key] = dict(zip(('data', 'seconds'), spectrogram(os.path.join(stems, fn))))
    mix = au['ProgAcidAI Stems']
    # acid automation (time in bars)
    acid = next(t for t in st['tracks'] if t['name'] == 'Acid Bass')
    lanes = {}
    for a in acid['automation']:
        if a['param'] in ('Filter_Frequency', 'Filter_Resonance') and len(a['events']) > 100:
            lanes[a['param']] = [[round(e[0] / 4 + 1, 3), round(e[1], 4)] for e in a['events'] if e[0] > -1e6]
    out = {
        'song': {'tempo': st['tempo'], 'bars': mi['bars'], 'minutes': mi['minutes'], 'key_audio': mix['tonal'],
                 'key_midi': mi['song_key_estimate'], 'pitch_class_weight': mi['song_pitch_class_weight'],
                 'locators': mi['locators'], 'mix_bands': mix['band_energy_pct'], 'mix_centroid': mix['spectral']['centroid_hz'],
                 'mix_level': mix['level'], 'mix_stereo': mix['stereo'],
                 'mix_modulation': mix['modulation_peaks_per_bar']},
        'timeline': mi['timeline_active_bars'], 'sections': mi['sections_4bar'],
        'tracks': tracks, 'spectrograms': specs, 'spec_shape': [BANDS, COLS, FMIN, FMAX],
        'acid_automation': lanes,
    }
    json.dump(out, open(dst, 'w', encoding='utf-8'), separators=(',', ':'), default=float)
    print('bytes', os.path.getsize(dst))

if __name__ == '__main__':
    main(sys.argv[1], sys.argv[2])

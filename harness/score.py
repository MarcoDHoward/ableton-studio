"""Score a rendered sound against a target thumbprint, in musician terms.

The target is one track's thumbprint from thumbprints/<Set>/thumbprints.json. The candidate is a
WAV (a resampled sound-design round, or an exported stem). Both go through the same STFT analysis
(analysis/analyze_audio.py), so the numbers are comparable.

    python harness/score.py <wav> --target "ProgAcidAI:Acid Bass" [--tempo 121] [--json]
"""
import argparse
import json
import math
import os
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(ROOT, 'analysis'))
from analyze_audio import analyze            # noqa: E402
from build_thumbprints import vector, descriptors  # noqa: E402

BAND_LABEL = {'sub': 'sub (20–60 Hz)', 'bass': 'bass (60–250 Hz)', 'low_mid': 'low-mids (250–500 Hz)',
              'mid': 'mids (500 Hz–2 kHz)', 'high_mid': 'high-mids (2–6 kHz)', 'air': 'air (6–20 kHz)'}


def load_target(spec):
    """'ProgAcidAI:Acid Bass' -> that track's thumbprint dict."""
    set_name, _, track = spec.partition(':')
    path = os.path.join(ROOT, 'thumbprints', set_name, 'thumbprints.json')
    with open(path, encoding='utf-8') as fh:
        prints = json.load(fh)['thumbprints']
    if track not in prints:
        raise SystemExit('no track "%s" in %s; have: %s' % (track, path, ', '.join(prints)))
    tp = prints[track]
    if not tp.get('sound') or not tp.get('vector'):
        raise SystemExit('track "%s" has no audio thumbprint (silent or not rendered)' % track)
    return tp


def _hz(v):
    return '%.1f kHz' % (v / 1000) if v >= 1000 else '%d Hz' % round(v)


def _cos_sim(a, b):
    num = sum(x * y for x, y in zip(a, b))
    den = math.sqrt(sum(x * x for x in a)) * math.sqrt(sum(y * y for y in b))
    return num / den if den else 0.0


def compare(cand, target):
    """cand: analyze() output. target: thumbprint. Returns a scorecard dict."""
    t = target['sound']
    findings = []

    def add(severity, text, fix):
        findings.append({'severity': round(severity, 2), 'finding': text, 'try': fix})

    # spectral balance, in percentage points per band
    for band, label in BAND_LABEL.items():
        d = cand['band_energy_pct'][band] - t['band_energy_pct'][band]
        if abs(d) >= 5:
            more = d > 0
            add(abs(d) / 10, '%s %s: %.0f%% of the energy vs %.0f%% in the target' % (
                'Too much' if more else 'Not enough', label, cand['band_energy_pct'][band], t['band_energy_pct'][band]),
                ('cut or high-pass around ' if more else 'boost, or open the filter around ') + label.split('(')[1].rstrip(')'))
    # brightness (spectral centre), in octaves
    c_med, t_med = cand['spectral']['centroid_hz']['median'], t['spectral']['centroid_hz']['median']
    octs = math.log2(max(c_med, 20) / max(t_med, 20))
    if abs(octs) >= 0.25:
        add(abs(octs) * 2, '%s than the target: brightness centre %s vs %s (%+.1f octaves)' % (
            'Brighter' if octs > 0 else 'Darker', _hz(c_med), _hz(t_med), octs),
            'lower the cutoff / reduce drive' if octs > 0 else 'raise the cutoff, add drive or a brighter oscillator')
    # brightness movement (filter sweeps)
    cm, tm = cand['spectral'].get('centroid_motion_ratio') or 1, t['spectral'].get('centroid_motion_ratio') or 1
    if abs(math.log2(cm / tm)) >= 0.7:
        add(abs(math.log2(cm / tm)), '%s filter movement than the target (brightness range ×%.1f vs ×%.1f)' % (
            'More' if cm > tm else 'Less', cm, tm),
            'reduce envelope/LFO amount on the cutoff' if cm > tm else 'add cutoff envelope or automation')
    # noisiness
    cf, tf = cand['spectral']['flatness'], t['spectral']['flatness']
    if max(cf, tf) > 0.01 and abs(math.log10((cf + 1e-4) / (tf + 1e-4))) >= 0.7:  # tiny flatness values are all "tonal"
        add(1.0, '%s than the target (flatness %.3f vs %.3f)' % ('Noisier' if cf > tf else 'Cleaner/more tonal', cf, tf),
            'reduce noise, drive or detune' if cf > tf else 'add noise, drive or a little detune')
    # envelope
    ca, ta = cand['envelope']['attack_ms_median'], t['envelope']['attack_ms_median']
    if ca and ta and abs(math.log2(ca / ta)) >= 1:
        add(abs(math.log2(ca / ta)) * 0.8, 'Attack is %s: %d ms vs %d ms' % ('slower' if ca > ta else 'faster', ca, ta),
            'shorten the amp attack' if ca > ta else 'lengthen the amp attack slightly')
    cd, td = cand['envelope']['decay20db_ms_median'], t['envelope']['decay20db_ms_median']
    if cd and td and abs(math.log2(cd / td)) >= 0.8:
        add(abs(math.log2(cd / td)) * 0.8, 'Notes ring %s: %d ms vs %d ms to fall 20 dB' % ('longer' if cd > td else 'shorter', cd, td),
            'shorten decay/release or the note lengths' if cd > td else 'lengthen decay/release or the note lengths')
    # stereo
    cw, tw = cand['stereo']['side_to_mid_db'], t['stereo']['side_to_mid_db']
    if abs(cw - tw) >= 6:
        add(abs(cw - tw) / 8, '%s than the target (side %.0f dB vs %.0f dB under mid)' % ('Wider' if cw > tw else 'Narrower', cw, tw),
            'reduce chorus/stereo spread' if cw > tw else 'add chorus, auto-pan or stereo spread (keep lows mono)')
    # rhythmic pulse
    cp, tpulse = cand.get('rhythmic_pulse'), t.get('rhythmic_pulse')
    if tpulse and not tpulse.startswith('free') and cp != tpulse:
        add(1.2, 'Pulse is %s, target pulses on %s' % (cp or 'none', tpulse), 'check the sequence/arp rate')
    findings.sort(key=lambda f: -f['severity'])

    cv, tv = vector(cand), target['vector']
    sim = _cos_sim(cv, tv) if cv and tv else 0.0
    # similarity of raw vectors sits high (shared offsets); stretch 0.9..1.0 onto 0..100
    score = max(0.0, min(100.0, (sim - 0.9) / 0.1 * 100))
    return {
        'score': round(score, 1), 'cosine': round(sim, 4),
        'target': target['track'], 'target_descriptors': target.get('descriptors'),
        'candidate_descriptors': descriptors(cand),
        'findings': findings[:5],
        'numbers': {
            'centroid_hz': [c_med, t_med], 'attack_ms': [ca, ta], 'decay_ms': [cd, td],
            'width_db': [cw, tw], 'pulse': [cp, tpulse],
            'bands_pct': {b: [cand['band_energy_pct'][b], t['band_energy_pct'][b]] for b in BAND_LABEL},
        },
    }


def score_wav(wav, target_spec, tempo):
    cand = analyze(wav, tempo)
    if cand.get('silent'):
        return {'score': 0.0, 'error': 'the recording is silent'}
    return compare(cand, load_target(target_spec))


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('wav')
    ap.add_argument('--target', required=True, help='"<Set>:<Track>", e.g. "ProgAcidAI:Acid Bass"')
    ap.add_argument('--tempo', type=float, default=121.0)
    ap.add_argument('--json', action='store_true')
    a = ap.parse_args(argv)
    if hasattr(sys.stdout, 'reconfigure'):
        sys.stdout.reconfigure(encoding='utf-8')
    card = score_wav(a.wav, a.target, a.tempo)
    if a.json:
        print(json.dumps(card, indent=1))
        return 0
    if 'error' in card:
        print('score 0:', card['error'])
        return 1
    print('score %.0f/100 vs %s (cosine %.3f)' % (card['score'], card['target'], card['cosine']))
    for f in card['findings']:
        print('  - %s -> try: %s' % (f['finding'], f['try']))
    if not card['findings']:
        print('  no big differences')
    return 0


if __name__ == '__main__':
    sys.exit(main())

"""Chart data for a phase-loop before/after page (the fixphase skill).

usage: python analysis/phase_report_data.py --before <R0 stems dir> --after <final stems dir> --set set.json
           --before-meter r0_meter.json --after-meter final_meter.json --chosen "Acid Bass=0.65,Lead=0.7"
           [--tempo 121] [--section-bars 8] --out report_data.json

Both stem folders must be full renders from 1.1.1 with the same export settings. The before folder needs
the optimizer's Gram cache (phase_gram_v2.npz), so run phase_optimize.py on it first.
Output (compact JSON, embed it in the page):
  cent, spec.{before,after}.{c,e}   correlation and energy (dB re max) per simulator sub-band, main mix
  bars.{before,after}               broadband correlation per bar, main mix
  sec.{starts, mid, lowmid}         per-section correlation (before, after, sim = chosen, sim0 = none, w = energy share)
  share.{before,after}.<stem>       share of side energy in 300 Hz-1 kHz per section (Gram diagonal)
  levels                            mid / side / stereo level change per band, after vs before (dB)
  bands, stems                      main band table and per-stem broadband figures from the meter JSONs
  sim                               simulator summary for no change and for the chosen settings
"""
import argparse, json, os, sys
import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import phase_meter as pm  # noqa: E402
import phase_optimize as po  # noqa: E402


def main_file(stems):
    for p in sorted(os.listdir(stems)):
        if p.lower().endswith('.wav') and pm.track_name(os.path.join(stems, p), stems) == 'MAIN':
            return os.path.join(stems, p)
    raise SystemExit('no main mix in %s (expected <folder name>.wav)' % stems)


def tables(path, tempo):
    """L², R², Re L·R* and M², S² per simulator sub-band and bar for one stereo file."""
    sr, x, sc = po.read_int(path)
    nfr = 1 + (len(x) - pm.N_FFT) // pm.HOP
    f = np.fft.rfftfreq(pm.N_FFT, 1 / sr)
    sb = np.digitize(f, po.EDGES) - 1
    nS = len(po.CENT)
    bar_of = (((np.arange(nfr) * pm.HOP + pm.N_FFT / 2) / sr) // (240 / tempo)).astype(int)
    T = np.zeros((5, nS, bar_of.max() + 1))
    win = np.hanning(pm.N_FFT).astype(np.float32)
    idx = [np.where(sb == s)[0] for s in range(nS)]
    for a in range(0, nfr, 256):
        b = min(nfr, a + 256)
        seg = np.asarray(x[a * pm.HOP:(b - 1) * pm.HOP + pm.N_FFT], np.float32) / sc
        L = np.fft.rfft(pm.frames(np.ascontiguousarray(seg[:, 0]))[:b - a] * win, axis=1)
        R = np.fft.rfft(pm.frames(np.ascontiguousarray(seg[:, 1]))[:b - a] * win, axis=1)
        q = [np.abs(L) ** 2, np.abs(R) ** 2, (L * R.conj()).real, np.abs((L + R) / 2) ** 2, np.abs((L - R) / 2) ** 2]
        bars = bar_of[a:b]
        for bar in np.unique(bars):
            rows = bars == bar
            for s, ii in enumerate(idx):
                for j in range(5):
                    T[j, s, bar] += q[j][rows][:, ii].sum()
    return T


def corr(l, r, x):
    return x / np.sqrt(np.maximum(l * r, 1e-30))


def r(v, n=4):
    return [round(float(x), n) for x in v]


def build(before, after, set_json, before_meter, after_meter, chosen, tempo=121.0, sec=8):
    out = {}
    Tb, Ta = tables(main_file(before), tempo), tables(main_file(after), tempo)
    nb = min(Tb.shape[2], Ta.shape[2]); Tb, Ta = Tb[:, :, :nb], Ta[:, :, :nb]
    out['cent'] = r(po.CENT, 1)
    out['spec'] = {}
    for tag, T in (('before', Tb), ('after', Ta)):
        e = T[0].sum(1) + T[1].sum(1)
        out['spec'][tag] = {'c': r(corr(T[0].sum(1), T[1].sum(1), T[2].sum(1))), 'e': r(10 * np.log10(e / e.max() + 1e-12), 2)}
    out['levels'] = {}
    for name, lo, hi in (('all', 20, 20000), ('150-300', 150, 300), ('300-1k', 300, 1000), ('1k-10k', 1000, 10000)):
        sel = (po.CENT >= lo) & (po.CENT < hi)
        m0, s0, m1, s1 = Tb[3][sel].sum(), Tb[4][sel].sum(), Ta[3][sel].sum(), Ta[4][sel].sum()
        out['levels'][name] = {'mid_db': round(10 * np.log10(m1 / m0), 2), 'side_db': round(10 * np.log10(s1 / s0), 2),
                               'stereo_db': round(10 * np.log10((m1 + s1) / (m0 + s0)), 2)}
    secs = np.arange(nb) // sec
    out['sec'] = {'size': sec, 'starts': [int(1 + sec * i) for i in range(secs.max() + 1)]}
    for key, lo, hi in (('lowmid', 150, 300), ('mid', 300, 1000)):
        sel = (po.CENT >= lo) & (po.CENT < hi)
        d = {}
        for tag, T in (('before', Tb), ('after', Ta)):
            l, rr, x = (np.bincount(secs, T[j][sel].sum(0)) for j in range(3))
            d[tag] = r(corr(l, rr, x))
            if tag == 'before': d['w'] = r((l + rr) / (l + rr).sum(), 6)
        out['sec'][key] = d

    # simulator, from the Gram cache of the before folder
    cache = os.path.join(before, 'phase_gram_v2.npz')
    if not os.path.exists(cache):
        raise SystemExit('run phase_optimize.py on %s first (needs %s)' % (before, cache))
    z = np.load(cache); G, adj = z['G'], list(z['names'])
    model = po.Model(G, adj + ['(rest of mix)'], adj)
    model.section = sec
    x0 = np.array([po.DEFAULTS[k] for _, k in model.cont])
    _, base = po.objective(model, x0, {}, 0)
    base_top = po.top_side_wide(base)
    out['sim'] = {}
    for tag, spec in (('none', ''), ('chosen', chosen)):
        val, st = po.evaluate(model, spec, base_top)
        out['sim'][tag] = {'spec': spec, 'objective': round(float(val), 3), 'top_side_drop_db': round(float(po.top_side_wide(st) - base_top), 2),
                           'bands': {b: round(v['corr'], 3) for b, v in st['bands'].items()},
                           'all_corr': round(st['all']['corr'], 3), 'mono_loss_db': round(st['all']['mono_loss_db'], 2)}
        for key in ('mid', 'lowmid'):
            out['sec'][key]['sim' if tag == 'chosen' else 'sim0'] = r(st['sec_' + key][0], 3)
    # side-energy share per stem and section, 300 Hz-1 kHz (Gram diagonal; cross terms left out)
    sel = (po.CENT >= 300) & (po.CENT < 1000)
    Gs = G[sel].sum(0)
    sec_g = np.arange(Gs.shape[0]) // sec
    widths = {c[0]: v for c, v in po.parse_spec(chosen).items() if c[1] == 'width'}
    out['share'] = {}
    for tag, w in (('before', {}), ('after', widths)):
        S = np.array([np.bincount(sec_g, Gs[:, 2 * i + 1, 2 * i + 1] * w.get(n, 1.0) ** 2, minlength=sec_g.max() + 1)
                      for i, n in enumerate(model.names)])
        frac = S / np.maximum(S.sum(0), 1e-30)
        out['share'][tag] = {n: r(frac[i]) for i, n in enumerate(model.names) if frac[i].max() > 0.02}

    # meter tables
    mb, ma = json.load(open(before_meter))['stems'], json.load(open(after_meter))['stems']
    out['bands'] = {b: {'energy_pct': mb['MAIN']['bands'][b]['energy_pct'], 'before': mb['MAIN']['bands'][b]['corr'],
                        'after': ma['MAIN']['bands'][b]['corr'], 'loss_before': mb['MAIN']['bands'][b]['mono_loss_db'],
                        'loss_after': ma['MAIN']['bands'][b]['mono_loss_db']} for b in mb['MAIN']['bands']}
    out['bars'] = {'before': [round(b['corr'], 3) for b in mb['MAIN']['per_bar']],
                   'after': [round(b['corr'], 3) for b in ma['MAIN']['per_bar']],
                   'low_before': [b['low_corr'] for b in mb['MAIN']['per_bar']],
                   'low_after': [b['low_corr'] for b in ma['MAIN']['per_bar']]}
    out['stems'] = {}
    for n, t in mb.items():
        u = ma.get(n)
        if not t.get('broadband') or not u or not u.get('broadband'): continue
        out['stems'][n] = {k: [t['broadband'][kk] if kk != 'pct_below_0' else t['short_term'][kk],
                               u['broadband'][kk] if kk != 'pct_below_0' else u['short_term'][kk]]
                           for k, kk in (('corr', 'corr'), ('mono_loss_db', 'mono_loss_db'), ('width_pct', 'width_pct'),
                                         ('pct_below_0', 'pct_below_0'))}
    out['locators'] = [{'bar': int(l['time'] // 4) + 1, 'name': l['name']}
                       for l in json.load(open(set_json, encoding='utf-8')).get('locators', []) if l.get('time', 0) > 0]
    return out


if __name__ == '__main__':
    ap = argparse.ArgumentParser(description=__doc__.split('\n')[0])
    ap.add_argument('--before', required=True); ap.add_argument('--after', required=True)
    ap.add_argument('--set', required=True)
    ap.add_argument('--before-meter', required=True); ap.add_argument('--after-meter', required=True)
    ap.add_argument('--chosen', required=True, help='the settings applied, in phase_optimize --eval syntax')
    ap.add_argument('--tempo', type=float, default=121); ap.add_argument('--section-bars', type=int, default=8)
    ap.add_argument('--out', required=True)
    a = ap.parse_args()
    d = build(a.before, a.after, a.set, a.before_meter, a.after_meter, a.chosen, a.tempo, a.section_bars)
    json.dump(d, open(a.out, 'w', encoding='utf-8'), separators=(',', ':'))
    print('wrote %s (%d KB)' % (a.out, os.path.getsize(a.out) // 1024))
    print('levels', d['levels'])
    print('sim chosen', d['sim']['chosen'])

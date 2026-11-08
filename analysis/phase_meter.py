"""Phase meter for rendered stems: stereo correlation, mid/side balance, mono-fold loss.

For each WAV (a whole stems folder or one file) it reports, over the active part of the audio:
  - correlation: energy-weighted L/R correlation (+1 mono, 0 uncorrelated, -1 anti-phase), plus the
    short-term spread from ~190 ms frames, the way a correlation meter needle moves;
  - side_db: side energy relative to mid (10*log10(S^2/M^2)), and width_pct = S^2 / (M^2 + S^2);
  - mono_loss_db: level of the mono fold (L+R)/2 against the stereo level. 0 dB is a mono source,
    -3 dB is uncorrelated (normal for wide reverb or pads), below -3 dB means real cancellation;
  - all of the above in bands, with the low end split finely below 150 Hz, plus the energy-weighted
    L/R phase angle and any L/R time offset in the low end (Haas, chorus, delay offsets);
  - per bar (with --tempo): the worst bars for low-end correlation and mono loss.
For a folder it also checks low-end interaction between stems: does the kick cancel the bass
(or toms, or a low synth) when they are summed? It reports the sum gain against the power sum,
the correlation and the lag that would line them up.

usage: python analysis/phase_meter.py <stems_dir | file.wav> [--tempo BPM] [--set set.json]
                                       [--bars A-B] [--start-bar N] [--json out.json] [--md out.md]
  --set        set.json from extract_als.py: marks group, return and main stems so pair checks skip them.
  --bars       only measure song bars A..B (needs --tempo), e.g. to compare a fragment render with
               the same bars of a full render.
  --start-bar  song bar at the start of the files (a render that starts at bar 111 -> 111).
"""
import argparse, glob, json, os, sys
import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from analyze_audio import load  # noqa: E402

N_FFT, HOP = 8192, 4096          # 5.4 Hz bins at 44.1 kHz, so 20-40 Hz still has several bins
LOW_HZ = 150
BANDS = [('20-40', 20, 40), ('40-60', 40, 60), ('60-80', 60, 80), ('80-100', 80, 100),
         ('100-150', 100, 150), ('150-300', 150, 300), ('300-1k', 300, 1000),
         ('1k-4k', 1000, 4000), ('4k-10k', 4000, 10000), ('10k-20k', 10000, 20000)]
LOW_BANDS = [b[0] for b in BANDS if b[2] <= LOW_HZ]
ACTIVE_DB = 50                   # frames more than this far below the loudest frame are ignored
EPS = 1e-20

# thresholds for the flags (see thresholds() in the JSON for what each one means)
T = {'low_corr_min': 0.90, 'low_side_db_max': -15.0, 'mono_loss_db_min': -3.5,
     'corr_min': 0.0, 'neg_time_pct_max': 5.0, 'lag_ms_max': 0.25, 'pair_corr_min': -0.2}


def db10(x): return float(10 * np.log10(max(float(x), EPS)))


def frames(x):
    """Hann-windowed frames (n_frames, N_FFT) as a strided view, no copy."""
    if len(x) < N_FFT: x = np.pad(x, (0, N_FFT - len(x)))
    n = 1 + (len(x) - N_FFT) // HOP
    return np.lib.stride_tricks.as_strided(x, (n, N_FFT), (x.strides[0] * HOP, x.strides[0]))


def spectra(x, sr, chunk=256):
    """Per-frame, per-band sums of |L|^2, |R|^2, Re/Im L.R*, and low-band M spectra and L.R* totals."""
    win = np.hanning(N_FFT).astype(np.float32)
    f = np.fft.rfftfreq(N_FFT, 1 / sr)
    sel = [(f >= lo) & (f < hi) for _, lo, hi in BANDS]
    low = f < LOW_HZ
    FL, FR = frames(np.ascontiguousarray(x[:, 0])), frames(np.ascontiguousarray(x[:, 1]))
    n = FL.shape[0]
    LL, RR, RE, IM = (np.zeros((n, len(BANDS))) for _ in range(4))
    tot = np.zeros(n)
    m_low = np.zeros((n, int(low.sum())), np.complex64)
    cross_low = np.zeros(int(low.sum()), np.complex128)
    for a in range(0, n, chunk):
        L = np.fft.rfft(FL[a:a + chunk] * win, axis=1)
        R = np.fft.rfft(FR[a:a + chunk] * win, axis=1)
        pl, pr, c = np.abs(L) ** 2, np.abs(R) ** 2, L * np.conj(R)
        for k, s in enumerate(sel):
            LL[a:a + chunk, k] = pl[:, s].sum(1); RR[a:a + chunk, k] = pr[:, s].sum(1)
            RE[a:a + chunk, k] = c[:, s].real.sum(1); IM[a:a + chunk, k] = c[:, s].imag.sum(1)
        tot[a:a + chunk] = (pl + pr)[:, (f >= 20) & (f <= 20000)].sum(1)
        m_low[a:a + chunk] = ((L + R) / 2)[:, low]
        cross_low += c[:, low].sum(0)
    return {'LL': LL, 'RR': RR, 'RE': RE, 'IM': IM, 'tot': tot, 'm_low': m_low,
            'cross_low': cross_low, 'f_low': f[low], 'sr': sr}


def stats(LL, RR, RE, IM=None):
    """Correlation, side/mid, width and mono loss from summed L/R powers and cross power."""
    mm = (LL + RR + 2 * RE) / 4; ss = (LL + RR - 2 * RE) / 4
    out = {'corr': round(float(RE / np.sqrt(LL * RR + EPS)), 3),
           'side_db': round(db10(ss) - db10(mm), 1),
           'width_pct': round(float(100 * ss / (mm + ss + EPS)), 1),
           'mono_loss_db': round(db10(mm) - db10(mm + ss), 2),
           'balance_db': round(db10(LL) - db10(RR), 2)}
    if IM is not None: out['phase_deg'] = round(float(np.degrees(np.arctan2(IM, RE))), 1)
    return out


def lag_ms(cross_low, f_low, sr, max_ms=5.0):
    """L/R time offset in the low end from the generalised cross-correlation of the low bins."""
    full = np.zeros(N_FFT // 2 + 1, np.complex128); full[:len(cross_low)] = cross_low
    full[:int(np.searchsorted(f_low, 20))] = 0
    r = np.fft.irfft(full, N_FFT)
    k = int(sr * max_ms / 1000)
    lags = np.r_[np.arange(0, k + 1), np.arange(-k, 0)]
    vals = np.r_[r[:k + 1], r[-k:]]
    i = int(np.argmax(vals))
    zero = r[0]
    return {'lag_ms': round(1000 * lags[i] / sr, 3), 'corr_gain_at_lag': round(float(vals[i] / (abs(zero) + EPS)), 3)}


def measure(path, tempo=None, bars=None, start_bar=1):
    sr, x = load(path)
    if 20 * np.log10(np.max(np.abs(x)) + 1e-12) < -80: return {'silent': True}, None
    S = spectra(x, sr)
    tot = S['tot']
    active = 10 * np.log10(tot + EPS) > 10 * np.log10(tot.max() + EPS) - ACTIVE_DB
    if tempo:
        song_bar = ((np.arange(len(tot)) * HOP + N_FFT / 2) / sr // (240.0 / tempo)).astype(int) + start_bar
        if bars: active &= (song_bar >= bars[0]) & (song_bar <= bars[1])
    if not active.any(): return {'silent': True}, None
    a = active
    sums = {k: S[k][a].sum(0) for k in ('LL', 'RR', 'RE', 'IM')}
    out = {'seconds': round(len(x) / sr, 2), 'active_ratio': round(float(a.mean()), 3)}
    bb = [s.sum() for s in (sums['LL'], sums['RR'], sums['RE'])]
    out['broadband'] = stats(*bb)
    # short-term correlation, one value per ~190 ms frame
    fc = S['RE'].sum(1) / np.sqrt(S['LL'].sum(1) * S['RR'].sum(1) + EPS)
    fc = fc[a]
    out['short_term'] = {'median': round(float(np.median(fc)), 3), 'p5': round(float(np.percentile(fc, 5)), 3),
                         'pct_below_0': round(float(100 * (fc < 0).mean()), 1),
                         'pct_below_0.3': round(float(100 * (fc < 0.3).mean()), 1)}
    out['bands'] = {}
    tot_e = (sums['LL'] + sums['RR']).sum()
    for k, (name, _, _) in enumerate(BANDS):
        st = stats(sums['LL'][k], sums['RR'][k], sums['RE'][k], sums['IM'][k])
        st['energy_pct'] = round(float(100 * (sums['LL'][k] + sums['RR'][k]) / (tot_e + EPS)), 2)
        out['bands'][name] = st
    li = [i for i, b in enumerate(BANDS) if b[2] <= LOW_HZ]
    low = stats(*(sums[k][li].sum() for k in ('LL', 'RR', 'RE', 'IM')))
    low['energy_pct'] = round(sum(out['bands'][b]['energy_pct'] for b in LOW_BANDS), 2)
    low.update(lag_ms(S['cross_low'], S['f_low'], sr))
    lf_e = (S['LL'][:, li] + S['RR'][:, li]).sum(1)
    la = lf_e > lf_e.max() * 10 ** (-30 / 10)       # frames where the low end is within 30 dB of its peak
    if la.any():
        lc = S['RE'][la][:, li].sum(1) / np.sqrt(S['LL'][la][:, li].sum(1) * S['RR'][la][:, li].sum(1) + EPS)
        low['short_term_p5'] = round(float(np.percentile(lc, 5)), 3)
        low['pct_below_0.5'] = round(float(100 * (lc < 0.5).mean()), 1)
    out['low'] = low
    if tempo:
        bar_s = 240.0 / tempo
        bar_of = ((np.arange(len(tot)) * HOP + N_FFT / 2) / sr // bar_s).astype(int) + start_bar - 1
        rows = []
        for b in np.unique(bar_of[a]):
            m = a & (bar_of == b)
            st = stats(*(S[k][m][:, li].sum() for k in ('LL', 'RR', 'RE')))
            bst = stats(*(S[k][m].sum() for k in ('LL', 'RR', 'RE')))
            e = float((S['LL'][m][:, li] + S['RR'][m][:, li]).sum())
            rows.append({'bar': int(b) + 1, 'low_corr': st['corr'], 'low_side_db': st['side_db'],
                         'corr': bst['corr'], 'mono_loss_db': bst['mono_loss_db'], '_low_e': e})
        emax = max((r['_low_e'] for r in rows), default=0)
        loud = [r for r in rows if r['_low_e'] > emax * 1e-3]      # bars with real low end
        for r in rows: r.pop('_low_e')
        out['per_bar'] = rows
        out['worst_bars_low'] = sorted(loud, key=lambda r: r['low_corr'])[:6]
        out['worst_bars_mono'] = sorted(rows, key=lambda r: r['mono_loss_db'])[:6]
    out['flags'] = flags(out)
    return out, {'m_low': S['m_low'], 'active': a, 'f_low': S['f_low'], 'sr': sr}


def flags(r):
    fl = []
    low, bb = r['low'], r['broadband']
    if low['energy_pct'] > 1:
        if low['corr'] < T['low_corr_min'] or low['side_db'] > T['low_side_db_max']:
            fl.append('low end not mono (<%d Hz corr %.2f, side %.1f dB)' % (LOW_HZ, low['corr'], low['side_db']))
        if abs(low['lag_ms']) > T['lag_ms_max'] and low['corr_gain_at_lag'] > 1.05:
            fl.append('L/R time offset in the low end (%.2f ms)' % low['lag_ms'])
    if bb['corr'] < T['corr_min']: fl.append('anti-phase overall (corr %.2f)' % bb['corr'])
    if r['short_term']['pct_below_0'] > T['neg_time_pct_max']:
        fl.append('negative correlation %.0f%% of the time' % r['short_term']['pct_below_0'])
    for name, st in r['bands'].items():
        if st['energy_pct'] > 2 and st['mono_loss_db'] < T['mono_loss_db_min']:
            fl.append('cancels in mono at %s Hz (%.1f dB)' % (name, st['mono_loss_db']))
    if abs(bb['balance_db']) > 1.5: fl.append('L/R imbalance %.1f dB' % bb['balance_db'])
    return fl


def pair_check(a, b, max_ms=10.0):
    """Low-end interaction of two stems' mono signals: sum gain vs power sum, correlation, best lag."""
    both = a['active'] & b['active']
    n = min(len(a['m_low']), len(b['m_low']))
    both = both[:n]
    A, B = a['m_low'][:n][both], b['m_low'][:n][both]
    f = a['f_low']; keep = f >= 20
    A, B = A[:, keep], B[:, keep]
    pa, pb = (np.abs(A) ** 2).sum(), (np.abs(B) ** 2).sum()
    if pa <= 0 or pb <= 0: return None
    c = (A * np.conj(B)).sum(0)
    corr = float(c.real.sum() / np.sqrt(pa * pb))
    gain = db10((np.abs(A + B) ** 2).sum()) - db10(pa + pb)
    full = np.zeros(N_FFT // 2 + 1, np.complex128); full[np.where(keep)[0]] = c
    r = np.fft.irfft(full, N_FFT); k = int(a['sr'] * max_ms / 1000)
    lags = np.r_[np.arange(0, k + 1), np.arange(-k, 0)]; vals = np.r_[r[:k + 1], r[-k:]]
    i = int(np.argmax(np.abs(vals)))
    return {'corr': round(corr, 3), 'sum_gain_db': round(gain, 2), 'ratio_db': round(db10(pa) - db10(pb), 1),
            'best_lag_ms': round(1000 * lags[i] / a['sr'], 2),
            'corr_at_best_lag': round(float(vals[i] * N_FFT / 2 / np.sqrt(pa * pb)), 3),  # irfft scale
            'overlap_pct': round(float(100 * both.mean()), 1)}


def track_name(path, stems_dir):
    base = os.path.splitext(os.path.basename(path))[0]
    prefix = os.path.basename(os.path.normpath(stems_dir))
    if base == prefix: return 'MAIN'
    return base[len(prefix):].strip() if base.startswith(prefix) else base


def kinds_from_set(set_json):
    d = json.load(open(set_json, encoding='utf-8'))
    k = {t['name']: {'GroupTrack': 'group', 'ReturnTrack': 'return'}.get(t['type'], 'track') for t in d['tracks']}
    k['MAIN'] = 'main'
    return k


def main(src, tempo=None, set_json=None, bars=None, start_bar=1):
    if os.path.isdir(src):
        files = sorted(p for p in glob.glob(os.path.join(src, '*.wav')))
        names = [track_name(p, src) for p in files]
    else:
        files, names = [src], ['MAIN']
    kinds = kinds_from_set(set_json) if set_json else {'MAIN': 'main'}
    res = {'source': os.path.abspath(src), 'tempo': tempo, 'bars': bars, 'start_bar': start_bar,
           'thresholds': T, 'stems': {}, 'pairs': []}
    lows = {}
    for p, n in zip(files, names):
        r, low = measure(p, tempo, bars, start_bar)
        r['kind'] = kinds.get(n, 'track')
        res['stems'][n] = r
        if low is not None and r['kind'] == 'track' and r['low']['energy_pct'] > 5:
            lows[n] = low
        print('%-28s %s' % (n, 'silent' if r.get('silent') else 'corr %+.2f  low %+.2f  loss %.1f dB  %s' % (
            r['broadband']['corr'], r['low']['corr'], r['broadband']['mono_loss_db'], '; '.join(r['flags']))),
            file=sys.stderr)
    ks = sorted(lows)
    for i in range(len(ks)):
        for j in range(i + 1, len(ks)):
            pc = pair_check(lows[ks[i]], lows[ks[j]])
            if pc and pc['overlap_pct'] > 5: res['pairs'].append({'a': ks[i], 'b': ks[j], **pc})
    res['pairs'].sort(key=lambda p: p['sum_gain_db'])
    return res


def md_report(res):
    S = res['stems']; L = []
    order = sorted((n for n in S if not S[n].get('silent')), key=lambda n: (n != 'MAIN', n.lower()))
    L.append('| Stem | kind | corr | ST p5 | %t<0 | side dB | mono loss dB | <150 corr | <150 side dB | <150 lag ms | flags |')
    L.append('|---|---|---|---|---|---|---|---|---|---|---|')
    for n in order:
        r = S[n]; b = r['broadband']; lo = r['low']
        L.append('| %s | %s | %+.2f | %+.2f | %.0f | %.1f | %.2f | %+.2f | %.1f | %.2f | %s |' % (
            n, r['kind'], b['corr'], r['short_term']['p5'], r['short_term']['pct_below_0'], b['side_db'],
            b['mono_loss_db'], lo['corr'], lo['side_db'], lo['lag_ms'], '; '.join(r['flags']) or 'ok'))
    silent = [n for n in S if S[n].get('silent')]
    if silent: L.append('\nSilent stems: ' + ', '.join(silent))
    if 'MAIN' in S and not S['MAIN'].get('silent'):
        L.append('\n**Main, by band**\n\n| band Hz | energy % | corr | side dB | mono loss dB | phase deg |\n|---|---|---|---|---|---|')
        for bn, st in S['MAIN']['bands'].items():
            L.append('| %s | %.2f | %+.2f | %.1f | %.2f | %.0f |' % (bn, st['energy_pct'], st['corr'], st['side_db'],
                                                                    st['mono_loss_db'], st['phase_deg']))
    if res['pairs']:
        L.append('\n**Low end between stems (<150 Hz, mono)**\n\n| a | b | corr | sum gain dB | best lag ms | corr at lag |\n|---|---|---|---|---|---|')
        for p in res['pairs'][:12]:
            L.append('| %s | %s | %+.2f | %+.2f | %.2f | %+.2f |' % (p['a'], p['b'], p['corr'], p['sum_gain_db'],
                                                                 p['best_lag_ms'], p['corr_at_best_lag']))
    return '\n'.join(L) + '\n'


if __name__ == '__main__':
    ap = argparse.ArgumentParser(description=__doc__.split('\n')[0])
    ap.add_argument('src'); ap.add_argument('--tempo', type=float); ap.add_argument('--set')
    ap.add_argument('--bars', help='song bars A-B'); ap.add_argument('--start-bar', type=int, default=1)
    ap.add_argument('--json'); ap.add_argument('--md')
    a = ap.parse_args()
    bars = tuple(int(v) for v in a.bars.split('-')) if a.bars else None
    if bars and not a.tempo: ap.error('--bars needs --tempo')
    res = main(a.src, a.tempo, a.set, bars, a.start_bar)
    if a.json: json.dump(res, open(a.json, 'w', encoding='utf-8'), indent=1)
    md = md_report(res)
    if a.md: open(a.md, 'w', encoding='utf-8').write(md)
    print(md)

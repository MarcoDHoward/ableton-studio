"""Mono-safe width optimizer: simulate stereo fixes on rendered stems, then search them by gradient.

Utility Width, Bass Mono, channel polarity and low cuts are linear, so their effect on the main mix
can be computed exactly from cross-products of each stem's mid (M) and side (S) spectra. We build
those cross-products once per sub-band and per bar (a Gram tensor), after which any setting is
scored in milliseconds without Live. L-BFGS-B (bounded quasi-Newton gradient descent, numerical
gradients) searches the continuous settings; on/off settings are enumerated.

What the simulator cannot model is left to the real render round: Live's crossover phase, group
and main-bus compression, and anything time-varying (phaser spread, filter LFOs).

The search space and the targets are the ones agreed with the user (SPACE and TARGETS below).

usage: python analysis/phase_optimize.py <stems_dir> --set set.json [--tempo 121] [--out plan.json]
       [--section-bars 8]        targets apply to every N-bar section, weighted by energy
       [--freeze "B-Delay,Main"] leave these tracks (or Main) out of the search
       [--no-switches]           skip the on/off polarity / Left Inv switches
       [--eval "Acid Bass=0.65,Lead=0.7"]  score given settings instead of searching (repeatable)
       [--space space.json] [--targets targets.json]  this set's agreed search space and targets
                                 (the built-in SPACE is the ReggaeBuilder one)
The stems must be rendered with the settings to search from; all settings are relative to them.
"""
import argparse, itertools, json, os, sys, time
import numpy as np
from scipy.io import wavfile
from scipy.optimize import minimize

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import phase_meter as pm  # noqa: E402

EDGES = np.array([20] + list(range(40, 401, 10)) + [500, 700, 1000, 1500, 2000, 3000, 4000, 6000, 8000,
                                                      10000, 14000, 20000], float)
CENT = np.sqrt(EDGES[:-1] * EDGES[1:])

# Agreed search space. kinds: width (Utility Stereo Width), bass_mono (Utility Bass Mono + Bass Freq),
# left_inv (Utility Left Inv), polarity (existing Utility L+R invert, keep or switch off),
# chorus_hp (Chorus-Ensemble high-pass on the wet signal), low_cut (Reverb input low cut).
SPACE = {
    'Lead': ['width', 'bass_mono', 'left_inv'],
    'Moody Melody': ['width', 'bass_mono', 'left_inv'],
    'High Keys': ['width', 'bass_mono'],
    'Shimmer Melody': ['width', 'bass_mono', 'polarity'],
    'Acid Bass': ['width', 'chorus_hp'],
    'Aliens': ['width', 'bass_mono'],
    'Hat Rolls': ['width', 'bass_mono'],
    'Shaker': ['width', 'bass_mono'],
    'Cowbell': ['width', 'bass_mono'],
    'A-Bright Long Verb': ['width', 'bass_mono', 'low_cut'],
    'B-Delay': ['width', 'bass_mono'],
}
BOUNDS = {'width': (0.5, 1.2), 'bass_mono': (60.0, 300.0), 'chorus_hp': (20.0, 300.0),
          'low_cut': (40.0, 300.0), 'main_bass': (120.0, 180.0)}
OFF_BELOW = {'bass_mono': 100.0, 'low_cut': 100.0}   # below this the setting is left off
TARGETS = {'low_corr': 0.98, 'lowmid_corr': 0.85, 'mid_corr': 0.60, 'neg_bar_corr': 0.05,
           'mono_loss_db': -0.8, 'stem_loss_db': -3.0, 'stem_band_pct': 2.0, 'side_drop_db': -2.0}


def side_keep(f, fc):
    """Side gain of a Linkwitz-Riley Bass Mono crossover at fc (power-complementary high band)."""
    return 1.0 / (1.0 + (fc / f) ** 4)


def hp2(f, fc):
    """Magnitude of a 2nd-order Butterworth high-pass (reverb input low cut)."""
    return 1.0 / np.sqrt(1.0 + (fc / f) ** 4)


def read_int(path):
    sr, x = wavfile.read(path, mmap=True)
    scale = {np.dtype('int16'): 32768.0, np.dtype('int32'): 2147483648.0}.get(x.dtype, 1.0)
    return sr, x, scale


def build_gram(groups, tempo, chunk=128, progress=False):
    """Gram tensor G[sub-band, bar, k, l] = Re sum X_k X_l^* over bins and frames, X = [M_1, S_1, ...].

    groups: list of file lists (or plain paths); the stems in one group are summed first, so the
    untouched stems can share one M/S pair and the tensor stays small."""
    groups = [[g] if isinstance(g, (str, tuple)) else g for g in groups]
    groups = [[(e, 1.0) if isinstance(e, str) else e for e in g] for g in groups]   # (path, weight)
    srx = [[read_int(p) + (w,) for p, w in g] for g in groups]
    sr = srx[0][0][0]
    n = min(len(x) for g in srx for _, x, _, _ in g)
    nfr = 1 + (n - pm.N_FFT) // pm.HOP
    f = np.fft.rfftfreq(pm.N_FFT, 1 / sr)
    sb = np.digitize(f, EDGES) - 1                    # sub-band index per bin, -1 / len outside
    valid = (sb >= 0) & (sb < len(CENT))
    bar_s = 240.0 / tempo
    bar_of = (((np.arange(nfr) * pm.HOP + pm.N_FFT / 2) / sr) // bar_s).astype(int)
    nbar = bar_of.max() + 1
    K = 2 * len(groups)
    G = np.zeros((len(CENT), nbar, K, K))
    win = np.hanning(pm.N_FFT).astype(np.float32)
    idx = [np.where(valid & (sb == s))[0] for s in range(len(CENT))]
    for a in range(0, nfr, chunk):
        b = min(nfr, a + chunk)
        X = []
        for g in srx:
            seg = sum(np.asarray(x[a * pm.HOP:(b - 1) * pm.HOP + pm.N_FFT], np.float32) * (w / scale)
                      for _, x, scale, w in g)
            fl = pm.frames(np.ascontiguousarray(seg[:, 0]))[:b - a] * win
            fr = pm.frames(np.ascontiguousarray(seg[:, 1]))[:b - a] * win
            L, R = np.fft.rfft(fl, axis=1).astype(np.complex64), np.fft.rfft(fr, axis=1).astype(np.complex64)
            X += [(L + R) / 2, (L - R) / 2]
        X = np.stack(X)                                # K, frames, bins
        bars = bar_of[a:b]
        for bar in np.unique(bars):
            Xb = X[:, bars == bar]
            for s, ii in enumerate(idx):
                Y = Xb[:, :, ii].reshape(K, -1)
                G[s, bar] += (Y @ Y.conj().T).real
        if progress: print('  gram %d/%d frames' % (b, nfr), file=sys.stderr, flush=True)
    return G


class Model:
    def __init__(self, G, names, adjustable, main_bass0=120.0, space=None):
        self.G = G; self.Gt = G.sum(1); self.names = names; self.adj = adjustable
        self.space = space or SPACE
        self.section = 0                                # 0: targets on the song average
        self.K = G.shape[2]; self.main_bass0 = main_bass0
        e = np.einsum('sbkk->b', G)
        self.active = e > e.max() * 10 ** (-ACTIVE_DB / 10)
        self.Ga = G[:, self.active]
        self.cont = []                                  # (stem, kind) continuous parameters
        for n in adjustable:
            for k in self.space[n]:
                if k in ('width', 'bass_mono', 'chorus_hp', 'low_cut'): self.cont.append((n, k))
        self.cont.append(('Main', 'main_bass'))
        self.lo = np.array([BOUNDS[k][0] for _, k in self.cont]); self.hi = np.array([BOUNDS[k][1] for _, k in self.cont])

    def coeffs(self, p, disc):
        """u_M, u_S: (sub-bands, K) weights that turn stem M/S spectra into main M/S."""
        nS = len(CENT)
        uM = np.zeros((nS, self.K)); uS = np.zeros((nS, self.K))
        per = {}
        for (n, k), v in zip(self.cont, p): per.setdefault(n, {})[k] = v
        for i, n in enumerate(self.names):
            m, s = 2 * i, 2 * i + 1
            a, b, c, d = np.ones(nS), np.zeros(nS), np.zeros(nS), np.ones(nS)   # M'=aM+bS, S'=cM+dS
            q = per.get(n, {})
            if disc.get((n, 'left_inv')):           # L -> -L swaps mid and side
                a, b, c, d = b * 0, -np.ones(nS), -np.ones(nS), d * 0
            if disc.get((n, 'polarity')):           # toggle both-channel polarity
                a, b, c, d = -a, -b, -c, -d
            g = np.ones(nS)
            if 'width' in q: g = g * q['width']
            if 'bass_mono' in q: g = g * side_keep(CENT, q['bass_mono'])   # 60 Hz is effectively off
            if 'chorus_hp' in q: g = g * side_keep(CENT, max(q['chorus_hp'], 20.0)) / side_keep(CENT, 20.0)
            c, d = c * g, d * g
            if 'low_cut' in q:
                h = hp2(CENT, q['low_cut']); a, b, c, d = a * h, b * h, c * h, d * h
            uM[:, m], uM[:, s], uS[:, m], uS[:, s] = a, b, c, d
        mb = per.get('Main', {}).get('main_bass', self.main_bass0)
        uS *= (side_keep(CENT, mb) / side_keep(CENT, self.main_bass0))[:, None]
        return uM, uS

    def energies(self, uM, uS, G):
        """E_MM, E_SS, E_MS for sub-bands (and bars if G has a bar axis)."""
        if G.ndim == 3:
            return (np.einsum('sk,skl,sl->s', uM, G, uM), np.einsum('sk,skl,sl->s', uS, G, uS),
                    np.einsum('sk,skl,sl->s', uM, G, uS))
        return (np.einsum('sk,sbkl,sl->sb', uM, G, uM), np.einsum('sk,sbkl,sl->sb', uS, G, uS),
                np.einsum('sk,sbkl,sl->sb', uM, G, uS))

    def stats(self, p, disc, per_bar=True):
        uM, uS = self.coeffs(p, disc)
        mm, ss, ms = self.energies(uM, uS, self.Gt)
        out = {'bands': {}}
        for name, lo, hi in pm.BANDS:
            sel = (CENT >= lo) & (CENT < hi)
            out['bands'][name] = corr_loss(mm[sel].sum(), ss[sel].sum(), ms[sel].sum())
        for key, lo, hi in (('low', 20, 150), ('lowmid', 150, 300), ('mid', 300, 1000), ('all', 20, 20000),
                            ('top', 1000, 10000)):
            sel = (CENT >= lo) & (CENT < hi)
            out[key] = corr_loss(mm[sel].sum(), ss[sel].sum(), ms[sel].sum())
        if per_bar:
            bm, bs, bx = self.energies(uM, uS, self.Ga)
            M, S, X = bm.sum(0), bs.sum(0), bx.sum(0)
            out['bar_corr'] = (M - S) / np.sqrt(np.maximum((M + S) ** 2 - 4 * X ** 2, 1e-30))
            if self.section:                        # targets per N-bar section, weighted by energy
                sec = np.arange(bm.shape[1]) // self.section
                for key, lo, hi in (('lowmid', 150, 300), ('mid', 300, 1000)):
                    sel = (CENT >= lo) & (CENT < hi)
                    M, S, X = (np.bincount(sec, v[sel].sum(0)) for v in (bm, bs, bx))
                    corr = (M - S) / np.sqrt(np.maximum((M + S) ** 2 - 4 * X ** 2, 1e-30))
                    out['sec_' + key] = (corr, (M + S) / (M + S).sum())
        out['stems'] = {}
        for i, n in enumerate(self.names):
            if n not in self.adj: continue
            sl = slice(2 * i, 2 * i + 2)
            g = self.Gt[:, sl, sl]
            m = np.einsum('sk,skl,sl->s', uM[:, sl], g, uM[:, sl]); s = np.einsum('sk,skl,sl->s', uS[:, sl], g, uS[:, sl])
            tot = (m + s).sum()
            bl = {}
            for name, lo, hi in pm.BANDS:
                sel = (CENT >= lo) & (CENT < hi)
                bl[name] = (100 * (m[sel] + s[sel]).sum() / (tot + 1e-30), pm.db10(m[sel].sum()) - pm.db10((m[sel] + s[sel]).sum()))
            out['stems'][n] = bl
        return out


ACTIVE_DB = 50


def corr_loss(mm, ss, ms):
    corr = (mm - ss) / np.sqrt(max((mm + ss) ** 2 - 4 * ms ** 2, 1e-30))
    return {'corr': float(corr), 'mono_loss_db': pm.db10(mm) - pm.db10(mm + ss), 'side_db': pm.db10(ss) - pm.db10(mm),
            'side_level_db': pm.db10(ss)}


def objective(model, p, disc, base_top_side, w=None):
    st = model.stats(p, disc)
    t = TARGETS
    r = lambda x: max(x, 0.0) ** 2
    if model.section:
        sec = lambda key, tgt: float(np.sum(st['sec_' + key][1] * np.maximum(tgt - st['sec_' + key][0], 0) ** 2))
        lowmid, mid = sec('lowmid', t['lowmid_corr']), sec('mid', t['mid_corr'])
    else:
        lowmid, mid = r(t['lowmid_corr'] - st['lowmid']['corr']), r(t['mid_corr'] - st['mid']['corr'])
    pen = 100 * (r(t['low_corr'] - st['low']['corr']) / 0.02 ** 2 * 0.01
                 + lowmid / 0.1 ** 2 * 0.1 + mid / 0.1 ** 2 * 0.1
                 + r(t['mono_loss_db'] - st['all']['mono_loss_db']) / 0.3 ** 2 * 0.1)
    drop = top_side_wide(st) - base_top_side
    pen += 100 * r(t['side_drop_db'] - drop) / 0.5 ** 2 * 0.1
    bc = st['bar_corr']
    pen += 100 * float(np.mean(np.maximum(t['neg_bar_corr'] - bc, 0) ** 2)) / 0.05 ** 2 * 0.1
    for n, bl in st['stems'].items():
        for name, (pct, loss) in bl.items():
            if pct > t['stem_band_pct']: pen += 10 * r(t['stem_loss_db'] - loss)
    # smallest change: smooth |1 - width|, bass mono / cuts cost by how high they reach
    reg = 0.0
    for (n, k), v in zip(model.cont, p):
        if k == 'width': reg += np.sqrt((1 - v) ** 2 + 1e-4)
        elif k in ('bass_mono', 'low_cut'): reg += 0.5 * max(v - 60, 0) / 240
        elif k == 'chorus_hp': reg += 0.3 * (v - 20) / 280
        elif k == 'main_bass': reg += 0.5 * (v - 120) / 60
    reg += 0.3 * sum(bool(v) for v in disc.values())
    return pen + reg, st


def top_side_wide(st):
    """Side energy in the 1-10 kHz band (dB): the top-end width we must keep."""
    return st['top']['side_level_db']


def optimize(model, x0, discs, base_top, iters=60):
    best = None
    for disc in discs:
        t0 = time.time()
        fun = lambda p: objective(model, p, disc, base_top)[0]
        res = minimize(fun, x0, method='L-BFGS-B', bounds=list(zip(model.lo, model.hi)),
                       options={'maxiter': iters, 'eps': 1e-3})
        p = snap(model, res.x)
        val, st = objective(model, p, disc, base_top)
        print('  %-40s obj %.3f (raw %.3f) %d it %.0fs' % (
            ','.join('%s:%s' % k for k, v in disc.items() if v) or 'no switches', val, res.fun, res.nit, time.time() - t0),
            file=sys.stderr, flush=True)
        if best is None or val < best[0]: best = (val, p, disc, st)
    return best


def snap(model, p):
    p = p.copy()
    for i, (n, k) in enumerate(model.cont):
        if k in OFF_BELOW and p[i] < OFF_BELOW[k]: p[i] = BOUNDS[k][0]
        if k == 'width' and abs(p[i] - 1) < 0.03: p[i] = 1.0
        if k == 'chorus_hp' and p[i] < 30: p[i] = 20.0
        if k in ('bass_mono', 'low_cut', 'chorus_hp', 'main_bass'): p[i] = round(p[i] / 5) * 5
        else: p[i] = round(p[i], 2)
    return p


def summary(st):
    nb = st['bar_corr']
    return {'low_corr': round(st['low']['corr'], 3), 'lowmid_corr': round(st['lowmid']['corr'], 3),
            'mid_corr': round(st['mid']['corr'], 3), 'all_corr': round(st['all']['corr'], 3),
            'mono_loss_db': round(st['all']['mono_loss_db'], 2), 'top_side_db': round(st['top']['side_db'], 2),
            'pct_bars_below_0': round(float(100 * (nb < 0).mean()), 1),
            'bands': {k: {kk: round(vv, 3) for kk, vv in v.items()} for k, v in st['bands'].items()},
            'stem_worst_loss': {n: round(min(l for pct, l in bl.values() if pct > TARGETS['stem_band_pct']), 2)
                                for n, bl in st['stems'].items()},
            'sections': {k: {'worst_corr': round(float(c[w > 0.5 / len(w)].min()), 3),
                             'below_target_pct_energy': round(float(100 * w[c < TARGETS[k + '_corr']].sum()), 1)}
                         for k, (c, w) in ((kk[4:], st[kk]) for kk in ('sec_lowmid', 'sec_mid') if kk in st)}}


DEFAULTS = {'width': 1.0, 'bass_mono': 60.0, 'chorus_hp': 20.0, 'low_cut': 40.0, 'main_bass': 120.0}


def parse_spec(spec):
    """'Acid Bass=0.65, Lead:bass_mono=180, Main:main_bass=150' -> {(track, kind): value}.
    A bare 'Track=value' means width. Switches take 1/0: 'Lead:left_inv=1'."""
    out = {}
    for part in (p.strip() for p in spec.split(',') if p.strip()):
        key, val = part.rsplit('=', 1)
        track, kind = (key.rsplit(':', 1) + ['width'])[:2] if ':' in key else (key, 'width')
        out[(track.strip(), kind.strip())] = float(val)
    return out


def evaluate(model, spec, base_top):
    """Score hand-picked settings with the simulator instead of searching."""
    want = parse_spec(spec) if isinstance(spec, str) else dict(spec)
    p = np.array([DEFAULTS[k] for _, k in model.cont])
    for i, c in enumerate(model.cont):
        if c in want: p[i] = want.pop(c)
    disc = {c: bool(v) for c, v in list(want.items()) if c[1] in ('left_inv', 'polarity')}
    for c in disc: want.pop(c)
    if want: raise SystemExit('not in the search space: %s' % ', '.join('%s:%s' % c for c in want))
    val, st = objective(model, p, disc, base_top)
    return val, st


def top_level(set_json):
    """Names of the tracks that feed Main directly: ungrouped tracks, top groups and returns."""
    d = json.load(open(set_json, encoding='utf-8'))
    return [t['name'] for t in d['tracks'] if str(t.get('group', '-1')) == '-1']


def main(stems, set_json, tempo, out, section=0, freeze=(), switches=True, evals=()):
    top = set(top_level(set_json))
    files, names = [], []
    for p in sorted(os.listdir(stems)):
        if not p.lower().endswith('.wav'): continue
        n = pm.track_name(os.path.join(stems, p), stems)
        if n == 'MAIN': continue
        path = os.path.join(stems, p)
        sr, x, sc = read_int(path)
        if np.max(np.abs(x[::64])) / sc < 1e-4: continue          # silent stem
        files.append(path); names.append(n)
    keep = [i for i, n in enumerate(names) if n in SPACE]
    adj = [names[i] for i in keep]
    tops = [i for i, n in enumerate(names) if n in top]
    # The untouched part of the mix = top-level stems (with their group buses' processing) minus the
    # adjustable stems, so the simulated sum carries the group compressors and saturation.
    residual = [(files[i], 1.0) for i in tops] + [(files[i], -1.0) for i in keep]
    print('stems: %d adjustable; residual = %d top-level stems minus the adjustable ones' % (len(keep), len(tops)),
          file=sys.stderr, flush=True)
    t0 = time.time()
    cache = os.path.join(stems, 'phase_gram_v2.npz')
    if os.path.exists(cache) and list(np.load(cache)['names']) == adj:
        G = np.load(cache)['G']
    else:
        G = build_gram([files[i] for i in keep] + [residual], tempo, progress=True)
        np.savez(cache, G=G, names=np.array(adj))
    print('gram ready in %.0fs' % (time.time() - t0), file=sys.stderr, flush=True)
    space = {n: v for n, v in SPACE.items() if n not in freeze}
    model = Model(G, [names[i] for i in keep] + ['(rest of mix)'], [n for n in adj if n in space], space=space)
    model.section = section
    if 'Main' in freeze:
        model.cont = [c for c in model.cont if c[0] != 'Main']
        model.lo, model.hi = model.lo[:len(model.cont)], model.hi[:len(model.cont)]
    x0 = np.array([{'width': 1.0, 'bass_mono': 60.0, 'chorus_hp': 20.0, 'low_cut': 40.0, 'main_bass': 120.0}[k]
                   for _, k in model.cont])
    base_val, base = objective(model, x0, {}, 0)
    base_top = top_side_wide(base)
    base_val, base = objective(model, x0, {}, base_top)
    if evals:                                       # score given settings, no search
        res = {'stems': os.path.abspath(stems), 'targets': TARGETS, 'before': summary(base),
               'objective_before': round(base_val, 3), 'options': []}
        for spec in evals:
            val, st = evaluate(model, spec, base_top)
            res['options'].append({'spec': spec, 'objective': round(val, 3), 'predicted': summary(st),
                                   'top_side_drop_db': round(top_side_wide(st) - base_top, 2)})
        if out: json.dump(res, open(out, 'w', encoding='utf-8'), indent=1)
        return res, model
    # which switches are allowed: left_inv only where the stem's side is louder than its mid
    sw = []
    for n in adj:
        if 'polarity' in SPACE[n]: sw.append((n, 'polarity'))
        if 'left_inv' in SPACE[n]:
            i = model.names.index(n); g = model.Gt[:, 2 * i:2 * i + 2, 2 * i:2 * i + 2].sum(0)
            if g[1, 1] > g[0, 0] * 10 ** (-1 / 10): sw.append((n, 'left_inv'))
    discs = [dict(zip(sw, combo)) for combo in itertools.product([False, True], repeat=len(sw))] if switches else [{}]
    print('switches: %s -> %d combinations' % (sw, len(discs)), file=sys.stderr)
    val, p, disc, st = optimize(model, x0, discs, base_top)
    plan = {'stems': os.path.abspath(stems), 'targets': TARGETS, 'objective_before': round(base_val, 3),
            'objective_after': round(val, 3), 'before': summary(base), 'predicted': summary(st), 'settings': []}
    for (n, k), v in zip(model.cont, p):
        dflt = {'width': 1.0, 'bass_mono': 60.0, 'chorus_hp': 20.0, 'low_cut': 40.0, 'main_bass': 120.0}[k]
        if v != dflt: plan['settings'].append({'track': n, 'setting': k, 'value': float(v)})
    for (n, k), v in disc.items():
        if v: plan['settings'].append({'track': n, 'setting': k, 'value': True})
    if out: json.dump(plan, open(out, 'w', encoding='utf-8'), indent=1)
    return plan, model


if __name__ == '__main__':
    ap = argparse.ArgumentParser(description=__doc__.split('\n')[0])
    ap.add_argument('stems'); ap.add_argument('--set', required=True); ap.add_argument('--tempo', type=float, default=121)
    ap.add_argument('--out')
    ap.add_argument('--section-bars', type=int, default=0, help='apply band targets to every N-bar section')
    ap.add_argument('--freeze', default='', help='comma list of tracks (or Main) to leave alone')
    ap.add_argument('--no-switches', action='store_true', help='skip the on/off polarity switches')
    ap.add_argument('--eval', action='append', default=[], metavar='SPEC',
                    help='score these settings instead of searching, e.g. "Acid Bass=0.65,Lead=0.7,Lead:bass_mono=180"; repeatable')
    ap.add_argument('--space', help='JSON file {track: [kinds]} replacing the built-in SPACE (the agreed search space for this set)')
    ap.add_argument('--targets', help='JSON file with target overrides, merged into TARGETS')
    a = ap.parse_args()
    if a.space:
        SPACE.clear(); SPACE.update(json.load(open(a.space, encoding='utf-8')))
        bad = {k for v in SPACE.values() for k in v} - {'width', 'bass_mono', 'left_inv', 'polarity', 'chorus_hp', 'low_cut'}
        if bad: raise SystemExit('unknown kinds in --space: %s' % ', '.join(sorted(bad)))
    if a.targets:
        TARGETS.update(json.load(open(a.targets, encoding='utf-8')))
    plan, _ = main(a.stems, a.set, a.tempo, a.out, a.section_bars,
                   tuple(x.strip() for x in a.freeze.split(',') if x.strip()), not a.no_switches, tuple(a.eval))
    if a.eval:
        print('before', {k: v for k, v in plan['before'].items() if k != 'bands'})
        for o in plan['options']:
            print('%s  obj %.3f  side 1-10k %+.2f dB' % (o['spec'], o['objective'], o['top_side_drop_db']))
            print('  ', {k: v for k, v in o['predicted'].items() if k not in ('bands', 'stem_worst_loss')})
        sys.exit(0)
    print(json.dumps({k: plan[k] for k in ('objective_before', 'objective_after', 'settings')}, indent=1))
    for k in ('before', 'predicted'):
        print(k, {kk: vv for kk, vv in plan[k].items() if kk not in ('bands',)})

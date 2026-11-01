"""Time-frequency analysis of rendered stems -> per-track audio fingerprint.

For each WAV: STFT (Hann 4096 / hop 1024), long-term 1/3-octave spectrum, band
energy split, spectral centroid/rolloff/flatness/flux, 13 MFCCs, chroma + key,
stereo width, envelope attack/decay, and a modulation spectrum (FFT of the loudness
envelope, expressed per bar) that captures pumping, gating and rhythmic motion.
Also writes a log-frequency spectrogram PNG per stem.

usage: python analyze_audio.py <stems_dir> <tempo_bpm> <out.json> <png_dir>
"""
import json, os, sys
import numpy as np
from scipy.io import wavfile
from scipy.signal import stft, find_peaks
from scipy.fft import dct

NAMES = ['C', 'C#', 'D', 'D#', 'E', 'F', 'F#', 'G', 'G#', 'A', 'A#', 'B']
MAJ = np.array([6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88])
MIN = np.array([6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17])
BANDS = [('sub', 20, 60), ('bass', 60, 250), ('low_mid', 250, 500), ('mid', 500, 2000),
         ('high_mid', 2000, 6000), ('air', 6000, 20000)]
THIRD_OCT = [round(1000 * 2 ** (k / 3), 1) for k in range(-17, 14)]  # 20 Hz .. 20 kHz centres
N_FFT, HOP = 4096, 1024

def load(path):
    sr, x = wavfile.read(path)
    if x.dtype == np.int16: x = x.astype(np.float32) / 32768
    elif x.dtype == np.int32: x = x.astype(np.float32) / 2147483648
    else: x = x.astype(np.float32)
    if x.ndim == 1: x = np.stack([x, x], 1)
    return sr, x

def db(x, floor=1e-12): return 10 * np.log10(np.maximum(x, floor))

def mel_fb(sr, n_fft, n_mels=40, fmin=20, fmax=16000):
    hz2mel = lambda f: 2595 * np.log10(1 + f / 700)
    mel2hz = lambda m: 700 * (10 ** (m / 2595) - 1)
    pts = mel2hz(np.linspace(hz2mel(fmin), hz2mel(fmax), n_mels + 2))
    bins = np.floor((n_fft + 1) * pts / sr).astype(int)
    fb = np.zeros((n_mels, n_fft // 2 + 1))
    for m in range(1, n_mels + 1):
        a, b, c = bins[m - 1], bins[m], bins[m + 1]
        if b > a: fb[m - 1, a:b] = (np.arange(a, b) - a) / (b - a)
        if c > b: fb[m - 1, b:c] = (c - np.arange(b, c)) / (c - b)
    return fb

def key_of(chroma):
    best = []
    for r in range(12):
        for prof, mode in ((MAJ, 'major'), (MIN, 'minor')):
            best.append((float(np.corrcoef(chroma, np.roll(prof, r))[0, 1]), '%s %s' % (NAMES[r], mode)))
    best.sort(reverse=True)
    return {'key': best[0][1], 'confidence': round(best[0][0], 3), 'runner_up': best[1][1]}

def analyze(path, tempo, png=None):
    sr, x = load(path)
    mid, side = x.mean(1), (x[:, 0] - x[:, 1]) / 2
    f, t, Z = stft(mid, sr, window='hann', nperseg=N_FFT, noverlap=N_FFT - HOP, boundary=None)
    P = (np.abs(Z) ** 2).astype(np.float32)                      # power, bins x frames
    frame_e = P.sum(0)
    if frame_e.max() <= 0 or 20 * np.log10(np.max(np.abs(x)) + 1e-12) < -80: return {'silent': True}
    active = db(frame_e) > db(frame_e.max()) - 50                # ignore silence / tails
    Pa = P[:, active]
    out = {'seconds': round(len(mid) / sr, 2), 'active_ratio': round(float(active.mean()), 3)}
    # loudness
    rms = np.sqrt(np.mean(mid ** 2)); peak = np.max(np.abs(x))
    fr_rms = np.sqrt(frame_e[active] / N_FFT)
    out['level'] = {'rms_dbfs': round(float(20 * np.log10(rms + 1e-12)), 1),
                    'peak_dbfs': round(float(20 * np.log10(peak + 1e-12)), 1),
                    'crest_db': round(float(20 * np.log10((peak + 1e-12) / (rms + 1e-12))), 1),
                    'dynamic_range_db': round(float(np.percentile(db(fr_rms ** 2), 95) - np.percentile(db(fr_rms ** 2), 10)), 1)}
    # long-term average spectrum
    ltas = Pa.mean(1)
    tot = ltas[(f >= 20) & (f <= 20000)].sum()
    out['band_energy_pct'] = {n: round(float(100 * ltas[(f >= lo) & (f < hi)].sum() / tot), 2) for n, lo, hi in BANDS}
    third = []
    for c in THIRD_OCT:
        lo, hi = c / 2 ** (1 / 6), c * 2 ** (1 / 6)
        sel = (f >= lo) & (f < hi)
        third.append(float(ltas[sel].mean()) if sel.any() else 0.0)
    third_db = db(np.array(third)); third_db -= third_db.max()
    out['third_octave_db'] = {str(c): round(float(v), 1) for c, v in zip(THIRD_OCT, third_db)}
    # spectral shape per frame
    fa = f[:, None]; S = Pa / (Pa.sum(0, keepdims=True) + 1e-20)
    cen = (fa * S).sum(0)
    cum = np.cumsum(S, 0); roll = f[np.argmax(cum >= 0.85, 0)]
    bw = np.sqrt((((fa - cen) ** 2) * S).sum(0))
    flat = np.exp(np.mean(np.log(Pa[1:] + 1e-20), 0)) / (np.mean(Pa[1:], 0) + 1e-20)
    mag = np.sqrt(Pa); flux = np.sqrt((np.maximum(np.diff(mag, axis=1), 0) ** 2).sum(0))
    out['spectral'] = {
        'centroid_hz': {'median': round(float(np.median(cen))), 'p10': round(float(np.percentile(cen, 10))),
                        'p90': round(float(np.percentile(cen, 90)))},
        'rolloff85_hz': round(float(np.median(roll))),
        'bandwidth_hz': round(float(np.median(bw))),
        'flatness': round(float(np.median(flat)), 4),           # 0 = pure tone, 1 = white noise
        'flux_norm': round(float(np.median(flux) / (np.median(mag.sum(0)) + 1e-20)), 4),
    }
    # brightness motion over the song: centroid per bar (filter sweeps)
    spb = 4 * 60 / tempo
    bar_idx = (t[active] // spb).astype(int)
    per_bar = [float(np.median(cen[bar_idx == b])) if (bar_idx == b).any() else None for b in range(int(t[-1] // spb) + 1)]
    pb = np.array([v for v in per_bar if v is not None])
    out['spectral']['centroid_per_bar_hz'] = [round(v) if v is not None else None for v in per_bar]
    out['spectral']['centroid_motion_ratio'] = round(float(np.percentile(pb, 95) / max(np.percentile(pb, 5), 1)), 2) if len(pb) else None
    # MFCC (timbre)
    fb = mel_fb(sr, N_FFT)
    mel = db(fb @ Pa)
    mf = dct(mel, type=2, axis=0, norm='ortho')[:13]
    out['mfcc_mean'] = [round(float(v), 2) for v in mf.mean(1)]
    out['mfcc_std'] = [round(float(v), 2) for v in mf.std(1)]
    # chroma (tonal content) from 55 Hz - 5 kHz
    sel = (f >= 55) & (f <= 5000)
    pcs = (np.round(12 * np.log2(f[sel] / 440.0)) + 9) % 12
    chroma = np.array([ltas[sel][pcs == k].sum() for k in range(12)])
    chroma = chroma / (chroma.sum() + 1e-20)
    out['chroma'] = {NAMES[k]: round(float(chroma[k]), 3) for k in range(12)}
    out['chroma_top'] = [NAMES[k] for k in np.argsort(-chroma)[:4]]
    out['tonal'] = key_of(chroma) if out['spectral']['flatness'] < 0.3 else None  # reliable on full mixes only
    # dominant low peak (fundamental-ish) for pitched material
    lowsel = (f >= 30) & (f <= 1000)
    pk = f[lowsel][np.argmax(ltas[lowsel])]
    out['dominant_peak_hz'] = round(float(pk), 1)
    out['dominant_peak_note'] = '%s%d' % (NAMES[int(round(12 * np.log2(pk / 440) + 9)) % 12], int(round(12 * np.log2(pk / 440) + 69)) // 12 - 2)
    # stereo
    se, me = np.mean(side ** 2), np.mean(mid ** 2)
    out['stereo'] = {'side_to_mid_db': round(float(db(se) - db(me)), 1),
                     'lr_correlation': round(float(np.corrcoef(x[:, 0], x[:, 1])[0, 1]) if x[:, 0].std() and x[:, 1].std() else 1.0, 3)}
    # fine envelope (5.8 ms hop) -> attack / decay around onsets, and modulation spectrum
    h = 256
    env = np.sqrt(np.convolve(mid ** 2, np.ones(h) / h, 'same')[::h])
    env_db = db(env ** 2)
    fps = sr / h
    on_curve = np.maximum(np.diff(env_db, prepend=env_db[0]), 0)
    peaks, _ = find_peaks(env_db, prominence=6, distance=max(1, int(0.06 * fps)))
    peaks = peaks[env_db[peaks] > env_db.max() - 40]
    atk, dec = [], []
    prev = 0
    for p in peaks[:4000]:
        lo = max(prev, p - int(0.25 * fps))                  # only look back to the previous hit
        prev = p
        seg = env_db[lo:p + 1]
        trough = lo + int(np.argmin(seg))                    # rise from the preceding trough
        rise = env_db[trough:p + 1]
        start = trough + int(np.argmax(rise >= env_db[trough] + 0.1 * (env_db[p] - env_db[trough])))
        atk.append((p - start) / fps * 1000)                 # 10% -> 100% of the rise, in ms
        tail = env_db[p:p + int(2 * fps)]
        below = np.where(tail < env_db[p] - 20)[0]
        if len(below): dec.append(below[0] / fps * 1000)
    out['envelope'] = {
        'events': int(len(peaks)),
        'events_per_bar': round(len(peaks) / max(1, out['seconds'] * out['active_ratio'] / spb), 2),
        'attack_ms_median': round(float(np.median(atk)), 1) if atk else None,
        'decay20db_ms_median': round(float(np.median(dec)), 1) if dec else None,
    }
    # modulation spectrum of the loudness envelope, in cycles per bar
    e = env[: int(len(env) // 2 * 2)]
    e = (e - e.mean()) * np.hanning(len(e))
    M = np.abs(np.fft.rfft(e)) ** 2
    mf_hz = np.fft.rfftfreq(len(e), 1 / fps)
    per_bar_cyc = mf_hz * spb
    keep = (per_bar_cyc >= 0.5) & (per_bar_cyc <= 32)
    Mk, Ck = M[keep], per_bar_cyc[keep]
    pk_i, _ = find_peaks(Mk, distance=max(1, int(0.2 / (Ck[1] - Ck[0]))))
    top = sorted(pk_i, key=lambda i: Mk[i], reverse=True)[:5]
    out['modulation_peaks_per_bar'] = [{'cycles_per_bar': round(float(Ck[i]), 2),
                                        'rel_db': round(float(db(Mk[i]) - db(Mk[top[0]])), 1)} for i in top]
    grid = {'1/1': 1, '1/2': 2, '1/4': 4, '1/8': 8, '1/16': 16, 'dotted 1/8': 16 / 3, '1/4T': 6, '1/8T': 12}
    if top:
        c0 = Ck[top[0]]
        name = min(grid, key=lambda k: abs(np.log2(grid[k] / c0)))
        out['rhythmic_pulse'] = name if abs(np.log2(grid[name] / c0)) < 0.08 else 'free %.2f/bar' % c0
    if png:
        plot(f, t, P, png, os.path.basename(path))
    return out

def plot(f, t, P, png, title):
    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.pyplot as plt
    edges = np.geomspace(20, 20000, 161)
    idx = np.digitize(f, edges) - 1
    L = np.zeros((160, P.shape[1]), np.float32)
    centres = np.sqrt(edges[:-1] * edges[1:])
    for b in range(160):
        sel = idx == b
        L[b] = P[sel].mean(0) if sel.any() else P[int(np.argmin(np.abs(f - centres[b])))]  # narrow low bands: nearest bin
    Ld = db(L); Ld -= Ld.max()
    step = max(1, P.shape[1] // 1600)
    fig, ax = plt.subplots(figsize=(14, 3.6), dpi=110)
    ax.imshow(Ld[:, ::step], origin='lower', aspect='auto', vmin=-80, vmax=0, cmap='magma',
              extent=[t[0], t[-1], 0, 160])
    ticks = [50, 100, 200, 500, 1000, 2000, 5000, 10000]
    ax.set_yticks([np.searchsorted(edges, v) for v in ticks]); ax.set_yticklabels(['50', '100', '200', '500', '1k', '2k', '5k', '10k'])
    ax.set_xlabel('seconds'); ax.set_ylabel('Hz'); ax.set_title(title)
    fig.tight_layout(); fig.savefig(png); plt.close(fig)

def main(stems, tempo, dst, png_dir):
    os.makedirs(png_dir, exist_ok=True)
    res = {}
    for fn in sorted(os.listdir(stems)):
        if not fn.lower().endswith('.wav'): continue
        name = os.path.splitext(fn)[0]
        print('analysing', fn, flush=True)
        try:
            res[name] = analyze(os.path.join(stems, fn), tempo, os.path.join(png_dir, name + '.png'))
        except Exception as e:  # keep going; record the failure
            res[name] = {'error': repr(e)}
        json.dump(res, open(dst, 'w', encoding='utf-8'), indent=1)
    return res

if __name__ == '__main__':
    r = main(sys.argv[1], float(sys.argv[2]), sys.argv[3], sys.argv[4])
    for n, a in r.items():
        if 'spectral' not in a: print(n, a); continue
        print('%-34s rms=%6.1f cen=%5d flat=%.3f width=%5.1f atk=%s dec=%s pulse=%-12s pcs=%s' % (
            n[:34], a['level']['rms_dbfs'], a['spectral']['centroid_hz']['median'], a['spectral']['flatness'],
            a['stereo']['side_to_mid_db'], a['envelope']['attack_ms_median'], a['envelope']['decay20db_ms_median'],
            a.get('rhythmic_pulse'), ' '.join(a['chroma_top'])))

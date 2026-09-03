#!/usr/bin/env python3
"""
Measure how far apart two devices actually are.

Record both devices playing together on a third device, save it as a wav,
and pass it in. The two speakers arrive at the mic at different times, so
the recording contains the same music twice, slightly offset. Autocorrelating
the recording against itself finds that offset.

    python3 tools/offset.py room.wav

Works best with a percussive track. Record a few seconds from a spot roughly
equidistant from both devices, otherwise you measure the speed of sound
across the room as well (about 3ms per metre of difference).
"""

import sys
import wave
import array
import math


def read_mono(path):
    with wave.open(path, 'rb') as w:
        if w.getsampwidth() != 2:
            sys.exit("need 16 bit wav")
        rate = w.getframerate()
        raw = array.array('h', w.readframes(w.getnframes()))
    ch = w.getnchannels()
    if ch > 1:
        raw = array.array('h', [sum(raw[i:i + ch]) // ch
                                for i in range(0, len(raw) - ch, ch)])
    return raw, rate


def envelope(sig, rate, hop_ms=1.0):
    # energy envelope at 1ms resolution. offsets in this range are about
    # onset timing, not waveform phase, so the envelope is enough and it
    # makes the correlation far cheaper
    hop = max(1, int(rate * hop_ms / 1000))
    out = []
    for i in range(0, len(sig) - hop, hop):
        acc = 0
        for s in sig[i:i + hop]:
            acc += s * s
        out.append(math.sqrt(acc / hop))
    # half wave rectified difference: keep only rising energy. a decaying
    # tail correlates with itself at every lag and would otherwise drown
    # the real echo
    onset = [max(0.0, out[i] - out[i - 1]) for i in range(1, len(out))]

    # thin each onset to its local peak. an attack is a few ms wide, and
    # that width alone correlates strongly at small lags, which otherwise
    # beats the echo we are looking for
    w = 3
    peaks = []
    for i, v in enumerate(onset):
        lo, hi = max(0, i - w), min(len(onset), i + w + 1)
        peaks.append(v if v >= max(onset[lo:hi]) and v > 0 else 0.0)

    mean = sum(peaks) / len(peaks) if peaks else 0
    return [v - mean for v in peaks], hop_ms


def best_offset(env, max_ms=400):
    n = len(env)
    best, best_lag = 0.0, 0
    # lag 0 is the trivial self match, everything past it is fair game
    for lag in range(2, min(max_ms, n // 2)):
        acc = 0.0
        for i in range(n - lag):
            acc += env[i] * env[i + lag]
        acc /= (n - lag)
        if acc > best:
            best, best_lag = acc, lag
    return best_lag, best


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    sig, rate = read_mono(sys.argv[1])
    if len(sig) < rate:
        sys.exit("recording is too short, give it a few seconds")

    env, hop_ms = envelope(sig, rate)
    lag, strength = best_offset(env)

    print(f"recording: {len(sig)/rate:.1f}s at {rate}Hz")
    if lag == 0 or strength <= 0:
        print("no second copy found. either the devices are already tight")
        print("or only one of them was audible in the recording.")
        return

    print(f"offset between devices: {lag * hop_ms:.0f}ms")
    if lag * hop_ms < 20:
        print("under 20ms, that is inside the echo threshold. good.")
    else:
        print(f"nudge the device that is behind by about {lag * hop_ms:.0f}ms")


if __name__ == '__main__':
    main()

// Clock sync and live playback.
//
// The host owns the clock and captures whatever it is playing. Every chunk
// arrives stamped with the host time at which its first sample should be
// heard. A speaker measures its offset from the host clock, converts that
// stamp into its own audio clock, and schedules the chunk there. Nothing is
// ever played "as soon as it arrives", which is what keeps devices together.

const SYNC_BURST = 8;        // pings per round, keep the least delayed one
const RESYNC_MS = 15000;
const MAGIC = 0x4c414154;    // "TAAL"
const HEADER = 20;           // magic + startAt + seq
// how far the anchored timeline may drift from the clock before it is
// worth the audible reseat. well above clock jitter, well below the buffer.
const RESEAT_S = 0.05;

export class Clock {
  constructor(ws) {
    this.ws = ws;
    this.offset = 0;         // hostTime - localTime
    this.base = null;        // first offset seen, the display baseline
    this.drift = 0;          // how far the offset has moved since then
    this.rtt = 0;
    this.best = null;
    this.ready = false;
  }

  accept(t0, hostTs) {
    const t1 = performance.now();
    const rtt = t1 - t0;
    // assume the trip is symmetric: host clock at t1 was hostTs + rtt/2
    const offset = hostTs + rtt / 2 - t1;
    if (!this.best || rtt < this.best.rtt) {
      this.best = { rtt, offset };
      // the raw offset is dominated by the gap between the two time
      // origins, epoch against page load, which says nothing about sync
      // quality. what is worth watching is how much it moves.
      if (this.base === null) this.base = offset;
      this.drift = offset - this.base;
      this.offset = offset;
      this.rtt = rtt;
      this.ready = true;
    }
  }

  ping() {
    this.best = null;
    for (let i = 0; i < SYNC_BURST; i++) {
      setTimeout(() => {
        if (this.ws.readyState === WebSocket.OPEN) {
          this.ws.send(JSON.stringify({ type: 'sync', t0: performance.now() }));
        }
      }, i * 40);
    }
  }

  start() {
    this.ping();
    this.timer = setInterval(() => this.ping(), RESYNC_MS);
  }

  stop() {
    clearInterval(this.timer);
  }

  now() {
    return performance.now() + this.offset;
  }
}

export class LivePlayer {
  constructor(clock, onState) {
    this.clock = clock;
    this.onState = onState;
    this.ctx = null;
    this.gain = null;
    this.volume = 1;
    this.nudgeMs = 0;
    this.rate = 48000;
    this.channels = 2;
    this.queued = 0;         // chunks scheduled but not yet played
    this.late = 0;           // chunks that arrived past their play time
    this.played = 0;
    this.lastLeadMs = 0;
    this.anchor = null;      // audio clock time of anchorSeq
    this.anchorSeq = 0;
  }

  // must be called from a user gesture, browsers refuse audio otherwise
  async unlock() {
    if (!this.ctx) {
      // never force a sample rate. ios hardware runs at 44100 and safari
      // will not open a context at a rate it does not want, so take what
      // the device gives and resample chunks into it instead
      const Ctor = window.AudioContext || window.webkitAudioContext;
      this.ctx = new Ctor();

      this.gain = this.ctx.createGain();
      this.gain.gain.value = this.volume;
      this.route();

      // safari starts suspended and stays that way until a gesture, and
      // silently ignores audio scheduled while suspended
      this.silentKick();
    }
    if (this.ctx.state === 'suspended') await this.ctx.resume();
  }

  // Send the graph through a real audio element rather than straight to the
  // speakers. ios treats a page with a playing media element as a media
  // player, which is what earns the lock screen controls and stops the
  // audio being suspended the moment the tab stops being looked at.
  // Falling back to ctx.destination is fine, it just loses that protection.
  route() {
    try {
      const sink = this.ctx.createMediaStreamDestination();
      const el = document.createElement('audio');
      el.srcObject = sink.stream;
      el.autoplay = true;
      el.playsInline = true;
      // the element is the output, muting it would mute everything
      el.volume = 1;
      document.body.appendChild(el);
      this.gain.connect(sink);
      this.sink = sink;
      this.sinkEl = el;
      const p = el.play();
      if (p && p.catch) p.catch(() => this.fallback());
    } catch (e) {
      this.fallback();
    }
  }

  fallback() {
    if (this.routedDirect) return;
    this.routedDirect = true;
    // drop the element route first, otherwise both paths play and the
    // volume doubles
    if (this.sink) {
      try {
        this.gain.disconnect(this.sink);
      } catch (e) {
        // was never connected
      }
      this.sink = null;
    }
    if (this.sinkEl) {
      this.sinkEl.remove();
      this.sinkEl = null;
    }
    this.gain.connect(this.ctx.destination);
  }

  // ios keeps the audio hardware asleep until something actually plays,
  // so a zero length buffer inside the gesture wakes it
  silentKick() {
    const b = this.ctx.createBuffer(1, 1, this.ctx.sampleRate);
    const s = this.ctx.createBufferSource();
    s.buffer = b;
    s.connect(this.ctx.destination);
    s.start(0);
  }

  setVolume(v) {
    this.volume = Math.min(1, Math.max(0, v));
    if (this.gain) {
      // a ramp instead of a jump, a step in gain is an audible click
      this.gain.gain.setTargetAtTime(this.volume, this.ctx.currentTime, 0.02);
    }
  }

  setNudge(ms) {
    this.nudgeMs = ms;
  }

  // total delay between ctx.currentTime and sound leaving the speaker
  outputLatency() {
    const base = this.ctx.baseLatency || 0;
    const out = this.ctx.outputLatency || 0;
    return (base + out) * 1000;
  }

  // the whole scheduling decision lives here: convert a host timestamp into
  // a moment on this device's audio clock
  hostToAudio(hostMs) {
    const aheadMs = hostMs - this.clock.now() - this.nudgeMs
      - this.outputLatency();
    return this.ctx.currentTime + aheadMs / 1000;
  }

  push(buf) {
    if (!this.ctx || !this.clock.ready) return;

    // safari suspends the context on its own (screen dimming, app switch)
    // and then silently swallows everything scheduled after
    if (this.ctx.state === 'suspended') {
      this.ctx.resume();
      return;
    }

    const view = new DataView(buf);
    if (view.getUint32(0, true) !== MAGIC) return;
    const startAt = view.getFloat64(4, true);
    const seq = Number(view.getBigInt64(12, true));

    // Placing every chunk from a fresh clock reading sounds wrong: the
    // estimate jitters by a fraction of a millisecond, so consecutive
    // buffers overlap or leave a hole and the seam clicks 50 times a
    // second. Anchor once, then place each chunk by its sample index so
    // neighbours butt together exactly, and only re-anchor if we have
    // genuinely lost the thread.
    let when;
    if (this.anchor === null || seq < this.anchorSeq) {
      when = this.hostToAudio(startAt);
      this.anchor = when;
      this.anchorSeq = seq;
    } else {
      when = this.anchor + (seq - this.anchorSeq) / this.rate;
      const ideal = this.hostToAudio(startAt);
      // a real drift, not jitter, means the anchor is stale
      if (Math.abs(ideal - when) > RESEAT_S) {
        when = ideal;
        this.anchor = when;
        this.anchorSeq = seq;
      }
    }

    const leadMs = (when - this.ctx.currentTime) * 1000;
    this.lastLeadMs = leadMs;

    // already past due. playing it now would be out of sync with every
    // other speaker, and a partial chunk clicks, so drop it.
    if (when <= this.ctx.currentTime) {
      this.late++;
      this.anchor = null;
      return;
    }

    const pcm = new Int16Array(buf, HEADER);
    const frames = pcm.length / this.channels;
    if (frames < 1) return;

    // the device decides its own rate. on ios that is usually 44100 while
    // the stream is 48000, so resample or everything plays 9% fast
    const devRate = this.ctx.sampleRate;
    const ratio = devRate / this.rate;
    const outFrames = Math.max(1, Math.round(frames * ratio));

    const audio = this.ctx.createBuffer(this.channels, outFrames, devRate);
    for (let ch = 0; ch < this.channels; ch++) {
      const out = audio.getChannelData(ch);
      if (outFrames === frames) {
        for (let i = 0; i < frames; i++) {
          out[i] = pcm[i * this.channels + ch] / 32768;
        }
      } else {
        // linear interpolation. chunks are 20ms so the seam error is tiny,
        // and it beats the alternative of wrong pitch
        for (let i = 0; i < outFrames; i++) {
          const src = i / ratio;
          const i0 = Math.floor(src);
          const i1 = Math.min(frames - 1, i0 + 1);
          const f = src - i0;
          const a = pcm[i0 * this.channels + ch] / 32768;
          const b = pcm[i1 * this.channels + ch] / 32768;
          out[i] = a + (b - a) * f;
        }
      }
    }

    const src = this.ctx.createBufferSource();
    src.buffer = audio;
    src.connect(this.gain);
    src.start(when);
    this.queued++;
    this.played++;
    src.onended = () => { this.queued--; };

    if (this.onState) {
      this.onState({ leadMs, late: this.late, played: this.played });
    }
  }

  configure(rate, channels) {
    this.rate = rate || this.rate;
    this.channels = channels || this.channels;
  }

  stop() {
    // nothing to tear down: chunks are fire and forget, they finish on
    // their own and no new ones arrive once the host stops
    this.queued = 0;
  }
}

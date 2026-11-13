// Audio engine. A Channel is one speaker: its own AudioContext (so each can be
// routed to a different output device) with volume, nudge and left/right mode.
// A Player plays one decoded track across one or more channels, starting them at
// the same moment on the performance.now() timeline.
import { planStart, wrapPosition, positionDiff } from './sync.js';

export const hasContextSink = () =>
  typeof AudioContext !== 'undefined' && 'setSinkId' in AudioContext.prototype;

/** One second of silence with a short click at the start, for tuning speakers by ear. */
export function clickBuffer(ctx) {
  const sr = ctx.sampleRate;
  const buf = ctx.createBuffer(2, sr, sr);
  for (let ch = 0; ch < 2; ch++) {
    const data = buf.getChannelData(ch);
    for (let i = 0; i < sr * 0.03; i++) {
      const t = i / sr;
      data[i] = Math.sin(2 * Math.PI * 1500 * t) * Math.exp(-t * 180) * 0.8;
    }
  }
  return buf;
}

export class Channel {
  constructor(name) {
    this.name = name;
    this.ctx = new AudioContext({ latencyHint: 'interactive' });
    this.gain = this.ctx.createGain();
    this.gain.connect(this.ctx.destination);
    this.meter = this.ctx.createAnalyser(); // taps what this speaker is playing, for the level meter
    this.meter.fftSize = 1024;
    this.gain.connect(this.meter);
    this.meterBuf = new Float32Array(this.meter.fftSize);
    this.mode = 'both'; // 'both' | 'left' | 'right'
    this.nudgeMs = 0; // positive = plays later
    this.src = null;
    this.loop = false;
    this.duration = 0;
    this.startPos = 0;
    this.startCtx = 0;
  }

  async setSink(deviceId) {
    if (!this.ctx.setSinkId) throw new Error('This browser cannot choose an output for Web Audio');
    await this.ctx.setSinkId(deviceId || '');
  }

  setVolume(v) {
    this.gain.gain.value = v;
  }

  resume() {
    return this.ctx.state === 'running' ? Promise.resolve() : this.ctx.resume();
  }

  /** Context time at which the given performance.now() moment is heard. */
  ctxTimeAt(perfMs) {
    const ts = this.ctx.getOutputTimestamp?.();
    if (ts && ts.performanceTime > 0) return ts.contextTime + (perfMs - ts.performanceTime) / 1000;
    return this.ctx.currentTime + (perfMs - performance.now()) / 1000;
  }

  /** Start `buffer` so that `position` is heard at atPerfMs (plus this speaker's nudge). */
  start(buffer, loop, position, atPerfMs) {
    this.stop();
    const plan = planStart(atPerfMs + this.nudgeMs, performance.now(), position);
    let pos = plan.position;
    if (!loop && pos >= buffer.duration) return;
    pos = wrapPosition(pos, buffer.duration, loop);

    let when = this.ctxTimeAt(plan.whenPerf);
    const now = this.ctx.currentTime;
    if (when < now) {
      pos = wrapPosition(pos + (now - when), buffer.duration, loop);
      when = now;
    }

    const src = this.ctx.createBufferSource();
    src.buffer = buffer;
    src.loop = loop;
    if (this.mode !== 'both' && buffer.numberOfChannels > 1) {
      const split = this.ctx.createChannelSplitter(2);
      src.connect(split);
      split.connect(this.gain, this.mode === 'left' ? 0 : 1); // one channel out, played on both sides
    } else {
      src.connect(this.gain);
    }
    src.start(when, pos);
    this.src = src;
    this.loop = loop;
    this.duration = buffer.duration;
    this.startPos = pos;
    this.startCtx = when;
  }

  stop() {
    if (this.src) {
      try {
        this.src.stop();
      } catch {
        /* already stopped */
      }
      this.src.disconnect();
      this.src = null;
    }
  }

  /** The track position this speaker is playing right now, or null when idle. */
  heardPosition() {
    if (!this.src) return null;
    const ts = this.ctx.getOutputTimestamp?.();
    const ctxNow = ts && ts.performanceTime > 0 ? ts.contextTime : this.ctx.currentTime;
    const elapsed = ctxNow - this.startCtx;
    if (elapsed < 0) return this.startPos;
    return wrapPosition(this.startPos + elapsed, this.duration, this.loop);
  }

  /** Loudness of what this speaker is playing right now, 0 (silent) to about 1. */
  level() {
    this.meter.getFloatTimeDomainData(this.meterBuf);
    let sum = 0;
    for (const v of this.meterBuf) sum += v * v;
    return Math.min(1, Math.sqrt(sum / this.meterBuf.length) * 3);
  }

  /** A 350 ms beep so you can tell which speaker is which. */
  tone() {
    const osc = this.ctx.createOscillator();
    const env = this.ctx.createGain();
    const t = this.ctx.currentTime;
    osc.frequency.value = 440;
    env.gain.setValueAtTime(0, t);
    env.gain.linearRampToValueAtTime(0.5, t + 0.02);
    env.gain.linearRampToValueAtTime(0, t + 0.35);
    osc.connect(env).connect(this.gain);
    osc.start(t);
    osc.stop(t + 0.4);
  }
}

export class Player {
  constructor(channels) {
    this.channels = channels;
    this.buffer = null;
    this.loop = false;
    this.anchor = null; // { pos, perf }: the track was at `pos` at performance time `perf`
    this.parked = 0; // position while stopped
  }

  get duration() {
    return this.buffer?.duration ?? 0;
  }
  get playing() {
    return this.anchor !== null;
  }

  setTrack(buffer, loop = false) {
    this.stop(0);
    this.buffer = buffer;
    this.loop = loop;
  }

  play(position, atPerfMs) {
    if (!this.buffer) return;
    this.anchor = { pos: position, perf: atPerfMs };
    for (const ch of this.channels) ch.start(this.buffer, this.loop, position, atPerfMs);
  }

  /** Stop all speakers and remember where we were (or `at`, if given). */
  stop(at) {
    const where = at ?? this.position();
    for (const ch of this.channels) ch.stop();
    this.anchor = null;
    this.parked = where;
    return where;
  }

  position() {
    if (!this.anchor) return this.parked;
    const raw = this.anchor.pos + Math.max(0, (performance.now() - this.anchor.perf) / 1000);
    return wrapPosition(raw, this.duration, this.loop);
  }

  hasEnded() {
    return this.playing && !this.loop && this.duration > 0 && this.position() >= this.duration;
  }

  /** Per speaker: how many ms ahead (+) or behind (-) of where it should be. null when idle. */
  driftsMs() {
    const expected = this.position();
    return this.channels.map((ch) => {
      const heard = ch.heardPosition();
      if (heard === null || !this.playing) return null;
      return positionDiff(heard, expected - ch.nudgeMs / 1000, this.duration, this.loop) * 1000;
    });
  }
}

export const canCaptureTab = () => !!navigator.mediaDevices?.getDisplayMedia;

/**
 * Plays another browser tab's sound (YouTube or any site) on every channel at once.
 * Chrome's tab sharing hands us the tab's audio as a live stream and, with
 * suppressLocalAudioPlayback, mutes the tab itself so the sound only comes out
 * of our speakers. Live audio cannot start early, so nudges become delays
 * relative to the earliest speaker.
 */
export class LiveRouter {
  constructor(channels) {
    this.channels = channels;
    this.stream = null;
    this.nodes = [];
    this.onEnded = null;
  }

  get active() {
    return this.stream !== null;
  }

  async start() {
    const stream = await navigator.mediaDevices.getDisplayMedia({
      video: { frameRate: 1 }, // Chrome requires video to share a tab; we never use it
      audio: { suppressLocalAudioPlayback: true, echoCancellation: false, noiseSuppression: false, autoGainControl: false },
      preferCurrentTab: false,
      selfBrowserSurface: 'exclude',
      surfaceSwitching: 'include',
      systemAudio: 'exclude',
    });
    const audio = stream.getAudioTracks()[0];
    if (!audio) {
      stream.getTracks().forEach((t) => t.stop());
      const err = new Error('The shared tab sent no audio');
      err.name = 'NoAudioError';
      throw err;
    }
    this.stop();
    this.stream = stream;
    // The user pressed "Stop sharing" in Chrome's bar, or closed the tab.
    audio.addEventListener('ended', () => {
      this.stop();
      this.onEnded?.();
    });
    this.wire();
  }

  /** (Re)build each speaker's path: tab audio -> optional left/right pick -> delay -> speaker. */
  wire() {
    this.unwire();
    if (!this.stream) return;
    for (const ch of this.channels) {
      const src = ch.ctx.createMediaStreamSource(this.stream);
      const delay = ch.ctx.createDelay(1);
      if (ch.mode !== 'both') {
        const split = ch.ctx.createChannelSplitter(2);
        src.connect(split);
        split.connect(delay, ch.mode === 'left' ? 0 : 1);
      } else {
        src.connect(delay);
      }
      delay.connect(ch.gain);
      this.nodes.push({ ch, src, delay });
    }
    this.applyNudges();
  }

  applyNudges() {
    const base = Math.min(...this.channels.map((c) => c.nudgeMs));
    for (const { ch, delay } of this.nodes) delay.delayTime.value = (ch.nudgeMs - base) / 1000;
  }

  unwire() {
    for (const { src, delay } of this.nodes) {
      src.disconnect();
      delay.disconnect();
    }
    this.nodes = [];
  }

  stop() {
    this.unwire();
    this.stream?.getTracks().forEach((t) => t.stop());
    this.stream = null;
  }
}

// Room Sync client: a WebSocket to the room server plus a clock estimate, so a
// server timestamp can be turned into a moment on this device's performance.now() timeline.
import { estimateOffset } from './sync.js';

const wall = () => performance.timeOrigin + performance.now();

export class RoomClient {
  /** handlers: { onJoined(code, state, members), onState(state), onMembers(n), onError(msg), onClose(), onClock(clock, previous) } */
  constructor(handlers) {
    this.h = handlers;
    this.ws = null;
    this.samples = [];
    this.clock = null; // { offset, rtt, samples }
    this.timer = null;
  }

  connect() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    return new Promise((resolve, reject) => {
      const ws = new WebSocket(`${proto}//${location.host}/ws`);
      this.ws = ws;
      ws.onopen = () => resolve();
      ws.onerror = () => reject(new Error('Could not reach the room server'));
      ws.onclose = () => {
        clearInterval(this.timer);
        this.h.onClose?.();
      };
      ws.onmessage = (ev) => this.#onMessage(JSON.parse(ev.data));
    });
  }

  #onMessage(m) {
    switch (m.t) {
      case 'pong':
      {
        this.samples.push({ t0: m.c0, ts: m.s, t1: wall() });
        if (this.samples.length > 20) this.samples.shift();
        const previous = this.clock;
        this.clock = estimateOffset(this.samples);
        this.h.onClock?.(this.clock, previous);
        break;
      }
      case 'joined':
        this.h.onJoined(m.code, m.state, m.members);
        break;
      case 'state':
        this.h.onState(m.state);
        break;
      case 'members':
        this.h.onMembers(m.n);
        break;
      case 'error':
        this.h.onError(m.message);
        break;
    }
  }

  send(msg) {
    if (this.ws?.readyState === 1) this.ws.send(JSON.stringify(msg));
  }
  create() {
    this.send({ t: 'create' });
  }
  join(code) {
    this.send({ t: 'join', code });
  }
  cmd(cmd) {
    this.send({ t: 'cmd', cmd });
  }

  /**
   * Quick burst of pings, then a few every 10 s so the estimate follows clock drift.
   * Resolves once the first burst is answered, so callers can measure the clock before joining.
   */
  async startClockSync() {
    const burst = async (n) => {
      for (let i = 0; i < n; i++) {
        this.send({ t: 'ping', c0: wall() });
        await new Promise((r) => setTimeout(r, 80));
      }
    };
    await burst(12);
    await new Promise((r) => setTimeout(r, 150)); // let the last replies land
    this.timer = setInterval(() => burst(4), 10_000);
  }

  /** Server timestamp (ms) -> performance.now() timeline on this device. */
  toLocalPerf(serverMs) {
    return serverMs - (this.clock?.offset ?? 0) - performance.timeOrigin;
  }

  close() {
    clearInterval(this.timer);
    this.ws?.close();
  }
}

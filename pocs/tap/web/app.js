import * as P from './protocol.js';

// Both demo phones stand at the Gateway Arch; geohash 7 is all that leaves the phone.
const PLACE = P.geohash(38.6247, -90.1848, 7);

const REASONS = {
  too_far: 'Hold closer: the phones were too far apart.',
  clock_mismatch: "This phone's clock is off. Try again.",
  timing_mismatch: 'The phones measured at different moments. Try again.',
  location_mismatch: 'The phones reported different places.',
  claims_disagree: 'The phones heard different handshakes. Try again.',
  blocked: "You can't tap this person.",
  frozen: 'Tapping is paused on this account.',
  rate_limited: "That's a lot of taps for one hour. Take a breather.",
  nonce_expired: 'That took too long. Try again.',
  self_tap: 'Both phones are signed in to the same account.',
  expired: "The other phone didn't answer in time.",
};

// h builds DOM nodes; text always goes through textContent, never innerHTML.
function h(tag, props = {}, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, '');
    else if (v !== false && v != null) el.setAttribute(k, v);
  }
  for (const kid of kids.flat()) if (kid != null && kid !== false) el.append(kid instanceof Node ? kid : String(kid));
  return el;
}

const logEl = document.getElementById('log');
function log(text, cls = '') {
  logEl.append(h('li', { class: cls }, text));
  logEl.scrollTop = logEl.scrollHeight;
}

class ApiError extends Error {
  constructor(status, code, msg) { super(msg); this.status = status; this.code = code; }
}

class Phone {
  constructor(root, label, defaults) {
    Object.assign(this, { root, label, defaults, tab: 'tap', step: 'start', token: '', user: null });
    this.render();
  }

  get ready() { return Boolean(this.deviceId); }

  async api(method, path, body) {
    const res = await fetch(path, {
      method,
      headers: { 'Content-Type': 'application/json', ...(this.token && { Authorization: `Bearer ${this.token}` }) },
      body: body && JSON.stringify(body),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new ApiError(res.status, data.code || 'error', data.error || res.statusText);
    return data;
  }

  async onboard(name, phone) {
    const { dev_code: code } = await this.api('POST', '/v1/auth/otp/start', { phone });
    const { token, user } = await this.api('POST', '/v1/auth/otp/verify', { phone, code, display_name: name });
    this.token = token;
    this.user = user;
    this.key = await P.generateDeviceKey();
    const { device_id } = await this.api('POST', '/v1/devices', { public_key: this.key.publicKey, attestation: 'dev:web-demo' });
    this.deviceId = device_id;
  }

  async startTap() {
    const { nonce } = await this.api('POST', '/v1/tap/nonce', { device_id: this.deviceId });
    return { nonce, fetchedAt: performance.now(), eph: await P.generateEphemeral() };
  }

  async claim(mine, peer, m) {
    const claim = {
      device_id: this.deviceId, nonce: mine.nonce, peer_nonce: peer.nonce,
      eph_pub: mine.eph, peer_eph_pub: peer.eph, distance_cm: m.distance,
      elapsed_ms: Math.round(performance.now() - mine.fetchedAt), geohash: PLACE, transport: m.transport,
    };
    return { claim, signature: await P.sign(this.key.privateKey, P.canonical(claim)) };
  }

  buzz() {
    if (!this.root) return;
    this.root.classList.remove('buzz');
    void this.root.offsetWidth;
    this.root.classList.add('buzz');
    navigator.vibrate?.([60, 80, 60]); // the two-pulse signature, where supported
  }

  // ---- rendering ----

  render() {
    if (!this.root) return;
    const screen = h('div', { class: 'screen' });
    this.root.replaceChildren(
      h('div', { class: 'status' }, h('span', {}, this.label), h('span', {}, this.user ? this.user.display_name : 'not signed in')),
      screen,
    );
    if (!this.ready) return this.renderOnboarding(screen);
    const views = { tap: this.renderTap, people: this.renderPeople, events: this.renderEvents, props: this.renderProps, you: this.renderYou };
    Promise.resolve(views[this.tab].call(this, screen)).catch((e) => screen.append(h('p', { class: 'empty' }, e.message)));
    const tabs = h('nav', { class: 'tabs', role: 'tablist' });
    for (const [id, name] of [['tap', 'Tap'], ['people', 'People'], ['events', 'Events'], ['props', 'Props'], ['you', 'You']]) {
      tabs.append(h('button', { role: 'tab', 'aria-selected': String(this.tab === id), onclick: () => { this.tab = id; this.render(); } }, name));
    }
    this.root.append(tabs);
  }

  renderOnboarding(screen) {
    const name = h('input', { value: this.defaults.name, maxlength: '40', 'aria-label': 'Display name' });
    const phone = h('input', { value: this.defaults.phone, inputmode: 'tel', 'aria-label': 'Phone number' });
    const err = h('p', { class: 'devcode' });
    const go = h('button', {
      class: 'go',
      onclick: async () => {
        go.disabled = true;
        try {
          await this.onboard(name.value.trim(), phone.value.trim());
          log(`${this.user.display_name} signed in and registered a device key`, 'ok');
          this.render();
          updateBridge();
        } catch (e) {
          err.textContent = e.message;
          go.disabled = false;
        }
      },
    }, 'Continue');
    screen.append(
      h('h2', {}, 'Tap'),
      h('p', {}, "You can't follow anyone. You have to meet them."),
      h('label', { class: 'field' }, 'Display name', name),
      h('label', { class: 'field' }, 'Phone number', phone),
      go,
      h('p', { class: 'devcode' }, 'Dev mode: the SMS code is filled in for you, and a device key is created in this browser.'),
      err,
    );
  }

  renderTap(screen) {
    const r = this.result;
    if (!r) {
      screen.append(
        h('div', { class: 'ring-wrap' }, h('div', { class: 'ring' }, 'Hold phones together')),
        h('p', { class: 'center' }, 'Your first tap is waiting.'),
      );
      return;
    }
    if (r.status !== 'matched') {
      screen.append(
        h('div', { class: 'ring-wrap' }, h('div', { class: 'ring fail' }, 'No tap')),
        h('p', { class: 'center' }, REASONS[r.code] || r.message || 'Try again.'),
      );
      return;
    }
    const note = h('input', { placeholder: 'Private note: where, what about…', maxlength: '500', 'aria-label': 'Private note' });
    const saved = h('p', { class: 'center devcode' });
    screen.append(
      h('div', { class: 'ring-wrap' }, h('div', { class: 'ring ok' },
        h('div', {}, h('div', { class: 'avatar' }, r.peer.display_name.slice(0, 1).toUpperCase()), `You met ${r.peer.display_name}`))),
      h('p', { class: 'center' }, r.tap_count > 1 ? `Tap #${r.tap_count} together` : 'First tap. Verified in person.'),
      h('div', { class: 'note' }, note, h('button', {
        onclick: async () => {
          await this.api('PUT', `/v1/people/${r.peer.user_id}/note`, { text: note.value });
          saved.textContent = 'Saved. Only you can see this.';
        },
      }, 'Save')),
      saved,
    );
  }

  async renderPeople(screen) {
    const { groups, total } = await this.api('GET', '/v1/people');
    if (!total) return screen.append(h('p', { class: 'empty' }, 'Your first tap is waiting.'));
    screen.append(h('h2', {}, `${total} ${total === 1 ? 'person' : 'people'}`));
    for (const g of groups) {
      const box = h('div', { class: 'group' }, h('h3', {}, g.title));
      for (const p of g.people) {
        const met = new Date(p.met_at).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
        box.append(h('div', { class: 'row' },
          h('div', { class: 'name' }, p.display_name),
          h('div', { class: 'meta' }, `Met ${met} · ${p.tap_count} ${p.tap_count === 1 ? 'tap' : 'taps'}${p.place ? ` · near ${p.place}` : ''}`),
          p.note && h('div', { class: 'meta' }, `“${p.note}”`),
          h('div', { class: 'actions' },
            h('button', { class: 'link', onclick: () => this.act('DELETE', `/v1/people/${p.user_id}`, null, `${this.user.display_name} silently removed ${p.display_name}`) }, 'Remove'),
            h('button', { class: 'link', onclick: () => this.act('POST', '/v1/blocks', { user_id: p.user_id }, `${this.user.display_name} blocked ${p.display_name}`) }, 'Block')),
        ));
      }
      screen.append(box);
    }
  }

  async act(method, path, body, message) {
    await this.api(method, path, body);
    log(message);
    renderAll();
  }

  async renderEvents(screen) {
    const { events } = await this.api('GET', '/v1/events');
    if (!events.length) return screen.append(h('p', { class: 'empty' }, 'Five taps in one place within an hour become an event.'));
    for (const ev of events) {
      const box = h('div', { class: 'row' },
        h('div', { class: 'name' }, ev.name),
        h('div', { class: 'meta' }, `You met ${ev.people_met} · ${ev.tap_count} taps there`));
      if (!ev.named) {
        const input = h('input', { placeholder: 'Name this event', maxlength: '60', 'aria-label': 'Event name' });
        box.append(h('div', { class: 'note' }, input, h('button', {
          onclick: () => this.act('PUT', `/v1/events/${ev.id}`, { name: input.value }, `${this.user.display_name} named the event “${input.value}”`),
        }, 'Name')));
      }
      box.append(h('img', { class: 'recap', alt: `Recap card: met ${ev.people_met} people at ${ev.name}`, src: `${ev.recap_image}?token=${encodeURIComponent(this.token)}&v=${ev.tap_count}${ev.named ? 'n' : ''}` }));
      screen.append(box);
    }
  }

  async renderProps(screen) {
    const round = await this.api('GET', '/v1/props/round');
    if (!round.unlocked) {
      screen.append(h('p', { class: 'empty' }, `Props unlocks at ${round.unlocks_at} connections. You have ${round.connections}.`));
      return;
    }
    const open = round.polls.filter((p) => !p.answered);
    screen.append(h('h2', {}, 'Props'), h('p', {}, `${round.polls.length - open.length} of ${round.polls.length} answered this week`));
    const poll = open[0];
    if (poll) {
      const answer = async (pick) => {
        await this.api('POST', '/v1/props/vote', { index: poll.index, pick });
        if (pick) log(`${this.user.display_name} sent a Props pick (anonymous)`);
        this.render();
      };
      screen.append(h('div', { class: 'poll' },
        h('div', { class: 'q' }, poll.prompt),
        h('div', { class: 'opts' },
          poll.options.map((o) => h('button', { onclick: () => answer(o.user_id) }, o.display_name)),
          h('button', { onclick: () => answer('') }, 'Skip'))));
    }
    const [{ picks }, card] = await Promise.all([this.api('GET', '/v1/props/inbox'), this.api('GET', '/v1/props/card')]);
    const group = h('div', { class: 'group' }, h('h3', {}, picks.length ? 'Picked for' : 'No picks yet this week'));
    for (const p of picks.slice(0, 6)) {
      group.append(h('div', { class: 'pick' }, h('div', {}, p.prompt), h('div', { class: 'meta' }, [p.hint || 'someone you met', p.picked_on].join(' · '))));
    }
    screen.append(group);
    if (card.picks) {
      const src = `/v1/props/card.svg?token=${encodeURIComponent(this.token)}&v=${card.picks}`;
      screen.append(
        h('img', { class: 'recap', alt: card.headline, src }),
        h('button', { class: 'go', onclick: () => shareCard(src, card.headline) }, 'Share to story'),
        h('p', { class: 'devcode' }, 'The card links to /props: "You can\'t send Props online. Meet them."'),
      );
    }
  }

  async renderYou(screen) {
    const me = await this.api('GET', '/v1/me');
    const out = h('pre', { class: 'devcode', style: 'white-space: pre-wrap' });
    screen.append(
      h('h2', {}, me.user.display_name),
      h('div', { class: 'stat' }, me.tap_count), h('p', {}, `taps · ${me.connections} connections`),
      h('button', { class: 'link', onclick: async () => { out.textContent = JSON.stringify(await this.api('GET', '/v1/me/export'), null, 2).slice(0, 1200); } }, 'Export my data'),
      ' ',
      h('button', {
        class: 'link',
        onclick: async () => {
          if (!confirm(`Delete ${me.user.display_name}'s account and all their connections?`)) return;
          await this.api('DELETE', '/v1/me');
          log(`${me.user.display_name} deleted their account`);
          Object.assign(this, { token: '', user: null, deviceId: null, result: null, tab: 'tap' });
          renderAll();
          updateBridge();
        },
      }, 'Delete account'),
      out,
    );
  }
}

const a = new Phone(document.getElementById('phone-a'), 'Phone 1', { name: 'Rohith', phone: '+13145550101' });
const b = new Phone(document.getElementById('phone-b'), 'Phone 2', { name: 'Maya', phone: '+13145550102' });

const distance = document.getElementById('distance');
const transport = document.getElementById('transport');
const tapBtn = document.getElementById('tap-btn');
const meetupBtn = document.getElementById('meetup-btn');

function describeDistance() {
  const cm = Number(distance.value);
  const label = cm <= 15 ? 'held back to back' : cm <= 50 ? 'close, not touching' : 'different rooms (relay attack)';
  document.getElementById('distance-label').textContent = `${cm} cm · ${label}`;
}
distance.addEventListener('input', describeDistance);
describeDistance();

function updateBridge() {
  tapBtn.disabled = !(a.ready && b.ready);
  meetupBtn.disabled = !a.ready;
  propsBtn.disabled = attendees.length === 0;
}

// shareCard rasterises the SVG card to PNG (Instagram and Snapchat take
// images, not SVG) and opens the system share sheet, or downloads it.
async function shareCard(src, headline) {
  const img = new Image();
  img.src = src;
  await img.decode();
  const canvas = Object.assign(document.createElement('canvas'), { width: 1080, height: 1920 });
  canvas.getContext('2d').drawImage(img, 0, 0);
  const blob = await new Promise((r) => canvas.toBlob(r, 'image/png'));
  const file = new File([blob], 'tap-props.png', { type: 'image/png' });
  if (navigator.canShare?.({ files: [file] })) {
    await navigator.share({ files: [file], text: headline, url: `${location.origin}/props` });
  } else {
    const link = Object.assign(document.createElement('a'), { href: URL.createObjectURL(blob), download: 'tap-props.png' });
    link.click();
    log('Saved tap-props.png (no share sheet in this browser)');
  }
}

function renderAll() { a.render(); b.render(); }

// handshake runs both halves of a tap, as the two apps would.
async function handshake(x, y, m, { verbose = true } = {}) {
  const say = verbose ? log : () => {};
  const [sx, sy] = await Promise.all([x.startTap(), y.startTap()]);
  say(`Both phones fetched single-use nonces (valid 20 s)`);
  say(`Swapped nonces + ephemeral keys over the radio: ${sx.eph.slice(0, 8)}… ↔ ${sy.eph.slice(0, 8)}…`);
  const [cx, cy] = await Promise.all([x.claim(sx, sy, m), y.claim(sy, sx, m)]);
  say(`Each phone signed a claim with its device key (${m.distance} cm, ${m.transport}, geohash ${PLACE})`);
  const settle = (p) => p.then((r) => r, (e) => ({ status: 'error', code: e.code, message: e.message }));
  const [rx, ry] = await Promise.all([
    settle(x.api('POST', '/v1/tap/claim', cx)),
    settle(y.api('POST', '/v1/tap/claim', cy)),
  ]);
  return [rx, ry];
}

tapBtn.addEventListener('click', async () => {
  tapBtn.disabled = true;
  logEl.replaceChildren();
  const m = { distance: Number(distance.value), transport: transport.value };
  try {
    const [ra, rb] = await handshake(a, b, m);
    a.result = ra;
    b.result = rb;
    a.tab = b.tab = 'tap';
    if (ra.status === 'matched') {
      log(`Server matched both claims → tap ${ra.receipt.tap_id}, receipt signed`, 'ok');
      a.buzz();
      b.buzz();
    } else {
      for (const [who, r] of [[a, ra], [b, rb]]) log(`${who.label}: ${r.code || r.status}: ${r.message || REASONS[r.status] || ''}`, 'bad');
      if (ra.status === 'expired') ra.code = 'expired';
      if (rb.status === 'expired') rb.code = 'expired';
    }
    renderAll();
  } finally {
    updateBridge();
  }
});

const attendees = [];
const propsBtn = document.getElementById('props-btn');

meetupBtn.addEventListener('click', async () => {
  meetupBtn.disabled = true;
  const names = ['Priya', 'Marcus', 'Lena', 'Diego', 'Sam'];
  const close = { distance: 5, transport: 'ble_ultrasonic' };
  const hosts = [a, b].filter((p) => p.ready);
  log(`Meetup: ${names.length} simulated attendees tap ${hosts.map((p) => p.user.display_name).join(' and ')}, and each other`);
  try {
    for (const name of names) {
      const guest = new Phone(null, name, {});
      await guest.onboard(name, `+1314${String(Math.floor(Math.random() * 1e7)).padStart(7, '0')}`);
      for (const host of hosts) {
        const [r] = await handshake(host, guest, close, { verbose: false });
        if (r.status !== 'matched') log(`${name} ↔ ${host.user.display_name}: ${r.code}`, 'bad');
      }
      for (const other of attendees) await handshake(other, guest, close, { verbose: false });
      attendees.push(guest);
      log(`${name} tapped ${hosts.length} phone${hosts.length > 1 ? 's' : ''} and ${attendees.length - 1} other attendees`, 'ok');
    }
    // Events are assigned just after taps commit; give the worker a moment.
    for (let i = 0; i < 20; i++) {
      const { events } = await a.api('GET', '/v1/events');
      if (events.length) { log(`Event formed: ${events[0].name} · ${events[0].tap_count} taps`, 'ok'); break; }
      await new Promise((r) => setTimeout(r, 150));
    }
    a.tab = 'events';
    renderAll();
  } finally {
    updateBridge();
  }
});

propsBtn.addEventListener('click', async () => {
  propsBtn.disabled = true;
  try {
    let picks = 0;
    for (const guest of attendees) {
      const round = await guest.api('GET', '/v1/props/round');
      if (!round.unlocked) continue;
      for (const poll of round.polls.filter((p) => !p.answered)) {
        const o = poll.options[Math.floor(Math.random() * poll.options.length)];
        await guest.api('POST', '/v1/props/vote', { index: poll.index, pick: o.user_id });
        picks++;
      }
    }
    log(`${attendees.length} attendees answered their polls: ${picks} anonymous picks`, 'ok');
    a.tab = b.tab = 'props';
    renderAll();
  } finally {
    updateBridge();
  }
});

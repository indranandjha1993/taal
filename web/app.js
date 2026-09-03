import { Clock, LivePlayer } from './sync.js';

const el = (id) => document.getElementById(id);
const NUDGE_KEY = 'taal.nudge';
const NAME_KEY = 'taal.name';

let ws, clock, player;
let joined = false;

function connect() {
  ws = new WebSocket(`ws://${location.host}/ws`);
  ws.binaryType = 'arraybuffer';

  clock = new Clock(ws);
  player = new LivePlayer(clock, onTick);
  player.setNudge(Number(localStorage.getItem(NUDGE_KEY) || 0));

  ws.onopen = () => {
    ws.send(JSON.stringify({
      type: 'hello',
      role: 'guest',
      name: localStorage.getItem(NAME_KEY) || '',
    }));
    clock.start();
    setState('connected');
  };

  ws.onclose = () => {
    setState('disconnected, retrying');
    clock.stop();
    player.stop();
    setTimeout(connect, 2000);
  };

  ws.onmessage = (ev) => {
    if (typeof ev.data === 'string') {
      handle(JSON.parse(ev.data));
    } else if (joined) {
      player.push(ev.data);
    }
  };
}

function handle(msg) {
  switch (msg.type) {
    case 'sync':
      clock.accept(msg.t0, msg.ts);
      el('offset').textContent = `${clock.drift.toFixed(1)} ms`;
      el('rtt').textContent = `${clock.rtt.toFixed(1)} ms`;
      break;

    case 'state':
      player.configure(msg.rate, msg.channels);
      el('source').textContent = msg.streaming
        ? msg.source
        : 'host is not streaming';
      if (joined) setState(msg.streaming ? 'playing' : 'waiting for the host');
      break;

    case 'gain':
      // the host moved this speaker on the mixer
      player.setVolume(msg.gain);
      el('vol').value = Math.round(msg.gain * 100);
      el('volVal').textContent = `${Math.round(msg.gain * 100)}%`;
      break;
  }
}

function onTick({ leadMs, late }) {
  const d = el('lead');
  d.textContent = `${leadMs.toFixed(0)} ms`;
  // a shrinking lead means this device is falling behind the stream and
  // will start dropping chunks
  d.style.color = leadMs > 40 ? '' : 'var(--accent)';
  el('late').textContent = late;
}

function setState(text) {
  el('state').textContent = text;
}

// screen lock suspends the tab and audio dies with it
async function keepAwake() {
  try {
    await navigator.wakeLock.request('screen');
  } catch (e) {
    // not supported or denied, nothing to do about it
  }
}

el('join').addEventListener('click', async () => {
  const name = el('name').value.trim();
  if (name) {
    localStorage.setItem(NAME_KEY, name);
    ws.send(JSON.stringify({ type: 'hello', role: 'guest', name }));
  }
  await player.unlock();
  await keepAwake();
  joined = true;
  el('setup').hidden = true;
  el('panel').hidden = false;
  setState('waiting for the host');
});

el('nudge').addEventListener('input', (e) => {
  const ms = Number(e.target.value);
  el('nudgeVal').textContent = `${ms} ms`;
  player.setNudge(ms);
  localStorage.setItem(NUDGE_KEY, ms);
});

el('vol').addEventListener('input', (e) => {
  const pct = Number(e.target.value);
  el('volVal').textContent = `${pct}%`;
  player.setVolume(pct / 100);
});

document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible' && joined) keepAwake();
});

const savedNudge = Number(localStorage.getItem(NUDGE_KEY) || 0);
if (savedNudge) {
  el('nudge').value = savedNudge;
  el('nudgeVal').textContent = `${savedNudge} ms`;
}
el('name').value = localStorage.getItem(NAME_KEY) || '';

connect();

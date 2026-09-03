const el = (id) => document.getElementById(id);

let ws;
let streaming = false;
let dragging = null;

el('url').textContent = location.host;

// The server holds the truth about whether audio is flowing, so reopening
// this page reads it back rather than assuming a fresh start. Closing the
// tab never stops the stream.
function connect() {
  // a secure page cannot open an insecure socket
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  ws = new WebSocket(`${proto}://${location.host}/ws`);

  ws.onopen = () => {
    setConn('connected', true);
    ws.send(JSON.stringify({ type: 'hello', role: 'host' }));
    checkSetup();
  };

  ws.onclose = () => {
    setConn('lost the server, retrying', false);
    setTimeout(connect, 2000);
  };

  ws.onmessage = (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.type === 'roster') renderSpeakers(msg.speakers);
    if (msg.type === 'state') applyState(msg);
    if (msg.type === 'error') showError(msg.msg);
  };
}

function setConn(text, ok) {
  const c = el('conn');
  c.textContent = text;
  c.className = ok ? 'sub' : 'sub warn';
}

function applyState(msg) {
  streaming = msg.streaming;
  el('dot').className = streaming ? 'dot on' : 'dot off';
  el('liveText').textContent = streaming
    ? 'live, anything this mac plays goes to the speakers below'
    : 'stopped, nothing is being sent';
  el('power').textContent = streaming ? 'stop streaming' : 'start streaming';
  el('audible').checked = msg.audible;
  el('delay').value = msg.delayMs;
  el('delayVal').textContent = `${msg.delayMs} ms`;
}

async function checkSetup() {
  const s = await (await fetch('/setup')).json();
  el('setup').hidden = s.ready;
  el('ready').hidden = !s.ready;
  if (s.ready) return;

  el('setupMsg').textContent = s.detail;
  el('install').hidden = false;
  el('setupCmd').hidden = true;
  el('setupCmd').textContent = s.command;
}

function showError(text) {
  el('liveText').textContent = text;
  el('dot').className = 'dot off';
}

function renderSpeakers(speakers) {
  el('count').textContent = speakers.length
    ? `${speakers.length} connected`
    : 'none yet';

  const box = el('speakers');
  if (!speakers.length) {
    box.innerHTML = '<p class="empty">scan the code on a phone to add one</p>';
    return;
  }

  const seen = new Set();
  for (const sp of speakers) {
    seen.add(sp.id);
    let row = box.querySelector(`[data-id="${sp.id}"]`);
    if (!row) {
      row = document.createElement('div');
      row.className = 'speaker';
      row.dataset.id = sp.id;
      row.innerHTML = `
        <span class="speaker-name"></span>
        <input type="range" min="0" max="100" step="1">
        <span class="speaker-gain"></span>
        <button class="mute ghost-sm">mute</button>`;
      box.appendChild(row);

      const slider = row.querySelector('input');
      slider.addEventListener('input', () => {
        dragging = sp.id;
        setGain(sp.id, slider.value / 100);
      });
      slider.addEventListener('change', () => { dragging = null; });
      row.querySelector('.mute').addEventListener('click', () => {
        const cur = Number(row.querySelector('input').value);
        setGain(sp.id, cur > 0 ? 0 : 1);
      });
    }

    row.querySelector('.speaker-name').textContent = sp.name;
    // leave the slider alone while it is being dragged, the echo of our
    // own change would fight the finger
    if (dragging !== sp.id) {
      row.querySelector('input').value = Math.round(sp.gain * 100);
    }
    row.querySelector('.speaker-gain').textContent = `${Math.round(sp.gain * 100)}%`;
    row.querySelector('.mute').textContent = sp.gain > 0 ? 'mute' : 'unmute';
  }

  for (const row of [...box.querySelectorAll('.speaker')]) {
    if (!seen.has(row.dataset.id)) row.remove();
  }
  const empty = box.querySelector('.empty');
  if (empty) empty.remove();
}

function setGain(id, gain) {
  ws.send(JSON.stringify({ type: 'gain', id, gain }));
}

el('power').addEventListener('click', () => {
  if (streaming) {
    ws.send(JSON.stringify({ type: 'stop' }));
  } else {
    ws.send(JSON.stringify({ type: 'start', audible: el('audible').checked }));
  }
});

el('install').addEventListener('click', async () => {
  const res = await fetch('/setup', { method: 'POST' });
  const out = await res.json();
  el('setupMsg').textContent = out.detail;
  el('setupCmd').hidden = false;
  el('setupCmd').textContent = out.command;
  el('install').textContent = 'check again';
  el('install').onclick = checkSetup;
});

el('audible').addEventListener('change', (e) => {
  if (!streaming) return;
  // it only takes effect when the routing is rebuilt, so do that now
  // rather than leaving a setting that silently does nothing
  ws.send(JSON.stringify({ type: 'stop' }));
  setTimeout(() => {
    ws.send(JSON.stringify({ type: 'start', audible: e.target.checked }));
  }, 400);
});

el('delay').addEventListener('input', (e) => {
  el('delayVal').textContent = `${e.target.value} ms`;
});

el('delay').addEventListener('change', (e) => {
  ws.send(JSON.stringify({ type: 'delay', ms: Number(e.target.value) }));
});

connect();

const el = (id) => document.getElementById(id);

let ws;
let dragging = null;

el('url').textContent = location.host;

function connect() {
  // a secure page cannot open an insecure socket
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  ws = new WebSocket(`${proto}://${location.host}/ws`);

  ws.onopen = () => ws.send(JSON.stringify({ type: 'hello', role: 'host' }));
  ws.onclose = () => setTimeout(connect, 2000);

  ws.onmessage = (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.type === 'roster') renderMixer(msg.speakers);
    if (msg.type === 'state') {
      el('start').disabled = msg.streaming;
      el('stop').disabled = !msg.streaming;
      if (msg.streaming) el('source').value = msg.source;
      el('delay').value = msg.delayMs;
      el('delayVal').textContent = `${msg.delayMs} ms`;
    }
    if (msg.type === 'error') el('hint').textContent = msg.msg;
  };
}

function renderMixer(speakers) {
  el('count').textContent =
    `${speakers.length} speaker${speakers.length === 1 ? '' : 's'}`;

  const box = el('speakers');
  if (!speakers.length) {
    box.innerHTML = '<p class="empty">nobody has joined yet</p>';
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

async function loadSources() {
  const devs = await (await fetch('/sources')).json();
  const sel = el('source');
  sel.innerHTML = '';
  for (const d of devs) {
    const opt = document.createElement('option');
    opt.value = d.name;
    opt.textContent = d.loopback ? `${d.name} (system audio)` : d.name;
    sel.appendChild(opt);
  }
  // a microphone would capture the room instead of the music, so start on
  // a loopback device if one exists
  const loop = devs.find((d) => d.loopback);
  if (loop) {
    sel.value = loop.name;
    el('hint').textContent = 'captures whatever this mac plays';
  } else {
    el('hint').textContent =
      'no loopback device found. install blackhole to capture system audio, '
      + 'otherwise you are about to stream a microphone.';
  }
}

el('start').addEventListener('click', () => {
  ws.send(JSON.stringify({ type: 'start', source: el('source').value }));
});

el('stop').addEventListener('click', () => {
  ws.send(JSON.stringify({ type: 'stop' }));
});

el('delay').addEventListener('input', (e) => {
  el('delayVal').textContent = `${e.target.value} ms`;
});

el('delay').addEventListener('change', (e) => {
  ws.send(JSON.stringify({ type: 'delay', ms: Number(e.target.value) }));
});

loadSources();

connect();

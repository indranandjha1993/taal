const el = (id) => document.getElementById(id);

// Asking "host or speaker?" every time is a question this page can answer
// for itself. The mac running taal is the host and cannot usefully be a
// speaker as well, because it would play the stream back into the device
// taal is capturing. Every other device can only be a speaker.
async function decide() {
  let me;
  try {
    me = await (await fetch('/whoami')).json();
  } catch (e) {
    me = { host: false, streaming: false, speakers: 0 };
  }

  const joined = localStorage.getItem('taal.joined') === '1';

  if (me.host) {
    el('sub').textContent = 'this is the mac taal runs on';
    show([
      card('/host', 'open the controls', accent(),
        me.streaming
          ? `streaming to ${count(me.speakers)}`
          : 'pick what to play and start streaming'),
    ]);
    el('tip').textContent = 'this mac is the source. playing the stream back '
      + 'here as well would feed taal its own output, so it hosts only.';
    return;
  }

  el('sub').textContent = me.streaming
    ? 'the mac is streaming now'
    : 'the mac is not streaming yet';

  show([
    card('/join', joined ? 'back to the room' : 'join as a speaker', accent(),
      joined ? 'you joined from this device before'
             : 'this device becomes one of the speakers'),
  ]);
}

function accent() { return 'card accent'; }

function count(n) {
  if (!n) return 'no speakers yet';
  return n === 1 ? '1 speaker' : `${n} speakers`;
}

function card(href, title, cls, note) {
  const a = document.createElement('a');
  a.className = cls;
  a.href = href;
  const t = document.createElement('span');
  t.className = 'card-title';
  t.textContent = title;
  const n = document.createElement('span');
  n.className = 'card-note';
  n.textContent = note;
  a.append(t, n);
  return a;
}

function show(cards) {
  const box = el('choice');
  box.innerHTML = '';
  for (const c of cards) box.appendChild(c);
}

decide();

# taal

Whatever your mac is playing comes out of every phone and laptop in the room,
in sync. Run a small binary on the mac, everyone else opens a browser. No app
to install, and the source can be anything: a music app, a browser tab, a local
file.

Taal means rhythm. Keeping devices in time is the whole problem.

## How it works

The mac captures its own audio output and streams it over the local network as
plain PCM. Every chunk carries the host time at which its first sample should
be heard. A speaker measures its offset from the host clock, converts that
stamp into its own audio clock, and schedules the chunk there. Nothing is ever
played on arrival, which is what keeps devices together.

Two pieces make the sync work:

**Clock agreement.** Every speaker runs an NTP style exchange with the host over
the WebSocket: send a local timestamp, get the host time back, halve the round
trip. It sends a burst and keeps the least delayed sample, since a slow reply is
a lie about the offset. On a LAN this lands within a few milliseconds.

**Scheduled chunks.** Each 20ms chunk is stamped with the host time its first
sample is heard, and every speaker schedules it against its own audio clock
minus its own output latency. A chunk that arrives past its due time is
dropped rather than played late, since playing it would put that speaker out
of step with the rest of the room.

## First run on a new mac

```
brew install --cask blackhole-2ch
make start
```

BlackHole is a small audio driver. macos gives no app direct access to what
the speakers are playing, so without it nothing can capture your audio, taal
included. It is open source, it is the same driver most screen recorders use,
and installing it asks for your admin password because it is a system driver.

Skipping this step is fine: taal starts anyway and the page explains what is
missing rather than showing controls that cannot work. Install the driver,
press check again, and it carries on.

You will also meet a certificate warning the first time each device opens
taal. That is expected, see the https section below.

## Running it

```
make          what the other commands are
make run      run it here, ctrl-c to stop
make start    run it in the background
make status   is it up, and on which address
make logs     follow the log
make stop     stop it and put the audio back
```

`make start` prints the two addresses and keeps the server alive after you
close the terminal. Starting twice does not launch a second one, and a start
on an occupied port fails loudly rather than pretending it worked.

It serves https with a self signed certificate it generates on first run and
keeps in your config directory. That is not decoration: the screen wake lock
and the media session only exist in a secure context, and a plain lan address
is not one, so over http a phone cannot keep itself awake. The tradeoff is
that each phone shows a certificate warning the first time, which you accept
once. Pass `-http` to serve plain http instead and give that up.

There is one address and no other paths. Open it on the mac and you get the
controls; open it anywhere else and you get the speaker page. Nothing asks
which one you want, because the answer is already known: the mac running taal
is the source, every other device is a speaker.

Streaming starts the moment the server does, so there is nothing to press to
begin. The controls carry a live indicator, a stop button, the QR code that
adds speakers, the mixer, and a settings section for the buffer and whether
the mac keeps playing out loud.

The mac cannot join as a speaker. It would play the stream back into the very
device taal is capturing, so the audio would echo itself once per buffer and
build up.

Press start and play something. That is the whole flow.

Behind that button: macos gives no app direct access to system audio, so a
small loopback driver is needed. If it is missing the host page shows only an
explanation and an install button, never a start button that cannot work.
taal then builds its own output device, points the mac at it, and puts
everything back the way it was when you stop. Nobody needs to open Audio MIDI
Setup or learn what a loopback device is.

That device is a singleton with a fixed identifier, so running taal twice or
restarting it a hundred times leaves exactly one, and stopping leaves none.
It outlives the process though, so if taal is killed rather than stopped, the
next start notices, puts the output back and clears the device away.

"keep playing on this mac too" decides whether the mac speakers stay live
while streaming. Leave it on and the mac plays immediately while the phones
sit a buffer behind, so in one room you hear both. Turn it off and only the
phones make sound.

Each speaker names itself when it joins, and that name is what the host sees
in the mixer, so a row can be turned down or muted without guessing whose
phone it is.

```
PORT=9000 make start
```

Works on home Wi-Fi or a phone hotspot. Nothing leaves the local network and
there is no internet dependency.

## The delay, and why it exists

Speakers play a fixed distance behind the mac, 400ms by default. That is the
jitter budget: a chunk has to be captured, sent, decoded and queued before its
play time arrives, and wifi is not punctual. Lower it and the room tightens up,
but chunks start missing their slot and speakers drop audio. The host has a
slider for it, and the speaker page shows both the current lead and how many
chunks it has dropped, so the two numbers together tell you when you have gone
too far.

This delay rules out video. Lip sync tolerance is around 45ms, so watching
something on the mac while phones carry the audio will look wrong no matter how
the buffer is tuned. Music is the use case.

## Getting it actually tight

In one room, the ear starts hearing two devices as an echo somewhere around
20 to 30ms apart. The clock sync gets well inside that. What does not is the
output latency of each device, which varies by hardware and cannot be fully
measured from the browser.

So every speaker has a nudge slider. Slide it until the echo collapses into one
sound. The value is remembered per device, so it is a one time setup per phone.

Two things that will ruin the result no matter how good the sync is:

- **Bluetooth speakers and headphones.** They add 100 to 300ms and it drifts.
  Built in speakers or wired only.
- **A locked screen.** The page takes a wake lock and claims a media session,
  which is enough on android to keep the screen on and the audio running. ios
  is different: safari drops the lock whenever the phone is locked or the tab
  leaves the foreground, and stops the audio with it. No web page can override
  that. The speaker page says which of the two you are getting.

## Measuring it instead of guessing

Ears are bad at telling 15ms from 40ms, they only report "something is off".
To get a number, put both devices in a room playing the same track, record a
few seconds on a third device, and run:

```
python3 tools/offset.py room.wav
```

The recording contains the same music twice, once from each speaker, so the
tool correlates it against itself and reports the gap. Record from a spot
roughly equidistant from both devices, otherwise the distance difference
shows up in the measurement too, at about 3ms per metre. A percussive track
measures far better than a pad or strings.

## Devices

Phones, tablets and laptops work through any modern browser. Fire Stick works
through the Silk browser, though typing the URL with a remote is a chore.
Google TV ships no browser, so it is not supported yet, the likely route being
a Chromecast receiver since that is an HTML page too.

## Layout

```
main.go          startup, LAN address detection
tls.go           self signed certificate, generated once and reused
setup.go         what this mac needs before it can capture
routing_darwin.go  builds the output device so nobody visits Audio MIDI Setup
output_darwin.go   reads and sets the system output
capture.go       coreaudio capture, device listing
stream.go        binary chunk framing
server.go        routes, source listing, QR
session.go       websocket hub, clock replies, stream control
web/sync.js      clock and scheduled player, the interesting part
web/app.js       speaker page
web/host.js      host page and mixer
tools/offset.py  measure the real gap from a room recording
```

The server pings every speaker every five seconds and drops any that stops
answering within twelve. Without that a phone which sleeps, loses wifi or is
swiped away never closes its socket, and the mixer slowly fills with speakers
that already left. Each device also carries its own id, so coming back
reclaims its row and its volume rather than adding a second one.

Tests cover the parts where a mistake is invisible at runtime: the QR pointing
at an unreachable address, a speaker joining mid stream not being told what is
playing, and chunk timestamps drifting or overlapping.

```
go test ./...
```

The web files are embedded in the binary, so a build is a single file with no
assets to ship alongside. There is no frontend build step and no framework.

## Limits

Audio is sent as uncompressed PCM, about 1.5 Mbit/s per speaker. Fine on wifi
for a handful of devices, wasteful for many. Opus would fix that and is the
obvious next step.

The host has to run natively on the mac. It cannot run in docker, because a
container has no access to coreaudio and no loopback device to read.

Anyone on the network can start, stop and remix the stream. That is the
intended tradeoff for a party on a trusted network, but do not run it on
public wifi.

## License

MIT

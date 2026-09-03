// Keeping a phone playing with the screen off is mostly out of a web page's
// hands, so this does the three things that actually help and reports
// honestly when they are not enough.
//
// Wake Lock holds the screen on. Chrome and Android honour it well. Safari
// only got it in 16.4 and drops it the moment the phone is locked or the tab
// leaves the foreground, so on iphone it keeps the screen alive while you are
// looking at the page and nothing more.
//
// A Media Session is the part that helps ios: once a page owns one, the
// system treats it as a media player rather than an idle tab, shows lock
// screen controls, and is far less eager to suspend its audio.

export class Awake {
  constructor(onChange) {
    this.lock = null;
    this.onChange = onChange;
    this.want = false;

    // ios drops the lock on every backgrounding, so take it again as soon
    // as the page is looked at
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible' && this.want) this.acquire();
    });
  }

  get held() {
    return this.lock !== null;
  }

  get supported() {
    return 'wakeLock' in navigator;
  }

  // the api is missing for two very different reasons and the fix differs,
  // so tell them apart: an insecure page can be fixed by using https, an
  // old browser cannot be fixed at all
  get reason() {
    if (this.supported) return '';
    return window.isSecureContext ? 'browser' : 'insecure';
  }

  async acquire() {
    this.want = true;
    if (!this.supported || this.lock) {
      this.report();
      return;
    }
    try {
      this.lock = await navigator.wakeLock.request('screen');
      // released by the system, not by us, so drop the stale handle
      this.lock.addEventListener('release', () => {
        this.lock = null;
        this.report();
      });
    } catch (e) {
      this.lock = null;
    }
    this.report();
  }

  async release() {
    this.want = false;
    if (this.lock) {
      try {
        await this.lock.release();
      } catch (e) {
        // already gone
      }
      this.lock = null;
    }
    this.report();
  }

  report() {
    if (this.onChange) this.onChange(this.status());
  }

  status() {
    if (!this.supported) return this.reason === 'insecure' ? 'insecure' : 'unsupported';
    return this.lock ? 'held' : 'lost';
  }
}

// Claim the media session. The controls are deliberately inert: this is a
// live stream, so there is nothing to pause or seek on this device. What
// matters is owning the session at all, which is what stops ios treating
// the tab as idle.
export function claimMediaSession(name) {
  if (!('mediaSession' in navigator)) return false;

  navigator.mediaSession.metadata = new MediaMetadata({
    title: 'taal',
    artist: name || 'speaker',
    album: 'live from the host',
  });
  navigator.mediaSession.playbackState = 'playing';

  // ios shows these on the lock screen. swallowing them keeps the stream
  // running rather than letting a stray tap tear the session down.
  for (const action of ['play', 'pause', 'stop']) {
    try {
      navigator.mediaSession.setActionHandler(action, () => {
        navigator.mediaSession.playbackState = 'playing';
      });
    } catch (e) {
      // action not supported on this browser
    }
  }
  return true;
}

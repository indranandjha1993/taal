package main

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"sync"
	"testing"
)

// the QR is the only way most guests join, so a wrong URL in it breaks
// the whole app while every endpoint still returns 200
func TestQRTargetsReachableHost(t *testing.T) {
	cases := []struct {
		name     string
		taalHost string
		reqHost  string
		want     string
	}{
		{"env wins over request", "192.168.1.50", "localhost:8225", "192.168.1.50:8225"},
		{"env without port on request", "192.168.1.50", "localhost", "192.168.1.50"},
		{"loopback swapped for lan", "", "127.0.0.1:8225", ""},
		{"real host kept as is", "", "192.168.1.77:8225", "192.168.1.77:8225"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TAAL_HOST", tc.taalHost)

			got := qrTarget(tc.reqHost)
			if tc.want == "" {
				// lan address is machine dependent, just prove it moved off loopback
				if got == tc.reqHost {
					t.Fatalf("loopback host %q was not replaced", tc.reqHost)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHostIsNotCountedAsListener(t *testing.T) {
	s := newServer(&capture{})
	s.hello(&client{send: make(chan []byte, 8)}, false, "", "")

	s.mu.Lock()
	n := s.guestCount()
	s.mu.Unlock()

	if n != 0 {
		t.Fatalf("host alone counted as %d listeners, want 0", n)
	}
}

// counts used to be taken under one lock and sent under another, so
// concurrent joins could report them out of order and the number would
// jump around on every screen
func TestCountsStayOrderedUnderChurn(t *testing.T) {
	s := newServer(&capture{})
	watcher := &client{send: make(chan []byte, 512)}
	s.hello(watcher, false, "", "")

	const n = 40
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := &client{send: make(chan []byte, 8)}
			go func() {
				for range c.send {
				}
			}()
			s.hello(c, true, "", "")
			s.drop(c)
		}()
	}
	wg.Wait()

	s.mu.Lock()
	final := s.guestCount()
	s.mu.Unlock()

	if final != 0 {
		t.Fatalf("after every guest left, count is %d, want 0", final)
	}
}

// a speaker joining mid stream must be told what is going on, otherwise it
// sits silent until the host happens to change something
// A phone that drops off and comes back must reclaim its row rather than
// adding another. This is the three-Oppos bug.
func TestReconnectReplacesInsteadOfDuplicating(t *testing.T) {
	s := newServer(&capture{})

	first := &client{send: make(chan []byte, 8), audio: make(chan []byte, 4)}
	go drain(first)
	s.hello(first, true, "Oppo", "dev-abc")
	s.setGain("dev-abc", 0.4)

	// same device, new socket, same id
	second := &client{send: make(chan []byte, 8), audio: make(chan []byte, 4)}
	go drain(second)
	s.hello(second, true, "Oppo", "dev-abc")

	s.mu.Lock()
	n := s.guestCount()
	gain := second.gain
	s.mu.Unlock()

	if n != 1 {
		t.Fatalf("%d speakers after a reconnect, want 1", n)
	}
	if gain != 0.4 {
		t.Fatalf("volume was %v after reconnect, want it carried across", gain)
	}

	// the stale connection being dropped later must not resurrect a row
	s.drop(first)
	s.mu.Lock()
	n = s.guestCount()
	s.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d speakers after the stale drop, want 1", n)
	}
}

func drain(c *client) {
	for range c.send {
	}
}

func TestJoinerLearnsStreamState(t *testing.T) {
	s := newServer(&capture{})
	s.streaming = true
	s.source = "BlackHole 2ch"
	s.delayMs = defaultDelayMs

	c := &client{send: make(chan []byte, 8), audio: make(chan []byte, 4)}
	s.hello(c, true, "", "")

	var got map[string]any
	for len(c.send) > 0 {
		var msg map[string]any
		if json.Unmarshal(<-c.send, &msg) == nil && msg["type"] == "state" {
			got = msg
		}
	}
	if got == nil {
		t.Fatal("no state message sent to a joining speaker")
	}
	if got["streaming"] != true || got["source"] != "BlackHole 2ch" {
		t.Fatalf("state did not describe the live stream: %v", got)
	}
}

// every chunk carries the host time its first sample is heard. if that is
// not monotonic the speakers cannot schedule anything.
func TestChunkTimestampsAdvance(t *testing.T) {
	s := newServer(&capture{})
	s.streaming = true
	s.delayMs = defaultDelayMs

	c := &client{send: make(chan []byte, 4), audio: make(chan []byte, 16)}
	s.hello(c, true, "", "")

	pcm := make([]byte, framesPerChunk*channels*2)
	var seq int64
	var stamps []float64
	for i := 0; i < 5; i++ {
		s.onChunk(seq, pcm)
		seq += framesPerChunk
	}
	for len(c.audio) > 0 {
		frame := <-c.audio
		if len(frame) < frameHeader {
			t.Fatal("chunk shorter than its header")
		}
		if binary.LittleEndian.Uint32(frame) != frameMagic {
			t.Fatal("chunk missing magic")
		}
		stamps = append(stamps, math.Float64frombits(
			binary.LittleEndian.Uint64(frame[4:])))
	}
	if len(stamps) < 5 {
		t.Fatalf("got %d chunks, want 5", len(stamps))
	}
	step := 1000.0 * framesPerChunk / sampleRate
	for i := 1; i < len(stamps); i++ {
		gap := stamps[i] - stamps[i-1]
		if math.Abs(gap-step) > 0.001 {
			t.Fatalf("chunk %d starts %.3fms after the last, want %.3f", i, gap, step)
		}
	}
	// and the first chunk must land in the future by roughly the buffer
	if lead := stamps[0] - nowMs(); lead < defaultDelayMs-50 {
		t.Fatalf("first chunk only %.0fms ahead, want about %d", lead, defaultDelayMs)
	}
}

// The routing device outlives the process, so a second run, a crash or a
// dozen restarts must never leave a pile of them behind.
//
// This one drives the real audio system, so it is skipped when a taal is
// already running: it would fight the live server for the same device.
func TestRoutingIsSingletonAndCleansUp(t *testing.T) {
	if currentOutputName() == routingName {
		t.Skip("a taal server is streaming, leave its device alone")
	}

	var loop string
	for _, d := range outputDevices() {
		if isLoopback(d.Name) {
			loop = d.Name
			break
		}
	}
	if loop == "" {
		t.Skip("no loopback driver on this machine")
	}

	count := func() int {
		n := 0
		for _, d := range outputDevices() {
			if d.Name == routingName {
				n++
			}
		}
		return n
	}

	orig := currentOutputName()
	defer func() {
		removeRouting()
		if orig != "" {
			setOutput(orig)
		}
	}()

	for i := 0; i < 3; i++ {
		if _, ok := buildRouting(loop, ""); !ok {
			t.Fatalf("build %d failed", i)
		}
		if n := count(); n != 1 {
			t.Fatalf("after build %d: %d routing devices, want 1", i, n)
		}
	}

	removeRouting()
	if n := count(); n != 0 {
		t.Fatalf("after cleanup: %d routing devices, want 0", n)
	}
}

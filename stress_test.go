package main

import (
	"sync"
	"testing"
	"time"
)

// A party is speakers joining, leaving, reconnecting and being remixed all
// at once while audio fans out. If anything races, it shows up here.
func TestPartyChaos(t *testing.T) {
	s := newServer(&capture{})
	s.streaming = true
	s.delayMs = defaultDelayMs

	var wg sync.WaitGroup
	pcm := make([]byte, framesPerChunk*channels*2)

	// audio thread hammering fanout. deliberately not in wg: it is stopped
	// after the workers finish, not before.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var seq int64
		for {
			select {
			case <-stop:
				return
			default:
				s.onChunk(seq, pcm)
				seq += framesPerChunk
				// the real callback fires every 20ms, an unpaced loop
				// just starves the scheduler
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// speakers churning
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c := &client{send: make(chan []byte, 8), audio: make(chan []byte, 8)}
			go func() {
				for range c.send {
				}
			}()
			go func() {
				for range c.audio {
				}
			}()
			id := "dev-" + string(rune('a'+n%8))
			s.hello(c, true, "phone", id)
			s.setGain(id, 0.5)
			s.drop(c)
		}(i)
	}

	wg.Wait()
	close(stop)
	<-done

	s.mu.Lock()
	n := s.guestCount()
	s.mu.Unlock()
	t.Logf("speakers left after the chaos: %d", n)
}

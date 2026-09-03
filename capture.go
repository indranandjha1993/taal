package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gen2brain/malgo"
)

const (
	sampleRate = 48000
	channels   = 2
	// 20ms per chunk. smaller means more websocket frames for no gain,
	// larger adds latency before a chunk can even be sent
	framesPerChunk = sampleRate / 50
)

// how far behind the source every speaker plays. this is the jitter budget:
// a chunk captured now must arrive, be decoded and be queued before its
// play time comes round. too small and wifi hiccups become dropouts.
const defaultDelayMs = 400

type capture struct {
	ctx  *malgo.AllocatedContext
	dev  *malgo.Device
	mu   sync.Mutex
	sink func(seq int64, pcm []byte)
	seq  int64
}

func newCapture() (*capture, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("audio init: %w", err)
	}
	return &capture{ctx: ctx}, nil
}

type device struct {
	Name string
	id   malgo.DeviceID
	Loop bool // true if this looks like a loopback device rather than a mic
}

func (c *capture) devices() ([]device, error) {
	infos, err := c.ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}
	out := make([]device, 0, len(infos))
	for _, in := range infos {
		name := in.Name()
		out = append(out, device{Name: name, id: in.ID, Loop: isLoopback(name)})
	}
	return out, nil
}

// a microphone would capture the room, not the music. these are the virtual
// devices that carry system output back in as an input.
func isLoopback(name string) bool {
	n := strings.ToLower(name)
	for _, hint := range []string{"blackhole", "soundflower", "loopback"} {
		if strings.Contains(n, hint) {
			return true
		}
	}
	return false
}

// a multi output or aggregate device can feed a loopback while also
// keeping the mac speakers live, which is what most people want
func isAggregate(name string) bool {
	n := strings.ToLower(name)
	for _, hint := range []string{"aggregate", "multi-output", "multi output", "capture"} {
		if strings.Contains(n, hint) {
			return true
		}
	}
	return false
}

func (c *capture) start(name string, sink func(seq int64, pcm []byte)) error {
	devs, err := c.devices()
	if err != nil {
		return err
	}
	var chosen *device
	for i := range devs {
		if devs[i].Name == name {
			chosen = &devs[i]
			break
		}
	}
	if chosen == nil {
		return fmt.Errorf("no capture device named %q", name)
	}

	c.stop()

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = channels
	cfg.SampleRate = sampleRate
	cfg.Capture.DeviceID = chosen.id.Pointer()
	cfg.PeriodSizeInFrames = framesPerChunk

	c.mu.Lock()
	c.sink = sink
	c.seq = 0
	c.mu.Unlock()

	dev, err := malgo.InitDevice(c.ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(_, in []byte, frames uint32) {
			if frames == 0 {
				return
			}
			// the callback owns this buffer and reuses it, so copy before
			// it leaves this goroutine
			pcm := make([]byte, frames*channels*2)
			copy(pcm, in)

			c.mu.Lock()
			s := c.seq
			c.seq += int64(frames)
			fn := c.sink
			c.mu.Unlock()

			if fn != nil {
				fn(s, pcm)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("open %q: %w", name, err)
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		return fmt.Errorf("start %q: %w", name, err)
	}
	c.dev = dev
	return nil
}

func (c *capture) stop() {
	if c.dev != nil {
		c.dev.Stop()
		c.dev.Uninit()
		c.dev = nil
	}
	c.mu.Lock()
	c.sink = nil
	c.mu.Unlock()
}

func (c *capture) close() {
	c.stop()
	if c.ctx != nil {
		c.ctx.Uninit()
		c.ctx.Free()
	}
}

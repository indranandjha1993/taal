package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type client struct {
	conn  *websocket.Conn
	send  chan []byte
	audio chan []byte
	guest bool
	id    string
	seq   int
	name  string
	gain  float64 // 0 to 1, set from the host mixer
}

type server struct {
	mu      sync.Mutex
	clients map[*client]bool

	seq int // ids for joining speakers

	cap         *capture
	streaming   bool
	source      string  // capture device name
	prevOut     string  // system output before taal changed it
	keepAudible bool    // also play through the mac speakers
	epoch       float64 // host ms at which stream sample 0 was captured
	delayMs     float64 // how far behind the source speakers play
}

func newServer(cap *capture) *server {
	return &server{
		clients: make(map[*client]bool),
		cap:     cap,
		delayMs: defaultDelayMs,
	}
}

func nowMs() float64 {
	return float64(time.Now().UnixNano()) / 1e6
}

func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{
		conn:  conn,
		send:  make(chan []byte, 16),
		audio: make(chan []byte, 24), // about half a second of chunks
	}
	go c.writePump()
	s.readPump(c)
}

func (c *client) writePump() {
	defer c.conn.Close()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			if c.conn.WriteMessage(websocket.TextMessage, msg) != nil {
				return
			}
		case pcm, ok := <-c.audio:
			if !ok {
				return
			}
			if c.conn.WriteMessage(websocket.BinaryMessage, pcm) != nil {
				return
			}
		}
	}
}

func (s *server) readPump(c *client) {
	defer s.drop(c)
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg map[string]any
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		switch msg["type"] {
		case "sync":
			t0, _ := msg["t0"].(float64)
			c.trySend(marshal(map[string]any{"type": "sync", "t0": t0, "ts": nowMs()}))
		case "hello":
			name, _ := msg["name"].(string)
			s.hello(c, msg["role"] == "guest", name)
		case "start":
			audible, ok := msg["audible"].(bool)
			s.startStream(ok && audible)
		case "stop":
			s.stopStream()
		case "delay":
			d, _ := msg["ms"].(float64)
			s.setDelay(d)
		case "gain":
			id, _ := msg["id"].(string)
			g, _ := msg["gain"].(float64)
			s.setGain(id, g)
		}
	}
}

// non-blocking so one stuck client never stalls the hub
func (c *client) trySend(msg []byte) {
	select {
	case c.send <- msg:
	default:
	}
}

// audio is realtime: a chunk that cannot be queued now is already too late
// to be useful, so drop it rather than stall the stream behind it
func (c *client) trySendBinary(pcm []byte) {
	select {
	case c.audio <- pcm:
	default:
	}
}

func marshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Println("marshal:", err)
	}
	return b
}

func (s *server) hello(c *client, guest bool, name string) {
	s.mu.Lock()
	c.guest = guest
	c.name = name
	c.gain = 1
	if c.id == "" {
		s.seq++
		c.seq = s.seq
		c.id = fmt.Sprintf("s%d", s.seq)
	}
	if c.name == "" {
		c.name = "speaker " + c.id[1:]
	}
	s.clients[c] = true

	c.trySend(marshal(map[string]any{"type": "you", "id": c.id, "name": c.name}))
	c.trySend(s.stateMsg())
	s.fanout(s.rosterMsg())
	s.mu.Unlock()
}

func (s *server) drop(c *client) {
	s.mu.Lock()
	if _, live := s.clients[c]; live {
		delete(s.clients, c)
		close(c.send)
		if c.audio != nil {
			close(c.audio)
		}
	}
	s.fanout(s.rosterMsg())
	s.mu.Unlock()
}

func (s *server) setGain(id string, gain float64) {
	if gain < 0 {
		gain = 0
	} else if gain > 1 {
		gain = 1
	}
	s.mu.Lock()
	for c := range s.clients {
		if c.id == id && c.guest {
			c.gain = gain
			c.trySend(marshal(map[string]any{"type": "gain", "gain": gain}))
			break
		}
	}
	s.fanout(s.rosterMsg())
	s.mu.Unlock()
}

// caller holds the lock
func (s *server) guestCount() int {
	n := 0
	for c := range s.clients {
		if c.guest {
			n++
		}
	}
	return n
}

// the host mixer needs one row per speaker, so the roster carries the
// count as well and there is only one message to keep in sync
func (s *server) rosterMsg() []byte {
	type row struct {
		seq int
		m   map[string]any
	}
	rows := make([]row, 0, len(s.clients))
	for c := range s.clients {
		if !c.guest {
			continue
		}
		rows = append(rows, row{c.seq, map[string]any{
			"id": c.id, "name": c.name, "gain": c.gain,
		}})
	}
	// join order, so the list does not reshuffle as people come and go
	sort.Slice(rows, func(i, j int) bool { return rows[i].seq < rows[j].seq })

	list := make([]map[string]any, len(rows))
	for i, r := range rows {
		list[i] = r.m
	}
	return marshal(map[string]any{"type": "roster", "speakers": list, "n": len(list)})
}

func (s *server) broadcast(v any) {
	s.mu.Lock()
	s.fanout(marshal(v))
	s.mu.Unlock()
}

// caller holds the lock
func (s *server) fanout(msg []byte) {
	for c := range s.clients {
		c.trySend(msg)
	}
}

// caller holds the lock
func (s *server) stateMsg() []byte {
	return marshal(map[string]any{
		"type":      "state",
		"streaming": s.streaming,
		"source":    s.source,
		"audible":   s.keepAudible,
		"delayMs":   s.delayMs,
		"rate":      sampleRate,
		"channels":  channels,
	})
}

// Sets up the whole chain: build an output device that feeds both the
// loopback and (optionally) the speakers, point the mac at it, then start
// capturing. The person clicking start never sees any of this.
func (s *server) startStream(audible bool) {
	s.mu.Lock()
	if s.cap == nil {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	setup := inspectAudio(s.cap)
	if !setup.Ready {
		s.mu.Lock()
		s.fanout(marshal(map[string]any{"type": "error", "msg": setup.Detail}))
		s.mu.Unlock()
		return
	}

	prev := currentOutputName()
	speakers := ""
	if audible {
		speakers = setup.Speakers
	}
	if name, ok := buildRouting(setup.Loopback, speakers); ok {
		setOutput(name)
	} else {
		// no routing device, fall back to sending everything to the
		// loopback. the mac goes quiet but the stream works.
		setOutput(setup.Loopback)
	}

	source := setup.Loopback

	// the audio callback runs on its own thread and fans out directly,
	// so the capture is started outside the lock
	err := s.cap.start(source, s.onChunk)

	s.mu.Lock()
	if err != nil {
		s.streaming = false
		if prev != "" {
			setOutput(prev)
		}
		removeRouting()
		s.fanout(marshal(map[string]any{"type": "error", "msg": err.Error()}))
		s.fanout(s.stateMsg())
		s.mu.Unlock()
		return
	}
	s.streaming = true
	s.source = source
	s.prevOut = prev
	s.keepAudible = audible
	s.epoch = 0 // set by the first chunk, when capture is really running
	s.fanout(s.stateMsg())
	s.mu.Unlock()
}

// leaving someone's mac routed through a device taal invented would be
// rude, so put the output back exactly as it was
func (s *server) stopStream() {
	s.cap.stop()

	s.mu.Lock()
	prev := s.prevOut
	s.streaming = false
	s.prevOut = ""
	s.mu.Unlock()

	if prev != "" {
		setOutput(prev)
	}
	removeRouting()

	s.mu.Lock()
	s.fanout(s.stateMsg())
	s.mu.Unlock()
}

func (s *server) setDelay(ms float64) {
	if ms < 100 {
		ms = 100
	} else if ms > 2000 {
		ms = 2000
	}
	s.mu.Lock()
	s.delayMs = ms
	// the epoch moves with the delay, otherwise already queued audio would
	// keep the old timing and the change would only apply to new chunks
	s.epoch = 0
	s.fanout(s.stateMsg())
	s.mu.Unlock()
}

// called from the audio thread for every captured chunk
func (s *server) onChunk(seq int64, pcm []byte) {
	s.mu.Lock()
	if !s.streaming {
		s.mu.Unlock()
		return
	}
	if s.epoch == 0 {
		// first chunk after start or a delay change anchors the timeline:
		// this sample is heard delayMs from now
		s.epoch = nowMs() + s.delayMs - framesToMs(seq)
	}
	startAt := s.epoch + framesToMs(seq)
	frame := encodeChunk(startAt, seq, pcm)
	for c := range s.clients {
		if c.guest {
			c.trySendBinary(frame)
		}
	}
	s.mu.Unlock()
}

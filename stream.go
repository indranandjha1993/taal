package main

import (
	"encoding/binary"
	"math"
)

// Binary frame layout, little endian:
//
//   magic   uint32   'T','A','A','L'
//   startAt float64  host ms at which sample 0 of this chunk is heard
//   seq     int64    sample index since the stream began
//   pcm     int16[]  interleaved stereo
//
// Audio goes as binary rather than JSON because base64 would cost a third
// more bytes for no benefit, and there is one of these every 20ms.

const (
	frameMagic  = 0x4c414154 // "TAAL" little endian
	frameHeader = 4 + 8 + 8
)

func encodeChunk(startAt float64, seq int64, pcm []byte) []byte {
	buf := make([]byte, frameHeader+len(pcm))
	binary.LittleEndian.PutUint32(buf[0:], frameMagic)
	binary.LittleEndian.PutUint64(buf[4:], math.Float64bits(startAt))
	binary.LittleEndian.PutUint64(buf[12:], uint64(seq))
	copy(buf[frameHeader:], pcm)
	return buf
}

// samples to milliseconds at the capture rate
func framesToMs(frames int64) float64 {
	return float64(frames) * 1000 / sampleRate
}

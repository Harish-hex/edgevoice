// Package audio holds pure audio helpers: wire framing, PCM conversion, WAV I/O, endpointing.
package audio

import (
	"encoding/binary"
	"io"
)

// Frame types on the host<->container stream: [type:1][len:4 LE][payload].
const (
	FramePCM     byte = 1 // int16 LE, 16 kHz mono
	FrameControl byte = 2 // e.g. "ptt_down", "ptt_up"
	FrameStatus  byte = 3 // UTF-8 status line, container -> host
	FrameFlush   byte = 4 // container -> host: drop queued playback (barge-in)
)

func WriteFrame(w io.Writer, typ byte, payload []byte) error {
	var hdr [5]byte
	hdr[0] = typ
	binary.LittleEndian.PutUint32(hdr[1:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func ReadFrame(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	buf := make([]byte, binary.LittleEndian.Uint32(hdr[1:]))
	_, err := io.ReadFull(r, buf)
	return hdr[0], buf, err
}

func PCMToBytes(p []int16) []byte {
	b := make([]byte, 2*len(p))
	for i, s := range p {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(s))
	}
	return b
}

func BytesToPCM(b []byte) []int16 {
	p := make([]int16, len(b)/2)
	for i := range p {
		p[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return p
}

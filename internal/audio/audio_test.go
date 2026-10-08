package audio

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	pcm := []int16{1, -2, 32767, -32768}
	WriteFrame(&buf, FramePCM, PCMToBytes(pcm))
	WriteFrame(&buf, FrameControl, []byte("ptt_down"))
	typ, p, err := ReadFrame(&buf)
	if err != nil || typ != FramePCM || len(BytesToPCM(p)) != 4 || BytesToPCM(p)[3] != -32768 {
		t.Fatalf("pcm frame: %v %v %v", typ, p, err)
	}
	typ, p, _ = ReadFrame(&buf)
	if typ != FrameControl || string(p) != "ptt_down" {
		t.Fatal("control frame")
	}
	if _, _, err := ReadFrame(&buf); err == nil {
		t.Fatal("want EOF")
	}
}

func TestWAVRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.wav")
	in := make([]int16, 1600)
	for i := range in {
		in[i] = int16(i)
	}
	if err := WriteWAV(p, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadWAV(p)
	if err != nil || len(out) != len(in) || out[100] != 100 {
		t.Fatalf("wav: %v len=%d", err, len(out))
	}
}

func TestEndpointer(t *testing.T) {
	ms := time.Millisecond
	complete := false
	e := &Endpointer{Silence: 400 * ms, EarlySilence: 200 * ms, MaxUtterance: 10 * time.Second, CompleteFn: func(string) bool { return complete }}
	if e.Update(false, "", 100*ms) {
		t.Fatal("no speech yet")
	}
	e.Update(true, "", 200*ms)
	e.Update(true, "", 1000*ms)
	if e.Update(false, "", 1300*ms) {
		t.Fatal("300ms silence < 400ms")
	}
	if !e.Update(false, "", 1400*ms) {
		t.Fatal("400ms silence should end")
	}
	if e.LastVoiced() != 1000*ms {
		t.Fatal("last voiced")
	}
	e.Reset()
	complete = true
	e.Update(true, "", 0)
	if !e.Update(false, "alarm", 200*ms) {
		t.Fatal("semantic early end at 200ms")
	}
}

func TestJoinCrossfade(t *testing.T) {
	a := make([]int16, 1000)
	b := make([]int16, 1000)
	if got := len(Join([][]int16{a, b}, 40)); got != 2000-640 {
		t.Fatalf("len %d", got)
	}
}

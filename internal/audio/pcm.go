package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const SampleRate = 16000

func ToFloat(p []int16) []float32 {
	f := make([]float32, len(p))
	for i, s := range p {
		f[i] = float32(s) / 32768
	}
	return f
}

func ToInt16(f []float32) []int16 {
	p := make([]int16, len(f))
	for i, s := range f {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		p[i] = int16(s * 32767)
	}
	return p
}

// Resample is a linear resampler — the only resampling point in the system (PRD §7.7).
func Resample(f []float32, from, to int) []float32 {
	if from == to || len(f) == 0 {
		return f
	}
	n := int(int64(len(f)) * int64(to) / int64(from))
	out := make([]float32, n)
	for i := range out {
		pos := float64(i) * float64(from) / float64(to)
		j := int(pos)
		frac := float32(pos - float64(j))
		if j+1 < len(f) {
			out[i] = f[j]*(1-frac) + f[j+1]*frac
		} else {
			out[i] = f[len(f)-1]
		}
	}
	return out
}

// Join concatenates clips with a linear crossfade of ms milliseconds.
func Join(clips [][]int16, ms int) []int16 {
	xf := SampleRate * ms / 1000
	var out []int16
	for _, c := range clips {
		if len(out) == 0 || xf == 0 {
			out = append(out, c...)
			continue
		}
		n := min(xf, len(out), len(c))
		base := len(out) - n
		for i := 0; i < n; i++ {
			a := float32(i) / float32(n)
			out[base+i] = int16(float32(out[base+i])*(1-a) + float32(c[i])*a)
		}
		out = append(out, c[n:]...)
	}
	return out
}

// ReadWAV reads a 16-bit PCM mono WAV and returns samples resampled to 16 kHz.
func ReadWAV(path string) ([]int16, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, fmt.Errorf("%s: not a WAV", path)
	}
	var rate, ch, bits int
	for off := 12; off+8 <= len(b); {
		id, sz := string(b[off:off+4]), int(binary.LittleEndian.Uint32(b[off+4:]))
		body := b[off+8 : min(off+8+sz, len(b))]
		switch id {
		case "fmt ":
			ch = int(binary.LittleEndian.Uint16(body[2:]))
			rate = int(binary.LittleEndian.Uint32(body[4:]))
			bits = int(binary.LittleEndian.Uint16(body[14:]))
		case "data":
			if bits != 16 {
				return nil, fmt.Errorf("%s: %d-bit WAV unsupported", path, bits)
			}
			p := BytesToPCM(body)
			if ch > 1 {
				mono := make([]int16, len(p)/ch)
				for i := range mono {
					mono[i] = p[i*ch]
				}
				p = mono
			}
			if rate == SampleRate {
				return p, nil
			}
			return ToInt16(Resample(ToFloat(p), rate, SampleRate)), nil
		}
		off += 8 + sz + sz%2
	}
	return nil, fmt.Errorf("%s: no data chunk", path)
}

// WriteWAV writes 16 kHz mono 16-bit PCM.
func WriteWAV(path string, p []int16) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeWAV(f, p)
}

func writeWAV(w io.Writer, p []int16) error {
	data := PCMToBytes(p)
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(data)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], SampleRate)
	binary.LittleEndian.PutUint32(h[28:], SampleRate*2)
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(data)))
	if _, err := w.Write(h); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

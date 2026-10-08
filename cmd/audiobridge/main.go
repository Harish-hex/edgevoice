// audiobridge: host-side mic/speaker <-> container (PRD §4). macOS CoreAudio via miniaudio (malgo).
//
//	go run ./cmd/audiobridge                       # spawns the container itself (stdio transport, --network=none)
//	go run ./cmd/audiobridge -transport sock       # connects to ./sock/audio.sock
//	go run ./cmd/audiobridge -loopback             # mic -> speaker test, no container
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/malgo"

	"edgevoice/internal/audio"
)

func main() {
	transport := flag.String("transport", "stdio", "stdio | sock")
	cfg := flag.String("config", "config/default.yaml", "config passed to the container")
	cpus := flag.String("cpus", "2", "container CPU limit")
	mem := flag.String("mem", "2g", "container memory limit")
	loopback := flag.Bool("loopback", false, "mic->speaker test without container")
	flag.Parse()

	var r io.Reader
	var w io.Writer
	switch {
	case *loopback:
		pr, pw := io.Pipe()
		r, w = pr, pw
	case *transport == "sock":
		c, err := net.Dial("unix", "sock/audio.sock")
		if err != nil {
			log.Fatalf("connect sock/audio.sock: %v (is the container running with -transport sock?)", err)
		}
		r, w = c, c
	default:
		cmd := exec.Command("docker/run.sh", "go", "run", "./cmd/edgevoice", "-config", *cfg, "-transport", "stdio")
		cmd.Env = append(os.Environ(), "CPUS="+*cpus, "MEM="+*mem, "NAME=edgevoice")
		cmd.Stderr = os.Stderr
		in, _ := cmd.StdinPipe()
		out, _ := cmd.StdoutPipe()
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		defer cmd.Process.Kill()
		r, w = out, in
		fmt.Printf("started container (cpus=%s mem=%s, --network=none); loading models...\n", *cpus, *mem)
	}

	// playback queue
	var mu sync.Mutex
	var play []int16
	bw := bufio.NewWriter(w)
	var wmu sync.Mutex

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer ctx.Uninit()
	dc := malgo.DefaultDeviceConfig(malgo.Duplex)
	dc.Capture.Format, dc.Capture.Channels = malgo.FormatS16, 1
	dc.Playback.Format, dc.Playback.Channels = malgo.FormatS16, 1
	dc.SampleRate = audio.SampleRate
	dc.PeriodSizeInMilliseconds = 32

	onData := func(out, in []byte, frames uint32) {
		if len(in) > 0 {
			if *loopback {
				mu.Lock()
				play = append(play, audio.BytesToPCM(in)...)
				mu.Unlock()
			} else {
				buf := append([]byte(nil), in...)
				go func() {
					wmu.Lock()
					audio.WriteFrame(bw, audio.FramePCM, buf)
					bw.Flush()
					wmu.Unlock()
				}()
			}
		}
		mu.Lock()
		n := min(len(out)/2, len(play))
		copy(out, audio.PCMToBytes(play[:n]))
		for i := 2 * n; i < len(out); i++ {
			out[i] = 0
		}
		play = play[n:]
		mu.Unlock()
	}
	dev, err := malgo.InitDevice(ctx.Context, dc, malgo.DeviceCallbacks{Data: onData})
	if err != nil {
		log.Fatal(err)
	}
	defer dev.Uninit()
	if err := dev.Start(); err != nil {
		log.Fatal(err)
	}
	if *loopback {
		fmt.Println("loopback: speak, you should hear yourself. Ctrl-C to stop.")
		select {}
	}

	br := bufio.NewReader(r)
	for {
		typ, payload, err := audio.ReadFrame(br)
		if err != nil {
			log.Printf("container stream ended: %v", err)
			return
		}
		switch typ {
		case audio.FramePCM:
			mu.Lock()
			play = append(play, audio.BytesToPCM(payload)...)
			mu.Unlock()
		case audio.FrameStatus:
			fmt.Printf("\n[%s] %s\n", time.Now().Format("15:04:05"), strings.TrimSpace(string(payload)))
		case audio.FrameFlush:
			mu.Lock()
			play = nil
			mu.Unlock()
		}
	}
}

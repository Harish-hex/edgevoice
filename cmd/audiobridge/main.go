// audiobridge: host-side mic/speaker <-> container (PRD §4). macOS CoreAudio via miniaudio (malgo).
//
//	go run ./cmd/audiobridge                 # spawns the container (stdio transport, --network=none)
//	go run ./cmd/audiobridge -list           # list microphones
//	go run ./cmd/audiobridge -mic 2          # use microphone #2 from -list
//	go run ./cmd/audiobridge -loopback       # mic -> speaker test with level meter, no container
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/malgo"

	"edgevoice/internal/audio"
)

func main() {
	cfg := flag.String("config", "config/default.yaml", "config passed to the container")
	cpus := flag.String("cpus", "2", "container CPU limit")
	mem := flag.String("mem", "2g", "container memory limit")
	list := flag.Bool("list", false, "list microphones and exit")
	micIdx := flag.Int("mic", -1, "microphone index from -list (default: system default)")
	gain := flag.Float64("gain", 1.0, "mic gain multiplier (try 2-4 for quiet headset mics)")
	loopback := flag.Bool("loopback", false, "mic->speaker test without container")
	uiAddr := flag.String("ui", "127.0.0.1:8080", "dashboard address (\"\" to disable)")
	openUI := flag.Bool("open", true, "open the dashboard in the browser")
	feed := flag.String("feed", "", "comma-separated WAV files to play INTO the assistant instead of the mic (testing / backup demo)")
	feedGap := flag.Duration("feedgap", 4*time.Second, "silence between -feed files")
	flag.Parse()

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer ctx.Uninit()
	mics, _ := ctx.Devices(malgo.Capture)
	if *list {
		for i, d := range mics {
			def := ""
			if d.IsDefault != 0 {
				def = " (default)"
			}
			fmt.Printf("%d: %s%s\n", i, d.Name(), def)
		}
		return
	}

	hub := newHub()
	if *uiAddr != "" && !*loopback {
		go serveUI(*uiAddr, hub)
		url := "http://" + *uiAddr
		fmt.Printf("dashboard: %s\n", url)
		if *openUI {
			exec.Command("open", url).Start()
		}
	}

	// ---- container stream ----
	var r io.Reader
	var w io.Writer
	if *loopback {
		pr, pw := io.Pipe()
		r, w = pr, pw
		go io.Copy(io.Discard, pr)
	} else {
		cmd := exec.Command("docker/run.sh", "bin/edgevoice", "-config", *cfg, "-transport", "stdio", "-dump")
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

	// ---- playback ----
	var mu sync.Mutex
	var play []int16
	pc := malgo.DefaultDeviceConfig(malgo.Playback)
	pc.Playback.Format, pc.Playback.Channels, pc.SampleRate = malgo.FormatS16, 1, audio.SampleRate
	spk, err := malgo.InitDevice(ctx.Context, pc, malgo.DeviceCallbacks{Data: func(out, _ []byte, _ uint32) {
		mu.Lock()
		n := min(len(out)/2, len(play))
		copy(out, audio.PCMToBytes(play[:n]))
		clear(out[2*n:])
		play = play[n:]
		mu.Unlock()
	}})
	if err != nil {
		log.Fatal(err)
	}
	defer spk.Uninit()
	spk.Start()

	// ---- capture: ONE ordered writer goroutine (frames must never be reordered) ----
	frames := make(chan []int16, 256)
	var level float64
	var lmu sync.Mutex
	cc := malgo.DefaultDeviceConfig(malgo.Capture)
	cc.Capture.Format, cc.Capture.Channels, cc.SampleRate = malgo.FormatS16, 1, audio.SampleRate
	cc.PeriodSizeInMilliseconds = 32
	micName := "system default"
	if *micIdx >= 0 && *micIdx < len(mics) {
		cc.Capture.DeviceID = mics[*micIdx].ID.Pointer()
		micName = mics[*micIdx].Name()
	} else {
		for _, d := range mics {
			if d.IsDefault != 0 {
				micName = d.Name() + " (default)"
			}
		}
	}
	mic, err := malgo.InitDevice(ctx.Context, cc, malgo.DeviceCallbacks{Data: func(_, in []byte, _ uint32) {
		p := audio.BytesToPCM(in)
		var sum float64
		for i, s := range p {
			v := float64(s) * *gain
			v = math.Max(-32768, math.Min(32767, v))
			p[i] = int16(v)
			sum += v * v
		}
		lmu.Lock()
		level = math.Sqrt(sum / float64(max(1, len(p))))
		lmu.Unlock()
		select {
		case frames <- p:
		default: // writer stalled: drop rather than block the audio thread
		}
	}})
	if err != nil {
		log.Fatal(err)
	}
	defer mic.Uninit()
	ready := make(chan struct{})
	if *feed == "" {
		if err := mic.Start(); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("microphone: %s  (change with -list / -mic N)\n", micName)
	} else {
		micName = "WAV feed"
		go feedWAVs(strings.Split(*feed, ","), *feedGap, ready, frames, &level, &lmu)
	}

	bw := bufio.NewWriter(w)
	go func() {
		for p := range frames {
			if *loopback {
				mu.Lock()
				play = append(play, p...)
				mu.Unlock()
				continue
			}
			audio.WriteFrame(bw, audio.FramePCM, audio.PCMToBytes(p))
			bw.Flush()
		}
	}()

	// level meter (same line), so you can see the mic is live
	go func() {
		for range time.Tick(150 * time.Millisecond) {
			lmu.Lock()
			l := level
			lmu.Unlock()
			db := 20 * math.Log10(math.Max(l, 1)/32768)
			hub.publish(map[string]any{"kind": "mic", "db": db, "mic": micName})
			bars := int(math.Max(0, (db+60)/2))
			fmt.Printf("\rmic %5.0f dB |%-30s|", db, strings.Repeat("#", min(bars, 30)))
		}
	}()

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
			select {
			case <-ready:
			default:
				close(ready)
			}
			fmt.Printf("\r%-60s\r[%s] %s\n", "", time.Now().Format("15:04:05"), strings.TrimSpace(string(payload)))
			hub.publish(map[string]any{"kind": "status", "text": strings.TrimSpace(string(payload))})
		case audio.FrameEvent:
			var ev map[string]any
			if json.Unmarshal(payload, &ev) == nil {
				hub.publish(ev)
				hostAction(ev, hub)
			}
		case audio.FrameFlush:
			mu.Lock()
			play = nil
			mu.Unlock()
		}
	}
}

// feedWAVs streams WAV files (real-time, 32 ms frames) in place of the microphone, with silence in
// between so the VAD/endpointer behave as with live speech. Starts once the container reports ready.
func feedWAVs(files []string, gap time.Duration, ready <-chan struct{}, frames chan<- []int16, level *float64, lmu *sync.Mutex) {
	const n = 512
	tick := time.NewTicker(32 * time.Millisecond)
	defer tick.Stop()
	send := func(p []int16) {
		<-tick.C
		var sum float64
		for _, v := range p {
			sum += float64(v) * float64(v)
		}
		lmu.Lock()
		*level = math.Sqrt(sum / float64(len(p)))
		lmu.Unlock()
		frames <- p
	}
	silence := func(d time.Duration) {
		for i := 0; i < int(d/(32*time.Millisecond)); i++ {
			send(make([]int16, n))
		}
	}
	<-ready
	time.Sleep(2 * time.Second) // let the dashboard connect
	for _, f := range files {
		pcm, err := audio.ReadWAV(strings.TrimSpace(f))
		if err != nil {
			log.Printf("feed: %v", err)
			continue
		}
		fmt.Printf("\nfeed: %s\n", f)
		for off := 0; off < len(pcm); off += n {
			send(append([]int16(nil), pcm[off:min(off+n, len(pcm))]...))
		}
		silence(gap)
	}
	fmt.Println("\nfeed: done (Ctrl-C to quit)")
	for {
		silence(time.Second)
	}
}

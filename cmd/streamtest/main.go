// streamtest: sends WAV files through the live stdio transport (exactly what audiobridge does with the
// mic) and prints the container's status lines and reply audio length. Verifies the live path without a mic.
package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"edgevoice/internal/audio"
)

func main() {
	cmd := exec.Command("docker/run.sh", "bin/edgevoice", "-transport", "stdio")
	cmd.Env = append(os.Environ(), "CPUS=2", "MEM=2g")
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	defer cmd.Process.Kill()
	ready := make(chan bool, 1)
	go func() {
		br := bufio.NewReader(out)
		samples := 0
		for {
			typ, p, err := audio.ReadFrame(br)
			if err != nil {
				return
			}
			switch typ {
			case audio.FrameStatus:
				fmt.Printf("[status] %s\n", p)
				select {
				case ready <- true:
				default:
				}
			case audio.FramePCM:
				samples += len(p) / 2
				fmt.Printf("\r[audio] %.2fs received", float64(samples)/16000)
			}
		}
	}()
	<-ready
	bw := bufio.NewWriter(in)
	for _, f := range os.Args[1:] {
		pcm, err := audio.ReadWAV(f)
		if err != nil {
			log.Fatal(err)
		}
		pcm = append(pcm, make([]int16, 16000*2)...)
		fmt.Printf("\n>>> sending %s\n", f)
		for off := 0; off < len(pcm); off += 512 {
			audio.WriteFrame(bw, audio.FramePCM, audio.PCMToBytes(pcm[off:min(off+512, len(pcm))]))
			bw.Flush()
			time.Sleep(32 * time.Millisecond)
		}
		time.Sleep(3 * time.Second)
	}
	fmt.Println()
}

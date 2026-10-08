package llm

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// Server supervises a llama-server subprocess bound to 127.0.0.1 (still fully offline).
type Server struct {
	Model   string
	Threads int
	Ctx     int
	Port    int

	mu  sync.Mutex
	cmd *exec.Cmd
}

func (s *Server) URL() string { return "http://127.0.0.1:" + strconv.Itoa(s.Port) }

// Start launches llama-server and waits for /health (up to 90 s).
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := exec.Command("llama-server", "-m", s.Model, "--threads", strconv.Itoa(s.Threads),
		"--ctx-size", strconv.Itoa(s.Ctx), "--parallel", "1", "--host", "127.0.0.1", "--port", strconv.Itoa(s.Port),
		"--jinja", "--no-webui", "-ngl", "0")
	cmd.Stdout, cmd.Stderr = nil, nil
	if os.Getenv("LLAMA_LOG") != "" {
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("llama-server: %w", err)
	}
	s.cmd = cmd
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if r, err := http.Get(s.URL() + "/health"); err == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("llama-server: not healthy after 90s")
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}
	s.cmd = nil
}

func (s *Server) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil || s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

const SystemEN = "You are EdgeVoice, an offline voice assistant on a small device. Answer the user's question directly and correctly in one or two short spoken sentences. Do not repeat the question. Answer confidently from your general knowledge. No lists, no markdown, no emojis. You cannot browse the internet. Never claim to have done an action."
const SystemTA = "You are EdgeVoice, an offline voice assistant. The user speaks Tanglish (Tamil written in English letters mixed with English). Reply in one or two short sentences of simple Tanglish written in English letters. No markdown. Never claim to have done an action."

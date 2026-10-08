package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
)

//go:embed ui.html
var uiHTML []byte

// hub fans dashboard events out to every connected browser (Server-Sent Events). It runs on the HOST,
// bound to 127.0.0.1; the container itself still has no network.
type hub struct {
	mu   sync.Mutex
	subs map[chan []byte]bool
	last map[string][]byte // latest event per kind, replayed to new browsers
	hist [][]byte          // recent turns, replayed to new browsers
}

func newHub() *hub { return &hub{subs: map[chan []byte]bool{}, last: map[string][]byte{}} }

func (h *hub) publish(ev map[string]any) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	kind, _ := ev["kind"].(string)
	h.mu.Lock()
	defer h.mu.Unlock()
	if kind == "turn" {
		h.hist = append(h.hist, b)
		if len(h.hist) > 50 {
			h.hist = h.hist[1:]
		}
	} else {
		h.last[kind] = b
	}
	for c := range h.subs {
		select {
		case c <- b:
		default: // slow browser: drop
		}
	}
}

func serveUI(addr string, h *hub) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(uiHTML)
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		c := make(chan []byte, 256)
		h.mu.Lock()
		for _, b := range h.hist {
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		for _, b := range h.last {
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		h.subs[c] = true
		h.mu.Unlock()
		fl.Flush()
		defer func() { h.mu.Lock(); delete(h.subs, c); h.mu.Unlock() }()
		for {
			select {
			case <-r.Context().Done():
				return
			case b := <-c:
				fmt.Fprintf(w, "data: %s\n\n", b)
				fl.Flush()
			}
		}
	})
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("dashboard: %v", err)
	}
}

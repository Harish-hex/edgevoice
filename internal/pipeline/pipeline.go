//go:build linux

// Package pipeline wires VAD → endpointer → ASR → NLU → (clips | LLM → clause TTS) → audio out.
package pipeline

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"edgevoice/internal/actions"
	"edgevoice/internal/audio"
	"edgevoice/internal/config"
	"edgevoice/internal/iface"
	"edgevoice/internal/llm"
	"edgevoice/internal/metrics"
	"edgevoice/internal/nlu"
	"edgevoice/internal/reply"
	"edgevoice/internal/sherpa"
)

// Output receives reply audio and status lines.
type Output interface {
	PCM(p []int16)
	Status(s string)
	Event(kind string, data map[string]any) // dashboard events (state changes, live samples, turns)
}

// UIState reports the assistant's visible state for the dashboard.
func (p *Pipeline) UIState() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := metrics.Now()
	switch {
	case now < p.busyUntil:
		return "speaking"
	case p.thinking:
		return "thinking"
	case p.listening:
		return "listening"
	case !p.Cfg.Wake.Enabled || now < p.awakeUntil:
		return "awake"
	}
	return "asleep"
}

func (p *Pipeline) setFlags(listening, thinking bool) {
	p.mu.Lock()
	p.listening, p.thinking = listening, thinking
	p.mu.Unlock()
	p.Out.Event("state", map[string]any{"state": p.UIState()})
}

type Pipeline struct {
	Cfg     *config.Config
	VAD     *sherpa.VAD
	ASR     *sherpa.StreamingASR // nil when Cfg.ASR.Engine == "whisper"
	Whisper *sherpa.Whisper
	Tamil   *sherpa.Whisper // IndicConformer-Tamil (dual ASR); nil => English only
	TTSEn   *sherpa.TTS
	TTSTa   *sherpa.TTS // may be nil (falls back to English voice)
	Clips   *ClipStore
	LLM     *llm.Client // nil => commands only (tier T3)
	Bus     *metrics.Bus
	Out     Output

	Norm   *nlu.Normalizer
	Parser *nlu.Parser
	State  actions.State
	Tier   func() string

	mu        sync.Mutex
	llmReady  bool
	busyUntil time.Duration
	// File labels the current replayed file (harness).
	File string
	// HalfDuplex drops mic input while a reply is playing (live mode). Off for replay.
	HalfDuplex bool
	// DumpDir, if set, receives each turn's input audio as turn_<id>.wav (debugging: what did it hear?).
	DumpDir string

	awakeUntil time.Duration
	listening  bool
	thinking   bool
	prev       *iface.Intent // last command, for short follow-ups ("innaikku illa, naalaikku")
	prevAt     time.Duration
}

func (p *Pipeline) SetLLMReady(v bool) { p.mu.Lock(); p.llmReady = v; p.mu.Unlock() }

// LLMReadyNow reports whether the chat path is usable.
func (p *Pipeline) LLMReadyNow() bool { return p.llmOK() }

func (p *Pipeline) llmOK() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.llmReady && p.LLM != nil }

// Run consumes PCM chunks of any size until in closes. Turns are handled synchronously
// (half-duplex: input during a reply is dropped so the speaker can't trigger the mic).
func (p *Pipeline) Run(ctx context.Context, in <-chan []int16) {
	ep := &audio.Endpointer{
		Silence:      time.Duration(p.Cfg.Endpoint.SilenceMs) * time.Millisecond,
		EarlySilence: time.Duration(p.Cfg.Endpoint.EarlySilenceMs) * time.Millisecond,
		MaxUtterance: time.Duration(p.Cfg.Endpoint.MaxUtteranceMs) * time.Millisecond,
	}
	// bareWake: partial is only the wake phrase (wait for the command); command: rest parses.
	split := func(partial string) (rest iface.NormalizedText, bareWake bool) {
		n := p.Norm.Normalize(iface.Transcript{Text: partial})
		if p.Cfg.Wake.Enabled {
			if r, ok := nlu.StripWake(n); ok {
				return r, len(r.Tokens) == 0
			}
		}
		return n, false
	}
	if ep.EarlySilence > 0 {
		ep.CompleteFn = func(partial string) bool {
			if partial == "" {
				return false
			}
			rest, bare := split(partial)
			return !bare && p.Parser.Parse(rest) != nil
		}
	}
	if p.Cfg.Wake.Enabled && p.Cfg.Wake.GraceMs > 0 {
		ep.ExtendFn = func(partial string) time.Duration {
			if _, bare := split(partial); bare {
				return time.Duration(p.Cfg.Wake.GraceMs) * time.Millisecond
			}
			return 0
		}
	}
	var buf, utter []int16
	var preroll [][]float32
	var turn *metrics.Turn
	partial, n := "", 0
	for chunk := range in {
		if ctx.Err() != nil {
			return
		}
		buf = append(buf, chunk...)
		for len(buf) >= sherpa.VADWindow {
			win := buf[:sherpa.VADWindow]
			buf = buf[sherpa.VADWindow:]
			now := metrics.Now()
			p.mu.Lock()
			busy := p.HalfDuplex && now < p.busyUntil
			p.mu.Unlock()
			if busy {
				continue
			}
			f := audio.ToFloat(win)
			speech := p.VAD.IsSpeech(f)
			if turn == nil {
				preroll = append(preroll, f)
				if len(preroll) > 20 { // ~640 ms of audio before VAD fired (quiet mics trigger late)
					preroll = preroll[1:]
				}
				if !speech {
					continue
				}
				turn = p.Bus.Start()
				p.setFlags(true, false)
				turn.MarkAt("t_speech_start", now)
				ep.Reset()
				partial, n, utter = "", 0, nil
				for _, pf := range preroll {
					p.feed(pf, &utter)
				}
				preroll = nil
				continue
			}
			p.feed(f, &utter)
			n++
			if p.ASR != nil && n%3 == 0 {
				partial = p.ASR.Partial()
			}
			t := now
			if speech {
				t = now - sherpa.Hangover
			}
			if ep.Update(speech, partial, t) {
				turn.MarkAt("t_last_voiced", ep.LastVoiced())
				turn.Mark("t_endpoint")
				p.setFlags(false, true)
				p.handle(ctx, turn, utter)
				p.setFlags(false, false)
				turn = nil
				p.VAD.Reset()
			}
		}
	}
}

func (p *Pipeline) feed(f []float32, utter *[]int16) {
	if p.ASR != nil {
		p.ASR.Accept(f)
	}
	*utter = append(*utter, audio.ToInt16(f)...)
}

// handle runs one turn end to end and writes its metrics record.
func (p *Pipeline) handle(ctx context.Context, turn *metrics.Turn, utter []int16) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("turn %d panic: %v", turn.ID, r)
			p.say(turn, reply.Fragments("error", nlu.ModeEnglish, nil), true)
		}
		p.Bus.End(turn)
	}()
	// Dual ASR: the Tamil model decodes the whole utterance while the English stream finalizes.
	var taText string
	var wg sync.WaitGroup
	if p.Tamil != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			taText = p.Tamil.Transcribe(audio.ToFloat(audio.TrimSilence(utter, 50)))
		}()
	}
	var text string
	if p.ASR != nil {
		text = p.ASR.Finalize()
	} else {
		text = p.Whisper.Transcribe(audio.ToFloat(utter))
	}
	wg.Wait()
	turn.Mark("t_asr_final")
	if p.DumpDir != "" {
		os.MkdirAll(p.DumpDir, 0o755)
		audio.WriteWAV(filepath.Join(p.DumpDir, fmt.Sprintf("turn_%03d.wav", turn.ID)), utter)
	}
	turn.Set("file", p.File)
	if p.Tier != nil {
		turn.Set("tier", p.Tier())
	}
	cands := []nlu.Candidate{p.candidate("en", text)}
	if p.ASR != nil {
		lp, tpw := nlu.ConfFromTokens(text, p.ASR.LastLogProbs)
		cands[0].HasConf, cands[0].AvgLogProb, cands[0].TokensPerWord = len(p.ASR.LastLogProbs) > 0, lp, tpw
		turn.Set("en_logprob", lp)
		turn.Set("en_tok_per_word", tpw)
	}
	if p.Tamil != nil {
		cands = append(cands, p.candidate("ta", taText))
		turn.Set("transcript_en", text)
		turn.Set("transcript_ta", taText)
	}
	if p.Cfg.Wake.Enabled {
		p.mu.Lock()
		// awake is judged when the user STARTED speaking, so a long follow-up isn't cut off
		awake := turn.Marks["t_speech_start"] < p.awakeUntil
		p.mu.Unlock()
		woke, wakeOnly := false, true
		for i := range cands {
			if rest, ok := nlu.StripWake(cands[i].Norm); ok {
				woke = true
				cands[i].Norm, cands[i].Text = rest, rest.Canonical
				cands[i].Intent = nil
				if p.Cfg.NLU.FastPath && len(rest.Tokens) > 0 {
					cands[i].Intent = p.Parser.Parse(rest)
				}
				if len(rest.Tokens) > 0 {
					wakeOnly = false
				}
			}
		}
		turn.Set("woke", woke)
		if !woke && !awake {
			turn.Set("route", "asleep")
			turn.Set("transcript", text)
			p.Out.Status(fmt.Sprintf("(asleep — say \"Hey Computer\") heard: %s", text))
			return
		}
		if woke && wakeOnly {
			turn.Set("route", "wake")
			turn.Set("transcript", "Hey Computer")
			mode := nlu.ModeEnglish
			if cands[len(cands)-1].Source == "ta" && nlu.LangMode(cands[len(cands)-1].Norm) == nlu.ModeTanglish {
				mode = nlu.ModeTanglish
			}
			turn.Set("reply", reply.Display("wake", mode, nil))
			p.Out.Status("edgevoice: " + reply.Display("wake", mode, nil) + "  (listening…)")
			p.say(turn, reply.Fragments("wake", mode, nil), p.Cfg.Reply.Clips)
			return
		}
	}
	gate := nlu.DefaultGate
	if !p.Cfg.NLU.Gate {
		gate = nlu.Gate{EnChatMinLP: -1e9, EnCmdMinLP: -1e9, EnMaxTPW: 1e9, ChatMinCov: 0, ChatMinWords: 1}
	}
	best, clear := p.Norm.Lex.ChooseWith(gate, cands)
	if best.Intent == nil && p.prev != nil && metrics.Now()-p.prevAt < 30*time.Second {
		for i := len(cands) - 1; i >= 0; i-- { // Tamil first: follow-ups are usually Tanglish
			if f := nlu.FollowUp(p.prev, cands[i].Norm); f != nil {
				best = cands[i]
				best.Intent = f
				clear = true
				turn.Set("followup", true)
				break
			}
		}
	}
	allEmpty := true
	for _, c := range cands {
		if strings.TrimSpace(c.Text) != "" {
			allEmpty = false
		}
	}
	if allEmpty {
		turn.Set("route", "empty")
		return
	}
	if !clear {
		// Fail gracefully: nothing was recognised confidently enough to act on or to send to the LLM.
		mode := nlu.UnclearMode(cands)
		turn.Set("route", "unclear")
		turn.Set("intent", "unclear")
		turn.Set("mode", mode)
		turn.Set("transcript", strings.TrimSpace(cands[len(cands)-1].Text+" / "+cands[0].Text))
		turn.Mark("t_nlu_done")
		disp := reply.Display("error", mode, nil)
		turn.Set("reply", disp)
		p.Out.Status(fmt.Sprintf("(not confident) heard: %s\nedgevoice: %s", turn.Fields["transcript"], disp))
		p.say(turn, reply.Fragments("error", mode, nil), p.Cfg.Reply.Clips)
		return
	}
	text, norm, in := best.Text, best.Norm, best.Intent
	turn.Set("transcript", text)
	turn.Set("asr_pick", best.Source)
	if best.Source == "ta" {
		text = norm.Canonical // romanized for display and the LLM (D13 default)
	}
	mode := nlu.LangMode(norm)
	turn.Mark("t_nlu_done")
	turn.Set("normalized", norm.Canonical)
	turn.Set("mode", mode)

	if in != nil {
		turn.Set("route", "cmd")
		turn.Set("intent", in.Name)
		turn.Set("slots", in.Slots)
		p.prev, p.prevAt = in, metrics.Now()
		res := p.State.Execute(in, time.Now(), mode)
		if in.Name == "app.open" || in.Name == "app.close" {
			// the container can't touch the Mac: the host bridge runs `open -a` / quit (whitelisted apps)
			p.Out.Event("app", map[string]any{"action": strings.TrimPrefix(in.Name, "app."), "app": res.Slots["mac_app"]})
		}
		disp := reply.Display(res.TemplateID, mode, res.Slots)
		turn.Set("reply", disp)
		p.Out.Status(fmt.Sprintf("you: %s\nedgevoice [%s/%s]: %s", text, mode, in.Name, disp))
		p.say(turn, reply.Fragments(res.TemplateID, mode, res.Slots), p.Cfg.Reply.Clips)
		return
	}
	turn.Set("route", "llm")
	turn.Set("intent", "llm")
	if !p.llmOK() {
		turn.Set("reply", reply.Display("quick_only", mode, nil))
		p.Out.Status(fmt.Sprintf("you: %s\nedgevoice: %s", text, reply.Display("quick_only", mode, nil)))
		p.say(turn, reply.Fragments("quick_only", mode, nil), p.Cfg.Reply.Clips)
		return
	}
	// The small LLM only understands English: a Tanglish question is rewritten into plain English
	// ("indhiyaavooda keepattal enna adhu" → "what is the capital of india?").
	query := text
	if best.Source == "ta" && p.Cfg.NLU.Gate {
		lx := p.Norm.Lex
		query = lx.ToEnglishQuery(nlu.QueryNormalizer(lx).Normalize(iface.Transcript{Text: best.Text}))
	}
	turn.Set("llm_query", query)
	p.chat(ctx, turn, text, query, mode)
}

func (p *Pipeline) candidate(src, text string) nlu.Candidate {
	n := p.Norm.Normalize(iface.Transcript{Text: text})
	c := nlu.Candidate{Source: src, Text: text, Norm: n}
	if p.Cfg.NLU.FastPath && strings.TrimSpace(text) != "" {
		c.Intent = p.Parser.Parse(n)
	}
	return c
}

// say renders fragments from clips (or live TTS on any miss) and plays them.
func (p *Pipeline) say(turn *metrics.Turn, frags []reply.Fragment, useClips bool) {
	var pcm []int16
	ok := false
	if useClips && p.Clips != nil {
		pcm, ok = p.Clips.Render(frags)
	}
	turn.Set("clips", ok)
	if !ok {
		var parts [][]int16
		for _, f := range frags {
			parts = append(parts, p.tts(f.Voice).Synth(f.Text))
		}
		pcm = audio.Join(parts, 40)
	}
	turn.Mark("t_tts_first_chunk")
	p.play(turn, pcm)
}

func (p *Pipeline) tts(voice string) *sherpa.TTS {
	if voice == "ta" && p.TTSTa != nil {
		return p.TTSTa
	}
	return p.TTSEn
}

func (p *Pipeline) play(turn *metrics.Turn, pcm []int16) {
	if len(pcm) == 0 {
		return
	}
	turn.Mark("t_first_audio_out") // first reply PCM leaves the container (DESIGN #10)
	p.output(pcm)
}

// output sends audio to the speaker and keeps the mic muted (half-duplex) while it plays.
func (p *Pipeline) output(pcm []int16) {
	defer func() { p.Out.Event("state", map[string]any{"state": "speaking"}) }()
	p.Out.PCM(pcm)
	d := time.Duration(len(pcm)) * time.Second / audio.SampleRate
	p.mu.Lock()
	if now := metrics.Now(); p.busyUntil < now {
		p.busyUntil = now
	}
	p.busyUntil += d + 300*time.Millisecond
	p.awakeUntil = p.busyUntil + time.Duration(p.Cfg.Wake.WindowS)*time.Second
	p.mu.Unlock()
}

// chat streams the LLM reply clause by clause into TTS (O6). LLM output is romanized, so the English
// voice speaks it in both modes.
func (p *Pipeline) chat(ctx context.Context, turn *metrics.Turn, text, query, mode string) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.Cfg.LLM.TimeoutMs)*time.Millisecond*4)
	defer cancel()
	c := *p.LLM
	toks, err := c.Stream(ctx, []iface.Message{{Role: "user", Content: query}}, p.Cfg.LLM.MaxTokens)
	if err != nil {
		log.Printf("llm: %v", err)
		p.say(turn, reply.Fragments("quick_only", mode, nil), p.Cfg.Reply.Clips)
		return
	}
	marked := make(chan string, 32)
	go func() {
		defer close(marked)
		first := true
		for t := range toks {
			if first {
				turn.Mark("t_llm_first_token")
				first = false
			}
			marked <- t
		}
	}()
	var full []string
	for cl := range llm.Clauses(marked, p.Cfg.LLM.ClauseTTS) {
		// Echo guard: a tiny LLM that doesn't understand often repeats the question back. Never speak that.
		// compare with what the user actually said (Tanglish), not our English rewrite, whose words a
		// correct answer naturally reuses ("who is prime minister?" → "The Prime Minister of India is …")
		// judge only complete sentences: a 4-word first chunk ("The Prime Minister of") always overlaps
		if p.Cfg.NLU.Gate && strings.ContainsAny(cl, ".?!") && llm.IsEcho(cl, text) {
			turn.Set("echo_dropped", true)
			continue
		}
		if p.Cfg.NLU.Gate && llm.IsHedge(cl) { // filler only; if nothing else comes, we say "not sure" ourselves
			continue
		}
		if len(full) == 0 {
			turn.Mark("t_first_clause")
			p.Out.Status(fmt.Sprintf("you: %s\nedgevoice [%s/llm]: ...", text, mode))
		}
		full = append(full, cl)
		pcm := p.TTSEn.Synth(cl)
		turn.Mark("t_tts_first_chunk")
		p.play(turn, pcm)
	}
	if len(full) == 0 { // nothing usable came back: say so honestly
		disp := reply.Display("dont_know", mode, nil)
		turn.Set("reply", disp)
		turn.Set("route", "unclear")
		p.Out.Status(fmt.Sprintf("you: %s\nedgevoice: %s", text, disp))
		p.say(turn, reply.Fragments("dont_know", mode, nil), p.Cfg.Reply.Clips)
		return
	}
	turn.Set("reply", strings.Join(full, " "))
	p.Out.Status(fmt.Sprintf("you: %s\nedgevoice [%s/llm]: %s", text, mode, strings.Join(full, " ")))
}

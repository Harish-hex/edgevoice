package audio

import "time"

// Endpointer decides when the user's turn has ended (PRD §7.1).
// Fed every frame with the current VAD speech flag; CompleteFn (O3) reports whether the partial
// transcript already parses as a complete command.
type Endpointer struct {
	Silence      time.Duration // e.g. 400ms (baseline 800ms)
	EarlySilence time.Duration // e.g. 200ms when CompleteFn(partial) is true
	MaxUtterance time.Duration // 10s
	CompleteFn   func(partial string) bool
	// ExtendFn may require a longer silence for this partial (e.g. 3.5 s after a bare wake phrase,
	// so "Hey Computer … <pause> … what time is it" stays one turn). 0 = default.
	ExtendFn func(partial string) time.Duration

	inSpeech            bool
	started, lastVoiced time.Duration
	haveSpeech          bool
}

func (e *Endpointer) Reset() {
	*e = Endpointer{Silence: e.Silence, EarlySilence: e.EarlySilence, MaxUtterance: e.MaxUtterance, CompleteFn: e.CompleteFn, ExtendFn: e.ExtendFn}
}

// LastVoiced is the end of the user's actual speech (t_last_voiced_frame).
func (e *Endpointer) LastVoiced() time.Duration { return e.lastVoiced }
func (e *Endpointer) Started() bool             { return e.haveSpeech }

// Update returns true when the turn has ended. now = end time of the frame just processed.
func (e *Endpointer) Update(speech bool, partial string, now time.Duration) bool {
	if speech {
		if !e.haveSpeech {
			e.started, e.haveSpeech = now, true
		}
		e.lastVoiced = now
		return e.MaxUtterance > 0 && now-e.started >= e.MaxUtterance
	}
	if !e.haveSpeech {
		return false
	}
	gap := now - e.lastVoiced
	if e.EarlySilence > 0 && gap >= e.EarlySilence && e.CompleteFn != nil && e.CompleteFn(partial) {
		return true
	}
	need := e.Silence
	if e.ExtendFn != nil {
		need = max(need, e.ExtendFn(partial))
	}
	return gap >= need
}

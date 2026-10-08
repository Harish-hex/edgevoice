//go:build linux

package pipeline

import (
	"context"
	"math"
	"time"

	"edgevoice/internal/actions"
	"edgevoice/internal/audio"
	"edgevoice/internal/reply"
)

// RunScheduler rings alarms, timers and reminders when they come due: a beep pattern followed by a
// spoken announcement (cached clips, or live TTS for free text), plus a dashboard/host notification.
func (p *Pipeline) RunScheduler(ctx context.Context) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for _, it := range p.State.Due(now) {
				p.ring(it)
			}
		}
	}
}

func (p *Pipeline) ring(it actions.Item) {
	tpl := map[string]string{"alarm": "alarm.ring", "timer": "timer.done", "reminder": "reminder.ring"}[it.Kind]
	frags := reply.Fragments(tpl, it.Mode, it.Slots)
	speech, ok := p.Clips.Render(frags)
	if !ok {
		var parts [][]int16
		for _, f := range frags {
			parts = append(parts, p.tts(f.Voice).Synth(f.Text))
		}
		speech = audio.Join(parts, 40)
	}
	beeps := Beeps(3)
	pcm := append(append(append(append([]int16{}, beeps...), make([]int16, audio.SampleRate/5)...), speech...), make([]int16, audio.SampleRate/3)...)
	if it.Kind == "alarm" { // alarms repeat once
		pcm = append(pcm, append(beeps, speech...)...)
	}
	disp := reply.Display(tpl, it.Mode, it.Slots)
	p.Out.Status("edgevoice: " + disp)
	p.Out.Event("fired", map[string]any{"type": it.Kind, "label": it.Label, "text": disp})
	p.output(pcm)
}

// Beeps returns n two-tone beeps (880/1320 Hz, 180 ms each, 120 ms gaps) with soft edges.
func Beeps(n int) []int16 {
	var out []int16
	for i := 0; i < n; i++ {
		for _, f := range []float64{880, 1320} {
			m := audio.SampleRate * 180 / 1000
			for s := 0; s < m; s++ {
				env := math.Min(1, math.Min(float64(s), float64(m-s))/160)
				out = append(out, int16(9000*env*math.Sin(2*math.Pi*f*float64(s)/audio.SampleRate)))
			}
		}
		out = append(out, make([]int16, audio.SampleRate*120/1000)...)
	}
	return out
}

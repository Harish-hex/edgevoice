// Package actions executes parsed intents against in-memory state and returns the reply template + slots.
package actions

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"edgevoice/internal/iface"
)

type Result struct {
	TemplateID string
	Slots      map[string]string
}

// Item is a scheduled alarm, timer or reminder. Mode remembers the language it was set in, so it
// rings in the same register.
type Item struct {
	Kind  string            `json:"kind"` // alarm | timer | reminder
	At    time.Time         `json:"at"`
	Mode  string            `json:"mode"`
	Slots map[string]string `json:"slots"` // reply slots for the ring announcement
	Label string            `json:"label"`
}

type State struct {
	mu    sync.Mutex
	Items []Item
}

// Due removes and returns every item whose time has come.
func (st *State) Due(now time.Time) []Item {
	st.mu.Lock()
	defer st.mu.Unlock()
	var due, keep []Item
	for _, it := range st.Items {
		if !it.At.After(now) {
			due = append(due, it)
		} else {
			keep = append(keep, it)
		}
	}
	st.Items = keep
	return due
}

// Scheduled returns a copy of pending items (dashboard).
func (st *State) Scheduled() []Item {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]Item(nil), st.Items...)
}

func (st *State) drop(kind string) int {
	n := 0
	var keep []Item
	for _, it := range st.Items {
		if it.Kind == kind {
			n++
			continue
		}
		keep = append(keep, it)
	}
	st.Items = keep
	return n
}

// absTime turns the parser's "HH:MM" + day into an absolute future time.
func absTime(hhmm, day string, now time.Time) (time.Time, bool) {
	t, err := time.ParseInLocation("15:04", hhmm, now.Location())
	if err != nil {
		return time.Time{}, false
	}
	at := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	if day == "tomorrow" {
		at = at.AddDate(0, 0, 1)
	}
	if !at.After(now) {
		at = at.AddDate(0, 0, 1)
	}
	return at, true
}

// period maps a 24h hour to the spoken period key used by reply tables.
func dayWord(t, now time.Time) string {
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return "today"
	}
	return "tomorrow"
}

func period(h int) string {
	switch {
	case h < 4:
		return "night"
	case h < 12:
		return "am"
	case h < 16:
		return "noon"
	case h < 19:
		return "pm"
	default:
		return "night"
	}
}

func clockSlots(t time.Time) map[string]string {
	h := t.Hour() % 12
	if h == 0 {
		h = 12
	}
	return map[string]string{"hour": strconv.Itoa(h), "minute": strconv.Itoa(t.Minute()), "period": period(t.Hour())}
}

// Execute runs an intent. mode is the reply language, stored on scheduled items.
func (st *State) Execute(in *iface.Intent, now time.Time, mode string) Result {
	st.mu.Lock()
	defer st.mu.Unlock()
	switch in.Name {
	case "alarm.set", "reminder.set":
		at, ok := absTime(in.Slots["time"], in.Slots["day"], now)
		if !ok {
			return Result{TemplateID: "error"}
		}
		s := clockSlots(at)
		s["day"] = dayWord(at, now)
		s["text"] = in.Slots["text"]
		kind, label := "alarm", "Alarm "+at.Format("Mon 15:04")
		if in.Name == "reminder.set" {
			kind, label = "reminder", in.Slots["text"]+" · "+at.Format("Mon 15:04")
		}
		st.Items = append(st.Items, Item{Kind: kind, At: at, Mode: mode, Slots: s, Label: label})
		return Result{in.Name, s}
	case "alarm.cancel":
		if st.drop("alarm") == 0 {
			return Result{TemplateID: "alarm.cancel.none"}
		}
		return Result{TemplateID: "alarm.cancel"}
	case "timer.cancel":
		if st.drop("timer") == 0 {
			return Result{TemplateID: "timer.cancel.none"}
		}
		return Result{TemplateID: "timer.cancel"}
	case "app.open", "app.close":
		return Result{in.Name, map[string]string{"app": in.Slots["app"], "mac_app": in.Slots["mac_app"]}}
	case "timer.set":
		d := in.Slots["duration"]
		unit := map[byte]string{'h': "hour", 'm': "minute", 's': "second"}[d[len(d)-1]]
		n := strings.TrimRight(d, "mhs")
		s := map[string]string{"amount": n, "unit": unit}
		if dur, err := time.ParseDuration(d); err == nil {
			st.Items = append(st.Items, Item{Kind: "timer", At: now.Add(dur), Mode: mode, Slots: s, Label: n + " " + unit + " timer"})
		}
		return Result{"timer.set", s}
	case "clock.time":
		return Result{"clock.time", clockSlots(now)}
	case "clock.date":
		day := in.Slots["day"]
		off, ok := map[string]int{"today": 0, "tomorrow": 1, "yesterday": -1, "day_after_tomorrow": 2, "day_before_yesterday": -2}[day]
		if !ok {
			day = "today"
		}
		d := now.AddDate(0, 0, off)
		return Result{"clock.date", map[string]string{"day": day, "weekday": d.Weekday().String(), "month": d.Month().String(), "dom": strconv.Itoa(d.Day())}}
	case "calc":
		return calc(in.Slots["expression"])
	case "system.stop", "smalltalk.greet", "smalltalk.identity", "offline.unsupported":
		return Result{TemplateID: in.Name}
	}
	return Result{TemplateID: "error"}
}

func calc(expr string) Result {
	for _, op := range []string{"*", "+", "-", "/"} {
		a, b, ok := strings.Cut(expr, op)
		if !ok {
			continue
		}
		x, e1 := strconv.Atoi(a)
		y, e2 := strconv.Atoi(b)
		if e1 != nil || e2 != nil {
			break
		}
		var r string
		switch op {
		case "*":
			r = strconv.Itoa(x * y)
		case "+":
			r = strconv.Itoa(x + y)
		case "-":
			r = strconv.Itoa(x - y)
		case "/":
			if y == 0 {
				return Result{TemplateID: "calc.error"}
			}
			r = strings.TrimSuffix(fmt.Sprintf("%.1f", float64(x)/float64(y)), ".0")
		}
		return Result{"calc", map[string]string{"a": a, "op": op, "b": b, "result": r}}
	}
	return Result{TemplateID: "calc.error"}
}

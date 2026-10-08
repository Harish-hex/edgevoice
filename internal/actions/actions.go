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

type State struct {
	mu     sync.Mutex
	Alarms []time.Time
	Timers []time.Time
}

// period maps a 24h hour to the spoken period key used by reply tables.
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

func (st *State) Execute(in *iface.Intent, now time.Time) Result {
	st.mu.Lock()
	defer st.mu.Unlock()
	switch in.Name {
	case "alarm.set", "reminder.set":
		t, err := time.ParseInLocation("15:04", in.Slots["time"], now.Location())
		if err != nil {
			return Result{TemplateID: "error"}
		}
		s := clockSlots(t)
		s["day"] = in.Slots["day"]
		s["text"] = in.Slots["text"]
		if in.Name == "alarm.set" {
			st.Alarms = append(st.Alarms, t)
		}
		return Result{in.Name, s}
	case "alarm.cancel":
		if len(st.Alarms) == 0 {
			return Result{TemplateID: "alarm.cancel.none"}
		}
		st.Alarms = nil
		return Result{TemplateID: "alarm.cancel"}
	case "timer.set":
		d := in.Slots["duration"]
		unit := "minute"
		if strings.HasSuffix(d, "h") {
			unit = "hour"
		}
		n := strings.TrimRight(d, "mh")
		if dur, err := time.ParseDuration(d); err == nil {
			st.Timers = append(st.Timers, now.Add(dur))
		}
		return Result{"timer.set", map[string]string{"amount": n, "unit": unit}}
	case "clock.time":
		return Result{"clock.time", clockSlots(now)}
	case "clock.date":
		return Result{"clock.date", map[string]string{"weekday": now.Weekday().String(), "month": now.Month().String(), "dom": strconv.Itoa(now.Day())}}
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

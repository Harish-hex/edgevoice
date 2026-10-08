package actions

import (
	"testing"
	"time"

	"edgevoice/internal/iface"
)

func TestExecute(t *testing.T) {
	now := time.Date(2026, 10, 8, 20, 5, 0, 0, time.Local)
	st := &State{}
	cases := []struct {
		in   iface.Intent
		tpl  string
		want map[string]string
	}{
		{iface.Intent{Name: "alarm.set", Slots: map[string]string{"time": "06:00", "day": "tomorrow"}}, "alarm.set", map[string]string{"hour": "6", "period": "am", "day": "tomorrow"}},
		{iface.Intent{Name: "alarm.set", Slots: map[string]string{"time": "22:30", "day": "today"}}, "alarm.set", map[string]string{"hour": "10", "minute": "30", "period": "night"}},
		{iface.Intent{Name: "alarm.cancel"}, "alarm.cancel", nil},
		{iface.Intent{Name: "alarm.cancel"}, "alarm.cancel.none", nil},
		{iface.Intent{Name: "timer.set", Slots: map[string]string{"duration": "5m"}}, "timer.set", map[string]string{"amount": "5", "unit": "minute"}},
		{iface.Intent{Name: "clock.time"}, "clock.time", map[string]string{"hour": "8", "minute": "5", "period": "night"}},
		{iface.Intent{Name: "calc", Slots: map[string]string{"expression": "12*8"}}, "calc", map[string]string{"result": "96"}},
		{iface.Intent{Name: "clock.date", Slots: map[string]string{"day": "tomorrow"}}, "clock.date", map[string]string{"day": "tomorrow", "weekday": "Friday", "dom": "9"}},
		{iface.Intent{Name: "calc", Slots: map[string]string{"expression": "7/0"}}, "calc.error", nil},
	}
	for _, c := range cases {
		in := c.in
		r := st.Execute(&in, now, "ENGLISH")
		if r.TemplateID != c.tpl {
			t.Errorf("%s: template %s want %s", c.in.Name, r.TemplateID, c.tpl)
		}
		for k, v := range c.want {
			if r.Slots[k] != v {
				t.Errorf("%s: slot %s=%q want %q", c.in.Name, k, r.Slots[k], v)
			}
		}
	}
}

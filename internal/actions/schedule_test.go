package actions

import (
	"testing"
	"time"

	"edgevoice/internal/iface"
)

func TestScheduleAndDue(t *testing.T) {
	now := time.Date(2026, 10, 8, 20, 0, 0, 0, time.Local)
	st := &State{}
	st.Execute(&iface.Intent{Name: "timer.set", Slots: map[string]string{"duration": "5m"}}, now, "TANGLISH")
	st.Execute(&iface.Intent{Name: "alarm.set", Slots: map[string]string{"time": "06:00", "day": "tomorrow"}}, now, "ENGLISH")
	st.Execute(&iface.Intent{Name: "alarm.set", Slots: map[string]string{"time": "06:00"}}, now, "ENGLISH") // next occurrence
	if n := len(st.Scheduled()); n != 3 {
		t.Fatalf("scheduled %d", n)
	}
	if d := st.Due(now.Add(4 * time.Minute)); len(d) != 0 {
		t.Fatal("nothing due yet")
	}
	d := st.Due(now.Add(5 * time.Minute))
	if len(d) != 1 || d[0].Kind != "timer" || d[0].Mode != "TANGLISH" || d[0].Slots["amount"] != "5" {
		t.Fatalf("timer due: %+v", d)
	}
	d = st.Due(time.Date(2026, 10, 9, 6, 0, 0, 0, time.Local))
	if len(d) != 2 || d[0].Kind != "alarm" || d[0].Slots["hour"] != "6" {
		t.Fatalf("alarms due: %+v", d)
	}
	st.Execute(&iface.Intent{Name: "timer.set", Slots: map[string]string{"duration": "1m"}}, now, "ENGLISH")
	if r := st.Execute(&iface.Intent{Name: "timer.cancel"}, now, "ENGLISH"); r.TemplateID != "timer.cancel" || len(st.Scheduled()) != 0 {
		t.Fatalf("cancel: %+v %d", r, len(st.Scheduled()))
	}
}

package nlu

import (
	"testing"

	"edgevoice/internal/iface"
)

func TestFollowUp(t *testing.T) {
	prev := &iface.Intent{Name: "clock.date", Slots: map[string]string{"day": "today"}}
	if got := FollowUp(prev, norm("innaikku illa naalaikku")); got == nil || got.Slots["day"] != "tomorrow" {
		t.Fatalf("got %+v", got)
	}
	if got := FollowUp(prev, norm("no tomorrow")); got == nil || got.Slots["day"] != "tomorrow" {
		t.Fatalf("en got %+v", got)
	}
	if FollowUp(&iface.Intent{Name: "clock.time"}, norm("naalaikku")) != nil || FollowUp(prev, norm("oru joke sollu")) != nil {
		t.Fatal("should not follow up")
	}
}

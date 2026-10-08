package nlu

import "testing"

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0}, {"abc", "", 3}, {"naliki", "naalaikku", 4}, {"pannu", "panna", 1}, {"alarm", "alaaram", 2},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("levenshtein(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestPhoneticKey(t *testing.T) {
	pairs := [][2]string{{"naliki", "naalaikku"}, {"vedhar", "weather"}, {"alaaram", "alarm"}}
	for _, p := range pairs {
		if phoneticKey(p[0]) != phoneticKey(p[1]) {
			t.Errorf("phoneticKey(%q)=%q != phoneticKey(%q)=%q", p[0], phoneticKey(p[0]), p[1], phoneticKey(p[1]))
		}
	}
}

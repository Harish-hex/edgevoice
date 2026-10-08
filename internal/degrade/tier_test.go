package degrade

import "testing"

func TestSelect(t *testing.T) {
	cases := []struct {
		cores float64
		mem   int64
		p95   float64
		want  string
	}{
		{2, 2048 * MB, 500, "T0"}, {0, 0, 0, "T0"}, {2, 1536 * MB, 500, "T1"}, {2, 2048 * MB, 2000, "T1"},
		{1, 2048 * MB, 500, "T2"}, {2, 1024 * MB, 500, "T2"}, {2, 768 * MB, 500, "T3"},
	}
	for _, c := range cases {
		if got := Select(c.cores, c.mem, c.p95); got != c.want {
			t.Errorf("Select(%v,%v,%v)=%s want %s", c.cores, c.mem/MB, c.p95, got, c.want)
		}
	}
	if P95([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) != 10 || P95([]float64{5}) != 5 {
		t.Error("p95")
	}
}

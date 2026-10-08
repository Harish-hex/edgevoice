package metrics

import "testing"

func TestCgroupParse(t *testing.T) {
	c, m := Limits("testdata")
	if c != 2 || m != 2147483648 {
		t.Fatalf("limits %v %v", c, m)
	}
	if u, err := CPUUsec("testdata"); err != nil || u != 123456 {
		t.Fatalf("cpu %v %v", u, err)
	}
}

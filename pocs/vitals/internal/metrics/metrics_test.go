package metrics

import (
	"math"
	"testing"
)

func TestLookupNamesAndAliases(t *testing.T) {
	for _, name := range []string{"sleep", "SLEEP", " hr ", "rhr", "meds"} {
		if _, ok := Lookup(name); !ok {
			t.Errorf("Lookup(%q) not found", name)
		}
	}
	if m, _ := Lookup("hr"); m.Name != "heartrate" {
		t.Errorf("hr alias resolved to %q", m.Name)
	}
	if _, ok := Lookup("blood"); ok {
		t.Error("unknown metric should not resolve")
	}
}

func TestValidate(t *testing.T) {
	sleep, _ := Lookup("sleep")
	mood, _ := Lookup("mood")
	cases := []struct {
		m    Metric
		v    float64
		want bool
	}{
		{sleep, 7.5, true},
		{sleep, 0, true},
		{sleep, 25, false},
		{sleep, -1, false},
		{sleep, math.NaN(), false},
		{sleep, math.Inf(1), false},
		{mood, 3, true},
		{mood, 3.5, false},
		{mood, 0, false},
	}
	for _, c := range cases {
		err := c.m.Validate(c.v)
		if (err == nil) != c.want {
			t.Errorf("%s.Validate(%v) err=%v, want ok=%v", c.m.Name, c.v, err, c.want)
		}
	}
}

func TestFormat(t *testing.T) {
	sleep, _ := Lookup("sleep")
	mood, _ := Lookup("mood")
	steps, _ := Lookup("steps")
	if got := sleep.FormatWithUnit(7.5); got != "7.5 h" {
		t.Errorf("sleep = %q", got)
	}
	if got := sleep.FormatWithUnit(7.25); got != "7.25 h" {
		t.Errorf("sleep = %q", got)
	}
	if got := mood.FormatWithUnit(4); got != "4/5" {
		t.Errorf("mood = %q", got)
	}
	if got := steps.FormatWithUnit(8000); got != "8000 steps" {
		t.Errorf("steps = %q", got)
	}
	if got := mood.FormatStatWithUnit(3.84); got != "3.8/5" {
		t.Errorf("mood mean = %q", got)
	}
	if got := mood.FormatStat(-0.3); got != "-0.3" {
		t.Errorf("mood delta = %q", got)
	}
	if got := mood.FormatStat(4.0); got != "4" {
		t.Errorf("whole mean = %q", got)
	}
	if got := steps.FormatStatWithUnit(7692.3); got != "7692 steps" {
		t.Errorf("steps mean = %q", got)
	}
}

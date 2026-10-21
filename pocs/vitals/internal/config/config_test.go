package config

import "testing"

func TestParseGoal(t *testing.T) {
	cases := []struct {
		in, def string
		want    Goal
		ok      bool
	}{
		{"8000", ">=", Goal{">=", 8000}, true},
		{"<=75", ">=", Goal{"<=", 75}, true},
		{">= 7.5", "", Goal{">=", 7.5}, true},
		{"75", "", Goal{}, false},
		{"lots", ">=", Goal{}, false},
	}
	for _, c := range cases {
		got, err := ParseGoal(c.in, c.def)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("ParseGoal(%q,%q) = %+v, %v", c.in, c.def, got, err)
		}
	}
}

func TestGoalMet(t *testing.T) {
	if !(Goal{">=", 8000}).Met(8000) || (Goal{">=", 8000}).Met(7999) {
		t.Error(">= goal wrong")
	}
	if !(Goal{"<=", 75}).Met(74) || (Goal{"<=", 75}).Met(76) {
		t.Error("<= goal wrong")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(dir)
	if err != nil || len(c.Goals) != 0 {
		t.Fatalf("empty load: %+v %v", c, err)
	}
	c.Goals = map[string]Goal{"steps": {">=", 8000}}
	if err := Save(dir, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || got.Goals["steps"] != (Goal{">=", 8000}) {
		t.Fatalf("round trip: %+v %v", got, err)
	}
}

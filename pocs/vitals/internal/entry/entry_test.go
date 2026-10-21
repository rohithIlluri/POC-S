package entry

import (
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	at := time.Now()
	cases := []struct {
		metric, label, value, note string
		wantErr                    string
		want                       float64
	}{
		{"sleep", "", "7.5", "", "", 7.5},
		{"hr", "", "58", "", "", 58},
		{"symptom", "headache", "", "", "", 3},
		{"med", "ibuprofen", "2", "", "", 2},
		{"sleep", "", "", "", "needs a value", 0},
		{"symptom", "", "4", "", "needs a name", 0},
		{"blood", "", "1", "", "unknown metric", 0},
		{"mood", "", "x", "", "not a number", 0},
		{"mood", "", "7", "", "out of range", 0},
		{"sleep", "", "7", "a\x07b", "control characters", 0},
		{"sleep", "", "7", strings.Repeat("x", 201), "longer than", 0},
	}
	for _, c := range cases {
		e, _, err := New(c.metric, c.label, c.value, c.note, at)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("New(%q,%q,%q) err = %v, want %q", c.metric, c.label, c.value, err, c.wantErr)
			}
			continue
		}
		if err != nil || e.Value != c.want || e.ID == "" {
			t.Errorf("New(%q,%q,%q) = %+v, %v", c.metric, c.label, c.value, e, err)
		}
	}
}

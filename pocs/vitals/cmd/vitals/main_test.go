package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
)

type harness struct {
	t   *testing.T
	app *app
	now time.Time
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, now: time.Date(2026, 10, 21, 20, 0, 0, 0, time.UTC)}
	h.app = &app{home: t.TempDir(), now: func() time.Time { return h.now }, loc: time.UTC}
	return h
}

// run executes a command line and returns stdout, stderr and the exit code.
func (h *harness) run(line string) (string, string, int) {
	var out, errb bytes.Buffer
	h.app.out, h.app.err = &out, &errb
	code := h.app.run(strings.Fields(line))
	return out.String(), errb.String(), code
}

func (h *harness) ok(line string) string {
	h.t.Helper()
	out, errOut, code := h.run(line)
	if code != 0 {
		h.t.Fatalf("%q exited %d: %s", line, code, errOut)
	}
	return out
}

func (h *harness) fails(line, wantErr string) {
	h.t.Helper()
	_, errOut, code := h.run(line)
	if code == 0 || !strings.Contains(errOut, wantErr) {
		h.t.Fatalf("%q: code=%d stderr=%q, want error containing %q", line, code, errOut, wantErr)
	}
}

func TestLogAndToday(t *testing.T) {
	h := newHarness(t)
	if out := h.ok("log sleep 7.5 --note rested"); !strings.Contains(out, "logged sleep 7.5 h") {
		t.Errorf("log output: %q", out)
	}
	h.ok("log symptom headache 4")
	h.ok("log med ibuprofen")
	h.ok("log --at yesterday mood 2")

	out := h.ok("today")
	for _, want := range []string{"sleep", "7.5 h", `"rested"`, "headache (4/5)", "ibuprofen"} {
		if !strings.Contains(out, want) {
			t.Errorf("today missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "mood") {
		t.Errorf("yesterday's entry leaked into today:\n%s", out)
	}
	if !strings.Contains(out, "2-day streak") {
		t.Errorf("streak missing:\n%s", out)
	}
	// bare `vitals` is `vitals today`
	if bare := h.ok(""); bare != out {
		t.Errorf("bare command differs from today")
	}
}

func TestLogValidation(t *testing.T) {
	h := newHarness(t)
	h.fails("log", "usage")
	h.fails("log blood 5", "unknown metric")
	h.fails("log sleep", "usage")
	h.fails("log sleep lots", "not a number")
	h.fails("log sleep 30", "out of range")
	h.fails("log mood 3.5", "whole number")
	h.fails("log symptom", "usage")
	h.fails("log symptom headache 9", "out of range")
	h.fails("log sleep 7 --at tomorrowish", "can't read --at")
	h.fails("log sleep 7 --at 2026-12-01", "future")
	h.fails("bogus", "unknown command")

	var out, errb bytes.Buffer
	h.app.out, h.app.err = &out, &errb
	if code := h.app.run([]string{"log", "sleep", "7", "--note", "bad\x1b[2Jnote"}); code == 0 || !strings.Contains(errb.String(), "control characters") {
		t.Errorf("control chars accepted: %d %q", code, errb.String())
	}
}

func TestBackdateForms(t *testing.T) {
	h := newHarness(t)
	h.ok("log sleep 7 --at 06:45")
	h.ok("log sleep 6 --at 2026-10-19")
	h.ok("log sleep 8 --at 2026-10-18T07:30")
	s, _ := store.Open(h.app.home)
	got, _, _ := s.Load()
	want := []string{"2026-10-18T07:30", "2026-10-19T12:00", "2026-10-21T06:45"}
	for i, e := range got {
		if e.Time.UTC().Format("2006-01-02T15:04") != want[i] {
			t.Errorf("entry %d at %v, want %s", i, e.Time, want[i])
		}
	}
}

func TestUndoRemovesLastWrittenEvenIfBackdated(t *testing.T) {
	h := newHarness(t)
	h.fails("undo", "nothing to undo")
	h.ok("log sleep 7")
	h.ok("log mood 4 --at 2026-10-15")
	if out := h.ok("undo"); !strings.Contains(out, "removed mood 4/5") {
		t.Errorf("undo output: %q", out)
	}
	if out := h.ok("list --days 30"); strings.Contains(out, "mood") || !strings.Contains(out, "sleep") {
		t.Errorf("after undo:\n%s", out)
	}
}

func TestListAndDelete(t *testing.T) {
	h := newHarness(t)
	h.ok("log steps 9000")
	h.ok("log water 2")
	out := h.ok("list --metric steps")
	if !strings.Contains(out, "9000 steps") || strings.Contains(out, "water") {
		t.Fatalf("filtered list:\n%s", out)
	}
	id := strings.Fields(strings.TrimSpace(out))[len(strings.Fields(strings.TrimSpace(out)))-1]
	if out := h.ok("delete " + id); !strings.Contains(out, "removed steps") {
		t.Errorf("delete output: %q", out)
	}
	h.fails("delete "+id, "no entry")
}

func TestTrends(t *testing.T) {
	h := newHarness(t)
	if out := h.ok("trends"); !strings.Contains(out, "no data") {
		t.Errorf("empty trends: %q", out)
	}
	for d := 13; d >= 0; d-- {
		h.now = time.Date(2026, 10, 21-d, 8, 0, 0, 0, time.UTC)
		v := "6"
		if d < 7 {
			v = "8"
		}
		h.ok("log sleep " + v)
	}
	h.ok("log symptom headache")
	h.now = time.Date(2026, 10, 21, 20, 0, 0, 0, time.UTC)
	out := h.ok("trends --days 14")
	for _, want := range []string{"sleep", "14", "7 h", "▲ +2", "▁▁▁▁▁▁▁███████", "symptom: headache ×1"} {
		if !strings.Contains(out, want) {
			t.Errorf("trends missing %q:\n%s", want, out)
		}
	}
	h.fails("trends --days 1", "between 2 and 366")
}

func TestGoalsFeedInsights(t *testing.T) {
	h := newHarness(t)
	if out := h.ok("goal"); !strings.Contains(out, "no goals") {
		t.Errorf("empty goals: %q", out)
	}
	h.ok("goal steps 8000")
	h.ok("goal weight <=75")
	h.fails("goal weight 75", "no natural direction")
	h.fails("goal symptom 2", "event")
	h.fails("goal sleep 30", "out of range")
	if out := h.ok("goal"); !strings.Contains(out, ">= 8000 steps") || !strings.Contains(out, "<= 75 kg") {
		t.Errorf("goals list: %q", out)
	}
	h.ok("log steps 9000")
	if out := h.ok("insights"); !strings.Contains(out, "steps goal (>= 8000 steps) met on 1 of 1") || !strings.Contains(out, "not medical advice") {
		t.Errorf("insights:\n%s", out)
	}
	h.ok("goal steps --clear")
	if out := h.ok("goal"); strings.Contains(out, "steps") {
		t.Errorf("goal not cleared: %q", out)
	}
}

func TestExport(t *testing.T) {
	h := newHarness(t)
	h.ok("log sleep 7.25")
	var out, errb bytes.Buffer
	h.app.out, h.app.err = &out, &errb
	if code := h.app.run([]string{"log", "symptom", "=HYPERLINK(evil)"}); code != 0 {
		t.Fatalf("log: %s", errb.String())
	}

	csvOut := h.ok("export")
	if !strings.HasPrefix(csvOut, "id,time,metric,value,unit,label,note\n") || !strings.Contains(csvOut, ",sleep,7.25,h,") {
		t.Errorf("csv:\n%s", csvOut)
	}
	if !strings.Contains(csvOut, "'=HYPERLINK(evil)") {
		t.Errorf("formula not defused:\n%s", csvOut)
	}

	var entries []store.Entry
	if err := json.Unmarshal([]byte(h.ok("export --format json")), &entries); err != nil || len(entries) != 2 {
		t.Errorf("json export: %v %d", err, len(entries))
	}
	h.fails("export --format xml", "unknown --format")
}

func TestHelpAndVersion(t *testing.T) {
	h := newHarness(t)
	if out := h.ok("help"); !strings.Contains(out, "vitals log <metric>") {
		t.Error("help missing usage")
	}
	if out := h.ok("version"); !strings.Contains(out, version) {
		t.Error("version missing")
	}
	if out := h.ok("metrics"); !strings.Contains(out, "heartrate (hr, rhr)") {
		t.Errorf("metrics: %q", out)
	}
}

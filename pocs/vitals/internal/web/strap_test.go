package web

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
)

func repeat(v string, n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = v
	}
	return strings.Join(parts, ",")
}

func savedEntries(t *testing.T, c *client) map[string]store.Entry {
	t.Helper()
	st, _ := store.Open(c.srv.Home)
	entries, _, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]store.Entry{}
	for _, e := range entries {
		out[e.Metric] = e
	}
	return out
}

func TestStrapRestingCheckWithRR(t *testing.T) {
	c := newClient(t)
	c.page()
	// 120 beats alternating 1000/1040 ms: ~58.8 bpm, RMSSD 40 ms.
	rr := make([]string, 120)
	for i := range rr {
		rr[i] = strconv.Itoa(1000 + (i%2)*40)
	}
	w := c.post("/strap/save", url.Values{"kind": {"resting"}, "hr": {repeat("59", 120)}, "rr": {strings.Join(rr, ",")}, "seconds": {"120"}})
	if w.Code != 303 {
		t.Fatalf("save = %d", w.Code)
	}
	got := savedEntries(t, c)
	if e := got["heartrate"]; e.Value != 59 || e.Source != StrapSource || e.Note != "resting check, 2 min" || !e.Time.Equal(now) {
		t.Errorf("heartrate = %+v", e)
	}
	if e := got["hrv"]; e.Value != 40 {
		t.Errorf("hrv = %+v", e)
	}
	if body := c.page(); !strings.Contains(body, "Saved from your strap: heartrate 59 bpm, hrv 40 ms") {
		t.Error("flash missing")
	}
}

func TestStrapRestingCheckWithoutRRSavesHeartRateOnly(t *testing.T) {
	c := newClient(t)
	c.page()
	c.post("/strap/save", url.Values{"kind": {"resting"}, "hr": {"0,0,61,60,62,95,60,61,60,59,61,60,60"}, "seconds": {"120"}})
	got := savedEntries(t, c)
	if got["heartrate"].Value != 60 {
		t.Errorf("heartrate = %+v", got["heartrate"])
	}
	if _, ok := got["hrv"]; ok {
		t.Error("HRV saved without RR intervals")
	}
}

func TestStrapWorkout(t *testing.T) {
	c := newClient(t)
	c.page()
	c.post("/strap/save", url.Values{"kind": {"workout"}, "hr": {"100,120,140,160,180,140,120,100,120,120"}, "seconds": {"1830"}})
	e := savedEntries(t, c)["exercise"]
	if e.Value != 31 || e.Note != "avg 130 bpm, max 180 bpm" || !e.Time.Equal(now.Add(-1830*time.Second)) {
		t.Errorf("exercise = %+v", e)
	}
}

func TestStrapRejectsBadSessions(t *testing.T) {
	cases := []struct {
		form url.Values
		want string
	}{
		{url.Values{"kind": {"resting"}, "hr": {"60,61"}, "seconds": {"120"}}, "too short"},
		{url.Values{"kind": {"workout"}, "hr": {repeat("120", 30)}, "seconds": {"30"}}, "shorter than a minute"},
		{url.Values{"kind": {"resting"}, "hr": {"60,abc"}, "seconds": {"120"}}, "not a number"},
		{url.Values{"kind": {"resting"}, "hr": {repeat("60", 20)}, "seconds": {"-5"}}, "session length"},
		{url.Values{"kind": {"resting"}, "hr": {repeat("60", 20)}, "seconds": {"99999"}}, "session length"},
		{url.Values{"kind": {"nap"}, "hr": {repeat("60", 20)}, "seconds": {"120"}}, "unknown session kind"},
	}
	for _, tc := range cases {
		c := newClient(t)
		c.page()
		c.post("/strap/save", tc.form)
		body := c.page()
		if !strings.Contains(body, "Strap session not saved") || !strings.Contains(body, tc.want) {
			t.Errorf("%v: expected error %q", tc.form, tc.want)
		}
		if len(savedEntries(t, c)) != 0 {
			t.Errorf("%v: something was saved", tc.form)
		}
	}
}

func TestStrapSaveNeedsCSRF(t *testing.T) {
	c := newClient(t)
	if w := c.do("POST", "/strap/save", url.Values{"kind": {"resting"}}, nil); w.Code != 403 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestPermissionsPolicyAllowsOnlyBluetooth(t *testing.T) {
	w := newClient(t).do("GET", "/", nil, nil)
	if pp := w.Header().Get("Permissions-Policy"); !strings.Contains(pp, "bluetooth=(self)") || !strings.Contains(pp, "camera=()") {
		t.Errorf("Permissions-Policy = %q", pp)
	}
}

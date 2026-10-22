package web

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

// jsonPost sends a JSON body with the page's CSRF token in a header.
func (c *client) jsonPost(path, body string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	return c.do("POST", path, nil, func(r *http.Request) {
		r.Body = http.NoBody
		r2 := httptest.NewRequest("POST", path, strings.NewReader(body))
		*r = *r2
		r.Host = "localhost:8737"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", c.csrf)
		if mutate != nil {
			mutate(r)
		}
	})
}

func sampleBody(n int, ago time.Duration) string {
	var parts []string
	base := now.Add(-ago).UnixMilli()
	for i := 0; i < n; i++ {
		parts = append(parts, `{"t":`+itoa(base+int64(i)*1000)+`,"hr":72,"rr":[830]}`)
	}
	return `{"samples":[` + strings.Join(parts, ",") + `]}`
}

func itoa(v int64) string { b, _ := json.Marshal(v); return string(b) }

func TestCSPAllowsFetchToOurOwnServer(t *testing.T) {
	w := newClient(t).do("GET", "/", nil, nil)
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'self'") {
		t.Fatalf("CSP %q blocks fetch(); the page could not reach /api or /strap/clock-command", csp)
	}
	if strings.Contains(csp, "connect-src *") || strings.Contains(csp, "http:") {
		t.Errorf("connect-src must stay same-origin only: %q", csp)
	}
}

func TestLiveSamplesAreStoredAndRead(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })

	w := c.jsonPost("/api/samples", sampleBody(30, time.Minute), nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"stored":30`) {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	from := now.Add(-time.Hour).UnixMilli()
	to := now.UnixMilli()
	r := c.do("GET", "/api/samples?from="+itoa(from)+"&to="+itoa(to)+"&points=10", nil, nil)
	var series struct {
		T  []int64   `json:"t"`
		HR []float64 `json:"hr"`
	}
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &series) != nil || len(series.HR) != 1 || series.HR[0] != 72 {
		t.Fatalf("series = %d %s", r.Code, r.Body)
	}

	st := c.do("GET", "/api/status", nil, nil)
	var o struct {
		DayZero   string           `json:"day_zero"`
		Readiness []map[string]any `json:"readiness"`
	}
	if st.Code != 200 || json.Unmarshal(st.Body.Bytes(), &o) != nil || o.DayZero != "2026-10-21" || len(o.Readiness) == 0 {
		t.Fatalf("status = %d %s", st.Code, st.Body)
	}
}

func TestSampleUploadIsGuardedAndValidated(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })

	if w := c.jsonPost("/api/samples", sampleBody(1, time.Minute), func(r *http.Request) { r.Header.Set("X-CSRF-Token", "nope") }); w.Code != http.StatusForbidden {
		t.Errorf("wrong token = %d", w.Code)
	}
	if w := c.jsonPost("/api/samples", sampleBody(1, time.Minute), func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin = %d", w.Code)
	}
	if w := c.jsonPost("/api/samples", sampleBody(1, time.Minute), func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("non-JSON = %d", w.Code)
	}
	if w := c.jsonPost("/api/samples", `{"samples":[]}`, nil); w.Code != http.StatusBadRequest {
		t.Errorf("empty = %d", w.Code)
	}
	if w := c.jsonPost("/api/samples", sampleBody(1, 48*time.Hour), nil); w.Code != http.StatusBadRequest {
		t.Errorf("two-day-old sample = %d (a sample the strap stamped long ago must not slip in)", w.Code)
	}
	if w := c.jsonPost("/api/samples", `{"samples":[{"t":`+itoa(now.UnixMilli())+`,"hr":0,"rr":[]}]}`, nil); w.Code != http.StatusBadRequest {
		t.Errorf("hr 0 = %d", w.Code)
	}
	if w := c.jsonPost("/api/samples", sampleBody(5001, time.Minute), nil); w.Code != http.StatusBadRequest {
		t.Errorf("5001 samples = %d", w.Code)
	}
	// Nothing was stored by any of the above.
	st := c.do("GET", "/api/status", nil, nil)
	if !strings.Contains(st.Body.String(), `"days":0`) {
		t.Errorf("a refused upload stored data: %s", st.Body)
	}
	// A GET of the write endpoint with a query is just the read endpoint: no write.
	if w := c.do("GET", "/api/samples", nil, nil); w.Code != http.StatusBadRequest {
		t.Errorf("series with no range = %d", w.Code)
	}
}

func TestProfileValidationAndStrain(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })

	for _, bad := range []string{`{"birth_year":1700}`, `{"birth_year":2026}`, `{"sex":"x"}`, `{"weight_kg":5}`} {
		if w := c.jsonPost("/api/profile", bad, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", bad, w.Code)
		}
	}
	if w := c.jsonPost("/api/profile", `{"birth_year":1990,"sex":"female","weight_kg":60}`, nil); w.Code != 200 {
		t.Fatalf("profile = %d %s", w.Code, w.Body)
	}
	if w := c.do("GET", "/api/profile", nil, nil); !strings.Contains(w.Body.String(), `"birth_year":1990`) {
		t.Errorf("profile = %s", w.Body)
	}
	// An hour of live heart rate at 150 bpm, then ask for the day.
	var parts []string
	base := now.Add(-time.Hour).UnixMilli()
	for i := 0; i < 3600; i++ {
		parts = append(parts, `{"t":`+itoa(base+int64(i)*1000)+`,"hr":150,"rr":[]}`)
	}
	for i := 0; i < len(parts); i += 1000 {
		end := min(i+1000, len(parts))
		if w := c.jsonPost("/api/samples", `{"samples":[`+strings.Join(parts[i:end], ",")+`]}`, nil); w.Code != 200 {
			t.Fatalf("upload = %d %s", w.Code, w.Body)
		}
	}
	w := c.do("GET", "/api/day?date=2026-10-21", nil, nil)
	var day struct {
		Summary struct {
			Strain     float64 `json:"strain"`
			HaveStrain bool    `json:"have_strain"`
			Samples    int     `json:"samples"`
		} `json:"summary"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &day) != nil || !day.Summary.HaveStrain || day.Summary.Strain <= 5 || day.Summary.Samples != 3600 {
		t.Fatalf("day = %d %s", w.Code, w.Body)
	}
	if w := c.do("GET", "/api/day?date=garbage", nil, nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad date = %d", w.Code)
	}
}

func TestProfileTakesAFullBirthDate(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })

	for _, bad := range []string{`{"birth_date":"1999-13-40"}`, `{"birth_date":"garbage"}`, `{"birth_date":"2026-01-01"}`, `{"birth_date":"1800-01-01"}`} {
		if w := c.jsonPost("/api/profile", bad, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", bad, w.Code)
		}
	}
	w := c.jsonPost("/api/profile", `{"birth_date":"2000-01-15"}`, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"birth_date":"2000-01-15"`) || !strings.Contains(w.Body.String(), `"birth_year":2000`) {
		t.Fatalf("profile = %d %s (birth_year should be filled from the date)", w.Code, w.Body)
	}
	if w := c.do("GET", "/api/profile", nil, nil); !strings.Contains(w.Body.String(), `"birth_date":"2000-01-15"`) {
		t.Errorf("not stored: %s", w.Body)
	}
}

func TestHealthReportsAnIntactDatabaseAndFreshness(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })
	c.jsonPost("/api/samples", sampleBody(10, 2*time.Minute), nil)
	w := c.do("GET", "/api/health", nil, nil)
	var h struct {
		OK       bool   `json:"ok"`
		Database string `json:"database"`
		Age      int    `json:"latest_sample_age_seconds"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &h) != nil || !h.OK || h.Database != "ok" {
		t.Fatalf("health = %d %s", w.Code, w.Body)
	}
	if h.Age < 100 || h.Age > 120 { // the newest sample is the last of 10, written 2 min - 9 s ago
		t.Errorf("latest sample age = %d s", h.Age)
	}
}

func TestAQuietDayStillReportsZeroStrain(t *testing.T) {
	// The page calls .toFixed() on these. A zero left out of the JSON made the
	// whole panel fail with "Cannot read properties of undefined".
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })
	c.jsonPost("/api/profile", `{"birth_date":"2000-01-15"}`, nil)
	var parts []string
	base := now.Add(-10 * time.Minute).UnixMilli()
	for i := 0; i < 600; i++ {
		parts = append(parts, `{"t":`+itoa(base+int64(i)*1000)+`,"hr":72,"rr":[]}`)
	}
	if w := c.jsonPost("/api/samples", `{"samples":[`+strings.Join(parts, ",")+`]}`, nil); w.Code != 200 {
		t.Fatalf("upload = %d %s", w.Code, w.Body)
	}
	w := c.do("GET", "/api/day?date=2026-10-21", nil, nil)
	var day struct {
		Summary map[string]any `json:"summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &day); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"strain", "edwards", "banister", "avg_hr", "min_hr", "max_hr", "hr_max_used", "minutes"} {
		if _, ok := day.Summary[key]; !ok {
			t.Errorf("summary has no %q on a quiet day: %s", key, w.Body)
		}
	}
	night := day.Summary["night"].(map[string]any)
	for _, key := range []string{"resting_hr", "rmssd", "ln_rmssd", "resp_rate"} {
		if _, ok := night[key]; !ok {
			t.Errorf("night has no %q: %s", key, w.Body)
		}
	}
	if day.Summary["have_strain"] != true || day.Summary["strain"].(float64) != 0 {
		t.Errorf("quiet day: have_strain=%v strain=%v", day.Summary["have_strain"], day.Summary["strain"])
	}
}

func TestCSRFTokenEndpointLetsAPageRecoverAfterARestart(t *testing.T) {
	c := newClient(t)
	page := c.page()
	if !strings.Contains(page, c.csrf) {
		t.Fatal("page has no token")
	}
	w := c.do("GET", "/api/csrf", nil, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") })
	var out struct{ Token string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Token != c.csrf {
		t.Fatalf("csrf = %d %s", w.Code, w.Body)
	}
	if w := c.do("GET", "/api/csrf", nil, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }); w.Code != http.StatusForbidden {
		t.Errorf("cross-site token request = %d: another website must not be handed the token", w.Code)
	}
	// What the page does after a restart: refused with the old token, then retries with the new one.
	t.Cleanup(func() { c.srv.Close() })
	if w := c.jsonPost("/api/samples", sampleBody(1, time.Minute), func(r *http.Request) { r.Header.Set("X-CSRF-Token", "old") }); w.Code != http.StatusForbidden {
		t.Errorf("old token = %d", w.Code)
	}
	if w := c.jsonPost("/api/samples", sampleBody(1, time.Minute), func(r *http.Request) { r.Header.Set("X-CSRF-Token", out.Token) }); w.Code != 200 {
		t.Errorf("refreshed token = %d", w.Code)
	}
}

func TestPageScriptRecoversFromAStaleToken(t *testing.T) {
	js := newClient(t).do("GET", "/static/app.js", nil, nil).Body.String()
	for _, want := range []string{"/api/csrf", "async function postJSON", "res.status === 403", "?? 0"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
	if strings.Contains(js, "day.strain.toFixed") || strings.Contains(js, "resp_rate.toFixed") {
		t.Error("app.js still calls .toFixed on a field the server may omit")
	}
	// Every JSON write goes through the one helper that can recover. The only other
	// POST is the session save, which sends the form with its own token.
	if n := strings.Count(js, `method: "POST"`); n != 2 {
		t.Errorf("found %d hand-rolled POSTs; expected postJSON plus the form-encoded session save", n)
	}
	if strings.Count(js, "application/x-www-form-urlencoded") != 1 {
		t.Error("exactly one write may be form-encoded (the session save)")
	}
}

func TestDaySummaryTellsThePageWhereTheDataStarts(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })
	read := func() int64 {
		var d struct {
			Summary struct {
				First int64 `json:"first_sample_ms"`
			} `json:"summary"`
		}
		json.Unmarshal(c.do("GET", "/api/day?date=2026-10-21", nil, nil).Body.Bytes(), &d)
		return d.Summary.First
	}
	if got := read(); got != 0 {
		t.Errorf("an empty day reported a first sample at %d", got)
	}
	c.jsonPost("/api/samples", sampleBody(20, time.Minute), nil)
	want := now.Add(-time.Minute).UnixMilli()
	if got := read(); got != want {
		t.Errorf("first_sample_ms = %d, want %d: the chart window starts here", got, want)
	}
}

func TestAShortWorkoutMessageIsNotWipedByTheStatusLoop(t *testing.T) {
	// The once-a-second status text used to overwrite the explanation within a second.
	js := newClient(t).do("GET", "/static/app.js", nil, nil).Body.String()
	for _, want := range []string{"holdNoteUntil", "const say =", `say("Workouts under a minute`, "if (Date.now() < holdNoteUntil) return;"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
	if strings.Contains(js, `note.textContent = "Workouts under a minute`) {
		t.Error("the short-workout message is written straight to the note again, where describe() erases it")
	}
}

func TestSavingASessionDoesNotReloadThePage(t *testing.T) {
	// form.submit() reloads the page, and a reload drops the Bluetooth connection:
	// the live recording used to end every time a resting check finished.
	js := newClient(t).do("GET", "/static/app.js", nil, nil).Body.String()
	if strings.Contains(js, "form.submit()") {
		t.Error("app.js submits the form, which reloads the page and disconnects the strap")
	}
	for _, want := range []string{"/strap/save", "new URLSearchParams(new FormData(form))", "DOMParser", "flashBanner"} {
		if want == "/strap/save" {
			continue // the action comes from the form itself
		}
		if !strings.Contains(js, want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
}

func TestFlashBannerOnlyUsesWhatIsInScope(t *testing.T) {
	// flashBanner sits in the strap section; the el()/sv() helpers belong to the
	// Today panel's own scope. Calling them from here threw a ReferenceError that
	// left the status line stuck on "Saving…".
	js := newClient(t).do("GET", "/static/app.js", nil, nil).Body.String()
	i := strings.Index(js, "const flashBanner")
	j := strings.Index(js[i:], "const save = async")
	if i < 0 || j < 0 {
		t.Fatal("flashBanner not found")
	}
	body := js[i : i+j]
	for _, helper := range []string{"el(", "sv(", "$("} {
		if strings.Contains(strings.ReplaceAll(body, "document.createElement(", ""), helper) && helper != "$(" {
			t.Errorf("flashBanner calls %s out of scope", helper)
		}
	}
	if !strings.Contains(js, "} finally {\n      status.textContent") {
		t.Error("save() must always leave the Saving… status, even when the banner fails")
	}
}

func TestOffWristSamplesAreNotPlottedAsAHeartRate(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })
	base := now.Add(-5 * time.Minute).UnixMilli()
	var parts []string
	for i := 0; i < 60; i++ {
		if i >= 20 && i < 40 { // the strap came off for 20 seconds
			parts = append(parts, `{"t":`+itoa(base+int64(i)*1000)+`,"hr":0,"rr":[],"off":true}`)
		} else {
			parts = append(parts, `{"t":`+itoa(base+int64(i)*1000)+`,"hr":72,"rr":[]}`)
		}
	}
	if w := c.jsonPost("/api/samples", `{"samples":[`+strings.Join(parts, ",")+`]}`, nil); w.Code != 200 {
		t.Fatalf("upload = %d %s", w.Code, w.Body)
	}
	w := c.do("GET", "/api/samples?from="+itoa(base-1000)+"&to="+itoa(now.UnixMilli())+"&points=600", nil, nil)
	var series struct {
		HR []float64 `json:"hr"`
	}
	json.Unmarshal(w.Body.Bytes(), &series)
	if len(series.HR) != 40 {
		t.Errorf("series has %d points, want the 40 on-wrist samples", len(series.HR))
	}
	for _, v := range series.HR {
		if v != 72 {
			t.Fatalf("series contains %v: off-wrist time leaked into the chart as a dip", v)
		}
	}
	// ...while the day still counts that time as off-wrist, separately.
	var day struct {
		Summary struct {
			Off float64 `json:"off_wrist_minutes"`
			Min float64 `json:"min_hr"`
		} `json:"summary"`
	}
	json.Unmarshal(c.do("GET", "/api/day?date=2026-10-21", nil, nil).Body.Bytes(), &day)
	if day.Summary.Off < 0.3 || day.Summary.Min != 72 {
		t.Errorf("off_wrist_minutes=%v min_hr=%v", day.Summary.Off, day.Summary.Min)
	}
}

func TestRefusedRequestsAreLoggedWithoutSecrets(t *testing.T) {
	c := newClient(t)
	var buf strings.Builder
	c.srv.Log = log.New(&buf, "", 0)
	c.page()
	// A tab with a stale token: every upload is refused. That must be visible in the log.
	for i := 0; i < 5; i++ {
		c.jsonPost("/api/samples", sampleBody(1, time.Minute), func(r *http.Request) { r.Header.Set("X-CSRF-Token", "secret-stale-token") })
	}
	out := buf.String()
	if !strings.Contains(out, "refused: POST /api/samples 403") {
		t.Fatalf("a refused upload left no trace in the log: %q", out)
	}
	if strings.Count(out, "refused: POST /api/samples 403") != 1 {
		t.Errorf("five identical refusals should log once per 30 s, got %d lines", strings.Count(out, "refused:"))
	}
	if strings.Contains(out, "secret-stale-token") || strings.Contains(out, c.csrf) {
		t.Error("the log leaked a token")
	}
	// Ordinary successful traffic is not logged.
	buf.Reset()
	c.jsonPost("/api/samples", sampleBody(1, time.Minute), nil)
	c.do("GET", "/api/status", nil, nil)
	if buf.Len() != 0 {
		t.Errorf("successful requests were logged: %q", buf.String())
	}
	t.Cleanup(func() { c.srv.Close() })
}

func TestThePageCanReportWhatHappenedToTheServerLog(t *testing.T) {
	c := newClient(t)
	var buf strings.Builder
	c.srv.Log = log.New(&buf, "", 0)
	c.page()
	w := c.jsonPost("/api/clientlog", `{"event":"connect_failed","message":"NotAllowedError: Permissions policy\nviolation"}`, nil)
	if w.Code != 200 {
		t.Fatalf("clientlog = %d %s", w.Code, w.Body)
	}
	if got := buf.String(); !strings.Contains(got, "page: connect_failed: NotAllowedError: Permissions policy violation") {
		t.Errorf("log = %q (newlines must be flattened so a page cannot forge log lines)", got)
	}
	if w := c.jsonPost("/api/clientlog", `{"event":"x","message":"y"}`, func(r *http.Request) { r.Header.Set("X-CSRF-Token", "no") }); w.Code != http.StatusForbidden {
		t.Errorf("clientlog without the token = %d", w.Code)
	}
	// Rate limited: a looping page cannot fill the disk.
	var limited int
	for i := 0; i < 80; i++ {
		if w := c.jsonPost("/api/clientlog", `{"event":"spam","message":"y"}`, nil); w.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Error("80 reports in a row were all accepted")
	}
	long := strings.Repeat("A", 5000)
	buf.Reset()
	c.srv.clientLogMu.Lock()
	c.srv.clientLogCount = 0
	c.srv.clientLogMu.Unlock()
	c.jsonPost("/api/clientlog", `{"event":"big","message":"`+long+`"}`, nil)
	if len(buf.String()) > 500 {
		t.Errorf("a 5000-character message made a %d-byte log line", len(buf.String()))
	}
}

func TestSetClockIsVerifiedAgainstTheStrapsOwnClock(t *testing.T) {
	// A strap with an invalid clock saves nothing to flash. The page must say so,
	// and Set strap clock must prove the strap took the time rather than assume it.
	js := newClient(t).do("GET", "/static/app.js", nil, nil).Body.String()
	for _, want := range []string{"STRAP_CLOCK_VALID_FROM", "updateClockHint", "checking that the strap took it", "The strap did not take the clock", "clock_verified", "clock_not_taken", "NOT saving heart-rate history"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
	page := newClient(t).page()
	if !strings.Contains(page, `id="strap-clock-hint"`) {
		t.Error("the dashboard has no clock hint line")
	}
}

func TestAutopilotIsInThePageAndOnlySendsTheClockAndHistoryCommands(t *testing.T) {
	c := newClient(t)
	page := c.page()
	for _, id := range []string{`id="strap-auto"`, `id="strap-auto-status"`, `id="strap-auto-row"`} {
		if !strings.Contains(page, id) {
			t.Errorf("dashboard lacks %s", id)
		}
	}
	js := c.do("GET", "/static/app.js", nil, nil).Body.String()
	for _, want := range []string{"const autopilotTick", "startAutopilot()", "stopAutopilot()", "const setClockNow", "const syncNow", "vitals.autopilot", "clockGood()", "heart-rate record"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
	// The tick body must only reach the clock and history steps.
	i := strings.Index(js, "const autopilotTick")
	j := strings.Index(js[i:], "const stopAutopilot")
	tick := js[i : i+j]
	for _, forbidden := range []string{"writeCmd(", "whoop.on", "whoop.off", "apiJSON("} {
		if strings.Contains(tick, forbidden) {
			t.Errorf("the autopilot tick calls %s directly: it must go through setClockNow / syncNow only", forbidden)
		}
	}
}

func TestSyncReportsHowManyHeartRecordsItFound(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })
	c.jsonPost("/api/history/start", `{}`, nil)
	// A burst of console logs only: the sync completes but says there were no heart-rate records.
	bodies := []string{hexBody(histMeta(whoopble.MetaHistoryStart))}
	for i := 0; i < 5; i++ {
		bodies = append(bodies, hexBody([]byte{whoopble.TypeConsoleLog, 1, 2, 3, byte(i)}))
	}
	bodies = append(bodies, hexBody(histMeta(whoopble.MetaHistoryEnd, 5, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8)), hexBody(histMeta(whoopble.MetaHistoryComplete)))
	w := c.jsonPost("/api/history/frames", `{"bodies":[`+strings.Join(bodies, ",")+`]}`, nil)
	var out struct {
		Done         bool `json:"done"`
		Records      int  `json:"records"`
		HeartRecords int  `json:"heart_records"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if !out.Done || out.Records != 5 || out.HeartRecords != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	st := c.do("GET", "/api/history/status", nil, nil).Body.String()
	if !strings.Contains(st, `"heart_records":0`) || !strings.Contains(st, `"packets":5`) {
		t.Errorf("last-sync summary missing: %s", st)
	}
}

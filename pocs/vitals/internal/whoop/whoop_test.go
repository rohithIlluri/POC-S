package whoop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
)

var now = time.Date(2026, 10, 21, 20, 0, 0, 0, time.UTC)

// fakeWhoop is a minimal stand-in for the WHOOP API.
type fakeWhoop struct {
	t            *testing.T
	mu           sync.Mutex
	access       string // the currently valid access token
	refresh      string
	refreshCalls int
	rateLimitOne bool
	strain       float64
	paths        []string
	revoked      bool
}

func newFake(t *testing.T) (*fakeWhoop, *httptest.Server) {
	f := &fakeWhoop{t: t, access: "at-1", refresh: "rt-1", strain: 8.4}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeWhoop) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)

	if r.URL.Path == tokenPath {
		_ = r.ParseForm()
		if r.Form.Get("client_id") != "cid" || r.Form.Get("client_secret") != "secret" {
			http.Error(w, `{"error":"invalid_client"}`, 401)
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "good-code" || r.Form.Get("redirect_uri") != DefaultRedirect {
				http.Error(w, `{"error":"invalid_grant"}`, 400)
				return
			}
		case "refresh_token":
			if r.Form.Get("refresh_token") != f.refresh {
				http.Error(w, `{"error":"invalid_grant","error_description":"refresh token revoked"}`, 400)
				return
			}
			f.refreshCalls++
		}
		f.access = fmt.Sprintf("at-%d", f.refreshCalls+2)
		f.refresh = fmt.Sprintf("rt-%d", f.refreshCalls+2) // WHOOP rotates refresh tokens
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": f.access, "refresh_token": f.refresh, "expires_in": 3600, "scope": strings.Join(Scopes, " "),
		})
		return
	}

	if r.Header.Get("Authorization") != "Bearer "+f.access {
		http.Error(w, `{"message":"unauthorized"}`, 401)
		return
	}
	if f.rateLimitOne {
		f.rateLimitOne = false
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(429)
		return
	}
	paged := !strings.HasPrefix(r.URL.Path, "/developer/v2/user/")
	if paged && (r.URL.Query().Get("limit") != "25" || r.URL.Query().Get("start") == "" || r.URL.Query().Get("end") == "") {
		f.t.Errorf("bad pagination query: %s", r.URL.RawQuery)
	}
	page := r.URL.Query().Get("nextToken")
	write := func(records any, next string) {
		json.NewEncoder(w).Encode(map[string]any{"records": records, "next_token": next})
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /developer/v2/user/profile/basic":
		json.NewEncoder(w).Encode(map[string]any{"user_id": 10129, "first_name": "Sam", "last_name": "Lee"})
	case "DELETE /developer/v2/user/access":
		f.revoked = true
		w.WriteHeader(204)
	case "GET /developer/v2/cycle":
		write([]map[string]any{
			{"id": 93845, "start": "2026-10-21T06:10:00Z", "end": nil, "score_state": "SCORED",
				"score": map[string]any{"strain": f.strain, "kilojoule": 8200.5, "average_heart_rate": 68, "max_heart_rate": 160}},
			{"id": 93844, "start": "2026-10-20T06:30:00Z", "end": "2026-10-21T06:10:00Z", "score_state": "PENDING_SCORE"},
		}, "")
	case "GET /developer/v2/recovery":
		write([]map[string]any{
			{"cycle_id": 93845, "sleep_id": "ecfc6a15-4661-442f-a9a4-f160dd7afae8", "created_at": "2026-10-21T06:25:00Z", "score_state": "SCORED",
				"score": map[string]any{"user_calibrating": false, "recovery_score": 64, "resting_heart_rate": 55, "hrv_rmssd_milli": 61.278, "spo2_percentage": 96.5, "skin_temp_celsius": 33.7}},
		}, "")
	case "GET /developer/v2/activity/sleep":
		if page == "" { // two pages, to exercise pagination
			write([]map[string]any{
				{"id": "ecfc6a15-4661-442f-a9a4-f160dd7afae8", "start": "2026-10-20T22:40:00Z", "end": "2026-10-21T06:10:00Z", "nap": false, "score_state": "SCORED",
					"score": map[string]any{
						"stage_summary":                map[string]any{"total_light_sleep_time_milli": 12600000, "total_slow_wave_sleep_time_milli": 5400000, "total_rem_sleep_time_milli": 6300000, "total_awake_time_milli": 2700000},
						"respiratory_rate":             15.82,
						"sleep_performance_percentage": 88,
					}},
			}, "page-2")
			return
		}
		write([]map[string]any{
			{"id": "b1", "start": "2026-10-21T14:00:00Z", "end": "2026-10-21T14:30:00Z", "nap": true, "score_state": "SCORED",
				"score": map[string]any{"stage_summary": map[string]any{"total_light_sleep_time_milli": 1200000}}},
		}, "")
	case "GET /developer/v2/activity/workout":
		write([]map[string]any{
			{"id": "w1", "start": "2026-10-21T17:00:00Z", "end": "2026-10-21T17:45:30Z", "sport_name": "running\x1b[2J", "score_state": "SCORED", "score": map[string]any{"strain": 9.1}},
		}, "")
	default:
		http.NotFound(w, r)
	}
}

func testClient(srv *httptest.Server, creds Credentials) (*Client, *[]Credentials) {
	var saved []Credentials
	cl := NewClient(creds, func(c Credentials) error { saved = append(saved, c); return nil })
	cl.Base = srv.URL
	cl.HTTP = srv.Client()
	cl.Now = func() time.Time { return now }
	cl.Sleep = func(time.Duration) {}
	return cl, &saved
}

func connected() Credentials {
	return Credentials{ClientID: "cid", ClientSecret: "secret", RedirectURL: DefaultRedirect,
		AccessToken: "at-1", RefreshToken: "rt-1", ExpiresAt: now.Add(time.Hour)}
}

func TestAuthURL(t *testing.T) {
	u, err := url.Parse(AuthURL(DefaultBase, Credentials{ClientID: "cid", RedirectURL: DefaultRedirect}, "state-123"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "api.prod.whoop.com" || u.Path != authPath || q.Get("client_id") != "cid" ||
		q.Get("redirect_uri") != DefaultRedirect || q.Get("state") != "state-123" || q.Get("response_type") != "code" ||
		!strings.Contains(q.Get("scope"), "offline") || !strings.Contains(q.Get("scope"), "read:recovery") {
		t.Fatalf("auth url = %s", u)
	}
	if len(NewState()) < 8 {
		t.Fatal("state too short for WHOOP")
	}
}

func TestExchangeSavesTokens(t *testing.T) {
	_, srv := newFake(t)
	cl, saved := testClient(srv, Credentials{ClientID: "cid", ClientSecret: "secret", RedirectURL: DefaultRedirect})
	if err := cl.Exchange(context.Background(), "bad-code"); err == nil {
		t.Fatal("bad code accepted")
	}
	if err := cl.Exchange(context.Background(), "good-code"); err != nil {
		t.Fatal(err)
	}
	c := (*saved)[len(*saved)-1]
	if !c.Connected() || c.RefreshToken == "" || !c.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("saved creds = %+v", c)
	}
}

func TestSyncConvertsAndIsIdempotent(t *testing.T) {
	f, srv := newFake(t)
	cl, _ := testClient(srv, connected())
	s, _ := store.Open(t.TempDir())

	r, err := Sync(context.Background(), cl, s, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.Pending != 1 || r.Added != 11 || r.Updated != 0 {
		t.Fatalf("first sync = %+v", r)
	}
	if !r.From.Equal(now.AddDate(0, 0, -FirstSyncDays)) {
		t.Errorf("first sync window from %v", r.From)
	}
	if cl.Creds.MemberName != "Sam" || !cl.Creds.LastSync.Equal(now) {
		t.Errorf("creds after sync = %+v", cl.Creds)
	}

	got, _, _ := s.Load()
	byID := map[string]store.Entry{}
	for _, e := range got {
		byID[e.ID] = e
		if e.Source != Source {
			t.Errorf("entry without whoop source: %+v", e)
		}
	}
	wake := time.Date(2026, 10, 21, 6, 10, 0, 0, time.UTC)
	checks := []struct {
		id    string
		value float64
		at    time.Time
	}{
		{"whoop:sleep:ecfc6a15-4661-442f-a9a4-f160dd7afae8", 6.75, wake},
		{"whoop:sleepperf:ecfc6a15-4661-442f-a9a4-f160dd7afae8", 88, wake},
		{"whoop:resprate:ecfc6a15-4661-442f-a9a4-f160dd7afae8", 15.82, wake},
		{"whoop:sleep:b1", 0.33, time.Date(2026, 10, 21, 14, 30, 0, 0, time.UTC)},
		{"whoop:recovery:93845", 64, wake}, // stamped at wake time, via sleep_id
		{"whoop:hrv:93845", 61.28, wake},
		{"whoop:heartrate:93845", 55, wake},
		{"whoop:spo2:93845", 96.5, wake},
		{"whoop:skintemp:93845", 33.7, wake},
		{"whoop:strain:93845", 8.4, time.Date(2026, 10, 21, 6, 10, 0, 0, time.UTC)},
		{"whoop:exercise:w1", 46, time.Date(2026, 10, 21, 17, 0, 0, 0, time.UTC)},
	}
	for _, c := range checks {
		e, ok := byID[c.id]
		if !ok {
			t.Errorf("missing %s", c.id)
			continue
		}
		if e.Value != c.value || !e.Time.Equal(c.at) {
			t.Errorf("%s = %v at %v, want %v at %v", c.id, e.Value, e.Time, c.value, c.at)
		}
	}
	if n := byID["whoop:sleep:b1"].Note; n != "nap" {
		t.Errorf("nap note = %q", n)
	}
	if n := byID["whoop:exercise:w1"].Note; n != "running[2J" {
		t.Errorf("untrusted sport name not sanitised: %q", n)
	}
	if n := byID["whoop:strain:93845"].Note; n != "in progress" {
		t.Errorf("open cycle note = %q", n)
	}

	// Re-sync: nothing new; then the day's strain rises and is updated in place.
	r, err = Sync(context.Background(), cl, s, 0)
	if err != nil || r.Added != 0 || r.Updated != 0 {
		t.Fatalf("repeat sync = %+v, %v", r, err)
	}
	if !r.From.Equal(now.Add(-overlap)) {
		t.Errorf("incremental window from %v", r.From)
	}
	f.strain = 14.2
	if r, err = Sync(context.Background(), cl, s, 0); err != nil || r.Added != 0 || r.Updated != 1 {
		t.Fatalf("rescored sync = %+v, %v", r, err)
	}
}

func TestExpiredTokenIsRefreshedAndRotated(t *testing.T) {
	f, srv := newFake(t)
	c := connected()
	c.ExpiresAt = now.Add(-time.Hour)
	cl, saved := testClient(srv, c)
	if _, err := cl.Profile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.refreshCalls != 1 || cl.Creds.RefreshToken != f.refresh || len(*saved) == 0 {
		t.Fatalf("refresh calls=%d creds=%+v", f.refreshCalls, cl.Creds)
	}
}

func TestRevokedAccessTokenTriggersOneRefresh(t *testing.T) {
	f, srv := newFake(t)
	f.access = "server-side-revoked"
	cl, _ := testClient(srv, connected())
	if _, err := cl.Profile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.refreshCalls != 1 {
		t.Fatalf("refresh calls = %d", f.refreshCalls)
	}
}

func TestDeadRefreshTokenAsksForLogin(t *testing.T) {
	f, srv := newFake(t)
	f.refresh = "something-else"
	c := connected()
	c.ExpiresAt = now.Add(-time.Hour)
	cl, _ := testClient(srv, c)
	_, err := cl.Profile(context.Background())
	if err == nil || !strings.Contains(err.Error(), "vitals whoop login") {
		t.Fatalf("err = %v", err)
	}
}

func TestRateLimitIsRetried(t *testing.T) {
	f, srv := newFake(t)
	f.rateLimitOne = true
	var waited time.Duration
	cl, _ := testClient(srv, connected())
	cl.Sleep = func(d time.Duration) { waited += d }
	if _, err := cl.Profile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if waited != time.Second {
		t.Fatalf("waited %v, want Retry-After 1s", waited)
	}
}

func TestNotConnected(t *testing.T) {
	_, srv := newFake(t)
	cl, _ := testClient(srv, Credentials{ClientID: "cid", ClientSecret: "secret"})
	if _, err := cl.Profile(context.Background()); err != ErrNotConnected {
		t.Fatalf("err = %v", err)
	}
}

func TestRevoke(t *testing.T) {
	f, srv := newFake(t)
	cl, _ := testClient(srv, connected())
	if err := cl.Revoke(context.Background()); err != nil || !f.revoked {
		t.Fatalf("revoke: %v %v", err, f.revoked)
	}
}

func TestCredentialsFileAndEnv(t *testing.T) {
	dir := t.TempDir()
	c, err := LoadCredentials(dir)
	if err != nil || c.Configured() || c.RedirectURL != DefaultRedirect {
		t.Fatalf("empty: %+v %v", c, err)
	}
	if err := SaveCredentials(dir, Credentials{ClientID: "file-id", ClientSecret: "file-secret", RefreshToken: "rt"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHOOP_CLIENT_ID", "env-id")
	c, _ = LoadCredentials(dir)
	if c.ClientID != "env-id" || c.ClientSecret != "file-secret" || !c.Connected() {
		t.Fatalf("merged: %+v", c)
	}
	c.Disconnect()
	if c.Connected() || !c.Configured() {
		t.Fatalf("after disconnect: %+v", c)
	}
}

func TestWindow(t *testing.T) {
	if from, _ := Window(Credentials{}, now, 7); !from.Equal(now.AddDate(0, 0, -7)) {
		t.Errorf("explicit days: %v", from)
	}
	if from, _ := Window(Credentials{LastSync: now.Add(-time.Hour)}, now, 0); !from.Equal(now.Add(-time.Hour - overlap)) {
		t.Errorf("incremental: %v", from)
	}
}

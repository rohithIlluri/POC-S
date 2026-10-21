package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoop"
)

// fakeWhoop serves the OAuth endpoints and a little data. Its authorize
// endpoint immediately "approves" and redirects to redirect_uri, standing in
// for the member clicking Allow.
func fakeWhoop(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/oauth2/auth":
			q := r.URL.Query()
			back, _ := url.Parse(q.Get("redirect_uri"))
			back.RawQuery = url.Values{"code": {"the-code"}, "state": {q.Get("state")}}.Encode()
			http.Redirect(w, r, back.String(), http.StatusFound)
		case "/oauth/oauth2/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") == "authorization_code" && r.Form.Get("code") != "the-code" {
				http.Error(w, `{"error":"invalid_grant"}`, 400)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "refresh_token": "rt", "expires_in": 3600})
		case "/developer/v2/user/profile/basic":
			json.NewEncoder(w).Encode(map[string]any{"first_name": "Sam"})
		case "/developer/v2/user/access":
			w.WriteHeader(204)
		case "/developer/v2/activity/sleep":
			json.NewEncoder(w).Encode(map[string]any{"records": []map[string]any{
				{"id": "s1", "start": "2026-10-20T23:00:00Z", "end": "2026-10-21T07:00:00Z", "score_state": "SCORED",
					"score": map[string]any{"stage_summary": map[string]any{"total_light_sleep_time_milli": 14400000, "total_slow_wave_sleep_time_milli": 5400000, "total_rem_sleep_time_milli": 5400000}, "sleep_performance_percentage": 91}},
			}})
		case "/developer/v2/recovery":
			json.NewEncoder(w).Encode(map[string]any{"records": []map[string]any{
				{"cycle_id": 7, "sleep_id": "s1", "score_state": "SCORED", "score": map[string]any{"recovery_score": 81, "resting_heart_rate": 52, "hrv_rmssd_milli": 88.4}},
			}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"records": []any{}})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func whoopHarness(t *testing.T) *harness {
	h := newHarness(t)
	api := fakeWhoop(t)
	h.app.whoopBase = api.URL
	return h
}

func (h *harness) withStdin(s string) { h.app.in, h.app.stdin = strings.NewReader(s), nil }

func TestWhoopStatusGuidesSetup(t *testing.T) {
	h := whoopHarness(t)
	out := h.ok("whoop")
	if !strings.Contains(out, "developer.whoop.com") || !strings.Contains(out, whoop.DefaultRedirect) {
		t.Fatalf("status:\n%s", out)
	}
	h.fails("whoop login", "vitals whoop setup")
	h.fails("whoop sync", "not connected")
	h.fails("whoop frobnicate", "unknown whoop command")
}

func TestWhoopSetupPromptsForSecret(t *testing.T) {
	h := whoopHarness(t)
	h.withStdin("s3cret\n")
	out := h.ok("whoop setup --client-id cid")
	if !strings.Contains(out, "next: vitals whoop login") {
		t.Fatalf("setup:\n%s", out)
	}
	c, _ := whoop.LoadCredentials(h.app.home)
	if c.ClientID != "cid" || c.ClientSecret != "s3cret" {
		t.Fatalf("saved %+v", c)
	}
	if out := h.ok("whoop status"); !strings.Contains(out, "not connected") {
		t.Errorf("status after setup: %s", out)
	}
	h.fails("whoop setup --redirect not-a-url", "absolute URL")
}

func TestWhoopSetupBothPromptsFromPipe(t *testing.T) {
	h := whoopHarness(t)
	h.withStdin("cid\nsecret\n") // printf 'cid\nsecret\n' | vitals whoop setup
	h.ok("whoop setup")
	if c, _ := whoop.LoadCredentials(h.app.home); c.ClientID != "cid" || c.ClientSecret != "secret" {
		t.Fatalf("saved %+v", c)
	}
}

func TestWhoopPasteLoginSyncLogout(t *testing.T) {
	h := whoopHarness(t)
	_ = whoop.SaveCredentials(h.app.home, whoop.Credentials{ClientID: "cid", ClientSecret: "secret", RedirectURL: whoop.DefaultRedirect})

	// Simulate the user: read the printed auth URL, "approve", paste back.
	h.app.in, h.app.stdin = &pasteReader{h: h}, nil
	out := h.ok("whoop login --paste")
	if !strings.Contains(out, "connected to WHOOP as Sam") {
		t.Fatalf("paste login:\n%s", out)
	}

	h.withStdin(whoop.DefaultRedirect + "?code=the-code&state=wrong\n")
	h.fails("whoop login --paste", "state mismatch")
	h.withStdin("garbage\n")
	h.fails("whoop login --paste", "doesn't look like")

	out = h.ok("whoop sync")
	if !strings.Contains(out, "5 new, 0 updated") {
		t.Fatalf("sync: %s", out)
	}
	if out := h.ok("whoop sync"); !strings.Contains(out, "0 new, 0 updated") {
		t.Errorf("re-sync should be a no-op: %s", out)
	}
	list := h.ok("list --metric recovery")
	if !strings.Contains(list, "81%") || !strings.Contains(list, "[whoop]") {
		t.Errorf("synced entries not listed:\n%s", list)
	}
	if out := h.ok("trends --days 7"); !strings.Contains(out, "recovery") || !strings.Contains(out, "hrv") {
		t.Errorf("trends missing WHOOP metrics:\n%s", out)
	}
	if out := h.ok("whoop status"); !strings.Contains(out, "connected as Sam") || !strings.Contains(out, "last sync") {
		t.Errorf("status: %s", out)
	}
	if out := h.ok("whoop logout"); !strings.Contains(out, "disconnected") {
		t.Errorf("logout: %s", out)
	}
	if c, _ := whoop.LoadCredentials(h.app.home); c.Connected() {
		t.Error("still connected after logout")
	}
}

// pasteReader answers the "Paste it here" prompt by following the auth URL
// the command printed, the way a person would in their browser.
type pasteReader struct {
	h    *harness
	done bool
}

func (p *pasteReader) Read(b []byte) (int, error) {
	if p.done {
		return 0, fmt.Errorf("eof")
	}
	p.done = true
	out := p.h.app.out.(*bytes.Buffer).String()
	var auth string
	for _, f := range strings.Fields(out) {
		if strings.Contains(f, "/oauth/oauth2/auth?") {
			auth = f
		}
	}
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := cl.Get(auth)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return copy(b, resp.Header.Get("Location")+"\n"), nil
}

func TestWhoopBrowserLoginEndToEnd(t *testing.T) {
	h := whoopHarness(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d/whoop/callback", port)
	_ = whoop.SaveCredentials(h.app.home, whoop.Credentials{ClientID: "cid", ClientSecret: "secret", RedirectURL: redirect})

	// Play the browser: once the callback server is up, open /whoop/connect
	// and follow redirects through the fake WHOOP and back.
	go func() {
		start := fmt.Sprintf("http://127.0.0.1:%d/whoop/connect", port)
		for i := 0; i < 100; i++ {
			resp, err := http.Get(start)
			if err == nil {
				resp.Body.Close()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	out := h.ok("whoop login --no-browser --timeout 10s")
	if !strings.Contains(out, "connected to WHOOP as Sam") {
		t.Fatalf("browser login:\n%s", out)
	}
	if c, _ := whoop.LoadCredentials(h.app.home); !c.Connected() || c.MemberName != "Sam" {
		t.Fatalf("creds: %+v", c)
	}
}

func TestServeRefusesPublicAddress(t *testing.T) {
	h := newHarness(t)
	h.fails("serve --addr 0.0.0.0:8737", "local-only")
}

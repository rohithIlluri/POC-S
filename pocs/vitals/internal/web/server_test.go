package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoop"
)

var now = time.Date(2026, 10, 21, 20, 0, 0, 0, time.UTC)

// fakeWhoopAPI answers just enough of the WHOOP API for the login + sync flow.
func fakeWhoopAPI(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/oauth2/token":
			_ = r.ParseForm()
			if r.Form.Get("code") != "the-code" && r.Form.Get("grant_type") == "authorization_code" {
				http.Error(w, `{"error":"invalid_grant"}`, 400)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "refresh_token": "rt", "expires_in": 3600})
		case "/developer/v2/user/profile/basic":
			json.NewEncoder(w).Encode(map[string]any{"first_name": "Sam"})
		case "/developer/v2/user/access":
			w.WriteHeader(204)
		case "/developer/v2/recovery":
			json.NewEncoder(w).Encode(map[string]any{"records": []map[string]any{
				{"cycle_id": 1, "created_at": "2026-10-21T06:30:00Z", "score_state": "SCORED",
					"score": map[string]any{"recovery_score": 72, "resting_heart_rate": 54, "hrv_rmssd_milli": 70}},
			}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"records": []any{}})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type client struct {
	t    *testing.T
	srv  *Server
	h    http.Handler
	csrf string
}

func newClient(t *testing.T) *client {
	api := fakeWhoopAPI(t)
	s := &Server{Home: t.TempDir(), Loc: time.UTC, Now: func() time.Time { return now }, WhoopBase: api.URL, HTTP: api.Client()}
	return &client{t: t, srv: s, h: s.Handler()}
}

func (c *client) do(method, target string, form url.Values, mutate func(*http.Request)) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, target, body)
	r.Host = "localhost:8737"
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if mutate != nil {
		mutate(r)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

var csrfRe = regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`)

func (c *client) page() string {
	c.t.Helper()
	w := c.do("GET", "/", nil, nil)
	if w.Code != 200 {
		c.t.Fatalf("GET / = %d", w.Code)
	}
	body := w.Body.String()
	if m := csrfRe.FindStringSubmatch(body); m != nil {
		c.csrf = m[1]
	}
	return body
}

func (c *client) post(path string, form url.Values) *httptest.ResponseRecorder {
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf", c.csrf)
	return c.do("POST", path, form, nil)
}

func TestRefusesNonLoopbackHost(t *testing.T) {
	c := newClient(t)
	w := c.do("GET", "/", nil, func(r *http.Request) { r.Host = "evil.example:8737" })
	if w.Code != http.StatusMisdirectedRequest {
		t.Fatalf("rebinding host got %d", w.Code)
	}
	for _, h := range []string{"localhost:8737", "127.0.0.1:8737", "[::1]:8737"} {
		if !IsLoopbackHost(h) {
			t.Errorf("%s should be loopback", h)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	w := newClient(t).do("GET", "/", nil, nil)
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP = %q", csp)
	}
	if w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("missing Referrer-Policy")
	}
}

func TestPostRequiresCSRFAndSameOrigin(t *testing.T) {
	c := newClient(t)
	c.page()
	form := url.Values{"metric": {"sleep"}, "value": {"7"}}
	if w := c.do("POST", "/log", form, nil); w.Code != 403 {
		t.Errorf("missing token got %d", w.Code)
	}
	form.Set("csrf", c.csrf)
	if w := c.do("POST", "/log", form, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }); w.Code != 403 {
		t.Errorf("cross-origin got %d", w.Code)
	}
	if w := c.do("POST", "/log", form, func(r *http.Request) { r.Header.Set("Origin", "http://localhost:8737") }); w.Code != 303 {
		t.Errorf("same-origin got %d", w.Code)
	}
}

func TestLogFormAndDelete(t *testing.T) {
	c := newClient(t)
	c.page()
	if w := c.post("/log", url.Values{"metric": {"sleep"}, "value": {"7.5"}, "note": {"<b>rested</b>"}, "days": {"7"}}); w.Code != 303 || w.Header().Get("Location") != "/?days=7" {
		t.Fatalf("log = %d %s", w.Code, w.Header().Get("Location"))
	}
	c.post("/log", url.Values{"metric": {"symptom"}, "label": {"headache"}, "value": {"4"}})
	c.post("/log", url.Values{"metric": {"mood"}, "value": {"9"}})

	body := c.page()
	for _, want := range []string{"Logged sleep 7.5 h", "Logged symptom headache (4/5)", "Not logged: mood: 9 is out of range", "&lt;b&gt;rested&lt;/b&gt;", "headache ×1"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(body, "<b>rested</b>") {
		t.Error("note was not HTML-escaped")
	}

	st, _ := store.Open(c.srv.Home)
	entries, _, _ := st.Load()
	c.post("/delete", url.Values{"id": {entries[0].ID}})
	if left, _, _ := st.Load(); len(left) != 1 {
		t.Fatalf("after delete: %d entries", len(left))
	}
}

func TestWhoopConnectCallbackSyncDisconnect(t *testing.T) {
	c := newClient(t)
	body := c.page()
	if !strings.Contains(body, "vitals whoop setup") {
		t.Fatal("unconfigured WHOOP card should explain setup")
	}

	if err := whoop.SaveCredentials(c.srv.Home, whoop.Credentials{ClientID: "cid", ClientSecret: "secret", RedirectURL: whoop.DefaultRedirect}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.page(), `href="/whoop/connect"`) {
		t.Fatal("connect button missing")
	}

	w := c.do("GET", "/whoop/connect", nil, nil)
	loc, _ := url.Parse(w.Header().Get("Location"))
	state := loc.Query().Get("state")
	if w.Code != 302 || !strings.HasSuffix(loc.Path, "/oauth/oauth2/auth") || len(state) < 8 {
		t.Fatalf("connect = %d %s", w.Code, loc)
	}

	// A forged or replayed state is refused.
	c.do("GET", "/whoop/callback?code=the-code&state=forged", nil, nil)
	if !strings.Contains(c.page(), "expired or didn&#39;t start here") {
		t.Error("forged state not refused")
	}

	var connected string
	c.srv.OnConnected = func(m string) { connected = m }
	if w := c.do("GET", "/whoop/callback?code=the-code&state="+state, nil, nil); w.Code != 303 {
		t.Fatalf("callback = %d", w.Code)
	}
	if connected != "Sam" {
		t.Errorf("OnConnected got %q", connected)
	}
	if w := c.do("GET", "/whoop/callback?code=the-code&state="+state, nil, nil); w.Code != 303 {
		t.Fatal("replay")
	}
	body = c.page()
	if !strings.Contains(body, "Connected to WHOOP as Sam") || !strings.Contains(body, "expired or didn") {
		t.Errorf("callback flashes wrong:\n%s", body)
	}

	c.post("/whoop/sync", nil)
	body = c.page()
	for _, want := range []string{"WHOOP synced", "3 new", "72%", "Green", "last synced just now"} {
		if !strings.Contains(body, want) {
			t.Errorf("after sync page missing %q", want)
		}
	}

	c.post("/whoop/disconnect", nil)
	creds, _ := whoop.LoadCredentials(c.srv.Home)
	if creds.Connected() || !creds.Configured() {
		t.Errorf("after disconnect: %+v", creds)
	}
}

func TestConnectRefusesMismatchedRedirect(t *testing.T) {
	c := newClient(t)
	_ = whoop.SaveCredentials(c.srv.Home, whoop.Credentials{ClientID: "cid", ClientSecret: "secret", RedirectURL: "http://localhost:9999/whoop/callback"})
	if w := c.do("GET", "/whoop/connect", nil, nil); w.Header().Get("Location") != "/" {
		t.Fatalf("should not start OAuth when the callback would miss this server: %s", w.Header().Get("Location"))
	}
	if !strings.Contains(c.page(), "localhost:9999") {
		t.Error("mismatch not explained")
	}
}

func TestStaticAssets(t *testing.T) {
	c := newClient(t)
	for _, p := range []string{"/static/app.css", "/static/app.js"} {
		if w := c.do("GET", p, nil, nil); w.Code != 200 {
			t.Errorf("%s = %d", p, w.Code)
		}
	}
	if w := c.do("GET", "/static/index.html", nil, nil); w.Code != 404 {
		t.Errorf("template should not be served raw: %d", w.Code)
	}
}

func TestListenRefusesNonLoopback(t *testing.T) {
	if _, err := Listen("0.0.0.0:0"); err == nil {
		t.Fatal("should refuse to bind all interfaces")
	}
	ls, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range ls {
		l.Close()
	}
}

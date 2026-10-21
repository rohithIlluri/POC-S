// Package web is the local dashboard behind `vitals serve`: today at a
// glance, trend charts, insights, a quick-log form, and the WHOOP connect /
// sync flow. It is a single-user app for the machine it runs on, so it only
// ever binds to loopback and defends the usual local-server attacks:
//
//   - DNS rebinding: requests whose Host isn't localhost/127.0.0.1/[::1] are
//     refused, so a hostile web page can't read the dashboard by pointing
//     its own domain at 127.0.0.1.
//   - CSRF: every state-changing request is a POST carrying a per-process
//     random token, and a cross-origin Origin header is refused.
//   - OAuth: each WHOOP login gets a single-use, 10-minute state value.
//   - A strict Content-Security-Policy with no inline script.
//
// It can also read a Bluetooth heart-rate strap live (WHOOP in broadcast
// mode, chest straps, watches) via Web Bluetooth in the page; see strap.go.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/entry"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoop"
)

//go:embed assets
var assets embed.FS

var page = template.Must(template.New("index.html").Funcs(template.FuncMap{
	"lower": strings.ToLower,
}).ParseFS(assets, "assets/index.html"))

const stateTTL = 10 * time.Minute

// Server is the dashboard. Zero values for Now, Loc and HTTP get defaults.
type Server struct {
	Home      string
	Loc       *time.Location
	Now       func() time.Time
	WhoopBase string
	HTTP      *http.Client
	Log       *log.Logger
	// OnConnected, if set, runs after a WHOOP login completes; the CLI's
	// `vitals whoop login` uses it to shut the server down.
	OnConnected func(member string)

	once   sync.Once
	csrf   string
	mu     sync.Mutex
	states map[string]time.Time
	flash  []flash
}

type flash struct {
	Level string // "good" | "watch" | "info"
	Text  string
}

func (s *Server) init() {
	s.once.Do(func() {
		var b [32]byte
		if _, err := rand.Read(b[:]); err != nil {
			panic(err)
		}
		s.csrf = hex.EncodeToString(b[:])
		s.states = map[string]time.Time{}
		if s.Loc == nil {
			s.Loc = time.Local
		}
		if s.Now == nil {
			s.Now = time.Now
		}
		if s.WhoopBase == "" {
			s.WhoopBase = whoop.DefaultBase
		}
		if s.HTTP == nil {
			s.HTTP = &http.Client{Timeout: 30 * time.Second}
		}
		if s.Log == nil {
			s.Log = log.New(log.Writer(), "vitals: ", 0)
		}
	})
}

// Handler returns the full HTTP handler.
func (s *Server) Handler() http.Handler {
	s.init()
	static, _ := fs.Sub(assets, "assets")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.dashboard)
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticFiles(static)))
	mux.HandleFunc("POST /log", s.post(s.logEntry))
	mux.HandleFunc("POST /delete", s.post(s.deleteEntry))
	mux.HandleFunc("POST /strap/save", s.post(s.strapSave))
	mux.HandleFunc("GET /whoop/connect", s.whoopConnect)
	mux.HandleFunc("GET /whoop/callback", s.whoopCallback)
	mux.HandleFunc("POST /whoop/sync", s.post(s.whoopSync))
	mux.HandleFunc("POST /whoop/disconnect", s.post(s.whoopDisconnect))
	return s.guard(mux)
}

func staticFiles(fsys fs.FS) http.Handler {
	files := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "app.css" && r.URL.Path != "app.js" {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}

// IsLoopbackHost reports whether a Host header names this machine.
func IsLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsLoopbackHost(r.Host) {
			http.Error(w, "vitals only answers on localhost", http.StatusMisdirectedRequest)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self' data:; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		// same-origin, not no-referrer: no-referrer makes browsers send
		// "Origin: null" on form posts, which the CSRF check must refuse.
		// same-origin still keeps the OAuth code out of any cross-site Referer.
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		h.Set("Permissions-Policy", "bluetooth=(self), camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// post wraps a state-changing handler with origin and CSRF checks.
func (s *Server) post(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			u, err := url.Parse(o)
			if err != nil || u.Host != r.Host {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // room for a long strap session
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(s.csrf)) != 1 {
			http.Error(w, "stale or missing form token — reload the page", http.StatusForbidden)
			return
		}
		fn(w, r)
	}
}

func (s *Server) addFlash(level, format string, a ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flash = append(s.flash, flash{level, fmt.Sprintf(format, a...)})
}

func (s *Server) takeFlash() []flash {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.flash
	s.flash = nil
	return f
}

func back(w http.ResponseWriter, r *http.Request) {
	target := "/"
	if d := r.FormValue("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil {
			target = "/?days=" + strconv.Itoa(n)
		}
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	days := 30
	if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && (n == 7 || n == 30 || n == 90) {
		days = n
	}
	st, err := store.Open(s.Home)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	entries, skipped, err := st.Load()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	cfg, err := config.Load(s.Home)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	creds, err := whoop.LoadCredentials(s.Home)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	v := buildView(entries, cfg, creds, s.Now(), s.Loc, days)
	v.CSRF = s.csrf
	v.Flash = s.takeFlash()
	if skipped > 0 {
		v.Flash = append(v.Flash, flash{"watch", fmt.Sprintf("Skipped %d unreadable line(s) in %s.", skipped, st.Path())})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := page.Execute(w, v); err != nil {
		s.Log.Printf("render: %v", err)
	}
}

func (s *Server) logEntry(w http.ResponseWriter, r *http.Request) {
	e, m, err := entry.New(r.PostForm.Get("metric"), r.PostForm.Get("label"), r.PostForm.Get("value"), r.PostForm.Get("note"), s.Now().In(s.Loc))
	if err == nil {
		var st *store.Store
		if st, err = store.Open(s.Home); err == nil {
			err = st.Append(e)
		}
	}
	if err != nil {
		s.addFlash("watch", "Not logged: %v", err)
	} else {
		s.addFlash("good", "Logged %s.", entry.Describe(m, e))
	}
	back(w, r)
}

func (s *Server) deleteEntry(w http.ResponseWriter, r *http.Request) {
	st, err := store.Open(s.Home)
	if err == nil {
		_, err = st.Delete(r.PostForm.Get("id"))
	}
	if err != nil {
		s.addFlash("watch", "Not removed: %v", err)
	} else {
		s.addFlash("info", "Entry removed.")
	}
	back(w, r)
}

func (s *Server) client(c whoop.Credentials) *whoop.Client {
	cl := whoop.NewClient(c, func(c whoop.Credentials) error { return whoop.SaveCredentials(s.Home, c) })
	cl.Base, cl.HTTP, cl.Now = s.WhoopBase, s.HTTP, s.Now
	return cl
}

func (s *Server) whoopConnect(w http.ResponseWriter, r *http.Request) {
	c, err := whoop.LoadCredentials(s.Home)
	if err != nil || !c.Configured() {
		s.addFlash("watch", "Add your WHOOP app credentials first: vitals whoop setup --client-id … (see the WHOOP card).")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	// The redirect URI WHOOP sends the browser back to must be served by
	// this process; otherwise the code lands on a dead port.
	if u, err := url.Parse(c.RedirectURL); err != nil || u.Host != r.Host && !(IsLoopbackHost(u.Host) && samePort(u.Host, r.Host)) {
		s.addFlash("watch", "Your WHOOP redirect URL is %s, but this dashboard is on %s. Run vitals serve on that address or change the redirect with vitals whoop setup.", c.RedirectURL, r.Host)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	state := whoop.NewState()
	s.mu.Lock()
	now := s.Now()
	for k, t := range s.states {
		if now.Sub(t) > stateTTL {
			delete(s.states, k)
		}
	}
	s.states[state] = now
	s.mu.Unlock()
	http.Redirect(w, r, whoop.AuthURL(s.WhoopBase, c, state), http.StatusFound)
}

func samePort(a, b string) bool {
	_, pa, errA := net.SplitHostPort(a)
	_, pb, errB := net.SplitHostPort(b)
	return errA == nil && errB == nil && pa == pb
}

// consumeState checks and burns a login state value.
func (s *Server) consumeState(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.states[state]
	delete(s.states, state)
	return ok && s.Now().Sub(t) <= stateTTL
}

func (s *Server) whoopCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !s.consumeState(q.Get("state")) {
		s.addFlash("watch", "WHOOP login expired or didn't start here — click Connect WHOOP again.")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if e := q.Get("error"); e != "" {
		s.addFlash("watch", "WHOOP login was not completed (%s).", truncate(e, 80))
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	member, err := s.CompleteLogin(r.Context(), q.Get("code"))
	if err != nil {
		s.addFlash("watch", "WHOOP login failed: %v", err)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.addFlash("good", "Connected to WHOOP%s. Click Sync to pull your last %d days.", nameSuffix(member), whoop.FirstSyncDays)
	http.Redirect(w, r, "/", http.StatusSeeOther)
	if s.OnConnected != nil {
		s.OnConnected(member)
	}
}

// CompleteLogin exchanges an authorization code and records who connected.
func (s *Server) CompleteLogin(ctx context.Context, code string) (string, error) {
	s.init()
	if code == "" {
		return "", errors.New("no authorization code in the callback")
	}
	c, err := whoop.LoadCredentials(s.Home)
	if err != nil {
		return "", err
	}
	cl := s.client(c)
	if err := cl.Exchange(ctx, code); err != nil {
		return "", err
	}
	if p, err := cl.Profile(ctx); err == nil {
		cl.Creds.MemberName = p.FirstName
		if err := whoop.SaveCredentials(s.Home, cl.Creds); err != nil {
			return "", err
		}
	}
	return cl.Creds.MemberName, nil
}

func nameSuffix(name string) string {
	if name == "" {
		return ""
	}
	return " as " + name
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (s *Server) whoopSync(w http.ResponseWriter, r *http.Request) {
	c, err := whoop.LoadCredentials(s.Home)
	if err == nil && !c.Connected() {
		err = whoop.ErrNotConnected
	}
	var res whoop.Result
	if err == nil {
		var st *store.Store
		if st, err = store.Open(s.Home); err == nil {
			res, err = whoop.Sync(r.Context(), s.client(c), st, 0)
		}
	}
	if err != nil {
		s.addFlash("watch", "WHOOP sync failed: %v", err)
	} else {
		s.addFlash("good", "%s", SyncSummary(res))
	}
	back(w, r)
}

// SyncSummary is the one-line result shown by both the CLI and the web.
func SyncSummary(r whoop.Result) string {
	msg := fmt.Sprintf("WHOOP synced %s → %s: %d new, %d updated", r.From.Format("2 Jan"), r.To.Format("2 Jan"), r.Added, r.Updated)
	if r.Pending > 0 {
		msg += fmt.Sprintf(", %d still being scored by WHOOP (picked up next sync)", r.Pending)
	}
	return msg + "."
}

func (s *Server) whoopDisconnect(w http.ResponseWriter, r *http.Request) {
	c, err := whoop.LoadCredentials(s.Home)
	if err != nil {
		s.addFlash("watch", "%v", err)
		back(w, r)
		return
	}
	if c.Connected() {
		if err := s.client(c).Revoke(r.Context()); err != nil {
			s.Log.Printf("WHOOP revoke (continuing with local disconnect): %v", err)
		}
	}
	c.Disconnect()
	if err := whoop.SaveCredentials(s.Home, c); err != nil {
		s.addFlash("watch", "%v", err)
	} else {
		s.addFlash("info", "Disconnected from WHOOP. Synced entries stay in your log.")
	}
	back(w, r)
}

// Listen binds the dashboard on a loopback address. A "localhost:port"
// address binds both 127.0.0.1 and ::1 (when available), because browsers
// may resolve localhost to either.
func Listen(addr string) ([]net.Listener, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if !IsLoopbackHost(host) {
		return nil, fmt.Errorf("refusing to listen on %s: the dashboard is local-only (use localhost or 127.0.0.1)", host)
	}
	hosts := []string{host}
	if strings.EqualFold(host, "localhost") {
		hosts = []string{"127.0.0.1", "::1"}
	}
	var ls []net.Listener
	for i, h := range hosts {
		l, err := net.Listen("tcp", net.JoinHostPort(h, port))
		if err != nil {
			if i > 0 {
				continue // no IPv6 loopback is fine
			}
			return nil, err
		}
		ls = append(ls, l)
	}
	return ls, nil
}

// Serve runs the handler on every listener until ctx is cancelled.
func Serve(ctx context.Context, h http.Handler, ls []net.Listener) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, len(ls))
	for _, l := range ls {
		go func(l net.Listener) { errc <- srv.Serve(l) }(l)
	}
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	case err := <-errc:
		return err
	}
}

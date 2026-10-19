package server_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/server"
	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/tap/protocol"
	"github.com/rohithIlluri/POC-S/pocs/tap/sim"
)

type clock struct {
	mu     sync.Mutex
	offset time.Duration
}

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return time.Now().Add(c.offset) }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); c.offset += d; c.mu.Unlock() }

var phoneSeq atomic.Int64

func newServer(t *testing.T, mutate func(*server.Config)) *httptest.Server {
	t.Helper()
	cfg, err := server.DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DevOTP = true
	cfg.ClaimWait = 1500 * time.Millisecond
	cfg.Logf = nil
	if mutate != nil {
		mutate(&cfg)
	}
	srv, err := server.New(cfg, store.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

func onboard(t *testing.T, ts *httptest.Server, name string) *sim.Phone {
	t.Helper()
	p := sim.NewPhone(ts.URL, name)
	if err := p.Onboard(fmt.Sprintf("+1314555%04d", phoneSeq.Add(1))); err != nil {
		t.Fatalf("onboard %s: %v", name, err)
	}
	return p
}

func tap(t *testing.T, a, b *sim.Phone) (sim.Result, sim.Result) {
	t.Helper()
	return tapWith(t, a, b, sim.DefaultMeasurement(), sim.DefaultMeasurement())
}

func tapWith(t *testing.T, a, b *sim.Phone, ma, mb sim.Measurement) (sim.Result, sim.Result) {
	t.Helper()
	ra, rb, err := sim.Handshake(a, b, ma, mb)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	return ra, rb
}

func mustMatch(t *testing.T, r sim.Result) sim.Match {
	t.Helper()
	if r.Err != nil {
		t.Fatalf("want match, got %v", r.Err)
	}
	if r.Match.Status != "matched" {
		t.Fatalf("want matched, got %q", r.Match.Status)
	}
	return r.Match
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if got := sim.Code(err); got != code {
		t.Fatalf("want error %q, got %v", code, err)
	}
}

type peopleResp struct {
	Groups []struct {
		Title   string `json:"title"`
		EventID string `json:"event_id"`
		People  []struct {
			UserID   string `json:"user_id"`
			Name     string `json:"display_name"`
			TapCount int    `json:"tap_count"`
			Note     string `json:"note"`
			Place    string `json:"place"`
		} `json:"people"`
	} `json:"groups"`
	Total int `json:"total"`
}

func people(t *testing.T, p *sim.Phone) peopleResp {
	t.Helper()
	var out peopleResp
	if err := p.Do("GET", "/v1/people", nil, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTapCreatesEdgeWithVerifiableReceipt(t *testing.T) {
	ts := newServer(t, nil)
	alice, bob := onboard(t, ts, "Alice"), onboard(t, ts, "Bob")
	ra, rb := tap(t, alice, bob)
	ma, mb := mustMatch(t, ra), mustMatch(t, rb)

	if ma.Receipt.TapID != mb.Receipt.TapID {
		t.Fatalf("sides got different taps: %s vs %s", ma.Receipt.TapID, mb.Receipt.TapID)
	}
	if ma.Peer.DisplayName != "Bob" || mb.Peer.DisplayName != "Alice" {
		t.Errorf("peers: alice saw %q, bob saw %q", ma.Peer.DisplayName, mb.Peer.DisplayName)
	}
	var key struct {
		PublicKey string `json:"public_key"`
	}
	if err := alice.Do("GET", "/v1/receipt-key", nil, &key); err != nil {
		t.Fatal(err)
	}
	pub, err := protocol.ParsePublicKey(key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.Verify(pub, ma.Receipt.Canonical(), ma.Signature); err != nil {
		t.Errorf("receipt does not verify: %v", err)
	}

	pa := people(t, alice)
	if pa.Total != 1 || pa.Groups[0].People[0].Name != "Bob" {
		t.Fatalf("alice's people: %+v", pa)
	}
	if pa.Groups[0].People[0].Place == "" {
		t.Error("place should be visible by default")
	}

	// A repeat tap counts as a tap but not as a new edge.
	ra, _ = tap(t, alice, bob)
	if got := mustMatch(t, ra).TapCount; got != 2 {
		t.Errorf("tap_count after repeat = %d, want 2", got)
	}
	if got := people(t, alice).Total; got != 1 {
		t.Errorf("repeat tap created a second connection: %d", got)
	}
}

func TestReplayAndRetry(t *testing.T) {
	ts := newServer(t, nil)
	alice, bob := onboard(t, ts, "Alice"), onboard(t, ts, "Bob")
	sa, _ := alice.StartTap()
	sb, _ := bob.StartTap()
	ca, siga, _ := sa.Claim(sb.Nonce, sb.EphPub(), sim.DefaultMeasurement())
	cb, sigb, _ := sb.Claim(sa.Nonce, sa.EphPub(), sim.DefaultMeasurement())
	done := make(chan sim.Result)
	go func() { m, err := alice.Submit(ca, siga); done <- sim.Result{Match: m, Err: err} }()
	mb, err := bob.Submit(cb, sigb)
	if err != nil {
		t.Fatal(err)
	}
	ma := mustMatch(t, <-done)

	// A retried request returns the same receipt instead of a second tap.
	again, err := alice.Submit(ca, siga)
	if err != nil || again.Receipt.TapID != ma.Receipt.TapID || mb.Receipt.TapID != ma.Receipt.TapID {
		t.Fatalf("retry: %+v %v", again, err)
	}

	// Reusing a spent nonce in a new handshake is a replay.
	sc, _ := bob.StartTap()
	replay, sig, _ := sa.Claim(sc.Nonce, sc.EphPub(), sim.DefaultMeasurement())
	_, err = alice.Submit(replay, sig)
	wantCode(t, err, "nonce_used")
}

func TestRejectsBadClaims(t *testing.T) {
	far := sim.DefaultMeasurement()
	far.DistanceCM = 400 // a relay: the phones are in different rooms
	skewed := sim.DefaultMeasurement()
	skewed.ElapsedOffset = 5 * time.Second
	elsewhere := sim.DefaultMeasurement()
	elsewhere.Geohash = protocol.Geohash(40.7128, -74.0060, 7) // New York

	cases := []struct {
		name         string
		ma, mb       sim.Measurement
		codeA, codeB string // "" means the side times out (status expired)
	}{
		{"relay distance", far, sim.DefaultMeasurement(), "too_far", ""},
		{"skewed clock", skewed, sim.DefaultMeasurement(), "clock_mismatch", ""},
		{"different places", elsewhere, sim.DefaultMeasurement(), "location_mismatch", "location_mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := newServer(t, func(c *server.Config) { c.ClaimWait = 500 * time.Millisecond })
			a, b := onboard(t, ts, "A"), onboard(t, ts, "B")
			ra, rb := tapWith(t, a, b, tc.ma, tc.mb)
			check := func(side string, r sim.Result, code string) {
				if code == "" {
					if r.Err != nil || r.Match.Status != "expired" {
						t.Errorf("%s: want expired, got %+v %v", side, r.Match, r.Err)
					}
					return
				}
				if sim.Code(r.Err) != code {
					t.Errorf("%s: want %s, got %v", side, code, r.Err)
				}
			}
			check("A", ra, tc.codeA)
			check("B", rb, tc.codeB)
			if people(t, a).Total != 0 {
				t.Error("a rejected tap created an edge")
			}
		})
	}
}

// Each claim alone is within the 2 s skew of the server's clock, but the two
// measurements are 2.5 s apart: not one simultaneous tap.
func TestTimingMismatch(t *testing.T) {
	ts := newServer(t, nil)
	a, b := onboard(t, ts, "A"), onboard(t, ts, "B")
	sa, _ := a.StartTap()
	sb, _ := b.StartTap()
	time.Sleep(1400 * time.Millisecond)
	early, late := sim.DefaultMeasurement(), sim.DefaultMeasurement()
	early.ElapsedOffset, late.ElapsedOffset = -1250*time.Millisecond, 1250*time.Millisecond
	ca, siga, _ := sa.Claim(sb.Nonce, sb.EphPub(), early)
	cb, sigb, _ := sb.Claim(sa.Nonce, sa.EphPub(), late)
	errs := make(chan error, 2)
	go func() { _, err := a.Submit(ca, siga); errs <- err }()
	go func() { _, err := b.Submit(cb, sigb); errs <- err }()
	for range 2 {
		wantCode(t, <-errs, "timing_mismatch")
	}
}

func TestTamperedSignature(t *testing.T) {
	ts := newServer(t, nil)
	a, b := onboard(t, ts, "A"), onboard(t, ts, "B")
	sa, _ := a.StartTap()
	sb, _ := b.StartTap()
	c, sig, _ := sa.Claim(sb.Nonce, sb.EphPub(), sim.DefaultMeasurement())
	c.DistanceCM = 1
	_, err := a.Submit(c, sig)
	wantCode(t, err, "bad_signature")
}

func TestSelfTap(t *testing.T) {
	ts := newServer(t, nil)
	a := onboard(t, ts, "A")
	second := sim.NewPhone(ts.URL, "A's tablet")
	second.Token, second.UserID = a.Token, a.UserID
	if err := second.RegisterDevice(); err != nil {
		t.Fatal(err)
	}
	ra, rb := tap(t, a, second)
	if sim.Code(ra.Err) != "self_tap" && sim.Code(rb.Err) != "self_tap" {
		t.Fatalf("want self_tap, got %v / %v", ra.Err, rb.Err)
	}
}

func TestRemoveBlockAndReport(t *testing.T) {
	ts := newServer(t, nil)
	alice, bob := onboard(t, ts, "Alice"), onboard(t, ts, "Bob")
	mustMatch(t, func() sim.Result { r, _ := tap(t, alice, bob); return r }())

	if err := alice.Do("PUT", "/v1/people/"+bob.UserID+"/note", map[string]string{"text": "met at DevFest"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := people(t, alice).Groups[0].People[0].Note; got != "met at DevFest" {
		t.Errorf("note = %q", got)
	}
	if got := people(t, bob).Groups[0].People[0].Note; got != "" {
		t.Errorf("bob can see alice's private note: %q", got)
	}
	if err := bob.Do("PUT", "/v1/people/"+alice.UserID+"/place", map[string]bool{"hidden": true}, nil); err != nil {
		t.Fatal(err)
	}
	if got := people(t, alice).Groups[0].People[0].Place; got != "" {
		t.Errorf("place hidden by bob still shown to alice: %q", got)
	}

	// Silent removal: gone for both sides.
	if err := alice.Do("DELETE", "/v1/people/"+bob.UserID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if people(t, alice).Total != 0 || people(t, bob).Total != 0 {
		t.Fatal("removed edge still listed")
	}
	// A fresh tap is fresh consent and restores the connection.
	mustMatch(t, func() sim.Result { r, _ := tap(t, alice, bob); return r }())
	if people(t, bob).Total != 1 {
		t.Fatal("re-tap did not restore the connection")
	}
	// Blocking removes the edge and refuses future taps.
	if err := alice.Do("POST", "/v1/blocks", map[string]string{"user_id": bob.UserID}, nil); err != nil {
		t.Fatal(err)
	}
	ra, rb := tap(t, alice, bob)
	if sim.Code(ra.Err) != "blocked" || sim.Code(rb.Err) != "blocked" {
		t.Fatalf("want blocked, got %v / %v", ra.Err, rb.Err)
	}
	err := bob.Do("PUT", "/v1/people/"+alice.UserID+"/note", map[string]string{"text": "x"}, nil)
	wantCode(t, err, "not_connected")
	// Strangers can't be reported (or probed).
	carol := onboard(t, ts, "Carol")
	err = carol.Do("POST", "/v1/reports", map[string]string{"user_id": alice.UserID, "reason": "spam"}, nil)
	wantCode(t, err, "not_connected")
}

func TestRepeatReportsFreezeTapping(t *testing.T) {
	ts := newServer(t, nil)
	mallory := onboard(t, ts, "Mallory")
	for i := range 3 {
		p := onboard(t, ts, fmt.Sprintf("P%d", i))
		mustMatch(t, func() sim.Result { r, _ := tap(t, p, mallory); return r }())
		if err := p.Do("POST", "/v1/reports", map[string]string{"user_id": mallory.UserID, "reason": "harassment"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	_, err := mallory.StartTap()
	wantCode(t, err, "frozen")
}

func TestEventModeAndRecap(t *testing.T) {
	ts := newServer(t, nil)
	var ps []*sim.Phone
	for i := range 6 {
		ps = append(ps, onboard(t, ts, fmt.Sprintf("Guest%d", i)))
	}
	host := ps[0]
	// Five taps in one place within the hour form an event.
	for i := 1; i <= 4; i++ {
		r, _ := tap(t, host, ps[i])
		if mustMatch(t, r).EventID != "" {
			t.Fatalf("event formed after only %d taps", i)
		}
	}
	r, _ := tap(t, ps[4], ps[5])
	evID := mustMatch(t, r).EventID
	if evID == "" {
		t.Fatal("fifth tap did not form an event")
	}
	// Later taps there join it.
	r, _ = tap(t, host, ps[5])
	if got := mustMatch(t, r).EventID; got != evID {
		t.Errorf("sixth tap joined %q, want %q", got, evID)
	}

	var evs struct {
		Events []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			PeopleMet int    `json:"people_met"`
		} `json:"events"`
	}
	if err := host.Do("GET", "/v1/events", nil, &evs); err != nil {
		t.Fatal(err)
	}
	if len(evs.Events) != 1 || evs.Events[0].PeopleMet != 5 {
		t.Fatalf("host events: %+v", evs)
	}
	if err := host.Do("PUT", "/v1/events/"+evID, map[string]string{"name": "DevFest"}, nil); err != nil {
		t.Fatal(err)
	}
	err := ps[1].Do("PUT", "/v1/events/"+evID, map[string]string{"name": "Other"}, nil)
	wantCode(t, err, "already_named")

	var recap struct {
		Headline string `json:"headline"`
	}
	if err := host.Do("GET", "/v1/events/"+evID+"/recap", nil, &recap); err != nil {
		t.Fatal(err)
	}
	if recap.Headline != "Met 5 humans at DevFest. Zero bots." {
		t.Errorf("headline = %q", recap.Headline)
	}
	resp, err := http.Get(ts.URL + "/v1/events/" + evID + "/recap.svg?token=" + host.Token)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "DevFest") {
		t.Errorf("recap.svg: %d %.80s", resp.StatusCode, body)
	}
	if g := people(t, host).Groups; len(g) == 0 || g[0].Title != "DevFest" {
		t.Errorf("people not grouped by event: %+v", g)
	}
}

func TestRateLimit(t *testing.T) {
	ts := newServer(t, func(c *server.Config) { c.TapsPerDevicePerHour = 2 })
	a, b := onboard(t, ts, "A"), onboard(t, ts, "B")
	for range 2 {
		mustMatch(t, func() sim.Result { r, _ := tap(t, a, b); return r }())
	}
	ra, _ := tap(t, a, b)
	wantCode(t, ra.Err, "rate_limited")
}

func TestNonceExpires(t *testing.T) {
	clk := &clock{}
	ts := newServer(t, func(c *server.Config) { c.Now = clk.Now })
	a, b := onboard(t, ts, "A"), onboard(t, ts, "B")
	sa, _ := a.StartTap()
	sb, _ := b.StartTap()
	clk.Advance(30 * time.Second)
	m := sim.DefaultMeasurement()
	m.ElapsedOffset = 30 * time.Second // the phone's clock agrees; the nonce is simply old
	c, sig, _ := sa.Claim(sb.Nonce, sb.EphPub(), m)
	_, err := a.Submit(c, sig)
	wantCode(t, err, "nonce_expired")
}

func TestDeleteAccountRemovesEverything(t *testing.T) {
	ts := newServer(t, nil)
	alice, bob := onboard(t, ts, "Alice"), onboard(t, ts, "Bob")
	mustMatch(t, func() sim.Result { r, _ := tap(t, alice, bob); return r }())
	var export struct {
		Connections []struct {
			Name string `json:"display_name"`
		} `json:"connections"`
	}
	if err := alice.Do("GET", "/v1/me/export", nil, &export); err != nil || len(export.Connections) != 1 {
		t.Fatalf("export: %+v %v", export, err)
	}
	if err := alice.Do("DELETE", "/v1/me", nil, nil); err != nil {
		t.Fatal(err)
	}
	if people(t, bob).Total != 0 {
		t.Error("deleted user still in bob's people")
	}
	err := alice.Do("GET", "/v1/me", nil, nil)
	wantCode(t, err, "unauthenticated")
}

func TestOTPLocksAfterWrongCodes(t *testing.T) {
	ts := newServer(t, nil)
	p := sim.NewPhone(ts.URL, "X")
	if err := p.Do("POST", "/v1/auth/otp/start", map[string]string{"phone": "+13145559999"}, nil); err != nil {
		t.Fatal(err)
	}
	var err error
	for range 6 {
		err = p.Do("POST", "/v1/auth/otp/verify", map[string]string{"phone": "+13145559999", "code": "nope", "display_name": "X"}, nil)
	}
	wantCode(t, err, "otp_locked")
	err = p.Do("POST", "/v1/auth/otp/start", map[string]string{"phone": "555"}, nil)
	wantCode(t, err, "bad_phone")
}

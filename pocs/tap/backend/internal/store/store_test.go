package store_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store/storetest"
	"github.com/rohithIlluri/POC-S/pocs/tap/protocol"
)

// These tests are the contract every Store implementation must meet. They
// run against memory by default and against Postgres with TAP_TEST_PG set.

var t0 = time.Date(2026, 10, 19, 18, 0, 0, 0, time.UTC)

func tx(t *testing.T, st store.Store, fn func(store.Tx)) {
	t.Helper()
	if err := st.Tx(func(tx store.Tx) error { fn(tx); return nil }); err != nil {
		t.Fatalf("%s: %v", storetest.Kind(), err)
	}
}

func seedUsers(t *testing.T, st store.Store, ids ...string) {
	tx(t, st, func(x store.Tx) {
		for _, id := range ids {
			x.PutUser(store.User{ID: id, DisplayName: id, PhoneHash: "h_" + id, CreatedAt: t0})
		}
	})
}

func TestUsersSessionsDevices(t *testing.T) {
	st := storetest.New(t)
	seedUsers(t, st, "usr_a")
	tx(t, st, func(x store.Tx) {
		if u, ok := x.UserByPhone("h_usr_a"); !ok || u.ID != "usr_a" || !u.CreatedAt.Equal(t0) {
			t.Errorf("UserByPhone = %+v %v", u, ok)
		}
		if _, ok := x.User("usr_missing"); ok {
			t.Error("missing user found")
		}
		u, _ := x.User("usr_a")
		u.Frozen = true
		x.PutUser(u)
		x.PutSession("tok", "usr_a", t0.Add(time.Hour))
		x.PutDevice(store.Device{ID: "dev_2", UserID: "usr_a", PublicKey: "k2", AttestStatus: "dev", CreatedAt: t0.Add(time.Second)})
		x.PutDevice(store.Device{ID: "dev_1", UserID: "usr_a", PublicKey: "k1", AttestStatus: "dev", CreatedAt: t0})
	})
	tx(t, st, func(x store.Tx) {
		if u, _ := x.User("usr_a"); !u.Frozen {
			t.Error("PutUser update lost")
		}
		if id, ok := x.SessionUser("tok", t0); !ok || id != "usr_a" {
			t.Errorf("SessionUser = %q %v", id, ok)
		}
		if _, ok := x.SessionUser("tok", t0.Add(2*time.Hour)); ok {
			t.Error("expired session accepted")
		}
		ds := x.DevicesOf("usr_a")
		if len(ds) != 2 || ds[0].ID != "dev_1" {
			t.Errorf("DevicesOf order = %+v", ds)
		}
	})
}

func TestPendingClaims(t *testing.T) {
	st := storetest.New(t)
	claim := func(dev string, dist int) store.PendingClaim {
		return store.PendingClaim{PairKey: "a.b", UserID: "u_" + dev, IssuedAt: t0, ReceivedAt: t0,
			Claim: protocol.Claim{DeviceID: dev, Nonce: "a", PeerNonce: "b", DistanceCM: dist, Geohash: "9yzgez3", Transport: protocol.TransportUWB}}
	}
	tx(t, st, func(x store.Tx) {
		x.PutPendingClaim(claim("d1", 4))
		x.PutPendingClaim(claim("d1", 5)) // a retry replaces, not appends
		x.PutPendingClaim(claim("d2", 6))
	})
	tx(t, st, func(x store.Tx) {
		pcs := x.PendingClaims("a.b")
		if len(pcs) != 2 || pcs[0].Claim.DistanceCM != 5 || pcs[0].Claim.Transport != protocol.TransportUWB {
			t.Fatalf("PendingClaims = %+v", pcs)
		}
		x.DeletePendingClaim("a.b", "d1")
	})
	tx(t, st, func(x store.Tx) {
		if pcs := x.PendingClaims("a.b"); len(pcs) != 1 || pcs[0].Claim.DeviceID != "d2" {
			t.Errorf("after delete = %+v", pcs)
		}
	})
}

func TestTapsEdgesAndEvents(t *testing.T) {
	st := storetest.New(t)
	seedUsers(t, st, "usr_a", "usr_b", "usr_c")
	mk := func(id, a, b string, at time.Time, gh string) store.Tap {
		return store.Tap{ID: id, UserA: a, UserB: b, DeviceA: "d_" + a, DeviceB: "d_" + b, CreatedAt: at,
			Geohash: gh, PairKey: "pk_" + id, Signature: "sig",
			Receipt: protocol.Receipt{TapID: id, UserA: a, UserB: b, CreatedAt: at.UnixMilli(), Geohash: gh, PairKey: "pk_" + id}}
	}
	tx(t, st, func(x store.Tx) {
		x.PutTap(mk("tap_1", "usr_a", "usr_b", t0, "9yzgez3"))
		x.PutTap(mk("tap_2", "usr_a", "usr_c", t0.Add(time.Minute), "9yzgez4"))
		x.PutTap(mk("tap_3", "usr_b", "usr_c", t0.Add(2*time.Hour), "dr5regw")) // elsewhere, later
		x.PutEdge(store.Edge{UserA: "usr_a", UserB: "usr_b", CreatedAt: t0, FirstTapID: "tap_1", LastTapAt: t0, TapCount: 1, Status: store.EdgeActive})
		x.PutMeta(store.EdgeMeta{OwnerID: "usr_a", OtherID: "usr_b", Note: "DevFest", HidePlace: true})
		x.PutBlock("usr_c", "usr_a")
		x.PutReport(store.Report{ID: "r1", ReporterID: "usr_a", SubjectID: "usr_c", Reason: "spam", CreatedAt: t0})
		x.PutReport(store.Report{ID: "r2", ReporterID: "usr_a", SubjectID: "usr_c", Reason: "spam", CreatedAt: t0})
		x.PutReport(store.Report{ID: "r3", ReporterID: "usr_b", SubjectID: "usr_c", Reason: "spam", CreatedAt: t0})
		x.PutEvent(store.Event{ID: "evt_1", Geohash6: "9yzgez", Start: t0, End: t0.Add(time.Minute), TapCount: 2, Members: []string{"usr_a", "usr_b", "usr_c", "deleted"}})
	})
	tx(t, st, func(x store.Tx) {
		tp, ok := x.TapByPairKey("pk_tap_1")
		if !ok || tp.Receipt.TapID != "tap_1" || tp.DeviceB != "d_usr_b" {
			t.Errorf("TapByPairKey = %+v %v", tp, ok)
		}
		tp.EventID = "evt_1"
		x.PutTap(tp)
		near := x.TapsNearSince("9yzgez", t0)
		if len(near) != 2 || near[0].ID != "tap_1" || near[1].ID != "tap_2" {
			t.Errorf("TapsNearSince = %v", ids(near))
		}
		if n := x.TapsByDeviceSince("d_usr_a", t0.Add(30*time.Second)); n != 1 {
			t.Errorf("TapsByDeviceSince = %d", n)
		}
		e, ok := x.Edge("usr_b", "usr_a") // either order
		if !ok || e.TapCount != 1 {
			t.Errorf("Edge = %+v %v", e, ok)
		}
		e.TapCount, e.Status = 2, store.EdgeRemovedByA
		x.PutEdge(e)
		if m := x.Meta("usr_b", "usr_a"); m.Note != "" || m.OwnerID != "usr_b" {
			t.Errorf("default meta = %+v", m)
		}
		if !x.Blocked("usr_a", "usr_c") || !x.Blocked("usr_c", "usr_a") || x.Blocked("usr_a", "usr_b") {
			t.Error("Blocked is not symmetric")
		}
		if n := x.DistinctReporters("usr_c"); n != 2 {
			t.Errorf("DistinctReporters = %d", n)
		}
		ev, ok := x.ActiveEventNear("9yzgez", t0)
		if !ok || ev.ID != "evt_1" || len(ev.Members) != 3 {
			t.Errorf("ActiveEventNear = %+v %v", ev, ok)
		}
		if _, ok := x.ActiveEventNear("9yzgez", t0.Add(time.Hour)); ok {
			t.Error("stale event still active")
		}
		ev.Members = []string{"usr_a", "usr_b"}
		ev.Name = "DevFest"
		x.PutEvent(ev)
	})
	tx(t, st, func(x store.Tx) {
		if tp, _ := x.Tap("tap_1"); tp.EventID != "evt_1" {
			t.Errorf("PutTap update lost: %+v", tp)
		}
		if e, _ := x.Edge("usr_a", "usr_b"); e.TapCount != 2 || e.Status != store.EdgeRemovedByA {
			t.Errorf("PutEdge update lost: %+v", e)
		}
		if evs := x.EventsOf("usr_c"); len(evs) != 0 {
			t.Errorf("removed member still sees event: %+v", evs)
		}
		if evs := x.EventsOf("usr_a"); len(evs) != 1 || evs[0].Name != "DevFest" {
			t.Errorf("EventsOf = %+v", evs)
		}
		x.DeleteUser("usr_a")
	})
	tx(t, st, func(x store.Tx) {
		if _, ok := x.UserByPhone("h_usr_a"); ok {
			t.Error("deleted user still found by phone")
		}
		if len(x.EdgesOf("usr_b")) != 0 {
			t.Error("deleted user's edges remain")
		}
		if tp, _ := x.Tap("tap_1"); tp.UserA != "deleted" || tp.DeviceA != "" {
			t.Errorf("tap not anonymised: %+v", tp)
		}
		if x.Blocked("usr_c", "usr_a") || x.DistinctReporters("usr_c") != 1 {
			t.Error("deleted user's blocks/reports remain")
		}
		if ev, _ := x.Event("evt_1"); len(ev.Members) != 1 || ev.Members[0] != "usr_b" {
			t.Errorf("event members after delete = %v", ev.Members)
		}
	})
}

func ids(ts []store.Tap) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

// TestCheckThenInsertIsAtomic is the property the matching engine relies
// on: many concurrent transactions doing "if no tap for this pair, insert
// one" produce exactly one tap.
func TestCheckThenInsertIsAtomic(t *testing.T) {
	st := storetest.New(t)
	seedUsers(t, st, "usr_a", "usr_b")
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inserted := false
			err := st.Tx(func(x store.Tx) error {
				inserted = false
				if _, ok := x.TapByPairKey("pk_race"); ok {
					return nil
				}
				x.PutTap(store.Tap{ID: "tap_race_" + string(rune('a'+i)), UserA: "usr_a", UserB: "usr_b", CreatedAt: t0, Geohash: "9yzgez3", PairKey: "pk_race"})
				inserted = true
				return nil
			})
			if err != nil {
				t.Errorf("tx: %v", err)
				return
			}
			if inserted {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if won != 1 {
		t.Fatalf("%s: %d transactions inserted a tap for the same pair, want 1", storetest.Kind(), won)
	}
}

func TestTxReturnsFnError(t *testing.T) {
	st := storetest.New(t)
	sentinel := errors.New("stop")
	if err := st.Tx(func(store.Tx) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("Tx = %v, want the fn's error", err)
	}
}

func TestPropsRoundsAndVotes(t *testing.T) {
	st := storetest.New(t)
	seedUsers(t, st, "usr_a", "usr_b", "usr_c")
	tx(t, st, func(x store.Tx) {
		x.PutPropsRound(store.PropsRound{UserID: "usr_a", Week: "2026-W43", CreatedAt: t0, Polls: []store.PropsPoll{
			{PromptID: "p_coffee", Options: []string{"usr_b", "usr_c"}},
			{PromptID: "p_story", Options: []string{"usr_c", "usr_b"}},
		}})
		x.PutPropsVote(store.PropsVote{ID: "v1", VoterID: "usr_a", PickedID: "usr_b", PromptID: "p_coffee", Week: "2026-W43", CreatedAt: t0})
		x.PutPropsVote(store.PropsVote{ID: "v2", VoterID: "usr_c", PickedID: "usr_b", PromptID: "p_story", Week: "2026-W43", EventID: "evt_1", CreatedAt: t0.Add(time.Minute)})
		x.PutPropsVote(store.PropsVote{ID: "v3", VoterID: "usr_b", PickedID: "usr_c", PromptID: "p_story", Week: "2026-W43", CreatedAt: t0})
	})
	tx(t, st, func(x store.Tx) {
		r, ok := x.PropsRound("usr_a", "2026-W43")
		if !ok || len(r.Polls) != 2 || r.Polls[1].Options[0] != "usr_c" || r.Polls[0].Answered {
			t.Fatalf("PropsRound = %+v %v", r, ok)
		}
		r.Polls[0].Answered = true
		x.PutPropsRound(r)
		if _, ok := x.PropsRound("usr_a", "2026-W44"); ok {
			t.Error("round found for the wrong week")
		}
		vs := x.PropsVotesFor("usr_b", t0)
		if len(vs) != 2 || vs[0].ID != "v2" || vs[0].EventID != "evt_1" {
			t.Errorf("PropsVotesFor = %+v", vs)
		}
		if vs := x.PropsVotesFor("usr_b", t0.Add(30*time.Second)); len(vs) != 1 {
			t.Errorf("since filter: %+v", vs)
		}
	})
	tx(t, st, func(x store.Tx) {
		if r, _ := x.PropsRound("usr_a", "2026-W43"); !r.Polls[0].Answered {
			t.Error("answered flag lost")
		}
		x.DeleteUser("usr_c")
	})
	tx(t, st, func(x store.Tx) {
		// usr_c's vote for usr_b and usr_b's vote for usr_c both go with the account.
		if vs := x.PropsVotesFor("usr_b", t0); len(vs) != 1 || vs[0].VoterID != "usr_a" {
			t.Errorf("after delete: %+v", vs)
		}
		x.DeleteUser("usr_a")
		if _, ok := x.PropsRound("usr_a", "2026-W43"); ok {
			t.Error("deleted user's round remains")
		}
	})
}

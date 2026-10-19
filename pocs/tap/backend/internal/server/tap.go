package server

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/tap/protocol"
)

// matchResult is what a waiting first claimant receives when its partner's
// claim is decided.
type matchResult struct {
	tap store.Tap
	err *apiError
}

func (s *Server) handleNonce(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	nonce := randomB64(16)
	err := s.st.Tx(func(tx store.Tx) error {
		if err := s.checkDevice(tx, userID, req.DeviceID); err != nil {
			return err
		}
		tx.PutNonce(store.Nonce{Value: nonce, DeviceID: req.DeviceID, IssuedAt: s.cfg.Now()})
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"nonce": nonce, "expires_in_ms": s.cfg.NonceTTL.Milliseconds()})
}

func (s *Server) checkDevice(tx store.Tx, userID, deviceID string) error {
	d, ok := tx.Device(deviceID)
	if !ok || d.UserID != userID {
		return fail(403, "device_unknown", "register this device first")
	}
	if u, _ := tx.User(userID); u.Frozen {
		return fail(403, "frozen", "tapping is paused on this account pending review")
	}
	return nil
}

// handleClaim accepts one phone's signed claim. The first claim of a
// handshake waits up to ClaimWait for its partner; the second decides the
// match for both. Every path is idempotent on the nonce pair, so a retried
// request gets the same receipt instead of a second edge.
func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		Claim     protocol.Claim `json:"claim"`
		Signature string         `json:"signature"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	c := req.Claim
	if err := c.Validate(); err != nil {
		writeErr(w, fail(400, "bad_claim", "%v", err))
		return
	}
	key := c.PairKey()

	var (
		matched *store.Tap
		pairErr *apiError
		waiter  chan matchResult
	)
	err := s.st.Tx(func(tx store.Tx) error {
		now := s.cfg.Now()
		if err := s.checkDevice(tx, userID, c.DeviceID); err != nil {
			return err
		}
		d, _ := tx.Device(c.DeviceID)
		pub, err := protocol.ParsePublicKey(d.PublicKey)
		if err != nil {
			return fail(500, "internal", "stored device key unreadable")
		}
		if err := protocol.Verify(pub, c.Canonical(), req.Signature); err != nil {
			return fail(401, "bad_signature", "%v", err)
		}
		if tp, ok := tx.TapByPairKey(key); ok {
			if tp.DeviceA == c.DeviceID || tp.DeviceB == c.DeviceID {
				matched = &tp // a retry of a claim that already matched
				return nil
			}
			return fail(409, "handshake_used", "this handshake already produced a tap")
		}
		n, ok := tx.Nonce(c.Nonce)
		if !ok || n.DeviceID != c.DeviceID {
			return fail(400, "nonce_unknown", "nonce was not issued to this device")
		}
		if n.Used {
			return fail(409, "nonce_used", "nonce already used (replay)")
		}
		elapsed := now.Sub(n.IssuedAt)
		if elapsed > s.cfg.NonceTTL {
			return fail(410, "nonce_expired", "nonce older than %s", s.cfg.NonceTTL)
		}
		if absDur(elapsed-time.Duration(c.ElapsedMS)*time.Millisecond) > s.cfg.MaxSkew {
			return fail(400, "clock_mismatch", "claimed elapsed time disagrees with the server's")
		}
		if max := s.cfg.MaxDistanceCM[c.Transport]; c.DistanceCM > max {
			return fail(422, "too_far", "measured %d cm; %s allows %d cm", c.DistanceCM, c.Transport, max)
		}
		if tx.TapsByDeviceSince(c.DeviceID, now.Add(-time.Hour)) >= s.cfg.TapsPerDevicePerHour {
			return fail(429, "rate_limited", "tap limit reached for this hour")
		}

		mine := store.PendingClaim{PairKey: key, UserID: userID, Claim: c, IssuedAt: n.IssuedAt, ReceivedAt: now}
		var partner *store.PendingClaim
		for _, p := range tx.PendingClaims(key) {
			if p.Claim.DeviceID != c.DeviceID {
				partner = &p
				break
			}
		}
		if partner == nil {
			tx.PutPendingClaim(mine)
			waiter = s.register(key)
			return nil
		}
		if err := s.checkPair(tx, mine, *partner, now); err != nil {
			tx.DeletePendingClaim(key, partner.Claim.DeviceID)
			pairErr = err
			return nil
		}
		tp, err := s.commit(tx, mine, *partner, now)
		if err != nil {
			return err
		}
		matched = &tp
		return nil
	})
	switch {
	case err != nil:
		writeErr(w, err)
	case pairErr != nil:
		s.notify(key, matchResult{err: pairErr})
		writeErr(w, pairErr)
	case matched != nil:
		s.notify(key, matchResult{tap: *matched})
		s.writeMatch(w, userID, *matched)
	default:
		s.awaitPartner(r.Context(), w, userID, c, waiter)
	}
}

func (s *Server) awaitPartner(ctx context.Context, w http.ResponseWriter, userID string, c protocol.Claim, ch chan matchResult) {
	key := c.PairKey()
	timer := time.NewTimer(s.cfg.ClaimWait)
	defer timer.Stop()
	select {
	case res := <-ch:
		if res.err != nil {
			writeErr(w, res.err)
			return
		}
		s.writeMatch(w, userID, res.tap)
		return
	case <-timer.C:
	case <-ctx.Done():
	}
	s.unregister(key, ch)
	// The partner may have matched in the instant the wait ended.
	var tp *store.Tap
	_ = s.st.Tx(func(tx store.Tx) error {
		if t, ok := tx.TapByPairKey(key); ok {
			tp = &t
			return nil
		}
		tx.DeletePendingClaim(key, c.DeviceID)
		return nil
	})
	if tp != nil {
		s.writeMatch(w, userID, *tp)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "expired"})
}

// checkPair decides whether two claims describe one physical tap.
func (s *Server) checkPair(tx store.Tx, mine, partner store.PendingClaim, now time.Time) *apiError {
	if partner.UserID == mine.UserID {
		return fail(400, "self_tap", "both phones belong to the same account")
	}
	if !mine.Claim.Mirrors(partner.Claim) {
		return fail(400, "claims_disagree", "the two phones saw different keys or nonces")
	}
	if now.Sub(partner.ReceivedAt) > s.cfg.ClaimWait {
		return fail(410, "partner_expired", "the other phone's claim timed out")
	}
	// Compare the two measurement moments on the server's clock.
	at := func(p store.PendingClaim) time.Time {
		return p.IssuedAt.Add(time.Duration(p.Claim.ElapsedMS) * time.Millisecond)
	}
	if absDur(at(mine).Sub(at(partner))) > s.cfg.MaxSkew {
		return fail(400, "timing_mismatch", "the phones measured more than %s apart", s.cfg.MaxSkew)
	}
	if mine.Claim.Geohash[:6] != partner.Claim.Geohash[:6] {
		return fail(400, "location_mismatch", "the phones reported different places")
	}
	if tx.Blocked(mine.UserID, partner.UserID) {
		return fail(403, "blocked", "these accounts can't tap")
	}
	if u, _ := tx.User(partner.UserID); u.Frozen {
		return fail(403, "frozen", "tapping is paused on the other account")
	}
	return nil
}

// commit writes the tap, the edge and any event in one transaction. The
// receipt is signed before the first write so a signing failure leaves no
// partial state.
func (s *Server) commit(tx store.Tx, mine, partner store.PendingClaim, now time.Time) (store.Tap, error) {
	a, b := store.Pair(mine.UserID, partner.UserID)
	devA, devB := mine.Claim.DeviceID, partner.Claim.DeviceID
	if a != mine.UserID {
		devA, devB = devB, devA
	}
	tp := store.Tap{
		ID: newID("tap_"), UserA: a, UserB: b, DeviceA: devA, DeviceB: devB,
		CreatedAt: now, Geohash: mine.Claim.Geohash, PairKey: mine.PairKey,
	}
	tp.Receipt = protocol.Receipt{TapID: tp.ID, UserA: a, UserB: b, CreatedAt: now.UnixMilli(), Geohash: tp.Geohash, PairKey: tp.PairKey}
	sig, err := protocol.Sign(s.cfg.ReceiptKey, tp.Receipt.Canonical())
	if err != nil {
		return store.Tap{}, fail(500, "internal", "receipt signing failed")
	}
	tp.Signature = sig

	for _, v := range []string{mine.Claim.Nonce, partner.Claim.Nonce} {
		n, _ := tx.Nonce(v)
		n.Used = true
		tx.PutNonce(n)
	}
	tx.PutTap(tp)
	if id := s.assignEvent(tx, tp, now); id != "" {
		tp.EventID = id
		tx.PutTap(tp)
	}
	e, ok := tx.Edge(a, b)
	if !ok {
		e = store.Edge{UserA: a, UserB: b, CreatedAt: now, FirstTapID: tp.ID}
	}
	e.Status = store.EdgeActive // a new tap after a removal is fresh mutual consent
	e.TapCount++
	e.LastTapAt = now
	tx.PutEdge(e)
	tx.DeletePendingClaim(mine.PairKey, devA)
	tx.DeletePendingClaim(mine.PairKey, devB)
	return tp, nil
}

// assignEvent implements event mode: EventMinTaps taps in one ~1 km cell
// (geohash 6) within EventWindow become an event; later taps there join it.
func (s *Server) assignEvent(tx store.Tx, tp store.Tap, now time.Time) string {
	g6 := tp.Geohash[:6]
	since := now.Add(-s.cfg.EventWindow)
	if ev, ok := tx.ActiveEventNear(g6, since); ok {
		ev.End = now
		ev.TapCount++
		ev.Members = addMembers(ev.Members, tp.UserA, tp.UserB)
		tx.PutEvent(ev)
		return ev.ID
	}
	var loose []store.Tap
	for _, t := range tx.TapsNearSince(g6, since) {
		if t.EventID == "" {
			loose = append(loose, t)
		}
	}
	if len(loose) < s.cfg.EventMinTaps {
		return ""
	}
	ev := store.Event{ID: newID("evt_"), Geohash6: g6, Start: loose[0].CreatedAt, End: now, TapCount: len(loose)}
	for _, t := range loose {
		ev.Members = addMembers(ev.Members, t.UserA, t.UserB)
		if t.ID != tp.ID {
			t.EventID = ev.ID
			tx.PutTap(t)
		}
	}
	tx.PutEvent(ev)
	return ev.ID
}

func addMembers(list []string, ids ...string) []string {
	for _, id := range ids {
		if !slices.Contains(list, id) {
			list = append(list, id)
		}
	}
	return list
}

func (s *Server) writeMatch(w http.ResponseWriter, userID string, tp store.Tap) {
	other := tp.UserA
	if other == userID {
		other = tp.UserB
	}
	var peer store.User
	var count int
	_ = s.st.Tx(func(tx store.Tx) error {
		peer, _ = tx.User(other)
		e, _ := tx.Edge(tp.UserA, tp.UserB)
		count = e.TapCount
		return nil
	})
	writeJSON(w, 200, map[string]any{
		"status":    "matched",
		"receipt":   tp.Receipt,
		"signature": tp.Signature,
		"event_id":  tp.EventID,
		"tap_count": count, // taps between these two people, this one included
		"peer":      map[string]string{"user_id": peer.ID, "display_name": peer.DisplayName, "photo_url": peer.PhotoURL},
	})
}

func (s *Server) register(key string) chan matchResult {
	ch := make(chan matchResult, 1)
	s.mu.Lock()
	s.wait[key] = ch
	s.mu.Unlock()
	return ch
}

func (s *Server) unregister(key string, ch chan matchResult) {
	s.mu.Lock()
	if s.wait[key] == ch {
		delete(s.wait, key)
	}
	s.mu.Unlock()
}

func (s *Server) notify(key string, res matchResult) {
	s.mu.Lock()
	ch, ok := s.wait[key]
	delete(s.wait, key)
	s.mu.Unlock()
	if ok {
		ch <- res
	}
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

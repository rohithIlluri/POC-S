package server

import (
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
)

// The event worker is the consumer the design puts behind Pub/Sub
// `tap.created`: it assigns committed taps to events. Every tap at a venue
// lands in the same geohash cell, so doing this inside the claim
// transaction made all of a venue's claims conflict with each other; one
// worker processing taps in commit order keeps the claim path free of it.
//
// The queue is in-process, so taps queued when the process stops are not
// assigned. The Pub/Sub version gets at-least-once delivery instead.

func (s *Server) enqueueTap(id string) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if s.queueClosed {
		// A claim that outlived shutdown's grace period: assign it here
		// rather than send on the closed queue.
		s.processTap(id)
		return
	}
	s.eventsIdle.Add(1)
	s.tapCreated <- id
}

// runJanitor prunes nonces and pending claims too old to matter: an old
// nonce is rejected as unknown just as it would be as expired, and no
// pending claim outlives ClaimWait. Without it both tables grow forever.
func (s *Server) runJanitor() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.stopJanitor:
			return
		case <-t.C:
			s.pruneHandshakes()
		}
	}
}

func (s *Server) pruneHandshakes() {
	cutoff := s.cfg.Now().Add(-(s.cfg.NonceTTL + s.cfg.ClaimWait + time.Minute))
	if err := s.st.Tx(func(tx store.Tx) error { tx.PruneHandshakes(cutoff); return nil }); err != nil {
		s.cfg.Logf("prune handshakes: %v", err)
	}
}

func (s *Server) runEventWorker() {
	defer close(s.workerDone)
	for id := range s.tapCreated {
		s.processTap(id)
		s.eventsIdle.Done()
	}
}

func (s *Server) processTap(id string) {
	err := s.st.Tx(func(tx store.Tx) error {
		tp, ok := tx.Tap(id)
		if !ok || tp.EventID != "" {
			return nil
		}
		if evID := s.assignEvent(tx, tp); evID != "" {
			tp.EventID = evID
			tx.PutTap(tp)
		}
		return nil
	})
	if err != nil {
		s.cfg.Logf("event assignment for %s: %v", id, err)
	}
}

// Flush waits until every committed tap has been through the event worker.
func (s *Server) Flush() { s.eventsIdle.Wait() }

// Close stops the event worker after it drains the queue. Taps committed
// by requests still running afterwards are assigned inline.
func (s *Server) Close() {
	s.queueMu.Lock()
	if !s.queueClosed {
		s.queueClosed = true
		close(s.tapCreated)
		close(s.stopJanitor)
	}
	s.queueMu.Unlock()
	<-s.workerDone
}

// assignEvent implements event mode: EventMinTaps taps in one ~1 km cell
// (geohash 6) within EventWindow become an event; later taps there join it.
// Time is measured from the tap itself, so the result doesn't depend on
// when the worker gets to it.
func (s *Server) assignEvent(tx store.Tx, tp store.Tap) string {
	g6 := tp.Geohash[:6]
	since := tp.CreatedAt.Add(-s.cfg.EventWindow)
	if ev, ok := tx.ActiveEventNear(g6, since); ok {
		ev.End = later(ev.End, tp.CreatedAt)
		ev.TapCount++
		ev.Members = addMembers(ev.Members, tp.UserA, tp.UserB)
		tx.PutEvent(ev)
		return ev.ID
	}
	var loose []store.Tap
	for _, t := range tx.TapsNearSince(g6, since) {
		if t.EventID == "" && !t.CreatedAt.After(tp.CreatedAt) {
			loose = append(loose, t)
		}
	}
	if len(loose) < s.cfg.EventMinTaps {
		return ""
	}
	ev := store.Event{ID: newID("evt_"), Geohash6: g6, Start: loose[0].CreatedAt, End: tp.CreatedAt, TapCount: len(loose)}
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

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

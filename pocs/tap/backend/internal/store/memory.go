package store

import (
	"slices"
	"sync"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/protocol"
)

// Memory is the in-memory Store. One mutex serialises transactions, which
// is the isolation a Postgres SERIALIZABLE transaction would give. There is
// no rollback: services finish every check before their first write.
type Memory struct {
	mu       sync.Mutex
	users    map[string]User
	byPhone  map[string]string
	sessions map[string]session
	devices  map[string]Device
	nonces   map[string]Nonce
	pending  map[string][]PendingClaim
	taps     map[string]Tap
	tapByKey map[string]string
	tapOrder []string
	edges    map[[2]string]Edge
	meta     map[[2]string]EdgeMeta
	blocks   map[[2]string]bool
	reports  []Report
	events   map[string]Event
	rounds   map[[2]string]PropsRound
	votes    []PropsVote
	attempts []Attempt
}

type session struct {
	userID  string
	expires time.Time
}

func NewMemory() *Memory {
	return &Memory{
		users: map[string]User{}, byPhone: map[string]string{}, sessions: map[string]session{},
		devices: map[string]Device{}, nonces: map[string]Nonce{}, pending: map[string][]PendingClaim{},
		taps: map[string]Tap{}, tapByKey: map[string]string{}, edges: map[[2]string]Edge{},
		meta: map[[2]string]EdgeMeta{}, blocks: map[[2]string]bool{}, events: map[string]Event{},
		rounds: map[[2]string]PropsRound{},
	}
}

func (m *Memory) Tx(fn func(Tx) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn((*memTx)(m))
}

type memTx Memory

func (t *memTx) User(id string) (User, bool) { u, ok := t.users[id]; return u, ok }

func (t *memTx) UserByPhone(h string) (User, bool) {
	id, ok := t.byPhone[h]
	if !ok {
		return User{}, false
	}
	return t.User(id)
}

func (t *memTx) PutUser(u User) {
	t.users[u.ID] = u
	if u.PhoneHash != "" {
		t.byPhone[u.PhoneHash] = u.ID
	}
}

// DeleteUser removes the account and everything that names it: devices,
// sessions, edges, notes, blocks, reports and event memberships. Tap rows
// keep the other side's history but lose this user's id.
func (t *memTx) DeleteUser(id string) {
	u, ok := t.users[id]
	if !ok {
		return
	}
	delete(t.users, id)
	delete(t.byPhone, u.PhoneHash)
	for tok, s := range t.sessions {
		if s.userID == id {
			delete(t.sessions, tok)
		}
	}
	for did, d := range t.devices {
		if d.UserID == id {
			delete(t.devices, did)
		}
	}
	for k := range t.edges {
		if k[0] == id || k[1] == id {
			delete(t.edges, k)
		}
	}
	for k := range t.meta {
		if k[0] == id || k[1] == id {
			delete(t.meta, k)
		}
	}
	for k := range t.blocks {
		if k[0] == id || k[1] == id {
			delete(t.blocks, k)
		}
	}
	t.reports = slices.DeleteFunc(t.reports, func(r Report) bool { return r.ReporterID == id || r.SubjectID == id })
	for tid, tp := range t.taps {
		if tp.UserA == id || tp.UserB == id {
			tp.Receipt, tp.Signature = protocol.Receipt{}, "" // the receipt names both users
		}
		if tp.UserA == id {
			tp.UserA, tp.DeviceA = "deleted", ""
		}
		if tp.UserB == id {
			tp.UserB, tp.DeviceB = "deleted", ""
		}
		t.taps[tid] = tp
	}
	for eid, e := range t.events {
		e.Members = slices.DeleteFunc(e.Members, func(m string) bool { return m == id })
		t.events[eid] = e
	}
	for k := range t.rounds {
		if k[0] == id {
			delete(t.rounds, k)
		}
	}
	t.votes = slices.DeleteFunc(t.votes, func(v PropsVote) bool { return v.VoterID == id || v.PickedID == id })
	t.attempts = slices.DeleteFunc(t.attempts, func(a Attempt) bool { return a.UserID == id })
}

func (t *memTx) PutSession(token, userID string, expires time.Time) {
	t.sessions[token] = session{userID, expires}
}

func (t *memTx) SessionUser(token string, now time.Time) (string, bool) {
	s, ok := t.sessions[token]
	if !ok || now.After(s.expires) {
		return "", false
	}
	return s.userID, true
}

func (t *memTx) Device(id string) (Device, bool) { d, ok := t.devices[id]; return d, ok }
func (t *memTx) PutDevice(d Device)              { t.devices[d.ID] = d }

func (t *memTx) DevicesOf(userID string) []Device {
	var out []Device
	for _, d := range t.devices {
		if d.UserID == userID {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b Device) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out
}

func (t *memTx) Nonce(v string) (Nonce, bool) { n, ok := t.nonces[v]; return n, ok }
func (t *memTx) PutNonce(n Nonce)             { t.nonces[n.Value] = n }

func (t *memTx) PendingClaims(k string) []PendingClaim { return slices.Clone(t.pending[k]) }

func (t *memTx) PruneHandshakes(before time.Time) {
	for v, n := range t.nonces {
		if n.IssuedAt.Before(before) {
			delete(t.nonces, v)
		}
	}
	for k, list := range t.pending {
		list = slices.DeleteFunc(list, func(p PendingClaim) bool { return p.ReceivedAt.Before(before) })
		if len(list) == 0 {
			delete(t.pending, k)
		} else {
			t.pending[k] = list
		}
	}
}

func (t *memTx) PutPendingClaim(pc PendingClaim) {
	list := slices.DeleteFunc(t.pending[pc.PairKey], func(p PendingClaim) bool {
		return p.Claim.DeviceID == pc.Claim.DeviceID
	})
	t.pending[pc.PairKey] = append(list, pc)
}

func (t *memTx) DeletePendingClaim(k, deviceID string) {
	list := slices.DeleteFunc(t.pending[k], func(p PendingClaim) bool { return p.Claim.DeviceID == deviceID })
	if len(list) == 0 {
		delete(t.pending, k)
		return
	}
	t.pending[k] = list
}

func (t *memTx) TapByPairKey(k string) (Tap, bool) {
	id, ok := t.tapByKey[k]
	if !ok {
		return Tap{}, false
	}
	return t.Tap(id)
}

func (t *memTx) Tap(id string) (Tap, bool) { tp, ok := t.taps[id]; return tp, ok }

func (t *memTx) PutTap(tp Tap) {
	if _, exists := t.taps[tp.ID]; !exists {
		t.tapOrder = append(t.tapOrder, tp.ID)
	}
	t.taps[tp.ID] = tp
	t.tapByKey[tp.PairKey] = tp.ID
}

func (t *memTx) TapsByDeviceSince(deviceID string, since time.Time) int {
	n := 0
	for _, tp := range t.taps {
		if (tp.DeviceA == deviceID || tp.DeviceB == deviceID) && !tp.CreatedAt.Before(since) {
			n++
		}
	}
	return n
}

func (t *memTx) TapsNearSince(g6 string, since time.Time) []Tap {
	var out []Tap
	for _, id := range t.tapOrder {
		tp := t.taps[id]
		if len(tp.Geohash) >= 6 && tp.Geohash[:6] == g6 && !tp.CreatedAt.Before(since) {
			out = append(out, tp)
		}
	}
	return out
}

func (t *memTx) Edge(a, b string) (Edge, bool) {
	a, b = Pair(a, b)
	e, ok := t.edges[[2]string{a, b}]
	return e, ok
}

func (t *memTx) EdgesOf(userID string) []Edge {
	var out []Edge
	for k, e := range t.edges {
		if k[0] == userID || k[1] == userID {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b Edge) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out
}

func (t *memTx) PutEdge(e Edge) { t.edges[[2]string{e.UserA, e.UserB}] = e }

func (t *memTx) Meta(owner, other string) EdgeMeta {
	if m, ok := t.meta[[2]string{owner, other}]; ok {
		return m
	}
	return EdgeMeta{OwnerID: owner, OtherID: other}
}

func (t *memTx) PutMeta(m EdgeMeta) { t.meta[[2]string{m.OwnerID, m.OtherID}] = m }

func (t *memTx) Blocked(a, b string) bool {
	return t.blocks[[2]string{a, b}] || t.blocks[[2]string{b, a}]
}

func (t *memTx) PutBlock(blocker, blocked string) { t.blocks[[2]string{blocker, blocked}] = true }

func (t *memTx) PutReport(r Report) { t.reports = append(t.reports, r) }

func (t *memTx) DistinctReporters(subject string) int {
	seen := map[string]bool{}
	for _, r := range t.reports {
		if r.SubjectID == subject {
			seen[r.ReporterID] = true
		}
	}
	return len(seen)
}

func (t *memTx) Event(id string) (Event, bool) {
	e, ok := t.events[id]
	e.Members = slices.Clone(e.Members)
	return e, ok
}

func (t *memTx) EventsOf(userID string) []Event {
	var out []Event
	for _, e := range t.events {
		if slices.Contains(e.Members, userID) {
			e.Members = slices.Clone(e.Members)
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b Event) int { return b.Start.Compare(a.Start) })
	return out
}

// ActiveEventNear returns the most recently active event in the cell, as
// the Postgres store does.
func (t *memTx) ActiveEventNear(g6 string, since time.Time) (Event, bool) {
	var best Event
	found := false
	for _, e := range t.events {
		if e.Geohash6 == g6 && !e.End.Before(since) && (!found || e.End.After(best.End)) {
			best, found = e, true
		}
	}
	best.Members = slices.Clone(best.Members)
	return best, found
}

// PutEvent stores e; ids of deleted accounts are dropped from Members.
func (t *memTx) PutEvent(e Event) {
	e.Members = slices.DeleteFunc(slices.Clone(e.Members), func(id string) bool {
		_, ok := t.users[id]
		return !ok
	})
	t.events[e.ID] = e
}

func cloneRound(r PropsRound) PropsRound {
	r.Polls = slices.Clone(r.Polls)
	for i := range r.Polls {
		r.Polls[i].Options = slices.Clone(r.Polls[i].Options)
	}
	return r
}

func (t *memTx) PropsRound(userID, week string) (PropsRound, bool) {
	r, ok := t.rounds[[2]string{userID, week}]
	return cloneRound(r), ok
}

func (t *memTx) PutPropsRound(r PropsRound) { t.rounds[[2]string{r.UserID, r.Week}] = cloneRound(r) }

func (t *memTx) PutPropsVote(v PropsVote) { t.votes = append(t.votes, v) }

func (t *memTx) PropsVotesFor(pickedID string, since time.Time) []PropsVote {
	var out []PropsVote
	for _, v := range t.votes {
		if v.PickedID == pickedID && !v.CreatedAt.Before(since) {
			out = append(out, v)
		}
	}
	slices.SortStableFunc(out, func(a, b PropsVote) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out
}

func (t *memTx) PropsVotesSince(since time.Time) int {
	n := 0
	for _, v := range t.votes {
		if !v.CreatedAt.Before(since) {
			n++
		}
	}
	return n
}

func (t *memTx) PutAttempt(a Attempt) { t.attempts = append(t.attempts, a) }

func (t *memTx) AttemptsSince(since time.Time) []Attempt {
	var out []Attempt
	for _, a := range t.attempts {
		if !a.At.Before(since) {
			out = append(out, a)
		}
	}
	return out
}

func (t *memTx) TapsSince(since time.Time) []Tap {
	var out []Tap
	for _, id := range t.tapOrder {
		if tp := t.taps[id]; !tp.CreatedAt.Before(since) {
			out = append(out, tp)
		}
	}
	return out
}

// Package store holds Tap's data model behind a transactional interface.
//
// The MVP ships an in-memory implementation so the whole system runs with
// `go run`; the pilot swaps in Postgres (then Spanner) by implementing the
// same Tx interface, without touching the services.
package store

import (
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/protocol"
)

type User struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	PhotoURL    string    `json:"photo_url,omitempty"`
	PhoneHash   string    `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	Frozen      bool      `json:"frozen,omitempty"` // tapping disabled after repeat reports
}

type Device struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	PublicKey    string    `json:"public_key"`
	AttestStatus string    `json:"attest_status"`
	CreatedAt    time.Time `json:"created_at"`
}

type Nonce struct {
	Value    string
	DeviceID string
	IssuedAt time.Time
	Used     bool
}

// PendingClaim is the first half of a handshake, held until its partner
// arrives or the wait window closes.
type PendingClaim struct {
	PairKey    string
	UserID     string
	Claim      protocol.Claim
	IssuedAt   time.Time // server time the claim's nonce was issued
	ReceivedAt time.Time
}

// Tap is one accepted handshake. Repeat taps of an existing connection are
// separate rows: the north-star metric counts taps, not edges.
type Tap struct {
	ID        string
	UserA     string // UserA < UserB
	UserB     string
	DeviceA   string
	DeviceB   string
	CreatedAt time.Time
	Geohash   string
	EventID   string
	PairKey   string
	Receipt   protocol.Receipt
	Signature string
}

type EdgeStatus string

const (
	EdgeActive     EdgeStatus = "active"
	EdgeRemovedByA EdgeStatus = "removed_by_a"
	EdgeRemovedByB EdgeStatus = "removed_by_b"
)

// Edge is the connection between two users; it exists only through taps.
type Edge struct {
	UserA      string
	UserB      string
	CreatedAt  time.Time
	FirstTapID string
	LastTapAt  time.Time
	TapCount   int
	Status     EdgeStatus
}

// EdgeMeta is one side's private view of an edge.
type EdgeMeta struct {
	OwnerID   string
	OtherID   string
	Note      string
	HidePlace bool
}

type Event struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Geohash6 string    `json:"geohash6"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	TapCount int       `json:"tap_count"`
	Members  []string  `json:"-"`
}

type Report struct {
	ID         string
	ReporterID string
	SubjectID  string
	Reason     string
	CreatedAt  time.Time
}

// PropsPoll is one question in a weekly Props round: a curated prompt and
// up to four of the user's connections to pick from.
type PropsPoll struct {
	PromptID string   `json:"prompt_id"`
	Options  []string `json:"options"`  // user ids
	Answered bool     `json:"answered"` // picked or skipped; the pick itself lives only in PropsVote
}

type PropsRound struct {
	UserID    string
	Week      string // ISO week, e.g. 2026-W43
	CreatedAt time.Time
	Polls     []PropsPoll
}

// PropsVote is one real pick. The voter is stored so removals and
// deletions can be honoured, and is never returned by any API.
type PropsVote struct {
	ID        string
	VoterID   string
	PickedID  string
	PromptID  string
	Week      string
	EventID   string // event of the voter–picked edge's first tap, for the hint
	CreatedAt time.Time
}

// Store runs fn as one transaction. Implementations must make fn's reads
// and writes atomic with respect to other transactions.
type Store interface {
	Tx(fn func(Tx) error) error
}

// Tx is everything the services may do inside a transaction. Getters
// return copies; changes take effect only through the Put methods.
type Tx interface {
	User(id string) (User, bool)
	UserByPhone(phoneHash string) (User, bool)
	PutUser(User)
	DeleteUser(id string)

	PutSession(token, userID string, expires time.Time)
	SessionUser(token string, now time.Time) (string, bool)

	Device(id string) (Device, bool)
	DevicesOf(userID string) []Device
	PutDevice(Device)

	Nonce(value string) (Nonce, bool)
	PutNonce(Nonce)

	PruneHandshakes(before time.Time) // drop nonces issued and pending claims received before
	PendingClaims(pairKey string) []PendingClaim
	PutPendingClaim(PendingClaim)
	DeletePendingClaim(pairKey, deviceID string)

	TapByPairKey(pairKey string) (Tap, bool)
	Tap(id string) (Tap, bool)
	PutTap(Tap)
	TapsByDeviceSince(deviceID string, since time.Time) int
	TapsNearSince(geohash6 string, since time.Time) []Tap

	Edge(a, b string) (Edge, bool)
	EdgesOf(userID string) []Edge
	PutEdge(Edge)
	Meta(ownerID, otherID string) EdgeMeta
	PutMeta(EdgeMeta)

	Blocked(a, b string) bool // either direction
	PutBlock(blockerID, blockedID string)

	PutReport(Report)
	DistinctReporters(subjectID string) int

	Event(id string) (Event, bool)
	EventsOf(userID string) []Event
	ActiveEventNear(geohash6 string, since time.Time) (Event, bool)
	PutEvent(Event)

	PropsRound(userID, week string) (PropsRound, bool)
	PutPropsRound(PropsRound)
	PutPropsVote(PropsVote)
	PropsVotesFor(pickedID string, since time.Time) []PropsVote // newest first
}

// Pair orders two user ids the way edges and taps store them.
func Pair(a, b string) (string, string) {
	if b < a {
		return b, a
	}
	return a, b
}

// Package server is Tap's HTTP API: identity, the tap matching engine, and
// the people/events surfaces. The MVP runs these in one process; each file
// maps onto one Cloud Run service of the design (identity, tap, people).
package server

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/tap/protocol"
)

// Config holds every threshold the design names, so tests and the pilot can
// tune them without code changes.
type Config struct {
	Now                  func() time.Time
	NonceTTL             time.Duration // a nonce is valid this long after issue
	ClaimWait            time.Duration // how long POST /tap/claim waits for the partner
	MaxSkew              time.Duration // allowed gap between the two measurements
	MaxDistanceCM        map[protocol.Transport]int
	TapsPerDevicePerHour int
	SessionTTL           time.Duration
	EventMinTaps         int // taps nearby within EventWindow that form an event
	EventWindow          time.Duration
	ReportsToFreeze      int    // distinct reporters that freeze tapping
	DevOTP               bool   // return OTP codes in the response instead of sending SMS
	PhoneSalt            []byte // HMAC key for phone hashes
	ReceiptKey           *ecdsa.PrivateKey
	Attest               AttestVerifier
	Static               fs.FS // optional web demo served at /
	Logf                 func(format string, args ...any)
}

// DefaultConfig returns the design's numbers with a fresh dev receipt key.
func DefaultConfig() (Config, error) {
	key, err := protocol.GenerateDeviceKey()
	if err != nil {
		return Config{}, err
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return Config{}, err
	}
	return Config{
		Now:       time.Now,
		NonceTTL:  20 * time.Second,
		ClaimWait: 10 * time.Second,
		MaxSkew:   2 * time.Second,
		MaxDistanceCM: map[protocol.Transport]int{
			protocol.TransportUWB:           15,
			protocol.TransportBLEUltrasonic: 30,
			protocol.TransportQRBLE:         50,
		},
		TapsPerDevicePerHour: 60,
		SessionTTL:           30 * 24 * time.Hour,
		EventMinTaps:         5,
		EventWindow:          time.Hour,
		ReportsToFreeze:      3,
		PhoneSalt:            salt,
		ReceiptKey:           key,
		Attest:               DevAttest{},
		Logf:                 log.Printf,
	}, nil
}

type Server struct {
	cfg   Config
	st    store.Store
	mux   *http.ServeMux
	mu    sync.Mutex
	wait  map[string]map[string]chan matchResult // pair key → device → waiting claim
	otps  map[string]otpEntry
	keyID string // receipt public key, base64url

	tapCreated chan string    // committed tap ids for the event worker
	eventsIdle sync.WaitGroup // taps queued but not yet processed
	workerDone chan struct{}
}

func New(cfg Config, st store.Store) (*Server, error) {
	if cfg.ReceiptKey == nil || cfg.Attest == nil || cfg.Now == nil {
		return nil, errors.New("server: config needs ReceiptKey, Attest and Now")
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	pub, err := protocol.EncodePublicKey(&cfg.ReceiptKey.PublicKey)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, st: st, mux: http.NewServeMux(), wait: map[string]map[string]chan matchResult{}, otps: map[string]otpEntry{}, keyID: pub}
	s.tapCreated = make(chan string, 1<<16)
	s.workerDone = make(chan struct{})
	go s.runEventWorker()
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	s.mux.HandleFunc("GET /v1/receipt-key", s.handleReceiptKey)

	s.mux.HandleFunc("POST /v1/auth/otp/start", s.handleOTPStart)
	s.mux.HandleFunc("POST /v1/auth/otp/verify", s.handleOTPVerify)
	s.mux.HandleFunc("POST /v1/devices", s.authed(s.handleRegisterDevice))
	s.mux.HandleFunc("GET /v1/me", s.authed(s.handleMe))
	s.mux.HandleFunc("GET /v1/me/export", s.authed(s.handleExport))
	s.mux.HandleFunc("DELETE /v1/me", s.authed(s.handleDeleteMe))

	s.mux.HandleFunc("POST /v1/tap/nonce", s.authed(s.handleNonce))
	s.mux.HandleFunc("POST /v1/tap/claim", s.authed(s.handleClaim))

	s.mux.HandleFunc("GET /v1/people", s.authed(s.handlePeople))
	s.mux.HandleFunc("PUT /v1/people/{id}/note", s.authed(s.handleNote))
	s.mux.HandleFunc("PUT /v1/people/{id}/place", s.authed(s.handleHidePlace))
	s.mux.HandleFunc("DELETE /v1/people/{id}", s.authed(s.handleRemove))
	s.mux.HandleFunc("POST /v1/blocks", s.authed(s.handleBlock))
	s.mux.HandleFunc("POST /v1/reports", s.authed(s.handleReport))

	s.mux.HandleFunc("GET /v1/events", s.authed(s.handleEvents))
	s.mux.HandleFunc("PUT /v1/events/{id}", s.authed(s.handleNameEvent))
	s.mux.HandleFunc("GET /v1/events/{id}/recap", s.authed(s.handleRecap))
	s.mux.HandleFunc("GET /v1/events/{id}/recap.svg", s.authed(s.handleRecapSVG))

	if s.cfg.Static != nil {
		s.mux.Handle("GET /", http.FileServerFS(s.cfg.Static))
	}
}

func (s *Server) handleReceiptKey(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"public_key": s.keyID, "alg": "ES256-raw"})
}

// apiError is the one error shape every endpoint returns.
type apiError struct {
	status int
	Code   string `json:"code"`
	Msg    string `json:"error"`
}

func (e *apiError) Error() string { return e.Code + ": " + e.Msg }

func fail(status int, code, format string, args ...any) *apiError {
	return &apiError{status: status, Code: code, Msg: fmt.Sprintf(format, args...)}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var ae *apiError
	if !errors.As(err, &ae) {
		log.Printf("internal error: %v", err)
		ae = fail(500, "internal", "internal error")
	}
	writeJSON(w, ae.status, ae)
}

func decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fail(400, "bad_json", "request body: %v", err)
	}
	return nil
}

type authedHandler func(w http.ResponseWriter, r *http.Request, userID string)

// authed resolves the session from the Bearer header, or from ?token= for
// image URLs that an <img> tag fetches without headers.
func (s *Server) authed(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			tok = r.URL.Query().Get("token")
		}
		var userID string
		if err := s.st.Tx(func(tx store.Tx) error {
			userID, _ = tx.SessionUser(tok, s.cfg.Now())
			return nil
		}); err != nil {
			writeErr(w, err)
			return
		}
		if tok == "" || userID == "" {
			writeErr(w, fail(401, "unauthenticated", "sign in first"))
			return
		}
		h(w, r, userID)
	}
}

func newID(prefix string) string { return prefix + randomB64(10) }

func randomB64(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read never fails on supported platforms
	return protocol.B64.EncodeToString(b)
}

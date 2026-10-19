package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/tap/protocol"
)

// AttestVerifier checks that a device key lives in genuine hardware: Apple
// App Attest on iOS, Key Attestation + Play Integrity on Android.
type AttestVerifier interface {
	Verify(publicKey, attestation string) (status string, err error)
}

// DevAttest accepts tokens of the form "dev:<anything>". It stands in for
// the real verifiers, which need Apple and Google credentials.
type DevAttest struct{}

func (DevAttest) Verify(_, attestation string) (string, error) {
	if strings.HasPrefix(attestation, "dev:") {
		return "dev", nil
	}
	return "", fmt.Errorf("unsupported attestation format")
}

type otpEntry struct {
	code     string
	expires  time.Time
	attempts int
}

var e164 = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

const (
	otpTTL         = 5 * time.Minute
	otpMaxAttempts = 5
)

func (s *Server) phoneHash(phone string) string {
	m := hmac.New(sha256.New, s.cfg.PhoneSalt)
	m.Write([]byte(phone))
	return hex.EncodeToString(m.Sum(nil))
}

func (s *Server) handleOTPStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if !e164.MatchString(req.Phone) {
		writeErr(w, fail(400, "bad_phone", "phone must be E.164, like +13145550123"))
		return
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(1_000_000))
	code := fmt.Sprintf("%06d", n.Int64())
	s.mu.Lock()
	s.otps[req.Phone] = otpEntry{code: code, expires: s.cfg.Now().Add(otpTTL)}
	s.mu.Unlock()
	resp := map[string]any{"sent": true}
	if s.cfg.DevOTP {
		resp["dev_code"] = code // no SMS provider in dev: hand the code back
	} else {
		s.cfg.Logf("otp for %s issued (SMS provider not configured)", req.Phone[:4]+"…")
	}
	writeJSON(w, 200, resp)
}

func (s *Server) handleOTPVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone       string `json:"phone"`
		Code        string `json:"code"`
		DisplayName string `json:"display_name"`
		PhotoURL    string `json:"photo_url"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.checkOTP(req.Phone, req.Code); err != nil {
		writeErr(w, err)
		return
	}
	now := s.cfg.Now()
	token := "tok_" + randomB64(32)
	var user store.User
	err := s.st.Tx(func(tx store.Tx) error {
		u, ok := tx.UserByPhone(s.phoneHash(req.Phone))
		if !ok {
			name := strings.TrimSpace(req.DisplayName)
			if name == "" || len(name) > 40 {
				return fail(400, "bad_name", "new accounts need a display name of 1–40 characters")
			}
			u = store.User{ID: newID("usr_"), DisplayName: name, PhotoURL: req.PhotoURL, PhoneHash: s.phoneHash(req.Phone), CreatedAt: now}
			tx.PutUser(u)
		}
		tx.PutSession(token, u.ID, now.Add(s.cfg.SessionTTL))
		user = u
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"token": token, "user": user})
}

func (s *Server) checkOTP(phone, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.otps[phone]
	if !ok || s.cfg.Now().After(e.expires) {
		return fail(400, "otp_expired", "request a new code")
	}
	if e.attempts >= otpMaxAttempts {
		return fail(429, "otp_locked", "too many attempts; request a new code")
	}
	if subtle.ConstantTimeCompare([]byte(e.code), []byte(code)) != 1 {
		e.attempts++
		s.otps[phone] = e
		return fail(400, "otp_wrong", "wrong code")
	}
	delete(s.otps, phone)
	return nil
}

func (s *Server) handleRegisterDevice(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		PublicKey   string `json:"public_key"`
		Attestation string `json:"attestation"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := protocol.ParsePublicKey(req.PublicKey); err != nil {
		writeErr(w, fail(400, "bad_key", "%v", err))
		return
	}
	status, err := s.cfg.Attest.Verify(req.PublicKey, req.Attestation)
	if err != nil {
		writeErr(w, fail(403, "attestation_failed", "%v", err))
		return
	}
	d := store.Device{ID: newID("dev_"), UserID: userID, PublicKey: req.PublicKey, AttestStatus: status, CreatedAt: s.cfg.Now()}
	if err := s.st.Tx(func(tx store.Tx) error { tx.PutDevice(d); return nil }); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{"device_id": d.ID})
}

func (s *Server) handleMe(w http.ResponseWriter, _ *http.Request, userID string) {
	var resp map[string]any
	if err := s.st.Tx(func(tx store.Tx) error {
		u, _ := tx.User(userID)
		taps := 0
		for _, e := range tx.EdgesOf(userID) {
			taps += e.TapCount
		}
		resp = map[string]any{"user": u, "tap_count": taps, "connections": len(activeOthers(tx, userID))}
		return nil
	}); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, resp)
}

// handleExport returns everything Tap holds about the caller.
func (s *Server) handleExport(w http.ResponseWriter, _ *http.Request, userID string) {
	type conn struct {
		DisplayName string    `json:"display_name"`
		MetAt       time.Time `json:"met_at"`
		TapCount    int       `json:"tap_count"`
		Note        string    `json:"note,omitempty"`
		Status      string    `json:"status"`
	}
	var resp map[string]any
	if err := s.st.Tx(func(tx store.Tx) error {
		u, _ := tx.User(userID)
		var conns []conn
		for _, e := range tx.EdgesOf(userID) {
			other := otherOf(e, userID)
			ou, _ := tx.User(other)
			conns = append(conns, conn{ou.DisplayName, e.CreatedAt, e.TapCount, tx.Meta(userID, other).Note, string(e.Status)})
		}
		resp = map[string]any{"user": u, "devices": tx.DevicesOf(userID), "connections": conns, "events": tx.EventsOf(userID)}
		return nil
	}); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, resp)
}

func (s *Server) handleDeleteMe(w http.ResponseWriter, _ *http.Request, userID string) {
	if err := s.st.Tx(func(tx store.Tx) error { tx.DeleteUser(userID); return nil }); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"deleted": true})
}

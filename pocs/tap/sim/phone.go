// Package sim plays virtual phones against a Tap server. It does what the
// iOS and Android apps do — onboard, register a device key, run the
// handshake, sign claims — with the short-range radio replaced by a direct
// in-memory exchange. Tests, the tapsim CLI and CI's e2e job all use it.
package sim

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/protocol"
)

// APIError is a non-2xx response from the server.
type APIError struct {
	Status int
	Code   string `json:"code"`
	Msg    string `json:"error"`
}

func (e *APIError) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Msg) }

// Code returns the API error code of err, or "" if err is not an APIError.
func Code(err error) string {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

type Phone struct {
	Base     string
	HTTP     *http.Client
	Name     string
	Token    string
	UserID   string
	DeviceID string
	Platform string // reported at device registration; "sim" when empty
	key      *ecdsa.PrivateKey
}

func NewPhone(base, name string) *Phone {
	return &Phone{Base: base, Name: name, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (p *Phone) Do(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.Base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		ae := &APIError{Status: resp.StatusCode}
		_ = json.Unmarshal(data, ae)
		return ae
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Onboard signs up (or in) with a phone number using the dev OTP flow,
// then registers a fresh device key.
func (p *Phone) Onboard(phone string) error {
	var start struct {
		DevCode string `json:"dev_code"`
	}
	if err := p.Do("POST", "/v1/auth/otp/start", map[string]string{"phone": phone}, &start); err != nil {
		return err
	}
	if start.DevCode == "" {
		return errors.New("server did not return a dev OTP; run it with -dev")
	}
	var verify struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := p.Do("POST", "/v1/auth/otp/verify", map[string]string{"phone": phone, "code": start.DevCode, "display_name": p.Name}, &verify); err != nil {
		return err
	}
	p.Token, p.UserID = verify.Token, verify.User.ID
	return p.RegisterDevice()
}

func (p *Phone) RegisterDevice() error {
	key, err := protocol.GenerateDeviceKey()
	if err != nil {
		return err
	}
	pub, err := protocol.EncodePublicKey(&key.PublicKey)
	if err != nil {
		return err
	}
	var out struct {
		DeviceID string `json:"device_id"`
	}
	platform := p.Platform
	if platform == "" {
		platform = "sim"
	}
	if err := p.Do("POST", "/v1/devices", map[string]string{"public_key": pub, "attestation": "dev:sim", "platform": platform}, &out); err != nil {
		return err
	}
	p.key, p.DeviceID = key, out.DeviceID
	return nil
}

// TapSession is one phone's half of a handshake in progress.
type TapSession struct {
	phone   *Phone
	Nonce   string
	fetched time.Time
	eph     *ecdh.PrivateKey
}

// StartTap enters tap mode: fetch a nonce, make an ephemeral key.
func (p *Phone) StartTap() (*TapSession, error) {
	var out struct {
		Nonce string `json:"nonce"`
	}
	if err := p.Do("POST", "/v1/tap/nonce", map[string]string{"device_id": p.DeviceID}, &out); err != nil {
		return nil, err
	}
	eph, err := protocol.GenerateEphemeral()
	if err != nil {
		return nil, err
	}
	return &TapSession{phone: p, Nonce: out.Nonce, fetched: time.Now(), eph: eph}, nil
}

func (t *TapSession) EphPub() string { return protocol.B64.EncodeToString(t.eph.PublicKey().Bytes()) }

// Measurement is what the radios report; tests bend it to simulate attacks.
type Measurement struct {
	DistanceCM    int
	Transport     protocol.Transport
	Geohash       string
	ElapsedOffset time.Duration // added to the honest elapsed time (clock skew)
}

// DefaultMeasurement is two phones held back to back near the Gateway Arch.
func DefaultMeasurement() Measurement {
	return Measurement{DistanceCM: 4, Transport: protocol.TransportBLEUltrasonic, Geohash: protocol.Geohash(38.6247, -90.1848, 7)}
}

// Claim builds and signs this phone's claim about its peer.
func (t *TapSession) Claim(peerNonce, peerEph string, m Measurement) (protocol.Claim, string, error) {
	c := protocol.Claim{
		DeviceID: t.phone.DeviceID, Nonce: t.Nonce, PeerNonce: peerNonce,
		EphPub: t.EphPub(), PeerEphPub: peerEph, DistanceCM: m.DistanceCM,
		ElapsedMS: (time.Since(t.fetched) + m.ElapsedOffset).Milliseconds(),
		Geohash:   m.Geohash, Transport: m.Transport,
	}
	sig, err := protocol.Sign(t.phone.key, c.Canonical())
	return c, sig, err
}

type Peer struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

type Match struct {
	Status    string           `json:"status"` // matched | expired
	Receipt   protocol.Receipt `json:"receipt"`
	Signature string           `json:"signature"`
	TapCount  int              `json:"tap_count"`
	Peer      Peer             `json:"peer"`
}

func (p *Phone) Submit(c protocol.Claim, sig string) (Match, error) {
	var m Match
	err := p.Do("POST", "/v1/tap/claim", map[string]any{"claim": c, "signature": sig}, &m)
	return m, err
}

// Result is one side's outcome of a handshake.
type Result struct {
	Match Match
	Err   error
}

// Handshake runs a full tap between a and b: both enter tap mode, swap
// nonces and ephemeral keys over the "radio", agree a session key, sign
// claims and submit them at the same time.
func Handshake(a, b *Phone, ma, mb Measurement) (Result, Result, error) {
	sa, err := a.StartTap()
	if err != nil {
		return Result{}, Result{}, fmt.Errorf("%s: %w", a.Name, err)
	}
	sb, err := b.StartTap()
	if err != nil {
		return Result{}, Result{}, fmt.Errorf("%s: %w", b.Name, err)
	}
	if err := checkSessionKeys(sa, sb); err != nil {
		return Result{}, Result{}, err
	}
	ca, siga, err := sa.Claim(sb.Nonce, sb.EphPub(), ma)
	if err != nil {
		return Result{}, Result{}, err
	}
	cb, sigb, err := sb.Claim(sa.Nonce, sa.EphPub(), mb)
	if err != nil {
		return Result{}, Result{}, err
	}
	var ra, rb Result
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); ra.Match, ra.Err = a.Submit(ca, siga) }()
	go func() { defer wg.Done(); rb.Match, rb.Err = b.Submit(cb, sigb) }()
	wg.Wait()
	return ra, rb, nil
}

// checkSessionKeys proves both phones derived the same key from the swap,
// which is what lets them exchange profile cards privately.
func checkSessionKeys(a, b *TapSession) error {
	probe := protocol.Claim{Nonce: a.Nonce, PeerNonce: b.Nonce, EphPub: a.EphPub(), PeerEphPub: b.EphPub()}
	ka, err := protocol.SessionKey(a.eph, b.EphPub(), probe.Transcript())
	if err != nil {
		return err
	}
	kb, err := protocol.SessionKey(b.eph, a.EphPub(), probe.Transcript())
	if err != nil {
		return err
	}
	if !bytes.Equal(ka, kb) {
		return errors.New("session keys differ")
	}
	return nil
}

type Event struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Named     bool   `json:"named"`
	TapCount  int    `json:"tap_count"`
	PeopleMet int    `json:"people_met"`
}

// WaitEvent polls until the phone belongs to an event. Events are assigned
// asynchronously after a tap commits, so they appear a moment later.
func (p *Phone) WaitEvent(timeout time.Duration) (Event, error) {
	deadline := time.Now().Add(timeout)
	for {
		var out struct {
			Events []Event `json:"events"`
		}
		if err := p.Do("GET", "/v1/events", nil, &out); err != nil {
			return Event{}, err
		}
		if len(out.Events) > 0 {
			return out.Events[0], nil
		}
		if time.Now().After(deadline) {
			return Event{}, errors.New("no event formed")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type PropsRound struct {
	Unlocked bool `json:"unlocked"`
	Polls    []struct {
		Index    int    `json:"index"`
		Prompt   string `json:"prompt"`
		Answered bool   `json:"answered"`
		Options  []Peer `json:"options"`
	} `json:"polls"`
}

type PropsCard struct {
	People   int    `json:"people"`
	Picks    int    `json:"picks"`
	Headline string `json:"headline"`
	ByPrompt []struct {
		Prompt string `json:"prompt"`
		Count  int    `json:"count"`
	} `json:"by_prompt"`
}

func (p *Phone) PropsRound() (PropsRound, error) {
	var r PropsRound
	err := p.Do("GET", "/v1/props/round", nil, &r)
	return r, err
}

// AnswerProps answers every open poll with choose (which returns an option
// index, or -1 to skip) and reports how many picks it made.
func (p *Phone) AnswerProps(choose func(prompt string, options []Peer) int) (int, error) {
	r, err := p.PropsRound()
	if err != nil {
		return 0, err
	}
	if !r.Unlocked {
		return 0, errors.New("props locked")
	}
	picks := 0
	for _, poll := range r.Polls {
		if poll.Answered {
			continue
		}
		pick := ""
		if i := choose(poll.Prompt, poll.Options); i >= 0 {
			pick = poll.Options[i].UserID
			picks++
		}
		if err := p.Do("POST", "/v1/props/vote", map[string]any{"index": poll.Index, "pick": pick}, nil); err != nil {
			return picks, err
		}
	}
	return picks, nil
}

func (p *Phone) PropsCard() (PropsCard, error) {
	var c PropsCard
	err := p.Do("GET", "/v1/props/card", nil, &c)
	return c, err
}

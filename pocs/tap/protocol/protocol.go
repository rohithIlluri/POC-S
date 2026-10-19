// Package protocol defines the bytes that cross between two phones and the
// Tap service: the signed tap claim, the server's edge receipt, and the
// ephemeral key exchange that binds both phones to one handshake.
//
// The same encoding is implemented in Swift, Kotlin and the web demo; the
// vectors in testdata/vectors.json are the contract all of them must pass.
package protocol

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const (
	ClaimVersion      = "tap-claim-v1"
	ReceiptVersion    = "tap-receipt-v1"
	transcriptVersion = "tap-transcript-v1"
	sessionInfo       = "tap-session-v1"
)

// Transport names the short-range channel that measured the distance.
type Transport string

const (
	TransportUWB           Transport = "uwb"
	TransportBLEUltrasonic Transport = "ble_ultrasonic"
	TransportQRBLE         Transport = "qr_ble"
)

func (t Transport) Valid() bool {
	switch t {
	case TransportUWB, TransportBLEUltrasonic, TransportQRBLE:
		return true
	}
	return false
}

// B64 is the encoding for every binary field: base64url without padding.
var B64 = base64.RawURLEncoding

// Claim is one phone's signed statement that it was close to another phone.
type Claim struct {
	DeviceID   string    `json:"device_id"`
	Nonce      string    `json:"nonce"`        // this phone's server nonce
	PeerNonce  string    `json:"peer_nonce"`   // the other phone's server nonce
	EphPub     string    `json:"eph_pub"`      // this phone's ephemeral X25519 public key
	PeerEphPub string    `json:"peer_eph_pub"` // the other phone's ephemeral X25519 public key
	DistanceCM int       `json:"distance_cm"`  // measured closeness
	ElapsedMS  int64     `json:"elapsed_ms"`   // monotonic time since this phone's nonce was issued
	Geohash    string    `json:"geohash"`      // precision 7, about 150 m
	Transport  Transport `json:"transport"`
}

// Validate checks field shapes before anything is signed or verified.
func (c Claim) Validate() error {
	for name, v := range map[string]string{
		"device_id": c.DeviceID, "nonce": c.Nonce, "peer_nonce": c.PeerNonce,
		"eph_pub": c.EphPub, "peer_eph_pub": c.PeerEphPub, "geohash": c.Geohash,
	} {
		if v == "" {
			return fmt.Errorf("claim: %s is empty", name)
		}
		if strings.ContainsAny(v, "\n\r") {
			return fmt.Errorf("claim: %s contains a line break", name)
		}
	}
	if c.Nonce == c.PeerNonce {
		return errors.New("claim: nonce equals peer_nonce")
	}
	if c.DistanceCM < 0 || c.ElapsedMS < 0 {
		return errors.New("claim: negative distance or elapsed time")
	}
	if !c.Transport.Valid() {
		return fmt.Errorf("claim: unknown transport %q", c.Transport)
	}
	if len(c.Geohash) != 7 || !isGeohash(c.Geohash) {
		return errors.New("claim: geohash must be 7 base-32 characters")
	}
	return nil
}

// Canonical is the exact byte string a device signs: fields in fixed order,
// one per line, integers in base 10.
func (c Claim) Canonical() []byte {
	return []byte(strings.Join([]string{
		ClaimVersion, c.DeviceID, c.Nonce, c.PeerNonce, c.EphPub, c.PeerEphPub,
		strconv.Itoa(c.DistanceCM), strconv.FormatInt(c.ElapsedMS, 10),
		c.Geohash, string(c.Transport),
	}, "\n"))
}

// PairKey identifies a handshake independent of which phone is asking:
// the two nonces in sorted order.
func (c Claim) PairKey() string {
	lo, hi := sorted(c.Nonce, c.PeerNonce)
	return lo + "." + hi
}

// Transcript hashes both nonces and both ephemeral keys in nonce order, so
// both phones compute the same value without agreeing on who went first.
func (c Claim) Transcript() []byte {
	type side struct{ nonce, eph string }
	a, b := side{c.Nonce, c.EphPub}, side{c.PeerNonce, c.PeerEphPub}
	if b.nonce < a.nonce {
		a, b = b, a
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{transcriptVersion, a.nonce, a.eph, b.nonce, b.eph}, "\n")))
	return sum[:]
}

// Mirrors reports whether other is the partner claim of the same handshake.
func (c Claim) Mirrors(other Claim) bool {
	return c.Nonce == other.PeerNonce && c.PeerNonce == other.Nonce &&
		c.EphPub == other.PeerEphPub && c.PeerEphPub == other.EphPub
}

// Receipt is the server's signed proof that an edge was created by a tap.
type Receipt struct {
	TapID     string `json:"tap_id"`
	UserA     string `json:"user_a"` // UserA < UserB
	UserB     string `json:"user_b"`
	CreatedAt int64  `json:"created_at"` // unix milliseconds
	Geohash   string `json:"geohash"`
	PairKey   string `json:"pair_key"`
}

func (r Receipt) Canonical() []byte {
	return []byte(strings.Join([]string{
		ReceiptVersion, r.TapID, r.UserA, r.UserB,
		strconv.FormatInt(r.CreatedAt, 10), r.Geohash, r.PairKey,
	}, "\n"))
}

// GenerateDeviceKey creates a P-256 signing key. On phones this key lives in
// the Secure Enclave or StrongBox and never leaves it.
func GenerateDeviceKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// EncodePublicKey returns the uncompressed SEC1 point, base64url.
func EncodePublicKey(pub *ecdsa.PublicKey) (string, error) {
	k, err := pub.ECDH()
	if err != nil {
		return "", err
	}
	return B64.EncodeToString(k.Bytes()), nil
}

// ParsePublicKey reads an uncompressed SEC1 P-256 point, base64url.
func ParsePublicKey(s string) (*ecdsa.PublicKey, error) {
	b, err := B64.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}
	if _, err := ecdh.P256().NewPublicKey(b); err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), b) //nolint:staticcheck // validated above
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
}

// Sign signs msg with SHA-256 and returns the raw r||s signature (64 bytes,
// base64url) — the format WebCrypto and CryptoKit produce natively.
func Sign(priv *ecdsa.PrivateKey, msg []byte) (string, error) {
	digest := sha256.Sum256(msg)
	r, s, err := ecdsa.Sign(rand.Reader, priv, digest[:])
	if err != nil {
		return "", err
	}
	raw := make([]byte, 64)
	r.FillBytes(raw[:32])
	s.FillBytes(raw[32:])
	return B64.EncodeToString(raw), nil
}

// Verify checks a raw r||s signature over msg.
func Verify(pub *ecdsa.PublicKey, msg []byte, sig string) error {
	raw, err := B64.DecodeString(sig)
	if err != nil || len(raw) != 64 {
		return errors.New("signature: want 64 bytes base64url")
	}
	digest := sha256.Sum256(msg)
	r, s := new(big.Int).SetBytes(raw[:32]), new(big.Int).SetBytes(raw[32:])
	if !ecdsa.Verify(pub, digest[:], r, s) {
		return errors.New("signature: does not verify")
	}
	return nil
}

// GenerateEphemeral creates the per-handshake X25519 key. It is advertised
// only for the few seconds of one tap and then discarded.
func GenerateEphemeral() (*ecdh.PrivateKey, error) {
	return ecdh.X25519().GenerateKey(rand.Reader)
}

// SessionKey derives the shared key both phones use to exchange their
// profile cards over the short-range channel.
func SessionKey(priv *ecdh.PrivateKey, peerEphPub string, transcript []byte) ([]byte, error) {
	b, err := B64.DecodeString(peerEphPub)
	if err != nil {
		return nil, fmt.Errorf("peer key: %w", err)
	}
	peer, err := ecdh.X25519().NewPublicKey(b)
	if err != nil {
		return nil, fmt.Errorf("peer key: %w", err)
	}
	shared, err := priv.ECDH(peer)
	if err != nil {
		return nil, err
	}
	return hkdf.Key(sha256.New, shared, transcript, sessionInfo, 32)
}

func sorted(a, b string) (string, string) {
	if b < a {
		return b, a
	}
	return a, b
}

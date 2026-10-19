package protocol

import (
	"bytes"
	"crypto/ecdh"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// vectors is the cross-language contract in testdata/vectors.json.
type vectors struct {
	Claim           Claim  `json:"claim"`
	Canonical       string `json:"canonical"`
	PairKey         string `json:"pair_key"`
	Transcript      string `json:"transcript"`
	DevicePublicKey string `json:"device_public_key"`
	Signature       string `json:"signature"`
	X25519          struct {
		APriv      string `json:"a_priv_hex"`
		BPriv      string `json:"b_priv_hex"`
		APub       string `json:"a_pub"`
		BPub       string `json:"b_pub"`
		SessionKey string `json:"session_key_hex"`
	} `json:"x25519"`
	Receipt          Receipt `json:"receipt"`
	ReceiptCanonical string  `json:"receipt_canonical"`
}

const vectorsPath = "testdata/vectors.json"

func loadVectors(t *testing.T) vectors {
	t.Helper()
	if os.Getenv("TAP_UPDATE_VECTORS") == "1" {
		writeVectors(t)
	}
	b, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func fixedX25519(t *testing.T, fill byte) *ecdh.PrivateKey {
	t.Helper()
	k, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// writeVectors regenerates the vectors file. The device key is random, so
// the signature changes on every run; everything else is deterministic.
func writeVectors(t *testing.T) {
	t.Helper()
	var v vectors
	a, b := fixedX25519(t, 0x11), fixedX25519(t, 0x22)
	v.X25519.APriv = hex.EncodeToString(a.Bytes())
	v.X25519.BPriv = hex.EncodeToString(b.Bytes())
	v.X25519.APub = B64.EncodeToString(a.PublicKey().Bytes())
	v.X25519.BPub = B64.EncodeToString(b.PublicKey().Bytes())
	v.Claim = Claim{
		DeviceID: "dev_7f3a", Nonce: "bm9uY2UtYWxpY2U", PeerNonce: "bm9uY2UtYm9i",
		EphPub: v.X25519.APub, PeerEphPub: v.X25519.BPub,
		DistanceCM: 4, ElapsedMS: 1830, Geohash: "9yzgez3", Transport: TransportBLEUltrasonic,
	}
	v.Canonical = string(v.Claim.Canonical())
	v.PairKey = v.Claim.PairKey()
	v.Transcript = B64.EncodeToString(v.Claim.Transcript())
	sk, err := SessionKey(a, v.X25519.BPub, v.Claim.Transcript())
	if err != nil {
		t.Fatal(err)
	}
	v.X25519.SessionKey = hex.EncodeToString(sk)
	key, err := GenerateDeviceKey()
	if err != nil {
		t.Fatal(err)
	}
	if v.DevicePublicKey, err = EncodePublicKey(&key.PublicKey); err != nil {
		t.Fatal(err)
	}
	if v.Signature, err = Sign(key, v.Claim.Canonical()); err != nil {
		t.Fatal(err)
	}
	v.Receipt = Receipt{TapID: "tap_01", UserA: "usr_a", UserB: "usr_b", CreatedAt: 1792400000000, Geohash: "9yzgez3", PairKey: v.PairKey}
	v.ReceiptCanonical = string(v.Receipt.Canonical())
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vectorsPath, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVectors(t *testing.T) {
	v := loadVectors(t)
	if err := v.Claim.Validate(); err != nil {
		t.Fatalf("vector claim invalid: %v", err)
	}
	if got := string(v.Claim.Canonical()); got != v.Canonical {
		t.Errorf("canonical:\n got %q\nwant %q", got, v.Canonical)
	}
	if got := v.Claim.PairKey(); got != v.PairKey {
		t.Errorf("pair key: got %q want %q", got, v.PairKey)
	}
	if got := B64.EncodeToString(v.Claim.Transcript()); got != v.Transcript {
		t.Errorf("transcript: got %q want %q", got, v.Transcript)
	}
	pub, err := ParsePublicKey(v.DevicePublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, []byte(v.Canonical), v.Signature); err != nil {
		t.Errorf("vector signature: %v", err)
	}
	if got := string(v.Receipt.Canonical()); got != v.ReceiptCanonical {
		t.Errorf("receipt canonical:\n got %q\nwant %q", got, v.ReceiptCanonical)
	}

	// Both sides derive the same session key from opposite ends.
	a, b := fixedX25519(t, 0x11), fixedX25519(t, 0x22)
	ka, err := SessionKey(a, v.X25519.BPub, v.Claim.Transcript())
	if err != nil {
		t.Fatal(err)
	}
	kb, err := SessionKey(b, v.X25519.APub, v.Claim.Transcript())
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(ka) != v.X25519.SessionKey || !bytes.Equal(ka, kb) {
		t.Errorf("session keys disagree: a=%x b=%x want %s", ka, kb, v.X25519.SessionKey)
	}
}

func TestMirrorAndTranscriptSymmetry(t *testing.T) {
	v := loadVectors(t)
	c := v.Claim
	peer := Claim{
		DeviceID: "dev_b", Nonce: c.PeerNonce, PeerNonce: c.Nonce,
		EphPub: c.PeerEphPub, PeerEphPub: c.EphPub,
		DistanceCM: 5, ElapsedMS: 1700, Geohash: c.Geohash, Transport: c.Transport,
	}
	if !c.Mirrors(peer) || !peer.Mirrors(c) {
		t.Fatal("partner claims should mirror each other")
	}
	if c.PairKey() != peer.PairKey() {
		t.Error("pair key depends on which side asks")
	}
	if !bytes.Equal(c.Transcript(), peer.Transcript()) {
		t.Error("transcript depends on which side asks")
	}
	peer.PeerEphPub = c.PeerEphPub
	if c.Mirrors(peer) {
		t.Error("a claim with a swapped ephemeral key must not mirror")
	}
}

func TestSignatureRejectsTampering(t *testing.T) {
	key, err := GenerateDeviceKey()
	if err != nil {
		t.Fatal(err)
	}
	v := loadVectors(t)
	sig, err := Sign(key, v.Claim.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	tampered := v.Claim
	tampered.DistanceCM = 3
	if err := Verify(&key.PublicKey, tampered.Canonical(), sig); err == nil {
		t.Error("tampered claim verified")
	}
	if err := Verify(&key.PublicKey, v.Claim.Canonical(), "short"); err == nil {
		t.Error("malformed signature verified")
	}
}

func TestValidate(t *testing.T) {
	base := loadVectors(t).Claim
	cases := map[string]func(c *Claim){
		"empty device":   func(c *Claim) { c.DeviceID = "" },
		"newline":        func(c *Claim) { c.DeviceID = "a\nb" },
		"same nonces":    func(c *Claim) { c.PeerNonce = c.Nonce },
		"bad transport":  func(c *Claim) { c.Transport = "carrier_pigeon" },
		"short geohash":  func(c *Claim) { c.Geohash = "9yzg" },
		"bad geohash":    func(c *Claim) { c.Geohash = "9yzgeqa" },
		"negative range": func(c *Claim) { c.DistanceCM = -1 },
	}
	for name, mutate := range cases {
		c := base
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestGeohash(t *testing.T) {
	// Reference value from the geohash definition (Wikipedia worked example).
	if got := Geohash(42.6, -5.6, 5); got != "ezs42" {
		t.Errorf("geohash = %q", got)
	}
}

package feed

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeJSON(t *testing.T, dir, name string, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFetchUnsignedLocal(t *testing.T) {
	dir := t.TempDir()
	p := writeJSON(t, dir, "feed.json", Manifest{Version: 1, Tips: []Tip{{ID: "t1", Title: "hello"}}})
	c, err := NewClient(p, "")
	if err != nil {
		t.Fatal(err)
	}
	m, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Tips) != 1 || m.Tips[0].Title != "hello" {
		t.Errorf("unexpected manifest: %+v", m)
	}
}

func TestSignedFeedVerifies(t *testing.T) {
	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(nil)

	manifestBytes, _ := json.Marshal(Manifest{Version: 1, Tips: []Tip{{ID: "x", Title: "signed"}}})
	sig := ed25519.Sign(priv, manifestBytes)
	env := signedEnvelope{
		ManifestB64: base64.StdEncoding.EncodeToString(manifestBytes),
		Signature:   base64.StdEncoding.EncodeToString(sig),
	}
	p := writeJSON(t, dir, "signed.json", env)

	// Correct key verifies.
	c, err := NewClient(p, base64.StdEncoding.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Fetch(); err != nil {
		t.Fatalf("valid signature should verify: %v", err)
	}

	// Wrong key is rejected.
	otherPub, _, _ := ed25519.GenerateKey(nil)
	bad, _ := NewClient(p, base64.StdEncoding.EncodeToString(otherPub))
	if _, err := bad.Fetch(); err == nil {
		t.Fatal("expected verification failure with wrong key")
	}
}

func TestUpdateAvailable(t *testing.T) {
	Version = "0.1.0"
	m := &Manifest{Update: &UpdateInfo{LatestVersion: "0.2.0"}}
	if ok, _ := m.UpdateAvailable(); !ok {
		t.Error("0.2.0 should be newer than 0.1.0")
	}
	m2 := &Manifest{Update: &UpdateInfo{LatestVersion: "0.1.0"}}
	if ok, _ := m2.UpdateAvailable(); ok {
		t.Error("same version should not be an update")
	}
}

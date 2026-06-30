// Package feed implements the enterprise-hosted update channel. An admin publishes
// a signed JSON manifest at a URL the company controls; the binary polls it to
// refresh pricing, receive market/efficiency tips, and learn about new versions
// of itself. Signatures (ed25519) ensure a tampered or spoofed feed is rejected,
// and all fetching is opt-in via config — with no FeedURL set, the bundled local
// sample is used and nothing leaves the machine.
package feed

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/enterprise/aipet/internal/pricing"
)

// Manifest is the payload an enterprise publishes.
type Manifest struct {
	Version     int                      `json:"version"`      // manifest schema version
	GeneratedAt time.Time                `json:"generated_at"`
	Pricing     map[string]pricing.Rate  `json:"pricing"`      // model-key -> rate overrides
	Tips        []Tip                    `json:"tips"`         // market updates & guidance
	Update      *UpdateInfo              `json:"update"`       // self-update info, optional
}

// Tip is a market update or efficiency note shown in the TUI.
type Tip struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Body     string    `json:"body"`
	Category string    `json:"category"` // "pricing" | "model" | "efficiency" | "news"
	Date     time.Time `json:"date"`
	URL      string    `json:"url,omitempty"`
}

// UpdateInfo tells the binary about a newer release.
type UpdateInfo struct {
	LatestVersion string `json:"latest_version"`
	Notes         string `json:"notes"`
	DownloadURL   string `json:"download_url"`
	SHA256        string `json:"sha256"`
}

// signedEnvelope wraps a manifest with a detached signature. The manifest is
// carried as base64 of its exact raw bytes (ManifestB64) so it survives JSON
// re-formatting unchanged — ed25519 verification is byte-exact, so the bytes
// signed must equal the bytes verified. An inline Manifest is also accepted for
// the unsigned local sample, where no signature is checked.
type signedEnvelope struct {
	ManifestB64 string          `json:"manifest_b64"`
	Manifest    json.RawMessage `json:"manifest"`
	Signature   string          `json:"signature"` // base64 ed25519 over the manifest bytes
}

// Client fetches and verifies manifests.
type Client struct {
	URL       string
	PublicKey ed25519.PublicKey // nil disables verification (local sample only)
	HTTP      *http.Client
}

// NewClient builds a feed client. b64Key may be empty to skip verification.
func NewClient(url, b64Key string) (*Client, error) {
	c := &Client{URL: url, HTTP: &http.Client{Timeout: 15 * time.Second}}
	if b64Key != "" {
		raw, err := base64.StdEncoding.DecodeString(b64Key)
		if err != nil {
			return nil, fmt.Errorf("decode public key: %w", err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, errors.New("invalid ed25519 public key size")
		}
		c.PublicKey = ed25519.PublicKey(raw)
	}
	return c, nil
}

// Fetch returns the current manifest. file:// and bare local paths are read from
// disk (used by the bundled sample); http(s) URLs are fetched. The signature is
// verified whenever a public key is configured.
func (c *Client) Fetch() (*Manifest, error) {
	raw, err := c.read()
	if err != nil {
		return nil, err
	}
	return c.parse(raw)
}

func (c *Client) read() ([]byte, error) {
	u := c.URL
	switch {
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "https://"):
		resp, err := c.HTTP.Get(u)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("feed returned %s", resp.Status)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	case strings.HasPrefix(u, "file://"):
		return os.ReadFile(strings.TrimPrefix(u, "file://"))
	default:
		return os.ReadFile(u) // treat as a local path
	}
}

func (c *Client) parse(raw []byte) (*Manifest, error) {
	// Try the signed envelope first; fall back to a bare manifest for the
	// unsigned local sample.
	var env signedEnvelope
	if json.Unmarshal(raw, &env) == nil && env.Signature != "" {
		// Recover the exact signed bytes. The base64 form is authoritative;
		// fall back to inline bytes only when manifest_b64 is absent.
		var manifestBytes []byte
		if env.ManifestB64 != "" {
			b, err := base64.StdEncoding.DecodeString(env.ManifestB64)
			if err != nil {
				return nil, fmt.Errorf("decode manifest_b64: %w", err)
			}
			manifestBytes = b
		} else {
			manifestBytes = env.Manifest
		}
		if len(manifestBytes) > 0 {
			if c.PublicKey != nil {
				sig, err := base64.StdEncoding.DecodeString(env.Signature)
				if err != nil {
					return nil, fmt.Errorf("decode signature: %w", err)
				}
				if !ed25519.Verify(c.PublicKey, manifestBytes, sig) {
					return nil, errors.New("feed signature verification failed")
				}
			}
			return unmarshalManifest(manifestBytes)
		}
	}
	if c.PublicKey != nil {
		return nil, errors.New("feed is unsigned but a verification key is configured")
	}
	return unmarshalManifest(raw)
}

func unmarshalManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

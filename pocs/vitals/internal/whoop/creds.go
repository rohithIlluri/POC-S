package whoop

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/fsutil"
)

// FileName holds the WHOOP app credentials and the member's tokens. It is
// written 0600 like everything else in the vitals home.
const FileName = "whoop.json"

// DefaultRedirect is the OAuth redirect URI vitals listens on, for both
// `vitals whoop login` and the web dashboard. It must be registered, exactly,
// on the app in the WHOOP Developer Dashboard.
const DefaultRedirect = "http://localhost:8737/whoop/callback"

// Credentials is the persisted WHOOP state.
type Credentials struct {
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
	RedirectURL  string    `json:"redirect_url"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	MemberName   string    `json:"member_name,omitempty"`
	LastSync     time.Time `json:"last_sync,omitempty"`
}

// Configured reports whether an app's client credentials are present.
func (c Credentials) Configured() bool { return c.ClientID != "" && c.ClientSecret != "" }

// Connected reports whether a member has authorized vitals.
func (c Credentials) Connected() bool { return c.AccessToken != "" || c.RefreshToken != "" }

// LoadCredentials reads dir/whoop.json, then lets WHOOP_CLIENT_ID /
// WHOOP_CLIENT_SECRET / WHOOP_REDIRECT_URL from the environment fill in or
// override the app credentials. A missing file is not an error.
func LoadCredentials(dir string) (Credentials, error) {
	var c Credentials
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return c, err
	default:
		if err := json.Unmarshal(data, &c); err != nil {
			return c, fmt.Errorf("%s: %w", FileName, err)
		}
	}
	if v := os.Getenv("WHOOP_CLIENT_ID"); v != "" {
		c.ClientID = v
	}
	if v := os.Getenv("WHOOP_CLIENT_SECRET"); v != "" {
		c.ClientSecret = v
	}
	if v := os.Getenv("WHOOP_REDIRECT_URL"); v != "" {
		c.RedirectURL = v
	}
	if c.RedirectURL == "" {
		c.RedirectURL = DefaultRedirect
	}
	return c, nil
}

// SaveCredentials writes dir/whoop.json atomically with 0600 permissions.
func SaveCredentials(dir string, c Credentials) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(filepath.Join(dir, FileName), append(data, '\n'))
}

// Disconnect forgets the member's tokens but keeps the app credentials, so
// reconnecting is one login away.
func (c *Credentials) Disconnect() {
	c.AccessToken, c.RefreshToken, c.Scope, c.MemberName = "", "", "", ""
	c.ExpiresAt, c.LastSync = time.Time{}, time.Time{}
}

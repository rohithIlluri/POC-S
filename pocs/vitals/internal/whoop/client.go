// Package whoop connects vitals to the WHOOP Developer API (v2): OAuth 2.0
// authorization-code login, token refresh, paginated reads of cycles,
// recoveries, sleeps and workouts, and conversion of those records into
// vitals log entries.
//
// This is the only code in vitals that talks to the network, and it only
// runs when the user asks (`vitals whoop login|sync|logout`, or the matching
// buttons in `vitals serve`). It reads, never writes, WHOOP data.
package whoop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBase is WHOOP's production API host.
const DefaultBase = "https://api.prod.whoop.com"

const (
	authPath  = "/oauth/oauth2/auth"
	tokenPath = "/oauth/oauth2/token"
	apiPrefix = "/developer/v2"
	pageLimit = 25 // WHOOP's maximum page size
	maxPages  = 400
)

// Scopes requested at login. "offline" is what returns a refresh token, so
// syncing keeps working after the hour-long access token expires.
var Scopes = []string{"offline", "read:profile", "read:recovery", "read:cycles", "read:sleep", "read:workout", "read:body_measurement"}

// ErrNotConnected means there is no usable token; the user must log in.
var ErrNotConnected = errors.New("not connected to WHOOP — run `vitals whoop login`")

// NewState returns a random OAuth state value (WHOOP requires ≥ 8 chars).
func NewState() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// AuthURL is where the member's browser goes to approve vitals.
func AuthURL(base string, c Credentials, state string) string {
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {c.ClientID},
		"redirect_uri":  {c.RedirectURL},
		"scope":         {strings.Join(Scopes, " ")},
		"state":         {state},
	}
	return base + authPath + "?" + q.Encode()
}

// Client calls the WHOOP API on behalf of one member.
type Client struct {
	Base  string
	HTTP  *http.Client
	Creds Credentials
	// Save persists credentials whenever tokens change (WHOOP rotates the
	// refresh token on every refresh, so the new one must be kept).
	Save  func(Credentials) error
	Now   func() time.Time
	Sleep func(time.Duration)
}

// NewClient returns a client with production defaults.
func NewClient(c Credentials, save func(Credentials) error) *Client {
	return &Client{
		Base:  DefaultBase,
		HTTP:  &http.Client{Timeout: 30 * time.Second},
		Creds: c,
		Save:  save,
		Now:   time.Now,
		Sleep: time.Sleep,
	}
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// Exchange trades an authorization code for tokens and saves them.
func (cl *Client) Exchange(ctx context.Context, code string) error {
	return cl.token(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {cl.Creds.RedirectURL},
	})
}

func (cl *Client) refresh(ctx context.Context) error {
	if cl.Creds.RefreshToken == "" {
		return ErrNotConnected
	}
	return cl.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {cl.Creds.RefreshToken},
		"scope":         {"offline"},
	})
}

func (cl *Client) token(ctx context.Context, form url.Values) error {
	form.Set("client_id", cl.Creds.ClientID)
	form.Set("client_secret", cl.Creds.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cl.Base+tokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := cl.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("WHOOP token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		if form.Get("grant_type") == "refresh_token" && (resp.StatusCode == 400 || resp.StatusCode == 401) {
			return fmt.Errorf("WHOOP rejected the saved login (%s) — run `vitals whoop login` again", apiError(resp.StatusCode, body))
		}
		return fmt.Errorf("WHOOP token request failed: %s", apiError(resp.StatusCode, body))
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil || tr.AccessToken == "" {
		return errors.New("WHOOP token response had no access token")
	}
	cl.Creds.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		cl.Creds.RefreshToken = tr.RefreshToken
	}
	if tr.Scope != "" {
		cl.Creds.Scope = tr.Scope
	}
	cl.Creds.ExpiresAt = cl.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	if cl.Save != nil {
		return cl.Save(cl.Creds)
	}
	return nil
}

// apiError summarises an error response without echoing large bodies.
func apiError(status int, body []byte) string {
	var e struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
		Message     string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	msg := strings.TrimSpace(strings.Join([]string{e.Error, e.Description, e.Message}, " "))
	if msg == "" {
		return http.StatusText(status) + " (" + strconv.Itoa(status) + ")"
	}
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return fmt.Sprintf("%d: %s", status, msg)
}

// do sends an authenticated API request, refreshing the token when it is
// expired or rejected, and backing off on rate limits.
func (cl *Client) do(ctx context.Context, method, path string, q url.Values, out any) error {
	if !cl.Creds.Connected() {
		return ErrNotConnected
	}
	if cl.Creds.AccessToken == "" || !cl.Now().Before(cl.Creds.ExpiresAt.Add(-time.Minute)) {
		if err := cl.refresh(ctx); err != nil {
			return err
		}
	}
	refreshed := false
	for attempt := 0; ; attempt++ {
		u := cl.Base + apiPrefix + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+cl.Creds.AccessToken)
		req.Header.Set("Accept", "application/json")
		resp, err := cl.HTTP.Do(req)
		if err != nil {
			return fmt.Errorf("WHOOP %s: %w", path, err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusUnauthorized && !refreshed:
			refreshed = true
			if err := cl.refresh(ctx); err != nil {
				return err
			}
			continue
		case resp.StatusCode == http.StatusTooManyRequests && attempt < 4:
			wait := time.Duration(1<<attempt) * time.Second
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 && s <= 120 {
				wait = time.Duration(s) * time.Second
			}
			cl.Sleep(wait)
			continue
		case resp.StatusCode == http.StatusForbidden:
			return fmt.Errorf("WHOOP %s: permission denied — log in again to grant the read scopes (%s)", path, apiError(resp.StatusCode, body))
		case resp.StatusCode < 200 || resp.StatusCode > 299:
			return fmt.Errorf("WHOOP %s: %s", path, apiError(resp.StatusCode, body))
		}
		if out == nil || len(body) == 0 {
			return nil
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("WHOOP %s: unexpected response: %w", path, err)
		}
		return nil
	}
}

// Profile is the member's basic profile.
type Profile struct {
	UserID    ID     `json:"user_id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// Profile fetches the member's name, used to confirm who is connected.
func (cl *Client) Profile(ctx context.Context) (Profile, error) {
	var p Profile
	err := cl.do(ctx, http.MethodGet, "/user/profile/basic", nil, &p)
	return p, err
}

// Revoke tells WHOOP to drop vitals' access for this member.
func (cl *Client) Revoke(ctx context.Context) error {
	return cl.do(ctx, http.MethodDelete, "/user/access", nil, nil)
}

type page[T any] struct {
	Records   []T    `json:"records"`
	NextToken string `json:"next_token"`
}

func list[T any](ctx context.Context, cl *Client, path string, start, end time.Time) ([]T, error) {
	var all []T
	q := url.Values{
		"limit": {strconv.Itoa(pageLimit)},
		"start": {start.UTC().Format("2006-01-02T15:04:05.000Z")},
		"end":   {end.UTC().Format("2006-01-02T15:04:05.000Z")},
	}
	for i := 0; i < maxPages; i++ {
		var p page[T]
		if err := cl.do(ctx, http.MethodGet, path, q, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Records...)
		if p.NextToken == "" {
			return all, nil
		}
		q.Set("nextToken", p.NextToken)
	}
	return nil, fmt.Errorf("WHOOP %s: too many pages", path)
}

// Cycles lists physiological cycles (days) that start in [start, end).
func (cl *Client) Cycles(ctx context.Context, start, end time.Time) ([]Cycle, error) {
	return list[Cycle](ctx, cl, "/cycle", start, end)
}

// Recoveries lists recoveries in [start, end).
func (cl *Client) Recoveries(ctx context.Context, start, end time.Time) ([]Recovery, error) {
	return list[Recovery](ctx, cl, "/recovery", start, end)
}

// Sleeps lists sleeps (including naps) in [start, end).
func (cl *Client) Sleeps(ctx context.Context, start, end time.Time) ([]Sleep, error) {
	return list[Sleep](ctx, cl, "/activity/sleep", start, end)
}

// Workouts lists workouts in [start, end).
func (cl *Client) Workouts(ctx context.Context, start, end time.Time) ([]Workout, error) {
	return list[Workout](ctx, cl, "/activity/workout", start, end)
}

package whoop

import (
	"context"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
)

const (
	// FirstSyncDays is how far back the first sync reaches.
	FirstSyncDays = 30
	// overlap re-reads recent days on every sync: WHOOP rescores sleep and
	// the current cycle's strain keeps rising until the day ends.
	overlap = 3 * 24 * time.Hour
)

// Result summarises one sync.
type Result struct {
	From, To time.Time
	Added    int
	Updated  int
	Pending  int
}

// Window picks the time range to sync: an explicit number of days back, or
// since the last sync (minus an overlap), or FirstSyncDays for a first sync.
func Window(c Credentials, now time.Time, days int) (from, to time.Time) {
	switch {
	case days > 0:
		from = now.AddDate(0, 0, -days)
	case c.LastSync.IsZero():
		from = now.AddDate(0, 0, -FirstSyncDays)
	default:
		from = c.LastSync.Add(-overlap)
	}
	return from, now.Add(time.Minute)
}

// Sync fetches WHOOP data for the window and merges it into the log.
func Sync(ctx context.Context, cl *Client, s *store.Store, days int) (Result, error) {
	now := cl.Now()
	from, to := Window(cl.Creds, now, days)
	r := Result{From: from, To: to}
	if cl.Creds.MemberName == "" {
		if p, err := cl.Profile(ctx); err == nil {
			cl.Creds.MemberName = p.FirstName
		}
	}
	data, err := cl.Fetch(ctx, from, to)
	if err != nil {
		return r, err
	}
	entries, pending := Convert(data)
	r.Pending = pending
	if r.Added, r.Updated, err = s.Upsert(entries); err != nil {
		return r, err
	}
	cl.Creds.LastSync = now
	if cl.Save != nil {
		if err := cl.Save(cl.Creds); err != nil {
			return r, err
		}
	}
	return r, nil
}

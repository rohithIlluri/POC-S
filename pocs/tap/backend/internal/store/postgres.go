package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Postgres is the pilot Store. Every transaction runs SERIALIZABLE and is
// retried on serialization failures, so concurrent claims for one
// handshake can never both create a tap.
type Postgres struct {
	pool       *pgxpool.Pool
	maxRetries int
}

// OpenPostgres connects and applies any pending migrations.
func OpenPostgres(ctx context.Context, url string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	p := &Postgres{pool: pool, maxRetries: 20}
	if err := p.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return p, nil
}

func (p *Postgres) Close() { p.pool.Close() }

func (p *Postgres) migrate(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var done bool
		if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// DBError marks a database failure, as opposed to an error fn returned.
type DBError struct{ Err error }

func (e *DBError) Error() string { return "database: " + e.Err.Error() }
func (e *DBError) Unwrap() error { return e.Err }

func (p *Postgres) Tx(fn func(Tx) error) error {
	ctx := context.Background()
	for attempt := 0; ; attempt++ {
		err := p.once(ctx, fn)
		if !retryable(err) || attempt >= p.maxRetries {
			return err
		}
		time.Sleep(time.Duration(1+rand.IntN(5*(attempt+1))) * time.Millisecond)
	}
}

func (p *Postgres) once(ctx context.Context, fn func(Tx) error) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return &DBError{err}
	}
	t := &pgTx{ctx: ctx, tx: tx}
	ferr := fn(t)
	if t.err != nil {
		_ = tx.Rollback(ctx)
		return &DBError{t.err}
	}
	if ferr != nil {
		_ = tx.Rollback(ctx)
		return ferr
	}
	if err := tx.Commit(ctx); err != nil {
		return &DBError{err}
	}
	return nil
}

func retryable(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01")
}

// pgTx implements Tx. The interface has no error returns (the in-memory
// store can't fail), so the first database error is kept in err, later
// calls become no-ops, and Postgres.Tx rolls back and reports it.
type pgTx struct {
	ctx context.Context
	tx  pgx.Tx
	err error
}

func (t *pgTx) exec(sql string, args ...any) {
	if t.err != nil {
		return
	}
	_, t.err = t.tx.Exec(t.ctx, sql, args...)
}

// one scans a single row; it reports false when there is no row or a
// previous call failed.
func (t *pgTx) one(scan func(pgx.Row) error, sql string, args ...any) bool {
	if t.err != nil {
		return false
	}
	err := scan(t.tx.QueryRow(t.ctx, sql, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	t.err = err
	return err == nil
}

func (t *pgTx) many(scan func(pgx.Rows) error, sql string, args ...any) {
	if t.err != nil {
		return
	}
	rows, err := t.tx.Query(t.ctx, sql, args...)
	if err != nil {
		t.err = err
		return
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			t.err = err
			return
		}
	}
	t.err = rows.Err()
}

func (t *pgTx) scalar(dest any, sql string, args ...any) {
	t.one(func(r pgx.Row) error { return r.Scan(dest) }, sql, args...)
}

func (t *pgTx) jsonArg(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil && t.err == nil {
		t.err = err
	}
	return b
}

// ---- users, sessions, devices ----

const userCols = `id, display_name, photo_url, phone_hash, created_at, frozen`

func scanUser(u *User) func(pgx.Row) error {
	return func(r pgx.Row) error {
		return r.Scan(&u.ID, &u.DisplayName, &u.PhotoURL, &u.PhoneHash, &u.CreatedAt, &u.Frozen)
	}
}

func (t *pgTx) User(id string) (User, bool) {
	var u User
	ok := t.one(scanUser(&u), `SELECT `+userCols+` FROM users WHERE id = $1`, id)
	return u, ok
}

func (t *pgTx) UserByPhone(h string) (User, bool) {
	var u User
	ok := t.one(scanUser(&u), `SELECT `+userCols+` FROM users WHERE phone_hash = $1`, h)
	return u, ok
}

func (t *pgTx) PutUser(u User) {
	t.exec(`INSERT INTO users (`+userCols+`) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET display_name = $2, photo_url = $3, phone_hash = $4, frozen = $6`,
		u.ID, u.DisplayName, u.PhotoURL, u.PhoneHash, u.CreatedAt, u.Frozen)
}

func (t *pgTx) DeleteUser(id string) {
	t.exec(`UPDATE taps SET user_a = 'deleted', device_a = '' WHERE user_a = $1`, id)
	t.exec(`UPDATE taps SET user_b = 'deleted', device_b = '' WHERE user_b = $1`, id)
	t.exec(`DELETE FROM users WHERE id = $1`, id) // cascades to everything else
}

func (t *pgTx) PutSession(token, userID string, expires time.Time) {
	t.exec(`INSERT INTO sessions (token, user_id, expires) VALUES ($1, $2, $3)
		ON CONFLICT (token) DO UPDATE SET user_id = $2, expires = $3`, token, userID, expires)
}

func (t *pgTx) SessionUser(token string, now time.Time) (string, bool) {
	var id string
	ok := t.one(func(r pgx.Row) error { return r.Scan(&id) },
		`SELECT user_id FROM sessions WHERE token = $1 AND expires >= $2`, token, now)
	return id, ok
}

const deviceCols = `id, user_id, public_key, attest_status, created_at`

func scanDevice(d *Device) func(pgx.Row) error {
	return func(r pgx.Row) error { return r.Scan(&d.ID, &d.UserID, &d.PublicKey, &d.AttestStatus, &d.CreatedAt) }
}

func (t *pgTx) Device(id string) (Device, bool) {
	var d Device
	ok := t.one(scanDevice(&d), `SELECT `+deviceCols+` FROM devices WHERE id = $1`, id)
	return d, ok
}

func (t *pgTx) DevicesOf(userID string) []Device {
	var out []Device
	t.many(func(r pgx.Rows) error {
		var d Device
		if err := scanDevice(&d)(r); err != nil {
			return err
		}
		out = append(out, d)
		return nil
	}, `SELECT `+deviceCols+` FROM devices WHERE user_id = $1 ORDER BY created_at`, userID)
	return out
}

func (t *pgTx) PutDevice(d Device) {
	t.exec(`INSERT INTO devices (`+deviceCols+`) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET public_key = $3, attest_status = $4`,
		d.ID, d.UserID, d.PublicKey, d.AttestStatus, d.CreatedAt)
}

// ---- nonces and pending claims ----

func (t *pgTx) Nonce(v string) (Nonce, bool) {
	var n Nonce
	ok := t.one(func(r pgx.Row) error { return r.Scan(&n.Value, &n.DeviceID, &n.IssuedAt, &n.Used) },
		`SELECT value, device_id, issued_at, used FROM nonces WHERE value = $1`, v)
	return n, ok
}

func (t *pgTx) PutNonce(n Nonce) {
	t.exec(`INSERT INTO nonces (value, device_id, issued_at, used) VALUES ($1, $2, $3, $4)
		ON CONFLICT (value) DO UPDATE SET used = $4`, n.Value, n.DeviceID, n.IssuedAt, n.Used)
}

func (t *pgTx) PendingClaims(k string) []PendingClaim {
	var out []PendingClaim
	t.many(func(r pgx.Rows) error {
		var pc PendingClaim
		var raw []byte
		if err := r.Scan(&pc.PairKey, &pc.UserID, &raw, &pc.IssuedAt, &pc.ReceivedAt); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &pc.Claim); err != nil {
			return err
		}
		out = append(out, pc)
		return nil
	}, `SELECT pair_key, user_id, claim, issued_at, received_at FROM pending_claims WHERE pair_key = $1 ORDER BY received_at`, k)
	return out
}

func (t *pgTx) PutPendingClaim(pc PendingClaim) {
	t.exec(`INSERT INTO pending_claims (pair_key, device_id, user_id, claim, issued_at, received_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (pair_key, device_id) DO UPDATE SET user_id = $3, claim = $4, issued_at = $5, received_at = $6`,
		pc.PairKey, pc.Claim.DeviceID, pc.UserID, t.jsonArg(pc.Claim), pc.IssuedAt, pc.ReceivedAt)
}

func (t *pgTx) DeletePendingClaim(k, deviceID string) {
	t.exec(`DELETE FROM pending_claims WHERE pair_key = $1 AND device_id = $2`, k, deviceID)
}

// ---- taps ----

const tapCols = `id, user_a, user_b, device_a, device_b, created_at, geohash, event_id, pair_key, receipt, signature`

func scanTap(tp *Tap) func(pgx.Row) error {
	return func(r pgx.Row) error {
		var raw []byte
		if err := r.Scan(&tp.ID, &tp.UserA, &tp.UserB, &tp.DeviceA, &tp.DeviceB, &tp.CreatedAt,
			&tp.Geohash, &tp.EventID, &tp.PairKey, &raw, &tp.Signature); err != nil {
			return err
		}
		return json.Unmarshal(raw, &tp.Receipt)
	}
}

func (t *pgTx) Tap(id string) (Tap, bool) {
	var tp Tap
	ok := t.one(scanTap(&tp), `SELECT `+tapCols+` FROM taps WHERE id = $1`, id)
	return tp, ok
}

func (t *pgTx) TapByPairKey(k string) (Tap, bool) {
	var tp Tap
	ok := t.one(scanTap(&tp), `SELECT `+tapCols+` FROM taps WHERE pair_key = $1`, k)
	return tp, ok
}

func (t *pgTx) PutTap(tp Tap) {
	g6 := tp.Geohash
	if len(g6) > 6 {
		g6 = g6[:6]
	}
	t.exec(`INSERT INTO taps (`+tapCols+`, geohash6) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET user_a = $2, user_b = $3, device_a = $4, device_b = $5, event_id = $8`,
		tp.ID, tp.UserA, tp.UserB, tp.DeviceA, tp.DeviceB, tp.CreatedAt, tp.Geohash, tp.EventID, tp.PairKey,
		t.jsonArg(tp.Receipt), tp.Signature, g6)
}

func (t *pgTx) TapsByDeviceSince(deviceID string, since time.Time) int {
	var n int
	t.scalar(&n, `SELECT count(*) FROM taps WHERE (device_a = $1 OR device_b = $1) AND created_at >= $2`, deviceID, since)
	return n
}

func (t *pgTx) TapsNearSince(g6 string, since time.Time) []Tap {
	var out []Tap
	t.many(func(r pgx.Rows) error {
		var tp Tap
		if err := scanTap(&tp)(r); err != nil {
			return err
		}
		out = append(out, tp)
		return nil
	}, `SELECT `+tapCols+` FROM taps WHERE geohash6 = $1 AND created_at >= $2 ORDER BY seq`, g6, since)
	return out
}

// ---- edges ----

const edgeCols = `user_a, user_b, created_at, first_tap_id, last_tap_at, tap_count, status`

func scanEdge(e *Edge) func(pgx.Row) error {
	return func(r pgx.Row) error {
		return r.Scan(&e.UserA, &e.UserB, &e.CreatedAt, &e.FirstTapID, &e.LastTapAt, &e.TapCount, &e.Status)
	}
}

func (t *pgTx) Edge(a, b string) (Edge, bool) {
	a, b = Pair(a, b)
	var e Edge
	ok := t.one(scanEdge(&e), `SELECT `+edgeCols+` FROM edges WHERE user_a = $1 AND user_b = $2`, a, b)
	return e, ok
}

func (t *pgTx) EdgesOf(userID string) []Edge {
	var out []Edge
	t.many(func(r pgx.Rows) error {
		var e Edge
		if err := scanEdge(&e)(r); err != nil {
			return err
		}
		out = append(out, e)
		return nil
	}, `SELECT `+edgeCols+` FROM edges WHERE user_a = $1 OR user_b = $1 ORDER BY created_at DESC`, userID)
	return out
}

func (t *pgTx) PutEdge(e Edge) {
	t.exec(`INSERT INTO edges (`+edgeCols+`) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_a, user_b) DO UPDATE SET last_tap_at = $5, tap_count = $6, status = $7`,
		e.UserA, e.UserB, e.CreatedAt, e.FirstTapID, e.LastTapAt, e.TapCount, string(e.Status))
}

func (t *pgTx) Meta(owner, other string) EdgeMeta {
	m := EdgeMeta{OwnerID: owner, OtherID: other}
	t.one(func(r pgx.Row) error { return r.Scan(&m.Note, &m.HidePlace) },
		`SELECT note, hide_place FROM edge_meta WHERE owner_id = $1 AND other_id = $2`, owner, other)
	return m
}

func (t *pgTx) PutMeta(m EdgeMeta) {
	t.exec(`INSERT INTO edge_meta (owner_id, other_id, note, hide_place) VALUES ($1, $2, $3, $4)
		ON CONFLICT (owner_id, other_id) DO UPDATE SET note = $3, hide_place = $4`,
		m.OwnerID, m.OtherID, m.Note, m.HidePlace)
}

// ---- safety ----

func (t *pgTx) Blocked(a, b string) bool {
	var blocked bool
	t.scalar(&blocked, `SELECT EXISTS (SELECT 1 FROM blocks WHERE (blocker_id = $1 AND blocked_id = $2) OR (blocker_id = $2 AND blocked_id = $1))`, a, b)
	return blocked
}

func (t *pgTx) PutBlock(blocker, blocked string) {
	t.exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, blocker, blocked)
}

func (t *pgTx) PutReport(r Report) {
	t.exec(`INSERT INTO reports (id, reporter_id, subject_id, reason, created_at) VALUES ($1, $2, $3, $4, $5)`,
		r.ID, r.ReporterID, r.SubjectID, r.Reason, r.CreatedAt)
}

func (t *pgTx) DistinctReporters(subject string) int {
	var n int
	t.scalar(&n, `SELECT count(DISTINCT reporter_id) FROM reports WHERE subject_id = $1`, subject)
	return n
}

// ---- events ----

const eventCols = `id, name, geohash6, start_at, end_at, tap_count`

func scanEvent(e *Event) func(pgx.Row) error {
	return func(r pgx.Row) error { return r.Scan(&e.ID, &e.Name, &e.Geohash6, &e.Start, &e.End, &e.TapCount) }
}

func (t *pgTx) members(e *Event) {
	e.Members = nil
	t.many(func(r pgx.Rows) error {
		var id string
		if err := r.Scan(&id); err != nil {
			return err
		}
		e.Members = append(e.Members, id)
		return nil
	}, `SELECT user_id FROM event_members WHERE event_id = $1 ORDER BY user_id`, e.ID)
}

func (t *pgTx) Event(id string) (Event, bool) {
	var e Event
	if !t.one(scanEvent(&e), `SELECT `+eventCols+` FROM events WHERE id = $1`, id) {
		return Event{}, false
	}
	t.members(&e)
	return e, true
}

func (t *pgTx) EventsOf(userID string) []Event {
	var out []Event
	t.many(func(r pgx.Rows) error {
		var e Event
		if err := scanEvent(&e)(r); err != nil {
			return err
		}
		out = append(out, e)
		return nil
	}, `SELECT e.id, e.name, e.geohash6, e.start_at, e.end_at, e.tap_count FROM events e
		JOIN event_members m ON m.event_id = e.id WHERE m.user_id = $1 ORDER BY e.start_at DESC`, userID)
	for i := range out {
		t.members(&out[i])
	}
	return out
}

func (t *pgTx) ActiveEventNear(g6 string, since time.Time) (Event, bool) {
	var e Event
	if !t.one(scanEvent(&e), `SELECT `+eventCols+` FROM events WHERE geohash6 = $1 AND end_at >= $2 ORDER BY end_at DESC LIMIT 1`, g6, since) {
		return Event{}, false
	}
	t.members(&e)
	return e, true
}

// PutEvent upserts the event and makes its member rows match e.Members.
// Ids of deleted accounts are skipped.
func (t *pgTx) PutEvent(e Event) {
	t.exec(`INSERT INTO events (`+eventCols+`) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET name = $2, start_at = $4, end_at = $5, tap_count = $6`,
		e.ID, e.Name, e.Geohash6, e.Start, e.End, e.TapCount)
	members := e.Members
	if members == nil {
		members = []string{}
	}
	t.exec(`DELETE FROM event_members WHERE event_id = $1 AND NOT (user_id = ANY($2))`, e.ID, members)
	t.exec(`INSERT INTO event_members (event_id, user_id)
		SELECT $1, u FROM unnest($2::text[]) AS u WHERE EXISTS (SELECT 1 FROM users WHERE id = u)
		ON CONFLICT DO NOTHING`, e.ID, members)
}

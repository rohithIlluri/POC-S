// Package strapdb is vitals' database for what the strap records: one
// embedded, pure-Go key-value file (bbolt) in the vitals home. No server, no
// cgo, no network.
//
// Layout. Every key starts with a big-endian time or number, so a cursor
// walks the data in order and a day or a night is one contiguous range read:
//
//	hr       unix ms (8)            → src, hr, n, n × RR u16     one row per sample
//	events   unix ms (8) + number(2) → event data
//	records  record number (4)      → raw 0x2F history body, kept so later
//	                                  decoders can re-read fields we don't parse yet
//	raw      sequence (8)           → any other packet from a history burst (console
//	                                  logs, motion...), kept verbatim, append-only
//	days     YYYY-MM-DD             → JSON summary (derived; safe to delete)
//	sync     named keys             → history-sync state
//	meta     named keys             → schema version, day zero, profile
//
// Writes are idempotent: a sample, event or record written twice replaces
// itself, so a re-sync never duplicates data.
//
// Day zero. Nothing is stored for a time before the strap's clock was valid
// (a strap that lost its clock stamps data from 1971) or before day zero, the
// local date of the first sample accepted. Metrics count days from there.
package strapdb

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

// FileName is the database file inside the vitals home.
const FileName = "strap.db"

const schemaVersion = 1

// MinValidTime is the earliest timestamp accepted. A strap whose clock was
// never set counts from 1971, so anything earlier is refused.
var MinValidTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// ErrClockUnset is returned for a sample or event stamped before MinValidTime.
var ErrClockUnset = errors.New("strapdb: timestamp is before 2020; the strap's clock is not set")

// Source says where a sample came from.
type Source byte

const (
	Realtime Source = 0 // live 1 Hz stream
	History  Source = 1 // downloaded from the strap's memory
)

var (
	bHR      = []byte("hr")
	bEvents  = []byte("events")
	bRecords = []byte("records")
	bRaw     = []byte("raw")
	bDays    = []byte("days")
	bSync    = []byte("sync")
	bMeta    = []byte("meta")
)

// DB is an open database. Safe for concurrent use.
type DB struct {
	b   *bolt.DB
	loc *time.Location
	// chk keeps bbolt's integrity check apart from writes. Check run while a
	// write commits reports false "reachable freed / unreachable unfreed"
	// pages, so every write holds the shared side and Check takes the
	// exclusive side. Reads never take it.
	chk sync.RWMutex
}

// update runs a write transaction.
func (d *DB) update(fn func(*bolt.Tx) error) error {
	d.chk.RLock()
	defer d.chk.RUnlock()
	return d.b.Update(fn)
}

// Open creates dir (0700: health data is private) and opens dir/strap.db.
// loc decides which local calendar day a time belongs to.
func Open(dir string, loc *time.Location) (*DB, error) {
	if loc == nil {
		loc = time.Local
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	b, err := bolt.Open(filepath.Join(dir, FileName), 0o600, &bolt.Options{
		Timeout:        2 * time.Second,
		FreelistType:   bolt.FreelistMapType,
		NoFreelistSync: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", FileName, err)
	}
	db := &DB{b: b, loc: loc}
	err = b.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bHR, bEvents, bRecords, bRaw, bDays, bSync, bMeta} {
			bk, err := tx.CreateBucketIfNotExists(name)
			if err != nil {
				return err
			}
			bk.FillPercent = 1.0 // keys only ever append in time order
		}
		m := tx.Bucket(bMeta)
		if m.Get([]byte("schema")) == nil {
			return m.Put([]byte("schema"), []byte{schemaVersion})
		}
		return nil
	})
	if err != nil {
		b.Close()
		return nil, err
	}
	db.restoreProfile(dir)
	return db, nil
}

// ProfileFile is a private backup of the profile next to the database. The
// database is the source of truth; this copy means your date of birth survives
// a lost, reset or replaced database file.
const ProfileFile = "profile.json"

// restoreProfile copies the backup into the database when the database has no
// profile yet. Best effort: a missing or unreadable backup is simply ignored.
func (d *DB) restoreProfile(dir string) {
	if p, _ := d.Profile(); p != (Profile{}) {
		return
	}
	raw, err := os.ReadFile(filepath.Join(dir, ProfileFile))
	if err != nil {
		return
	}
	var p Profile
	if json.Unmarshal(raw, &p) == nil && p != (Profile{}) {
		d.SetProfile(p)
	}
}

// writeProfileBackup writes the profile atomically with private permissions.
func (d *DB) writeProfileBackup(p Profile) {
	dir := filepath.Dir(d.b.Path())
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".profile-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(append(raw, '\n'))
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Chmod(tmp.Name(), 0o600) != nil || os.Rename(tmp.Name(), filepath.Join(dir, ProfileFile)) != nil {
		os.Remove(tmp.Name())
	}
}

// Close flushes and closes the file.
func (d *DB) Close() error { return d.b.Close() }

// Check runs bbolt's full integrity check and returns the first problems found
// (nil when the file is sound).
func (d *DB) Check() error {
	d.chk.Lock()
	defer d.chk.Unlock()
	var errs []error
	err := d.b.View(func(tx *bolt.Tx) error {
		for e := range tx.Check() {
			if len(errs) < 5 {
				errs = append(errs, e)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return errors.Join(errs...)
}

// Latest returns the time of the newest stored sample; ok is false when none.
func (d *DB) Latest() (t time.Time, ok bool, err error) {
	s, ok, err := d.LatestSample()
	return s.Time, ok, err
}

// LatestSample returns the newest stored sample; ok is false when none.
func (d *DB) LatestSample() (s Sample, ok bool, err error) {
	err = d.b.View(func(tx *bolt.Tx) error {
		if k, v := tx.Bucket(bHR).Cursor().Last(); k != nil {
			s, ok = decodeSample(k, v)
		}
		return nil
	})
	return
}

// Path is the database file location.
func (d *DB) Path() string { return d.b.Path() }

// Size is the database file size in bytes.
func (d *DB) Size() (int64, error) {
	var n int64
	err := d.b.View(func(tx *bolt.Tx) error { n = tx.Size(); return nil })
	return n, err
}

// Sample is one second of heart data.
type Sample struct {
	Time   time.Time
	HR     uint8
	RR     []uint16 // beat-to-beat intervals in ms
	Source Source
	// OffWrist marks a sample taken while the strap was not on a body. It is
	// stored (wear time is a metric) but is not used for heart-rate metrics.
	OffWrist bool
}

const offWristBit = 0x80 // kept in the high bit of the source byte

func tsKey(t time.Time) []byte {
	var k [8]byte
	binary.BigEndian.PutUint64(k[:], uint64(t.UnixMilli()))
	return k[:]
}

func encodeSample(s Sample) []byte {
	v := make([]byte, 3+2*len(s.RR))
	v[0], v[1], v[2] = byte(s.Source), s.HR, byte(len(s.RR))
	if s.OffWrist {
		v[0] |= offWristBit
	}
	for i, r := range s.RR {
		binary.LittleEndian.PutUint16(v[3+2*i:], r)
	}
	return v
}

func decodeSample(k, v []byte) (Sample, bool) {
	if len(k) != 8 || len(v) < 3 || len(v) != 3+2*int(v[2]) {
		return Sample{}, false
	}
	s := Sample{
		Time:     time.UnixMilli(int64(binary.BigEndian.Uint64(k))),
		Source:   Source(v[0] &^ offWristBit),
		OffWrist: v[0]&offWristBit != 0,
		HR:       v[1],
	}
	if n := int(v[2]); n > 0 {
		s.RR = make([]uint16, n)
		for i := range s.RR {
			s.RR[i] = binary.LittleEndian.Uint16(v[3+2*i:])
		}
	}
	return s, true
}

// PutSamples stores samples in one transaction. A sample before MinValidTime
// fails the whole call with ErrClockUnset and nothing is written. The first
// accepted sample fixes day zero.
func (d *DB) PutSamples(samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	for _, s := range samples {
		if s.Time.Before(MinValidTime) {
			return ErrClockUnset
		}
	}
	return d.update(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bHR)
		for _, s := range samples {
			if err := bk.Put(tsKey(s.Time), encodeSample(s)); err != nil {
				return err
			}
		}
		if err := d.invalidate(tx, samples); err != nil {
			return err
		}
		return d.fixDayZero(tx, samples[0].Time)
	})
}

// invalidate drops the cached summaries a write can change: the day of each
// sample and the next day, whose overnight window starts the evening before.
func (d *DB) invalidate(tx *bolt.Tx, samples []Sample) error {
	days := tx.Bucket(bDays)
	seen := map[string]bool{}
	for _, s := range samples {
		day := d.Day(s.Time)
		if seen[day] {
			continue
		}
		seen[day] = true
		if err := days.Delete([]byte(day)); err != nil {
			return err
		}
		if err := days.Delete([]byte(d.Day(s.Time.AddDate(0, 0, 1)))); err != nil {
			return err
		}
	}
	return nil
}

// Samples calls fn for each sample in [from, to) in time order until fn
// returns false.
func (d *DB) Samples(from, to time.Time, fn func(Sample) bool) error {
	return d.b.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bHR).Cursor()
		end := tsKey(to)
		for k, v := c.Seek(tsKey(from)); k != nil && bytes.Compare(k, end) < 0; k, v = c.Next() {
			if s, ok := decodeSample(k, v); ok && !fn(s) {
				return nil
			}
		}
		return nil
	})
}

// CountSamples returns how many samples fall in [from, to).
func (d *DB) CountSamples(from, to time.Time) (n int, err error) {
	err = d.Samples(from, to, func(Sample) bool { n++; return true })
	return
}

// Event is one housekeeping packet from the strap.
type Event struct {
	Time   time.Time
	Number uint16
	Data   []byte
}

func eventKey(t time.Time, n uint16) []byte {
	k := make([]byte, 10)
	binary.BigEndian.PutUint64(k, uint64(t.UnixMilli()))
	binary.BigEndian.PutUint16(k[8:], n)
	return k
}

// PutEvent stores one event. Events before MinValidTime are refused.
func (d *DB) PutEvent(e Event) error {
	if e.Time.Before(MinValidTime) {
		return ErrClockUnset
	}
	return d.update(func(tx *bolt.Tx) error {
		return tx.Bucket(bEvents).Put(eventKey(e.Time, e.Number), append([]byte(nil), e.Data...))
	})
}

// Events calls fn for each event in [from, to) in time order.
func (d *DB) Events(from, to time.Time, fn func(Event) bool) error {
	return d.b.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bEvents).Cursor()
		end := tsKey(to)
		for k, v := c.Seek(tsKey(from)); k != nil && bytes.Compare(k[:8], end) < 0; k, v = c.Next() {
			e := Event{
				Time:   time.UnixMilli(int64(binary.BigEndian.Uint64(k))),
				Number: binary.BigEndian.Uint16(k[8:]),
				Data:   append([]byte(nil), v...),
			}
			if !fn(e) {
				return nil
			}
		}
		return nil
	})
}

// Record is one raw 0x2F history body, kept verbatim.
type Record struct {
	Number uint32
	Body   []byte
}

func recordKey(n uint32) []byte {
	var k [4]byte
	binary.BigEndian.PutUint32(k[:], n)
	return k[:]
}

// PutRecords stores raw history records in one transaction.
func (d *DB) PutRecords(recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	return d.update(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bRecords)
		for _, r := range recs {
			if err := bk.Put(recordKey(r.Number), r.Body); err != nil {
				return err
			}
		}
		return nil
	})
}

// Record returns the raw body of one history record.
func (d *DB) Record(n uint32) (body []byte, ok bool, err error) {
	err = d.b.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket(bRecords).Get(recordKey(n)); v != nil {
			body, ok = append([]byte(nil), v...), true
		}
		return nil
	})
	return
}

// Records calls fn for every stored raw history record in record-number order
// until fn returns false. The body is only valid during the call.
func (d *DB) Records(fn func(Record) bool) error {
	return d.b.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bRecords).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if len(k) != 4 {
				continue
			}
			if !fn(Record{Number: binary.BigEndian.Uint32(k), Body: v}) {
				return nil
			}
		}
		return nil
	})
}

// ReplaceHistorySamples deletes the History-source samples stored at the given
// times and then writes add, all in one transaction. Realtime samples at the
// same times are left alone. It exists to repair samples that were decoded from
// the wrong record layout: the raw records are the source of truth.
func (d *DB) ReplaceHistorySamples(remove []time.Time, add []Sample) (removed int, err error) {
	for _, s := range add {
		if s.Time.Before(MinValidTime) {
			return 0, ErrClockUnset
		}
	}
	err = d.update(func(tx *bolt.Tx) error {
		hb := tx.Bucket(bHR)
		touched := make([]Sample, 0, len(remove)+len(add))
		for _, t := range remove {
			k := tsKey(t)
			if v := hb.Get(k); v != nil {
				if s, ok := decodeSample(k, v); ok && s.Source == History {
					if err := hb.Delete(k); err != nil {
						return err
					}
					removed++
					touched = append(touched, s)
				}
			}
		}
		for _, s := range add {
			if err := hb.Put(tsKey(s.Time), encodeSample(s)); err != nil {
				return err
			}
			touched = append(touched, s)
		}
		if len(touched) == 0 {
			return nil
		}
		return d.invalidate(tx, touched)
	})
	return removed, err
}

// SaveBurst stores a burst of history (its raw records plus the samples
// decoded from them) and the sync state in ONE transaction. This is the
// "persist before acknowledging" step: only after it returns nil may the
// strap be told it can free that memory.
func (d *DB) SaveBurst(recs []Record, samples []Sample, other [][]byte, st SyncState) error {
	for _, s := range samples {
		if s.Time.Before(MinValidTime) {
			return ErrClockUnset
		}
	}
	return d.update(func(tx *bolt.Tx) error {
		rb, hb, ob := tx.Bucket(bRecords), tx.Bucket(bHR), tx.Bucket(bRaw)
		for _, r := range recs {
			if err := rb.Put(recordKey(r.Number), r.Body); err != nil {
				return err
			}
		}
		for _, body := range other {
			n, err := ob.NextSequence()
			if err != nil {
				return err
			}
			var k [8]byte
			binary.BigEndian.PutUint64(k[:], n)
			if err := ob.Put(k[:], body); err != nil {
				return err
			}
		}
		for _, s := range samples {
			if err := hb.Put(tsKey(s.Time), encodeSample(s)); err != nil {
				return err
			}
		}
		if len(samples) > 0 {
			if err := d.invalidate(tx, samples); err != nil {
				return err
			}
			if err := d.fixDayZero(tx, samples[0].Time); err != nil {
				return err
			}
		}
		return putSyncState(tx, st)
	})
}

// RawBodies calls fn with every stored non-record packet body (console logs,
// events and the like) in the order they were received, until fn returns false.
// The body is only valid during the call.
func (d *DB) RawBodies(fn func([]byte) bool) error {
	return d.b.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bRaw).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if !fn(v) {
				return nil
			}
		}
		return nil
	})
}

// RawPackets is how many non-record packets from history bursts are stored.
func (d *DB) RawPackets() (n int, err error) {
	err = d.b.View(func(tx *bolt.Tx) error { n = tx.Bucket(bRaw).Stats().KeyN; return nil })
	return
}

// SyncState is where the last history sync stopped.
type SyncState struct {
	LastRecord uint32    // highest record number stored
	LastToken  [8]byte   // token of the last acknowledged burst
	SyncedAt   time.Time // when that burst was stored
}

func putSyncState(tx *bolt.Tx, st SyncState) error {
	b := tx.Bucket(bSync)
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], st.LastRecord)
	if err := b.Put([]byte("last_record"), n[:]); err != nil {
		return err
	}
	if err := b.Put([]byte("last_token"), st.LastToken[:]); err != nil {
		return err
	}
	var t [8]byte
	binary.BigEndian.PutUint64(t[:], uint64(st.SyncedAt.Unix()))
	return b.Put([]byte("synced_at"), t[:])
}

// SetSyncState records sync progress on its own.
func (d *DB) SetSyncState(st SyncState) error {
	return d.update(func(tx *bolt.Tx) error { return putSyncState(tx, st) })
}

// SyncState returns the last sync progress (zero value before the first sync).
func (d *DB) SyncState() (st SyncState, err error) {
	err = d.b.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bSync)
		if v := b.Get([]byte("last_record")); len(v) == 4 {
			st.LastRecord = binary.BigEndian.Uint32(v)
		}
		if v := b.Get([]byte("last_token")); len(v) == 8 {
			copy(st.LastToken[:], v)
		}
		if v := b.Get([]byte("synced_at")); len(v) == 8 {
			st.SyncedAt = time.Unix(int64(binary.BigEndian.Uint64(v)), 0)
		}
		return nil
	})
	return
}

// Day returns the local calendar day (YYYY-MM-DD) a time belongs to.
func (d *DB) Day(t time.Time) string { return t.In(d.loc).Format("2006-01-02") }

// DayBounds returns the start and end of a local calendar day.
func (d *DB) DayBounds(day string) (from, to time.Time, err error) {
	t, err := time.ParseInLocation("2006-01-02", day, d.loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("bad day %q: %w", day, err)
	}
	return t, t.AddDate(0, 0, 1), nil
}

// fixDayZero records the local date of the first sample, once.
func (d *DB) fixDayZero(tx *bolt.Tx, first time.Time) error {
	m := tx.Bucket(bMeta)
	if m.Get([]byte("day_zero")) != nil {
		return nil
	}
	return m.Put([]byte("day_zero"), []byte(d.Day(first)))
}

// DayZero returns the local date of the first stored sample.
func (d *DB) DayZero() (day string, ok bool, err error) {
	err = d.b.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket(bMeta).Get([]byte("day_zero")); v != nil {
			day, ok = string(v), true
		}
		return nil
	})
	return
}

// Profile is what vitals needs to know about you. It stays in this file.
// BirthDate ("YYYY-MM-DD") gives an exact age; BirthYear is the older, coarser
// field and is filled from BirthDate when that is set.
type Profile struct {
	BirthDate string  `json:"birth_date,omitempty"`
	BirthYear int     `json:"birth_year,omitempty"`
	Sex       string  `json:"sex,omitempty"` // "male" | "female"
	WeightKg  float64 `json:"weight_kg,omitempty"`
}

// BirthTime parses BirthDate; ok is false when it is unset or malformed.
func (p Profile) BirthTime() (time.Time, bool) {
	t, err := time.Parse("2006-01-02", p.BirthDate)
	return t, err == nil
}

// SetProfile stores the profile.
func (d *DB) SetProfile(p Profile) error {
	v, err := json.Marshal(p)
	if err != nil {
		return err
	}
	// Summaries depend on the profile (max heart rate comes from age), so drop them all.
	err = d.update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(bDays); err != nil {
			return err
		}
		if _, err := tx.CreateBucket(bDays); err != nil {
			return err
		}
		return tx.Bucket(bMeta).Put([]byte("profile"), v)
	})
	if err == nil {
		d.writeProfileBackup(p)
	}
	return err
}

// MergeProfile changes only the fields set in update and keeps the rest, so
// saving a sex never erases a date of birth. It returns the merged profile.
func (d *DB) MergeProfile(update Profile) (Profile, error) {
	cur, err := d.Profile()
	if err != nil {
		return cur, err
	}
	if update.BirthDate != "" {
		cur.BirthDate = update.BirthDate
	}
	if update.BirthYear != 0 {
		cur.BirthYear = update.BirthYear
	}
	if update.Sex != "" {
		cur.Sex = update.Sex
	}
	if update.WeightKg != 0 {
		cur.WeightKg = update.WeightKg
	}
	return cur, d.SetProfile(cur)
}

// Profile returns the stored profile (zero value if none).
func (d *DB) Profile() (p Profile, err error) {
	err = d.b.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket(bMeta).Get([]byte("profile")); v != nil {
			return json.Unmarshal(v, &p)
		}
		return nil
	})
	return
}

// PutDay caches a derived summary for one day. It is a cache: delete it and
// it is recomputed from the samples.
func (d *DB) PutDay(day string, summary any) error {
	v, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	return d.update(func(tx *bolt.Tx) error { return tx.Bucket(bDays).Put([]byte(day), v) })
}

// Day summary lookup: ok is false when the day isn't cached.
func (d *DB) GetDay(day string, into any) (ok bool, err error) {
	err = d.b.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket(bDays).Get([]byte(day)); v != nil {
			ok = true
			return json.Unmarshal(v, into)
		}
		return nil
	})
	return
}

// DaysWithData lists, in order, the local days that hold at least minSamples
// samples. It walks the heart-rate bucket once.
func (d *DB) DaysWithData(minSamples int) ([]string, error) {
	counts := map[string]int{}
	var order []string
	err := d.b.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bHR).Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			day := d.Day(time.UnixMilli(int64(binary.BigEndian.Uint64(k))))
			if counts[day] == 0 {
				order = append(order, day)
			}
			counts[day]++
		}
		return nil
	})
	var out []string
	for _, day := range order {
		if counts[day] >= minSamples {
			out = append(out, day)
		}
	}
	return out, err
}

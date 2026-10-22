package ingest

import (
	"encoding/binary"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

// RepairReport counts what RepairHistory saw and changed.
type RepairReport struct {
	Records int // raw history records stored
	Heart   int // records with a verified heart-rate layout, samples rebuilt from them
	Skipped int // dated records without one: kept raw, no sample
	Undated int // records stamped on the strap's unset clock: left as they were
	Removed int // stored History samples deleted at the skipped records' seconds
}

// RepairHistory rebuilds the History-source samples from the raw records, which
// are kept verbatim and are the source of truth. Before HasHeartLayout existed,
// every 0x2F record was read as heart rate, so the strap's other record kinds
// (sensor frames, ~40 per 20 minutes) wrote small numbers over real samples at
// their second. This deletes the History sample at every second a non-heart
// record claims, then rewrites the samples from the records that do have a
// verified layout (so a second shared with a real record gets its real value
// back). Realtime samples and records on the unset clock are not touched, and
// running it again changes nothing.
func RepairHistory(db *strapdb.DB) (RepairReport, error) {
	var rep RepairReport
	var remove []time.Time
	var add []strapdb.Sample
	err := db.Records(func(r strapdb.Record) bool {
		b := r.Body
		if len(b) < 11 || b[0] != whoopble.TypeHistory {
			return true
		}
		rep.Records++
		ts := binary.LittleEndian.Uint32(b[7:11])
		if ts < minValidUnix {
			rep.Undated++
			return true
		}
		smp, _, ok := whoopble.DecodeHistory(b)
		if !ok || smp.HR == 0 {
			rep.Skipped++
			remove = append(remove, time.Unix(int64(ts), 0))
			return true
		}
		rr := make([]uint16, len(smp.RR))
		for i, v := range smp.RR {
			rr[i] = uint16(v)
		}
		add = append(add, strapdb.Sample{Time: smp.Time, HR: uint8(smp.HR), RR: rr, Source: strapdb.History})
		rep.Heart++
		return true
	})
	if err != nil {
		return rep, err
	}
	rep.Removed, err = db.ReplaceHistorySamples(remove, add)
	return rep, err
}

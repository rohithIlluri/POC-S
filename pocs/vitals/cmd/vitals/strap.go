package main

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/ingest"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

// cmdStrapRepair rebuilds the stored history samples from the raw records. Stop
// `vitals serve` first: the database file can only be open once.
func (a *app) cmdStrapRepair() error {
	db, err := strapdb.Open(a.home, a.loc)
	if err != nil {
		return err
	}
	defer db.Close()
	rep, err := ingest.RepairHistory(db)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%d history records: %d heart-rate samples rebuilt, %d other kinds kept raw only, %d on the unset clock left alone, %d wrong samples removed\n",
		rep.Records, rep.Heart, rep.Skipped, rep.Undated, rep.Removed)
	return nil
}

func (a *app) cmdStrap(args []string) error {
	if len(args) == 1 && args[0] == "repair" {
		return a.cmdStrapRepair()
	}
	if len(args) != 2 || args[0] != "decode" {
		return errors.New("usage: vitals strap decode <capture.txt> | vitals strap repair")
	}
	f, err := os.Open(args[1])
	if err != nil {
		return err
	}
	defer f.Close()
	s, err := whoopble.SummarizeCapture(f)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%d notifications/writes, %d valid frames, %d failed checksums\n\n", s.Lines, s.Frames, s.Dropped)
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  channel\ttype\tcount\tfirst body")
	for _, t := range s.Types {
		ex := t.Example
		if len(ex) > 64 {
			ex = ex[:64] + "…"
		}
		fmt.Fprintf(tw, "  %s\t0x%02x\t%d\t%s\n", t.Channel, t.Type, t.Count, ex)
	}
	tw.Flush()
	if rt := s.Realtime; rt.Samples > 0 {
		fmt.Fprintf(a.out, "\nrealtime: %d samples, %d–%d bpm, %d RR intervals, strap clock %s → %s\n",
			rt.Samples, rt.MinHR, rt.MaxHR, rt.RRIntervals,
			time.Unix(rt.FirstUnix, 0).In(a.loc).Format(time.DateTime), time.Unix(rt.LastUnix, 0).In(a.loc).Format(time.DateTime))
	}
	if s.History.Records > 0 {
		fmt.Fprintf(a.out, "history: %d records\n", s.History.Records)
	}
	return nil
}

package ingest

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/science"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

// TestRealNightStages prints the stage estimate for a night in a COPY of a
// real database, with and without the time prior, plus a text hypnogram. Like
// TestRealNight it only runs when VITALS_REAL_DB_DIR and VITALS_REAL_NIGHT are
// set.
func TestRealNightStages(t *testing.T) {
	dir, day := os.Getenv("VITALS_REAL_DB_DIR"), os.Getenv("VITALS_REAL_NIGHT")
	if dir == "" || day == "" {
		t.Skip("set VITALS_REAL_DB_DIR and VITALS_REAL_NIGHT to run against a copy of a real database")
	}
	loc, _ := time.LoadLocation("America/Chicago")
	db, err := strapdb.Open(dir, loc)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	night, err := Sleep(db, day, loc)
	if err != nil || night.Motion == nil {
		t.Fatalf("no night: %v", err)
	}
	d, _ := time.ParseInLocation("2006-01-02", day, loc)
	from, to := d.Add(-12*time.Hour), d.Add(12*time.Hour)
	var ss []science.StageSample
	_ = db.Records(func(r strapdb.Record) bool {
		h, ok := whoopble.DecodeHistoryFull(r.Body)
		if !ok || h.Time.Before(from) || !h.Time.Before(to) {
			return true
		}
		rr := make([]float64, len(h.RR))
		for i, v := range h.RR {
			rr[i] = float64(v)
		}
		ss = append(ss, science.StageSample{T: h.Time, HR: h.HR, RR: rr, G: [3]float64{float64(h.Gravity[0]), float64(h.Gravity[1]), float64(h.Gravity[2])}, Movement: float64(h.Movement), Device: int(h.Sleep)})
		return true
	})
	sortStageSamples(ss)
	for _, prior := range []float64{science.DefaultStageOptions().TimePrior, 0} {
		h, ok := science.StageNight(ss, night.Motion.Window, science.StageOptions{TimePrior: prior})
		if !ok {
			t.Fatalf("not staged")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "\n=== time prior %.2f\n", prior)
		tt := h.Totals
		fmt.Fprintf(&b, "TST %.0f min: light %.0f (%.0f%%) deep %.0f (%.0f%%) REM %.0f (%.0f%%); wake %.0f, nodata %.0f\n", tt.TSTMin, tt.LightMin, tt.LightPct, tt.DeepMin, tt.DeepPct, tt.REMMin, tt.REMPct, tt.WakeMin, tt.NoDataMin)
		fmt.Fprintf(&b, "rest onset %s, sleep onset %s (SOL %.0f), final wake %s, REM latency %.0f, WASO %.0f, cycles %d\n",
			tt.RestOnset.In(loc).Format("15:04"), tt.SleepOnset.In(loc).Format("15:04"), tt.SOLMin, tt.FinalWake.In(loc).Format("15:04"), tt.REMLatencyMin, tt.WASOMin, tt.Cycles)
		fmt.Fprintf(&b, "rr coverage %.2f sufficient %v, mean conf %.2f, low-conf epochs %d, device agreement %.2f kappa %.2f, stage HR %v\n", h.RRCoverage, h.RRSufficient, h.MeanConfidence, h.LowConfidenceEpochs, h.DeviceAgreement, h.DeviceKappa, h.StageHR)
		for _, c := range h.Cycles {
			fmt.Fprintf(&b, "cycle %s-%s %.0f min deep %.0f REM %.0f complete %v\n", c.Start.In(loc).Format("15:04"), c.End.In(loc).Format("15:04"), c.Minutes, c.DeepMinutes, c.REMMinutes, c.Complete)
		}
		for _, c := range h.Checks {
			fmt.Fprintf(&b, "check %-45s pass=%-5v indep=%-5v %s\n", c.Name, c.Pass, c.Independent, c.Detail)
		}
		// text hypnogram: one char per 2.5 min
		sym := map[science.Stage]byte{science.StageWake: 'W', science.StageLight: '-', science.StageDeep: '_', science.StageREM: 'R', science.StageNoData: ' '}
		for i := 0; i < len(h.Epochs); i += 5 {
			if i%120 == 0 {
				fmt.Fprintf(&b, "\n%s ", h.Epochs[i].Start.In(loc).Format("15:04"))
			}
			cnt := map[science.Stage]int{}
			for j := i; j < i+5 && j < len(h.Epochs); j++ {
				cnt[h.Epochs[j].Stage]++
			}
			best, bn := science.StageNoData, -1
			for s, c := range cnt {
				if c > bn || (c == bn && s > best) {
					best, bn = s, c
				}
			}
			b.WriteByte(sym[best])
		}
		b.WriteString("\n")
		if prior > 0 && os.Getenv("VITALS_STAGE_EPOCHS") != "" {
			for _, e := range h.Epochs {
				fmt.Fprintf(&b, "%s %-6s c%.2f hr %5.1f dev %5.1f sd %4.1f rm %5.1f resp %4.1f mv %.4f ang %5.1f cov %.2f dev %d d %5.2f r %5.2f\n", e.Start.In(loc).Format("15:04:05"), e.Stage, e.Confidence, nz(e.HR), nz(e.HRDev), nz(e.HRSD), nz(e.RMSSD), nz(e.Resp), nz(e.Move), nz(e.Angle), e.RRCoverage, e.Device, nz(e.DeepScore), nz(e.REMScore))
			}
		}
		t.Log(b.String())
	}
}

func nz(x float64) float64 {
	if math.IsNaN(x) {
		return -9
	}
	return x
}

func sortStageSamples(s []science.StageSample) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].T.Before(s[j-1].T); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

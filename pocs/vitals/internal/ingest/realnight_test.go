package ingest

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
)

// TestRealNight prints the sleep report for a night in a COPY of a real
// database. It only runs when VITALS_REAL_DB_DIR (a directory holding the copy)
// and VITALS_REAL_NIGHT (YYYY-MM-DD, the morning) are set; it never runs in CI.
func TestRealNight(t *testing.T) {
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
	s, err := Sleep(db, day, loc)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	t.Logf("\n%s", b)
}

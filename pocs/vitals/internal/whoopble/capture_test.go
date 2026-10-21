package whoopble

import (
	"fmt"
	"strings"
	"testing"
)

func TestSummarizeCapture(t *testing.T) {
	// Realtime frames split across notifications, a history record, a
	// corrupted frame and the command we wrote.
	rt := capturedRealtime[1]
	bad := []byte(capturedRealtime[0])
	bad[40] = 'f' // corrupt a body byte
	capture := strings.Join([]string{
		"# vitals WHOOP capture v1",
		"0\twrite\taa0800a823010301aed62bce",
		"950\tdata\t" + rt[:20],
		"951\tdata\t" + rt[20:],
		"1950\tdata\t" + string(bad),
		"2950\tdata\t" + capturedHistory[0],
		"3000\tevents\t" + capturedMetadata,
	}, "\n")
	s, err := SummarizeCapture(strings.NewReader(capture))
	if err != nil {
		t.Fatal(err)
	}
	if s.Lines != 6 || s.Frames != 4 || s.Dropped != 1 {
		t.Fatalf("lines=%d frames=%d dropped=%d", s.Lines, s.Frames, s.Dropped)
	}
	if s.Realtime.Samples != 1 || s.Realtime.MinHR != 0x44 || s.History.Records != 1 {
		t.Fatalf("realtime=%+v history=%+v", s.Realtime, s.History)
	}
	want := []string{"data/28", "data/2f", "events/31", "write/23"}
	for i, ts := range s.Types {
		if got := fmt.Sprintf("%s/%02x", ts.Channel, ts.Type); got != want[i] {
			t.Errorf("type %d = %s, want %s", i, got, want[i])
		}
	}
	if _, err := SummarizeCapture(strings.NewReader("garbage line")); err == nil {
		t.Error("malformed capture accepted")
	}
}

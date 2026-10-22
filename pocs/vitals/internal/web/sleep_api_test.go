package web

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSleepEndpointIsReadOnlyAndSaysItIsAnEstimate(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })

	w := c.do("GET", "/api/sleep?date=2026-10-21", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/sleep = %d %s", w.Code, w.Body)
	}
	var n struct {
		Day     string `json:"day"`
		Records int    `json:"records"`
		Note    string `json:"note"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil {
		t.Fatal(err)
	}
	if n.Day != "2026-10-21" || n.Records != 0 || n.Note == "" {
		t.Errorf("an empty night should say so and still carry the caveat: %+v", n)
	}
	if w := c.do("GET", "/api/sleep?date=garbage", nil, nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad date = %d", w.Code)
	}
	if w := c.do("POST", "/api/sleep", nil, nil); w.Code == http.StatusOK {
		t.Error("the sleep endpoint must not accept writes")
	}
}

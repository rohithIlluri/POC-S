package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
)

// mockServer stands in for api.anthropic.com: every POST /v1/messages
// returns the given line as a one-block text response, counting requests so
// tests can assert the once-per-day cache actually prevents calls.
func mockServer(t *testing.T, line string, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_test", "type": "message", "role": "assistant",
			"model":       "claude-haiku-4-5",
			"content":     []map[string]any{{"type": "text", "text": line}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 40, "output_tokens": 12},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	// The SDK reads several credential env vars; pin them so a developer's
	// real key can never be used (or billed) by a unit test.
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
}

func TestLineGeneratesOnceThenCaches(t *testing.T) {
	isolateHome(t)
	var calls atomic.Int32
	srv := mockServer(t, "the cache is warm and so am i.", &calls)
	opts := []option.RequestOption{option.WithBaseURL(srv.URL)}

	got, ok := Line(context.Background(), "", "playful", false, "cheerful", "", "2026-07-15", opts...)
	if !ok || got != "the cache is warm and so am i." {
		t.Fatalf("first call: got %q ok=%v", got, ok)
	}
	got2, ok2 := Line(context.Background(), "", "playful", false, "cheerful", "", "2026-07-15", opts...)
	if !ok2 || got2 != got {
		t.Fatalf("second call should serve the cached line, got %q ok=%v", got2, ok2)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("expected exactly 1 API call (cache must absorb the second), got %d", n)
	}
}

func TestLineRegeneratesOnMoodChange(t *testing.T) {
	isolateHome(t)
	var calls atomic.Int32
	srv := mockServer(t, "new mood, new line.", &calls)
	opts := []option.RequestOption{option.WithBaseURL(srv.URL)}

	Line(context.Background(), "", "playful", false, "cheerful", "", "2026-07-15", opts...)
	Line(context.Background(), "", "playful", false, "worried", "token_bloat", "2026-07-15", opts...)
	if n := calls.Load(); n != 2 {
		t.Errorf("a mood change should regenerate (2 calls), got %d", n)
	}
}

func TestLineFallsBackOnAPIError(t *testing.T) {
	isolateHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized) // not retried by the SDK
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"nope"}}`))
	}))
	defer srv.Close()

	got, ok := Line(context.Background(), "", "playful", false, "cheerful", "", "2026-07-15", option.WithBaseURL(srv.URL))
	if ok || got != "" {
		t.Fatalf("API error must report ok=false so the caller uses the canned line, got %q ok=%v", got, ok)
	}
	// The failed attempt still counts toward the cap so an offline machine
	// doesn't re-dial on every /aipet.
	c := loadCache()
	if c.CallsToday != 1 {
		t.Errorf("failed attempt should count toward the daily cap, got %d", c.CallsToday)
	}
}

func TestLineDailyCapStopsCalls(t *testing.T) {
	isolateHome(t)
	saveCache(voiceCache{Day: "2026-07-15", CallsToday: maxCallsPerDay})
	var calls atomic.Int32
	srv := mockServer(t, "should never be requested", &calls)

	_, ok := Line(context.Background(), "", "playful", false, "cheerful", "", "2026-07-15", option.WithBaseURL(srv.URL))
	if ok {
		t.Error("cap hit must fall back to canned")
	}
	if calls.Load() != 0 {
		t.Error("cap hit must not touch the network at all")
	}

	// A new day resets the cap.
	_, ok = Line(context.Background(), "", "playful", false, "cheerful", "", "2026-07-16", option.WithBaseURL(srv.URL))
	if !ok || calls.Load() != 1 {
		t.Errorf("new day should reset the cap and generate, ok=%v calls=%d", ok, calls.Load())
	}
}

func TestSanitizeLine(t *testing.T) {
	in := "\"hello\nthere\x1b[31m friend\t\" "
	got := sanitizeLine(in)
	if strings.ContainsAny(got, "\n\t\x1b") {
		t.Errorf("control characters must be stripped, got %q", got)
	}
	if strings.HasPrefix(got, `"`) || strings.HasSuffix(got, `"`) {
		t.Errorf("wrapping quotes must be trimmed, got %q", got)
	}
	long := strings.Repeat("a", 300)
	if len(sanitizeLine(long)) > 140 {
		t.Error("length must be capped at 140")
	}
}

package collector

import (
	"bufio"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/enterprise/aipet/internal/pricing"
	"github.com/enterprise/aipet/internal/store"
)

// Codex writes rollout/session files as JSONL under ~/.codex/sessions. The schema
// has shifted across versions, so we parse defensively: we look for any object
// that carries a token-usage block and a model id, wherever they sit. Fields are
// matched by several known aliases to survive format drift.
type codexLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	TS        string          `json:"ts"`
	Model     string          `json:"model"`
	Payload   json.RawMessage `json:"payload"`
	Info      json.RawMessage `json:"info"`
	Usage     *codexUsage     `json:"usage"`
}

type codexUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	PromptTokens      int64 `json:"prompt_tokens"`      // alt naming
	CompletionTokens  int64 `json:"completion_tokens"`  // alt naming
	CachedInputTokens int64 `json:"cached_input_tokens"`
	CacheReadTokens   int64 `json:"cache_read_input_tokens"`
}

func (u codexUsage) normalize() pricing.Usage {
	in := u.InputTokens
	if in == 0 {
		in = u.PromptTokens
	}
	out := u.OutputTokens
	if out == 0 {
		out = u.CompletionTokens
	}
	cr := u.CacheReadTokens
	if cr == 0 {
		cr = u.CachedInputTokens
	}
	return pricing.Usage{Input: in, Output: out, CacheRead: cr}
}

// CollectCodex scans Codex session files under root for usage-bearing turns.
func CollectCodex(root string, st *store.Store, prices *pricing.Table) (int, error) {
	var added int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr
		}
		if !strings.HasSuffix(path, ".jsonl") && !strings.HasSuffix(path, ".json") {
			return nil
		}
		n, _ := collectCodexFile(path, st, prices)
		added += n
		return nil
	})
	return added, err
}

func collectCodexFile(path string, st *store.Store, prices *pricing.Table) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	session := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	project := projectName(filepath.Base(filepath.Dir(path)))

	var added, idx int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		idx++
		raw := sc.Bytes()
		var l codexLine
		if json.Unmarshal(raw, &l) != nil {
			continue
		}
		usage := extractCodexUsage(l, raw)
		if usage == nil {
			continue
		}
		model := l.Model
		if model == "" {
			model = "codex" // fall back to the generic codex rate
		}
		ts := parseCodexTime(l)
		// No stable per-turn uuid is guaranteed, so derive a deterministic key
		// from file+line content. Idempotent across re-scans of the same file.
		sum := sha1.Sum(raw)
		key := fmt.Sprintf("codex|%s|%d|%x", session, idx, sum[:6])
		if st.Has(key) {
			continue
		}
		e := store.Event{
			Key:       key,
			Source:    "codex",
			Session:   session,
			Project:   project,
			Model:     model,
			Timestamp: ts,
			Input:     usage.Input,
			Output:    usage.Output,
			CacheRead: usage.CacheRead,
			CostUSD:   prices.Cost(model, *usage),
		}
		if ok, _ := st.Append(e); ok {
			added++
		}
	}
	return added, sc.Err()
}

// extractCodexUsage finds a usage block at the top level or nested in payload/info.
func extractCodexUsage(l codexLine, raw []byte) *pricing.Usage {
	if l.Usage != nil {
		u := l.Usage.normalize()
		if u.Input+u.Output+u.CacheRead > 0 {
			return &u
		}
	}
	for _, blob := range []json.RawMessage{l.Payload, l.Info} {
		if len(blob) == 0 {
			continue
		}
		var wrap struct {
			Usage *codexUsage `json:"usage"`
			Model string      `json:"model"`
		}
		if json.Unmarshal(blob, &wrap) == nil && wrap.Usage != nil {
			u := wrap.Usage.normalize()
			if u.Input+u.Output+u.CacheRead > 0 {
				return &u
			}
		}
	}
	return nil
}

func parseCodexTime(l codexLine) time.Time {
	for _, s := range []string{l.Timestamp, l.TS} {
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Now()
}

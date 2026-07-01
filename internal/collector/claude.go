// Package collector reads coding-tool session logs from disk and turns each model
// turn into a normalized store.Event. It calls no network and no LLM: the token
// counts are already written to disk by the tools, so attributing spend costs
// nothing. This is the core of the "watch usage without burning budget" design.
package collector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/enterprise/aipet/internal/pricing"
	"github.com/enterprise/aipet/internal/store"
)

// claudeLine is the subset of a Claude Code JSONL transcript line we need.
type claudeLine struct {
	Type      string    `json:"type"`
	UUID      string    `json:"uuid"`
	SessionID string    `json:"sessionId"`
	Cwd       string    `json:"cwd"`
	Timestamp time.Time `json:"timestamp"`
	Message   struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens             int64 `json:"input_tokens"`
			OutputTokens            int64 `json:"output_tokens"`
			CacheCreationInputToken int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens    int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// CollectClaude scans every *.jsonl under root, appending unseen assistant turns
// to the store. It returns the number of new events recorded. Errors on a single
// file are skipped so one corrupt session never blocks collection.
func CollectClaude(root string, st *store.Store, prices *pricing.Table) (int, error) {
	var added int
	var errs fileErrors
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil //nolint:nilerr // skip unreadable entries, keep walking
		}
		n, ferr := collectClaudeFile(path, st, prices)
		added += n
		errs.add(path, ferr)
		return nil
	})
	if err != nil {
		return added, err
	}
	return added, errs.err()
}

func collectClaudeFile(path string, st *store.Store, prices *pricing.Table) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var added int
	sc := newLineScanner(f)
	for sc.Scan() {
		var l claudeLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		// Only assistant turns carry usage; everything else is metadata.
		if l.Type != "assistant" || l.Message.Model == "" {
			continue
		}
		u := l.Message.Usage
		if u.InputTokens == 0 && u.OutputTokens == 0 &&
			u.CacheReadInputTokens == 0 && u.CacheCreationInputToken == 0 {
			continue
		}
		key := "claude-code|" + l.SessionID + "|" + l.UUID
		if st.Has(key) {
			continue
		}
		usage := pricing.Usage{
			Input:      u.InputTokens,
			Output:     u.OutputTokens,
			CacheWrite: u.CacheCreationInputToken,
			CacheRead:  u.CacheReadInputTokens,
		}
		e := store.Event{
			Key:        key,
			Source:     "claude-code",
			Session:    l.SessionID,
			Project:    projectName(l.Cwd),
			Model:      l.Message.Model,
			Timestamp:  l.Timestamp,
			Input:      usage.Input,
			Output:     usage.Output,
			CacheWrite: usage.CacheWrite,
			CacheRead:  usage.CacheRead,
			CostUSD:    prices.Cost(l.Message.Model, usage),
		}
		if ok, _ := st.Append(e); ok {
			added++
		}
	}
	return added, sc.Err()
}

// projectName shortens a cwd to its base directory for readable grouping.
func projectName(cwd string) string {
	if cwd == "" {
		return "(unknown)"
	}
	return filepath.Base(cwd)
}

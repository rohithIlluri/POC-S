// Package config persists user settings (currently: goals) as config.json in
// the vitals home.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileName is the settings file inside the vitals home.
const FileName = "config.json"

// Goal is a daily target for one metric. Op is ">=" (reach at least) or
// "<=" (stay at or under).
type Goal struct {
	Op     string  `json:"op"`
	Target float64 `json:"target"`
}

// Met reports whether a daily value satisfies the goal.
func (g Goal) Met(v float64) bool {
	if g.Op == "<=" {
		return v <= g.Target
	}
	return v >= g.Target
}

// ParseGoal reads "8000", ">=8000" or "<=75". A bare number takes defaultOp.
func ParseGoal(s, defaultOp string) (Goal, error) {
	s = strings.TrimSpace(s)
	op := defaultOp
	for _, p := range []string{">=", "<="} {
		if strings.HasPrefix(s, p) {
			op, s = p, strings.TrimSpace(s[len(p):])
			break
		}
	}
	if op == "" {
		return Goal{}, errors.New("this metric has no natural direction; prefix the target with >= or <=")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return Goal{}, fmt.Errorf("goal target %q is not a number", s)
	}
	return Goal{Op: op, Target: v}, nil
}

// Config is the whole settings file.
type Config struct {
	Goals map[string]Goal `json:"goals,omitempty"`
}

// Load reads dir/config.json; a missing file yields an empty config.
func Load(dir string) (Config, error) {
	var c Config
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("%s: %w", FileName, err)
	}
	return c, nil
}

// Save writes the config atomically with private permissions.
func Save(dir string, c Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, FileName))
}

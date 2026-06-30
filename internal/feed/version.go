package feed

import (
	"strconv"
	"strings"
)

// Version is the running binary's semantic version, set at build time via
// -ldflags "-X .../feed.Version=1.2.3". Defaults to a dev marker.
var Version = "0.1.0-dev"

// UpdateAvailable reports whether info advertises a version newer than the one
// running. Comparison is a simple semver-ish numeric compare on dotted parts.
func (m *Manifest) UpdateAvailable() (bool, *UpdateInfo) {
	if m.Update == nil || m.Update.LatestVersion == "" {
		return false, nil
	}
	if newer(m.Update.LatestVersion, Version) {
		return true, m.Update
	}
	return false, nil
}

// newer returns true if a > b, comparing dot-separated numeric prefixes and
// ignoring any pre-release suffix (so "0.2.0" > "0.1.0-dev").
func newer(a, b string) bool {
	pa, pb := parts(a), parts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func parts(v string) []int {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	segs := strings.Split(v, ".")
	out := make([]int, 0, len(segs))
	for _, s := range segs {
		n, _ := strconv.Atoi(s)
		out = append(out, n)
	}
	return out
}

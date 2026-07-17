package host

import (
	"os"
	"path/filepath"
	"strings"
)

// aipetPath is the command prefix setup writes into host config for invoking
// this binary — an absolute path, not "aipet". H4 live verification found
// that Claude Code's statusline and hook commands run in an environment
// where the go-install bin dir (~/go/bin) is not necessarily on PATH, so a
// bare "aipet" silently becomes "command not found" inside the host even on
// machines where the user's own shell resolves it fine.
//
// A var (not a const or a lazy func) so tests can pin it to a fixed fake
// path and assert generated content deterministically.
var aipetPath = resolveAipetPath()

func resolveAipetPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "aipet" // degrade to PATH lookup; better than writing nothing
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe
}

// isOurCommand reports whether a command string invokes this program with
// exactly the given arguments — regardless of which path the binary lived at
// when the entry was written (a bare "aipet" from an early install, or any
// absolute path from a later one). Matching by base name + exact argument
// suffix is what keeps idempotence and --remove working after the binary
// moves between installs.
//
// The split on the first space assumes the binary path itself contains none —
// true for every go-install layout; a space-containing GOBIN would also need
// shell quoting we don't emit, so it's out of scope by construction.
func isOurCommand(cmd, args string) bool {
	bin, rest, found := strings.Cut(cmd, " ")
	if !found || rest != args {
		return false
	}
	base := strings.TrimSuffix(filepath.Base(bin), ".exe")
	return base == "aipet"
}

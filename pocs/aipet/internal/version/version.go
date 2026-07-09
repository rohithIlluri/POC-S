// Package version holds the binary's semantic version, kept in its own tiny
// package so any command or screen can stamp itself without importing feature
// code.
package version

// Version is set at build time via
// -ldflags "-X github.com/enterprise/aipet/internal/version.Version=1.2.3".
var Version = "1.0.0"

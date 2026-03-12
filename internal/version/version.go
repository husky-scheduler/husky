// Package version holds build-time version information for all Husky binaries.
// The values are injected at compile time via -ldflags; the fallback values
// are used during local `go run` invocations.
package version

var (
	// Version is the semantic version string (e.g. "1.0.0").
	Version = "dev"

	// Commit is the short Git SHA of the build.
	Commit = "none"

	// BuildDate is the RFC 3339 UTC timestamp of the build.
	BuildDate = "unknown"
)

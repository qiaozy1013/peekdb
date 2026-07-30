// Package version exposes build-time version information for peekdb.
//
// Version is set at compile time via -ldflags by GoReleaser, e.g.:
//
//	-X github.com/qiaozy1013/peekdb/internal/version.Version=v1.0.0
//	-X github.com/qiaozy1013/peekdb/internal/version.Commit=abc1234
//	-X github.com/qiaozy1013/peekdb/internal/version.BuildTime=2026-07-27T00:00:00Z
package version

// These variables are set via -ldflags at build time. See .goreleaser.yml.
var (
	// Version is the semantic version, e.g. "v1.0.0" or "dev" for local builds.
	Version = "dev"

	// Commit is the git commit hash this binary was built from.
	Commit = "unknown"

	// BuildTime is the RFC3339 timestamp of the build.
	BuildTime = "unknown"

	// BuildTags lists the build tags enabled, e.g. "write" for the peekdb-mut binary.
	BuildTags = ""
)

//go:build !write

// Package main is the peekdb CLI entrypoint. It is a thin
// wrapper around the cobra command tree in root.go and
// the subcommand packages. All real behavior lives in
// those subpackages so the root stays small and easy to
// audit (security review only needs to read ~50 lines).
//
// The default build is strictly read-only — no write APIs
// are linked. The `!write` build constraint selects this
// file; the `//go:build write` sibling `write.go` is used
// for the future v2.0 `peekdb-mut` binary. See write.go
// for the build-tag rationale and security.md § 2.1 for
// the three-layer read-only guarantee.
package main

import (
	"os"
)

func main() {
	os.Exit(Execute())
}

//go:build write

// write.go is the entry point for the write-capable
// `peekdb-mut` binary, selected by the `write` build
// tag. The default build (without `-tags=write`)
// compiles main.go instead, which is guarded by
// `//go:build !write`.
//
// As of v1.0 there are no actual write subcommands.
// This file exists so the build-tag mechanism is
// *real* (not just documented intent) and so a
// v2.0 contributor can add `edit` / `write` / `drop`
// handlers here behind the same `forbiddenArgNames`
// deny-list that protects the default read-only
// build — see root.go for that list and security.md
// § 2.1 for the three-layer read-only guarantee.
//
// The visible effect today is: a write-capable
// binary prints a loud stderr banner so the user
// can never forget they're running a non-readonly
// tool. The `version` subcommand also reports
// "write-capable build (write)" instead of
// "read-only build" (see version.go).
package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "🔴  WRITE-CAPABLE BUILD  🔴")
	_, _ = fmt.Fprintln(os.Stderr, "    v1.0 ships no write subcommands yet;")
	_, _ = fmt.Fprintln(os.Stderr, "    this binary is functionally a read-only peekdb.")
	_, _ = fmt.Fprintln(os.Stderr, "    Real write subcommands land in v2.0.")
	os.Exit(Execute())
}

// Build ignore: this script is run via `go run`, not compiled into peekdb.
//go:build ignore

// gen-leveldb-fixture regenerates testdata/leveldb/empty/ with a few
// keys. Run with: `go run scripts/gen-leveldb-fixture/main.go` from
// the repo root.
//
// The original 32-byte "empty" fixture is right at goleveldb's
// internal block boundary: a single record in a single 32-byte log
// block. On the Linux CI runners with `-race`, goleveldb's read
// state machine walks into a code path that treats this as an empty
// db, so `Items()` returns nothing. A slightly larger fixture (5
// keys across at least 2 log records) avoids the boundary case.
package main

import (
	"fmt"
	"os"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	const path = "testdata/leveldb/empty"
	// Wipe the existing fixture so we don't accumulate leftover
	// stale files (CURRENT, LOCK, etc.) from a previous run.
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("clean %s: %w", path, err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}

	db, err := leveldb.OpenFile(path, &opt.Options{ReadOnly: false})
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	// 5 keys with no "/" so the leveldb inspector groups them by
	// first byte (which is what the test already exercises).
	pairs := [][2]string{
		{"alpha", "1"},
		{"bravo", "2"},
		{"charlie", "3"},
		{"delta", "4"},
		{"echo", "5"},
	}
	for _, kv := range pairs {
		if err := db.Put([]byte(kv[0]), []byte(kv[1]), nil); err != nil {
			_ = db.Close()
			return fmt.Errorf("put %q: %w", kv[0], err)
		}
	}
	return db.Close()
}

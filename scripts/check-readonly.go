// Build-ignore: this script is run via `go run`, not compiled into peekdb.
//go:build ignore

// check-readonly.go — verifies the read-only invariant of the peekdb binary.
//
// Usage:
//
//	go run scripts/check-readonly.go /path/to/peekdb
//
// Exits 0 if the binary is read-only. Exits 1 otherwise, with a message on stderr.
//
//	(): the previous version had four
//
// coverage gaps:
//
//	(a) the subcommand probe only checked 2 of the 17 verbs
//	(b) the source-grep regex only covered 3 of the 17 verbs
//	(c) the regex's \b end-anchor let variants through
//	    (e.g. PutBucket, WriteBatch, DeleteBucket)
//	(d) the source scan only covered internal/, not cmd/
//
// This rewrite fixes all four. The 22-verb list is duplicated from
// cmd/peekdb/root.go (we cannot import main packages into a //go:build
// ignore script) — keep them in sync.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/check-readonly.go <path-to-peekdb>")
		os.Exit(2)
	}
	binary := os.Args[1]

	// Resolve to absolute path for clarity in messages.
	abs, err := filepath.Abs(binary)
	if err == nil {
		binary = abs
	}

	failures := 0

	// Check 1: every entry in the write-verb deny-list must be
	// rejected as "unknown command" by the default build.
	//  (a): previously only 'write' and 'edit' were probed;
	// a future contributor who added a 'dump' subcommand to the
	// default build would not be caught.
	for _, verb := range writeVerbs {
		if !checkCommand(binary, verb) {
			failures++
		}
	}

	// Check 2: no write-verb-named method in the source tree.
	//  (b, c, d):
	//   - one alternation regex covers all 22 verbs (was 3 narrow ones)
	//   - drop the trailing \b so variant names like PutBucket,
	//     WriteBatch, DeleteBucket are caught
	//   - scan both internal/ and cmd/ (was internal/ only)
	if !checkNoMethodInSource(writeMethodPattern) {
		failures++
	}

	if failures > 0 {
		fmt.Fprintf(os.Stderr, "\nFAIL: %d read-only invariant check(s) failed\n", failures)
		os.Exit(1)
	}
	fmt.Println("OK: default build is read-only")
}

// writeVerbs is the duplicate of cmd/peekdb/root.go's
// forbiddenArgNames keys. Keep in sync — the runtime deny-list
// and this static check are the two layers of the read-only
// guarantee. We cannot import the main package into a //go:build
// ignore script, so duplication is the simplest mechanism. A
// future cleanup could move this into a shared internal/registry
// package that both consumers import.
var writeVerbs = []string{
	"write", "edit", "delete", "drop", "create",
	"insert", "update", "remove", "modify", "alter",
	"truncate", "import", "export", "dump", "load",
	"backup", "restore",
	"put", "set", "add", "commit", "rollback",
}

// writeMethodPattern matches any Go function whose name starts
// with one of the 22 write verbs. The pattern:
//
//	func .*\b(verb1|verb2|...)\b
//
// has no trailing \b, so it catches variants like PutBucket,
// WriteBatch, DeleteBucket (which a contributor might add as
// "helpers" but which would expose the underlying library's
// write methods).  (c).
var writeMethodPattern = `func .*\b(write|edit|delete|drop|create|insert|update|remove|modify|alter|truncate|import|export|dump|load|backup|restore|put|set|add|commit|rollback)\b`

// checkCommand runs `peekdb <cmd> 2>&1` and returns true if the output contains
// "unknown command" (i.e. the subcommand does not exist).
func checkCommand(binary, cmd string) bool {
	out, err := exec.Command(binary, cmd).CombinedOutput() //nolint:gosec
	output := strings.ToLower(string(out))
	if err == nil {
		fmt.Fprintf(os.Stderr, "FAIL: 'peekdb %s' succeeded (exit 0); the subcommand must not exist in the default build\n", cmd)
		return false
	}
	if !strings.Contains(output, "unknown command") {
		fmt.Fprintf(os.Stderr, "FAIL: 'peekdb %s' failed but output did not mention 'unknown command' (got: %q)\n", cmd, strings.TrimSpace(string(out)))
		return false
	}
	fmt.Printf("  [OK] 'peekdb %s' rejected as unknown command\n", cmd)
	return true
}

// checkNoMethodInSource greps the source tree for a method
// pattern and returns true if no matches are found.
//
// Scans both internal/ and cmd/ — a future
// contributor could regress the read-only invariant by writing
// a Put/Delete/Write helper in cmd/ that takes an Inspector
// and dispatches to a concrete type's write method.
func checkNoMethodInSource(pattern string) bool {
	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARN: could not find project root: %v (skipping source check for %s)\n", err, pattern)
		return true
	}
	re := regexp.MustCompile(pattern)
	dirs := []string{
		filepath.Join(root, "internal"),
		filepath.Join(root, "cmd"),
	}
	anyFail := false
	for _, dir := range dirs {
		matches, err := scanDir(dir, re)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: scanning %s failed: %v\n", dir, err)
			continue
		}
		if len(matches) > 0 {
			fmt.Fprintf(os.Stderr, "FAIL: method matching %q found in %s:\n", pattern, dir)
			for _, m := range matches {
				fmt.Fprintf(os.Stderr, "  %s\n", m)
			}
			anyFail = true
			continue
		}
		fmt.Printf("  [OK] no method matching %q in %s\n", pattern, filepath.Base(dir))
	}
	return !anyFail
}

// scanDir walks a directory and returns lines that match re.
func scanDir(dir string, re *regexp.Regexp) ([]string, error) {
	var matches []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range bytes.Split(data, []byte("\n")) {
			if re.Match(line) {
				rel, _ := filepath.Rel(dir, path)
				matches = append(matches, fmt.Sprintf("%s:%d: %s", rel, i+1, string(line)))
			}
		}
		return nil
	})
	return matches, err
}

// findProjectRoot walks up from the current directory to find go.mod.
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

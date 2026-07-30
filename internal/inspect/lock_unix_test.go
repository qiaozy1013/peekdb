//go:build !windows

package inspect_test

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/qiaozy1013/peekdb/internal/inspect"
)

// writeTempFile is shared with inspect_test.go but is
// duplicated here for clarity; both files build on the
// non-windows path.
func writeTempFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestTryWriteLock_Uncontended(t *testing.T) {
	path := writeTempFile(t)
	got := inspect.TryWriteLock(path)
	if got != inspect.LockNone {
		t.Errorf("TryWriteLock on unlocked file = %q, want %q", got, inspect.LockNone)
	}
}

func TestTryWriteLock_ExclusiveHeld(t *testing.T) {
	path := writeTempFile(t)
	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("syscall.Open: %v", err)
	}
	defer func() { _ = syscall.Close(fd) }()
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		t.Fatalf("acquire test LOCK_EX: %v", err)
	}
	defer func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }()

	got := inspect.TryWriteLock(path)
	if got != inspect.LockExclusive {
		t.Errorf("TryWriteLock on exclusive-held file = %q, want %q",
			got, inspect.LockExclusive)
	}
}

func TestTryWriteLock_SharedHeld(t *testing.T) {
	path := writeTempFile(t)
	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("syscall.Open: %v", err)
	}
	defer func() { _ = syscall.Close(fd) }()
	if err := syscall.Flock(fd, syscall.LOCK_SH); err != nil {
		t.Fatalf("acquire test LOCK_SH: %v", err)
	}
	defer func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }()

	got := inspect.TryWriteLock(path)
	if got != inspect.LockExclusive {
		t.Errorf("TryWriteLock on shared-held file = %q, want %q",
			got, inspect.LockExclusive)
	}
}

func TestTryWriteLock_MissingFile(t *testing.T) {
	got := inspect.TryWriteLock(filepath.Join(t.TempDir(), "nope"))
	if got != inspect.LockUnknown {
		t.Errorf("TryWriteLock on missing file = %q, want %q",
			got, inspect.LockUnknown)
	}
}

func TestTryWriteLock_Directory(t *testing.T) {
	got := inspect.TryWriteLock(t.TempDir())
	if got == inspect.LockExclusive {
		t.Errorf("TryWriteLock on directory = %q, want LockNone or LockUnknown",
			got)
	}
}

func TestTryWriteLock_Concurrent(t *testing.T) {
	path := writeTempFile(t)
	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("syscall.Open: %v", err)
	}
	defer func() { _ = syscall.Close(fd) }()
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		t.Fatalf("acquire test LOCK_EX: %v", err)
	}
	defer func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }()

	var wg sync.WaitGroup
	results := make([]inspect.LockState, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = inspect.TryWriteLock(path)
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r != inspect.LockExclusive {
			t.Errorf("goroutine %d: TryWriteLock = %q, want LockExclusive", i, r)
		}
	}
}

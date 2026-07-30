//go:build windows

package inspect_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/qiaozy1013/peekdb/internal/inspect"
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
)

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
	h, err := openForTest(path)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(h, lockfileFailImmediately|lockfileExclusiveLock, 0, 1, 0, &overlapped); err != nil {
		t.Fatalf("acquire test LOCK_EX: %v", err)
	}
	defer func() { _ = windows.UnlockFileEx(h, 0, 1, 0, &overlapped) }()

	got := inspect.TryWriteLock(path)
	if got != inspect.LockExclusive {
		t.Errorf("TryWriteLock on exclusive-held file = %q, want %q",
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
	h, err := openForTest(path)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(h, lockfileFailImmediately|lockfileExclusiveLock, 0, 1, 0, &overlapped); err != nil {
		t.Fatalf("acquire test LOCK_EX: %v", err)
	}
	defer func() { _ = windows.UnlockFileEx(h, 0, 1, 0, &overlapped) }()

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

func openForTest(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(
		p,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
}

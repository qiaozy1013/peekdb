package inspect_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qiaozy1013/peekdb/internal/inspect"
)

func TestCheckWAL_NoSidecars(t *testing.T) {
	// Plain file, no -wal or -shm.
	path := filepath.Join(t.TempDir(), "plain.db")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	info := inspect.CheckWAL(path)
	if info.State != inspect.WALNone {
		t.Errorf("State = %v, want WALNone", info.State)
	}
	if info.WALSize != 0 || info.SHMSize != 0 {
		t.Errorf("sizes = (%d, %d), want (0, 0)", info.WALSize, info.SHMSize)
	}
}

func TestCheckWAL_WALOnly(t *testing.T) {
	// -wal present, -shm absent: still WAL-active.
	path := filepath.Join(t.TempDir(), "wal.db")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+"-wal", []byte("wal-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	info := inspect.CheckWAL(path)
	if info.State != inspect.WALActive {
		t.Errorf("State = %v, want WALActive", info.State)
	}
	if info.WALSize == 0 {
		t.Errorf("WALSize = 0, want > 0")
	}
	if info.SHMSize != 0 {
		t.Errorf("SHMSize = %d, want 0", info.SHMSize)
	}
}

func TestCheckWAL_FullMode(t *testing.T) {
	// -wal + -shm both present.
	path := filepath.Join(t.TempDir(), "full.db")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+"-wal", make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+"-shm", make([]byte, 32*1024), 0o600); err != nil {
		t.Fatal(err)
	}
	info := inspect.CheckWAL(path)
	if info.State != inspect.WALActive {
		t.Errorf("State = %v, want WALActive", info.State)
	}
	if info.WALSize != 4096 {
		t.Errorf("WALSize = %d, want 4096", info.WALSize)
	}
	if info.SHMSize != 32*1024 {
		t.Errorf("SHMSize = %d, want %d", info.SHMSize, 32*1024)
	}
}

func TestCheckWAL_MissingPath(t *testing.T) {
	// Main db does not exist: CheckWAL cannot stat sidecars
	// either. State stays WALUnknown.
	info := inspect.CheckWAL(filepath.Join(t.TempDir(), "nope.db"))
	if info.State != inspect.WALUnknown {
		t.Errorf("State = %v, want WALUnknown", info.State)
	}
}

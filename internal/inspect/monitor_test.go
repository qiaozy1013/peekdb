package inspect_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qiaozy1013/peekdb/internal/inspect"
)

// monitorTestInterval is short enough to keep tests fast
// without being so short that the timer misses CPU
// contention on slow CI machines.
const monitorTestInterval = 20 * time.Millisecond

// monitorTestTimeout caps how long a test waits for an
// event. Must be much larger than the interval to avoid
// timing flakes on busy runners.
const monitorTestTimeout = 2 * time.Second

func TestMonitor_NoEventOnIdle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("init"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := inspect.NewMonitor(path, monitorTestInterval)
	defer m.Stop()

	// Within monitorTestTimeout, the file is not touched.
	// We expect zero events.
	select {
	case ev := <-m.Events:
		t.Errorf("got event %v on idle file; want none", ev)
	case <-time.After(monitorTestTimeout):
		// expected: no event.
	}
}

func TestMonitor_EventOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := inspect.NewMonitor(path, monitorTestInterval)
	defer m.Stop()

	// Wait for at least one tick to pass so the monitor
	// has settled its initial state.
	time.Sleep(2 * monitorTestInterval)

	// Modify the file. Many filesystems update mtime with
	// 1-second granularity, so we also bump the size to
	// make the test robust on those.
	if err := os.WriteFile(path, []byte("v1-changed"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case <-m.Events:
		// expected: event fires after a tick.
	case <-time.After(monitorTestTimeout):
		t.Errorf("no event after file change within %v", monitorTestTimeout)
	}
}

func TestMonitor_MultipleChangesCoalesce(t *testing.T) {
	// Events channel is buffered size 1: rapid changes
	// collapse into at most one pending event.
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := inspect.NewMonitor(path, monitorTestInterval)
	defer m.Stop()

	time.Sleep(2 * monitorTestInterval)

	// Five rapid changes within a single tick.
	for range 5 {
		if err := os.WriteFile(path, []byte("v1-aaaaaa"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-m.Events:
		// expected: at least one event.
	case <-time.After(monitorTestTimeout):
		t.Errorf("no event after rapid changes within %v", monitorTestTimeout)
	}
	// Drain anything that might still be pending. We do
	// not assert the queue is empty because timing is
	// non-deterministic across the two ticks.
	for {
		select {
		case <-m.Events:
		case <-time.After(2 * monitorTestInterval):
			return
		}
	}
}

func TestMonitor_StopIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("init"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := inspect.NewMonitor(path, monitorTestInterval)
	m.Stop()
	m.Stop() // must not panic
}

func TestMonitor_StopUnblocksGoroutine(t *testing.T) {
	// After Stop, the monitor goroutine must exit; the
	// simplest signal is that a long sleep on Events does
	// not leak a goroutine. We do not have a direct goroutine
	// count API, so we settle for: Stop returns within
	// monitorTestTimeout (the goroutine has unwound).
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("init"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := inspect.NewMonitor(path, monitorTestInterval)
	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()
	select {
	case <-done:
		// expected
	case <-time.After(monitorTestTimeout):
		t.Errorf("Stop did not return within %v", monitorTestTimeout)
	}
}

func TestMonitor_MissingFileStillFires(t *testing.T) {
	// A file that does not exist on construction: the
	// initial Stat fails, lastMTime stays zero. If the
	// file later appears, the first tick sees a change
	// and fires.
	path := filepath.Join(t.TempDir(), "later.txt")
	m := inspect.NewMonitor(path, monitorTestInterval)
	defer m.Stop()

	time.Sleep(2 * monitorTestInterval)

	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case <-m.Events:
		// expected
	case <-time.After(monitorTestTimeout):
		t.Errorf("no event when missing file appears within %v", monitorTestTimeout)
	}
}

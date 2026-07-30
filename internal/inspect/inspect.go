// Package inspect detects how a database file is being used by other
// processes: file locks, mtime changes, and SQLite-specific WAL activity.
//
// The package is read-only: it probes locks and reads metadata, but
// never opens the file in write mode.
//
// See docs/architecture.md § 4.3 and docs/security.md § 4 for the design.
package inspect

import (
	"os"
	"time"
)

// LockState describes who (if anyone) holds an exclusive lock on a file.
type LockState string

const (
	// LockNone means no other process was detected holding an exclusive lock.
	LockNone LockState = "none"

	// LockShared means at least one other process holds a shared (read) lock.
	LockShared LockState = "shared"

	// LockExclusive means at least one other process holds an exclusive (write) lock.
	LockExclusive LockState = "exclusive"

	// LockUnknown means the lock state could not be determined
	// (e.g. the format does not use advisory locks).
	LockUnknown LockState = "unknown"
)

// Status is a snapshot of file-usage information.
type Status struct {
	// Size is the file size in bytes.
	Size int64

	// MTime is the file's last-modified time.
	MTime time.Time

	// Lock is the detected lock state.
	Lock LockState
}

// TryWriteLock is implemented per-platform:
//
//   - lock_unix.go  (Linux, macOS, BSD)  — syscall.Flock with LOCK_EX|LOCK_NB
//   - lock_windows.go (Windows)          — windows.LockFileEx
//
// Returns LockNone if no exclusive lock is held, LockExclusive if one is,
// and LockUnknown if detection failed for any reason (unsupported FS, etc.).
//
// Important: the probe uses a fresh file handle, so it does not see locks
// held by the calling process. Callers that have already opened the file
// should invoke TryWriteLock BEFORE opening it.

// Monitor polls a file's mtime and size and emits change events on its
// Events channel. Used to detect concurrent writes by other processes.
//
// The zero value is not usable; construct with NewMonitor.
type Monitor struct {
	// Events delivers change notifications. Buffered size 1; the
	// most recent unconsumed event is kept and the next overwrites
	// it. Consumers should treat each event as "the file may have
	// changed since the last poll" and re-Stat to learn what.
	Events chan struct{}

	path      string
	interval  time.Duration
	lastMTime time.Time
	lastSize  int64

	stop chan struct{}
	done chan struct{}
}

// NewMonitor starts monitoring the file at path. The initial
// mtime and size are captured during construction; only
// subsequent changes trigger Events. The returned *Monitor
// spawns a background goroutine; call Stop to terminate.
//
// interval is the polling interval. The mvp-spec default is
// 1 second; smaller intervals trade CPU for lower latency.
func NewMonitor(path string, interval time.Duration) *Monitor {
	if interval <= 0 {
		interval = time.Second
	}
	m := &Monitor{
		Events:   make(chan struct{}, 1),
		path:     path,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	// Capture initial state so the first tick does not fire.
	if info, err := os.Stat(path); err == nil {
		m.lastMTime = info.ModTime()
		m.lastSize = info.Size()
	}
	go m.run()
	return m
}

// run is the monitor goroutine. It polls on every tick and
// exits when stop is closed, signaling done so Stop can
// return only after the goroutine has fully unwound.
func (m *Monitor) run() {
	defer close(m.done)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.check()
		}
	}
}

// check stats the file and emits an event when mtime or size
// changed since the previous check. Non-blocking send: if the
// consumer is not keeping up, we drop the event (the next
// change will resend).
func (m *Monitor) check() {
	info, err := os.Stat(m.path)
	if err != nil {
		// File disappeared (deleted, renamed, etc.). Treat
		// as a change so the consumer can decide what to do.
		select {
		case m.Events <- struct{}{}:
		default:
		}
		return
	}
	mt, sz := info.ModTime(), info.Size()
	// Use Equal for time.Time (== compares struct values
	// including the monotonic clock field and is not what
	// we want here).
	if mt.Equal(m.lastMTime) && sz == m.lastSize {
		return
	}
	m.lastMTime = mt
	m.lastSize = sz
	select {
	case m.Events <- struct{}{}:
	default:
	}
}

// Stop terminates the monitor and waits for the background
// goroutine to exit. Safe to call multiple times.
func (m *Monitor) Stop() {
	select {
	case <-m.stop:
		// already closed
	default:
		close(m.stop)
	}
	<-m.done
}

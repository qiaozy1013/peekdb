//go:build !windows

// Unix implementation of TryWriteLock. Uses syscall.Flock
// with LOCK_EX|LOCK_NB. On success, releases the lock
// immediately so we do not hold a lock any longer than
// necessary.
//
// We intentionally use LOCK_EX rather than LOCK_SH: the
// probe is "is anyone writing to this file?" and a SH
// lock is allowed to coexist with another SH, so it
// cannot answer that question. LOCK_EX is blocked by
// any other holder, so success means "no one is
// writing" and failure means "someone has it".
//
// Note: this probe uses a fresh fd, so it sees only
// locks held by *other* file descriptors. It will not
// detect locks held by the calling process on the same
// file. Callers that have already opened the file
// (e.g. via inspector.Open) should call TryWriteLock
// BEFORE opening, or accept that the result will be
// "exclusive" while the local SH lock is held.

package inspect

import (
	"syscall"
)

// TryWriteLock returns LockNone if no other process holds
// an exclusive (write) lock on path. Returns LockExclusive
// if another process holds an exclusive or otherwise
// conflicting lock. Returns LockUnknown on I/O errors that
// are not lock conflicts.
func TryWriteLock(path string) LockState {
	// O_RDONLY: we never intend to write. O_NOFOLLOW would be
	// nice but is not portable to macOS pre-13. The user's
	// file is the one we mean; the OS resolves the path
	// before we get here.
	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		// ENOENT etc.: cannot determine. Return Unknown.
		return LockUnknown
	}
	defer func() { _ = syscall.Close(fd) }()

	// LOCK_NB: return EWOULDBLOCK instead of blocking.
	err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		// Got the lock; release it before returning so we
		// do not hold a write lock any longer than needed.
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		return LockNone
	}
	// EWOULDBLOCK / EAGAIN mean another process holds a
	// conflicting lock (typically EX; could also be SH on
	// BSDs where LOCK_EX semantics differ). On Linux and
	// macOS these are the same Errno value, so listing both
	// as case labels would be a duplicate-case compile error
	// (the build tags here exclude Windows, which is where
	// EWOULDBLOCK and EAGAIN happen to differ). EAGAIN is
	// the POSIX-standard name; use it.
	if errno, ok := err.(syscall.Errno); ok {
		if errno == syscall.EAGAIN {
			return LockExclusive
		}
	}
	// Some other error: we cannot tell. ENOTSUP is common
	// on filesystems (e.g. some FUSE mounts) that do not
	// implement flock; treat that as Unknown.
	return LockUnknown
}

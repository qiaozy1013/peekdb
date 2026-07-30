//go:build windows

// Windows implementation of TryWriteLock. Uses
// windows.LockFileEx with LOCKFILE_FAIL_IMMEDIATELY and
// LOCKFILE_EXCLUSIVE_LOCK, which together behave like
// POSIX LOCK_EX|LOCK_NB: if any other process holds a
// conflicting lock, the call returns ERROR_LOCK_VIOLATION
// instead of blocking.

package inspect

import (
	"golang.org/x/sys/windows"
)

// LockFileEx flags. From LockFileEx docs in MSDN.
const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
)

// TryWriteLock returns LockNone if no other process holds
// an exclusive (write) lock on path. Returns LockExclusive
// if another process holds a conflicting lock. Returns
// LockUnknown on I/O errors that are not lock conflicts.
func TryWriteLock(path string) LockState {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return LockUnknown
	}
	// Open with FILE_SHARE_READ | FILE_SHARE_WRITE: we are
	// only probing, not actually reading. Sharing both
	// modes means we can open the file even while another
	// process holds its own handle.
	h, err := windows.CreateFile(
		p,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		// : ERROR_SHARING_VIOLATION from
		// CreateFile is the same "another process holds this
		// file exclusively" signal that POSIX flock(2)
		// surfaces as EWOULDBLOCK from LOCK_EX|LOCK_NB. The
		// Unix probe catches it; the Windows probe previously
		// folded it into LockUnknown, so the most common
		// concurrent-writer scenario on Windows (e.g. bbolt in
		// RW mode, another sqlite3.exe session with
		// FILE_SHARE_NONE) showed up as 'unknown' in the
		// status bar instead of 'exclusive'. Map it the same
		// way the LockFileEx branch does.
		if errno, ok := err.(windows.Errno); ok && errno == windows.ERROR_SHARING_VIOLATION {
			return LockExclusive
		}
		return LockUnknown
	}
	defer func() { _ = windows.CloseHandle(h) }()

	// Lock the entire file: 0 high bytes, 1 low byte in the
	// length, offset 0. An Overlapped with all-zero fields
	// is the synchronous-IO form.
	var overlapped windows.Overlapped
	err = windows.LockFileEx(
		h,
		lockfileFailImmediately|lockfileExclusiveLock,
		0, 1, 0,
		&overlapped,
	)
	if err == nil {
		// Got the lock; release before returning.
		_ = windows.UnlockFileEx(h, 0, 1, 0, &overlapped)
		return LockNone
	}
	// ERROR_LOCK_VIOLATION is the expected "someone else
	// holds it" signal. Everything else is Unknown.
	if errno, ok := err.(windows.Errno); ok && errno == windows.ERROR_LOCK_VIOLATION {
		return LockExclusive
	}
	return LockUnknown
}

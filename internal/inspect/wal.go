package inspect

import "os"

// WALState describes the SQLite WAL/SHM auxiliary file
// footprint of a SQLite database.
//
// SQLite's WAL mode writes a separate -wal file alongside
// the main database and (for shared-memory coordination
// across processes) a -shm file. The presence of these
// files is a strong signal that the database is being used
// in WAL mode by some process. peekdb uses this signal to
// populate the "concurrent write" hint in the status bar.
type WALState int

const (
	// WALNone means neither -wal nor -shm is present.
	// The database is in rollback-journal mode (or no
	// other process is using it in WAL mode).
	WALNone WALState = iota

	// WALActive means the -wal file exists. Combined
	// with -shm this is "full WAL mode"; -wal alone
	// usually means "WAL mode, single process".
	WALActive

	// WALUnknown means the state could not be determined
	// (e.g. permission denied reading the directory).
	WALUnknown
)

// WALInfo is a snapshot of the WAL/SHM sidecar files for
// a SQLite database.
type WALInfo struct {
	// State is the overall WAL mode assessment.
	State WALState

	// WALSize is the size of the -wal file in bytes. 0
	// when the file does not exist.
	WALSize int64

	// SHMSize is the size of the -shm file in bytes. 0
	// when the file does not exist.
	SHMSize int64
}

// CheckWAL inspects the -wal and -shm sidecar files for a
// SQLite database at dbPath. dbPath is the main .db file
// (not the -wal or -shm itself). Returns WALInfo{State:
// WALUnknown} when the main file is missing or unreadable
// so the caller can degrade gracefully.
func CheckWAL(dbPath string) WALInfo {
	info := WALInfo{State: WALUnknown}

	// Stat the main db first. If it does not exist, the
	// sidecars are not meaningful either.
	if _, err := os.Stat(dbPath); err != nil {
		return info
	}

	walPath := dbPath + "-wal"
	shmPath := dbPath + "-shm"

	walStat, walErr := os.Stat(walPath)
	shmStat, shmErr := os.Stat(shmPath)

	// Permission errors on either sidecar: state stays
	// Unknown. File-not-exist on either is the normal case.
	if walErr != nil && !os.IsNotExist(walErr) {
		return info
	}
	if shmErr != nil && !os.IsNotExist(shmErr) {
		return info
	}

	hasWAL := walErr == nil
	hasSHM := shmErr == nil

	switch {
	case hasWAL && hasSHM:
		info.State = WALActive
		info.WALSize = walStat.Size()
		info.SHMSize = shmStat.Size()
	case hasWAL:
		// -wal without -shm is unusual but not invalid;
		// it indicates WAL mode with no shared-memory
		// coordination (typically a single writer).
		info.State = WALActive
		info.WALSize = walStat.Size()
	default:
		info.State = WALNone
	}
	return info
}

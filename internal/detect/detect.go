package detect

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	bolt "go.etcd.io/bbolt"
)

// Public errors. Callers can use errors.Is to discriminate.
var (
	// ErrFileNotFound is returned when the path does not exist.
	ErrFileNotFound = errors.New("file not found")

	// ErrIsDirectory is returned when the path is a directory but
	// the caller expected a file.
	ErrIsDirectory = errors.New("path is a directory, not a file")

	// ErrUnsupportedFormat is returned when the file exists but its
	// format cannot be identified.
	ErrUnsupportedFormat = errors.New("unsupported file format")

	// ErrPermissionDenied is returned when the file exists but cannot
	// be read due to permission restrictions.
	ErrPermissionDenied = errors.New("permission denied")
)

// sqliteMagic is the first 16 bytes of every SQLite database file.
// See https://www.sqlite.org/fileformat.html
var sqliteMagic = []byte("SQLite format 3\x00")

// probeTimeout caps how long a probe may block on a file lock. Detect
// must not hang on a file held by another process.
const probeTimeout = 2 * time.Second

// Detect returns the format of the file or directory at path.
//
// Algorithm:
//  1. Stat the path. File-not-found, permission-denied and is-a-dir
//     cases are reported as distinct errors.
//  2. Read the first 16 bytes and compare against strong magic bytes.
//     SQLite has a 16-byte header that uniquely identifies it.
//  3. If the strong magic does not match, try to open the file with
//     bbolt (ReadOnly). On success, return FormatBolt.
//  4. If the path is a directory, try to open it as LevelDB. On
//     success, return FormatLevelDB.
//  5. If all probes fail, return FormatUnknown and ErrUnsupportedFormat.
//
// Detect never writes to the file or directory, and never modifies
// timestamps. All probes use the libraries' ReadOnly modes.
func Detect(path string) (Format, error) {
	if path == "" {
		return FormatUnknown, fmt.Errorf("detect: empty path: %w", ErrUnsupportedFormat)
	}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FormatUnknown, fmt.Errorf("detect %q: %w", path, ErrFileNotFound)
		}
		if isPermissionDenied(err) {
			return FormatUnknown, fmt.Errorf("detect %q: %w", path, ErrPermissionDenied)
		}
		return FormatUnknown, fmt.Errorf("detect %q: stat: %w", path, err)
	}

	// Directories can only be LevelDB. bbolt and SQLite are files.
	if info.IsDir() {
		return detectLevelDB(path)
	}

	// Files: read the magic header. Strong magic wins outright.
	header, err := readHeader(path, magicHeader)
	if err != nil {
		return FormatUnknown, err
	}

	// A zero-byte file is not a database. (bbolt's Open will accept
	// an empty file because the empty page is "valid" by its
	// minimal checks, but that mis-identifies e.g. an empty log
	// file as bbolt. Refuse here.)
	if len(header) == 0 {
		return FormatUnknown, fmt.Errorf("detect %q: %w", path, ErrUnsupportedFormat)
	}

	if hasMagic(header, sqliteMagic) {
		return FormatSQLite, nil
	}

	// No strong magic matched. Probe the file with the candidate
	// libraries (each in ReadOnly mode) until one accepts it.
	if format, ok := probeBolt(path); ok {
		return format, nil
	}

	return FormatUnknown, fmt.Errorf("detect %q: %w", path, ErrUnsupportedFormat)
}

// hasMagic reports whether prefix matches the first len(prefix) bytes
// of header.
func hasMagic(header, prefix []byte) bool {
	if len(header) < len(prefix) {
		return false
	}
	for i, b := range prefix {
		if header[i] != b {
			return false
		}
	}
	return true
}

// readHeader reads the first n bytes of path. It tolerates short reads
// (empty files, partial files) and returns the bytes actually read.
// A short read is not an error here — Detect continues with the
// truncated header so the format probe still gets a chance.
func readHeader(path string, n int) ([]byte, error) {
	f, err := os.Open(path) // #nosec G304 -- path comes from CLI/user input; we never write.
	if err != nil {
		if isPermissionDenied(err) {
			return nil, fmt.Errorf("open %q: %w", path, ErrPermissionDenied)
		}
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	header := make([]byte, n)
	read, err := io.ReadFull(f, header)
	switch {
	case err == nil:
		// full read; nothing to do
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		// short read at EOF — acceptable
	default:
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	return header[:read], nil
}

// isPermissionDenied reports whether err is a permission-related error
// across platforms. We rely on string matching because os.IsPermission
// is not always reliable on Windows.
func isPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if os.IsPermission(err) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "access is denied") ||
		strings.Contains(msg, "Access is denied")
}

// probeBolt tries to open path as a bbolt database in ReadOnly mode.
// It returns (FormatBolt, true) on success and (FormatUnknown, false)
// on any failure (not bbolt, or bbolt but corrupt).
func probeBolt(path string) (Format, bool) {
	// Use recover to defend against panics inside bbolt on malformed
	// input. bbolt is robust but we should not bring down the host
	// TUI/CLI on a single weird file.
	defer func() {
		_ = recover()
	}()

	db, err := bolt.Open(path, 0o600, &bolt.Options{
		ReadOnly: true,
		Timeout:  probeTimeout,
	})
	if err != nil {
		return FormatUnknown, false
	}
	// Confirm we can actually read something. A zero-byte file
	// is technically openable but not a bbolt file.
	if err := db.View(func(tx *bolt.Tx) error {
		_ = tx // existence of the read tx is enough
		return nil
	}); err != nil {
		_ = db.Close()
		return FormatUnknown, false
	}
	_ = db.Close()
	return FormatBolt, true
}

// detectLevelDB tries to open path as a LevelDB directory in ReadOnly
// mode. LevelDB is always a directory, never a single file.
func detectLevelDB(path string) (Format, error) {
	defer func() {
		_ = recover()
	}()

	// Clean the path: LevelDB rejects paths with trailing separators.
	cleaned := filepath.Clean(path)

	db, err := leveldb.OpenFile(cleaned, &opt.Options{
		ReadOnly: true,
	})
	if err != nil {
		return FormatUnknown, fmt.Errorf("detect %q: %w", cleaned, ErrUnsupportedFormat)
	}
	_ = db.Close()
	return FormatLevelDB, nil
}

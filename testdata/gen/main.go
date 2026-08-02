// Note: this file lives under testdata/ which Go tools ignore by default,
// so it does not affect the peekdb build. Run it explicitly via
// `task testdata` or `cd testdata/gen && go run .`.

// gen — generate mock test data for peekdb integration tests.
//
// For formats whose file-format cannot be hand-rolled (bbolt and
// LevelDB) we use the real library in Write mode to create a
// minimal but valid file. For SQLite we use the magic-header
// heuristic (the SQLite magic in the first 16 bytes is enough to
// pass Detect's strong-magic step; the library probe is the
// Inspector's job, not Detect's).
//
// All data is synthetic — never commit real user data.
//
// Run:    cd testdata/gen && go run .
// Verify: task test
package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	bolt "go.etcd.io/bbolt"
)

// testdataRoot is relative to the testdata/gen/ working directory.
const testdataRoot = "../"

// magicHeader is the first 16 bytes of every SQLite database file.
// See https://www.sqlite.org/fileformat.html
var magicHeader = []byte("SQLite format 3\x00")

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
	fmt.Println("Mock data generated under", filepath.Clean(testdataRoot))
}

func run() error {
	dirs := []string{
		"sqlite",
		"bbolt",
		"leveldb/empty",
		"leveldb/chrome-indexeddb",
		"leveldb/with-manifest",
		"negative",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(testdataRoot, d), 0o755); err != nil {
			return err
		}
	}

	// === SQLite mocks ===
	// Magic-only: the first 16 bytes are the SQLite header. Detect's
	// strong-magic step recognises this; subsequent bytes are
	// deliberately non-functional so the library probe is the
	// Inspector's problem.
	sqlite := func(path string) error {
		return writeFileWithMagic(path, magicHeader, 4096)
	}
	if err := sqlite(filepath.Join(testdataRoot, "sqlite/chrome-history.db")); err != nil {
		return err
	}
	if err := copyFile(
		filepath.Join(testdataRoot, "sqlite/chrome-history.db"),
		filepath.Join(testdataRoot, "sqlite/vscode-state.db"),
	); err != nil {
		return err
	}

	// sqlite/empty.db — only the magic, no page body. Still magic-
	// valid; library probe would fail, which is fine.
	if err := writeFileWithMagic(filepath.Join(testdataRoot, "sqlite/empty.db"), magicHeader, 16); err != nil {
		return err
	}

	// sqlite/corrupt.db — magic + random tail. Magic probe still wins.
	if err := writeFileWithRandomTail(
		filepath.Join(testdataRoot, "sqlite/corrupt.db"),
		magicHeader, 4096,
	); err != nil {
		return err
	}

	// === bbolt mocks (via go.etcd.io/bbolt) ===
	if err := writeBolt(filepath.Join(testdataRoot, "bbolt/empty.db"), "test"); err != nil {
		return err
	}
	if err := writeBolt(filepath.Join(testdataRoot, "bbolt/etcd-like.db"), "key"); err != nil {
		return err
	}

	// === LevelDB mocks (via syndtr/goleveldb) ===
	if err := writeLevelDB(filepath.Join(testdataRoot, "leveldb/empty")); err != nil {
		return err
	}
	if err := writeLevelDB(filepath.Join(testdataRoot, "leveldb/chrome-indexeddb")); err != nil {
		return err
	}
	if err := writeLevelDB(filepath.Join(testdataRoot, "leveldb/with-manifest")); err != nil {
		return err
	}

	// === Negative cases (non-database) ===
	pngMagic := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(filepath.Join(testdataRoot, "negative/image.png"), pngMagic, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(
		filepath.Join(testdataRoot, "negative/notes.txt"),
		[]byte("This is just a text file, not a database.\n"),
		0o644,
	); err != nil {
		return err
	}
	// Empty file (zero bytes).
	if err := os.WriteFile(filepath.Join(testdataRoot, "negative/empty"), nil, 0o644); err != nil {
		return err
	}
	// Random bytes — not a database.
	if err := writeRandomFile(filepath.Join(testdataRoot, "negative/random.bin"), 64); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(testdataRoot, "README.md"), []byte(testdataREADME), 0o644)
}

func writeBolt(path, bucket string) error {
	// bbolt.Open refuses to open a file that already exists and is
	// not a bbolt file; the previous gen run may have left a
	// magic-only mock here. Remove it first.
	_ = os.Remove(path)

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return fmt.Errorf("bbolt open %s: %w", path, err)
	}
	defer func() { _ = db.Close() }()

	return db.Update(func(tx *bolt.Tx) error {
		_, cerr := tx.CreateBucketIfNotExists([]byte(bucket))
		return cerr
	})
}

func writeLevelDB(dir string) error {
	// Remove any pre-existing contents so leveldb doesn't refuse to
	// open an already-existing dir.
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	db, err := leveldb.OpenFile(dir, &opt.Options{})
	if err != nil {
		return fmt.Errorf("leveldb open %s: %w", dir, err)
	}
	if err := db.Put([]byte("hello"), []byte("world"), nil); err != nil {
		_ = db.Close()
		return fmt.Errorf("leveldb put: %w", err)
	}
	return db.Close()
}

func writeFileWithMagic(path string, magic []byte, totalSize int) error {
	if totalSize < len(magic) {
		totalSize = len(magic)
	}
	data := make([]byte, totalSize)
	copy(data, magic)
	return os.WriteFile(path, data, 0o644)
}

func writeFileWithRandomTail(path string, magic []byte, totalSize int) error {
	if totalSize < len(magic) {
		totalSize = len(magic)
	}
	data := make([]byte, totalSize)
	copy(data, magic)
	if _, err := rand.Read(data[len(magic):]); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeRandomFile(path string, n int) error {
	data := make([]byte, n)
	if _, err := rand.Read(data); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

const testdataREADME = `# testdata/

This directory contains mock data for peekdb integration tests.

## Layout

- sqlite/           magic-header-only SQLite files (16-byte header + zeros/random)
- bbolt/            valid bbolt files (created by go.etcd.io/bbolt in Write mode)
- leveldb/          valid LevelDB directories (created by goleveldb in Write mode)
- negative/         non-database files (PNG, text, empty, random)

## IMPORTANT

The bbolt and LevelDB files are real (the libraries' own Open functions
will accept them in ReadOnly mode). The SQLite files contain only the
16-byte magic header — Detect's strong-magic step accepts them, but
the library probe will fail (which is fine; the library probe is the
Inspector's job, not Detect's).

Real user data (e.g. a copy of your Chrome History) must NEVER be
committed to this repository. See docs/testing.md for the data privacy
policy.

## Regenerate

` + "```" + `
cd testdata/gen && go run .
` + "```" + `

## Files

### SQLite (magic-only)
- sqlite/empty.db               16-byte header only
- sqlite/chrome-history.db       magic + 4 KB zeros
- sqlite/vscode-state.db         same as chrome-history.db
- sqlite/corrupt.db              magic + random tail

### bbolt (valid)
- bbolt/empty.db                valid bbolt with a 'test' bucket
- bbolt/etcd-like.db             valid bbolt with a 'key' bucket

### LevelDB (valid)
- leveldb/empty/                 minimal LevelDB with one key
- leveldb/chrome-indexeddb/      minimal LevelDB with one key
- leveldb/with-manifest/         minimal LevelDB with one key

### Negative (not a database)
- negative/image.png             PNG header
- negative/notes.txt             plain text
- negative/empty                 zero-byte file
- negative/random.bin            random bytes
`

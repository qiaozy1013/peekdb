package inspector_test

import (
	"fmt"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/qiaozy1013/peekdb/internal/detect"
	"github.com/qiaozy1013/peekdb/internal/inspector"
)

func TestBolt_NewBolt(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("bbolt", "empty.db"))
	insp, err := inspector.NewBolt(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	defer func() { _ = insp.Close() }()

	if got := insp.Format(); got != detect.FormatBolt {
		t.Errorf("Format = %q, want %q", got, detect.FormatBolt)
	}
	if got := insp.Path(); got != path {
		t.Errorf("Path = %q, want %q", got, path)
	}
}

func TestBolt_NewBolt_FileNotFound(t *testing.T) {
	resetRegistry(t)
	_, err := inspector.NewBolt(filepath.Join(t.TempDir(), "nope.db"), inspector.Options{})
	if err == nil {
		t.Errorf("NewBolt on missing file: expected error, got nil")
	}
}

func TestBolt_Open_Dispatches(t *testing.T) {
	// The init() in bbolt.go registered the factory. After
	// resetRegistry we have to re-register it; we do that by
	// reaching into the package via a wrapper factory.
	resetRegistry(t)
	inspector.MustRegister(detect.FormatBolt,
		func(path string, opts inspector.Options) (inspector.Inspector, error) {
			return inspector.NewBolt(path, opts)
		})

	path := findTestdata(t, filepath.Join("bbolt", "empty.db"))
	insp, err := inspector.Open(path, inspector.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = insp.Close() }()

	if got := insp.Format(); got != detect.FormatBolt {
		t.Errorf("Format = %q, want %q", got, detect.FormatBolt)
	}
}

func TestBolt_Items(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("bbolt", "etcd-like.db"))
	insp, err := inspector.NewBolt(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	defer func() { _ = insp.Close() }()

	items, _ := insp.Items()
	// etcd-like.db has a "key" bucket. We expect at least one
	// item.
	if len(items) == 0 {
		t.Fatalf("Items() returned empty slice; expected at least the 'key' bucket")
	}
	// All items should have Kind="bucket".
	for _, it := range items {
		if it.Kind != "bucket" {
			t.Errorf("Item %q: Kind = %q, want %q", it.Name, it.Kind, "bucket")
		}
	}
	// The "key" bucket should be in the list (it's the only
	// top-level bucket the mock generator creates).
	var found bool
	for _, it := range items {
		if it.Name == "key" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Items() missing top-level 'key' bucket; got %v", items)
	}
}

func TestBolt_OpenItem_KV(t *testing.T) {
	resetRegistry(t)
	// Use the empty.db mock; it has a "test" bucket with no
	// entries. We can't test row content here, but we can test
	// the reader lifecycle.
	path := findTestdata(t, filepath.Join("bbolt", "empty.db"))
	insp, err := inspector.NewBolt(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	defer func() { _ = insp.Close() }()

	r, err := insp.OpenItem("test")
	if err != nil {
		t.Fatalf("OpenItem(test): %v", err)
	}
	defer func() { _ = r.Close() }()
	// "test" bucket has no KV entries in the empty mock. We
	// only verify the reader is non-nil and returns no rows
	// without error.
	for r.Next() {
		_ = r.Scan()
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestBolt_OpenItem_NotFound(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("bbolt", "empty.db"))
	insp, err := inspector.NewBolt(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	defer func() { _ = insp.Close() }()

	if _, err := insp.OpenItem("does-not-exist"); err == nil {
		t.Errorf("OpenItem(missing) returned nil error")
	}
}

func TestBolt_OpenItem_EmptyPath(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("bbolt", "empty.db"))
	insp, err := inspector.NewBolt(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	defer func() { _ = insp.Close() }()

	if _, err := insp.OpenItem(""); err == nil {
		t.Errorf("OpenItem(\"\") returned nil error; want error about root")
	}
}

func TestBolt_Stats(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("bbolt", "etcd-like.db"))
	insp, err := inspector.NewBolt(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	defer func() { _ = insp.Close() }()

	stats := insp.Stats()
	if stats.Size == 0 {
		t.Errorf("Stats.Size = 0, want > 0")
	}
	if stats.MTime == 0 {
		t.Errorf("Stats.MTime = 0, want > 0")
	}
	if stats.NumItems == 0 {
		t.Errorf("Stats.NumItems = 0, want > 0 (at least the 'key' bucket)")
	}
	if stats.ReadMode != "readonly" {
		t.Errorf("Stats.ReadMode = %q, want %q", stats.ReadMode, "readonly")
	}
	if stats.FormatVer == "" {
		t.Errorf("Stats.FormatVer empty, want page size info")
	}
}

func TestBolt_Close_Idempotent(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("bbolt", "empty.db"))
	insp, err := inspector.NewBolt(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	if err := insp.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	// bbolt itself is fine to Close twice; we just verify it
	// doesn't panic in our wrapper.
	_ = insp.Close()
}

// TestBolt_OpenItem_ManyNestedBuckets is the regression test for C1
// in . It builds a bbolt DB whose root
// bucket contains 1500 nested sub-buckets AND 1 real KV pair, then
// asserts that OpenItem(root) returns the real KV pair (not an
// empty/short result).
//
// Before the C1 fix, prefetchBoltBucket had a `skipped >= 1024`
// early-exit that silently dropped all real KV pairs in any bucket
// with 1024+ nested sub-buckets. 1500 nested buckets > 1024 = the
// trigger condition; we add a KV pair AFTER the 1024th nested
// bucket so the truncated read missed it.
func TestBolt_OpenItem_ManyNestedBuckets(t *testing.T) {
	resetRegistry(t)
	path := filepath.Join(t.TempDir(), "many-nested.db")

	db, openErr := bolt.Open(path, 0o600, nil)
	if openErr != nil {
		t.Fatalf("bolt.Open: %v", openErr)
	}
	const nested = 1500
	if updateErr := db.Update(func(tx *bolt.Tx) error {
		root, cbErr := tx.CreateBucket([]byte("root"))
		if cbErr != nil {
			return cbErr
		}
		for i := range nested {
			if _, err := root.CreateBucket([]byte(fmt.Sprintf("n%04d", i))); err != nil {
				return err
			}
		}
		// Real KV AFTER the 1024th nested bucket — this is the
		// row the old `skipped >= 1024` cap would silently drop.
		return root.Put([]byte("real-key"), []byte("real-value"))
	}); updateErr != nil {
		_ = db.Close()
		t.Fatalf("setup: %v", updateErr)
	}
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("close: %v", closeErr)
	}

	insp, err := inspector.NewBolt(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	defer func() { _ = insp.Close() }()

	r, err := insp.OpenItem("root")
	if err != nil {
		t.Fatalf("OpenItem(root): %v", err)
	}
	defer func() { _ = r.Close() }()

	foundReal := false
	total := 0
	for r.Next() {
		total++
		row := r.Scan()
		if string(row.Key) == "real-key" {
			foundReal = true
			if string(row.Value) != "real-value" {
				t.Errorf("real-key value = %q, want %q", row.Value, "real-value")
			}
		}
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
	if !foundReal {
		t.Errorf("OpenItem(root) with %d nested buckets dropped the real KV; total rows = %d (C1 fix)", nested, total)
	}
}

// TestBolt_AfterClose is the regression test from
// . After Close, the inspector
// must not panic on Stats / Items / OpenItem. It may return
// zero values or an error — the package's contract is
// "After Close, further method calls are undefined", but a
// TUI goroutine firing Stats() after a Close() must not
// crash the process. Cheap insurance.
func TestBolt_AfterClose(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("bbolt", "empty.db"))
	insp, err := inspector.NewBolt(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	if err := insp.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// All three post-Close calls must not panic. We don't
	// assert specific return values (the contract is
	// "undefined"); we just want to know they don't crash.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("post-Close call panicked: %v", r)
		}
	}()
	_ = insp.Stats()
	_, _ = insp.Items()
	_, _ = insp.OpenItem("test")
}

// TestBolt_OpenItem_SlashInName is the regression test
// from . It documents the
// known v1.0 limitation: a top-level bucket whose name
// contains '/' is *listed* by Items() but cannot be opened
// by OpenItem() (which splits on '/').
//
// This test exists to lock in the behavior so a future
// change to the path-separator handling is a conscious
// decision, not a silent regression. v0.2.0 plans to switch
// the separator to NUL (bbolt cannot store NUL in keys),
// which would make this test obsolete.
func TestBolt_OpenItem_SlashInName(t *testing.T) {
	resetRegistry(t)
	path := filepath.Join(t.TempDir(), "slash-name.db")

	// Create a DB with a top-level bucket whose name contains
	// a slash. bbolt allows this; bbolt itself treats the
	// bytes opaquely.
	db, openErr := bolt.Open(path, 0o600, nil)
	if openErr != nil {
		t.Fatalf("bolt.Open: %v", openErr)
	}
	if cerr := db.Update(func(tx *bolt.Tx) error {
		_, cerr := tx.CreateBucket([]byte("users/active"))
		return cerr
	}); cerr != nil {
		_ = db.Close()
		t.Fatalf("setup: %v", cerr)
	}
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("close: %v", closeErr)
	}

	insp, err := inspector.NewBolt(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	defer func() { _ = insp.Close() }()

	// Items() must list the bucket by its raw name.
	items, err := insp.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	found := false
	for _, it := range items {
		if it.Name == "users/active" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Items() did not list 'users/active' bucket; got %+v", items)
	}

	// OpenItem('users/active') must FAIL — the slash is
	// interpreted as a nested-bucket boundary, so it tries
	// to open bucket 'users' (which doesn't exist) and
	// returns 'bucket not found'. This is the documented
	// v1.0 limitation; the test locks it in.
	_, openErr = insp.OpenItem("users/active")
	if openErr == nil {
		t.Errorf("OpenItem('users/active') succeeded; expected 'bucket not found' error (v1.0 limitation)")
	}
}

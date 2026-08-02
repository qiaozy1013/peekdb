package inspector

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/qiaozy1013/peekdb/internal/detect"
)

// BoltInspector exposes a bbolt database in read-only mode.
//
// Inspection model:
//
//   - Items() returns a flat list of all buckets in the database.
//     Top-level buckets have a bare name; nested buckets use a
//     "/"-separated path (e.g. "users/active"). The Children
//     field on each Item lists the names of immediate sub-buckets.
//   - OpenItem(path) opens one bucket and returns an ItemReader
//     that streams its key/value pairs. The "/"-separated path
//     must match what Items() reported.
//
// Known limitation (): bbolt allows arbitrary bytes
// in bucket names, including '/'. A top-level bucket whose name
// contains '/' is *listed* by Items() (which doesn't split on
// path separators) but *cannot* be opened by OpenItem() (which
// does split, treating the '/' as a nested-bucket boundary).
// This is a v1.0 limitation; the alternative — using NUL or
// some other byte bbolt cannot store as the separator — would
// require a wire-format change. See TestBolt_OpenItem_SlashInName
// for the regression test that locks in this behavior. The
// fix (switching the separator) is tracked for v0.2.0.
//
// BoltInspector never writes. bbolt's ReadOnly flag plus
// Tx.View make this a hard guarantee at the driver level.
//
//	Stats / Items / OpenItem all check the
//
// `closed` flag (set once by Close) and return zero values if
// the inspector is closed. The naive behavior — calling
// bbolt's db.View after db.Close — panics with "database file
// isn't correctly mapped" (verified). A TUI goroutine firing
// Stats() after a Close() in the TUI goroutine would crash
// the whole process. We guard against that here.
type BoltInspector struct {
	db        *bolt.DB
	path      string
	options   Options
	closeOnce sync.Once
	closed    bool
}

// bucketPathSep separates bucket names in nested paths.
const bucketPathSep = "/"

// NewBolt opens a bbolt database for read-only inspection.
//
// Note: bbolt 1.3.10 has a bug where bolt.Open with
// ReadOnly: true on a missing path silently creates a new
// empty file on Windows. We stat the path first and refuse
// if it is absent — this is a defensive check that becomes
// redundant if a future bbolt version fixes the underlying
// behavior. See the D4.1 commit message for the
// reproduction.
func NewBolt(path string, opts Options) (*BoltInspector, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Second
	}
	// bbolt.Open with ReadOnly: true on a missing path will
	// happily create a new (empty, all-zeros) file on some
	// platforms (observed on Windows + bbolt v1.3.10). That
	// violates our read-only guarantee: an empty file would
	// suddenly appear on disk. Stat first and refuse.
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("bbolt: stat %q: %w", path, err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{
		ReadOnly: true,
		Timeout:  opts.Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("bbolt: open %q: %w", path, err)
	}
	return &BoltInspector{db: db, path: path, options: opts}, nil
}

// Close releases the underlying bbolt handle. After Close,
// Stats / Items / OpenItem return zero values rather than
// touching the closed bbolt handle (which would panic).
// Close itself is idempotent.
func (b *BoltInspector) Close() error {
	var err error
	b.closeOnce.Do(func() {
		b.closed = true
		err = b.db.Close()
	})
	return err
}

// Format returns detect.FormatBolt.
func (b *BoltInspector) Format() detect.Format { return detect.FormatBolt }

// Path returns the original file path passed to NewBolt.
func (b *BoltInspector) Path() string { return b.path }

// Stats returns a diagnostic snapshot. LockState is left empty
// until D5 (use-detection) lands — a real value requires flock
// probing that lives in a different package.
func (b *BoltInspector) Stats() Stats {
	if b.closed {
		//  avoid touching the closed bbolt handle (which
		// would panic). os.Stat may still succeed on the
		// file path even after Close (the file is not
		// deleted by bbolt's Close), so we report size/mtime
		// but skip the live db calls.
		info, _ := os.Stat(b.path)
		s := Stats{ReadMode: "readonly (closed)"}
		if info != nil {
			s.Size = info.Size()
			s.MTime = info.ModTime().UnixNano()
		}
		return s
	}
	info, err := os.Stat(b.path)
	var size int64
	var mtime int64
	if err == nil {
		size = info.Size()
		mtime = info.ModTime().UnixNano()
	}

	// Count buckets via a depth-first walk. We do not iterate
	// key/value pairs — for very large dbs that would be slow and
	// the count is a best-effort UI hint anyway.
	var numBuckets int
	silentErr(b.db.View(func(tx *bolt.Tx) error {
		numBuckets = countBuckets(tx)
		return nil
	}), "bbolt:Stats:countBuckets")

	pageSize := 0
	if info := b.db.Info(); info != nil {
		pageSize = info.PageSize
	}

	return Stats{
		Size:      size,
		MTime:     mtime,
		NumItems:  numBuckets,
		FormatVer: fmt.Sprintf("bbolt page=%d", pageSize),
		ReadMode:  "readonly",
	}
}

// Items returns a flat list of all buckets in the database.
// Nested buckets are included with their full path. The order
// is depth-first, parent-before-children.
func (b *BoltInspector) Items() ([]Item, error) {
	if b.closed {
		return nil, nil
	}
	var items []Item
	silentErr(b.db.View(func(tx *bolt.Tx) error {
		items = collectBoltBuckets(tx, nil)
		return nil
	}), "bbolt:Items:collectBuckets")
	return items, nil
}

// OpenItem opens a bucket by its "/" path. The empty path is
// not supported — bbolt's root is not a Bucket object; callers
// should use Items() to enumerate top-level buckets instead.
func (b *BoltInspector) OpenItem(path string) (ItemReader, error) {
	if b.closed {
		return nil, fmt.Errorf("bbolt: inspector is closed")
	}
	if path == "" {
		return nil, fmt.Errorf("bbolt: empty path: use Items() to list top-level buckets")
	}
	parts := strings.Split(path, bucketPathSep)
	// bbolt cursors are bound to the View() transaction; once
	// the View closure returns, the cursor's key/value memory
	// is no longer safe to use. We must prefetch inside the tx
	// and copy the bytes, then close the tx.
	rows, err := b.prefetchBoltBucket(parts, b.options.Limit)
	if err != nil {
		return nil, err
	}
	return newBoltItemReader(rows), nil
}

// prefetchBoltBucket walks the bucket at parts inside a single
// View transaction and returns a slice of Rows with copied
// Key/Value bytes. The transaction is closed before this
// function returns, so the returned Rows own their memory.
//
// Returns ("", nil) if the bucket does not exist; ("empty", nil)
// for a found-but-empty bucket; (rows, nil) otherwise. The
// caller converts the marker strings to a real error.
func (b *BoltInspector) prefetchBoltBucket(parts []string, limit int) ([]Row, error) {
	found := false
	var rows []Row
	err := b.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(parts[0]))
		for _, p := range parts[1:] {
			if bucket == nil {
				return nil
			}
			bucket = bucket.Bucket([]byte(p))
		}
		if bucket == nil {
			return nil
		}
		found = true
		c := bucket.Cursor()
		// First() positions at the first key; nested buckets
		// appear with v == nil and must be skipped.
		//
		// : the previous
		// version had a `skipped >= 1024` early-exit, which
		// silently dropped all real KV pairs in any bucket that
		// contained 1024+ nested sub-buckets. The cap was a
		// safety belt against an "infinite nested bucket loop"
		// we never actually saw; bbolt's cursor iterates a
		// finite inline-bucket layout, so the cap was pure
		// risk for no benefit. Removed.
		k, v := c.First()
		for k != nil {
			if v == nil {
				// nested bucket — skip and continue
				k, v = c.Next()
				continue
			}
			rows = append(rows, Row{
				Key:   append([]byte(nil), k...),
				Value: append([]byte(nil), v...),
			})
			if limit > 0 && len(rows) >= limit {
				break
			}
			k, v = c.Next()
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("bbolt: open %q: %w", strings.Join(parts, bucketPathSep), err)
	}
	if !found {
		return nil, fmt.Errorf("bbolt: bucket %q not found", strings.Join(parts, bucketPathSep))
	}
	return rows, nil
}

// collectBoltBuckets walks the bucket tree depth-first, returning
// a flat slice of Items. prefix accumulates the parent path.
//
// The function works on both *bolt.Tx (for the root level, where
// ForEach yields (name, *Bucket)) and *bolt.Bucket (for nested
// levels, where ForEach yields (key, value)). At the bucket level
// we detect nested buckets by walking the cursor and looking for
// v == nil, which is bbolt's marker for an inline bucket.
func collectBoltBuckets(tx *bolt.Tx, prefix []string) []Item {
	var items []Item
	_ = tx.ForEach(func(name []byte, b *bolt.Bucket) error {
		full := append(append([]string{}, prefix...), string(name))
		itemPath := strings.Join(full, bucketPathSep)

		// Recurse first; we want parent before children and we
		// also want Children to be populated with sub-paths.
		children := collectBucketsInBucket(b, full)
		childNames := make([]string, 0, len(children))
		var subSize int64
		for _, c := range children {
			childNames = append(childNames, c.Name)
			subSize += c.Size
		}

		bst := b.Stats()
		items = append(items, Item{
			Name:     itemPath,
			Kind:     "bucket",
			Size:     int64(bst.LeafInuse+bst.BranchInuse) + subSize,
			Count:    int64(bst.KeyN),
			Children: childNames,
		})
		items = append(items, children...)
		return nil
	})
	return items
}

// collectBucketsInBucket is the *bolt.Bucket version of
// collectBoltBuckets. It walks the cursor to find nested
// buckets (v == nil) and recurses.
func collectBucketsInBucket(parent *bolt.Bucket, prefix []string) []Item {
	var items []Item
	c := parent.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		if v != nil {
			continue // real KV pair, not a nested bucket
		}
		nested := parent.Bucket(k)
		if nested == nil {
			continue
		}
		full := append(append([]string{}, prefix...), string(k))
		itemPath := strings.Join(full, bucketPathSep)

		children := collectBucketsInBucket(nested, full)
		childNames := make([]string, 0, len(children))
		var subSize int64
		for _, c := range children {
			childNames = append(childNames, c.Name)
			subSize += c.Size
		}

		bst := nested.Stats()
		items = append(items, Item{
			Name:     itemPath,
			Kind:     "bucket",
			Size:     int64(bst.LeafInuse+bst.BranchInuse) + subSize,
			Count:    int64(bst.KeyN),
			Children: childNames,
		})
		items = append(items, children...)
	}
	return items
}

// countBuckets returns the total number of buckets in the tree,
// including nested ones. Used by Stats().
func countBuckets(tx *bolt.Tx) int {
	n := 0
	_ = tx.ForEach(func(_ []byte, b *bolt.Bucket) error {
		n++
		n += countBucketsInBucket(b)
		return nil
	})
	return n
}

func countBucketsInBucket(b *bolt.Bucket) int {
	n := 0
	// : the original code called
	// b.ForEach(func(_, _ []byte) error { return nil }) which
	// does nothing — ForEach only yields KV pairs, never nested
	// buckets, so the lambda body was dead. We use Cursor
	// directly, which gives us both leaves and nested buckets
	// (the latter via v == nil).
	c := b.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		_ = v
		if v != nil {
			// Real KV pair, not a nested bucket.
			continue
		}
		// v == nil means the key is a nested bucket.
		if nested := b.Bucket(k); nested != nil {
			n++
			n += countBucketsInBucket(nested)
		}
	}
	return n
}

// boltItemReader iterates a slice of prefetched Rows. bbolt
// cursors are bound to their View transaction, so we copy the
// key/value bytes inside the tx and iterate the in-memory
// slice after the tx is closed.
type boltItemReader struct {
	rows   []Row
	pos    int
	err    error
	closed bool
}

func newBoltItemReader(rows []Row) *boltItemReader {
	return &boltItemReader{rows: rows}
}

// Next advances to the next row.
func (r *boltItemReader) Next() bool {
	if r.closed || r.err != nil {
		return false
	}
	if r.pos >= len(r.rows) {
		return false
	}
	r.pos++
	return true
}

// Scan returns the current row. Only valid after Next() == true.
func (r *boltItemReader) Scan() Row {
	return r.rows[r.pos-1]
}

// Err returns the first error encountered during iteration.
func (r *boltItemReader) Err() error { return r.err }

// Close releases the reader. Idempotent.
func (r *boltItemReader) Close() error {
	r.closed = true
	return nil
}

// init wires NewBolt into the package-level registry. Each
// inspector format file does this once so that Open() can
// dispatch by Format without an import cycle.
func init() {
	Register(detect.FormatBolt, func(path string, opts Options) (Inspector, error) {
		return NewBolt(path, opts)
	})
}

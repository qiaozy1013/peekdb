package inspector

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"

	"github.com/qiaozy1013/peekdb/internal/detect"
)

// LevelDBInspector exposes a LevelDB directory in read-only mode.
//
// Inspection model:
//
//   - LevelDB has no concept of sub-databases; everything is a flat
//     keyspace. peekdb groups keys into "keygroups" using a small
//     heuristic so the TUI can render something other than one
//     giant list.
//   - The heuristic is: keys that share a "/"-separated first
//     segment form a group (e.g. "users/42" and "users/43" are
//     both in the "users" group). Keys with no "/" fall back to
//     grouping by the first byte (printed as a 2-char hex).
//   - Items() returns the discovered groups. Each Item's Count
//     is the number of keys in that group.
//   - OpenItem(name) iterates the group's keys in lexicographic
//     order.
//
// LevelDBInspector never writes. goleveldb's ReadOnly flag plus
// an opt.Options{ReadOnly: true} make this a hard guarantee.
type LevelDBInspector struct {
	db      *leveldb.DB
	path    string
	options Options
	groups  []levelDBGroup // pre-computed by computeGroups
}

type levelDBGroup struct {
	name   string
	prefix *util.Range // key range to iterate; nil only when no keys exist
	count  int64       // number of keys in this group
}

// NewLevelDB opens a LevelDB directory for read-only inspection.
// path must point to an existing directory.
func NewLevelDB(path string, opts Options) (*LevelDBInspector, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Second
	}
	// Stat first so that, on platforms where the leveldb driver
	// might create a fresh dir for an unknown path, we refuse
	// before that happens. (goleveldb's OpenFile returns an
	// error on a missing dir, but we also rely on this for
	// the "is a directory?" check below.)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("leveldb: stat %q: %w", path, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("leveldb: %q is not a directory", path)
	}

	db, err := leveldb.OpenFile(path, &opt.Options{
		ReadOnly: true,
	})
	if err != nil {
		return nil, fmt.Errorf("leveldb: open %q: %w", path, err)
	}
	insp := &LevelDBInspector{db: db, path: path, options: opts}
	groups, err := insp.computeGroups()
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("leveldb: scan %q: %w", path, err)
	}
	insp.groups = groups
	return insp, nil
}

// Close releases the underlying leveldb handle.
func (l *LevelDBInspector) Close() error { return l.db.Close() }

// Format returns detect.FormatLevelDB.
func (l *LevelDBInspector) Format() detect.Format { return detect.FormatLevelDB }

// Path returns the directory path passed to NewLevelDB.
func (l *LevelDBInspector) Path() string { return l.path }

// Stats returns a diagnostic snapshot. NumItems is the number of
// discovered key groups. LockState is left empty until D5.
func (l *LevelDBInspector) Stats() Stats {
	info, err := os.Stat(l.path)
	var size int64
	var mtime int64
	if err == nil {
		// For a directory, sum the sizes of all files inside
		// (best-effort; ignores nested dirs). A real on-disk
		// size for LevelDB is the sum of all *.ldb, *.log,
		// CURRENT, MANIFEST, etc. in the dir.
		size = dirSize(l.path)
		mtime = info.ModTime().UnixNano()
	}
	return Stats{
		Size:      size,
		MTime:     mtime,
		NumItems:  len(l.groups),
		FormatVer: "leveldb",
		ReadMode:  "readonly",
	}
}

// Items returns the discovered key groups as a flat list sorted
// by name. Each Item's Name is the group's name (e.g. "users"
// or "0x00" for byte-prefix groups), Kind is "keygroup", and
// Children is empty (groups do not nest in LevelDB).
func (l *LevelDBInspector) Items() ([]Item, error) {
	out := make([]Item, 0, len(l.groups))
	for _, g := range l.groups {
		out = append(out, Item{
			Name:  g.name,
			Kind:  "keygroup",
			Count: g.count, // populated below in computeGroups
		})
	}
	// Items() consumers expect sorted, stable order.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// OpenItem returns a reader over the keys in the named group.
// The name must match exactly what Items() reported.
func (l *LevelDBInspector) OpenItem(name string) (ItemReader, error) {
	var g *levelDBGroup
	for i := range l.groups {
		if l.groups[i].name == name {
			g = &l.groups[i]
			break
		}
	}
	if g == nil {
		return nil, fmt.Errorf("leveldb: key group %q not found", name)
	}
	var slice *util.Range
	if g.prefix != nil {
		slice = g.prefix
	}
	iter := l.db.NewIterator(slice, nil)
	return newLevelDBItemReader(iter, l.options.Limit), nil
}

// computeGroups scans every key once and groups them by the
// "/"-first-segment heuristic. Returns the groups in name order
// (Items() re-sorts so this is just a stable, deterministic
// internal order).
func (l *LevelDBInspector) computeGroups() ([]levelDBGroup, error) {
	counts := map[string]int{}
	// Iterate all keys. We use a snapshot via NewIterator(nil).
	iter := l.db.NewIterator(nil, nil)
	defer iter.Release()

	for iter.Next() {
		key := append([]byte(nil), iter.Key()...) // copy
		seg, _, found := bytes.Cut(key, []byte("/"))
		if found {
			counts[string(seg)]++
			continue
		}
		// No "/": group by first byte.
		if len(key) == 0 {
			counts["<empty>"]++
			continue
		}
		counts[fmt.Sprintf("0x%02x", key[0])]++
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}

	out := make([]levelDBGroup, 0, len(counts))
	for name, n := range counts {
		g := levelDBGroup{name: name, count: int64(n)}
		switch {
		case strings.HasPrefix(name, "0x") && len(name) == 4:
			// byte-prefix group: name is "0xNN"; the actual
			// prefix is the single byte.
			b, err := hex.DecodeString(name[2:])
			if err == nil && len(b) == 1 {
				g.prefix = util.BytesPrefix(b)
			}
		case name == "<empty>":
			// "<empty>" holds exactly the empty key. The
			// previous implementation set prefix=nil here,
			// which made OpenItem fall through to
			// NewIterator(nil) and stream the ENTIRE
			// database — . The
			// correct range is [Start=empty, Limit=0x00):
			// empty < any non-empty key, and 0x00 is
			// excluded, so this matches only the empty key.
			g.prefix = &util.Range{
				Start: []byte{},
				Limit: []byte{0x00},
			}
		default:
			// "/"-segment group: prefix is "name/".
			g.prefix = util.BytesPrefix([]byte(name + "/"))
		}
		out = append(out, g)
	}

	// If no key had a "/", the heuristic degenerated to
	// first-byte grouping. The groups are still valid; we do
	// not need any special handling here.
	return out, nil
}

// levelDBItemReader iterates a LevelDB key range in key order.
type levelDBItemReader struct {
	iter      iterator.Iterator
	started   bool
	key       []byte
	val       []byte
	err       error
	closed    bool
	remaining int
}

func newLevelDBItemReader(iter iterator.Iterator, limit int) *levelDBItemReader {
	// remaining uses -1 as the "unlimited" sentinel so the
	// common case (limit > 0) can decrement on every Next().
	remaining := limit
	if limit <= 0 {
		remaining = -1
	}
	return &levelDBItemReader{iter: iter, remaining: remaining}
}

// Next advances the iterator. Returns false at end-of-stream or
// on error.
func (r *levelDBItemReader) Next() bool {
	if r.closed || r.err != nil {
		return false
	}
	// remaining < 0 means "unlimited" (Limit <= 0 in Options).
	// remaining == 0 means "limit reached".
	if r.remaining == 0 {
		return false
	}
	if r.remaining > 0 {
		r.remaining--
	}

	defer func() {
		if rec := recover(); rec != nil {
			r.err = fmt.Errorf("leveldb: panic during iteration: %v", rec)
			r.key = nil
		}
	}()

	if !r.started {
		// goleveldb requires First() (or Seek/Last) before Key/Value
		// are valid. NewIterator alone leaves the cursor in an
		// undefined position.
		if !r.iter.First() {
			r.key = nil
			return false
		}
		r.started = true
	} else if !r.iter.Next() {
		r.key = nil
		return false
	}
	r.key = append([]byte(nil), r.iter.Key()...)
	r.val = append([]byte(nil), r.iter.Value()...)
	// C2 fix: return iter.Valid() instead of `r.key != nil`. An
	// empty key is a legitimate row; `append([]byte(nil), []byte{}...)`
	// produces nil, which the old check would treat as "no row".
	// (Before the C2 fix, the <empty> key group was never visited
	// because the buggy nil-prefix code path streamed the whole DB
	// and the user's first row would always be a non-empty key.)
	return r.iter.Valid()
}

// Scan returns the current row. Only valid after Next() == true.
func (r *levelDBItemReader) Scan() Row {
	return Row{Key: r.key, Value: r.val}
}

// Err returns the first error encountered during iteration.
func (r *levelDBItemReader) Err() error {
	return r.err
}

// Close releases the iterator. Idempotent.
func (r *levelDBItemReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.iter != nil {
		r.iter.Release()
	}
	return nil
}

// dirSize returns the on-disk size of a directory by summing
// the sizes of its immediate children. Best-effort: errors
// yield 0. Not recursive (LevelDB's actual files live flat
// in the dir, so a non-recursive sum is usually correct).
func dirSize(path string) int64 {
	entries, err := os.ReadDir(path)
	if err != nil {
		silentErr(err, "leveldb:dirSize:ReadDir")
		return 0
	}
	var total int64
	for _, e := range entries {
		ei, err := e.Info()
		if err != nil {
			silentErr(err, "leveldb:dirSize:entry.Info")
			continue
		}
		if ei.IsDir() {
			continue
		}
		total += ei.Size()
	}
	return total
}

// init wires NewLevelDB into the package-level registry.
func init() {
	Register(detect.FormatLevelDB, func(path string, opts Options) (Inspector, error) {
		return NewLevelDB(path, opts)
	})
}

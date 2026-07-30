package inspector_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/qiaozy1013/peekdb/internal/detect"
	"github.com/qiaozy1013/peekdb/internal/inspector"
)

// fakeInspector is a minimal Inspector implementation used by
// registry tests. It records what it was opened with and exposes
// canned values for Format/Path/Stats.
type fakeInspector struct {
	format detect.Format
	path   string
	opts   inspector.Options
	closed bool
	mu     sync.Mutex
}

func (f *fakeInspector) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeInspector) Format() detect.Format { return f.format }
func (f *fakeInspector) Path() string          { return f.path }
func (f *fakeInspector) Stats() inspector.Stats {
	return inspector.Stats{Size: 1024, MTime: 0, NumItems: 0, ReadMode: "readonly"}
}
func (f *fakeInspector) Items() ([]inspector.Item, error) {
	return nil, nil
}
func (f *fakeInspector) OpenItem(name string) (inspector.ItemReader, error) {
	return nil, nil
}

// makeFakeFactory returns a Factory that records the (path, opts)
// it was called with into the provided pointer.
func makeFakeFactory(format detect.Format, out *struct {
	path string
	opts inspector.Options
}) inspector.Factory {
	return func(p string, o inspector.Options) (inspector.Inspector, error) {
		out.path = p
		out.opts = o
		return &fakeInspector{format: format, path: p, opts: o}, nil
	}
}

// resetRegistry is a per-test helper that wipes the registry and
// restores it on test cleanup. Tests that share the registry MUST
// NOT use t.Parallel() — the global registry is shared state and
// parallel tests would race on Register/Reset. Tests here run
// serially by design.
func resetRegistry(t *testing.T) {
	t.Helper()
	inspector.Reset()
	t.Cleanup(inspector.Reset)
}

func TestRegister_PanicsOnUnknownFormat(t *testing.T) {
	resetRegistry(t)
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Register(unknown) did not panic")
		}
	}()
	inspector.Register(detect.FormatUnknown, func(string, inspector.Options) (inspector.Inspector, error) {
		return nil, nil
	})
}

func TestRegister_PanicsOnNilFactory(t *testing.T) {
	resetRegistry(t)
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Register(_, nil) did not panic")
		}
	}()
	inspector.Register(detect.FormatSQLite, nil)
}

func TestRegister_PanicsOnDuplicate(t *testing.T) {
	resetRegistry(t)
	mk := func(string, inspector.Options) (inspector.Inspector, error) { return nil, nil }
	_ = inspector.MustRegister(detect.FormatSQLite, mk)
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Register on duplicate format did not panic")
		}
	}()
	inspector.Register(detect.FormatSQLite, mk)
}

func TestMustRegister_Errors(t *testing.T) {
	resetRegistry(t)
	t.Run("unknown_format", func(t *testing.T) {
		err := inspector.MustRegister(detect.FormatUnknown, nil)
		if err == nil {
			t.Errorf("expected error for unknown format, got nil")
		}
	})
	t.Run("nil_factory", func(t *testing.T) {
		err := inspector.MustRegister(detect.FormatSQLite, nil)
		if err == nil {
			t.Errorf("expected error for nil factory, got nil")
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		mk := func(string, inspector.Options) (inspector.Inspector, error) { return nil, nil }
		if err := inspector.MustRegister(detect.FormatSQLite, mk); err != nil {
			t.Fatalf("first register: %v", err)
		}
		if err := inspector.MustRegister(detect.FormatSQLite, mk); err == nil {
			t.Errorf("expected error for duplicate, got nil")
		}
	})
}

func TestLookup_AndList(t *testing.T) {
	resetRegistry(t)
	mk := func(string, inspector.Options) (inspector.Inspector, error) { return nil, nil }
	_ = inspector.MustRegister(detect.FormatSQLite, mk)
	_ = inspector.MustRegister(detect.FormatBolt, mk)

	f, ok := inspector.Lookup(detect.FormatSQLite)
	if !ok || f == nil {
		t.Errorf("Lookup(sqlite) ok=%v, factory nil=%v", ok, f == nil)
	}
	if _, ok := inspector.Lookup(detect.FormatLevelDB); ok {
		t.Errorf("Lookup(leveldb) should be false before register")
	}
	got := inspector.ListFormats()
	if len(got) != 2 {
		t.Errorf("ListFormats len=%d, want 2 (got=%v)", len(got), got)
	}
}

func TestUnregister(t *testing.T) {
	resetRegistry(t)
	mk := func(string, inspector.Options) (inspector.Inspector, error) { return nil, nil }
	_ = inspector.MustRegister(detect.FormatSQLite, mk)
	inspector.Unregister(detect.FormatSQLite)
	if _, ok := inspector.Lookup(detect.FormatSQLite); ok {
		t.Errorf("after Unregister, Lookup should be false")
	}
}

func TestOpen_FileNotFound(t *testing.T) {
	resetRegistry(t)
	_, err := inspector.Open(filepath.Join(t.TempDir(), "does-not-exist.db"), inspector.Options{})
	if !errors.Is(err, detect.ErrFileNotFound) {
		t.Errorf("err = %v, want ErrFileNotFound", err)
	}
}

func TestOpen_UnsupportedFormat(t *testing.T) {
	resetRegistry(t)
	// Real text file: detect rejects it. No Factory involved.
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("just a text file"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := inspector.Open(path, inspector.Options{})
	if !errors.Is(err, detect.ErrUnsupportedFormat) {
		t.Errorf("err = %v, want ErrUnsupportedFormat", err)
	}
}

func TestOpen_RecognisedButNoFactory(t *testing.T) {
	resetRegistry(t)
	// Use a real bbolt file (we have it in testdata) so detect
	// returns FormatBolt, but with no Factory registered, Open
	// should report a registry miss.
	boltPath := findTestdata(t, filepath.Join("bbolt", "empty.db"))
	_, err := inspector.Open(boltPath, inspector.Options{})
	if !errors.Is(err, detect.ErrUnsupportedFormat) {
		t.Errorf("err = %v, want ErrUnsupportedFormat", err)
	}
}

func TestOpen_DispatchesToFactory(t *testing.T) {
	resetRegistry(t)

	var captured struct {
		path string
		opts inspector.Options
	}
	_ = inspector.MustRegister(detect.FormatBolt,
		makeFakeFactory(detect.FormatBolt, &captured))

	boltPath := findTestdata(t, filepath.Join("bbolt", "empty.db"))

	insp, err := inspector.Open(boltPath, inspector.Options{CopyOnOpen: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got, want := insp.Format(), detect.FormatBolt; got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
	if got, want := insp.Path(), boltPath; got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
	if captured.path != boltPath {
		t.Errorf("factory path = %q, want %q", captured.path, boltPath)
	}
	if !captured.opts.CopyOnOpen {
		t.Errorf("factory opts.CopyOnOpen = false, want true")
	}
	if !captured.opts.ReadOnly {
		t.Errorf("factory opts.ReadOnly = false, want true (forced by Open)")
	}
	if captured.opts.Timeout <= 0 {
		t.Errorf("factory opts.Timeout = %v, want positive (default applied)", captured.opts.Timeout)
	}
}

func TestOpen_AppliesDefaults(t *testing.T) {
	resetRegistry(t)

	var captured struct {
		path string
		opts inspector.Options
	}
	_ = inspector.MustRegister(detect.FormatBolt,
		makeFakeFactory(detect.FormatBolt, &captured))

	boltPath := findTestdata(t, filepath.Join("bbolt", "empty.db"))
	// Pass an Options{} that explicitly sets ReadOnly=false and
	// Timeout=0. Open should still hand the factory a read-only
	// request with a non-zero timeout.
	insp, err := inspector.Open(boltPath, inspector.Options{ReadOnly: false, Timeout: 0})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = insp
	if !captured.opts.ReadOnly {
		t.Errorf("Open did not force ReadOnly=true")
	}
	if captured.opts.Timeout != 2*time.Second {
		t.Errorf("opts.Timeout = %v, want 2s default", captured.opts.Timeout)
	}
}

func TestOpen_PropagatesFactoryError(t *testing.T) {
	resetRegistry(t)
	sentinel := errors.New("factory boom")
	_ = inspector.MustRegister(detect.FormatBolt,
		func(string, inspector.Options) (inspector.Inspector, error) {
			return nil, sentinel
		})
	boltPath := findTestdata(t, filepath.Join("bbolt", "empty.db"))
	_, err := inspector.Open(boltPath, inspector.Options{})
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want wrap of sentinel", err)
	}
}

func TestOpen_DefaultRegistrationForRealFormats(t *testing.T) {
	// This test guards the boundary between D3 (no real
	// inspectors) and D4 (SQLite/bbolt/LevelDB init() functions
	// register factories). It is intentionally permissive: it
	// only fails if a Factory exists for FormatUnknown, which
	// would indicate a wiring bug.
	resetRegistry(t)
	if _, ok := inspector.Lookup(detect.FormatUnknown); ok {
		t.Errorf("Factory registered for FormatUnknown — should never happen")
	}
}

// findTestdata locates the repo's testdata/ directory relative
// to this test file. Mirrors the helper in internal/detect.
func findTestdata(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("testdata %s not found: %v", abs, err)
	}
	return abs
}

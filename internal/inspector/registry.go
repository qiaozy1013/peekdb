package inspector

import (
	"fmt"
	"sync"
	"time"

	"github.com/qiaozy1013/peekdb/internal/detect"
)

// Options controls how an Inspector is opened. v1 fields are
// conservative — the only knob the user actually controls in v1 is
// CopyOnOpen. ReadOnly is always true in the default build; the
// field exists so that v2 (peekdb-mut) can flip it without changing
// signatures.
//
// Important: Open() applies defaults to opts in place. Specifically,
// ReadOnly is forced to true regardless of caller input (the
// v1 build has no write path) and Timeout is replaced with 2s
// when the caller passed 0 or negative. Callers that need to
// observe the *effective* options should read the struct
// back after Open returns, or use inspector.Stats() which
// reports the ReadMode field derived from the effective
// options.
type Options struct {
	// CopyOnOpen, when true, instructs the inspector to copy the
	// file to a temporary path before opening it. This lets the
	// user keep browsing even when the original is locked or being
	// actively written by another process. Copy-on-Open is a
	// user-driven choice (CLI flag or TUI key), not the default.
	CopyOnOpen bool

	// ReadOnly is the requested access mode. The default build
	// always opens read-only regardless of this field. v2
	// (peekdb-mut) may set it false.
	ReadOnly bool

	// Timeout caps how long an open may block on a file lock.
	// 0 means "use the package default" (2s). Inspectors apply
	// this to their underlying driver options.
	Timeout time.Duration

	// Limit caps how many rows an ItemReader returns in one
	// OpenItem call. 0 means unlimited. v1 may not honor this on
	// every format; the field is here so the TUI can cap
	// rendering without a separate API.
	Limit int
}

// defaultTimeout is the fallback when Options.Timeout is zero. 2s
// is long enough to ride out normal file-lock contention but short
// enough to keep a TUI responsive.
const defaultTimeout = 2 * time.Second

// Factory creates an Inspector for a given file path. It is
// called by Open after detect.Detect has classified the file.
//
// Factories must:
//   - Open the file in read-only mode (v1 hard guarantee).
//   - Return a non-nil Inspector or a non-nil error.
//   - Never panic on malformed input — recover internally and
//     return an error instead.
//
// Factories should NOT:
//   - Touch the filesystem outside the file at path (e.g. do not
//     auto-create a temp copy here; that is the TUI's job via
//     CopyOnOpen).
type Factory func(path string, opts Options) (Inspector, error)

// registry maps a detect.Format to its Factory. Populated by
// Register (typically from each format-specific inspector file's
// init function in D4). The map itself is package-private so that
// the only sanctioned entry point is Register — tests that need
// to inspect or replace the map should use the exported helpers
// (ListFormats, MustRegister, Reset) below.
var (
	registryMu sync.RWMutex
	registry   = map[detect.Format]Factory{}
)

// Register binds a format to a Factory. It panics on duplicate
// registration so that dev-time wiring errors surface immediately
// rather than silently overwriting a previous binding.
//
// In production code, Register is called from each inspector's
// init() in D4. In tests, use Reset() to clear the registry
// between cases.
func Register(format detect.Format, factory Factory) {
	if format == detect.FormatUnknown {
		panic("inspector: cannot register unknown format")
	}
	if factory == nil {
		panic("inspector: cannot register nil factory for format " + string(format))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[format]; exists {
		panic("inspector: format " + string(format) + " already registered")
	}
	registry[format] = factory
}

// MustRegister is like Register but does not panic; instead it
// returns an error. Use this in test code or anywhere panics on
// duplicate registration are undesirable.
func MustRegister(format detect.Format, factory Factory) error {
	if format == detect.FormatUnknown {
		return fmt.Errorf("inspector: cannot register unknown format")
	}
	if factory == nil {
		return fmt.Errorf("inspector: cannot register nil factory for format %q", format)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[format]; exists {
		return fmt.Errorf("inspector: format %q already registered", format)
	}
	registry[format] = factory
	return nil
}

// Unregister removes a Factory from the registry. Primarily a test
// helper; production code does not need this.
func Unregister(format detect.Format) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(registry, format)
}

// Reset clears the registry. Test-only — calling this in
// production would remove all built-in inspectors.
func Reset() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[detect.Format]Factory{}
}

// Lookup returns the Factory bound to a format, if any. The
// boolean is false when no Factory is registered.
func Lookup(format detect.Format) (Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[format]
	return f, ok
}

// ListFormats returns the set of registered formats. The order is
// not specified.
func ListFormats() []detect.Format {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]detect.Format, 0, len(registry))
	for f := range registry {
		out = append(out, f)
	}
	return out
}

// Open detects the format of path and returns the appropriate
// Inspector.
//
// Errors:
//   - detect.ErrFileNotFound / ErrIsDirectory / ErrPermissionDenied
//     are returned unchanged from the underlying call.
//   - detect.ErrUnsupportedFormat is returned as-is when the file
//     does not match any known format.
//   - A new error wrapping ErrUnsupportedFormat is returned when
//     the format is recognized but no Factory is registered (e.g.
//     during D3 development before D4 has wired in the real
//     inspectors).
//
// The returned Inspector is always non-nil on err == nil.
func Open(path string, opts Options) (Inspector, error) {
	format, err := detect.Detect(path)
	if err != nil {
		return nil, err
	}

	registryMu.RLock()
	factory, ok := registry[format]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("inspector: no factory registered for format %q: %w",
			format, detect.ErrUnsupportedFormat)
	}

	// Apply defaults so factories do not have to.
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	// Hard guarantee: v1 never writes. Defensive even if a caller
	// sets opts.ReadOnly = false.
	if !opts.ReadOnly {
		opts.ReadOnly = true
	}

	return factory(path, opts)
}

# Architecture

> How peekdb is organised, why each piece exists, and the three-layer read-only
> defense. No code here — read the source.

## Layout

```
peekdb/
├── cmd/peekdb/          CLI entrypoint (cobra)
├── internal/
│   ├── detect/          format auto-detection (magic bytes + probe)
│   ├── inspector/       per-format read-only inspectors
│   │                    + Registry for v1.1 formats
│   ├── inspect/         file lock + mtime monitor
│   ├── parse/           value decomposition (JSON / string / binary)
│   ├── encode/          character encoding
│   ├── tui/             Bubble Tea UI
│   └── version/         build-time version info
├── scripts/
│   └── check-readonly.go  CI gate: verifies default build has no write APIs
├── testdata/            mock data (never real user data)
└── docs/
    ├── requirements.md
    ├── architecture.md  (this file)
    └── roadmap.md
```

## Module responsibilities

| Package | Job |
|---------|-----|
| `detect` | Read magic bytes, optionally probe to confirm, return a `Format` |
| `inspector` | Open a file read-only, expose items + stats; SQLite/bbolt/LevelDB impls |
| `inspect` | Probe whether the file is locked (cross-platform flock / `LockFileEx`); 1Hz mtime poller; SQLite WAL sidecar detection |
| `parse` | Try JSON, fall back to string, fall back to binary hex-dump |
| `encode` | Decode byte buffers as UTF-8 / UTF-16 LE/BE / GBK / Big5 / Latin1 |
| `tui` | Three-pane browser; vim-style keys; `:`-command mode; mtime hint in status bar |
| `version` | Version / commit / build-time ldflag-injected vars |

The CLI is a thin wrapper: each subcommand is its own file (`inspect.go`, `query.go`,
`version.go`, `help.go`) and is wired into the root command by `root.go`.

## Inspector interface

`Inspector` is intentionally small — six methods: `Close`, `Format`, `Path`, `Stats`,
`Items`, `OpenItem`. Format-specific methods (`SQLite.Query`, `Bolt.ForEachBucket`,
`LevelDB.<...>`) live on the concrete type. Callers reach them through `, ok := insp.(*SQLiteInspector)`
assertions in `cmd/` and `internal/tui/`.

This keeps the interface stable as formats are added: each new format adds a new
concrete inspector without changing the interface.

## The three-layer read-only defense

The default build is read-only by **three independent mechanisms**. Removing any
one would still leave at least two:

1. **Compile-time isolation** (`cmd/peekdb/main.go` vs `cmd/peekdb/write.go`).
   The default `main` is gated `//go:build !write`; the write-capable `main` is gated
   `//go:build write`. Building with `-tags=write` produces a separate binary
   (`peekdb-mut`) that prints a loud stderr banner on every invocation.

2. **Driver-level read-only flags**. Every driver is opened with the strictest
   read-only option the format supports: SQLite with `mode=ro&immutable=1`
   (the SQLite engine itself rejects writes), bbolt with `Options{ReadOnly: true}`,
   LevelDB opened without write support.

3. **CLI deny-list** (`forbiddenArgNames` in `cmd/peekdb/root.go`). A small map of
   write-verb names (`write`, `edit`, `delete`, `put`, `set`, ...) is checked at
   parse time. Typing `peekdb write <anything>` returns
   `unknown command "write" for "peekdb"` rather than being treated as a file path.

CI runs `scripts/check-readonly.go` against every release binary to confirm no
write subcommand exists and no `Put`/`Delete`/`Write` method is linked from
`internal/inspector/`.

## Key selection decisions

- **modernc.org/sqlite** (not `mattn/go-sqlite3`): pure Go, no CGO. The whole
  project is pure Go so we don't have to ship a C toolchain.
- **go.etcd.io/bbolt**: same author as the original Bolt, actively maintained,
  has a `ReadOnly: true` option that uses flock on Linux.
- **syndtr/goleveldb**: pure Go LevelDB, opens read-only via path-only mode.
- **Bubble Tea** for the TUI: small surface, good defaults, easy to test the model
  without a real terminal.
- **`cobra`** for the CLI: same reason.
- **Build-tag isolation** for the write build: makes the read-only guarantee
  *physically real* (the public binary literally cannot link write code), not
  merely a runtime check.

## Testing

Table-driven tests live next to the code (`*_test.go`). The test task uses
`GOTMPDIR` pointing at Go's own cache directory to avoid Windows Defender racing
the test-binary fork+exec (see `Taskfile.yml`).

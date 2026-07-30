# Requirements

> What's in peekdb v0.1.0, what isn't, and what's known to be limited.

## What peekdb is

A read-only TUI/CLI for browsing local database files. Open SQLite, bbolt, or LevelDB
in your terminal. Search, view, navigate — without writing a byte.

The default build is **physically** read-only: writes are gated behind a `//go:build write`
tag, so the public binary cannot modify your data even by accident.

## In scope (v0.1.0)

- **Three formats**: SQLite (incl. WAL), bbolt, LevelDB — auto-detected from magic bytes
- **Read-only inspectors** for all three
- **Concurrent-use detection** (file lock probe + 1Hz mtime poller)
- **Value rendering**: JSON / string / binary decomposition; UTF-8 / UTF-16 / GBK / Big5 / Latin1
- **CLI**:
  - `peekdb <file>` — TUI
  - `peekdb inspect <file>` — diagnostic dump (text / --json)
  - `peekdb query <file> <sql>` — read-only SQL (SQLite only)
  - `peekdb version` / `peekdb help`
- **TUI**: 3-pane browser, vim-style navigation, `:`-command mode
- **Cross-platform**: macOS, Linux, Windows
- **Pure Go**: no CGO, no gcc required to build

## Out of scope (deferred)

- **Other formats**: DuckDB, RocksDB, BadgerDB, LMDB — v1.1
- **Encryption**: SQLCipher — v2 / v3
- **Write mode**: requires `peekdb-mut` (separate `-tags=write` build) — v2.0
- **Parquet / HDF5 / MessagePack / BSON** — v1.1 / v2

## Known limitations (v0.1.0)

- **Copy-on-Open**: not shipped. The mtime hint in the status bar is the only signal
  that another process is writing the file.
- **`:search` / `:export` / `:layout` / `:theme`** commands not shipped — v1.1
- **Column-level encoding override** not shipped — v1.1
- **bbolt `/`-in-name limitation**: opening an item with `/` in its name returns
  "bucket not found" (the inspector uses `/` as the bucket separator). Documented in code.
- **No Windows ARM64** in release binaries (GoReleaser ignores that matrix cell).

## Design constraints

- **Default build = read-only**: no write APIs linked, no write subcommands.
- **Public Inspector interface is small**: lifecycle + status only. Format-specific
  methods live on concrete types and are reached via type assertion.
- **No CGO**: keeps the build simple and the binary portable.

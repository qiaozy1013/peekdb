# Roadmap

> Where peekdb is going. Roughly ordered by dependency.

## v0.1.0 — current

What's in: three read-only formats, TUI, CLI (`inspect` / `query` / `version`),
auto-detection, concurrent-use detection. See [requirements.md](requirements.md).

## v1.1

- **More formats**: DuckDB, RocksDB, BadgerDB
- **TUI commands**: `:search`, `:export` (JSON / CSV), `:layout`, `:theme`
- **Copy-on-Open dialog** (4-option: open / copy / readonly / fail) when the file is locked
- **Column-level encoding override** (`:encoding column N <enc>`)
- **WAL sidecar file viewer** in TUI (currently a status-bar hint only)

## v1.2

- **Export** the inspected view to JSON / CSV / SQL dump (read-only — the dump
  is a snapshot, not a live connection)
- **Saved views** — bookmark common filter / sort combos in `peekdb` config
- **In-app help browser** — `?` shows the keymap and `:help` lists all commands

## v2.0 — the write build

- **`peekdb-mut` binary** (separate `-tags=write` build, separate release artifact).
  The write build is *physically* the same code as `peekdb` plus a `//go:build write`
  guard; the default binary never contains it. See [architecture.md](architecture.md).
- Subcommands: `peekdb-mut put`, `peekdb-mut delete`, `peekdb-mut edit`,
  `peekdb-mut import`, `peekdb-mut export`
- **Destructive operations prompt for confirmation** by default; `--yes` skips
- **Audit log** to stderr for every write

## v2.x

- **Encryption support** (SQLCipher)
- **Parquet / HDF5 / MessagePack / BSON** readers
- **Plugin interface** for third-party format inspectors (compile-time, via
  build tags — no runtime loading)

## v3.0

- **Schema-aware queries**: a separate "schema" mode that lets you do
  `peekdb query state.db "users JOIN orders ON ..."` with proper completion
- **Multi-file session** (open several files at once, switch between them)
- **Remote / S3-backed databases** (read-only)

## Not planned

- **GUI / desktop app** — TUI is the sweet spot
- **Auto-updater** — go-install and GoReleaser make this unnecessary
- **Telemetry / analytics** — read-only tool, no usage data leaves your machine

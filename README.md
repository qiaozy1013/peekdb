# peekdb

> A read-only TUI/CLI for browsing local database files.
> SQLite, bbolt, LevelDB. Open and explore — without writing a byte.

[中文](README.zh-CN.md) · [Docs](docs/requirements.md)

## Install

```bash
go install github.com/qiaozy1013/peekdb/cmd/peekdb@latest
```

## Quick start

```bash
peekdb ~/path/to/History.db                # browse interactively (TUI)
peekdb inspect --json state.db | jq .Format # diagnostic info
peekdb query state.db "SELECT * FROM users LIMIT 10"  # SQLite only
peekdb write                                # → unknown command (read-only guaranteed)
```

## What it does

- **Auto-detects** the format from the file's magic bytes.
- **Opens read-only** — never writes to your data, even by accident.
- **Concurrent-safe** — won't break apps that have the file open; tells you if the file is locked.
- **3-format support**: SQLite (incl. WAL), bbolt, LevelDB.
- **Cross-platform** — single static binary on macOS, Linux, Windows.
- **Pure Go** — no CGO, no gcc required.

## Verify the read-only guarantee

```bash
peekdb write   # → Error: unknown command "write" for "peekdb"
```

The default binary is **physically** read-only: writes are gated behind a `//go:build write` tag, so the public binary *cannot* modify your data. See [docs/architecture.md](docs/architecture.md) for the three-layer defense.

## Documentation

- [Requirements](docs/requirements.md) — what's in scope, what's not
- [Architecture](docs/architecture.md) — how it's built
- [Roadmap](docs/roadmap.md) — what's coming next

## License

MIT

# Contributing to peekdb

peekdb is a read-only TUI/CLI for browsing local database files. Contributions welcome — bug reports, docs, code, tests.

## Quick links

- **Bugs / features**: open a [GitHub Issue](https://github.com/qiaozy1013/peekdb/issues)
- **Questions / ideas**: [GitHub Discussions](https://github.com/qiaozy1013/peekdb/discussions)
- **Security**: open a confidential report via [GitHub Security Advisories](https://github.com/qiaozy1013/peekdb/security/advisories/new) (do NOT open a public issue)

## Bug reports

Include in the report:

- `peekdb version` output
- OS / arch / Go version
- Database format and file size
- The command you ran and its full output
- Expected vs actual behavior

## Development

```bash
git clone https://github.com/qiaozy1013/peekdb.git
cd peekdb
go mod download
task build
./dist/peekdb version
```

### Local checks (must all pass)

```bash
task fmt
task vet
task test
task check-readonly
```

CI runs the same plus cross-platform tests on macOS, Linux, Windows.

## Project layout

See [AGENTS.md](AGENTS.md). The TL;DR:

- `cmd/peekdb/` — CLI entrypoint
- `internal/inspector/` — per-format inspectors
- `internal/inspect/` — file lock + mtime monitor
- `internal/parse/`, `internal/encode/`, `internal/detect/` — value / encoding / magic-byte helpers
- `internal/tui/` — Bubble Tea UI
- `docs/` — requirements, architecture, roadmap

## Things to watch out for

- **Read-only guarantee**: the default build must stay read-only. Write APIs are gated by the `write` build tag. See [docs/architecture.md](docs/architecture.md) for the three-layer defense.
- **No CGO**: keep dependencies pure Go. No C compiler required to build.
- **Inspector interface**: extend via format-specific methods, not by adding to the public interface.
- **Magic bytes**: register new formats in `internal/detect/magic.go`.
- **No real test data**: use the generator under `testdata/gen/`.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short description>

<optional body>

<optional footer>
```

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `chore`, `revert`.

## Release

Tag-based:

```bash
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

CI builds and publishes binaries to GitHub Releases via [GoReleaser](https://goreleaser.com/).

## License

MIT

# AGENTS.md

> 给 AI 编码助手（Claude Code / Cursor / Copilot / Devin / Codex / ...）看的项目说明
> 人类贡献者请看 [README.md](README.md) 和 [CONTRIBUTING.md](CONTRIBUTING.md)

## What this project is

**peekdb** — 一个**只读**的 TUI/CLI 工具，用于浏览本地数据库文件（SQLite、bbolt、LevelDB 等）。
**默认 build 完全只读**，写功能通过独立 build tag 物理隔离（见 [docs/security.md](docs/security.md)）。

## Tech stack (locked)

- **Language**: Go 1.25+, **pure Go** (无 CGO)
- **TUI**: [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [bubbles](https://github.com/charmbracelet/bubbles) + [lipgloss](https://github.com/charmbracelet/lipgloss)
- **CLI**: [spf13/cobra](https://github.com/spf13/cobra)
- **SQLite**: [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) (pure Go, no CGO)
- **bbolt**: [go.etcd.io/bbolt](https://github.com/etcd-io/bbolt) v1.3.10+ (ReadOnly fix)
- **LevelDB**: [syndtr/goleveldb](https://github.com/syndtr/goleveldb)
- **JSON**: stdlib `encoding/json`
- **Encoding**: `golang.org/x/text/encoding`
- **Linter**: [golangci-lint](https://golangci-lint.run/) v2 (see `.golangci.yml`)
- **Test**: stdlib `testing` + [testify](https://github.com/stretchr/testify)

**不要更换选型**——所有选型在 [docs/design-decisions.md](docs/design-decisions.md) 有 ADR 记录，更换前先讨论。

## Project layout

```
peekdb/
├── cmd/
│   └── peekdb/                  # main entrypoint
│       ├── main.go              # go:build !write / go:build write
│       └── root.go              # cobra root command
├── internal/
│   ├── detect/                  # format auto-detection (magic + heuristic)
│   │   ├── magic.go             # magic byte table
│   │   ├── detect.go            # main detection logic
│   │   └── detect_test.go
│   ├── inspector/               # read-only database inspectors
│   │   ├── inspector.go         # Inspector interface (small)
│   │   ├── registry.go          # format → factory registry
│   │   ├── item.go              # Item, Row, Column types
│   │   ├── sqlite.go            # SQLiteInspector
│   │   ├── bbolt.go             # BoltInspector
│   │   ├── leveldb.go           # LevelDBInspector
│   │   └── *_test.go
│   ├── inspect/                 # use-detection (file lock, mtime, ...)
│   │   ├── lock_unix.go         # flock probe (Unix)
│   │   ├── lock_windows.go      # LockFileEx probe (Windows)
│   │   ├── monitor.go           # mtime polling
│   │   └── *_test.go
│   ├── parse/                   # value decomposition (JSON/string/binary)
│   │   ├── json.go
│   │   ├── string.go
│   │   ├── binary.go
│   │   └── detect.go            # tries each parser
│   ├── encode/                  # character encoding (UTF-8, GBK, UTF-16, ...)
│   │   ├── utf8.go
│   │   ├── gbk.go
│   │   └── utf16.go
│   ├── tui/                     # Bubble Tea UI
│   │   ├── model.go             # main Model
│   │   ├── update.go            # Update function
│   │   ├── view.go              # View function
│   │   ├── keys.go              # key bindings
│   │   ├── command.go           # :command mode (vim-style)
│   │   └── statusbar.go         # bottom status bar
│   └── version/                 # build info
│       └── version.go
├── testdata/                    # mock test data (NEVER real user data)
│   ├── sqlite/
│   ├── bbolt/
│   ├── leveldb/
│   └── gen/                     # generator for mock data
├── docs/                        # all documentation
├── .github/workflows/           # CI/CD
├── README.md
├── LICENSE
├── CHANGELOG.md
├── CONTRIBUTING.md
├── AGENTS.md                    # this file
└── go.mod
```

## Build tags (CRITICAL)

peekdb uses Go build tags to physically separate read-only code from write code:

```go
// Default build: read-only
//go:build !write

// Write build: includes write APIs
//go:build write
```

**When working on this codebase:**

1. **Default build** is the public release. It must be read-only.
2. **Write build** (`-tags=write`) is opt-in and produces a separate binary.
3. **Do NOT add write APIs in default-build files.** Write code must live under `//go:build write` files or in directories only compiled with the tag.
4. **CI checks enforce this** — see `.github/workflows/ci.yml`.

## Code conventions

### Style

- Standard Go style (`gofmt`, `goimports`, `golangci-lint`)
- Use `go vet ./...` before committing
- Follow [Effective Go](https://go.dev/doc/effective_go) and [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)

### Error handling

- **Always wrap errors** with context: `fmt.Errorf("opening file %q: %w", path, err)`
- **Never discard errors** with `_` unless explicitly documented
- **Public errors** should be typed: `var ErrFileLocked = errors.New("file is locked")`
- **Error messages** should include: (1) what failed, (2) why, (3) what to do next

### Naming

- Packages: lowercase, single word, no underscores (`inspector`, not `inspector_pkg`)
- Files: lowercase, snake_case (`sqlite.go`, `lock_unix.go`)
- Types: PascalCase, no stuttering (`inspector.Inspector`, not `inspector.InspectorInspector`)
- Interfaces: small, idiomatic Go (`type Reader interface { Read(...) }`)

### Comments

- **All exported symbols** must have godoc comments
- Comments explain *why*, not *what*
- Use TODO(author): for known incomplete work
- Use FIXME: for known bugs

### Testing

- **Every package** should have `*_test.go`
- **Table-driven tests** for multiple cases
- **Real test data** must NEVER be committed — use mocks under `testdata/gen/`
- Run `go test ./...` before committing

## Adding a new database format

To add a new format (e.g., DuckDB for v1.1):

1. **Create `internal/inspector/duckdb.go`** with `DuckDBInspector` struct
2. **Implement** the `Inspector` interface + format-specific methods
3. **Add magic byte** to `internal/detect/magic.go`
4. **Register** in `internal/inspector/registry.go` (one line)
5. **Add test data** under `testdata/duckdb/` (mocks only)
6. **Update docs/architecture.md** and **docs/mvp-spec.md** (or roadmap)
7. **No changes needed** in TUI/CLI — they auto-pick-up via the registry

**Do not skip the magic byte step** — auto-detection is core to peekdb's value proposition.

## What NOT to do

- ❌ Don't add CGO dependencies (breaks pure-Go promise)
- ❌ Don't add write APIs in default-build files
- ❌ Don't commit real user data (privacy)
- ❌ Don't use `interface{}` when a concrete type or generic works
- ❌ Don't add a new top-level dependency without discussion in an ADR
- ❌ Don't change the public Inspector interface casually — extensions go in format-specific methods
- ❌ Don't replace stdlib when stdlib works (e.g., `encoding/json` over `goccy/go-json`)

## What TO do

- ✅ Run `gofmt`, `goimports`, `go vet`, `golangci-lint` before committing
- ✅ Write tests for new code
- ✅ Update docs when behavior changes
- ✅ Use `internal/` for non-exported code (enforced by Go itself)
- ✅ Add godoc comments on all exported symbols
- ✅ Use `errors.Is` / `errors.As` for error inspection
- ✅ Use `slog` (or `log/slog` after Go 1.21) for structured logging

## Common tasks

### Build the default (read-only) binary

We use [Task](https://taskfile.dev) as the canonical task runner — it works
the same on Windows, macOS, and Linux without GNU Make.

```bash
# Install task (one-time)
go install github.com/go-task/task/v3/cmd/task@latest

# Build
task build              # current platform
task build-mut          # write build (v2.0+)
task release-snapshot   # cross-platform via GoReleaser
```

`task` is the only task runner. If you prefer `make` syntax, use it as
a one-liner per command (e.g. `task build` directly). We do not ship a
Makefile.

### Run all tests

```bash
task test               # works without gcc (no -race)
task test-race          # requires gcc; CI on Linux/macOS
```

### Run tests with coverage

```bash
task coverage           # works without gcc
task coverage-race      # requires gcc
```

### Lint (golangci-lint v2)

```bash
task install-lint       # one-time: install golangci-lint v2 if missing
task lint               # run all enabled linters
task lint-fix           # auto-fix what can be fixed
```

### Tidy dependencies

```bash
task tidy
```

### Update documentation

```bash
# After architecture changes
# Edit docs/architecture.md
# Edit this file (AGENTS.md) if layout changes
```

## Where to look for context

When you're unsure about a design decision, **always check the docs first**:

- **Why this tech stack?** → [docs/design-decisions.md](docs/design-decisions.md)
- **What's in v1?** → [docs/mvp-spec.md](docs/mvp-spec.md)
- **What's coming?** → [docs/roadmap.md](docs/roadmap.md)
- **Why is it read-only?** → [docs/security.md](docs/security.md)
- **How is it tested?** → [docs/testing.md](docs/testing.md)
- **What about format X?** → [docs/architecture.md](docs/architecture.md)

## Working with the human maintainer

- **Default to action**: when the task is clear, do it
- **Surface decisions**: when you make a non-obvious choice, explain why
- **Don't ask before tiny things**: file naming, code organization within agreed-upon structure
- **Do ask before big things**: changing public APIs, adding dependencies, breaking read-only guarantee
- **Show your work**: in PRs, include reasoning, not just diff

## License

MIT — see [LICENSE](LICENSE).

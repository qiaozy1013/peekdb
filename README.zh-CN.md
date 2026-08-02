# peekdb（中文）

> 只读的 TUI/CLI 工具，用于浏览本地数据库文件。
> SQLite、bbolt、LevelDB。打开探索，不写一个字节。

[English](README.md) · [文档](docs/requirements.md)

[![CI](https://github.com/qiaozy1013/peekdb/actions/workflows/ci.yml/badge.svg)](https://github.com/qiaozy1013/peekdb/actions/workflows/ci.yml)

## 安装

```bash
go install github.com/qiaozy1013/peekdb/cmd/peekdb@latest
```

## 快速开始

```bash
peekdb ~/path/to/History.db                       # 交互式浏览（TUI）
peekdb inspect --json state.db | jq .Format        # 诊断信息
peekdb query state.db "SELECT * FROM users LIMIT 10"  # 仅 SQLite
peekdb write                                       # → unknown command（只读保证）
```

## 它能做什么

- **自动识别**文件格式（基于 magic bytes）
- **只读打开** — 永远不写你的数据
- **并发安全** — 不破坏持有文件的 app；告诉你文件是否被锁
- **三种格式**：SQLite（含 WAL）、bbolt、LevelDB
- **跨平台** — macOS / Linux / Windows 单一静态二进制
- **纯 Go** — 零 CGO，编译不需要 gcc

## 验证只读

```bash
peekdb write   # → Error: unknown command "write" for "peekdb"
```

默认二进制**物理**只读：写操作被 `//go:build write` 标签隔离，公开二进制**不可能**修改你的数据。三层防御见 [docs/architecture.md](docs/architecture.md)。

## 文档

- [需求](docs/requirements.md) — 范围内 / 范围外
- [架构](docs/architecture.md) — 怎么构建的
- [路线图](docs/roadmap.md) — 下一步

## 许可证

MIT

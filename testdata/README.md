# testdata/

This directory contains mock data for peekdb integration tests.

## Layout

- sqlite/           magic-header-only SQLite files (16-byte header + zeros/random)
- bbolt/            valid bbolt files (created by go.etcd.io/bbolt in Write mode)
- leveldb/          valid LevelDB directories (created by goleveldb in Write mode)
- negative/         non-database files (PNG, text, empty, random)

## IMPORTANT

The bbolt and LevelDB files are real (the libraries' own Open functions
will accept them in ReadOnly mode). The SQLite files contain only the
16-byte magic header — Detect's strong-magic step accepts them, but
the library probe will fail (which is fine; the library probe is the
Inspector's job, not Detect's).

Real user data (e.g. a copy of your Chrome History) must NEVER be
committed to this repository. See docs/testing.md for the data privacy
policy.

## Regenerate

```
cd testdata/gen && go run .
```

## Files

### SQLite (magic-only)
- sqlite/empty.db               16-byte header only
- sqlite/chrome-history.db       magic + 4 KB zeros
- sqlite/vscode-state.db         same as chrome-history.db
- sqlite/corrupt.db              magic + random tail

### bbolt (valid)
- bbolt/empty.db                valid bbolt with a 'test' bucket
- bbolt/etcd-like.db             valid bbolt with a 'key' bucket

### LevelDB (valid)
- leveldb/empty/                 minimal LevelDB with one key
- leveldb/chrome-indexeddb/      minimal LevelDB with one key
- leveldb/with-manifest/         minimal LevelDB with one key

### Negative (not a database)
- negative/image.png             PNG header
- negative/notes.txt             plain text
- negative/empty                 zero-byte file
- negative/random.bin            random bytes

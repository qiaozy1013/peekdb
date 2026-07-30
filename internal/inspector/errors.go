package inspector

import (
	"log/slog"
)

// silentErr is the inspector package's shared helper for
// best-effort probe failures ().
//
//  4 call sites in the inspector package that
// silently swallow errors with `_`:
//
//   - bbolt.go:94       Stats:     _ = b.db.View(...)
//   - bbolt.go:118      Items:     _ = b.db.View(...)
//   - sqlite.go:218     Stats:     stats, _ := s.tableStats()
//   - leveldb.go:301    dirSize:   _ = os.ReadDir(...)
//
// AGENTS.md says: "Never discard errors with `_` unless
// explicitly documented." The intent here is genuinely
// best-effort — Stats / Items have no error channel (Stats
// is a value, Items returns (slice, error) but the items list
// itself is informational). silentErr keeps the existing
// semantics (return zero values) while surfacing the error
// in the log at Debug level for diagnostics.
//
// In v1.0 the default build never produces these errors in
// practice (the db is opened read-only and the file exists
// by construction — see NewBolt/NewLevelDB/NewSQLite). The
// helper exists so a future format-specific probe that does
// hit an error gets logged, not lost.
func silentErr(err error, op string) {
	if err == nil {
		return
	}
	slog.Debug("inspector: best-effort probe failed", "op", op, "err", err)
}

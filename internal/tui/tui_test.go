package tui

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	_ "modernc.org/sqlite"

	"github.com/qiaozy1013/peekdb/internal/detect"
	"github.com/qiaozy1013/peekdb/internal/inspector"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{42 * 1024 * 1024, "42.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatBytes(tt.in); got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatCount(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "-"},
		{-1, "-"},
		{1, "1"},
		{12, "12"},
		{123, "123"},
		{1234, "1,234"},
		{12345, "12,345"},
		{123456, "123,456"},
		{12432, "12,432"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatCount(tt.in); got != tt.want {
				t.Errorf("formatCount(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTruncateForDisplay(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 4, "hel…"},
		{"hello", 1, "h"},
		{"hello", 0, "hello"},
		{"hello", -1, "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := truncateForDisplay(tt.in, tt.n); got != tt.want {
				t.Errorf("truncateForDisplay(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
		})
	}
}

func TestStringifyValue(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{nil, "<nil>"},
		{int64(42), "42"},
		{float64(3.14), "3.14"},
		{true, "true"},
		{"hi", "hi"},
		{[]byte("hello"), `"hello"`},       // byte slice -> quoted
		{[]byte{0xff, 0xfe}, `"\xff\xfe"`}, // non-printable -> escaped
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := stringifyValue(tt.in)
			if got != tt.want {
				t.Errorf("stringifyValue(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExecuteCommand(t *testing.T) {
	t.Run("quit", func(t *testing.T) {
		m := model{}
		newM, _ := m.executeCommand("q")
		if !newM.(model).quit {
			t.Errorf(":q should set quit=true")
		}
	})
	t.Run("quit_alias", func(t *testing.T) {
		m := model{}
		newM, _ := m.executeCommand("QUIT")
		if !newM.(model).quit {
			t.Errorf(":quit should set quit=true")
		}
	})
	t.Run("help", func(t *testing.T) {
		m := model{}
		newM, _ := m.executeCommand("help")
		if !newM.(model).showHelp {
			t.Errorf(":help should set showHelp=true")
		}
	})
	t.Run("h_alias", func(t *testing.T) {
		m := model{}
		newM, _ := m.executeCommand("h")
		if !newM.(model).showHelp {
			t.Errorf(":h should set showHelp=true")
		}
	})
	t.Run("empty", func(t *testing.T) {
		m := model{}
		newM, _ := m.executeCommand("")
		if newM.(model).quit || newM.(model).showHelp {
			t.Errorf("empty command should be a no-op")
		}
		if newM.(model).err != nil {
			t.Errorf("empty command should not set err; got %v", newM.(model).err)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		m := model{}
		newM, _ := m.executeCommand("frobnicate")
		fm := newM.(model)
		if fm.err == nil {
			t.Errorf("unknown command should set err")
		}
		if !strings.Contains(fm.err.Error(), "frobnicate") {
			t.Errorf("err should mention the command name; got %v", fm.err)
		}
	})
	t.Run("encoding_shows_current", func(t *testing.T) {
		m := model{}
		newM, _ := m.executeCommand("encoding")
		fm := newM.(model)
		if fm.err == nil || !strings.Contains(fm.err.Error(), "utf-8") {
			t.Errorf(":encoding with no arg should set err showing current; got %v", fm.err)
		}
	})
	t.Run("encoding_set_valid", func(t *testing.T) {
		m := model{}
		newM, _ := m.executeCommand("encoding gbk")
		fm := newM.(model)
		if fm.encoding != "gbk" {
			t.Errorf(":encoding gbk: encoding = %q, want %q", fm.encoding, "gbk")
		}
	})
	t.Run("encoding_set_invalid", func(t *testing.T) {
		m := model{}
		newM, _ := m.executeCommand("encoding euc-kr")
		fm := newM.(model)
		if fm.err == nil || !strings.Contains(fm.err.Error(), "euc-kr") {
			t.Errorf(":encoding euc-kr: expected err mentioning encoding name; got %v", fm.err)
		}
	})
}

func TestMonitorTickMsg(t *testing.T) {
	// The monitor goroutine sends a monitorTickMsg when the
	// file changes. The Update handler must set m.changed
	// and re-issue listenForMonitor so the TUI keeps
	// watching.
	// We cannot easily spin a real monitor in a unit
	// test (it needs a file), so we test the Update path
	// directly: a monitorTickMsg with m.monitor=nil is
	// still safe (listenForMonitor returns nil immediately).
	m := model{monitor: nil, monitorPath: "fake"}
	newM, cmd := m.Update(monitorTickMsg{})
	fm := newM.(model)
	if fm.changed.IsZero() {
		t.Errorf("monitorTickMsg should set m.changed")
	}
	if cmd == nil {
		t.Errorf("monitorTickMsg should re-issue listenForMonitor")
	}
}

func TestStatusBar_WithChanged(t *testing.T) {
	// When m.changed is set, the status bar should mention
	// it and the r-to-refresh hint.
	m := model{
		stats:   inspectorStats(),
		items:   []inspector.Item{},
		changed: time.Now().Add(-3 * time.Second),
	}
	got := renderStatusBar(m)
	if !strings.Contains(got, "changed") {
		t.Errorf("status bar missing 'changed' marker; got %q", got)
	}
	if !strings.Contains(got, "r to refresh") {
		t.Errorf("status bar missing 'r to refresh' hint; got %q", got)
	}
}

func TestRelativeAge(t *testing.T) {
	tests := []struct {
		age  time.Duration
		want string
	}{
		{3 * time.Second, "3s"},
		{5 * time.Minute, "5m"},
		{2 * time.Hour, "2h"},
		{48 * time.Hour, "2d"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := relativeAge(time.Now().Add(-tt.age))
			if got != tt.want {
				t.Errorf("relativeAge(%v) = %q, want %q", tt.age, got, tt.want)
			}
		})
	}
}

func TestCommandModeKeyHandling(t *testing.T) {
	t.Run("type_and_backspace", func(t *testing.T) {
		m := model{commandMode: true, commandBuf: "hel"}
		// Backspace removes one char.
		newM, _ := m.updateCommand(tea.KeyMsg{Type: tea.KeyBackspace})
		fm := newM.(model)
		if fm.commandBuf != "he" {
			t.Errorf("after backspace: commandBuf = %q, want %q", fm.commandBuf, "he")
		}
		if !fm.commandMode {
			t.Errorf("command mode should still be active after backspace")
		}
	})
	t.Run("esc_clears", func(t *testing.T) {
		m := model{commandMode: true, commandBuf: "qq"}
		newM, _ := m.updateCommand(tea.KeyMsg{Type: tea.KeyEsc})
		fm := newM.(model)
		if fm.commandMode {
			t.Errorf("esc should exit command mode")
		}
		if fm.commandBuf != "" {
			t.Errorf("esc should clear commandBuf; got %q", fm.commandBuf)
		}
	})
	t.Run("enter_executes", func(t *testing.T) {
		m := model{commandMode: true, commandBuf: "q"}
		newM, _ := m.updateCommand(tea.KeyMsg{Type: tea.KeyEnter})
		fm := newM.(model)
		if fm.commandMode {
			t.Errorf("enter should exit command mode")
		}
		if !fm.quit {
			t.Errorf("enter on :q should quit")
		}
	})
}

func TestView_LoadingState(t *testing.T) {
	m := model{ready: false}
	if got := m.View(); got != "loading…" {
		t.Errorf("not-ready View = %q, want %q", got, "loading…")
	}
}

func TestView_HelpRenders(t *testing.T) {
	m := model{ready: true, showHelp: true, width: 80, height: 24}
	got := m.View()
	if !strings.Contains(got, "peekdb TUI help") {
		t.Errorf("help View missing title; got %q", got)
	}
	if !strings.Contains(got, ":q") {
		t.Errorf("help View should document :q; got %q", got)
	}
}

func TestView_StatusBarHasExpectedFields(t *testing.T) {
	// Compose a model with known stats and verify the
	// status bar carries the field shapes documented in
	// docs/tui.md § 4.
	m := model{
		ready: true,
		stats: inspectorStats(),
		items: []inspector.Item{},
		width: 80, height: 24,
	}
	got := renderStatusBar(m)
	for _, want := range []string{"[", "]", "items", "readonly", "no-lock"} {
		if !strings.Contains(got, want) {
			t.Errorf("status bar missing %q; got %q", want, got)
		}
	}
}

func TestView_ItemsViewRendersColumn(t *testing.T) {
	m := model{
		ready: true,
		items: []inspector.Item{
			{Name: "users", Kind: "table", Count: 12432},
			{Name: "visits", Kind: "table", Count: 89210},
		},
		cursor:      0,
		stats:       inspectorStats(),
		width:       80,
		height:      24,
		itemsScroll: 0,
	}
	got := m.View()
	if !strings.Contains(got, "▸ users") {
		t.Errorf("Items view missing cursor marker on 'users'; got %q", got)
	}
	if !strings.Contains(got, "12,432") {
		t.Errorf("Items view missing count with thousand-separator; got %q", got)
	}
}

// ensure the unused-argument helper still has a body.
// (Go vet may flag a function whose `_ = err` line is
// dead code; the test below gives the import a use.)
var _ = errors.New

// inspectorStats is a tiny constructor for the Stats
// fields the tests assert against. Kept in the test file
// because the production TUI builds a real Stats from a
// real inspector; the test only needs the shape.
func inspectorStats() inspector.Stats {
	return inspector.Stats{
		FormatVer: "SQLite 3.45.0",
		Size:      42 * 1024 * 1024,
		ReadMode:  "readonly",
		LockState: "",
	}
}

// makeSearchTUIDB creates a small SQLite DB in t.TempDir() and
// returns a fully-wired model with a real SQLiteInspector. Used
// by the :search command tests. Duplicated from the inspector
// test helpers (rather than exporting them) so the TUI tests
// stay self-contained.
func makeSearchTUIDB(t *testing.T) model {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tui-search.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	stmts := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT)`,
		`INSERT INTO users (name, email) VALUES ('alice', 'a@example.com')`,
		`INSERT INTO users (name, email) VALUES ('bob',   'b@example.com')`,
		`INSERT INTO users (name, email) VALUES ('carol', 'c@example.com')`,
	}
	for _, s := range stmts {
		if _, execErr := db.Exec(s); execErr != nil {
			t.Fatalf("exec %q: %v", s, execErr)
		}
	}
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	items, err := insp.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	return model{
		ins:    insp,
		items:  items,
		ready:  true,
		cursor: 0,
		stats:  inspectorStats(),
	}
}

func TestExecuteCommand_Search(t *testing.T) {
	t.Run("hit", func(t *testing.T) {
		m := makeSearchTUIDB(t)
		defer func() { _ = m.ins.Close() }()
		newM, _ := m.executeCommand("search alice")
		fm := newM.(model)
		if fm.currentName != "users" {
			t.Errorf("currentName = %q, want users", fm.currentName)
		}
		if len(fm.contentLines) != 1 {
			t.Errorf("contentLines = %d, want 1; got %+v", len(fm.contentLines), fm.contentLines)
		}
		if fm.err == nil || !strings.Contains(fm.err.Error(), "matched 1") {
			t.Errorf("err should report matched 1; got %v", fm.err)
		}
		if !strings.Contains(fm.contentLines[0], "alice") {
			t.Errorf("content line should contain 'alice'; got %q", fm.contentLines[0])
		}
	})
	t.Run("multi_word_pattern", func(t *testing.T) {
		m := makeSearchTUIDB(t)
		defer func() { _ = m.ins.Close() }()
		// "example.com" matches all 3 rows.
		newM, _ := m.executeCommand("search example.com")
		fm := newM.(model)
		if len(fm.contentLines) != 3 {
			t.Errorf("contentLines = %d, want 3", len(fm.contentLines))
		}
	})
	t.Run("no_match", func(t *testing.T) {
		m := makeSearchTUIDB(t)
		defer func() { _ = m.ins.Close() }()
		newM, _ := m.executeCommand("search zzzz_nope")
		fm := newM.(model)
		if len(fm.contentLines) != 0 {
			t.Errorf("contentLines = %d, want 0", len(fm.contentLines))
		}
		if fm.err == nil || !strings.Contains(fm.err.Error(), "matched 0") {
			t.Errorf("err should report matched 0; got %v", fm.err)
		}
	})
	t.Run("missing_pattern", func(t *testing.T) {
		m := makeSearchTUIDB(t)
		defer func() { _ = m.ins.Close() }()
		newM, _ := m.executeCommand("search")
		fm := newM.(model)
		if fm.err == nil || !strings.Contains(fm.err.Error(), "pattern") {
			t.Errorf("err should mention pattern; got %v", fm.err)
		}
	})
	t.Run("s_alias", func(t *testing.T) {
		m := makeSearchTUIDB(t)
		defer func() { _ = m.ins.Close() }()
		newM, _ := m.executeCommand("s bob")
		fm := newM.(model)
		if len(fm.contentLines) != 1 {
			t.Errorf(":s bob: contentLines = %d, want 1", len(fm.contentLines))
		}
	})
}

func TestExecuteCommand_Search_NonSQLiteRejected(t *testing.T) {
	// Build a model whose inspector is NOT a *SQLiteInspector
	// (we use the bbolt path of the inspector package). The
	// command must refuse with a clear error, not panic.
	dir := t.TempDir()
	bpath := filepath.Join(dir, "x.bbolt")
	mk, closeFn, err := openBoltForTest(bpath)
	if err != nil {
		t.Fatalf("setup bbolt: %v", err)
	}
	defer closeFn()
	m := model{
		ins:    mk,
		items:  []inspector.Item{{Name: "default", Kind: "bucket"}},
		cursor: 0,
		ready:  true,
		stats:  inspectorStats(),
	}
	newM, _ := m.executeCommand("search alice")
	fm := newM.(model)
	if fm.err == nil {
		t.Fatalf("expected :search on bbolt to set err; got nil")
	}
	if !strings.Contains(fm.err.Error(), "only implemented for SQLite") {
		t.Errorf("err should mention SQLite limitation; got %v", fm.err)
	}
}

// openBoltForTest is a thin wrapper that returns an open
// *BoltInspector. If the bbolt inspector is not registered
// yet (it should be, via init() in production), the test
// fails. The pair of (inspector, closeFn) keeps the t.Cleanup
// wiring local to the test.
func openBoltForTest(path string) (inspector.Inspector, func(), error) {
	// NewBolt requires the file to exist (it stats first to
	// refuse directories). Touch an empty file so the open
	// proceeds; we do not need a meaningful bbolt for this
	// test — only an inspector whose concrete type is NOT
	// *SQLiteInspector, so :search's type assertion fails.
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return nil, nil, err
	}
	insp, err := inspector.NewBolt(path, inspector.Options{})
	if err != nil {
		return nil, nil, err
	}
	return insp, func() { _ = insp.Close() }, nil
}

// detect import: keep it referenced so `goimports` does not
// strip it from the import list when no other test uses it.
var _ = detect.FormatSQLite

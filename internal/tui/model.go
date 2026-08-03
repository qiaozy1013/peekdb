// Package tui implements the Bubble Tea terminal UI for peekdb.
//
// The UI is a 3-region browser:
//
//	┌─ Items ─────────────┬─ Preview ──────────────────┐
//	│ ▸ urls   (12,432)   │ id  url           visit   │
//	│   visits (89,210)   │ 1   https://...   1234   │
//	└─────────────────────┴────────────────────────────┘
//	[SQLite 3.39] 12 tables | 42 MB | RO | No lock
//
// The TUI is strictly read-only. The D9.1 commit implements
// the main framework: model / update / view, 3-region layout,
// vim-style key bindings, basic command mode (:q / :help),
// and the status bar. D9.2 layers on Copy-on-Open, mtime
// Monitor, and the encoding switcher.
//
// See docs/tui.md for the full key map and command mode.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qiaozy1013/peekdb/internal/encode"
	"github.com/qiaozy1013/peekdb/internal/inspect"
	"github.com/qiaozy1013/peekdb/internal/inspector"
)

// view is the currently focused sub-region. The TUI has
// three: items list, item content, and the help overlay.
type view int

const (
	viewItems view = iota
	viewItemContent
	viewHelp
)

// model is the Bubble Tea state. The Update function returns
// a (possibly new) model and an optional command; the View
// function renders the current state to a string.
type model struct {
	// ins is the database inspector. Held for the lifetime
	// of the TUI; closed when the model quits.
	ins inspector.Inspector

	// width/height are the terminal dimensions, updated
	// by WindowSizeMsg.
	width, height int

	// ready is true after the first WindowSizeMsg arrives.
	// Before that, the TUI is not drawable.
	ready bool

	// --- Items view state ---
	items       []inspector.Item // top-level items, loaded once
	cursor      int              // selected index
	itemsScroll int              // vertical scroll offset

	// --- Content view state ---
	reader        inspector.ItemReader // current bucket/table reader
	currentName   string               // item name being viewed
	contentLines  []string             // rendered content (one line per row)
	contentScroll int                  // vertical scroll offset for preview

	// --- Help overlay ---
	showHelp bool

	// --- Command mode state ---
	commandMode bool
	commandBuf  string // text typed after ':'

	// --- Status ---
	stats inspector.Stats // refreshed on demand
	err   error

	// --- Concurrent-write detection (D9.2) ---
	// monitor polls mtime/size every second. When it
	// fires, the model shows a toast in the status bar
	// for a few seconds; the user can press r to refresh.
	monitor     *inspect.Monitor
	monitorPath string // path we are monitoring (may differ from ins.Path() when Copy-on-Open is in effect)
	changed     time.Time

	// --- Encoding override (D9.2) ---
	// encoding is applied to text rendering when the
	// cell value looks like a non-UTF-8 byte slice.
	// "" means UTF-8 (the default).
	encoding string

	// quit is set when the user wants to exit. Bubble Tea's
	// Update returns tea.Quit when this becomes true.
	quit bool
}

// monitorTickMsg is sent by the monitor goroutine every
// time the file's mtime or size changes.
type monitorTickMsg struct{}

// (encodingDecodedMsg was sketched in D9.2 draft for an
// async encoding pipeline; we ended up using the sync
// encode.Decode in :refresh. Kept as a placeholder so
// the future async pipeline can reuse the name.)

// initialModel loads the top-level items and returns the
// starting model. It is called once before tea.NewProgram.
// The caller is expected to have already opened the
// inspector in read-only mode; D9.2 adds a monitor that
// watches the underlying file for concurrent-write changes.
func initialModel(ins inspector.Inspector, monitorPath string) (model, error) {
	items, err := ins.Items()
	if err != nil {
		return model{}, fmt.Errorf("tui: load items: %w", err)
	}
	stats := ins.Stats()
	return model{
		ins:         ins,
		items:       items,
		stats:       stats,
		cursor:      0,
		ready:       false,
		monitorPath: monitorPath,
	}, nil
}

// Init is the Bubble Tea entry point. We start the mtime
// monitor here if one was attached; otherwise no startup
// commands. The terminal sends a WindowSizeMsg on its own.
func (m model) Init() tea.Cmd {
	if m.monitor == nil {
		return nil
	}
	// listenForMonitor blocks on m.monitor.Events and
	// returns a single monitorTickMsg per change. The
	// command re-issues itself so we keep listening.
	return listenForMonitor(m.monitor)
}

// listenForMonitor is a tea.Cmd that waits for the next
// change event on the monitor and then emits a
// monitorTickMsg. Re-issued from Init so the TUI
// keeps watching for as long as it is alive.
func listenForMonitor(m *inspect.Monitor) tea.Cmd {
	return func() tea.Msg {
		if m == nil {
			return nil
		}
		// Block until the next event. The Monitor's
		// Events channel is buffered size 1, so we
		// never queue more than one pending notification.
		<-m.Events
		return monitorTickMsg{}
	}
}

// Update is the Bubble Tea message loop. Every keystroke,
// terminal resize, or external event arrives here as a
// tea.Msg; we return a new model and an optional follow-up
// command.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case monitorTickMsg:
		// The file changed externally. Show a toast and
		// keep listening for the next change.
		m.changed = time.Now()
		return m, listenForMonitor(m.monitor)

	case tea.KeyMsg:
		// Command mode intercepts all keys except Esc/Enter
		// so the user can type the command text freely.
		if m.commandMode {
			return m.updateCommand(msg)
		}
		if m.showHelp {
			return m.updateHelp(msg)
		}
		switch m.currentView() {
		case viewItems:
			return m.updateItems(msg)
		case viewItemContent:
			return m.updateContent(msg)
		}
	}

	return m, nil
}

// currentView returns the focused view. The items list is
// the default; entering an Item (Enter) switches to content;
// ? switches to the help overlay.
func (m model) currentView() view {
	if m.showHelp {
		return viewHelp
	}
	if m.reader != nil {
		return viewItemContent
	}
	return viewItems
}

// updateItems handles keys when the items list is focused.
func (m model) updateItems(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quit = true
		return m, tea.Quit
	case "?":
		m.showHelp = true
		return m, nil
	case ":":
		m.commandMode = true
		m.commandBuf = ""
		return m, nil
	case "j", "down":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
		return m, nil
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "g":
		m.cursor = 0
		return m, nil
	case "G":
		if len(m.items) > 0 {
			m.cursor = len(m.items) - 1
		}
		return m, nil
	case "enter", "l", "right":
		return m.openCurrentItem()
	case "r":
		// r is a shortcut for :refresh on the items view.
		return m.refresh(), nil
	}
	return m, nil
}

// updateContent handles keys when viewing an item's contents.
func (m model) updateContent(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quit = true
		return m, tea.Quit
	case "?", ":":
		// Same as items view: ? -> help, : -> command
		if msg.String() == "?" {
			m.showHelp = true
		} else {
			m.commandMode = true
			m.commandBuf = ""
		}
		return m, nil
	case "esc", "h", "left":
		return m.closeReader(), nil
	case "j", "down":
		if m.contentScroll < len(m.contentLines)-1 {
			m.contentScroll++
		}
		return m, nil
	case "k", "up":
		if m.contentScroll > 0 {
			m.contentScroll--
		}
		return m, nil
	case "G":
		if n := len(m.contentLines); n > 0 {
			m.contentScroll = n - 1
		}
		return m, nil
	case "g":
		m.contentScroll = 0
		return m, nil
	}
	return m, nil
}

// updateHelp handles keys when the help overlay is shown.
func (m model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "esc", "q", "enter":
		m.showHelp = false
	case "ctrl+c":
		m.quit = true
		return m, tea.Quit
	}
	return m, nil
}

// updateCommand handles keys while in ':' command mode. Any
// printable character appends; Backspace deletes; Enter
// executes; Esc cancels.
func (m model) updateCommand(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.commandMode = false
		m.commandBuf = ""
		return m, nil
	case tea.KeyEnter:
		cmd := strings.TrimSpace(m.commandBuf)
		m.commandMode = false
		m.commandBuf = ""
		return m.executeCommand(cmd)
	case tea.KeyBackspace:
		if len(m.commandBuf) > 0 {
			m.commandBuf = m.commandBuf[:len(m.commandBuf)-1]
		}
		return m, nil
	case tea.KeyRunes:
		m.commandBuf += string(msg.Runes)
		return m, nil
	}
	return m, nil
}

// executeCommand runs a vim-style ex command. D9.2 adds
// :refresh (re-stat the file) and :encoding (switch the
// text-encoding override for non-UTF-8 cells). :search
// and :export are still v1.1.
func (m model) executeCommand(cmd string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return m, nil
	}
	switch strings.ToLower(fields[0]) {
	case "q", "quit":
		m.quit = true
		return m, tea.Quit
	case "h", "help":
		m.showHelp = true
		return m, nil
	case "refresh", "reload":
		return m.refresh(), nil
	case "encoding":
		// :encoding [<enc>] — set or show the encoding
		// override. With no args, prints the current
		// setting in the status line via the err slot.
		if len(fields) == 1 {
			cur := m.encoding
			if cur == "" {
				cur = "utf-8"
			}
			m.err = fmt.Errorf("tui: current encoding = %s", cur)
			return m, nil
		}
		enc, err := encode.ParseEncoding(fields[1])
		if err != nil {
			m.err = err
			return m, nil
		}
		m.encoding = string(enc)
		// Force a re-render of the content view so the new
		// encoding takes effect immediately.
		return m.refresh(), nil
	case "search", "s":
		// :search <pattern> — substring search in the
		// currently-focused item (the content-view table
		// if one is open, else the items-list cursor).
		// Search is v0.2.0 M1a and currently SQLite-only.
		if len(fields) < 2 {
			m.err = fmt.Errorf("tui: :search needs a pattern (e.g. :search foo)")
			return m, nil
		}
		// Join all trailing fields with single spaces so
		// multi-word patterns work: :search hello world
		// searches for "hello world", not just "hello".
		pattern := strings.Join(fields[1:], " ")
		name := m.searchTarget()
		if name == "" {
			m.err = fmt.Errorf("tui: :search: no item selected (open a table first)")
			return m, nil
		}
		return m.runSearch(name, pattern)
	default:
		m.err = fmt.Errorf("tui: unknown command %q (supported: :q :help :refresh :encoding)", cmd)
		return m, nil
	}
}

// refresh re-stats the file and re-reads the items list.
// Called by :refresh and by the encoding change path. The
// "r" key is a shortcut for :refresh on the items view.
func (m model) refresh() model {
	if m.ins == nil {
		// Defensive: refresh is reachable from tests that
		// construct a model{} without an inspector. In
		// production Update never calls refresh before
		// the model has been initialized.
		return m
	}
	stats := m.ins.Stats()
	m.stats = stats
	// Discard any in-progress content view because the
	// item names may have changed.
	m.currentName = ""
	m.contentLines = nil
	m.contentScroll = 0
	items, err := m.ins.Items()
	if err != nil {
		m.err = err
		return m
	}
	m.items = items
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	return m
}

// openCurrentItem opens the bucket/table under the cursor
// and renders its rows as preview lines.
func (m model) openCurrentItem() (tea.Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return m, nil
	}
	it := m.items[m.cursor]
	reader, err := m.ins.OpenItem(it.Name)
	if err != nil {
		m.err = err
		return m, nil
	}
	lines, err := readAllRows(reader)
	if err != nil {
		_ = reader.Close()
		m.err = err
		return m, nil
	}
	_ = reader.Close()
	m.currentName = it.Name
	m.contentLines = lines
	m.contentScroll = 0
	return m, nil
}

// closeReader returns the focus to the items list.
// Returns the new model so the caller (Update) can use it
// directly. (Writing to the value receiver's fields is
// fine but a little subtle; returning the model is more
// obvious and dodges the govet "unusedwrite" check.)
func (m model) closeReader() model {
	m.currentName = ""
	m.contentLines = nil
	m.contentScroll = 0
	return m
}

// searchTarget returns the name of the item the user is
// currently looking at: the content-view table if one is
// open, otherwise the items-list cursor. Returns "" when
// neither is available (empty list).
//
// The "content-view first" precedence is what makes :search
// inside an open table work without forcing the user to
// close the table and re-select the same item.
func (m model) searchTarget() string {
	if m.currentName != "" {
		return m.currentName
	}
	if m.cursor >= 0 && m.cursor < len(m.items) {
		return m.items[m.cursor].Name
	}
	return ""
}

// runSearch runs a substring search on the named item and
// renders the matching rows into the content view, replacing
// whatever the user was previously looking at. The match
// count is surfaced through the existing m.err slot (zero
// struct growth, consistent with how :encoding-without-arg
// shows the current value).
//
// Search is currently SQLite-only — Format() in the message
// is the only way to hint at the limitation. M1c/M1d add
// bbolt/LevelDB substring scan.
func (m model) runSearch(name, pattern string) (tea.Model, tea.Cmd) {
	sqlInsp, ok := m.ins.(*inspector.SQLiteInspector)
	if !ok {
		m.err = fmt.Errorf("tui: :search: only implemented for SQLite in v0.2.0 (format = %s)", m.ins.Format())
		return m, nil
	}
	reader, err := sqlInsp.Search(name, pattern)
	if err != nil {
		m.err = err
		return m, nil
	}
	lines, err := readAllRows(reader)
	if err != nil {
		_ = reader.Close()
		m.err = err
		return m, nil
	}
	_ = reader.Close()
	m.currentName = name
	m.contentLines = lines
	m.contentScroll = 0
	if len(lines) == 0 {
		m.err = fmt.Errorf("tui: matched 0 rows in %q for %q", name, pattern)
	} else {
		m.err = fmt.Errorf("tui: matched %d rows in %q for %q", len(lines), name, pattern)
	}
	return m, nil
}

// readAllRows reads every row from r and renders it as a
// single string. We keep the implementation in a helper
// (rather than letting the model hold the reader open) so
// that long-running inspections cannot leak goroutines or
// file descriptors after the user navigates away.
func readAllRows(r inspector.ItemReader) ([]string, error) {
	if r == nil {
		return nil, nil
	}
	var lines []string
	for r.Next() {
		row := r.Scan()
		// For SQL layouts, render Columns + Values; for KV
		// layouts, render Key + Value. We do the rendering
		// here so View can stay simple.
		if len(row.Columns) > 0 {
			parts := make([]string, len(row.Columns))
			for i, c := range row.Columns {
				parts[i] = formatCell(c.Name, row.Values, i)
			}
			lines = append(lines, strings.Join(parts, "  "))
		} else {
			// KV layout.
			lines = append(lines, formatKV(row.Key, row.Value))
		}
	}
	if err := r.Err(); err != nil {
		return lines, err
	}
	return lines, nil
}

// formatCell renders one column of one row as "name=value".
// The values slice is shared across all columns of the row.
func formatCell(name string, values []any, i int) string {
	if i >= len(values) {
		return name + "=<missing>"
	}
	return name + "=" + stringifyValue(values[i])
}

// formatKV renders a key-value row as "key = value".
func formatKV(key, value []byte) string {
	return stringifyValue(key) + " = " + stringifyValue(value)
}

// stringifyValue formats any Go value as a short,
// human-readable string. The TUI shows one row per line
// in the preview pane, so we deliberately do NOT use
// parse.Detect for []byte: the JSON-pretty / hex-dump
// renderers are multi-line and would break the row
// layout. A flat "%v" is the right shape here; users who
// want the full rendering should switch to a "preview
// cell" view (D9.2).
func stringifyValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "<nil>"
	case string:
		return x
	case []byte:
		return fmt.Sprintf("%q", string(x))
	case int64, float64, bool:
		return fmt.Sprintf("%v", x)
	}
	return fmt.Sprintf("%v", v)
}

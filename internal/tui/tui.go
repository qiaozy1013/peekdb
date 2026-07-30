package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qiaozy1013/peekdb/internal/inspect"
	"github.com/qiaozy1013/peekdb/internal/inspector"
)

// Run starts the TUI on the given file. It blocks until
// the user quits or the terminal is closed.
//
// The TUI is strictly read-only: it opens the file via
// inspector.Open, which selects the read-only driver
// option for each format. There is no path from any
// keystroke to a write call.
//
// D9.2 also starts an inspect.Monitor that watches the
// file for mtime/size changes. When the file changes, the
// status bar shows a "changed 3s ago" hint until the user
// presses r or runs :refresh.
func Run(path string) error {
	ins, err := inspector.Open(path, inspector.Options{})
	if err != nil {
		return fmt.Errorf("tui: open %q: %w", path, err)
	}
	defer func() { _ = ins.Close() }()

	monitor := inspect.NewMonitor(path, time.Second)
	defer func() { monitor.Stop() }()

	m, err := initialModel(ins, path)
	if err != nil {
		return err
	}
	m.monitor = monitor

	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("tui: run: %w", err)
	}
	// finalModel is the same model we started with, mutated
	// by Update calls. We can read it here to surface any
	// final state (e.g. a deferred error) to the caller, but
	// for v1 we only need it to know whether the user
	// pressed q / Ctrl+C (which returns nil from Run) or
	// an unrecoverable error happened.
	if fm, ok := finalModel.(model); ok && fm.err != nil {
		return fm.err
	}
	return nil
}

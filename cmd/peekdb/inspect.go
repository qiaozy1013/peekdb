package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/qiaozy1013/peekdb/internal/detect"
	"github.com/qiaozy1013/peekdb/internal/inspector"
)

// inspectCmd prints a diagnostic snapshot of a database
// file: format, size, mtime, top-level item count,
// detected lock state, and the file's format-specific
// version (e.g. SQLite 3.45.0). It is the script-friendly
// counterpart to the TUI's status bar.
var inspectCmd = &cobra.Command{
	Use:   "inspect <file>",
	Short: "Print diagnostic info for a database file",
	Long: `inspect opens the file in read-only mode and prints a diagnostic
snapshot: the detected format, file size, modification time, the
number of top-level items (tables / buckets / keygroups), the
detected lock state, and the format-specific engine version.

Useful in shell pipelines and bug reports:

  peekdb inspect ~/Library/Application Support/Google/Chrome/Default/History
  peekdb inspect --json /var/lib/etcd/member/snap/db | jq .Format`,
	Args: cobra.ExactArgs(1),
	RunE: runInspect,
}

func runInspect(cmd *cobra.Command, args []string) error {
	path := args[0]
	insp, err := inspector.Open(path, inspector.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = insp.Close() }()

	stats := insp.Stats()
	items, _ := insp.Items()
	itemNames := make([]string, 0, len(items))
	for _, it := range items {
		itemNames = append(itemNames, it.Name)
	}

	out := cmd.OutOrStdout()
	if rootFlags.JSON {
		return writeInspectJSON(out, insp, stats, itemNames)
	}
	writeInspectText(out, insp, stats, itemNames)
	return nil
}

func writeInspectText(w io.Writer, insp inspector.Inspector, stats inspector.Stats, items []string) {
	// Output is best-effort: a write error to a closed
	// pipe (e.g. when piped to 'head') cannot be
	// recovered, and surfacing it would only clutter
	// the output. Discard the error.
	_, _ = fmt.Fprintf(w, "Path:         %s\n", insp.Path())
	_, _ = fmt.Fprintf(w, "Format:       %s\n", insp.Format())
	if stats.FormatVer != "" {
		_, _ = fmt.Fprintf(w, "Format ver:   %s\n", stats.FormatVer)
	}
	_, _ = fmt.Fprintf(w, "Size:         %s\n", formatBytes(stats.Size))
	if stats.WALSize > 0 {
		_, _ = fmt.Fprintf(w, "WAL size:     %s\n", formatBytes(stats.WALSize))
	}
	if stats.MTime != 0 {
		_, _ = fmt.Fprintf(w, "MTime:        %d\n", stats.MTime)
	}
	_, _ = fmt.Fprintf(w, "Read mode:    %s\n", stats.ReadMode)
	if stats.LockState != "" {
		_, _ = fmt.Fprintf(w, "Lock state:   %s\n", stats.LockState)
	}
	_, _ = fmt.Fprintf(w, "Items:        %s\n", strconv.Itoa(stats.NumItems))
	if len(items) > 0 && len(items) <= 50 {
		_, _ = fmt.Fprintf(w, "Item list:\n")
		for _, name := range items {
			_, _ = fmt.Fprintf(w, "  - %s\n", name)
		}
	} else if len(items) > 50 {
		_, _ = fmt.Fprintf(w, "Item list:    %s ... (%d more)\n", items[0], len(items)-1)
	}
}

// inspectOutput is the JSON shape produced by
// 'peekdb inspect --json'. The field order in this
// struct is the order in the emitted JSON — encoding/json
// marshals struct fields in declaration order, giving us
// stable output without a custom encoder.
type inspectOutput struct {
	Path      string   `json:"Path"`
	Format    string   `json:"Format"`
	FormatVer string   `json:"FormatVer"`
	Size      int64    `json:"Size"`
	WALSize   int64    `json:"WALSize"`
	MTime     int64    `json:"MTime"`
	ReadMode  string   `json:"ReadMode"`
	LockState string   `json:"LockState"`
	NumItems  int      `json:"NumItems"`
	Items     []string `json:"Items"`
}

func writeInspectJSON(w io.Writer, insp inspector.Inspector, stats inspector.Stats, items []string) error {
	out := inspectOutput{
		Path:      insp.Path(),
		Format:    string(insp.Format()),
		FormatVer: stats.FormatVer,
		Size:      stats.Size,
		WALSize:   stats.WALSize,
		MTime:     stats.MTime,
		ReadMode:  stats.ReadMode,
		LockState: stats.LockState,
		NumItems:  stats.NumItems,
		Items:     items,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// formatBytes returns a human-readable byte size (e.g.
// "42 MB"). It is intentionally simple; precise unit
// formatting belongs in the TUI, not in the CLI.
func formatBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * 1024
		gb = 1024 * 1024 * 1024
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// compile-time guard: keep detect imported so adding a
// new format only requires updating the inspector
// registry, not the CLI.
var _ = detect.FormatUnknown

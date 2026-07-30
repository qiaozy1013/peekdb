// Command peekdb is the top-level CLI entrypoint. It
// builds the cobra command tree and dispatches to one
// of the subcommands (inspect / query / version / help),
// or, when no subcommand is given, to the default
// behavior: launch the TUI on the supplied file, or
// print usage if no file is given.
//
// The default build is strictly read-only; this file
// never opens a database in write mode.

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qiaozy1013/peekdb/internal/tui"
)

// globalFlags carries the values of the persistent
// (root-level) flags. Subcommands read from this struct
// via cmd.Root().Flags().Lookup("name").Value.
//
// Persistent flags (available on every subcommand):
//
//	--json, --csv         output format for non-TUI subcommands
//	--no-color            disable ANSI color codes
//	--copy / --no-copy    enable or disable Copy-on-Open
//	--on-lock <mode>      what to do when the file is locked
//	                      (o/c/r/f -> open/copy/readonly/fail)
//	--encoding <enc>      default encoding for cell values
//	--non-interactive     never start a TUI; error out instead
//	--verbose / --quiet   log level knobs
type globalFlags struct {
	JSON           bool
	CSV            bool
	NoColor        bool
	CopyOnOpen     bool
	NoCopy         bool
	OnLock         string
	Encoding       string
	NonInteractive bool
	Verbose        bool
	Quiet          bool
}

// rootFlags is the singleton that every subcommand sees.
// It is populated by cobra before Execute returns.
var rootFlags globalFlags

// rootCmd is the top-level *cobra.Command. Subcommands
// are added by the package-init() side effects imported
// at the top of this file.
var rootCmd = &cobra.Command{
	Use:   "peekdb [<file>]",
	Short: "Read-only TUI/CLI for browsing local database files",
	Long: `peekdb is a read-only TUI/CLI for safely browsing local SQLite,
bbolt, and LevelDB files. The default build never writes to disk
and never opens a database in write mode.

When invoked with a file path, peekdb opens the file and launches
the TUI. With no path it prints this help. See 'peekdb help' for
the full command list.`,
	// : the original MaximumNArgs(1)
	// short-circuited the forbiddenArgNames check (which used
	// to live in runRoot) for any 2-arg invocation — so
	// 'peekdb write foo.db' was rejected by cobra with the
	// generic "accepts at most 1 arg(s)" message, never
	// reaching the deny-list. The user got a USAGE error
	// instead of a SECURITY error. We now run the
	// forbidden-arg check at parse time, so the user
	// always sees "unknown command 'write' for 'peekdb'".
	Args: parseRootArgs,
	// When the user types 'peekdb <file>', we want the
	// default behavior to be "open the file in the TUI"
	// rather than treating the file as a subcommand.
	// Cobra's default behavior for an unknown subcommand
	// is to error out; RunE handles the positional-arg
	// case explicitly.
	RunE: runRoot,
	// SilenceUsage and SilenceErrors prevent cobra from
	// printing the full usage banner on every error; we
	// print a tight, actionable message ourselves.
	SilenceUsage:  true,
	SilenceErrors: true,
}

// parseRootArgs is the cobra Args function for the root command.
// It enforces:
//
//   - at most 1 positional arg (file path)
//   - 1 arg, if it matches the forbidden-arg deny-list, is
//     rejected with cobra's "unknown command" shape (so
//     scripts/check-readonly.go's substring probe still works
//     and the user gets a SECURITY error, not USAGE)
//
// This function runs at parse time, BEFORE subcommand
// dispatch and BEFORE runRoot — so the deny-list always
// wins over any TUI-fallback.
//
// Note: we check the deny-list FIRST so 'peekdb write foo.db'
// produces 'unknown command "write" for "peekdb"' rather
// than 'accepts at most 1 arg(s), received 2'. The latter
// would mask the security error as a usage error.
func parseRootArgs(cmd *cobra.Command, args []string) error {
	if len(args) >= 1 && isForbiddenArg(args[0]) {
		// Mimic cobra's "unknown command" error shape so
		// scripts/check-readonly.go can still detect it
		// with a single substring check.
		return fmt.Errorf("unknown command %q for %q", args[0], cmd.Name())
	}
	if len(args) > 1 {
		return fmt.Errorf("accepts at most 1 arg(s), received %d", len(args))
	}
	return nil
}

func init() {
	// Subcommands are declared as package-level vars in
	// sibling files (inspect.go, query.go, version.go,
	// help.go). Add them here so the user sees them in
	// 'peekdb --help' and 'peekdb help'.
	rootCmd.AddCommand(inspectCmd)
	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(helpCmd)
	// Suppress cobra's auto-generated "help" subcommand
	// (we ship our own in help.go that delegates to
	// root help, so the user gets one consistent entry).
	rootCmd.SetHelpCommand(helpCmd)

	pf := rootCmd.PersistentFlags()
	pf.BoolVar(&rootFlags.JSON, "json", false, "output JSON (where supported)")
	pf.BoolVar(&rootFlags.CSV, "csv", false, "output CSV (where supported)")
	pf.BoolVar(&rootFlags.NoColor, "no-color", false, "disable ANSI color codes")
	pf.BoolVar(&rootFlags.CopyOnOpen, "copy", false, "Copy-on-Open: copy the file to a temp path before opening")
	pf.BoolVar(&rootFlags.NoCopy, "no-copy", false, "explicitly disable Copy-on-Open (default)")
	pf.StringVar(&rootFlags.OnLock, "on-lock", "readonly", "what to do when the file is locked: open|copy|readonly|fail")
	pf.StringVar(&rootFlags.Encoding, "encoding", "utf-8", "default character encoding for cell values (utf-8, utf-16le, utf-16be, gbk, big5, latin1)")
	pf.BoolVar(&rootFlags.NonInteractive, "non-interactive", false, "do not start the TUI; error out if a subcommand needs it")
	pf.BoolVar(&rootFlags.Verbose, "verbose", false, "verbose logging")
	pf.BoolVar(&rootFlags.Quiet, "quiet", false, "suppress non-essential output")
}

// Execute is the entrypoint called from main.go. It
// returns the process exit code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		// We set SilenceErrors on the root command so
		// cobra does not print the full usage banner on
		// every error. Print a tight, actionable line
		// here instead.
		_, _ = fmt.Fprintln(rootCmd.ErrOrStderr(), "Error:", err)
		return 1
	}
	return 0
}

// forbiddenArgNames is the list of positional args that
// look like a write-subcommand name and must be rejected
// before any file path is opened.
//
// The list is checked in rootCmd.Args (parse-time), not in
// runRoot, so the user gets a clear "unknown command" error
// instead of cobra's generic "accepts at most 1 arg(s)"
// message. 
// why; the  added the 5 missing verbs (put, set, add,
// commit, rollback) to the table.
//
// The list is short on purpose: the security check is
// "is this a *write* word?", not "is this a known read
// word?". Everything not on the list is treated as a file
// path for the TUI default.
//
// Keep this map in sync with the regex alternation in
// scripts/check-readonly.go so the deny-list (runtime
// rejection) and the source-grep scan (CI gate) cover the
// same set of verbs. The check-readonly check is the
// authoritative one because the runtime list is what users
// see; the source scan is a safety net for the future.
var forbiddenArgNames = map[string]bool{
	"write":    true,
	"edit":     true,
	"delete":   true,
	"drop":     true,
	"create":   true,
	"insert":   true,
	"update":   true,
	"remove":   true,
	"modify":   true,
	"alter":    true,
	"truncate": true,
	"import":   true,
	"export":   true,
	"dump":     true,
	"load":     true,
	"backup":   true,
	"restore":  true,
	// Added in 
	"put":      true,
	"set":      true,
	"add":      true,
	"commit":   true,
	"rollback": true,
}

// isForbiddenArg reports whether a positional arg looks
// like a write-subcommand name. The check is
// case-insensitive: a user typing "WrItE" or "WRITE" must
// hit the same deny list as "write", otherwise a future
// format-specific inspector could be tricked into
// running with an unexpected name. The map is small
// enough that the case-insensitive lookup (a single
// strings.ToLower) is cheaper than maintaining both
// cases in the table.
func isForbiddenArg(s string) bool {
	return forbiddenArgNames[strings.ToLower(s)]
}

// runRoot handles the no-subcommand case:
//
//   - 0 args            -> print short help, exit 0
//   - 1 arg (a path)    -> launch the TUI
//
// The forbidden-arg check has moved to parseRootArgs
// so the user sees a SECURITY error rather than a USAGE error
// when typing 'peekdb write <anything>'.
func runRoot(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	// Default behavior: open the file and run the TUI.
	return tui.Run(args[0])
}

package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/qiaozy1013/peekdb/internal/version"
)

// versionCmd is the cobra subcommand for 'peekdb version'.
// It prints the build metadata (version, commit, build time,
// build tags) so users can include it in bug reports.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print peekdb build metadata",
	Long: `Print the peekdb version, build commit, build time, and build tags.

Useful for bug reports: include the output of this command so the
maintainer can reproduce the exact binary you are running.`,
	Args: cobra.NoArgs,
	RunE: runVersion,
}

func runVersion(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	// Build mode is derived from the ldflag-injected BuildTags
	// (set by Taskfile's `build-mut` task). An empty BuildTags
	// means the default read-only build. Non-empty (currently
	// only "write") means the write-capable build for v2.0+.
	mode := "read-only build"
	if version.BuildTags != "" {
		mode = fmt.Sprintf("write-capable build (%s)", version.BuildTags)
	}
	_, _ = fmt.Fprintf(out, "peekdb %s (%s)\n", version.Version, mode)
	_, _ = fmt.Fprintf(out, "  Go version:    %s\n", runtime.Version())
	_, _ = fmt.Fprintf(out, "  GOOS/GOARCH:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
	_, _ = fmt.Fprintf(out, "  Commit:        %s\n", version.Commit)
	_, _ = fmt.Fprintf(out, "  Build time:    %s\n", version.BuildTime)
	// Local builds (no ldflag) leave BuildTags empty; render
	// that as "(none)" so the line is human-friendly instead of
	// a 4-space gap.
	tags := version.BuildTags
	if tags == "" {
		tags = "(none)"
	}
	_, _ = fmt.Fprintf(out, "  Build tags:    %s\n", tags)
	return nil
}

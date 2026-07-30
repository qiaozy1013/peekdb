package main

import (
	"github.com/spf13/cobra"
)

// helpCmd prints the full root help. Most users will hit
// --help directly; this command exists for symmetry with
// the other subcommands and for users who type 'peekdb help'.
var helpCmd = &cobra.Command{
	Use:   "help",
	Short: "Print detailed help",
	Long: `Print the full peekdb help, including the available subcommands
and the global flags.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Root().Help()
	},
}

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gqlgoclientgen",
	Short: "GraphQL Go client generator",
	Long: `gqlgoclientgen is a tool for generating Go clients from GraphQL schemas.

It supports introspecting GraphQL endpoints to retrieve schemas and generating
type-safe Go clients based on those schemas.`,
	SilenceErrors: true,
}

// Execute runs the root command and exits with a non-zero code on failure.
func Execute() {
	rootCmd.Version = Version

	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

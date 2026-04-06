package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print yeet version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("yeet %s\n", Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

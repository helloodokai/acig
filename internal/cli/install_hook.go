package cli

import (
	"github.com/spf13/cobra"

	"github.com/helloodokai/acig/internal/githook"
)

var installHookCmd = &cobra.Command{
	Use:   "install-hook",
	Short: "Install the acig pre-push hook in the current git repo",
	RunE: func(cmd *cobra.Command, args []string) error {
		return githook.InstallHook()
	},
}

func init() {
	rootCmd.AddCommand(installHookCmd)
}
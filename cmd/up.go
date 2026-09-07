package cmd

import (
	"github.com/spf13/cobra"
)

func wrapperCmd(sub, short, long string) *cobra.Command {
	return &cobra.Command{
		Use:                sub + " [-- devcontainer args...]",
		Short:              short,
		Long:               long,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWrapped(sub, args)
		},
	}
}

func init() {
	rootCmd.AddCommand(wrapperCmd("up", "Start the dev container with your overrides applied",
		`Renders the merged configuration to a temporary file and runs

  devcontainer up --workspace-folder <ws> --override-config <merged>

The repo's devcontainer.json is never touched. Arguments after -- are passed
through to the devcontainer CLI.

When overlays change the "features" property, --no-lockfile is added by default
so personal Features are not written into the repo's committed
devcontainer-lock.json.`))

	rootCmd.AddCommand(wrapperCmd("build", "Build the dev container image with your overrides applied", ""))
	rootCmd.AddCommand(wrapperCmd("exec", "Run a command in the dev container resolved from the merged config", ""))
}

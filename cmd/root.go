package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var globalOpts struct {
	workspace  string
	configPath string
	profile    string
	userConfig string
	noUser     bool
	features   []string
	mounts     []string
	sets       []string
}

var rootCmd = &cobra.Command{
	Use:   "dcp",
	Short: "Patch committed devcontainer.json files with your personal overrides",
	Long: `devcontainer-patcher layers your personal dev container overrides on top of a
repository's committed .devcontainer/devcontainer.json, without modifying the
file tracked in git.

Layers are applied lowest to highest:

  repo        <workspace>/.devcontainer/devcontainer.json
  user        <config>/local.json
  profile     <config>/profiles/<name>.json      (--profile)
  repo-local  <workspace>/.devcontainer/devcontainer.local.json
  flags       --feature / --mount / --set

Merge semantics follow the dev container spec's own metadata merge logic:
arrays union, lifecycle commands collect, booleans OR, scalars last-wins,
hostRequirements max. Override any of it per path with "$strategy".`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the CLI, propagating the devcontainer CLI's exit code.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitCodeFor(err))
	}
}

func init() {
	f := rootCmd.PersistentFlags()
	f.StringVarP(&globalOpts.workspace, "workspace-folder", "w", "", "workspace folder (default: current directory)")
	f.StringVar(&globalOpts.configPath, "config", "", "path to the repo devcontainer.json (default: auto-discovered)")
	f.StringVarP(&globalOpts.profile, "profile", "p", "", "profile to apply from <config>/profiles/<name>.json")
	f.StringVar(&globalOpts.userConfig, "user-config", "", "path to the user overlay (default: <config>/local.json)")
	f.BoolVar(&globalOpts.noUser, "no-user", false, "skip the user and profile layers")
	f.StringArrayVar(&globalOpts.features, "feature", nil, "add a Feature, e.g. --feature ghcr.io/devcontainers/features/go:1='{\"version\":\"1.27\"}'")
	f.StringArrayVar(&globalOpts.mounts, "mount", nil, "add a mount, in devcontainer.json string form")
	f.StringArrayVar(&globalOpts.sets, "set", nil, "set a dotted path, e.g. --set remoteUser=root")
}

package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/clamoriniere/devcontainer-patcher/internal/layers"
	"github.com/clamoriniere/devcontainer-patcher/internal/runner"
	"github.com/spf13/cobra"
)

// findBase is the discovery used by commands that do not merge layers.
func findBase(ws string) (string, error) {
	return layers.FindBaseConfig(ws)
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show which layers apply here and whether an install is active",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		res, err := resolve()
		if err != nil {
			return err
		}

		fmt.Println("workspace:", res.Workspace)
		fmt.Println("base config:", res.Set.BasePath)

		fmt.Println("\nlayers:")
		for _, l := range res.Set.Layers {
			src := l.Path
			if src == "" {
				src = "(command line)"
			}
			fmt.Printf("  %-16s %s\n", l.Name, src)
		}

		if dir, err := layers.ConfigDir(); err == nil {
			fmt.Printf("\nuser config dir: %s\n", dir)
			fmt.Printf("  user overlay:   %s\n", filepath.Join(dir, layers.UserConfigName))
			fmt.Printf("  profiles:       %s\n", filepath.Join(dir, "profiles"))
		}

		fmt.Println("\ninstall:")
		if res.State == nil {
			fmt.Println("  not installed (use `dcp install`)")
		} else {
			fmt.Printf("  mode:      %s\n", res.State.Mode)
			fmt.Printf("  generated: %s\n", res.State.GeneratedPath)
			fmt.Printf("  installed: %s\n", res.State.InstalledAt)
			if drifted, known := res.Installer.BaseDrift(res.State); known && drifted {
				fmt.Println("  STALE: the committed devcontainer.json changed since install; re-run `dcp install`")
			}
			if len(res.State.SkipWorktree) > 0 {
				fmt.Printf("  skip-worktree: %v\n", res.State.SkipWorktree)
			}
		}

		fmt.Println("\ndevcontainer CLI:")
		if bin, err := runner.Lookup(); err != nil {
			fmt.Println(" ", err)
		} else {
			fmt.Println(" ", bin)
		}

		printWarnings(res.Result)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

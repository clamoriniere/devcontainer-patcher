package cmd

import (
	"fmt"
	"os"

	"github.com/clamoriniere/devcontainer-patcher/internal/install"
	"github.com/spf13/cobra"
)

var installOpts struct {
	mode string
	name string
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Write the merged config into the workspace for your editor to find",
	Long: `Editors start dev containers themselves, so they need the merged configuration
on disk. Which path works depends on the editor:

  --mode profile   (default) writes .devcontainer/<name>/devcontainer.json.
                   VS Code and Codespaces list sub-folder configs in the
                   "Reopen in Container" picker. Nothing tracked is modified.

  --mode in-place  replaces .devcontainer/devcontainer.json, keeping the
                   original in .devcontainer/.dcp/base.json and marking the
                   tracked file skip-worktree so git ignores the change.
                   Required for editors that auto-discover only the two
                   well-known paths and pass no --config, such as Zed.

Either way the generated file is added to .git/info/exclude, not .gitignore,
so the repo's own ignore rules stay untouched. Run "dcp uninstall" to restore.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mode := install.Mode(installOpts.mode)
		if mode != install.ModeProfile && mode != install.ModeInPlace {
			return fmt.Errorf("--mode must be %q or %q", install.ModeProfile, install.ModeInPlace)
		}

		res, err := resolve()
		if err != nil {
			return err
		}
		printWarnings(res.Result)

		state, err := res.Installer.Install(res.Result, mode, installOpts.name)
		if err != nil {
			return err
		}

		fmt.Printf("installed (%s): %s\n", state.Mode, state.GeneratedPath)
		switch state.Mode {
		case install.ModeProfile:
			fmt.Printf("open the workspace and pick the %q configuration when reopening in a container\n", state.ProfileName)
		case install.ModeInPlace:
			fmt.Println("original saved to", state.BaseBackup)
			if len(state.SkipWorktree) > 0 {
				fmt.Fprintln(os.Stderr,
					"note: run `dcp uninstall` before `git pull` — git refuses to update skip-worktree files that changed upstream")
			}
			if state.LockBackup != "" {
				fmt.Println("lockfile protected:", state.LockBackup)
			}
		}
		return nil
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Restore the workspace to its committed configuration",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ws, err := workspaceFolder()
		if err != nil {
			return err
		}
		basePath := globalOpts.configPath
		if basePath == "" {
			if basePath, err = findBase(ws); err != nil {
				return err
			}
		}

		inst := install.New(ws, basePath)
		state, err := inst.Uninstall()
		if err != nil {
			return err
		}
		if state == nil {
			fmt.Println("nothing installed")
			return nil
		}
		fmt.Printf("uninstalled (%s): restored %s\n", state.Mode, state.BasePath)
		return nil
	},
}

func init() {
	installCmd.Flags().StringVar(&installOpts.mode, "mode", string(install.ModeProfile),
		"where to write the merged config: profile or in-place")
	installCmd.Flags().StringVar(&installOpts.name, "name", install.DefaultProfileName,
		"sub-folder name for --mode profile")
	rootCmd.AddCommand(installCmd, uninstallCmd)
}

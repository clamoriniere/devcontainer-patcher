package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/clamoriniere/devcontainer-patcher/internal/jsonx"
	"github.com/spf13/cobra"
)

var renderOpts struct {
	output  string
	explain bool
}

var renderCmd = &cobra.Command{
	Use:   "render",
	Short: "Print the merged devcontainer.json",
	Long: `Render merges every layer and writes the result to stdout, or to a file with
-o. Nothing in the workspace is modified, which makes render the command to
reach for when checking what a change to your overlay actually does.

--explain reports which layer contributed each property instead of the config.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		res, err := resolve()
		if err != nil {
			return err
		}
		printWarnings(res.Result)

		if renderOpts.explain {
			return explain(res)
		}

		data, err := jsonx.Marshal(res.Result.Config)
		if err != nil {
			return err
		}
		if renderOpts.output == "" || renderOpts.output == "-" {
			_, err = os.Stdout.Write(data)
			return err
		}
		return os.WriteFile(renderOpts.output, data, 0o644)
	},
}

func explain(res *resolution) error {
	out := cmdOut()

	fmt.Fprintln(out, "Layers (lowest precedence first):")
	for _, l := range res.Set.Layers {
		src := l.Path
		if src == "" {
			src = "(command line)"
		}
		fmt.Fprintf(out, "  %-16s %s\n", l.Name, src)
	}

	fmt.Fprintln(out, "\nProperties:")
	paths := make([]string, 0, len(res.Result.Provenance))
	for p := range res.Result.Provenance {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// Only report the deepest path touched on each branch: reporting both
	// "features" and "features.ghcr.io/...:1" is noise.
	for i, p := range paths {
		if i+1 < len(paths) && strings.HasPrefix(paths[i+1], p+".") {
			continue
		}
		fmt.Fprintf(out, "  %-50s %s\n", p, strings.Join(res.Result.Provenance[p], " -> "))
	}
	return nil
}

func cmdOut() *os.File { return os.Stdout }

func init() {
	renderCmd.Flags().StringVarP(&renderOpts.output, "output", "o", "", "write to a file instead of stdout")
	renderCmd.Flags().BoolVar(&renderOpts.explain, "explain", false, "show which layer set each property")
	rootCmd.AddCommand(renderCmd)
}

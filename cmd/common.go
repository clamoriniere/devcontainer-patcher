package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/clamoriniere/devcontainer-patcher/internal/install"
	"github.com/clamoriniere/devcontainer-patcher/internal/jsonx"
	"github.com/clamoriniere/devcontainer-patcher/internal/layers"
	"github.com/clamoriniere/devcontainer-patcher/internal/merge"
	"github.com/clamoriniere/devcontainer-patcher/internal/runner"
	"github.com/clamoriniere/devcontainer-patcher/internal/subst"
)

// exitError carries a child process's exit code up to Execute.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func exitCodeFor(err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return 1
}

// resolution is everything a command needs after layers are merged.
type resolution struct {
	Workspace string
	Installer *install.Installer
	State     *install.State
	Set       *layers.Set
	Result    *merge.Result
}

func workspaceFolder() (string, error) {
	ws := globalOpts.workspace
	if ws == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		ws = cwd
	}
	return filepath.Abs(ws)
}

// resolve loads and merges every layer.
//
// When an in-place install is active the file at the well-known path is our
// own generated output, so the pristine copy is used as the base instead.
// Without this, repeated installs would compound overlays onto themselves.
func resolve() (*resolution, error) {
	ws, err := workspaceFolder()
	if err != nil {
		return nil, err
	}

	basePath := globalOpts.configPath
	if basePath == "" {
		basePath, err = layers.FindBaseConfig(ws)
		if err != nil {
			return nil, err
		}
	} else if basePath, err = filepath.Abs(basePath); err != nil {
		return nil, err
	}

	inst := install.New(ws, basePath)
	state, err := inst.LoadState()
	if err != nil {
		return nil, err
	}

	opts := layers.Options{
		WorkspaceFolder: ws,
		ConfigPath:      inst.PristineBase(state),
		Profile:         globalOpts.profile,
		UserConfigPath:  globalOpts.userConfig,
		NoUser:          globalOpts.noUser,
		Features:        globalOpts.features,
		Mounts:          globalOpts.mounts,
		Sets:            globalOpts.sets,
	}

	set, err := layers.Load(opts)
	if err != nil {
		return nil, err
	}
	result, err := merge.Merge(set.Layers)
	if err != nil {
		return nil, err
	}

	if err := expandPlaceholders(result); err != nil {
		return nil, err
	}

	return &resolution{
		Workspace: ws,
		Installer: inst,
		State:     state,
		Set:       set,
		Result:    result,
	}, nil
}

// expandPlaceholders rewrites ${dcp:name} tokens in the merged config, in
// place. The built-in names are remoteUser and containerUser, taken from the
// merged config (the spec has no variable for either); $vars directives from
// any overlay layer add to and override them. Workspace-folder interpolation
// is left to the spec's own ${localWorkspaceFolder} / ...Basename.
//
// Because placeholders resolve after the merge, two mounts that were textually
// distinct at merge time can now share a target; the list is re-settled so the
// last such entry wins, matching the merge's own mount rule.
func expandPlaceholders(result *merge.Result) error {
	vars := subst.Vars{}
	if s, ok := result.Config["remoteUser"].(string); ok {
		vars["remoteuser"] = s
	}
	if s, ok := result.Config["containerUser"].(string); ok {
		vars["containeruser"] = s
	}
	for k, v := range result.Vars {
		vars[strings.ToLower(k)] = v
	}
	if err := subst.Expand(result.Config, vars); err != nil {
		return err
	}
	if ms, ok := result.Config["mounts"].([]any); ok {
		collapsed, warns := merge.CollapseMountsByTarget(ms)
		result.Config["mounts"] = collapsed
		result.Warnings = append(result.Warnings, warns...)
	}
	return nil
}

// printWarnings surfaces merge warnings on stderr so stdout stays pipeable.
func printWarnings(r *merge.Result) {
	for _, w := range r.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
}

// featuresChanged reports whether overlays touched the "features" property.
// When they did, letting the devcontainer CLI write a lockfile would record
// personal Features into the repo's committed devcontainer-lock.json.
func featuresChanged(res *resolution) bool {
	if len(res.Set.Layers) == 0 {
		return false
	}
	base := res.Set.Layers[0].Data["features"]
	return !jsonx.Equal(base, res.Result.Config["features"])
}

// writeTempConfig renders the merged config to a temporary file for
// --override-config. The devcontainer CLI keeps configFilePath pointing at the
// workspace's own .devcontainer, so relative Dockerfile and compose paths still
// resolve correctly from here; no path rewriting is needed.
func writeTempConfig(doc map[string]any) (string, func(), error) {
	dir, err := os.MkdirTemp("", "dcp-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }

	data, err := jsonx.Marshal(doc)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	path := filepath.Join(dir, "devcontainer.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

// runWrapped renders the merged config and hands it to the devcontainer CLI
// via --override-config, forwarding any extra arguments verbatim.
func runWrapped(sub string, passthrough []string) error {
	res, err := resolve()
	if err != nil {
		return err
	}
	printWarnings(res.Result)

	cfg, cleanup, err := writeTempConfig(res.Result.Config)
	if err != nil {
		return err
	}
	defer cleanup()

	args := []string{sub, "--workspace-folder", res.Workspace, "--override-config", cfg}
	if featuresChanged(res) && !hasFlag(passthrough, "--no-lockfile") && !hasFlag(passthrough, "--experimental-lockfile") {
		// Guard by default; --experimental-lockfile opts back in.
		args = append(args, "--no-lockfile")
	}
	args = append(args, passthrough...)

	if err := runner.Run(args); err != nil {
		if errors.Is(err, runner.ErrNotFound) {
			return err
		}
		return &exitError{code: runner.ExitCode(err), err: fmt.Errorf("devcontainer %s failed", sub)}
	}
	return nil
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

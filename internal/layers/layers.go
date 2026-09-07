// Package layers locates the documents that make up a patched configuration.
//
// Precedence, lowest to highest:
//
//  1. repo      <workspace>/.devcontainer/devcontainer.json (or .devcontainer.json)
//  2. user      ~/.config/dcp/local.json
//  3. profile   ~/.config/dcp/profiles/<name>.json
//  4. repo-local <workspace>/.devcontainer/devcontainer.local.json  (git-ignored)
//  5. flags     --feature / --mount / --set
package layers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/clamoriniere/devcontainer-patcher/internal/jsonx"
	"github.com/clamoriniere/devcontainer-patcher/internal/merge"
)

const (
	// AppName is the binary name.
	AppName = "devcontainer-patcher"
	// ConfigDirName is the directory under ~/.config holding user overlays.
	ConfigDirName = "dcp"
	// RepoLocalName is the git-ignored per-project overlay.
	RepoLocalName = "devcontainer.local.json"
	// UserConfigName is the user-wide overlay.
	UserConfigName = "local.json"
)

// Options selects which layers to load.
type Options struct {
	WorkspaceFolder string
	// ConfigPath overrides discovery of the repo's devcontainer.json.
	ConfigPath string
	// Profile names a file under <config>/profiles; empty means none.
	Profile string
	// UserConfigPath overrides discovery of the user overlay.
	UserConfigPath string
	// NoUser skips the user and profile layers, for reproducing CI behaviour.
	NoUser bool
	// Features, Mounts and Sets are the command-line layer.
	Features []string
	Mounts   []string
	Sets     []string
}

// Set is the resolved set of layers plus where the base came from.
type Set struct {
	BasePath string
	Layers   []merge.Layer
}

// ErrNoBaseConfig is returned when the workspace has no devcontainer.json.
var ErrNoBaseConfig = errors.New("no devcontainer.json found")

// WellKnownPaths returns the two locations the reference CLI auto-discovers,
// in the order it checks them (spec-configuration/configurationCommonUtils.ts).
func WellKnownPaths(workspace string) []string {
	return []string{
		filepath.Join(workspace, ".devcontainer", "devcontainer.json"),
		filepath.Join(workspace, ".devcontainer.json"),
	}
}

// FindBaseConfig locates the repo's committed configuration.
func FindBaseConfig(workspace string) (string, error) {
	for _, p := range WellKnownPaths(workspace) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w under %s", ErrNoBaseConfig, workspace)
}

// ConfigDir returns the user configuration directory for the tool: always
// ~/.config/dcp, regardless of platform (os.UserConfigDir diverges on
// Darwin to ~/Library/Application Support, which isn't where users expect
// to find or edit this file).
func ConfigDir() (string, error) {
	if dir := os.Getenv("DCP_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", ConfigDirName), nil
}

// Load resolves and reads every applicable layer.
func Load(opts Options) (*Set, error) {
	basePath := opts.ConfigPath
	if basePath == "" {
		found, err := FindBaseConfig(opts.WorkspaceFolder)
		if err != nil {
			return nil, err
		}
		basePath = found
	}

	baseDoc, err := jsonx.ReadFile(basePath)
	if err != nil {
		return nil, err
	}

	set := &Set{
		BasePath: basePath,
		Layers:   []merge.Layer{{Name: "repo", Path: basePath, Data: baseDoc}},
	}

	if !opts.NoUser {
		userPath := opts.UserConfigPath
		if userPath == "" {
			dir, err := ConfigDir()
			if err != nil {
				return nil, err
			}
			userPath = filepath.Join(dir, UserConfigName)
		}
		if err := set.appendFile("user", userPath, opts.UserConfigPath != ""); err != nil {
			return nil, err
		}

		if opts.Profile != "" {
			dir, err := ConfigDir()
			if err != nil {
				return nil, err
			}
			p := filepath.Join(dir, "profiles", opts.Profile+".json")
			// A profile named explicitly must exist.
			if err := set.appendFile("profile:"+opts.Profile, p, true); err != nil {
				return nil, err
			}
		}
	}

	repoLocal := filepath.Join(filepath.Dir(basePath), RepoLocalName)
	if err := set.appendFile("repo-local", repoLocal, false); err != nil {
		return nil, err
	}

	flagLayer, err := buildFlagLayer(opts)
	if err != nil {
		return nil, err
	}
	if flagLayer != nil {
		set.Layers = append(set.Layers, merge.Layer{Name: "flags", Data: flagLayer})
	}

	return set, nil
}

func (s *Set) appendFile(name, path string, required bool) error {
	doc, err := jsonx.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !required {
			return nil
		}
		return err
	}
	s.Layers = append(s.Layers, merge.Layer{Name: name, Path: path, Data: doc})
	return nil
}

// buildFlagLayer turns --feature/--mount/--set into a synthetic overlay.
func buildFlagLayer(opts Options) (map[string]any, error) {
	if len(opts.Features) == 0 && len(opts.Mounts) == 0 && len(opts.Sets) == 0 {
		return nil, nil
	}
	doc := map[string]any{}

	if len(opts.Features) > 0 {
		features := map[string]any{}
		for _, f := range opts.Features {
			id, rawOpts, hasOpts := strings.Cut(f, "=")
			if !hasOpts {
				features[id] = map[string]any{}
				continue
			}
			parsed, err := jsonx.Parse([]byte(rawOpts))
			if err != nil {
				return nil, fmt.Errorf("--feature %q: options must be a JSON object: %w", f, err)
			}
			features[id] = parsed
		}
		doc["features"] = features
	}

	if len(opts.Mounts) > 0 {
		mounts := make([]any, 0, len(opts.Mounts))
		for _, m := range opts.Mounts {
			mounts = append(mounts, m)
		}
		doc["mounts"] = mounts
	}

	for _, s := range opts.Sets {
		path, raw, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("--set %q: expected path=value", s)
		}
		if err := setPath(doc, strings.Split(path, "."), decodeScalar(raw)); err != nil {
			return nil, fmt.Errorf("--set %q: %w", s, err)
		}
	}

	return doc, nil
}

// decodeScalar parses a --set value as JSON, falling back to a bare string so
// that --set remoteUser=root does not need quoting.
func decodeScalar(raw string) any {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err == nil && !dec.More() {
		return v
	}
	return raw
}

func setPath(doc map[string]any, path []string, value any) error {
	cur := doc
	for i, key := range path {
		if i == len(path)-1 {
			cur[key] = value
			return nil
		}
		next, ok := cur[key]
		if !ok {
			child := map[string]any{}
			cur[key] = child
			cur = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("%s is not an object", strings.Join(path[:i+1], "."))
		}
		cur = child
	}
	return nil
}

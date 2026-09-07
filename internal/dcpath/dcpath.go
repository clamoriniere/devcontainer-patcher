// Package dcpath rewrites the location-sensitive properties of a
// devcontainer.json when the generated config lives in a different directory
// than the original.
//
// The reference CLI resolves build.dockerfile, build.context and
// dockerComposeFile against the *directory of the config file*
// (spec-configuration/configuration.ts: resolveConfigFilePath uses
// parentURI(configFilePath)). Note that `devcontainer up --override-config`
// does NOT need this: it keeps configFilePath pointing at the workspace's own
// .devcontainer while reading content from the override file
// (spec-node/configContainer.ts). Only configs written into a different
// directory and loaded via --config, such as an installed profile, do.
package dcpath

import (
	"path/filepath"
	"strings"
)

// relativeKeys are the properties resolved against the config file's directory.
var relativeKeys = [][]string{
	{"build", "dockerfile"},
	{"build", "context"},
	{"dockerFile"}, // deprecated spelling, still accepted by the CLI
	{"context"},
	{"dockerComposeFile"},
}

// Rewrite adjusts relative paths in doc so they still resolve after the config
// moves from fromDir to toDir. Absolute paths and values containing a
// ${variable} substitution are left untouched.
func Rewrite(doc map[string]any, fromDir, toDir string) {
	if fromDir == toDir {
		return
	}
	for _, key := range relativeKeys {
		rewriteAt(doc, key, fromDir, toDir)
	}
}

func rewriteAt(doc map[string]any, key []string, fromDir, toDir string) {
	parent := doc
	for _, k := range key[:len(key)-1] {
		child, ok := parent[k].(map[string]any)
		if !ok {
			return
		}
		parent = child
	}

	leaf := key[len(key)-1]
	switch v := parent[leaf].(type) {
	case string:
		parent[leaf] = rebase(v, fromDir, toDir)
	case []any:
		// dockerComposeFile accepts an array of files.
		out := make([]any, len(v))
		for i, item := range v {
			if s, ok := item.(string); ok {
				out[i] = rebase(s, fromDir, toDir)
			} else {
				out[i] = item
			}
		}
		parent[leaf] = out
	}
}

// isAbs reports whether p is an absolute path. devcontainer.json paths are
// POSIX-style regardless of host platform, so a leading slash counts as
// absolute even on Windows, where filepath.IsAbs would reject it. The
// filepath.IsAbs fallback still catches Windows drive-letter forms.
func isAbs(p string) bool {
	return strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || filepath.IsAbs(p)
}

func rebase(p, fromDir, toDir string) string {
	if p == "" || isAbs(p) || strings.Contains(p, "${") {
		return p
	}
	rel, err := filepath.Rel(toDir, filepath.Join(fromDir, p))
	if err != nil {
		return p
	}
	// devcontainer.json paths are POSIX-style regardless of host platform.
	return filepath.ToSlash(rel)
}

// Package subst expands ${dcp:name} placeholders in a merged devcontainer.json.
//
// The syntax mirrors the dev container spec's own namespaced variables
// (${localEnv:VAR:default}) but lives in the "dcp:" namespace, so it never
// collides with a spec variable. dcp resolves and removes every ${dcp:...}
// before the config is written or handed to the devcontainer CLI; plain
// ${...} tokens are left untouched for the CLI to resolve.
//
// A placeholder is ${dcp:name} or ${dcp:name:fallback}. The fallback is used
// verbatim when name has no value. A placeholder with neither a value nor a
// fallback is an error.
package subst

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Vars is the placeholder lookup table. Keys are canonical lowercase.
type Vars map[string]string

// placeholderRE matches ${dcp:name} with an optional ":" default that runs to
// the closing brace (and so may not contain '}').
var placeholderRE = regexp.MustCompile(`\$\{dcp:([A-Za-z_][A-Za-z0-9_]*)(?::([^}]*))?\}`)

// Expand walks every string in cfg and rewrites ${dcp:name} tokens in place. It
// returns an error naming every placeholder that had no value and no default,
// each with the JSON path where it appeared.
func Expand(cfg map[string]any, vars Vars) error {
	e := &expander{vars: vars}
	e.walk("", cfg)
	if len(e.missing) > 0 {
		return fmt.Errorf("unresolved placeholder(s): %s", strings.Join(e.missing, "; "))
	}
	return nil
}

type expander struct {
	vars    Vars
	missing []string
}

func (e *expander) walk(path string, v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t[k] = e.walk(child(path, k), t[k])
		}
		return t
	case []any:
		for i, item := range t {
			t[i] = e.walk(fmt.Sprintf("%s[%d]", path, i), item)
		}
		return t
	case string:
		return e.expandString(path, t)
	default:
		return v
	}
}

func (e *expander) expandString(path, s string) string {
	if !strings.Contains(s, "${dcp:") {
		return s
	}
	return placeholderRE.ReplaceAllStringFunc(s, func(tok string) string {
		m := placeholderRE.FindStringSubmatch(tok)
		name := m[1]
		if v, ok := e.vars[strings.ToLower(name)]; ok {
			return v
		}
		// The name can't contain ':', and "${dcp:" contributes exactly one, so
		// a second ':' in the token means a (possibly empty) default was given.
		if strings.Count(tok, ":") >= 2 {
			return m[2]
		}
		e.missing = append(e.missing, fmt.Sprintf("${dcp:%s} (at %s)", name, path))
		return tok
	})
}

func child(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

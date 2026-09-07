// Package merge layers partial devcontainer.json documents on top of a repo's
// committed configuration.
//
// The merge semantics deliberately mirror the dev container spec's own metadata
// merge logic (arrays union, lifecycle commands collect, scalars last-wins,
// booleans OR, hostRequirements max) so that the result is unsurprising to
// anyone who already knows how Features merge into a devcontainer.json. Each
// overlay is "considered last", exactly as the spec says of devcontainer.json
// relative to image metadata.
package merge

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/clamoriniere/devcontainer-patcher/internal/jsonx"
)

// directiveStrategy is the key an overlay uses to override the default rule for
// a given dotted path, e.g. {"$strategy": {"postCreateCommand": "replace"}}.
const directiveStrategy = "$strategy"

// directiveVars is the key an overlay uses to define custom %%name%%
// placeholders, e.g. {"$vars": {"cacheDir": "%%localWorkspaceFolder%%/.cache"}}.
// The placeholders themselves are expanded after the merge, in package subst.
const directiveVars = "$vars"

// varNameRE is the identifier shape allowed for a $vars key.
var varNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Layer is one input document in the merge.
type Layer struct {
	Name string // provenance label, e.g. "user" or "profile:go"
	Path string // source file, empty for synthesized layers
	Data map[string]any
}

// Result is a merged configuration plus the bookkeeping needed to explain it.
type Result struct {
	Config map[string]any
	// Provenance maps a dotted path to the layers that contributed to it, in
	// application order.
	Provenance map[string][]string
	Warnings   []string
	Layers     []Layer
	// Vars collects the $vars directives from every layer, later layers
	// overriding earlier ones. Keys are kept as written; consumers lowercase.
	Vars map[string]string
}

// Merge folds layers left to right. The first layer is the base (normally the
// repo's committed devcontainer.json); every later layer overrides it.
func Merge(layers []Layer) (*Result, error) {
	res := &Result{
		Config:     map[string]any{},
		Provenance: map[string][]string{},
		Layers:     layers,
		Vars:       map[string]string{},
	}

	for _, layer := range layers {
		if layer.Data == nil {
			continue
		}
		strategies, vars, data, err := extractDirectives(layer.Data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", layer.Name, err)
		}
		for k, v := range vars {
			res.Vars[k] = v
		}
		m := &merger{res: res, layer: layer.Name, strategies: strategies}
		merged := m.mergeObject("", res.Config, data)
		res.Config = merged
	}

	return res, nil
}

// extractDirectives splits $-prefixed control keys out of an overlay so they
// never reach the generated config.
func extractDirectives(doc map[string]any) (map[string]Rule, map[string]string, map[string]any, error) {
	strategies := map[string]Rule{}
	vars := map[string]string{}
	out := make(map[string]any, len(doc))

	for k, v := range doc {
		if !strings.HasPrefix(k, "$") {
			out[k] = v
			continue
		}
		switch k {
		case directiveStrategy:
			raw, ok := v.(map[string]any)
			if !ok {
				return nil, nil, nil, fmt.Errorf("%s must be an object of path -> strategy", directiveStrategy)
			}
			for path, s := range raw {
				name, ok := s.(string)
				if !ok {
					return nil, nil, nil, fmt.Errorf("%s[%q] must be a string", directiveStrategy, path)
				}
				rule, err := parseRule(name)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("%s[%q]: %w", directiveStrategy, path, err)
				}
				strategies[path] = rule
			}
		case directiveVars:
			raw, ok := v.(map[string]any)
			if !ok {
				return nil, nil, nil, fmt.Errorf("%s must be an object of name -> string", directiveVars)
			}
			for name, s := range raw {
				if !varNameRE.MatchString(name) {
					return nil, nil, nil, fmt.Errorf("%s[%q]: name must match %s", directiveVars, name, varNameRE)
				}
				str, ok := s.(string)
				if !ok {
					return nil, nil, nil, fmt.Errorf("%s[%q] must be a string", directiveVars, name)
				}
				vars[name] = str
			}
		default:
			// $schema and friends are metadata; ignore rather than fail.
		}
	}
	return strategies, vars, out, nil
}

func parseRule(name string) (Rule, error) {
	switch Rule(name) {
	case RuleReplace, RuleAppend, RulePrepend, RuleUnion, RuleRemove, RuleDeepMerge:
		return Rule(name), nil
	default:
		return "", fmt.Errorf("unknown strategy %q (want replace, append, prepend, union, remove or merge)", name)
	}
}

type merger struct {
	res        *Result
	layer      string
	strategies map[string]Rule
}

func (m *merger) warn(format string, args ...any) {
	m.res.Warnings = append(m.res.Warnings, fmt.Sprintf(format, args...))
}

func (m *merger) record(path string) {
	if path == "" {
		return
	}
	cur := m.res.Provenance[path]
	if len(cur) > 0 && cur[len(cur)-1] == m.layer {
		return
	}
	m.res.Provenance[path] = append(cur, m.layer)
}

// recordTree credits a layer for every path inside a value it introduced
// wholesale. Without it, a nested key later overridden by another layer would
// look as if only that later layer ever set it.
func (m *merger) recordTree(path string, v any) {
	m.record(path)
	obj, ok := v.(map[string]any)
	if !ok {
		return
	}
	for k, sub := range obj {
		child := k
		if path != "" {
			child = path + "." + k
		}
		m.recordTree(child, sub)
	}
}

func (m *merger) rule(path string) Rule {
	if r, ok := m.strategies[path]; ok {
		return r
	}
	return ruleFor(path)
}

func (m *merger) mergeObject(prefix string, base, overlay map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}

	// Deterministic iteration keeps warnings and provenance stable.
	keys := make([]string, 0, len(overlay))
	for k := range overlay {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		ov := overlay[k]

		// A null overlay value deletes the key, following the JSON Merge
		// Patch convention that people already expect from that syntax.
		if ov == nil {
			if _, existed := out[k]; existed {
				delete(out, k)
				m.record(path)
			}
			continue
		}

		existing, had := out[k]
		if !had {
			out[k] = jsonx.Clone(ov)
			m.recordTree(path, ov)
			continue
		}
		merged := m.mergeValue(path, existing, ov)
		if _, drop := merged.(deleted); drop {
			delete(out, k)
		} else {
			out[k] = merged
		}
		m.record(path)
	}
	return out
}

func (m *merger) mergeValue(path string, base, overlay any) any {
	switch m.rule(path) {
	case RuleReplace:
		return jsonx.Clone(overlay)

	case RuleOr:
		bb, ok1 := base.(bool)
		ob, ok2 := overlay.(bool)
		if !ok1 || !ok2 {
			return jsonx.Clone(overlay)
		}
		return bb || ob

	case RuleUnion:
		return m.mergeArray(path, base, overlay, true, false)

	case RuleAppend:
		return m.mergeArray(path, base, overlay, false, false)

	case RulePrepend:
		return m.mergeArray(path, base, overlay, false, true)

	case RuleRemove:
		return m.removeValues(path, base, overlay)

	case RuleMounts:
		return m.mergeMounts(path, base, overlay)

	case RuleCommands:
		return m.mergeCommands(path, base, overlay)

	case RuleMax:
		return m.mergeMax(path, base, overlay)

	case RuleDeepMerge:
		bo, ok1 := base.(map[string]any)
		oo, ok2 := overlay.(map[string]any)
		if !ok1 || !ok2 {
			return jsonx.Clone(overlay)
		}
		return m.mergeObject(path, bo, oo)

	default: // RuleAuto
		bo, ok1 := base.(map[string]any)
		oo, ok2 := overlay.(map[string]any)
		if ok1 && ok2 {
			return m.mergeObject(path, bo, oo)
		}
		if _, isArr1 := base.([]any); isArr1 {
			if _, isArr2 := overlay.([]any); isArr2 {
				return m.mergeArray(path, base, overlay, false, false)
			}
		}
		if typeName(base) != typeName(overlay) {
			m.warn("%s: %s replaces %s value with %s value", path, m.layer, typeName(base), typeName(overlay))
		}
		return jsonx.Clone(overlay)
	}
}

func (m *merger) mergeArray(path string, base, overlay any, dedup, prepend bool) any {
	ba, ok1 := toArray(base)
	oa, ok2 := toArray(overlay)
	if !ok1 || !ok2 {
		m.warn("%s: cannot combine %s with %s, %s value wins", path, typeName(base), typeName(overlay), m.layer)
		return jsonx.Clone(overlay)
	}

	first, second := ba, oa
	if prepend {
		first, second = oa, ba
	}

	out := make([]any, 0, len(first)+len(second))
	out = append(out, jsonx.Clone(first).([]any)...)
	for _, v := range second {
		if dedup && containsValue(out, v) {
			continue
		}
		out = append(out, jsonx.Clone(v))
	}
	return out
}

// deleted is the sentinel mergeValue returns when a rule resolves to
// "drop this key entirely" rather than to a JSON value.
type deleted struct{}

// removeValues subtracts the overlay's entries from a base array. Against a
// non-array base there is nothing to subtract, so the key is dropped instead.
func (m *merger) removeValues(path string, base, overlay any) any {
	ba, ok := toArray(base)
	if !ok {
		return deleted{}
	}
	oa, ok := toArray(overlay)
	if !ok {
		oa = []any{overlay}
	}
	out := make([]any, 0, len(ba))
	for _, v := range ba {
		if containsValue(oa, v) {
			continue
		}
		out = append(out, jsonx.Clone(v))
	}
	if len(out) == len(ba) {
		m.warn("%s: %s strategy \"remove\" matched nothing", path, m.layer)
	}
	return out
}

// mergeMounts implements the spec's "collected list of all mountpoints,
// conflicts: last source wins" by keying on the mount target.
func (m *merger) mergeMounts(path string, base, overlay any) any {
	ba, ok1 := toArray(base)
	oa, ok2 := toArray(overlay)
	if !ok1 || !ok2 {
		m.warn("%s: expected arrays, %s value wins", path, m.layer)
		return jsonx.Clone(overlay)
	}

	out := jsonx.Clone(ba).([]any)
	for _, mount := range oa {
		target := mountTarget(mount)
		replaced := false
		if target != "" {
			for i, existing := range out {
				if mountTarget(existing) == target {
					out[i] = jsonx.Clone(mount)
					replaced = true
					break
				}
			}
		}
		if !replaced && !containsValue(out, mount) {
			out = append(out, jsonx.Clone(mount))
		}
	}
	return out
}

// mountTarget extracts the container-side path from either mount form:
// the string form "source=...,target=...,type=bind" or the object form.
func mountTarget(v any) string {
	switch t := v.(type) {
	case string:
		for _, part := range strings.Split(t, ",") {
			k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				continue
			}
			switch k {
			case "target", "dst", "destination":
				return val
			}
		}
	case map[string]any:
		for _, k := range []string{"target", "dst", "destination"} {
			if s, ok := t[k].(string); ok {
				return s
			}
		}
	}
	return ""
}

// mergeCommands folds lifecycle commands into the object form, which is the
// only representation a single devcontainer.json field has for "run both".
//
// Note the semantic shift this implies: string and array forms run
// sequentially, whereas object-form entries run in parallel. We warn so the
// user can reach for $strategy "replace" when that matters.
func (m *merger) mergeCommands(path string, base, overlay any) any {
	baseObj, baseIsObj := base.(map[string]any)
	overObj, overIsObj := overlay.(map[string]any)

	out := map[string]any{}
	if baseIsObj {
		for k, v := range baseObj {
			out[k] = jsonx.Clone(v)
		}
	} else {
		out[commandKey(out, "repo")] = jsonx.Clone(base)
	}

	if overIsObj {
		for k, v := range overObj {
			out[k] = jsonx.Clone(v)
		}
	} else {
		out[commandKey(out, m.layer)] = jsonx.Clone(overlay)
	}

	if !baseIsObj || !overIsObj {
		m.warn("%s: combined into object form so both commands run; object-form entries run in parallel "+
			"(use \"$strategy\": {%q: \"replace\"} to override)", path, path)
	}
	return out
}

// commandKey derives a stable, unique key for a command contributed by a layer.
func commandKey(existing map[string]any, layer string) string {
	base := strings.NewReplacer(":", "-", " ", "-", "/", "-").Replace(layer)
	if base == "" {
		base = "layer"
	}
	key := base
	for i := 2; ; i++ {
		if _, taken := existing[key]; !taken {
			return key
		}
		key = fmt.Sprintf("%s-%d", base, i)
	}
}

// mergeMax implements hostRequirements' "max value wins", recursing into the
// object so cpus/memory/storage are compared individually.
func (m *merger) mergeMax(path string, base, overlay any) any {
	bo, ok1 := base.(map[string]any)
	oo, ok2 := overlay.(map[string]any)
	if ok1 && ok2 {
		out := make(map[string]any, len(bo))
		for k, v := range bo {
			out[k] = jsonx.Clone(v)
		}
		for k, v := range oo {
			sub := path + "." + k
			if existing, had := out[k]; had {
				out[k] = m.mergeMax(sub, existing, v)
			} else {
				out[k] = jsonx.Clone(v)
			}
			m.record(sub)
		}
		return out
	}

	bn, ok1 := toBytes(base)
	on, ok2 := toBytes(overlay)
	if ok1 && ok2 {
		if bn >= on {
			return jsonx.Clone(base)
		}
		return jsonx.Clone(overlay)
	}

	// gpu accepts true | "optional" | object; anything truthy beats false.
	if bb, ok := base.(bool); ok {
		if ob, ok := overlay.(bool); ok {
			return bb || ob
		}
		if bb {
			return true
		}
	}
	return jsonx.Clone(overlay)
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "number"
	}
}

func toArray(v any) ([]any, bool) {
	a, ok := v.([]any)
	return a, ok
}

func containsValue(list []any, v any) bool {
	for _, item := range list {
		if jsonx.Equal(item, v) {
			return true
		}
	}
	return false
}

package merge

import (
	"strings"
)

// Rule describes how an overlay value combines with a base value at a path.
type Rule string

const (
	// RuleAuto picks a rule from the runtime types: objects deep merge,
	// arrays append, everything else is replaced by the overlay.
	RuleAuto Rule = "auto"
	// RuleOr is boolean OR, per the spec's merge logic for init/privileged.
	RuleOr Rule = "or"
	// RuleUnion appends and removes duplicates.
	RuleUnion Rule = "union"
	// RuleAppend appends without deduplicating. Correct for positional
	// arrays such as runArgs, where "-e" legitimately repeats.
	RuleAppend Rule = "append"
	// RulePrepend is RuleAppend with the overlay first.
	RulePrepend Rule = "prepend"
	// RuleReplace discards the base value.
	RuleReplace Rule = "replace"
	// RuleRemove subtracts the overlay's entries from a base array, or
	// deletes the key outright when the overlay value is not an array.
	RuleRemove Rule = "remove"
	// RuleDeepMerge merges objects key by key, overlay winning per key.
	RuleDeepMerge Rule = "merge"
	// RuleMounts appends mounts, replacing any base mount with the same target.
	RuleMounts Rule = "mounts"
	// RuleCommands folds lifecycle commands into the object form so that
	// both the repo's command and ours survive.
	RuleCommands Rule = "commands"
	// RuleMax keeps the larger of two hostRequirements values.
	RuleMax Rule = "max"
)

// lifecycleCommands are the properties the spec merges as a "collected list".
// initializeCommand is not in the spec's table (it is not part of image
// metadata) but behaves the same way from a user's point of view.
var lifecycleCommands = map[string]bool{
	"initializeCommand":    true,
	"onCreateCommand":      true,
	"updateContentCommand": true,
	"postCreateCommand":    true,
	"postStartCommand":     true,
	"postAttachCommand":    true,
}

// topLevelRules mirrors the merge table in the dev container spec
// (containers.dev/implementors/spec/#merge-logic). Paths not listed here fall
// through to RuleAuto, which is what makes unknown and tool-specific
// properties survive instead of being dropped.
var topLevelRules = map[string]Rule{
	"init":                        RuleOr,
	"privileged":                  RuleOr,
	"capAdd":                      RuleUnion,
	"securityOpt":                 RuleUnion,
	"forwardPorts":                RuleUnion,
	"overrideFeatureInstallOrder": RuleUnion,
	"mounts":                      RuleMounts,
	"hostRequirements":            RuleMax,

	// Explicit last-value-wins, per the spec table. RuleAuto would reach the
	// same answer for scalars, but listing them documents the intent.
	"waitFor":              RuleReplace,
	"containerUser":        RuleReplace,
	"remoteUser":           RuleReplace,
	"userEnvProbe":         RuleReplace,
	"overrideCommand":      RuleReplace,
	"shutdownAction":       RuleReplace,
	"updateRemoteUserUID":  RuleReplace,
	"otherPortsAttributes": RuleReplace,
}

// ruleFor returns the default rule for a dotted path.
func ruleFor(path string) Rule {
	if lifecycleCommands[path] {
		return RuleCommands
	}
	if r, ok := topLevelRules[path]; ok {
		return r
	}

	// customizations is "left to the tools" by the spec. Every tool namespace
	// we know of models extensions as an additive list, so union them; the
	// rest (settings, keybindings) deep merges via RuleAuto.
	if strings.HasPrefix(path, "customizations.") && strings.HasSuffix(path, ".extensions") {
		return RuleUnion
	}

	// hostRequirements sub-properties inherit max-wins.
	if strings.HasPrefix(path, "hostRequirements.") {
		return RuleMax
	}

	// portsAttributes merges per port, not per attribute: a port present in
	// the overlay replaces the base's entry for that port wholesale.
	if strings.HasPrefix(path, "portsAttributes.") && strings.Count(path, ".") == 1 {
		return RuleReplace
	}

	return RuleAuto
}

package merge

import (
	"testing"

	"github.com/clamoriniere/devcontainer-patcher/internal/jsonx"
	"github.com/google/go-cmp/cmp"
)

// mergeDocs is a helper that merges two JSONC snippets as repo + user layers.
func mergeDocs(t *testing.T, base, overlay string) *Result {
	t.Helper()

	baseDoc, err := jsonx.Parse([]byte(base))
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	overlayDoc, err := jsonx.Parse([]byte(overlay))
	if err != nil {
		t.Fatalf("parse overlay: %v", err)
	}

	res, err := Merge([]Layer{
		{Name: "repo", Data: baseDoc},
		{Name: "user", Data: overlayDoc},
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	return res
}

func want(t *testing.T, doc string) map[string]any {
	t.Helper()
	v, err := jsonx.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse expectation: %v", err)
	}
	return v
}

func TestFeaturesDeepMerge(t *testing.T) {
	// A Feature present in both layers keeps the repo's options and lets the
	// overlay win per key; a new Feature is simply added.
	res := mergeDocs(t,
		`{"features": {
			"ghcr.io/devcontainers/features/go:1": {"version": "1.27", "golangciLintVersion": "latest"},
			"ghcr.io/devcontainers/features/common-utils:2": {"installZsh": true}
		}}`,
		`{"features": {
			"ghcr.io/devcontainers/features/go:1": {"version": "1.28"},
			"ghcr.io/devcontainers/features/fish:1": {}
		}}`)

	expected := want(t, `{"features": {
		"ghcr.io/devcontainers/features/go:1": {"version": "1.28", "golangciLintVersion": "latest"},
		"ghcr.io/devcontainers/features/common-utils:2": {"installZsh": true},
		"ghcr.io/devcontainers/features/fish:1": {}
	}}`)

	if diff := cmp.Diff(expected, res.Config); diff != "" {
		t.Errorf("unexpected merge (-want +got):\n%s", diff)
	}
}

func TestUnknownPropertiesSurvive(t *testing.T) {
	// Tool-specific keys the spec does not define (Zed's use_podman, say) must
	// pass through untouched rather than being dropped by a typed schema.
	res := mergeDocs(t,
		`{"name": "proj", "use_podman": true, "podman_path": "podman"}`,
		`{"name": "proj-local"}`)

	expected := want(t, `{"name": "proj-local", "use_podman": true, "podman_path": "podman"}`)
	if diff := cmp.Diff(expected, res.Config); diff != "" {
		t.Errorf("unexpected merge (-want +got):\n%s", diff)
	}
}

func TestArrayRules(t *testing.T) {
	res := mergeDocs(t,
		`{
			"forwardPorts": [3000, 8080],
			"capAdd": ["SYS_PTRACE"],
			"runArgs": ["-e", "FOO=1"],
			"customizations": {"vscode": {"extensions": ["golang.go", "eamodio.gitlens"]}}
		}`,
		`{
			"forwardPorts": [8080, 5432],
			"capAdd": ["SYS_PTRACE", "NET_ADMIN"],
			"runArgs": ["-e", "BAR=2"],
			"customizations": {"vscode": {"extensions": ["eamodio.gitlens", "vscodevim.vim"]}}
		}`)

	// forwardPorts and capAdd union; extensions union; runArgs appends
	// without dedup because "-e" is positional and must repeat.
	expected := want(t, `{
		"forwardPorts": [3000, 8080, 5432],
		"capAdd": ["SYS_PTRACE", "NET_ADMIN"],
		"runArgs": ["-e", "FOO=1", "-e", "BAR=2"],
		"customizations": {"vscode": {"extensions": ["golang.go", "eamodio.gitlens", "vscodevim.vim"]}}
	}`)

	if diff := cmp.Diff(expected, res.Config); diff != "" {
		t.Errorf("unexpected merge (-want +got):\n%s", diff)
	}
}

func TestMountsReplaceByTarget(t *testing.T) {
	res := mergeDocs(t,
		`{"mounts": [
			"source=cache,target=/var/cache,type=volume",
			{"source": "/host/a", "target": "/a", "type": "bind"}
		]}`,
		`{"mounts": [
			"source=${localEnv:HOME}/.cache,target=/var/cache,type=bind",
			"source=${localEnv:HOME}/.zsh_history,target=/home/vscode/.zsh_history,type=bind"
		]}`)

	// /var/cache exists in both, so the overlay's mount replaces it in place;
	// the new target is appended.
	expected := want(t, `{"mounts": [
		"source=${localEnv:HOME}/.cache,target=/var/cache,type=bind",
		{"source": "/host/a", "target": "/a", "type": "bind"},
		"source=${localEnv:HOME}/.zsh_history,target=/home/vscode/.zsh_history,type=bind"
	]}`)

	if diff := cmp.Diff(expected, res.Config); diff != "" {
		t.Errorf("unexpected merge (-want +got):\n%s", diff)
	}
}

func TestLifecycleCommandsCollect(t *testing.T) {
	res := mergeDocs(t,
		`{"postCreateCommand": "go mod download"}`,
		`{"postCreateCommand": "fish -c 'echo hi'"}`)

	expected := want(t, `{"postCreateCommand": {
		"repo": "go mod download",
		"user": "fish -c 'echo hi'"
	}}`)

	if diff := cmp.Diff(expected, res.Config); diff != "" {
		t.Errorf("unexpected merge (-want +got):\n%s", diff)
	}
	if len(res.Warnings) == 0 {
		t.Error("expected a warning about the parallel object form")
	}
}

func TestLifecycleObjectFormMergesByKey(t *testing.T) {
	// Object form on both sides merges by key with no warning, which is the
	// clean way for an overlay to add a step.
	res := mergeDocs(t,
		`{"postCreateCommand": {"deps": "go mod download"}}`,
		`{"postCreateCommand": {"dotfiles": "~/dotfiles/install.sh"}}`)

	expected := want(t, `{"postCreateCommand": {
		"deps": "go mod download",
		"dotfiles": "~/dotfiles/install.sh"
	}}`)

	if diff := cmp.Diff(expected, res.Config); diff != "" {
		t.Errorf("unexpected merge (-want +got):\n%s", diff)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Warnings)
	}
}

func TestBooleansOrAndHostRequirementsMax(t *testing.T) {
	res := mergeDocs(t,
		`{"init": false, "privileged": true, "hostRequirements": {"cpus": 4, "memory": "8gb", "storage": "32gb"}}`,
		`{"init": true, "privileged": false, "hostRequirements": {"cpus": 2, "memory": "16gb"}}`)

	expected := want(t, `{
		"init": true,
		"privileged": true,
		"hostRequirements": {"cpus": 4, "memory": "16gb", "storage": "32gb"}
	}`)

	if diff := cmp.Diff(expected, res.Config); diff != "" {
		t.Errorf("unexpected merge (-want +got):\n%s", diff)
	}
}

func TestStrategyOverrides(t *testing.T) {
	res := mergeDocs(t,
		`{
			"postCreateCommand": "make setup",
			"forwardPorts": [3000, 8080, 9229],
			"customizations": {"vscode": {"extensions": ["golang.go", "ms-python.python"]}}
		}`,
		`{
			"$strategy": {
				"postCreateCommand": "replace",
				"forwardPorts": "remove",
				"customizations.vscode.extensions": "prepend"
			},
			"postCreateCommand": "make setup-local",
			"forwardPorts": [9229],
			"customizations": {"vscode": {"extensions": ["vscodevim.vim"]}}
		}`)

	expected := want(t, `{
		"postCreateCommand": "make setup-local",
		"forwardPorts": [3000, 8080],
		"customizations": {"vscode": {"extensions": ["vscodevim.vim", "golang.go", "ms-python.python"]}}
	}`)

	if diff := cmp.Diff(expected, res.Config); diff != "" {
		t.Errorf("unexpected merge (-want +got):\n%s", diff)
	}
	if _, leaked := res.Config["$strategy"]; leaked {
		t.Error("$strategy leaked into the generated config")
	}
}

func TestNullDeletesKey(t *testing.T) {
	res := mergeDocs(t,
		`{"remoteUser": "vscode", "postStartCommand": "echo hi"}`,
		`{"postStartCommand": null}`)

	expected := want(t, `{"remoteUser": "vscode"}`)
	if diff := cmp.Diff(expected, res.Config); diff != "" {
		t.Errorf("unexpected merge (-want +got):\n%s", diff)
	}
}

func TestProvenanceTracksLayers(t *testing.T) {
	res := mergeDocs(t,
		`{"name": "proj", "remoteUser": "vscode"}`,
		`{"remoteUser": "root", "containerUser": "root"}`)

	if got := res.Provenance["remoteUser"]; len(got) != 2 || got[0] != "repo" || got[1] != "user" {
		t.Errorf("remoteUser provenance = %v, want [repo user]", got)
	}
	if got := res.Provenance["containerUser"]; len(got) != 1 || got[0] != "user" {
		t.Errorf("containerUser provenance = %v, want [user]", got)
	}
}

func TestJSONCCommentsAndTrailingCommas(t *testing.T) {
	res := mergeDocs(t,
		`{
			// the repo's own comment
			"name": "proj",
			"forwardPorts": [3000,],
		}`,
		`{"forwardPorts": [8080]}`)

	expected := want(t, `{"name": "proj", "forwardPorts": [3000, 8080]}`)
	if diff := cmp.Diff(expected, res.Config); diff != "" {
		t.Errorf("unexpected merge (-want +got):\n%s", diff)
	}
}

func TestUnknownStrategyIsAnError(t *testing.T) {
	doc, err := jsonx.Parse([]byte(`{"$strategy": {"mounts": "clobber"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Merge([]Layer{{Name: "repo", Data: map[string]any{}}, {Name: "user", Data: doc}}); err == nil {
		t.Fatal("expected an error for an unknown strategy")
	}
}

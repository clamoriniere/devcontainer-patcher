package subst

import (
	"strings"
	"testing"

	"github.com/clamoriniere/devcontainer-patcher/internal/jsonx"
	"github.com/google/go-cmp/cmp"
)

func expand(t *testing.T, doc string, vars Vars) map[string]any {
	t.Helper()
	cfg, err := jsonx.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := Expand(cfg, vars); err != nil {
		t.Fatalf("expand: %v", err)
	}
	return cfg
}

func want(t *testing.T, doc string) map[string]any {
	t.Helper()
	v, err := jsonx.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse expectation: %v", err)
	}
	return v
}

func TestExpandMountPlaceholder(t *testing.T) {
	got := expand(t, `{
		"mounts": ["source=${localEnv:HOME}/.claude,target=/home/${dcp:remoteUser}/.claude,type=bind"]
	}`, Vars{"remoteuser": "vscode"})

	exp := want(t, `{
		"mounts": ["source=${localEnv:HOME}/.claude,target=/home/vscode/.claude,type=bind"]
	}`)
	if diff := cmp.Diff(exp, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestExpandInlineDefaultUsedWhenMissing(t *testing.T) {
	got := expand(t, `{"remoteUser": "${dcp:remoteUser:vscode}"}`, Vars{})
	if got["remoteUser"] != "vscode" {
		t.Errorf("got %q, want %q", got["remoteUser"], "vscode")
	}
}

func TestExpandInlineDefaultIgnoredWhenPresent(t *testing.T) {
	got := expand(t, `{"remoteUser": "${dcp:remoteUser:vscode}"}`, Vars{"remoteuser": "node"})
	if got["remoteUser"] != "node" {
		t.Errorf("got %q, want %q", got["remoteUser"], "node")
	}
}

func TestExpandEmptyDefault(t *testing.T) {
	got := expand(t, `{"name": "prefix-${dcp:suffix:}"}`, Vars{})
	if got["name"] != "prefix-" {
		t.Errorf("got %q, want %q", got["name"], "prefix-")
	}
}

func TestExpandDefaultMayContainColon(t *testing.T) {
	got := expand(t, `{"s": "${dcp:url:https://example.test}"}`, Vars{})
	if got["s"] != "https://example.test" {
		t.Errorf("got %q, want %q", got["s"], "https://example.test")
	}
}

func TestExpandUnresolvedIsError(t *testing.T) {
	cfg, err := jsonx.Parse([]byte(`{
		"mounts": ["target=/home/${dcp:remoteUser}/.claude"]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	err = Expand(cfg, Vars{})
	if err == nil {
		t.Fatal("expected an error for the unresolved placeholder")
	}
	for _, sub := range []string{"${dcp:remoteUser}", "mounts[0]"} {
		if !strings.Contains(err.Error(), sub) {
			t.Errorf("error %q missing %q", err.Error(), sub)
		}
	}
}

func TestExpandReportsEveryUnresolved(t *testing.T) {
	cfg := want(t, `{"a": "${dcp:x}", "b": "${dcp:y}"}`)
	err := Expand(cfg, Vars{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "${dcp:x}") || !strings.Contains(err.Error(), "${dcp:y}") {
		t.Errorf("error should name both placeholders: %v", err)
	}
}

func TestExpandLeavesSpecVariablesUntouched(t *testing.T) {
	doc := `{"mounts": ["source=${localEnv:HOME}/x,target=${containerWorkspaceFolder}/x"]}`
	got := expand(t, doc, Vars{})
	if diff := cmp.Diff(want(t, doc), got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestExpandNested(t *testing.T) {
	got := expand(t, `{
		"customizations": {"vscode": {"settings": {"path": "/w/${dcp:project}"}}},
		"runArgs": ["--name", "${dcp:project}-dev"]
	}`, Vars{"project": "dcp"})

	exp := want(t, `{
		"customizations": {"vscode": {"settings": {"path": "/w/dcp"}}},
		"runArgs": ["--name", "dcp-dev"]
	}`)
	if diff := cmp.Diff(exp, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestExpandCaseInsensitive(t *testing.T) {
	got := expand(t, `{"a": "${dcp:remoteuser}", "b": "${dcp:remoteUser}", "c": "${dcp:REMOTEUSER}"}`,
		Vars{"remoteuser": "vscode"})
	for _, k := range []string{"a", "b", "c"} {
		if got[k] != "vscode" {
			t.Errorf("%s = %q, want vscode", k, got[k])
		}
	}
}

func TestExpandMultipleInOneString(t *testing.T) {
	got := expand(t, `{"s": "${dcp:a}/${dcp:b}/${dcp:a}"}`, Vars{"a": "1", "b": "2"})
	if got["s"] != "1/2/1" {
		t.Errorf("got %q, want %q", got["s"], "1/2/1")
	}
}

func TestExpandNoPlaceholdersIsNoOp(t *testing.T) {
	doc := `{"name": "plain", "n": 3, "ok": true}`
	got := expand(t, doc, Vars{"a": "1"})
	if diff := cmp.Diff(want(t, doc), got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

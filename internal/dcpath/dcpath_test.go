package dcpath

import (
	"testing"

	"github.com/clamoriniere/devcontainer-patcher/internal/jsonx"
	"github.com/google/go-cmp/cmp"
)

func TestRewriteIntoSubfolder(t *testing.T) {
	doc, err := jsonx.Parse([]byte(`{
		"build": {"dockerfile": "Dockerfile", "context": ".."},
		"dockerComposeFile": ["docker-compose.yml", "../compose.override.yml"],
		"image": "mcr.microsoft.com/devcontainers/base:bookworm"
	}`))
	if err != nil {
		t.Fatal(err)
	}

	// An installed profile lives one directory deeper than the original.
	Rewrite(doc, "/repo/.devcontainer", "/repo/.devcontainer/local")

	expected, _ := jsonx.Parse([]byte(`{
		"build": {"dockerfile": "../Dockerfile", "context": "../.."},
		"dockerComposeFile": ["../docker-compose.yml", "../../compose.override.yml"],
		"image": "mcr.microsoft.com/devcontainers/base:bookworm"
	}`))

	if diff := cmp.Diff(expected, doc); diff != "" {
		t.Errorf("unexpected rewrite (-want +got):\n%s", diff)
	}
}

func TestRewriteLeavesAbsoluteAndVariablePaths(t *testing.T) {
	doc, err := jsonx.Parse([]byte(`{
		"build": {"dockerfile": "/abs/Dockerfile"},
		"dockerComposeFile": "${localWorkspaceFolder}/compose.yml"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	before := jsonx.Clone(doc)

	Rewrite(doc, "/repo/.devcontainer", "/repo/.devcontainer/local")

	if diff := cmp.Diff(before, any(doc)); diff != "" {
		t.Errorf("absolute and ${} paths must not be rewritten (-want +got):\n%s", diff)
	}
}

func TestRewriteIsNoOpForSameDirectory(t *testing.T) {
	doc, err := jsonx.Parse([]byte(`{"build": {"dockerfile": "Dockerfile"}}`))
	if err != nil {
		t.Fatal(err)
	}
	Rewrite(doc, "/repo/.devcontainer", "/repo/.devcontainer")

	if got := doc["build"].(map[string]any)["dockerfile"]; got != "Dockerfile" {
		t.Errorf("dockerfile = %v, want Dockerfile", got)
	}
}

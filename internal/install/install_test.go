package install

import (
	"path/filepath"
	"testing"

	"github.com/clamoriniere/devcontainer-patcher/internal/merge"
)

func TestLabelProfile(t *testing.T) {
	tests := []struct {
		name     string
		docName  any
		prov     []string
		profile  string
		repo     string
		wantName any
	}{
		{
			name:     "base name present keeps its label with suffix",
			docName:  "proj",
			prov:     []string{"repo"},
			profile:  "local",
			repo:     "myrepo",
			wantName: "proj (local)",
		},
		{
			name:     "no base name falls back to repo name",
			docName:  nil,
			prov:     nil,
			profile:  "local",
			repo:     "myrepo",
			wantName: "myrepo (local)",
		},
		{
			name:     "empty base name falls back to repo name",
			docName:  "",
			prov:     []string{"repo"},
			profile:  "local",
			repo:     "myrepo",
			wantName: "myrepo (local)",
		},
		{
			name:     "no base name and no repo name yields bare profile",
			docName:  nil,
			prov:     nil,
			profile:  "local",
			repo:     "",
			wantName: "local",
		},
		{
			name:     "overlay-set name is left untouched",
			docName:  "custom",
			prov:     []string{"repo", "user"},
			profile:  "local",
			repo:     "myrepo",
			wantName: "custom",
		},
		{
			name:     "non-default profile flows into the suffix",
			docName:  "proj",
			prov:     []string{"repo"},
			profile:  "perf",
			repo:     "myrepo",
			wantName: "proj (perf)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := map[string]any{}
			if tt.docName != nil {
				doc["name"] = tt.docName
			}
			res := &merge.Result{Provenance: map[string][]string{}}
			if tt.prov != nil {
				res.Provenance["name"] = tt.prov
			}

			labelProfile(doc, res, tt.profile, tt.repo)

			if got := doc["name"]; got != tt.wantName {
				t.Errorf("name = %#v, want %#v", got, tt.wantName)
			}
		})
	}
}

func TestRepoName(t *testing.T) {
	if got := repoName(filepath.Join("home", "user", "devcontainer-patcher")); got != "devcontainer-patcher" {
		t.Errorf("repoName = %q, want %q", got, "devcontainer-patcher")
	}
	if got := repoName("."); got != "" {
		t.Errorf("repoName(\".\") = %q, want empty", got)
	}
	if got := repoName(string(filepath.Separator)); got != "" {
		t.Errorf("repoName(root) = %q, want empty", got)
	}
}

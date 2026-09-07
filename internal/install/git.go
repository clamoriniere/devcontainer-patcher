package install

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// git is a thin helper around the git CLI, scoped to one workspace.
type git struct {
	workspace string
	available bool
}

func newGit(workspace string) *git {
	g := &git{workspace: workspace}
	if _, err := exec.LookPath("git"); err != nil {
		return g
	}
	if _, err := g.run("rev-parse", "--git-dir"); err != nil {
		return g
	}
	g.available = true
	return g
}

func (g *git) run(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", g.workspace}, args...)...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(out.String()), err
	}
	return strings.TrimSpace(out.String()), nil
}

// tracked reports whether path is under version control.
func (g *git) tracked(rel string) bool {
	if !g.available {
		return false
	}
	_, err := g.run("ls-files", "--error-unmatch", "--", rel)
	return err == nil
}

// dirty reports whether a tracked path has uncommitted changes. Paths marked
// skip-worktree read as clean, which is exactly what we want: an already
// installed file must not block a reinstall.
func (g *git) dirty(rel string) bool {
	if !g.available {
		return false
	}
	out, err := g.run("status", "--porcelain", "--", rel)
	return err == nil && out != ""
}

func (g *git) setSkipWorktree(rel string, skip bool) error {
	if !g.available {
		return nil
	}
	flag := "--skip-worktree"
	if !skip {
		flag = "--no-skip-worktree"
	}
	_, err := g.run("update-index", flag, "--", rel)
	return err
}

// headBlob returns the committed content of rel at HEAD.
func (g *git) headBlob(rel string) ([]byte, bool) {
	if !g.available {
		return nil, false
	}
	cmd := exec.Command("git", "-C", g.workspace, "show", "HEAD:"+filepath.ToSlash(rel))
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, false
	}
	return out.Bytes(), true
}

// exclude adds a pattern to .git/info/exclude, the per-clone ignore file. We
// use it rather than .gitignore so installing never dirties a tracked file.
func (g *git) exclude(patterns ...string) error {
	if !g.available {
		return nil
	}
	gitDir, err := g.run("rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil
	}
	path := filepath.Join(gitDir, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	existing, _ := os.ReadFile(path)
	lines := map[string]bool{}
	for _, l := range strings.Split(string(existing), "\n") {
		lines[strings.TrimSpace(l)] = true
	}

	var add []string
	for _, p := range patterns {
		if !lines[p] {
			add = append(add, p)
		}
	}
	if len(add) == 0 {
		return nil
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	var buf bytes.Buffer
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		buf.WriteString("\n")
	}
	buf.WriteString("\n# added by devcontainer-patcher\n")
	for _, p := range add {
		buf.WriteString(p + "\n")
	}
	_, err = f.Write(buf.Bytes())
	return err
}

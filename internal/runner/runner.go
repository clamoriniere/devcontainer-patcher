// Package runner shells out to the upstream devcontainer CLI.
package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// Binary is the devcontainer CLI executable, overridable for testing.
func Binary() string {
	if b := os.Getenv("DCP_DEVCONTAINER_BIN"); b != "" {
		return b
	}
	return "devcontainer"
}

// ErrNotFound reports a missing devcontainer CLI.
var ErrNotFound = errors.New("devcontainer CLI not found in PATH")

// Lookup resolves the devcontainer binary.
func Lookup() (string, error) {
	path, err := exec.LookPath(Binary())
	if err != nil {
		return "", fmt.Errorf("%w (install it with: npm install -g @devcontainers/cli)", ErrNotFound)
	}
	return path, nil
}

// Run executes the devcontainer CLI with stdio wired to the caller's.
func Run(args []string) error {
	bin, err := Lookup()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ExitCode extracts the child's exit status so we can propagate it.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}

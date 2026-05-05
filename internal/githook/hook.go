package githook

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed pre_push.tmpl
var prePushScript string

func InstallHook() error {
	gitDir, err := findGitDir()
	if err != nil {
		return fmt.Errorf("finding git directory: %w", err)
	}

	hookPath := filepath.Join(gitDir, "hooks", "pre-push")
	if _, err := os.Stat(hookPath); err == nil {
		existing, err := os.ReadFile(hookPath)
		if err != nil {
			return fmt.Errorf("reading existing hook: %w", err)
		}
		if strings.Contains(string(existing), "acig") {
			fmt.Println("acig pre-push hook already installed, updating...")
		} else {
			return fmt.Errorf("existing pre-push hook found at %s; remove it or integrate acig manually", hookPath)
		}
	}

	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("creating hooks directory: %w", err)
	}

	if err := os.WriteFile(hookPath, []byte(prePushScript), 0o755); err != nil {
		return fmt.Errorf("writing pre-push hook: %w", err)
	}

	fmt.Printf("Installed pre-push hook at %s\n", hookPath)
	return nil
}

func findGitDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		gitDir := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitDir); err == nil {
			if info.IsDir() {
				return gitDir, nil
			}
			// .git is a file (worktree), read it
			content, err := os.ReadFile(gitDir)
			if err != nil {
				return "", err
			}
			// Format: gitdir: /path/to/gitdir
			parts := strings.SplitN(string(content), ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not in a git repository")
		}
		dir = parent
	}
}
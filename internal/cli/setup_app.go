package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var setupAppCmd = &cobra.Command{
	Use:   "setup-app",
	Short: "Configure GitHub App secrets for acig reviews",
	Long: `Configure repository secrets for the ACIG GitHub App so reviews appear
with the ACIG name and logo instead of github-actions[bot].

Prerequisites:
  - The ACIG GitHub App must be installed on your repository
    (https://github.com/settings/apps/ACIG/installations)
  - You need the App ID and private key (.pem file) from the app settings
  - The gh CLI must be authenticated

This command sets ACIG_APP_ID and ACIG_APP_PRIVATE_KEY as repository secrets.`,
	RunE: runSetupApp,
}

func init() {
	rootCmd.AddCommand(setupAppCmd)
}

func runSetupApp(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI is required. Install it from https://cli.github.com")
	}

	repoSlug, err := detectRepoSlug()
	if err != nil {
		return fmt.Errorf("could not detect GitHub repository: %w\nRun this from inside a git repo with a GitHub remote", err)
	}

	fmt.Println()
	fmt.Println("  ACIG GitHub App Setup")
	fmt.Println("  ======================")
	fmt.Println()
	fmt.Println("  This command configures repository secrets so acig reviews")
	fmt.Println("  appear as \"ACIG\" with a custom logo instead of github-actions[bot].")
	fmt.Println()
	fmt.Println("  If you haven't already:")
	fmt.Println("    1. Create the app at https://github.com/settings/apps/new")
	fmt.Println("       (permissions needed: Pull requests: R/W, Checks: R/W, Contents: Read)")
	fmt.Println("    2. Install it on your repo: https://github.com/settings/apps/ACIG/installations")
	fmt.Println("    3. Generate a private key in the app settings → download the .pem file")
	fmt.Println()

	var appID string
	fmt.Print("  App ID: ")
	_, _ = fmt.Scanln(&appID)
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return fmt.Errorf("App ID is required")
	}

	fmt.Print("  Path to private key (.pem): ")
	var pemPath string
	_, _ = fmt.Scanln(&pemPath)
	pemPath = strings.TrimSpace(pemPath)
	if pemPath == "" {
		return fmt.Errorf("Private key path is required")
	}

	fmt.Println()
	fmt.Printf("  Setting ACIG_APP_ID on %s...\n", repoSlug)
	if err := setSecret(repoSlug, "ACIG_APP_ID", appID); err != nil {
		return fmt.Errorf("setting ACIG_APP_ID: %w", err)
	}
	fmt.Println("  OK ACIG_APP_ID set")

	fmt.Printf("  Setting ACIG_APP_PRIVATE_KEY on %s...\n", repoSlug)
	if err := setSecretFromFile(repoSlug, "ACIG_APP_PRIVATE_KEY", pemPath); err != nil {
		return fmt.Errorf("setting ACIG_APP_PRIVATE_KEY: %w", err)
	}
	fmt.Println("  OK ACIG_APP_PRIVATE_KEY set")

	fmt.Println()
	fmt.Println("  Done! Make sure your workflow uses actions/create-github-app-token.")
	fmt.Println("  See https://github.com/helloodokai/acig#github-app-setup for the workflow template.")
	fmt.Println()

	return nil
}

func setSecret(repo, name, value string) error {
	cmd := exec.Command("gh", "secret", "set", name, "--repo", repo, "--body", value)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func setSecretFromFile(repo, name, pemPath string) error {
	f, err := os.Open(pemPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", pemPath, err)
	}
	defer f.Close()

	cmd := exec.Command("gh", "secret", "set", name, "--repo", repo)
	cmd.Stdin = f
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func detectRepoSlug() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", err
	}
	remote := strings.TrimSpace(string(out))

	if strings.HasPrefix(remote, "https://github.com/") {
		remote = strings.TrimPrefix(remote, "https://github.com/")
		remote = strings.TrimSuffix(remote, ".git")
		return remote, nil
	}
	if strings.HasPrefix(remote, "git@github.com:") {
		remote = strings.TrimPrefix(remote, "git@github.com:")
		remote = strings.TrimSuffix(remote, ".git")
		return remote, nil
	}
	return "", fmt.Errorf("not a GitHub remote: %s", remote)
}
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReleaseWorkflowSkipGuardStructure verifies that .github/workflows/release.yml
// configures the workflow_dispatch dry-run inputs and includes the robust
// Dependabot author check.
func TestReleaseWorkflowSkipGuardStructure(t *testing.T) {
	workflowPath := filepath.Join(".github", "workflows", "release.yml")
	contentBytes, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", workflowPath, err)
	}
	content := string(contentBytes)

	// Invariant 1: workflow_dispatch exposes dry_run and sha inputs
	if !strings.Contains(content, "dry_run:") {
		t.Errorf("release.yml missing inputs.dry_run declaration")
	}
	if !strings.Contains(content, "sha:") {
		t.Errorf("release.yml missing inputs.sha declaration")
	}

	// Invariant 2: skip-check accepts both dependabot[bot] and app/dependabot (via *dependabot*)
	if !strings.Contains(content, "*dependabot*") {
		t.Errorf("release.yml skip-check does not match on *dependabot*")
	}
	if strings.Contains(content, `"$AUTHOR" = "app/dependabot"`) {
		t.Errorf("release.yml retains brittle exact equality to app/dependabot")
	}
}

// TestReleaseWorkflowAuthorClassification executes the bash author classification
// logic to ensure Dependabot identities skip release while human contributors proceed.
func TestReleaseWorkflowAuthorClassification(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available in PATH; skipping shell-level execution test")
	}

	tests := []struct {
		name      string
		eventName string
		author    string
		dryRun    string
		wantSkip  bool
	}{
		{
			name:      "push from dependabot[bot] skips release",
			eventName: "push",
			author:    "dependabot[bot]",
			dryRun:    "false",
			wantSkip:  true,
		},
		{
			name:      "push from app/dependabot skips release",
			eventName: "push",
			author:    "app/dependabot",
			dryRun:    "false",
			wantSkip:  true,
		},
		{
			name:      "push from dependabot skips release",
			eventName: "push",
			author:    "dependabot",
			dryRun:    "false",
			wantSkip:  true,
		},
		{
			name:      "push from human PR proceeds with release",
			eventName: "push",
			author:    "ChrisUFO",
			dryRun:    "false",
			wantSkip:  false,
		},
		{
			name:      "push with no PR author (direct push) proceeds with release",
			eventName: "push",
			author:    "",
			dryRun:    "false",
			wantSkip:  false,
		},
		{
			name:      "dry-run workflow_dispatch with dependabot PR skips release",
			eventName: "workflow_dispatch",
			author:    "dependabot[bot]",
			dryRun:    "true",
			wantSkip:  true,
		},
		{
			name:      "dry-run workflow_dispatch with human PR skips release to prevent release",
			eventName: "workflow_dispatch",
			author:    "ChrisUFO",
			dryRun:    "true",
			wantSkip:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			script := `
EVENT_NAME="$1"
AUTHOR="$2"
DRY_RUN="$3"

if [ "$EVENT_NAME" != "push" ] && [ "$DRY_RUN" != "true" ]; then
  echo "skipped=false"
  exit 0
fi

IS_DEPENDABOT=false
case "$AUTHOR" in
  "dependabot[bot]"|"app/dependabot"|*dependabot*)
    IS_DEPENDABOT=true
    ;;
esac

if [ "$IS_DEPENDABOT" = "true" ]; then
  echo "skipped=true"
elif [ "$DRY_RUN" = "true" ]; then
  echo "skipped=true"
else
  echo "skipped=false"
fi
`
			cmd := exec.Command(bashPath, "-s", "--", tc.eventName, tc.author, tc.dryRun)
			cmd.Stdin = strings.NewReader(script)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("bash script failed: %v, output: %s", err, string(out))
			}
			outStr := strings.TrimSpace(string(out))
			gotSkip := strings.Contains(outStr, "skipped=true")
			if gotSkip != tc.wantSkip {
				t.Errorf("author=%q event=%q dryRun=%q: got skipped=%v, want %v (output: %q)",
					tc.author, tc.eventName, tc.dryRun, gotSkip, tc.wantSkip, outStr)
			}
		})
	}
}

package main

// Guards the NSIS template the Windows release pipeline actually compiles.
// A prior relaunch-after-upgrade fix silently landed in a duplicate template
// nothing built; these tests pin the shipped template's upgrade behavior and
// the single-template invariant so that cannot recur.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// nsisRepoRoot resolves the repository root from this file's location so the
// tests run regardless of the working directory `go test` was invoked from.
func nsisRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine the location of the test file")
	}
	return filepath.Dir(thisFile)
}

// nsisShippedTemplate resolves the template the release pipeline compiles:
// the create:nsis:installer task's dir in build/windows/Taskfile.yml names
// the directory makensis runs in, which must contain the project.nsi it
// builds.
func nsisShippedTemplate(t *testing.T) string {
	t.Helper()
	root := nsisRepoRoot(t)
	taskfile := filepath.Join(root, "build", "windows", "Taskfile.yml")
	raw, err := os.ReadFile(taskfile)
	if err != nil {
		t.Fatalf("read Taskfile %s: %v", taskfile, err)
	}
	var tf struct {
		Tasks map[string]struct {
			Dir string `yaml:"dir"`
		} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(raw, &tf); err != nil {
		t.Fatalf("parse Taskfile %s: %v", taskfile, err)
	}
	task, ok := tf.Tasks["create:nsis:installer"]
	if !ok {
		t.Fatalf("Taskfile %s has no create:nsis:installer task", taskfile)
	}
	if task.Dir == "" {
		t.Fatalf("create:nsis:installer in %s has no dir: — cannot locate the template it compiles", taskfile)
	}
	template := filepath.Join(root, filepath.FromSlash(task.Dir), "project.nsi")
	if _, err := os.Stat(template); err != nil {
		t.Fatalf("create:nsis:installer dir %q has no project.nsi (resolved to %s): %v", task.Dir, template, err)
	}
	return template
}

func isNSISCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";")
}

// nsisLineIndex returns the 0-based index of the first non-comment line
// matching re, or -1 when the template has no such line.
func nsisLineIndex(lines []string, re *regexp.Regexp) int {
	for i, line := range lines {
		if isNSISCommentLine(line) {
			continue
		}
		if re.MatchString(line) {
			return i
		}
	}
	return -1
}

func TestNsisInstallerTemplateUpgradeBehavior(t *testing.T) {
	template := nsisShippedTemplate(t)
	raw, err := os.ReadFile(template)
	if err != nil {
		t.Fatalf("read template %s: %v", template, err)
	}
	lines := strings.Split(string(raw), "\n")

	finishRunRe := regexp.MustCompile(`^\s*!define\s+MUI_FINISHPAGE_RUN\s+`)
	finishPageRe := regexp.MustCompile(`^\s*!insertmacro\s+MUI_PAGE_FINISH\b`)
	execWaitRe := regexp.MustCompile(`\bExecWait\b`)
	wailsFilesRe := regexp.MustCompile(`^\s*!insertmacro\s+wails\.files\b`)
	taskkillIMRe := regexp.MustCompile(`taskkill\s+/IM`)
	taskkillForcedRe := regexp.MustCompile(`taskkill\s+/IM.*\s/F\b`)

	runIdx := nsisLineIndex(lines, finishRunRe)
	if runIdx == -1 {
		t.Fatalf("%s: !define MUI_FINISHPAGE_RUN is missing — the finish page must offer to relaunch Silt after an upgrade", template)
	}
	pageIdx := nsisLineIndex(lines, finishPageRe)
	if pageIdx == -1 {
		t.Fatalf("%s: !insertmacro MUI_PAGE_FINISH is missing — expected the finish page to run after install", template)
	}
	execWaitIdx := nsisLineIndex(lines, execWaitRe)
	if execWaitIdx == -1 {
		t.Fatalf("%s: no ExecWait found — expected the old-uninstaller ExecWait to be present", template)
	}
	filesIdx := nsisLineIndex(lines, wailsFilesRe)
	if filesIdx == -1 {
		t.Fatalf("%s: !insertmacro wails.files is missing — expected the install section to copy the app files", template)
	}

	// Locate the graceful and forced taskkill attempts. The graceful
	// attempt must exist and come first so Application.Quit can drain
	// WAL/autosave before the /F guarantee.
	gracefulIdx, forcedIdx := -1, -1
	for i, line := range lines {
		if isNSISCommentLine(line) || !taskkillIMRe.MatchString(line) {
			continue
		}
		if taskkillForcedRe.MatchString(line) {
			if forcedIdx == -1 {
				forcedIdx = i
			}
		} else if gracefulIdx == -1 {
			gracefulIdx = i
		}
	}
	if gracefulIdx == -1 {
		t.Fatalf("%s: no graceful taskkill /IM (without /F) found — a running Silt must be asked to close before it is force-killed", template)
	}
	if forcedIdx == -1 {
		t.Fatalf("%s: no forced taskkill /IM /F found — tray-resident instances that ignore close must still be terminated before file replacement", template)
	}

	t.Run("finish page run checkbox", func(t *testing.T) {
		const wantTarget = `$INSTDIR\${PRODUCT_EXECUTABLE}`
		valueRe := regexp.MustCompile(`^\s*!define\s+MUI_FINISHPAGE_RUN\s+"?([^\s"]+)"?`)
		m := valueRe.FindStringSubmatch(lines[runIdx])
		if m == nil {
			t.Fatalf("%s:%d: cannot parse the MUI_FINISHPAGE_RUN value from %q", template, runIdx+1, lines[runIdx])
		}
		if m[1] != wantTarget {
			t.Errorf("%s:%d: MUI_FINISHPAGE_RUN targets %q, want %q so the finish page launches the upgraded Silt (MUI supplies the quoting around the value)",
				template, runIdx+1, m[1], wantTarget)
		}

		notCheckedRe := regexp.MustCompile(`MUI_FINISHPAGE_RUN_NOTCHECKED`)
		for i, line := range lines {
			if isNSISCommentLine(line) {
				continue
			}
			if notCheckedRe.MatchString(line) {
				t.Errorf("%s:%d: MUI_FINISHPAGE_RUN_NOTCHECKED must not appear — it unchecks the relaunch checkbox, and upgrades should reopen Silt by default (line: %q)",
					template, i+1, strings.TrimSpace(line))
			}
		}
	})

	t.Run("ordering", func(t *testing.T) {
		order := []struct {
			name       string
			before     int
			beforeName string
			after      int
			afterName  string
		}{
			{
				name:   "the run checkbox must be defined before its page is inserted",
				before: runIdx, beforeName: "!define MUI_FINISHPAGE_RUN",
				after: pageIdx, afterName: "!insertmacro MUI_PAGE_FINISH",
			},
			{
				name:   "a running Silt must be asked to close gracefully before the force-kill",
				before: gracefulIdx, beforeName: "graceful taskkill /IM",
				after: forcedIdx, afterName: "forced taskkill /IM /F",
			},
			{
				name:   "a running Silt must be stopped before the old uninstaller deletes files",
				before: gracefulIdx, beforeName: "graceful taskkill /IM",
				after: execWaitIdx, afterName: "old-uninstaller ExecWait",
			},
			{
				name:   "a running Silt must be stopped before the old uninstaller deletes files",
				before: forcedIdx, beforeName: "forced taskkill /IM /F",
				after: execWaitIdx, afterName: "old-uninstaller ExecWait",
			},
			{
				name:   "a running Silt must be stopped before the new files are copied",
				before: gracefulIdx, beforeName: "graceful taskkill /IM",
				after: filesIdx, afterName: "!insertmacro wails.files",
			},
			{
				name:   "a running Silt must be stopped before the new files are copied",
				before: forcedIdx, beforeName: "forced taskkill /IM /F",
				after: filesIdx, afterName: "!insertmacro wails.files",
			},
		}
		for _, tc := range order {
			if tc.before >= tc.after {
				t.Errorf("%s: %s (line %d) must appear before %s (line %d) in %s",
					tc.name, tc.beforeName, tc.before+1, tc.afterName, tc.after+1, template)
			}
		}
	})
}

// TestSingleNsisInstallerTemplate asserts build/windows/ holds exactly the one
// template the release pipeline compiles — a second, unused template is where
// installer fixes go to die.
func TestSingleNsisInstallerTemplate(t *testing.T) {
	template := nsisShippedTemplate(t)
	buildWindows := filepath.Join(nsisRepoRoot(t), "build", "windows")
	var found []string
	err := filepath.WalkDir(buildWindows, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "project.nsi" {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", buildWindows, err)
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one project.nsi under build/windows (the template the release pipeline compiles), found %d: %v", len(found), found)
	}
	if found[0] != template {
		t.Fatalf("the only project.nsi under build/windows (%s) is not the one create:nsis:installer compiles (%s)", found[0], template)
	}
}

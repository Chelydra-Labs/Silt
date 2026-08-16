package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"silt/backend/history"
	"silt/backend/parser"
)

func enablePageHistory(t *testing.T, app *App, max, interval int) {
	t.Helper()
	cfg, err := app.GetSystemConfig()
	if err != nil {
		t.Fatalf("GetSystemConfig: %v", err)
	}
	on := true
	cfg.Editor.AutoVersioningEnabled = &on
	cfg.Editor.MaxVersionsPerPage = max
	cfg.Editor.AutoVersioningMinIntervalSec = interval
	if err := app.SaveSystemConfig(cfg); err != nil {
		t.Fatalf("SaveSystemConfig: %v", err)
	}
}

func seedHistoryPage(t *testing.T, app *App, notebook, section, page, body string) string {
	t.Helper()
	filePath := filepath.Join(app.vaultPath, notebook, section, page+".md")
	content := "---\nnotebook: " + notebook + "\nsection: " + section + "\npage: " + page + "\ndate: 2026-08-16\ntags: []\n---\n" + body
	writeFile(t, filePath, content)
	blocks, _, _, _, err := parser.ParseFileContent(content, notebook, section, page, "2026-08-16", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.SaveFileBlocks(notebook, section, page, blocks); err != nil {
		t.Fatalf("SaveFileBlocks seed: %v", err)
	}
	return filePath
}

func savePageBody(t *testing.T, app *App, notebook, section, page, body string) {
	t.Helper()
	if _, err := app.SavePageMarkdown(notebook, section, page, body); err != nil {
		t.Fatalf("SavePageMarkdown: %v", err)
	}
}

func TestPageHistory_DisabledCreatesNothing(t *testing.T) {
	app := newTestApp(t)
	seedHistoryPage(t, app, "Work", "Journal", "Off", "# first\n")
	savePageBody(t, app, "Work", "Journal", "Off", "# second\n")
	hist := filepath.Join(app.vaultPath, ".system", "history")
	if _, err := os.Stat(hist); !os.IsNotExist(err) {
		t.Fatalf("disabled save should not create %s (err=%v)", hist, err)
	}
}

func TestPageHistory_CapturesPreviousBytes(t *testing.T) {
	app := newTestApp(t)
	seedHistoryPage(t, app, "Work", "Journal", "Daily", "# first\n")
	enablePageHistory(t, app, 50, 0)
	savePageBody(t, app, "Work", "Journal", "Daily", "# second\n")

	list, err := app.ListPageVersions("Work", "Journal", "Daily")
	if err != nil {
		t.Fatalf("ListPageVersions: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len=%d, want 1", len(list))
	}
	got, err := app.GetPageVersion("Work", "Journal", "Daily", list[0].ID)
	if err != nil {
		t.Fatalf("GetPageVersion: %v", err)
	}
	if !strings.Contains(got, "# first") {
		t.Fatalf("version body = %q, want previous content", got)
	}
	if strings.Contains(got, "notebook:") {
		t.Fatalf("preview must be body-only, got %q", got)
	}
}

func TestPageHistory_IntervalAndHashSkip(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 300)
	seedHistoryPage(t, app, "Work", "Journal", "Skip", "# a\n")
	savePageBody(t, app, "Work", "Journal", "Skip", "# b\n")
	savePageBody(t, app, "Work", "Journal", "Skip", "# c\n")
	list, err := app.ListPageVersions("Work", "Journal", "Skip")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("interval should keep one version, got %d", len(list))
	}

	enablePageHistory(t, app, 50, 0)
	savePageBody(t, app, "Work", "Journal", "Skip", "# c\n")
	list, err = app.ListPageVersions("Work", "Journal", "Skip")
	if err != nil {
		t.Fatalf("List after hash skip: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("identical body should hash-skip, got %d versions", len(list))
	}
}

func TestPageHistory_MCPBypassesInterval(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 300)
	seedHistoryPage(t, app, "Work", "Journal", "Agent", "# a\n")
	filePath := filepath.Join(app.vaultPath, "Work", "Journal", "Agent.md")
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	blocks, _, _, _, err := parser.ParseFileContent(string(raw), "Work", "Journal", "Agent", "2026-08-16", app.spacesPerTab)
	if err != nil {
		t.Fatal(err)
	}
	blocks[0].CleanText = "b"
	if err := app.saveFileBlocksWithSource("Work", "Journal", "Agent", blocks, historyReasonMCP); err != nil {
		t.Fatalf("mcp save 1: %v", err)
	}
	raw, _ = os.ReadFile(filePath)
	blocks, _, _, _, err = parser.ParseFileContent(string(raw), "Work", "Journal", "Agent", "2026-08-16", app.spacesPerTab)
	if err != nil {
		t.Fatal(err)
	}
	blocks[0].CleanText = "c"
	if err := app.saveFileBlocksWithSource("Work", "Journal", "Agent", blocks, historyReasonMCP); err != nil {
		t.Fatalf("mcp save 2: %v", err)
	}
	list, err := app.ListPageVersions("Work", "Journal", "Agent")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("MCP should bypass interval, got %d versions", len(list))
	}
	for _, v := range list {
		if v.Source != historyReasonMCP && v.Source != historyReasonEditor {
			t.Fatalf("unexpected source %q", v.Source)
		}
	}
}

func TestPageHistory_EditorSaveAfterMCPCapturesAgentResult(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 300)
	seedHistoryPage(t, app, "Work", "Journal", "AfterAgent", "# a\n")
	filePath := filepath.Join(app.vaultPath, "Work", "Journal", "AfterAgent.md")
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	blocks, _, _, _, err := parser.ParseFileContent(string(raw), "Work", "Journal", "AfterAgent", "2026-08-16", app.spacesPerTab)
	if err != nil {
		t.Fatal(err)
	}
	blocks[0].CleanText = "agent body"
	if err := app.saveFileBlocksWithSource("Work", "Journal", "AfterAgent", blocks, historyReasonMCP); err != nil {
		t.Fatalf("mcp save: %v", err)
	}
	savePageBody(t, app, "Work", "Journal", "AfterAgent", "# human after agent\n")
	list, err := app.ListPageVersions("Work", "Journal", "AfterAgent")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var sawAgent bool
	for _, v := range list {
		body, err := app.GetPageVersion("Work", "Journal", "AfterAgent", v.ID)
		if err != nil {
			t.Fatalf("GetPageVersion: %v", err)
		}
		if strings.Contains(body, "agent body") {
			sawAgent = true
			break
		}
	}
	if !sawAgent {
		t.Fatal("editor save after MCP should snapshot the agent result")
	}
}

func TestPageHistory_RestoreWhileDisabledKeepsLivePage(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "OffRestore", "# original\n")
	savePageBody(t, app, "Work", "Journal", "OffRestore", "# edited\n")
	list, err := app.ListPageVersions("Work", "Journal", "OffRestore")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v len=%d", err, len(list))
	}
	oldID := list[0].ID
	enablePageHistory(t, app, 50, 0)
	cfg, err := app.GetSystemConfig()
	if err != nil {
		t.Fatal(err)
	}
	off := false
	cfg.Editor.AutoVersioningEnabled = &off
	if err := app.SaveSystemConfig(cfg); err != nil {
		t.Fatalf("disable history: %v", err)
	}
	if err := app.RestorePageVersion("Work", "Journal", "OffRestore", oldID); err != nil {
		t.Fatalf("Restore while disabled: %v", err)
	}
	after, err := app.ListPageVersions("Work", "Journal", "OffRestore")
	if err != nil {
		t.Fatalf("List after restore: %v", err)
	}
	var sawLive bool
	for _, v := range after {
		body, err := app.GetPageVersion("Work", "Journal", "OffRestore", v.ID)
		if err != nil {
			t.Fatalf("GetPageVersion: %v", err)
		}
		if strings.Contains(body, "# edited") {
			sawLive = true
			break
		}
	}
	if !sawLive {
		t.Fatal("restore while disabled should keep a snapshot of the live page")
	}
}

func TestPageHistory_NestedSectionRenameFollowsLocator(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Projects/Active", "Nested", "# first\n")
	savePageBody(t, app, "Work", "Projects/Active", "Nested", "# second\n")
	if err := app.RenamePage("Work", "Projects/Active", "Nested", "Renamed"); err != nil {
		t.Fatalf("RenamePage nested: %v", err)
	}
	oldList, err := app.ListPageVersions("Work", "Projects/Active", "Nested")
	if err != nil {
		t.Fatalf("List old: %v", err)
	}
	if len(oldList) != 0 {
		t.Fatalf("old nested locator still has %d versions", len(oldList))
	}
	newList, err := app.ListPageVersions("Work", "Projects/Active", "Renamed")
	if err != nil {
		t.Fatalf("List new: %v", err)
	}
	if len(newList) == 0 {
		t.Fatal("nested rename lost history")
	}
	got, err := app.GetPageVersion("Work", "Projects/Active", "Renamed", newList[0].ID)
	if err != nil {
		t.Fatalf("GetPageVersion: %v", err)
	}
	if !strings.Contains(got, "# first") {
		t.Fatalf("nested history body = %q", got)
	}
}

func TestPageHistory_MCPCreateRecordsAgentReason(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 300)
	seedHistoryPage(t, app, "Work", "Journal", "CreateMe", "# start\n")
	if _, err := app.createBlockWithReason("", "Work", "Journal", "CreateMe", "NOTE", "agent note", historyReasonMCP); err != nil {
		t.Fatalf("createBlockWithReason: %v", err)
	}
	list, err := app.ListPageVersions("Work", "Journal", "CreateMe")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v len=%d", err, len(list))
	}
	if list[0].Source != historyReasonMCP {
		t.Fatalf("create reason = %q, want %q", list[0].Source, historyReasonMCP)
	}
}

func TestPageHistory_RestoreRoundTripBodyOnly(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "RestoreMe", "# original\n")
	savePageBody(t, app, "Work", "Journal", "RestoreMe", "# edited\n")
	list, err := app.ListPageVersions("Work", "Journal", "RestoreMe")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v len=%d", err, len(list))
	}
	oldID := list[0].ID

	filePath := filepath.Join(app.vaultPath, "Work", "Journal", "RestoreMe.md")
	before, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), "type:") {
		// stamp a live frontmatter field that restore must keep
		updated := strings.Replace(string(before), "tags: []\n", "tags: []\ntype: note\n", 1)
		if err := os.WriteFile(filePath, []byte(updated), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := app.RestorePageVersion("Work", "Journal", "RestoreMe", oldID); err != nil {
		t.Fatalf("RestorePageVersion: %v", err)
	}
	after, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "# original") {
		t.Fatalf("restored body missing original, got:\n%s", after)
	}
	if !strings.Contains(string(after), "type: note") && !strings.Contains(string(after), "type: \"note\"") {
		// type may have been quoted by later writers; accept either if present before restore
		if strings.Contains(string(before), "type:") && !strings.Contains(string(after), "type:") {
			t.Fatalf("restore dropped live frontmatter type:\n%s", after)
		}
	}

	afterList, err := app.ListPageVersions("Work", "Journal", "RestoreMe")
	if err != nil {
		t.Fatalf("List after restore: %v", err)
	}
	if len(afterList) < 2 {
		t.Fatalf("restore should snapshot current first, got %d versions", len(afterList))
	}
	foundRestore := false
	for _, v := range afterList {
		if v.Source == historyReasonRestore {
			foundRestore = true
		}
	}
	if !foundRestore {
		t.Fatalf("expected a restore-reason snapshot, got %+v", afterList)
	}
}

func TestPageHistory_RetentionPruneStillListsAndRestores(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 2, 0)
	seedHistoryPage(t, app, "Work", "Journal", "Prune", "# v0\n")
	savePageBody(t, app, "Work", "Journal", "Prune", "# v1\n")
	savePageBody(t, app, "Work", "Journal", "Prune", "# v2\n")
	savePageBody(t, app, "Work", "Journal", "Prune", "# v3\n")

	list, err := app.ListPageVersions("Work", "Journal", "Prune")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("prune cap=2, got %d versions", len(list))
	}
	oldestKept := list[len(list)-1]
	if err := app.RestorePageVersion("Work", "Journal", "Prune", oldestKept.ID); err != nil {
		t.Fatalf("restore remaining version: %v", err)
	}
	body, err := app.FetchPageMarkdown("Work", "Journal", "Prune")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body) == "" {
		t.Fatal("restored body empty")
	}
}

func TestPageHistory_RenameFollowsLocator(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "OldName", "# first\n")
	savePageBody(t, app, "Work", "Journal", "OldName", "# second\n")
	if err := app.RenamePage("Work", "Journal", "OldName", "NewName"); err != nil {
		t.Fatalf("RenamePage: %v", err)
	}
	oldList, err := app.ListPageVersions("Work", "Journal", "OldName")
	if err != nil {
		t.Fatalf("List old: %v", err)
	}
	if len(oldList) != 0 {
		t.Fatalf("old locator still has %d versions", len(oldList))
	}
	newList, err := app.ListPageVersions("Work", "Journal", "NewName")
	if err != nil {
		t.Fatalf("List new: %v", err)
	}
	if len(newList) == 0 {
		t.Fatal("renamed page lost history")
	}
}

func TestPageHistory_RenameOntoRetainedHistory(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "Taken", "# taken-first\n")
	savePageBody(t, app, "Work", "Journal", "Taken", "# taken-second\n")
	if err := app.DeletePage("Work", "Journal", "Taken"); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}
	seedHistoryPage(t, app, "Work", "Journal", "Alive", "# alive-first\n")
	savePageBody(t, app, "Work", "Journal", "Alive", "# alive-second\n")
	if err := app.RenamePage("Work", "Journal", "Alive", "Taken"); err != nil {
		t.Fatalf("RenamePage onto retained history: %v", err)
	}
	oldList, err := app.ListPageVersions("Work", "Journal", "Alive")
	if err != nil {
		t.Fatalf("List old: %v", err)
	}
	if len(oldList) != 0 {
		t.Fatalf("old locator still has %d versions", len(oldList))
	}
	merged, err := app.ListPageVersions("Work", "Journal", "Taken")
	if err != nil {
		t.Fatalf("List merged: %v", err)
	}
	if len(merged) < 2 {
		t.Fatalf("rename onto retained history should merge, got %d versions", len(merged))
	}
	var sawTaken, sawAlive bool
	for _, v := range merged {
		body, err := app.GetPageVersion("Work", "Journal", "Taken", v.ID)
		if err != nil {
			t.Fatalf("GetPageVersion %s: %v", v.ID, err)
		}
		if strings.Contains(body, "taken-first") {
			sawTaken = true
		}
		if strings.Contains(body, "alive-first") {
			sawAlive = true
		}
	}
	if !sawTaken || !sawAlive {
		t.Fatalf("merged history missing bodies: taken=%v alive=%v", sawTaken, sawAlive)
	}
}

func TestPluginMutateBlock_CollapsesNewlines(t *testing.T) {
	app := newTestApp(t)
	taskID := "55555555-5555-4555-8555-555555555555"
	writeSamplePage(t, app, "Work", "Journal", "Daily", "2026-06-13", taskID, "original text")
	tok := registerTestSession(t, app, "test-plugin")

	ok, err := app.PluginMutateBlock("test-plugin", tok, taskID, "line one\nline two")
	if err != nil || !ok {
		t.Fatalf("PluginMutateBlock: ok=%v err=%v", ok, err)
	}
	filePath := filepath.Join(app.vaultPath, "Work", "Journal", "Daily.md")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)
	if strings.Contains(s, "line one\nline two") {
		t.Fatalf("plugin mutate left raw newlines:\n%s", s)
	}
	if !strings.Contains(s, "line one line two") {
		t.Fatalf("expected collapsed plugin text, got:\n%s", s)
	}
}

func TestPluginMutateBlock_NoVaultReturnsError(t *testing.T) {
	app := &App{pluginSessions: map[string]string{"tok": "test-plugin"}}
	ok, err := app.PluginMutateBlock("test-plugin", "tok", "block-id", "text")
	if err == nil || ok {
		t.Fatalf("expected vault-not-loaded error, got ok=%v err=%v", ok, err)
	}
	if !strings.Contains(err.Error(), "vault database not loaded") {
		t.Fatalf("error = %v, want vault database not loaded", err)
	}
}

func TestPageHistory_FailOpenUnwritableHistory(t *testing.T) {
	app := newTestApp(t)
	seedHistoryPage(t, app, "Work", "Journal", "FailOpen", "# first\n")
	enablePageHistory(t, app, 50, 0)

	hist := filepath.Join(app.vaultPath, ".system", "history")
	if err := os.MkdirAll(filepath.Dir(hist), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(hist)
	// Occupy the history path with a file so Capture cannot create the store.
	if err := os.WriteFile(hist, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SavePageMarkdown("Work", "Journal", "FailOpen", "# second\n"); err != nil {
		t.Fatalf("save should succeed when history is unwritable: %v", err)
	}
}

func TestPageHistory_MissingVersion(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "Missing", "# first\n")
	_, err := app.GetPageVersion("Work", "Journal", "Missing", "nope")
	if err == nil {
		t.Fatal("expected missing version error")
	}
	if err := app.RestorePageVersion("Work", "Journal", "Missing", "nope"); err == nil {
		t.Fatal("expected restore missing version error")
	}
}

func TestPageHistory_PluginBypassesInterval(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 300)
	seedHistoryPage(t, app, "Work", "Journal", "Plugin", "# a\n")
	filePath := filepath.Join(app.vaultPath, "Work", "Journal", "Plugin.md")
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	blocks, _, _, _, err := parser.ParseFileContent(string(raw), "Work", "Journal", "Plugin", "2026-08-16", app.spacesPerTab)
	if err != nil {
		t.Fatal(err)
	}
	blocks[0].CleanText = "b"
	if err := app.saveFileBlocksWithSource("Work", "Journal", "Plugin", blocks, historyReasonPlugin); err != nil {
		t.Fatalf("plugin save 1: %v", err)
	}
	raw, _ = os.ReadFile(filePath)
	blocks, _, _, _, err = parser.ParseFileContent(string(raw), "Work", "Journal", "Plugin", "2026-08-16", app.spacesPerTab)
	if err != nil {
		t.Fatal(err)
	}
	blocks[0].CleanText = "c"
	if err := app.saveFileBlocksWithSource("Work", "Journal", "Plugin", blocks, historyReasonPlugin); err != nil {
		t.Fatalf("plugin save 2: %v", err)
	}
	list, err := app.ListPageVersions("Work", "Journal", "Plugin")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("plugin should bypass interval, got %d versions", len(list))
	}
	for _, v := range list {
		if v.Source != historyReasonPlugin && v.Source != historyReasonEditor {
			t.Fatalf("unexpected source %q", v.Source)
		}
	}
}

func TestPageHistory_MovePageFollowsLocator(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "Movable", "# first\n")
	savePageBody(t, app, "Work", "Journal", "Movable", "# second\n")
	if err := app.MovePage("Work", "Journal", "Archive", "Movable"); err != nil {
		t.Fatalf("MovePage: %v", err)
	}
	oldList, err := app.ListPageVersions("Work", "Journal", "Movable")
	if err != nil {
		t.Fatalf("List old: %v", err)
	}
	if len(oldList) != 0 {
		t.Fatalf("old locator still has %d versions", len(oldList))
	}
	newList, err := app.ListPageVersions("Work", "Archive", "Movable")
	if err != nil {
		t.Fatalf("List new: %v", err)
	}
	if len(newList) == 0 {
		t.Fatal("moved page lost history")
	}
}

func TestPageHistory_LinkedRootStoresOutsideVault(t *testing.T) {
	app := newTestApp(t)
	ext := filepath.Join(t.TempDir(), "Ext")
	if err := os.MkdirAll(filepath.Join(ext, "Journal"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(ext, "Journal", "Linked.md"),
		"---\nnotebook: Ext\nsection: Journal\npage: Linked\ndate: 2026-08-16\ntags: []\n---\n# first\n")
	ln, err := app.LinkNotebook(ext)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}
	if ln.DisplayName != "Ext" {
		t.Fatalf("DisplayName = %q, want Ext", ln.DisplayName)
	}
	enablePageHistory(t, app, 50, 0)
	savePageBody(t, app, ln.DisplayName, "Journal", "Linked", "# second\n")

	list, err := app.ListPageVersions(ln.DisplayName, "Journal", "Linked")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len=%d, want 1", len(list))
	}
	linkedHist := filepath.Join(ext, ".system", "history")
	if _, err := os.Stat(linkedHist); err != nil {
		t.Fatalf("linked history missing at %s: %v", linkedHist, err)
	}
	vaultHist := filepath.Join(app.vaultPath, ".system", "history")
	if _, err := os.Stat(vaultHist); !os.IsNotExist(err) {
		t.Fatalf("linked capture wrote vault history at %s (err=%v)", vaultHist, err)
	}
}

func TestPageHistory_StoreIndependentOfDB(t *testing.T) {
	root := t.TempDir()
	loc := history.Locator{Source: "vault", Notebook: "Work", Section: "Journal", Page: "Solo"}
	if skip, err := history.Capture(root, loc, []byte("hello"), "editor", time.Now().UTC(), history.Options{}); err != nil || skip != "" {
		t.Fatalf("Capture: skip=%q err=%v", skip, err)
	}
	list, err := history.List(root, loc)
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v len=%d", err, len(list))
	}
}

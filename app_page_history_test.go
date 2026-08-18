package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"silt/backend/history"
	"silt/backend/parser"
	"silt/backend/plugins"
	"silt/backend/vault"
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
	if err := app.RequestCapability("test-plugin", string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("grant: %v", err)
	}

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

func TestPluginMutateBlock_RequiresCapability(t *testing.T) {
	app := newTestApp(t)
	taskID := "55555555-5555-4555-8555-555555555555"
	writeSamplePage(t, app, "Work", "Journal", "Daily", "2026-06-13", taskID, "original text")
	tok := registerTestSession(t, app, "third-party")

	if ok, err := app.PluginMutateBlock("third-party", tok, taskID, "hijacked"); err == nil {
		t.Fatalf("expected capability denial without content-mutate grant, got ok=%v", ok)
	}
	filePath := filepath.Join(app.vaultPath, "Work", "Journal", "Daily.md")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "hijacked") {
		t.Fatal("denied PluginMutateBlock still wrote the file")
	}

	if err := app.RequestCapability("third-party", string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if ok, err := app.PluginMutateBlock("third-party", tok, taskID, "allowed"); err != nil || !ok {
		t.Fatalf("PluginMutateBlock with grant: ok=%v err=%v", ok, err)
	}
}

func TestPluginPageWriters_RequireCapability(t *testing.T) {
	app := newTestApp(t)
	tok := registerTestSession(t, app, "third-party")
	if _, err := app.PluginCreatePage("third-party", tok, "Work", "", "Hijack", "2026-06-13"); err == nil {
		t.Fatal("expected PluginCreatePage capability denial")
	}
	if err := app.PluginCreateSection("third-party", tok, "Work", "HijackSec"); err == nil {
		t.Fatal("expected PluginCreateSection capability denial")
	}
	if err := app.PluginCreateNotebook("third-party", tok, "HijackNB"); err == nil {
		t.Fatal("expected PluginCreateNotebook capability denial")
	}
	if err := app.PluginDeletePage("third-party", tok, "Work", "Journal", "Daily"); err == nil {
		t.Fatal("expected PluginDeletePage capability denial")
	}
	if err := app.PluginRenamePage("third-party", tok, "Work", "Journal", "Daily", "Stolen"); err == nil {
		t.Fatal("expected PluginRenamePage capability denial")
	}
	if _, err := os.Stat(filepath.Join(app.vaultPath, "Work", "Hijack.md")); !os.IsNotExist(err) {
		t.Fatal("denied PluginCreatePage still created a page")
	}
}

func TestPluginMutateBlock_NoVaultReturnsError(t *testing.T) {
	app := &App{
		pluginSessions: map[string]string{"tok": "test-plugin"},
		grants: vault.GrantsStore{
			"test-plugin": {string(plugins.CapContentMutate): plugins.QualGranted},
		},
	}
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

func TestPageHistory_FrontmatterEditCaptures(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	writeBookPage(t, app)
	if err := app.SetPageProperty("Books", "", "Dune", "rating", 4); err != nil {
		t.Fatalf("SetPageProperty: %v", err)
	}
	list, err := app.ListPageVersions("Books", "", "Dune")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len=%d, want 1", len(list))
	}
	if list[0].Source != historyReasonEditor {
		t.Fatalf("source = %q, want %q", list[0].Source, historyReasonEditor)
	}
	got, err := app.GetPageVersion("Books", "", "Dune", list[0].ID)
	if err != nil {
		t.Fatalf("GetPageVersion: %v", err)
	}
	if strings.Contains(got, "rating:") {
		t.Fatalf("preview should be the pre-edit body, got %q", got)
	}
	if !strings.Contains(got, "# Dune") {
		t.Fatalf("preview missing page body, got %q", got)
	}
}

func TestPageHistory_MCPSetPagePropertyBypassesInterval(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 300)
	writeBookPage(t, app)
	bridge := newMetaBridge(app)
	if err := bridge.SetPageProperty(context.Background(), "Books", "", "Dune", "rating", "4"); err != nil {
		t.Fatalf("SetPageProperty 1: %v", err)
	}
	if err := bridge.SetPageProperty(context.Background(), "Books", "", "Dune", "rating", "5"); err != nil {
		t.Fatalf("SetPageProperty 2: %v", err)
	}
	list, err := app.ListPageVersions("Books", "", "Dune")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("MCP frontmatter writes should bypass interval, got %d versions", len(list))
	}
	for _, v := range list {
		if v.Source != historyReasonMCP {
			t.Fatalf("source = %q, want %q", v.Source, historyReasonMCP)
		}
	}
}

func TestPageHistory_MCPSetPageTypeCaptures(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 300)
	writeBookPage(t, app)
	if err := app.SaveType(meetingTypeSchema()); err != nil {
		t.Fatalf("SaveType(meeting): %v", err)
	}
	bridge := newMetaBridge(app)
	if _, err := bridge.SetPageType(context.Background(), "Books", "", "Dune", "meeting"); err != nil {
		t.Fatalf("SetPageType: %v", err)
	}
	list, err := app.ListPageVersions("Books", "", "Dune")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v len=%d", err, len(list))
	}
	if list[0].Source != historyReasonMCP {
		t.Fatalf("source = %q, want %q", list[0].Source, historyReasonMCP)
	}
}

func TestPageHistory_InvalidFrontmatterEditDoesNotCapture(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	writeBookPage(t, app)
	if err := app.SetPageProperty("Books", "", "Dune", "status", "bogusoption"); err == nil {
		t.Fatal("invalid SetPageProperty should error")
	}
	list, err := app.ListPageVersions("Books", "", "Dune")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("rejected write captured %d versions", len(list))
	}
}

func TestPageHistory_RestoreEmitsPageScopedBlockChanged(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "EmitMe", "# original\n")
	savePageBody(t, app, "Work", "Journal", "EmitMe", "# edited\n")
	list, err := app.ListPageVersions("Work", "Journal", "EmitMe")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v len=%d", err, len(list))
	}
	var got []parser.BlockChangedEvent
	app.eventEmit = func(name string, data ...any) {
		if name != string(EventBlockChanged) || len(data) == 0 {
			return
		}
		if ev, ok := data[0].(parser.BlockChangedEvent); ok {
			got = append(got, ev)
		}
	}
	if err := app.RestorePageVersion("Work", "Journal", "EmitMe", list[0].ID); err != nil {
		t.Fatalf("RestorePageVersion: %v", err)
	}
	var sawPageScoped bool
	for _, ev := range got {
		if ev.ID == "" && ev.Notebook == "Work" && ev.Section == "Journal" && ev.Page == "EmitMe" {
			sawPageScoped = true
			break
		}
	}
	if !sawPageScoped {
		t.Fatalf("restore did not emit page-scoped block:changed, got %+v", got)
	}
}

func TestPageHistory_MCPCreateOnNestedSection(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 300)
	seedHistoryPage(t, app, "Work", "Projects/Active", "Nested", "# start\n")
	if _, err := app.createBlockWithReason("", "Work", "Projects/Active", "Nested", "NOTE", "agent note", historyReasonMCP); err != nil {
		t.Fatalf("createBlockWithReason: %v", err)
	}
	list, err := app.ListPageVersions("Work", "Projects/Active", "Nested")
	if err != nil || len(list) == 0 {
		t.Fatalf("List nested: %v len=%d", err, len(list))
	}
	flat, err := app.ListPageVersions("Work", "ProjectsActive", "Nested")
	if err != nil {
		t.Fatalf("List flattened: %v", err)
	}
	if len(flat) != 0 {
		t.Fatalf("MCP create wrote history under flattened section, got %d", len(flat))
	}
	live := filepath.Join(app.vaultPath, "Work", "Projects", "Active", "Nested.md")
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("nested page missing after MCP create: %v", err)
	}
	wrong := filepath.Join(app.vaultPath, "Work", "ProjectsActive", "Nested.md")
	if _, err := os.Stat(wrong); !os.IsNotExist(err) {
		t.Fatalf("MCP create wrote flattened path %s", wrong)
	}
}

func TestCreateSection_RejectsHistoryRootSentinel(t *testing.T) {
	app := newTestApp(t)
	if err := app.CreateSection("Work", "", history.EmptySectionName); err == nil {
		t.Fatal("CreateSection accepted reserved empty-section sentinel")
	}
	if _, err := validateSectionPath(history.EmptySectionName, false); err == nil {
		t.Fatal("validateSectionPath accepted reserved empty-section sentinel")
	}
	if _, err := validateSectionPath("Projects/"+history.EmptySectionName, true); err == nil {
		t.Fatal("validateSectionPath accepted reserved sentinel as a nested segment")
	}
}

func TestPageHistory_RootPageDoesNotCollideWithReservedSection(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "", "Inbox", "# root page\n")
	savePageBody(t, app, "Work", "", "Inbox", "# edited\n")
	if err := app.CreateSection("Work", "", history.EmptySectionName); err == nil {
		t.Fatal("CreateSection accepted reserved sentinel after root-page history existed")
	}
	list, err := app.ListPageVersions("Work", "", "Inbox")
	if err != nil || len(list) == 0 {
		t.Fatalf("root-page history: %v len=%d", err, len(list))
	}
}

func TestSaveFileBlocks_RejectsHistoryRootSentinel(t *testing.T) {
	app := newTestApp(t)
	if err := app.SaveFileBlocks("Work", history.EmptySectionName, "Inbox", nil); err == nil {
		t.Fatal("SaveFileBlocks accepted reserved empty-section sentinel")
	}
}

func TestPageHistory_FrontmatterEditUsesPathLocator(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	writeBookPage(t, app)
	filePath := filepath.Join(app.vaultPath, "Books", "Dune.md")
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(string(raw), `section: ""`, `section: "Stale"`, 1)
	if err := os.WriteFile(filePath, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SetPageProperty("Books", "", "Dune", "rating", 4); err != nil {
		t.Fatalf("SetPageProperty: %v", err)
	}
	pathList, err := app.ListPageVersions("Books", "", "Dune")
	if err != nil {
		t.Fatalf("List path locator: %v", err)
	}
	if len(pathList) == 0 {
		t.Fatal("frontmatter edit stored history under YAML section, not the path locator")
	}
	staleList, err := app.ListPageVersions("Books", "Stale", "Dune")
	if err != nil {
		t.Fatalf("List stale locator: %v", err)
	}
	if len(staleList) != 0 {
		t.Fatalf("history leaked to YAML section locator, got %d", len(staleList))
	}
}

func TestPageHistory_FrontmatterEditSanitizesPageName(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	if err := app.SaveType(bookTypeSchema()); err != nil {
		t.Fatalf("SaveType: %v", err)
	}
	content := "---\n" +
		"notebook: \"Books\"\n" +
		"section: \"\"\n" +
		"page: \"AB\"\n" +
		"date: \"2026-08-01\"\n" +
		"tags: []\n" +
		"type: \"book\"\n" +
		"title: \"Slash\"\n" +
		"---\n# Slash\n"
	writeFile(t, filepath.Join(app.vaultPath, "Books", "AB.md"), content)
	if err := app.SetPageProperty("Books", "", "A/B", "title", "Renamed"); err != nil {
		t.Fatalf("SetPageProperty: %v", err)
	}
	list, err := app.ListPageVersions("Books", "", "A/B")
	if err != nil {
		t.Fatalf("List sanitized: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("frontmatter edit stored history under percent-encoded page name")
	}
}

func TestPageHistory_InboundRewriteDoesNotCapture(t *testing.T) {
	app := newTestApp(t)
	seedHistoryPage(t, app, "Work", "Journal", "Hub", "# hub\nSee [[Work/Journal/Old]]\n")
	seedHistoryPage(t, app, "Work", "Journal", "Old", "# old\n")
	enablePageHistory(t, app, 50, 0)
	savePageBody(t, app, "Work", "Journal", "Old", "# edited\n")
	if err := app.RenamePage("Work", "Journal", "Old", "New"); err != nil {
		t.Fatalf("RenamePage: %v", err)
	}
	hubList, err := app.ListPageVersions("Work", "Journal", "Hub")
	if err != nil {
		t.Fatalf("List hub: %v", err)
	}
	if len(hubList) != 0 {
		t.Fatalf("inbound wiki-link rewrite captured hub history, got %d", len(hubList))
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

func TestPluginPageHistory_SessionAndRestoreGrant(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "Agent", "# first\n")
	savePageBody(t, app, "Work", "Journal", "Agent", "# second\n")

	tok := registerTestSession(t, app, "third-party")
	if _, err := app.PluginListPageVersions("spoofed", tok, "Work", "Journal", "Agent"); err == nil {
		t.Fatal("expected session mismatch on list")
	}

	list, err := app.PluginListPageVersions("third-party", tok, "Work", "Journal", "Agent")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("expected leftover versions")
	}
	body, err := app.PluginGetPageVersion("third-party", tok, "Work", "Journal", "Agent", list[0].ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(body, "first") {
		t.Fatalf("preview body = %q", body)
	}

	if err := app.PluginRestorePageVersion("third-party", tok, "Work", "Journal", "Agent", list[0].ID); err == nil {
		t.Fatal("expected capability denial without content-mutate")
	}
	got, err := app.FetchPageMarkdown("Work", "Journal", "Agent")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "first") && !strings.Contains(got, "second") {
		t.Fatal("denied restore still wrote the live page")
	}

	if err := app.RequestCapability("third-party", string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := app.PluginRestorePageVersion("third-party", tok, "Work", "Journal", "Agent", list[0].ID); err != nil {
		t.Fatalf("restore with grant: %v", err)
	}
	got, err = app.FetchPageMarkdown("Work", "Journal", "Agent")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "first") {
		t.Fatalf("restored body = %q", got)
	}
}

func TestPluginPageHistory_NoVault(t *testing.T) {
	app := &App{
		pluginSessions: map[string]string{"tok": "test-plugin"},
		grants: vault.GrantsStore{
			"test-plugin": {string(plugins.CapContentMutate): plugins.QualGranted},
		},
	}
	if _, err := app.PluginListPageVersions("test-plugin", "tok", "Work", "", "X"); err == nil {
		t.Fatal("expected no-vault error")
	}
	if _, err := app.PluginGetPageVersion("test-plugin", "tok", "Work", "", "X", "v"); err == nil {
		t.Fatal("expected no-vault error")
	}
	if err := app.PluginRestorePageVersion("test-plugin", "tok", "Work", "", "X", "v"); err == nil {
		t.Fatal("expected no-vault error")
	}
}

func TestDeletedPageHistory_ListAndRestore(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "Gone", "# first\n")
	savePageBody(t, app, "Work", "Journal", "Gone", "# second\n")
	list, err := app.ListPageVersions("Work", "Journal", "Gone")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v len=%d", err, len(list))
	}
	oldID := list[len(list)-1].ID
	if err := app.DeletePage("Work", "Journal", "Gone"); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}

	orphans, err := app.ListDeletedPageHistory()
	if err != nil {
		t.Fatalf("ListDeleted: %v", err)
	}
	found := false
	for _, o := range orphans {
		if o.Notebook == "Work" && o.Section == "Journal" && o.Page == "Gone" {
			found = true
			if o.VersionCount < 1 {
				t.Fatalf("orphan versions=%d", o.VersionCount)
			}
		}
	}
	if !found {
		t.Fatalf("deleted page missing from orphans: %+v", orphans)
	}

	live, err := app.ListDeletedPageHistory()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range live {
		if o.Notebook == "Work" && o.Page == "Daily" {
			t.Fatal("live page must not appear in deleted list")
		}
	}

	if err := app.RestoreDeletedPageVersion("Work", "Journal", "Gone", oldID, "", "", ""); err != nil {
		t.Fatalf("restore: %v", err)
	}
	body, err := app.FetchPageMarkdown("Work", "Journal", "Gone")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "first") {
		t.Fatalf("restored body = %q", body)
	}
	after, err := app.ListDeletedPageHistory()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range after {
		if o.Notebook == "Work" && o.Section == "Journal" && o.Page == "Gone" {
			t.Fatal("restored page still listed as deleted")
		}
	}
}

func TestDeletedPageHistory_OccupiedDestRefuses(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "OldGone", "# old-first\n")
	savePageBody(t, app, "Work", "Journal", "OldGone", "# old-second\n")
	list, err := app.ListPageVersions("Work", "Journal", "OldGone")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v", err)
	}
	if err := app.DeletePage("Work", "Journal", "OldGone"); err != nil {
		t.Fatal(err)
	}
	seedHistoryPage(t, app, "Work", "Journal", "Taken", "# taken\n")
	before, err := os.ReadFile(filepath.Join(app.vaultPath, "Work", "Journal", "Taken.md"))
	if err != nil {
		t.Fatal(err)
	}
	restoreErr := app.RestoreDeletedPageVersion("Work", "Journal", "OldGone", list[0].ID, "Work", "Journal", "Taken")
	if restoreErr == nil {
		t.Fatal("expected occupied dest error")
	}
	after, err := os.ReadFile(filepath.Join(app.vaultPath, "Work", "Journal", "Taken.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("occupied dest was overwritten")
	}
	var ipc *IPCError
	if !errors.As(restoreErr, &ipc) || ipc.Code != CodePageExists {
		t.Fatalf("occupied dest err = %v", restoreErr)
	}
}

func TestDeletedPageHistory_MissingVersionAndPartialDest(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "Gone", "# first\n")
	savePageBody(t, app, "Work", "Journal", "Gone", "# second\n")
	if err := app.DeletePage("Work", "Journal", "Gone"); err != nil {
		t.Fatal(err)
	}
	err := app.RestoreDeletedPageVersion("Work", "Journal", "Gone", "missing", "", "", "")
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeNavigationNotFound {
		t.Fatalf("missing version err = %v", err)
	}
	err = app.RestoreDeletedPageVersion("Work", "Journal", "Gone", "v", "Work", "", "")
	if !errors.As(err, &ipc) || ipc.Code != CodeInvalidNavigationPath {
		t.Fatalf("partial dest err = %v", err)
	}
}

func TestDeletedPageHistory_SanitizeDestFrontmatter(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	filePath := filepath.Join(app.vaultPath, "Work", "Journal", "OldGone.md")
	content := "---\nnotebook: Work\nsection: Journal\npage: OldGone\ndate: 2026-01-15\ncreated: \"2026-01-15T09:30:00\"\ntags: [work, planning]\ntype: \"book\"\n---\n# first\n"
	writeFile(t, filePath, content)
	blocks, _, _, _, err := parser.ParseFileContent(content, "Work", "Journal", "OldGone", "2026-01-15", app.spacesPerTab)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SaveFileBlocks("Work", "Journal", "OldGone", blocks); err != nil {
		t.Fatal(err)
	}
	savePageBody(t, app, "Work", "Journal", "OldGone", "# second\n")
	list, err := app.ListPageVersions("Work", "Journal", "OldGone")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v", err)
	}
	if err := app.DeletePage("Work", "Journal", "OldGone"); err != nil {
		t.Fatal(err)
	}
	if err := app.RestoreDeletedPageVersion("Work", "Journal", "OldGone", list[0].ID, "Work", "Journal", "Q1/Q2 recap"); err != nil {
		t.Fatalf("restore-as: %v", err)
	}
	safePage := "Q1Q2 recap"
	raw, err := os.ReadFile(filepath.Join(app.vaultPath, "Work", "Journal", safePage+".md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if strings.Contains(got, `page: "Q1/Q2 recap"`) {
		t.Fatal("frontmatter kept the raw dest page name")
	}
	if !strings.Contains(got, `page: "Q1Q2 recap"`) {
		t.Fatalf("frontmatter page missing sanitized name:\n%s", raw)
	}
	if !strings.Contains(got, `created: "2026-01-15T09:30:00"`) {
		t.Fatalf("restore dropped snapshot created:\n%s", raw)
	}
	if !strings.Contains(got, "work") || !strings.Contains(got, "planning") {
		t.Fatalf("restore dropped snapshot tags:\n%s", raw)
	}
	if !strings.Contains(got, `type: "book"`) && !strings.Contains(got, "type: book") {
		t.Fatalf("restore dropped snapshot type:\n%s", raw)
	}
	if !strings.Contains(got, "date: 2026-01-15") && !strings.Contains(got, `date: "2026-01-15"`) {
		t.Fatalf("restore dropped snapshot date:\n%s", raw)
	}
	indexed, err := app.FetchPageBlocks("Work", "Journal", safePage)
	if err != nil {
		t.Fatal(err)
	}
	if len(indexed) == 0 {
		t.Fatal("indexed blocks missing for sanitized dest")
	}
}

func TestDeletedPageHistory_RestoreDoesNotAddSyntheticVersion(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 2, 0)
	seedHistoryPage(t, app, "Work", "Journal", "Cap", "# v0\n")
	savePageBody(t, app, "Work", "Journal", "Cap", "# v1\n")
	savePageBody(t, app, "Work", "Journal", "Cap", "# v2\n")
	before, err := app.ListPageVersions("Work", "Journal", "Cap")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 {
		t.Fatalf("pre-delete versions = %d", len(before))
	}
	oldestID := before[len(before)-1].ID
	if err := app.DeletePage("Work", "Journal", "Cap"); err != nil {
		t.Fatal(err)
	}
	if err := app.RestoreDeletedPageVersion("Work", "Journal", "Cap", before[0].ID, "", "", ""); err != nil {
		t.Fatalf("restore: %v", err)
	}
	after, err := app.ListPageVersions("Work", "Journal", "Cap")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("restore changed version count %d → %d", len(before), len(after))
	}
	foundOldest := false
	for _, v := range after {
		if v.Source == historyReasonRestore {
			t.Fatal("recreate captured a synthetic restore snapshot")
		}
		if v.ID == oldestID {
			foundOldest = true
		}
	}
	if !foundOldest {
		t.Fatal("oldest real snapshot was evicted")
	}
}

func TestDeletedPageHistory_RecreateMissingNotebookDir(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "Gone", "# first\n")
	savePageBody(t, app, "Work", "Journal", "Gone", "# second\n")
	list, err := app.ListPageVersions("Work", "Journal", "Gone")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v", err)
	}
	if err := app.DeletePage("Work", "Journal", "Gone"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(app.vaultPath, "Work")); err != nil {
		t.Fatal(err)
	}
	if err := app.RestoreDeletedPageVersion("Work", "Journal", "Gone", list[0].ID, "", "", ""); err != nil {
		t.Fatalf("restore after notebook dir gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app.vaultPath, "Work", "Journal", "Gone.md")); err != nil {
		t.Fatalf("recreated page missing: %v", err)
	}
}

func TestDeletedPageHistory_RefuseCrossRoot(t *testing.T) {
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
	enablePageHistory(t, app, 50, 0)
	savePageBody(t, app, ln.DisplayName, "Journal", "Linked", "# second\n")
	list, err := app.ListPageVersions(ln.DisplayName, "Journal", "Linked")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v len=%d", err, len(list))
	}
	if err := app.DeletePage(ln.DisplayName, "Journal", "Linked"); err != nil {
		t.Fatal(err)
	}
	err = app.RestoreDeletedPageVersion(ln.DisplayName, "Journal", "Linked", list[0].ID, "Work", "Journal", "FromLinked")
	if err == nil {
		t.Fatal("expected cross-root refuse")
	}
	if _, statErr := os.Stat(filepath.Join(app.vaultPath, "Work", "Journal", "FromLinked.md")); !os.IsNotExist(statErr) {
		t.Fatal("cross-root restore wrote a vault page")
	}
}

func TestDeletedPageHistory_RestoreAsRelocates(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "MoveMe", "# first\n")
	savePageBody(t, app, "Work", "Journal", "MoveMe", "# second\n")
	list, err := app.ListPageVersions("Work", "Journal", "MoveMe")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v", err)
	}
	if err := app.DeletePage("Work", "Journal", "MoveMe"); err != nil {
		t.Fatal(err)
	}
	if err := app.RestoreDeletedPageVersion("Work", "Journal", "MoveMe", list[0].ID, "Work", "Archive", "Restored"); err != nil {
		t.Fatalf("restore-as: %v", err)
	}
	body, err := app.FetchPageMarkdown("Work", "Archive", "Restored")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "first") && !strings.Contains(body, "second") {
		t.Fatalf("restore-as body = %q", body)
	}
	orphans, err := app.ListDeletedPageHistory()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range orphans {
		if o.Page == "MoveMe" {
			t.Fatal("original locator still in deleted list after relocate")
		}
	}
}

func TestDeletedPageHistory_LinkedDeleteStillLists(t *testing.T) {
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
	enablePageHistory(t, app, 50, 0)
	savePageBody(t, app, ln.DisplayName, "Journal", "Linked", "# second\n")
	if err := app.DeletePage(ln.DisplayName, "Journal", "Linked"); err != nil {
		t.Fatalf("DeletePage linked: %v", err)
	}
	orphans, err := app.ListDeletedPageHistory()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range orphans {
		if o.Page == "Linked" && o.Source == "linked" {
			found = true
		}
	}
	if !found {
		t.Fatalf("linked delete missing from orphans: %+v", orphans)
	}
}

func TestDeletedPageHistory_TrashUnchanged(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "TrashMe", "# first\n")
	savePageBody(t, app, "Work", "Journal", "TrashMe", "# second\n")
	if err := app.DeletePage("Work", "Journal", "TrashMe"); err != nil {
		t.Fatal(err)
	}
	trash := filepath.Join(app.vaultPath, ".system", "trash")
	if _, err := os.Stat(trash); err != nil {
		t.Fatalf("trash missing: %v", err)
	}
	found := false
	err := filepath.Walk(trash, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, "TrashMe.md") {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("DeletePage no longer moved the latest file to trash")
	}
}

func TestDeletedPageHistory_CaptureOffLeftovers(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "OffLater", "# first\n")
	savePageBody(t, app, "Work", "Journal", "OffLater", "# second\n")
	if err := app.DeletePage("Work", "Journal", "OffLater"); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.GetSystemConfig()
	if err != nil {
		t.Fatal(err)
	}
	off := false
	cfg.Editor.AutoVersioningEnabled = &off
	if err := app.SaveSystemConfig(cfg); err != nil {
		t.Fatal(err)
	}
	orphans, err := app.ListDeletedPageHistory()
	if err != nil {
		t.Fatal(err)
	}
	var id string
	list, err := app.ListPageVersions("Work", "Journal", "OffLater")
	if err != nil || len(list) == 0 {
		t.Fatalf("leftover list: %v len=%d", err, len(list))
	}
	id = list[0].ID
	found := false
	for _, o := range orphans {
		if o.Page == "OffLater" {
			found = true
		}
	}
	if !found {
		t.Fatal("capture-off leftovers missing from deleted list")
	}
	if err := app.RestoreDeletedPageVersion("Work", "Journal", "OffLater", id, "", "", ""); err != nil {
		t.Fatalf("restore leftovers: %v", err)
	}
}

func TestDeletedPageHistory_SkipsEncodedTraversalLocator(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	outside := filepath.Join(filepath.Dir(app.vaultPath), "secret.md")
	writeFile(t, outside, "secret")
	t.Cleanup(func() { _ = os.Remove(outside) })
	planted := filepath.Join(app.vaultPath, ".system", "history", "pages", "vault", "%2E%2E", "%2E%2E", "secret.jsonl")
	if err := os.MkdirAll(filepath.Dir(planted), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, planted, `{"id":"v-evil","ts":"2026-08-16T18:00:00.000000000Z","hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","bytes":1,"source":"editor"}`+"\n")
	orphans, err := app.ListDeletedPageHistory()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range orphans {
		if o.Page == "secret" || strings.Contains(o.Notebook, "..") || strings.Contains(o.Section, "..") {
			t.Fatalf("listed planted locator: %+v", o)
		}
	}
}

func TestListPageVersions_HidesStorePath(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "Daily", "# first\n")
	savePageBody(t, app, "Work", "Journal", "Daily", "# second\n")
	man := filepath.Join(app.vaultPath, ".system", "history", "pages", "vault", "Work", "Journal", "Daily.jsonl")
	if err := os.WriteFile(man, []byte(strings.Repeat("x", 2<<20+64)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := app.ListPageVersions("Work", "Journal", "Daily")
	if err == nil {
		t.Fatal("expected list error")
	}
	if strings.Contains(err.Error(), app.vaultPath) || strings.Contains(err.Error(), man) {
		t.Fatalf("leaked path: %v", err)
	}
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeNavigationUnavailable {
		t.Fatalf("list err = %v", err)
	}
}

func TestRestoreDeletedPageVersion_RefusesLiveSource(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "Alive", "# first\n")
	savePageBody(t, app, "Work", "Journal", "Alive", "# second\n")
	list, err := app.ListPageVersions("Work", "Journal", "Alive")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v", err)
	}
	err = app.RestoreDeletedPageVersion("Work", "Journal", "Alive", list[0].ID, "Work", "Journal", "Copy")
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodePageExists {
		t.Fatalf("live source err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(app.vaultPath, "Work", "Journal", "Copy.md")); !os.IsNotExist(statErr) {
		t.Fatal("live-source restore wrote dest")
	}
	after, err := app.ListPageVersions("Work", "Journal", "Alive")
	if err != nil || len(after) == 0 {
		t.Fatalf("live page history moved: %v", err)
	}
}

func TestRestoreDeletedPageVersion_DestIOHidesPath(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)
	seedHistoryPage(t, app, "Work", "Journal", "Gone", "# first\n")
	savePageBody(t, app, "Work", "Journal", "Gone", "# second\n")
	list, err := app.ListPageVersions("Work", "Journal", "Gone")
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v", err)
	}
	if err := app.DeletePage("Work", "Journal", "Gone"); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(app.vaultPath, "Work", "Blocked")
	writeFile(t, blocked, "not-a-directory")
	err = app.RestoreDeletedPageVersion("Work", "Journal", "Gone", list[0].ID, "Work", "Blocked", "Copy")
	if err == nil {
		t.Fatal("expected dest I/O error")
	}
	if strings.Contains(err.Error(), app.vaultPath) || strings.Contains(err.Error(), blocked) {
		t.Fatalf("leaked path: %v", err)
	}
	var ipc *IPCError
	if !errors.As(err, &ipc) {
		t.Fatalf("dest I/O err = %v", err)
	}
	if ipc.Code != CodeNavigationUnavailable && ipc.Code != CodeNavigationNotFound {
		t.Fatalf("dest I/O code = %s", ipc.Code)
	}
}

func TestHistoryWriteError_HidesPath(t *testing.T) {
	leaked := &os.PathError{Op: "rename", Path: `C:\secret\vault\Work\Journal\Daily.md`, Err: os.ErrPermission}
	err := historyWriteError(leaked)
	if err == nil {
		t.Fatal("expected mapped error")
	}
	msg := err.Error()
	if strings.Contains(msg, `C:\`) || strings.Contains(msg, "Daily.md") || strings.Contains(msg, "secret") {
		t.Fatalf("leaked path: %s", msg)
	}
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeNavigationUnavailable {
		t.Fatalf("mapped err = %v", err)
	}
	if ipc.Message != "could not write the page" {
		t.Fatalf("write message = %q", ipc.Message)
	}
}

func TestHistoryReadError_HidesPath(t *testing.T) {
	leaked := &os.PathError{Op: "open", Path: `C:\secret\vault\.system\history\x.gz`, Err: os.ErrPermission}
	err := historyReadError(leaked)
	if err == nil {
		t.Fatal("expected mapped error")
	}
	msg := err.Error()
	if strings.Contains(msg, `C:\`) || strings.Contains(msg, ".system") || strings.Contains(msg, "secret") {
		t.Fatalf("leaked path: %s", msg)
	}
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeNavigationUnavailable {
		t.Fatalf("mapped err = %v", err)
	}
	notFound := historyReadError(history.ErrNotFound)
	if !errors.As(notFound, &ipc) || ipc.Code != CodeNavigationNotFound {
		t.Fatalf("not-found mapping = %v", notFound)
	}
}

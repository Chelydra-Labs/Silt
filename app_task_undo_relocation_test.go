package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"silt/backend/parser"
	"silt/backend/plugins"
)

// A page rename after the mutation changes the file path recorded in the
// journal; the replay must re-resolve the block's current location instead of
// popping the entry as skipped.
func TestUndoLastTaskMutation_SurvivesPageRename(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0022-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "BeforeRename", "2026-07-01", content)

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}
	if err := app.RenamePage("W", "S", "BeforeRename", "AfterRename"); err != nil {
		t.Fatalf("RenamePage: %v", err)
	}
	renamedPath := filepath.Join(app.vaultPath, "W", "S", "AfterRename.md")
	if line := requireTaskLine(t, readTaskPage(t, renamedPath), id); !strings.Contains(line, "[owner:: Alice]") {
		t.Fatalf("mutation lost during rename: %q", line)
	}

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || result.Skipped {
		t.Fatalf("undo after rename = %+v, want restored", result)
	}
	if result.Notebook != "W" || result.Section != "S" || result.Page != "AfterRename" {
		t.Errorf("result locator = %+v, want the renamed page", result)
	}

	if line := requireTaskLine(t, readTaskPage(t, renamedPath), id); strings.Contains(line, "[owner::") {
		t.Errorf("undo did not restore the owner on the renamed page: %q", line)
	}
	requireTaskProjection(t, app, id, func(t *testing.T, task parser.TaskResult) {
		if task.Page != "AfterRename" || task.Owner != "" {
			t.Errorf("index row after undo = page %q owner %q, want AfterRename/empty", task.Page, task.Owner)
		}
	})
}

// A block moved to another page after the mutation leaves the recorded page
// still present (so the file read succeeds) but the target block absent there.
// The replay must find it at its new location.
func TestUndoLastTaskMutation_SurvivesBlockMoveToAnotherPage(t *testing.T) {
	app := newTestApp(t)
	const (
		id       = "a11d0023-1111-1111-1111-111111111111"
		anchorID = "a11d0024-1111-1111-1111-111111111111"
	)
	indexTestFile(t, app, "W", "S", "MoveSource", "2026-07-01",
		"- [ ] ship <!-- id: "+id+" -->\n")
	targetPath := indexTestFile(t, app, "W", "S", "MoveTarget", "2026-07-01",
		"- destination <!-- id: "+anchorID+" -->\n")

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}

	const pluginID = "mover"
	token := registerTestSession(t, app, pluginID)
	if err := app.RequestCapability(pluginID, string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("RequestCapability: %v", err)
	}
	if err := app.PluginMoveBlock(pluginID, token, id, "", "W", "S", "MoveTarget"); err != nil {
		t.Fatalf("PluginMoveBlock: %v", err)
	}
	loc, err := app.db.GetBlockLocation(id)
	if err != nil || loc.Page != "MoveTarget" {
		t.Fatalf("block did not move: loc=%+v err=%v", loc, err)
	}
	if line := requireTaskLine(t, readTaskPage(t, targetPath), id); !strings.Contains(line, "[owner:: Alice]") {
		t.Fatalf("mutation lost during move: %q", line)
	}

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || result.Skipped {
		t.Fatalf("undo after move = %+v, want restored", result)
	}
	if result.Page != "MoveTarget" {
		t.Errorf("result page = %q, want MoveTarget", result.Page)
	}

	if line := requireTaskLine(t, readTaskPage(t, targetPath), id); strings.Contains(line, "[owner::") {
		t.Errorf("undo did not restore the owner at the block's new location: %q", line)
	}
	requireTaskProjection(t, app, id, func(t *testing.T, task parser.TaskResult) {
		if task.Page != "MoveTarget" || task.Owner != "" {
			t.Errorf("index row after undo = page %q owner %q, want MoveTarget/empty", task.Page, task.Owner)
		}
	})
}

// A cross-source move (vault page → linked root) after the mutation must
// publish the undo's index row and history snapshot under the source that
// holds the written file. Publishing under the recorded source would leave the
// linked row stale and invent a phantom row under vault for a page that does
// not exist there.
func TestUndoLastTaskMutation_SurvivesCrossSourceMove(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)

	ext := t.TempDir()
	ln, err := app.LinkNotebook(ext)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}
	linkedSource := "linked:" + ln.ID
	const (
		id       = "a11d0025-1111-1111-1111-111111111111"
		anchorID = "a11d0026-1111-1111-1111-111111111111"
	)
	indexTestFile(t, app, "W", "S", "CrossSource", "2026-07-01",
		"- [ ] ship <!-- id: "+id+" -->\n")

	// Seed the linked root's target page in the index exactly as the scanner
	// would: the move resolves its target source from the notebook name and
	// reads the source page's indexed location.
	targetContent := "- destination <!-- id: " + anchorID + " -->\n"
	writeFile(t, filepath.Join(ext, "Plan.md"), targetContent)
	targetBlocks, targetMeta, _, _, err := parser.ParseFileContent(targetContent, ln.DisplayName, "", "Plan", "2026-07-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent target: %v", err)
	}
	if err := app.db.IndexFileBlocks(linkedSource, targetMeta.Notebook, targetMeta.Section, targetMeta.Page, targetBlocks, targetMeta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks target: %v", err)
	}

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}

	const pluginID = "cross-source-mover"
	token := registerTestSession(t, app, pluginID)
	if err := app.RequestCapability(pluginID, string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("RequestCapability: %v", err)
	}
	if err := app.PluginMoveBlock(pluginID, token, id, "", ln.DisplayName, "", "Plan"); err != nil {
		t.Fatalf("PluginMoveBlock: %v", err)
	}
	loc, err := app.db.GetBlockLocation(id)
	if err != nil {
		t.Fatalf("GetBlockLocation after move: %v", err)
	}
	if loc.Source != linkedSource || loc.Page != "Plan" {
		t.Fatalf("block did not move across sources: loc=%+v", loc)
	}
	linkedPath := filepath.Join(ext, "Plan.md")
	if line := requireTaskLine(t, readTaskPage(t, linkedPath), id); !strings.Contains(line, "[owner:: Alice]") {
		t.Fatalf("mutation lost during cross-source move: %q", line)
	}

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || result.Skipped {
		t.Fatalf("cross-source undo = %+v, want restored", result)
	}
	if line := requireTaskLine(t, readTaskPage(t, linkedPath), id); strings.Contains(line, "[owner::") {
		t.Errorf("cross-source undo did not restore the owner: %q", line)
	}
	requireTaskProjection(t, app, id, func(t *testing.T, task parser.TaskResult) {
		if task.Owner != "" || task.Source != linkedSource || task.Page != "Plan" {
			t.Errorf("linked index row after undo = owner %q source %q page %q, want empty/%s/Plan",
				task.Owner, task.Source, task.Page, linkedSource)
		}
	})

	// The written file is the linked one, so the linked row must carry the
	// restored state...
	linkedBlocks, err := app.db.FetchPageBlocks(linkedSource, targetMeta.Notebook, targetMeta.Section, "Plan")
	if err != nil {
		t.Fatalf("FetchPageBlocks linked: %v", err)
	}
	found := false
	for _, b := range linkedBlocks {
		if b.ID != id {
			continue
		}
		found = true
		if b.Owner != "" {
			t.Errorf("linked index row still reports owner %q after undo", b.Owner)
		}
	}
	if !found {
		t.Fatalf("undo left no linked index row for %s", id)
	}
	// ...and the source the block left must not gain a row for the new page.
	phantom, err := app.db.FetchPageBlocks("vault", targetMeta.Notebook, targetMeta.Section, "Plan")
	if err != nil {
		t.Fatalf("FetchPageBlocks vault: %v", err)
	}
	if len(phantom) != 0 {
		t.Errorf("undo published %d block(s) under vault for the linked page", len(phantom))
	}

	// History attribution follows the same source: the linked page owns the
	// undo snapshot.
	versions, err := app.ListPageVersions(targetMeta.Notebook, targetMeta.Section, "Plan")
	if err != nil {
		t.Fatalf("ListPageVersions: %v", err)
	}
	undoVersion := false
	for _, v := range versions {
		if v.Source == historyReasonUndo {
			undoVersion = true
		}
	}
	if !undoVersion {
		t.Errorf("linked page history has no undo snapshot (versions: %+v)", versions)
	}
}

// A mutation recorded against a linked notebook that is unlinked before the
// undo can never be replayed: the source no longer resolves. That entry must
// be skipped and popped, not error forever — otherwise every older mutation in
// the session journal becomes unreachable behind it.
func TestUndoLastTaskMutation_SkipsUnresolvableLinkedTarget(t *testing.T) {
	app := newTestApp(t)
	ext := t.TempDir()
	ln, err := app.LinkNotebook(ext)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}
	const (
		vaultID  = "a11d0029-1111-1111-1111-111111111111"
		linkedID = "a11d002a-1111-1111-1111-111111111111"
	)
	vaultPath := indexTestFile(t, app, "W", "S", "AfterUnlink", "2026-07-01",
		"- [ ] vault task <!-- id: "+vaultID+" -->\n")

	linkedContent := "- [ ] linked task <!-- id: " + linkedID + " -->\n"
	linkedPath := filepath.Join(ext, "Plan.md")
	writeFile(t, linkedPath, linkedContent)
	source := "linked:" + ln.ID
	linkedBlocks, linkedMeta, _, _, err := parser.ParseFileContent(linkedContent, ln.DisplayName, "", "Plan", "2026-07-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent linked: %v", err)
	}
	if err := app.db.IndexFileBlocks(source, linkedMeta.Notebook, linkedMeta.Section, linkedMeta.Page, linkedBlocks, linkedMeta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks linked: %v", err)
	}

	// An older vault mutation sits under the newer linked one.
	if err := app.SetTaskOwner(vaultID, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner vault: %v", err)
	}
	if err := app.SetTaskOwner(linkedID, "Bob"); err != nil {
		t.Fatalf("SetTaskOwner linked: %v", err)
	}
	if got := taskUndoLen(app); got != 2 {
		t.Fatalf("journal after two mutations = %d, want 2", got)
	}
	if err := app.UnlinkNotebook(ln.ID); err != nil {
		t.Fatalf("UnlinkNotebook: %v", err)
	}

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("unlinked target must skip, not error: %v", err)
	}
	if !result.Skipped || result.Undone {
		t.Fatalf("result = %+v, want a skipped entry", result)
	}
	if len(result.ChangedFields) != 0 {
		t.Errorf("skipped result advertised restored fields: %v", result.ChangedFields)
	}
	if !reflect.DeepEqual(result.SkippedBlockIDs, []string{linkedID}) {
		t.Errorf("skipped block ids = %v, want [%s]", result.SkippedBlockIDs, linkedID)
	}
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("dead entry was not popped: journal = %d, want 1", got)
	}
	// Unlinking never touches the external files.
	if line := requireTaskLine(t, readTaskPage(t, linkedPath), linkedID); !strings.Contains(line, "[owner:: Bob]") {
		t.Errorf("skip modified the unlinked file: %q", line)
	}

	// The older vault mutation is still reachable.
	result, err = app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("second UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || result.Remaining != 0 {
		t.Fatalf("older undo result = %+v, want the vault entry reversed", result)
	}
	if line := requireTaskLine(t, readTaskPage(t, vaultPath), vaultID); strings.Contains(line, "[owner::") {
		t.Errorf("older vault mutation was not reversed: %q", line)
	}
}

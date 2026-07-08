package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"silt/backend/db"
	"silt/backend/monitor"
	"silt/backend/parser"
	"silt/backend/plugins"
)

// taskLineForID returns the rendered line carrying the given block ID, or ""
// if not found. Used to scope substring assertions to the subject block so
// unrelated blocks (with their own tokens) don't false-positive.
func taskLineForID(content, id string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "<!-- id: "+id) {
			return line
		}
	}
	return ""
}

// TestTaskTagRegex_MirrorsIndexer pins taskTagRegex's pattern against
// backend/db.tagRegex so the byte-surgery stays in lockstep with the
// canonical indexer derivation.
func TestTaskTagRegex_MirrorsIndexer(t *testing.T) {
	// db.tagRegex is package-private; round-trip via ExtractTags on a probe
	// that exercises the canonical-name derivation (incl. TrimRight of "/"
	// and "-"). If the local regex disagrees with db's, ExtractTags will
	// report a different set than the local FindAllStringSubmatch.
	probe := "x #work/project #work- #alpha #1num"
	wantCanonical := db.ExtractTags(probe)
	matches := taskTagRegex.FindAllStringSubmatch(probe, -1)
	gotCanonical := make([]string, 0, len(matches))
	seen := make(map[string]bool)
	for _, m := range matches {
		c := strings.TrimRight(m[1], "/-")
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		gotCanonical = append(gotCanonical, c)
	}
	if len(wantCanonical) != len(gotCanonical) {
		t.Fatalf("regex drift: db.ExtractTags=%v local=%v", wantCanonical, gotCanonical)
	}
	for i := range wantCanonical {
		if wantCanonical[i] != gotCanonical[i] {
			t.Fatalf("regex drift at %d: db=%q local=%q", i, wantCanonical[i], gotCanonical[i])
		}
	}
}

// ---------------------------------------------------------------------------
// SetTaskOwner
// ---------------------------------------------------------------------------

func TestSetTaskOwner_SetAndClear(t *testing.T) {
	app := newTestApp(t)
	const id = "aaaa1212-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "OwnerSet", "2026-07-01", content)

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner set: %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	line := taskLineForID(string(updated), id)
	if !strings.Contains(line, "[owner:: Alice]") {
		t.Errorf("set: expected [owner:: Alice] token in line: %s", line)
	}

	// Round-trip: re-parse yields the value.
	blocks, _, _, _, perr := parser.ParseFileContent(string(updated), "W", "S", "OwnerSet", "2026-07-01", app.spacesPerTab)
	if perr != nil {
		t.Fatalf("re-parse: %v", perr)
	}
	var owner string
	for _, b := range blocks {
		if b.ID == id {
			owner = b.Owner
		}
	}
	if owner != "Alice" {
		t.Errorf("round-trip: expected owner=Alice, got %q", owner)
	}

	// Clear owner → token omitted.
	if err := app.SetTaskOwner(id, ""); err != nil {
		t.Fatalf("SetTaskOwner clear: %v", err)
	}
	updated, _ = os.ReadFile(filePath)
	line = taskLineForID(string(updated), id)
	if strings.Contains(line, "[owner::") {
		t.Errorf("clear: [owner::] token should be omitted in line: %s", line)
	}
}

func TestSetTaskOwner_RejectsNonTask(t *testing.T) {
	app := newTestApp(t)
	const note = "bbbb1212-1111-1111-1111-111111111111"
	content := "- a note <!-- id: " + note + " -->\n"
	indexTestFile(t, app, "W", "S", "OwnerNonTask", "2026-07-01", content)

	if err := app.SetTaskOwner(note, "Alice"); err == nil {
		t.Fatal("expected error setting owner on a non-task block, got nil")
	}
}

// ---------------------------------------------------------------------------
// SetTaskPriority
// ---------------------------------------------------------------------------

func TestSetTaskPriority_SetAndClear(t *testing.T) {
	app := newTestApp(t)
	const id = "cccc1212-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "PrioSet", "2026-07-01", content)

	if err := app.SetTaskPriority(id, 1); err != nil {
		t.Fatalf("SetTaskPriority set: %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	line := taskLineForID(string(updated), id)
	if !strings.Contains(line, "[priority:: 1]") {
		t.Errorf("set: expected [priority:: 1] token in line: %s", line)
	}

	// Round-trip: re-parse yields the value.
	blocks, _, _, _, perr := parser.ParseFileContent(string(updated), "W", "S", "PrioSet", "2026-07-01", app.spacesPerTab)
	if perr != nil {
		t.Fatalf("re-parse: %v", perr)
	}
	var prio int
	for _, b := range blocks {
		if b.ID == id {
			prio = b.Priority
		}
	}
	if prio != 1 {
		t.Errorf("round-trip: expected priority=1, got %d", prio)
	}

	// Clear priority → 0 omits the token (writer.go ~:1198 omit-when-0).
	if err := app.SetTaskPriority(id, 0); err != nil {
		t.Fatalf("SetTaskPriority clear: %v", err)
	}
	updated, _ = os.ReadFile(filePath)
	line = taskLineForID(string(updated), id)
	if strings.Contains(line, "[priority::") {
		t.Errorf("clear: [priority::] token should be omitted in line: %s", line)
	}
}

func TestSetTaskPriority_RejectsNonTask(t *testing.T) {
	app := newTestApp(t)
	const note = "dddd1212-1111-1111-1111-111111111111"
	content := "- a note <!-- id: " + note + " -->\n"
	indexTestFile(t, app, "W", "S", "PrioNonTask", "2026-07-01", content)

	if err := app.SetTaskPriority(note, 1); err == nil {
		t.Fatal("expected error setting priority on a non-task block, got nil")
	}
}

// ---------------------------------------------------------------------------
// SetTaskTags
// ---------------------------------------------------------------------------

func TestSetTaskTags_AddRemoveClear(t *testing.T) {
	app := newTestApp(t)
	const id = "eeee1212-1111-1111-1111-111111111111"
	// Use a real-form UUID for the ((ref)) so parser.BlockRefRegex catches it
	// (the regex is strict: 8-4-4-4-12 hex). `abc-123` is shorthand only.
	const refUUID = "01234567-89ab-cdef-0123-456789abcdef"
	prose := "Implement search #work/project ((" + refUUID + "))"
	content := "- [ ] " + prose + " <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "TagsSet", "2026-07-01", content)

	// Add #urgent alongside #work/project.
	if err := app.SetTaskTags(id, []string{"work/project", "urgent"}); err != nil {
		t.Fatalf("SetTaskTags add: %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	updatedStr := string(updated)
	line := taskLineForID(updatedStr, id)

	// Byte-preservation: prose + ref + kept tag survive.
	for _, want := range []string{
		"Implement search",
		"#work/project",
		"((" + refUUID + "))",
		"#urgent",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("add: expected %q in line: %s", want, line)
		}
	}
	// Re-parse re-derives exactly the new tag set.
	blocks, _, _, _, perr := parser.ParseFileContent(updatedStr, "W", "S", "TagsSet", "2026-07-01", app.spacesPerTab)
	if perr != nil {
		t.Fatalf("re-parse add: %v", perr)
	}
	for _, b := range blocks {
		if b.ID == id {
			got := db.ExtractTags(b.CleanText)
			if !sameSet(got, []string{"work/project", "urgent"}) {
				t.Errorf("add round-trip: expected tags [work/project urgent], got %v (cleantext=%q)", got, b.CleanText)
			}
			// The ((uuid)) ref MUST survive the tag surgery byte-for-byte.
			if !strings.Contains(b.CleanText, "(("+refUUID+"))") {
				t.Errorf("add round-trip: ref lost from CleanText: %q", b.CleanText)
			}
			// Prose MUST survive.
			if !strings.Contains(b.CleanText, "Implement search") {
				t.Errorf("add round-trip: prose lost from CleanText: %q", b.CleanText)
			}
		}
	}

	// Clear all tags → hashtags stripped, prose + ref intact.
	if err := app.SetTaskTags(id, nil); err != nil {
		t.Fatalf("SetTaskTags clear: %v", err)
	}
	updated, _ = os.ReadFile(filePath)
	line = taskLineForID(string(updated), id)
	if strings.Contains(line, "#work/project") || strings.Contains(line, "#urgent") {
		t.Errorf("clear: hashtags should be stripped in line: %s", line)
	}
	for _, want := range []string{
		"Implement search",
		"((" + refUUID + "))",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("clear: expected %q preserved in line: %s", want, line)
		}
	}
}

// TestSetTaskTags_DoesNotCorruptTokens verifies the [key::] tokens, the
// checkbox marker, and the identity comment survive tag surgery. The
// renderer re-emits these from ParsedBlock fields; tag surgery operates on
// CleanText only.
func TestSetTaskTags_DoesNotCorruptTokens(t *testing.T) {
	app := newTestApp(t)
	const id = "eeee1213-1111-1111-1111-111111111111"
	content := "- [ ] task #alpha [owner:: Bob] [due:: 2026-07-01] <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "TagsTokens", "2026-07-01", content)

	if err := app.SetTaskTags(id, []string{"beta"}); err != nil {
		t.Fatalf("SetTaskTags: %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	line := taskLineForID(string(updated), id)

	// #alpha stripped, #beta appended.
	if strings.Contains(line, "#alpha") {
		t.Errorf("expected #alpha stripped: %s", line)
	}
	if !strings.Contains(line, "#beta") {
		t.Errorf("expected #beta added: %s", line)
	}
	// [owner:: Bob], [due:: 2026-07-01], checkbox, identity comment intact.
	// The id comment carries an optional ` @ <fileDate>` suffix stamped by
	// the renderer when the block has no prior date; match the prefix.
	for _, want := range []string{
		"- [ ] task",
		"[owner:: Bob]",
		"[due:: 2026-07-01]",
		"<!-- id: " + id,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("expected %q preserved in line: %s", want, line)
		}
	}
}

func TestSetTaskTags_RejectsNonTask(t *testing.T) {
	app := newTestApp(t)
	const note = "ffff1212-1111-1111-1111-111111111111"
	content := "- a note #alpha <!-- id: " + note + " -->\n"
	indexTestFile(t, app, "W", "S", "TagsNonTask", "2026-07-01", content)

	if err := app.SetTaskTags(note, []string{"beta"}); err == nil {
		t.Fatal("expected error setting tags on a non-task block, got nil")
	}
}

// ---------------------------------------------------------------------------
// SetTaskTitle
// ---------------------------------------------------------------------------

func TestSetTaskTitle_PreservesTagsAndRefs(t *testing.T) {
	app := newTestApp(t)
	const id = "1212aaaa-1111-1111-1111-111111111111"
	const refUUID = "01234567-89ab-cdef-0123-456789abcdef"
	prose := "Implement search #work/project ((" + refUUID + "))"
	content := "- [ ] " + prose + " <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "TitleSet", "2026-07-01", content)

	if err := app.SetTaskTitle(id, "Build search v2"); err != nil {
		t.Fatalf("SetTaskTitle: %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	updatedStr := string(updated)
	line := taskLineForID(updatedStr, id)

	// New title present, hashtag + ref preserved verbatim.
	for _, want := range []string{
		"Build search v2",
		"#work/project",
		"((" + refUUID + "))",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("expected %q in line: %s", want, line)
		}
	}
	// Old prose MUST be gone.
	if strings.Contains(line, "Implement search") {
		t.Errorf("old prose should be replaced: %s", line)
	}

	// Round-trip: CleanText prose == "Build search v2"; tags + refs intact.
	blocks, _, _, _, perr := parser.ParseFileContent(updatedStr, "W", "S", "TitleSet", "2026-07-01", app.spacesPerTab)
	if perr != nil {
		t.Fatalf("re-parse: %v", perr)
	}
	for _, b := range blocks {
		if b.ID == id {
			if !strings.Contains(b.CleanText, "Build search v2") {
				t.Errorf("round-trip: title lost from CleanText: %q", b.CleanText)
			}
			tags := db.ExtractTags(b.CleanText)
			if !sameSet(tags, []string{"work/project"}) {
				t.Errorf("round-trip: expected tags [work/project], got %v (cleantext=%q)", tags, b.CleanText)
			}
			if !strings.Contains(b.CleanText, "(("+refUUID+"))") {
				t.Errorf("round-trip: ref lost from CleanText: %q", b.CleanText)
			}
		}
	}
}

// TestSetTaskTitle_DoesNotCorruptTokens verifies the checkbox marker,
// [key::] tokens, and identity comment survive a title rewrite. These live
// outside CleanText (the renderer re-emits them from ParsedBlock fields), so
// title surgery cannot touch them.
func TestSetTaskTitle_DoesNotCorruptTokens(t *testing.T) {
	app := newTestApp(t)
	const id = "1212aabb-1111-1111-1111-111111111111"
	content := "- [ ] original #alpha [owner:: Bob] [due:: 2026-07-01] <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "TitleTokens", "2026-07-01", content)

	if err := app.SetTaskTitle(id, "renamed"); err != nil {
		t.Fatalf("SetTaskTitle: %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	line := taskLineForID(string(updated), id)

	// New title present, old title gone.
	if !strings.Contains(line, "renamed") {
		t.Errorf("expected new title in line: %s", line)
	}
	if strings.Contains(line, "original") {
		t.Errorf("old title should be gone: %s", line)
	}
	// Hashtag survives (lives in CleanText, must be preserved).
	if !strings.Contains(line, "#alpha") {
		t.Errorf("expected #alpha preserved in line: %s", line)
	}
	// Checkbox + tokens + identity comment intact. The id comment may carry
	// an optional ` @ <fileDate>` suffix stamped by the renderer; match the
	// prefix.
	for _, want := range []string{
		"- [ ] renamed",
		"[owner:: Bob]",
		"[due:: 2026-07-01]",
		"<!-- id: " + id,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("expected %q preserved in line: %s", want, line)
		}
	}
}

func TestSetTaskTitle_RejectsNonTask(t *testing.T) {
	app := newTestApp(t)
	const note = "1212cccc-1111-1111-1111-111111111111"
	content := "- a note <!-- id: " + note + " -->\n"
	indexTestFile(t, app, "W", "S", "TitleNonTask", "2026-07-01", content)

	if err := app.SetTaskTitle(note, "renamed"); err == nil {
		t.Fatal("expected error setting title on a non-task block, got nil")
	}
}

// TestSetTaskTitle_RejectsEmptyTitle is the C2 SDK contract guard: a plugin
// calling setTaskTitle(id, "") (or whitespace-only) must NOT silently strip
// all prose from the task. replaceTitleInCleanText("", cleanText) would leave
// only #tags + ((uuid)) refs — a title-less block that still parses as a
// task. The backend is the contract surface for every plugin, so reject up
// front and leave the file untouched (no write, no block:changed emission).
func TestSetTaskTitle_RejectsEmptyTitle(t *testing.T) {
	cases := []struct {
		name  string
		title string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t)
			const id = "1212dddd-1111-1111-1111-111111111111"
			content := "- [ ] original prose #tag <!-- id: " + id + " -->\n"
			filePath := indexTestFile(t, app, "W", "S", "TitleEmpty", "2026-07-01", content)

			before, _ := os.ReadFile(filePath)

			err := app.SetTaskTitle(id, tc.title)
			if err == nil {
				t.Fatal("expected error for empty title, got nil")
			}

			after, _ := os.ReadFile(filePath)
			if string(before) != string(after) {
				t.Errorf("file must NOT be written on rejected title\n--- before ---\n%s\n--- after ---\n%s", before, after)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Index reflection: tags + owner + priority round-trip through SQLite.
// ---------------------------------------------------------------------------

func TestSetTaskOwner_IndexReflectsChange(t *testing.T) {
	app := newTestApp(t)
	const id = "3434aaaa-1111-1111-1111-111111111111"
	content := "- [ ] task <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "OwnerIdx", "2026-07-01", content)

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}
	tasks, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasks: %v", err)
	}
	for _, tk := range tasks {
		if tk.ID == id {
			if tk.Owner != "Alice" {
				t.Errorf("expected owner=Alice in index, got %q", tk.Owner)
			}
			return
		}
	}
	t.Errorf("subject %s not returned by QueryTasks", id)
}

func TestSetTaskPriority_IndexReflectsChange(t *testing.T) {
	app := newTestApp(t)
	const id = "3434bbbb-1111-1111-1111-111111111111"
	content := "- [ ] task <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "PrioIdx", "2026-07-01", content)

	if err := app.SetTaskPriority(id, 2); err != nil {
		t.Fatalf("SetTaskPriority: %v", err)
	}
	tasks, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasks: %v", err)
	}
	for _, tk := range tasks {
		if tk.ID == id {
			if tk.Priority != 2 {
				t.Errorf("expected priority=2 in index, got %d", tk.Priority)
			}
			return
		}
	}
	t.Errorf("subject %s not returned by QueryTasks", id)
}

func TestSetTaskTags_IndexReflectsChange(t *testing.T) {
	app := newTestApp(t)
	const id = "3434cccc-1111-1111-1111-111111111111"
	content := "- [ ] task #alpha <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "TagsIdx", "2026-07-01", content)

	if err := app.SetTaskTags(id, []string{"beta", "gamma"}); err != nil {
		t.Fatalf("SetTaskTags: %v", err)
	}
	tasks, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasks: %v", err)
	}
	for _, tk := range tasks {
		if tk.ID == id {
			if !sameSet(tk.Tags, []string{"beta", "gamma"}) {
				t.Errorf("expected tags [beta gamma] in index, got %v", tk.Tags)
			}
			return
		}
	}
	t.Errorf("subject %s not returned by QueryTasks", id)
}

// ---------------------------------------------------------------------------
// sameSet helper
// ---------------------------------------------------------------------------

// sameSet reports whether two string slices hold the same set (order
// ignored). Used to compare tag derivations where the canonical order is
// not contractually fixed.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		if !set[s] {
			return false
		}
	}
	return true
}

// TestDedupeTags_StripsLeadingHashAndDedupes locks the contract-boundary
// normalization in dedupeTags: a single leading "#" is stripped (so plugin
// SDK callers may pass "#work" or "work" interchangeably), the dedup runs
// against the post-strip name (so "#work" + "work" collapse to one entry),
// and empties are dropped.
func TestDedupeTags_StripsLeadingHashAndDedupes(t *testing.T) {
	got := dedupeTags([]string{"#work", "work", "#urgent", ""})
	want := []string{"work", "urgent"}
	if len(got) != len(want) {
		t.Fatalf("dedupeTags = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dedupeTags[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// ---------------------------------------------------------------------------
// SetTaskOrder (#426)
// ---------------------------------------------------------------------------

// TestSetTaskOrder_RewritesToken stamps a positive [order:: N] onto a task,
// asserts the rendered line carries the token, the index row re-derives
// manual_order=N, and a parse → render → parse round trip is byte-stable.
func TestSetTaskOrder_RewritesToken(t *testing.T) {
	app := newTestApp(t)
	const id = "5656aaaa-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "OrderSet", "2026-07-01", content)

	if err := app.SetTaskOrder(id, 7); err != nil {
		t.Fatalf("SetTaskOrder set: %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	updatedStr := string(updated)
	line := taskLineForID(updatedStr, id)
	if !strings.Contains(line, "[order:: 7]") {
		t.Errorf("set: expected [order:: 7] token in line: %s", line)
	}

	// Round-trip through the parser yields the value, and a second
	// parse→render→parse stays byte-stable (the omit-when-0 + fixed token
	// position let the renderer produce an idempotent file).
	blocks, _, _, _, perr := parser.ParseFileContent(updatedStr, "W", "S", "OrderSet", "2026-07-01", app.spacesPerTab)
	if perr != nil {
		t.Fatalf("re-parse: %v", perr)
	}
	var order int
	for _, b := range blocks {
		if b.ID == id {
			order = b.ManualOrder
		}
	}
	if order != 7 {
		t.Errorf("round-trip: expected manual_order=7, got %d", order)
	}

	// Render→parse byte-stability: re-rendering the parsed blocks must
	// produce a file whose task line is unchanged (the token set + order
	// are deterministic on output).
	frontmatter, body := parser.SplitFrontmatter(updatedStr)
	rerendered := parser.RenderFileContent(blocks, body, frontmatter, app.spacesPerTab)
	rerenderedLine := taskLineForID(rerendered, id)
	if rerenderedLine != line {
		t.Errorf("byte-stability: rendered line drifted\n first:  %s\n second: %s", line, rerenderedLine)
	}

	// Index reflection: the tasks.manual_order cache holds the new value.
	tasks, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasks: %v", err)
	}
	for _, tk := range tasks {
		if tk.ID == id {
			if tk.ManualOrder != 7 {
				t.Errorf("index: expected manual_order=7, got %d", tk.ManualOrder)
			}
			return
		}
	}
	t.Errorf("subject %s not returned by QueryTasks", id)
}

// TestSetTaskOrder_ClearsToken verifies that passing 0 (the omit-when-0
// sentinel) strips the [order::] token from the file.
func TestSetTaskOrder_ClearsToken(t *testing.T) {
	app := newTestApp(t)
	const id = "5656bbbb-1111-1111-1111-111111111111"
	content := "- [ ] ship [order:: 5] <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "OrderClear", "2026-07-01", content)

	if err := app.SetTaskOrder(id, 0); err != nil {
		t.Fatalf("SetTaskOrder clear: %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	line := taskLineForID(string(updated), id)
	if strings.Contains(line, "[order::") {
		t.Errorf("clear: [order::] token should be omitted in line: %s", line)
	}
}

// TestSetTaskOrder_NegativeRejects is the contract guard: a negative order
// is a UI bug, not user intent. The backend is the contract surface for
// every plugin + the in-app reorder, so reject up front and leave the
// file untouched (no write, no block:changed emission).
func TestSetTaskOrder_NegativeRejects(t *testing.T) {
	app := newTestApp(t)
	const id = "5656cccc-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "OrderNegative", "2026-07-01", content)

	before, _ := os.ReadFile(filePath)
	if err := app.SetTaskOrder(id, -1); err == nil {
		t.Fatal("expected error for negative order, got nil")
	}
	after, _ := os.ReadFile(filePath)
	if string(before) != string(after) {
		t.Errorf("file must NOT be written on rejected order\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

// TestSetTaskOrder_OverMillionRejects mirrors the negative-rejection guard:
// the 1,000,001 ceiling is a contract violation (not user intent), so the
// backend rejects up front and leaves the file untouched.
func TestSetTaskOrder_OverMillionRejects(t *testing.T) {
	app := newTestApp(t)
	const id = "5656ccdd-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "OrderOverMillion", "2026-07-01", content)

	before, _ := os.ReadFile(filePath)
	if err := app.SetTaskOrder(id, 1_000_001); err == nil {
		t.Fatal("expected error for order > 1,000,000, got nil")
	}
	after, _ := os.ReadFile(filePath)
	if string(before) != string(after) {
		t.Errorf("file must NOT be written on rejected order\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

// TestPluginSetTaskOrder_GatedByCapability mirrors the content-mutate gate
// pattern: a third-party plugin without the grant is denied; the same
// plugin with content-mutate succeeds and the file gets the token.
func TestPluginSetTaskOrder_GatedByCapability(t *testing.T) {
	app := newTestApp(t)
	const id = "5656dddd-1111-1111-1111-111111111111"
	content := "- [ ] gated <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "OrderGated", "2026-07-01", content)

	tok := registerTestSession(t, app, "third-party")
	// Without the content-mutate grant: rejected, file untouched.
	before, _ := os.ReadFile(filePath)
	if _, err := app.PluginSetTaskOrder("third-party", tok, id, 3); err == nil {
		t.Fatal("expected capability denial without content-mutate grant")
	}
	after, _ := os.ReadFile(filePath)
	if string(before) != string(after) {
		t.Errorf("file must NOT be written on a denied call\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}

	// Grant content-mutate; the same call now succeeds and lands the token.
	if err := app.RequestCapability("third-party", string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("grant: %v", err)
	}
	ok, err := app.PluginSetTaskOrder("third-party", tok, id, 3)
	if err != nil || !ok {
		t.Fatalf("PluginSetTaskOrder with grant: ok=%v err=%v", ok, err)
	}
	updated, _ := os.ReadFile(filePath)
	if !strings.Contains(taskLineForID(string(updated), id), "[order:: 3]") {
		t.Errorf("granted call should have stamped [order:: 3]: %s", taskLineForID(string(updated), id))
	}
}

// TestSetTaskOrders_RewritesMultipleInOneFile stamps [order:: N] on three
// tasks in the same file in one atomic write. All three tokens must land in
// the rendered output, the index must reflect all three, and the file must
// have been written exactly once (one read-modify-write cycle for the group).
func TestSetTaskOrders_RewritesMultipleInOneFile(t *testing.T) {
	app := newTestApp(t)
	const (
		id1 = "5656e111-1111-1111-1111-111111111111"
		id2 = "5656e222-1111-1111-1111-111111111111"
		id3 = "5656e333-1111-1111-1111-111111111111"
	)
	content := "- [ ] first <!-- id: " + id1 + " -->\n" +
		"- [ ] second <!-- id: " + id2 + " -->\n" +
		"- [ ] third <!-- id: " + id3 + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "OrderBatch", "2026-07-01", content)

	ids := []string{id1, id2, id3}
	orders := []int{3, 1, 2}
	if err := app.SetTaskOrders(ids, orders); err != nil {
		t.Fatalf("SetTaskOrders: %v", err)
	}

	updated, _ := os.ReadFile(filePath)
	updatedStr := string(updated)
	for i, id := range ids {
		line := taskLineForID(updatedStr, id)
		want := strings.Contains(line, "[order:: ")
		if !want {
			t.Errorf("id[%d] %s: expected [order:: %d] in line: %s", i, id, orders[i], line)
			continue
		}
		expected := fmt.Sprintf("[order:: %d]", orders[i])
		if !strings.Contains(line, expected) {
			t.Errorf("id[%d] %s: expected %s in line: %s", i, id, expected, line)
		}
	}

	// Index reflects all three.
	tasks, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasks: %v", err)
	}
	orderByID := make(map[string]int)
	for _, tk := range tasks {
		orderByID[tk.ID] = tk.ManualOrder
	}
	for i, id := range ids {
		if orderByID[id] != orders[i] {
			t.Errorf("index: id %s expected manual_order=%d, got %d", id, orders[i], orderByID[id])
		}
	}
}

// TestSetTaskOrders_MismatchedLengthRejects is the contract guard: parallel
// slices must have equal length.
func TestSetTaskOrders_MismatchedLengthRejects(t *testing.T) {
	app := newTestApp(t)
	const id = "5656f111-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "OrderBatchMismatch", "2026-07-01", content)

	if err := app.SetTaskOrders([]string{id}, []int{1, 2}); err == nil {
		t.Fatal("expected error for mismatched lengths, got nil")
	}
}

// TestSetTaskOrders_EmptyIsNoOp verifies that empty slices return nil without
// touching disk.
func TestSetTaskOrders_EmptyIsNoOp(t *testing.T) {
	app := newTestApp(t)
	if err := app.SetTaskOrders(nil, nil); err != nil {
		t.Fatalf("empty SetTaskOrders should be a no-op: %v", err)
	}
	if err := app.SetTaskOrders([]string{}, []int{}); err != nil {
		t.Fatalf("empty SetTaskOrders should be a no-op: %v", err)
	}
}

// TestPluginSetTaskOrders_GatedByCapability mirrors the individual gate test.
func TestPluginSetTaskOrders_GatedByCapability(t *testing.T) {
	app := newTestApp(t)
	const id1 = "5656aaaa-2222-1111-1111-111111111111"
	const id2 = "5656bbbb-2222-1111-1111-111111111111"
	content := "- [ ] a <!-- id: " + id1 + " -->\n- [ ] b <!-- id: " + id2 + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "OrderBatchGated", "2026-07-01", content)

	tok := registerTestSession(t, app, "third-party")
	before, _ := os.ReadFile(filePath)
	if _, err := app.PluginSetTaskOrders("third-party", tok, []string{id1, id2}, []int{2, 1}); err == nil {
		t.Fatal("expected capability denial without content-mutate grant")
	}
	after, _ := os.ReadFile(filePath)
	if string(before) != string(after) {
		t.Errorf("file must NOT be written on a denied call")
	}

	if err := app.RequestCapability("third-party", string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("grant: %v", err)
	}
	ok, err := app.PluginSetTaskOrders("third-party", tok, []string{id1, id2}, []int{2, 1})
	if err != nil || !ok {
		t.Fatalf("PluginSetTaskOrders with grant: ok=%v err=%v", ok, err)
	}
	updated, _ := os.ReadFile(filePath)
	if !strings.Contains(taskLineForID(string(updated), id1), "[order:: 2]") {
		t.Errorf("granted batch should have stamped [order:: 2] on id1")
	}
	if !strings.Contains(taskLineForID(string(updated), id2), "[order:: 1]") {
		t.Errorf("granted batch should have stamped [order:: 1] on id2")
	}
}

// TestSetTaskOrders_DuplicateIDLastOrderWins pins the round-3 fix that
// changed len(group) → len(orderByID): a duplicate id in the input produces
// two pending entries but only one block to find, so the old check reported a
// phantom shortfall and errored. With the map-dedup, the last value wins
// (map assignment is last-write-wins) and no error is returned.
func TestSetTaskOrders_DuplicateIDLastOrderWins(t *testing.T) {
	app := newTestApp(t)
	const id = "5656ee11-2222-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "OrderDupID", "2026-07-01", content)

	if err := app.SetTaskOrders([]string{id, id}, []int{5, 10}); err != nil {
		t.Fatalf("SetTaskOrders duplicate id: %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	line := taskLineForID(string(updated), id)
	// Last value wins via map dedup (orderByID[id] = 5, then overwritten to 10).
	if !strings.Contains(line, "[order:: 10]") {
		t.Errorf("expected [order:: 10] (last value wins) in line: %s", line)
	}

	tasks, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasks: %v", err)
	}
	for _, tk := range tasks {
		if tk.ID == id {
			if tk.ManualOrder != 10 {
				t.Errorf("index: expected manual_order=10, got %d", tk.ManualOrder)
			}
			return
		}
	}
	t.Errorf("subject %s not returned by QueryTasks", id)
}

// TestSetTaskOrders_WritesEachFileAtomically verifies the per-file atomicity
// contract: tasks spread across two different files (different page names)
// each get their own read-parse-render-write-reindex cycle, so both files are
// written and the index reflects all orders.
func TestSetTaskOrders_WritesEachFileAtomically(t *testing.T) {
	app := newTestApp(t)
	const (
		idA1 = "5656ff11-2222-1111-1111-111111111111"
		idA2 = "5656ff22-2222-1111-1111-111111111111"
		idB1 = "5656ff33-2222-1111-1111-111111111111"
	)
	contentA := "- [ ] first <!-- id: " + idA1 + " -->\n" +
		"- [ ] second <!-- id: " + idA2 + " -->\n"
	contentB := "- [ ] third <!-- id: " + idB1 + " -->\n"
	// Different page names → different files (same notebook/section).
	fileA := indexTestFile(t, app, "W", "S", "OrderMultiA", "2026-07-01", contentA)
	fileB := indexTestFile(t, app, "W", "S", "OrderMultiB", "2026-07-01", contentB)

	ids := []string{idA1, idA2, idB1}
	orders := []int{2, 1, 3}
	if err := app.SetTaskOrders(ids, orders); err != nil {
		t.Fatalf("SetTaskOrders multi-file: %v", err)
	}

	// Both files must carry their respective [order::] tokens.
	updatedA, _ := os.ReadFile(fileA)
	updatedB, _ := os.ReadFile(fileB)
	strA, strB := string(updatedA), string(updatedB)
	if want := "[order:: 2]"; !strings.Contains(taskLineForID(strA, idA1), want) {
		t.Errorf("file A idA1: expected %s in line: %s", want, taskLineForID(strA, idA1))
	}
	if want := "[order:: 1]"; !strings.Contains(taskLineForID(strA, idA2), want) {
		t.Errorf("file A idA2: expected %s in line: %s", want, taskLineForID(strA, idA2))
	}
	if want := "[order:: 3]"; !strings.Contains(taskLineForID(strB, idB1), want) {
		t.Errorf("file B idB1: expected %s in line: %s", want, taskLineForID(strB, idB1))
	}

	// Index reflects all three orders across both files.
	tasks, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasks: %v", err)
	}
	orderByID := make(map[string]int)
	for _, tk := range tasks {
		orderByID[tk.ID] = tk.ManualOrder
	}
	for i, id := range ids {
		if orderByID[id] != orders[i] {
			t.Errorf("index: id %s expected manual_order=%d, got %d", id, orders[i], orderByID[id])
		}
	}
}

// TestSetTaskOrders_ConcurrentWithSetTaskOwner_NoLostUpdate verifies the
// per-file write lock serializes concurrent same-file mutations from
// different entry points. SetTaskOrders([A,B],[1,2]) locks block A then the
// file; SetTaskOwner(B,"Alice") locks block B then the same file. The file
// lock must serialize them so neither mutation is lost (the second writer
// re-reads the post-first-write file content). Run with -race to also catch
// any unsynchronized memory access.
func TestSetTaskOrders_ConcurrentWithSetTaskOwner_NoLostUpdate(t *testing.T) {
	app := newTestApp(t)
	const (
		idA = "5656aaaa-4444-1111-1111-111111111111"
		idB = "5656bbbb-4444-1111-1111-111111111111"
	)
	content := "- [ ] first <!-- id: " + idA + " -->\n" +
		"- [ ] second <!-- id: " + idB + " -->\n"
	indexTestFile(t, app, "W", "S", "ConcurrentOrderOwner", "2026-07-01", content)

	// Channel barrier: both goroutines block until close(barrier), then race
	// to acquire the file write lock. No real sleeping — the barrier is the
	// only synchronization point.
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	var ordersErr, ownerErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-barrier
		ordersErr = app.SetTaskOrders([]string{idA, idB}, []int{1, 2})
	}()
	go func() {
		defer wg.Done()
		<-barrier
		ownerErr = app.SetTaskOwner(idB, "Alice")
	}()
	close(barrier)
	wg.Wait()

	if ordersErr != nil {
		t.Fatalf("SetTaskOrders: %v", ordersErr)
	}
	if ownerErr != nil {
		t.Fatalf("SetTaskOwner: %v", ownerErr)
	}

	tasks, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasks: %v", err)
	}
	for _, tk := range tasks {
		switch tk.ID {
		case idA:
			if tk.ManualOrder != 1 {
				t.Errorf("A: expected manual_order=1, got %d", tk.ManualOrder)
			}
		case idB:
			if tk.ManualOrder != 2 {
				t.Errorf("B: expected manual_order=2, got %d", tk.ManualOrder)
			}
			if tk.Owner != "Alice" {
				t.Errorf("B: expected owner=Alice, got %q", tk.Owner)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Focus-lock guard (#444)
//
// mutateTaskBlock + setTaskOrders refuse to write a file the user is actively
// editing (mirrors MutateBlock's errBlockBeingEdited). PluginSetTaskDueDate +
// PluginUpdateTaskMeta inherit the guard after the #476 refactor onto
// mutateTaskBlock — the inheritance test pins that.
// ---------------------------------------------------------------------------

// withFocusLockWatcher wires a real DirectoryWatcher onto the test app so
// AcquireFocusLock/IsFocusLocked behave as in production. Mirrors the setup in
// TestFocusLocking_AcquireAndRelease (app_api_test.go).
func withFocusLockWatcher(t *testing.T, app *App) {
	t.Helper()
	watcher, err := monitor.NewDirectoryWatcher(app.vaultPath, app.db, app.tracker, app.coordinator, app.spacesPerTab)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher failed: %v", err)
	}
	app.watcher = watcher
}

func TestSetTaskOwner_RefusedWhileFocusLocked(t *testing.T) {
	app := newTestApp(t)
	withFocusLockWatcher(t, app)
	const id = "f1f1aaaa-4444-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "FocusLockOwner", "2026-07-01", content)

	if err := app.AcquireFocusLock("W", "S", "FocusLockOwner"); err != nil {
		t.Fatalf("AcquireFocusLock: %v", err)
	}
	// While the editor holds the focus lock, the setter must refuse rather than
	// clobber the in-flight edit.
	if err := app.SetTaskOwner(id, "Alice"); !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited while focus-locked, got %v", err)
	}
	// After release the write succeeds.
	if err := app.ReleaseFocusLock("W", "S", "FocusLockOwner"); err != nil {
		t.Fatalf("ReleaseFocusLock: %v", err)
	}
	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner after release: %v", err)
	}
}

func TestSetTaskOrders_RefusedWhileFocusLocked(t *testing.T) {
	app := newTestApp(t)
	withFocusLockWatcher(t, app)
	const (
		idA = "f1f1bbbb-4444-1111-1111-111111111111"
		idB = "f1f1cccc-4444-1111-1111-111111111111"
	)
	content := "- [ ] first <!-- id: " + idA + " -->\n" +
		"- [ ] second <!-- id: " + idB + " -->\n"
	indexTestFile(t, app, "W", "S", "FocusLockOrders", "2026-07-01", content)

	if err := app.AcquireFocusLock("W", "S", "FocusLockOrders"); err != nil {
		t.Fatalf("AcquireFocusLock: %v", err)
	}
	// The batch reorder path (setTaskOrders) does NOT route through
	// mutateTaskBlock — the guard is added there independently. A DnD reorder
	// on a focused file is the highest-risk clobber, so it must refuse too.
	if err := app.SetTaskOrders([]string{idA, idB}, []int{1, 2}); !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited while focus-locked, got %v", err)
	}
	if err := app.ReleaseFocusLock("W", "S", "FocusLockOrders"); err != nil {
		t.Fatalf("ReleaseFocusLock: %v", err)
	}
	if err := app.SetTaskOrders([]string{idA, idB}, []int{1, 2}); err != nil {
		t.Fatalf("SetTaskOrders after release: %v", err)
	}
}

// TestSetTaskOrders_NoPartialApplyAcrossFiles pins the pre-scan: a batch
// reorder spanning multiple files must refuse the WHOLE batch up front if any
// target file is focus-locked. Without the pre-scan, the per-file write loop
// would write the first file(s) before a later locked file returns
// errBlockBeingEdited, leaving the reorder half-applied on disk.
func TestSetTaskOrders_NoPartialApplyAcrossFiles(t *testing.T) {
	app := newTestApp(t)
	withFocusLockWatcher(t, app)
	const (
		idA = "f2f2aaaa-4444-1111-1111-111111111111"
		idB = "f2f2bbbb-4444-1111-1111-111111111111"
	)
	contentA := "- [ ] first <!-- id: " + idA + " -->\n"
	contentB := "- [ ] second <!-- id: " + idB + " -->\n"
	// Two separate files, each with one task. The batch spans both.
	indexTestFile(t, app, "W", "S", "OrdersFileA", "2026-07-01", contentA)
	indexTestFile(t, app, "W", "S", "OrdersFileB", "2026-07-01", contentB)

	// Lock ONLY file B. The pre-scan must refuse the whole batch before any
	// write — otherwise file A gets its order applied before file B aborts.
	if err := app.AcquireFocusLock("W", "S", "OrdersFileB"); err != nil {
		t.Fatalf("AcquireFocusLock: %v", err)
	}
	if err := app.SetTaskOrders([]string{idA, idB}, []int{1, 2}); !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited while file B focus-locked, got %v", err)
	}

	// File A must be byte-for-byte unchanged: the pre-scan refused before any
	// file write. This assertion fails on the partial-apply bug (file A would
	// carry the freshly-written [order:: 1] token).
	got, err := os.ReadFile(filepath.Join(app.vaultPath, "W", "S", "OrdersFileA.md"))
	if err != nil {
		t.Fatalf("read file A: %v", err)
	}
	if string(got) != contentA {
		t.Errorf("file A partially written despite file B being focus-locked:\nwant %q\ngot  %q", contentA, string(got))
	}

	if err := app.ReleaseFocusLock("W", "S", "OrdersFileB"); err != nil {
		t.Fatalf("ReleaseFocusLock: %v", err)
	}
	// After release the whole batch applies cleanly.
	if err := app.SetTaskOrders([]string{idA, idB}, []int{1, 2}); err != nil {
		t.Fatalf("SetTaskOrders after release: %v", err)
	}
}

// TestPluginSetTaskDueDate_RefusedWhileFocusLocked pins the #476 refactor's
// inheritance: PluginSetTaskDueDate used to inline the write chain; now it
// delegates to mutateTaskBlock, so it inherits the #444 focus-lock guard for
// free. If someone re-inlines the chain, this test fails.
func TestPluginSetTaskDueDate_RefusedWhileFocusLocked(t *testing.T) {
	app := newTestApp(t)
	withFocusLockWatcher(t, app)
	const id = "f1f1dddd-4444-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "FocusLockDue", "2026-07-01", content)

	// PluginSetTaskDueDate's session/grant gates need a registered plugin
	// session; bypass them by exercising the underlying mutation through the
	// app-level path that the refactored Plugin* wrapper delegates to. We
	// confirm the guard is inherited by calling the plugin path's shared core
	// (mutateTaskBlock) indirectly via SetTaskOwner, AND directly assert the
	// plugin entry returns errBlockBeingEdited by stubbing a valid session.
	if err := app.AcquireFocusLock("W", "S", "FocusLockDue"); err != nil {
		t.Fatalf("AcquireFocusLock: %v", err)
	}
	// Direct delegation check: the refactored PluginSetTaskDueDate calls
	// mutateTaskBlock, which now guards. Use a real plugin session + grant so
	// the session/grant gates pass and the focus-lock guard is the only blocker.
	tok := registerTestSession(t, app, "focus-plugin")
	if err := app.RequestCapability("focus-plugin", string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("grant: %v", err)
	}
	ok, err := app.PluginSetTaskDueDate("focus-plugin", tok, id, "2026-08-01")
	if ok {
		t.Fatalf("expected ok=false while focus-locked")
	}
	if !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited from refactored PluginSetTaskDueDate, got %v", err)
	}
	if err := app.ReleaseFocusLock("W", "S", "FocusLockDue"); err != nil {
		t.Fatalf("ReleaseFocusLock: %v", err)
	}
	ok, err = app.PluginSetTaskDueDate("focus-plugin", tok, id, "2026-08-01")
	if err != nil || !ok {
		t.Fatalf("PluginSetTaskDueDate after release: ok=%v err=%v", ok, err)
	}
}

// TestPluginUpdateTaskMeta_RefusedWhileFocusLocked pins the same inheritance
// for the other refactored caller (#476). Also confirms the refactor preserved
// the no-op short-circuit (pin=-1,progress=-1 returns ok without touching disk
// — no focus-lock error even while locked).
func TestPluginUpdateTaskMeta_RefusedWhileFocusLocked(t *testing.T) {
	app := newTestApp(t)
	withFocusLockWatcher(t, app)
	const id = "f1f1eeee-4444-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "FocusLockMeta", "2026-07-01", content)
	tok := registerTestSession(t, app, "focus-plugin")
	// PluginUpdateTaskMeta is now capability-gated; grant content-mutate so the
	// focus-lock guard is the only blocker (not the capability gate).
	if err := app.RequestCapability("focus-plugin", string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if err := app.AcquireFocusLock("W", "S", "FocusLockMeta"); err != nil {
		t.Fatalf("AcquireFocusLock: %v", err)
	}
	// No-op short-circuit must NOT trip the guard: it writes nothing, so a
	// focus lock is irrelevant. Preserves the pre-refactor behavior.
	if ok, err := app.PluginUpdateTaskMeta("focus-plugin", tok, id, -1, -1); err != nil || !ok {
		t.Fatalf("no-op PluginUpdateTaskMeta while locked: ok=%v err=%v", ok, err)
	}
	// A real update inherits the focus-lock guard from mutateTaskBlock.
	if ok, err := app.PluginUpdateTaskMeta("focus-plugin", tok, id, 1, 50); ok {
		t.Fatalf("expected ok=false while focus-locked")
	} else if !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited from refactored PluginUpdateTaskMeta, got %v", err)
	}
	if err := app.ReleaseFocusLock("W", "S", "FocusLockMeta"); err != nil {
		t.Fatalf("ReleaseFocusLock: %v", err)
	}
	if ok, err := app.PluginUpdateTaskMeta("focus-plugin", tok, id, 1, 50); err != nil || !ok {
		t.Fatalf("PluginUpdateTaskMeta after release: ok=%v err=%v", ok, err)
	}
}

// ---------------------------------------------------------------------------
// Focus-lock guard on the remaining task writers (#444 consistency follow-up).
//
// UpdateBlockState (status), SetTaskRecurrence, and SetTaskBlockedBy are task
// mutations that previously wrote without the IsFocusLocked check, leaving the
// drawer's status / recurrence / dependency edits able to clobber an actively
// edited file while owner/priority/tags/title/due/order were already guarded.
// These pin the guard on each path (mirrors TestSetTaskOwner_RefusedWhileFocusLocked).
// ---------------------------------------------------------------------------

func TestUpdateBlockState_RefusedWhileFocusLocked(t *testing.T) {
	app := newTestApp(t)
	withFocusLockWatcher(t, app)
	const id = "f1f1ffff-4444-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "FocusLockStatus", "2026-07-01", content)

	if err := app.AcquireFocusLock("W", "S", "FocusLockStatus"); err != nil {
		t.Fatalf("AcquireFocusLock: %v", err)
	}
	// A status change on a focus-locked file must refuse, not clobber the edit.
	if err := app.UpdateBlockState(id, "DONE"); !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited while focus-locked, got %v", err)
	}
	if err := app.ReleaseFocusLock("W", "S", "FocusLockStatus"); err != nil {
		t.Fatalf("ReleaseFocusLock: %v", err)
	}
	if err := app.UpdateBlockState(id, "DONE"); err != nil {
		t.Fatalf("UpdateBlockState after release: %v", err)
	}
}

func TestSetTaskRecurrence_RefusedWhileFocusLocked(t *testing.T) {
	app := newTestApp(t)
	withFocusLockWatcher(t, app)
	const id = "f1f10000-4444-1111-1111-111111111111"
	// Recurrence requires a due-date anchor (the resolver advances [due::]).
	content := "- [ ] ship [due:: 2026-08-01] <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "FocusLockRecur", "2026-07-01", content)

	if err := app.AcquireFocusLock("W", "S", "FocusLockRecur"); err != nil {
		t.Fatalf("AcquireFocusLock: %v", err)
	}
	if err := app.SetTaskRecurrence(id, "every week"); !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited while focus-locked, got %v", err)
	}
	if err := app.ReleaseFocusLock("W", "S", "FocusLockRecur"); err != nil {
		t.Fatalf("ReleaseFocusLock: %v", err)
	}
	if err := app.SetTaskRecurrence(id, "every week"); err != nil {
		t.Fatalf("SetTaskRecurrence after release: %v", err)
	}
}

func TestSetTaskBlockedBy_RefusedWhileFocusLocked(t *testing.T) {
	app := newTestApp(t)
	withFocusLockWatcher(t, app)
	const (
		id     = "f1f10001-4444-1111-1111-111111111111"
		prereq = "f1f10002-4444-1111-1111-111111111112"
	)
	content := "- [ ] prereq <!-- id: " + prereq + " -->\n" +
		"- [ ] ship <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "FocusLockBlocked", "2026-07-01", content)

	if err := app.AcquireFocusLock("W", "S", "FocusLockBlocked"); err != nil {
		t.Fatalf("AcquireFocusLock: %v", err)
	}
	if err := app.SetTaskBlockedBy(id, []string{prereq}); !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited while focus-locked, got %v", err)
	}
	if err := app.ReleaseFocusLock("W", "S", "FocusLockBlocked"); err != nil {
		t.Fatalf("ReleaseFocusLock: %v", err)
	}
	if err := app.SetTaskBlockedBy(id, []string{prereq}); err != nil {
		t.Fatalf("SetTaskBlockedBy after release: %v", err)
	}
}

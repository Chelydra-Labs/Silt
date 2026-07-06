package main

import (
	"os"
	"strings"
	"testing"

	"silt/backend/db"
	"silt/backend/parser"
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

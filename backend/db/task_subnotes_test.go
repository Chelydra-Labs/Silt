package db

// Prose sub-note presence cache (#1173).
//
// has_subnotes is derived at index time as the complement of
// parser.IsCommentBlock under the same nearest-TASK ownership walk that feeds
// comments_count, so the two columns partition one classification pass. These
// tests pin the derivation matrix and both read paths; the app-level
// conversion test pins that converting a block between the two shapes moves
// the columns together on re-index.

import (
	"fmt"
	"testing"

	"silt/backend/parser"
)

const (
	subnoteRootTaskID  = "c0de0001-2222-4222-8222-222222222222"
	subnoteSubTaskID   = "c0de0002-2222-4222-8222-222222222222"
	subnoteProseID     = "c0de0003-2222-4222-8222-222222222222"
	subnoteMarkerID    = "c0de0004-2222-4222-8222-222222222222"
	subnoteAttribID    = "c0de0005-2222-4222-8222-222222222222"
	subnoteChainAID    = "c0de0006-2222-4222-8222-222222222222"
	subnoteChainBID    = "c0de0007-2222-4222-8222-222222222222"
	subnoteSubProseID  = "c0de0008-2222-4222-8222-222222222222"
	subnoteTaggedTask  = "c0de0009-2222-4222-8222-222222222222"
	subnoteTaggedNote  = "c0de000a-2222-4222-8222-222222222222"
	subnoteCommentTask = "c0de000b-2222-4222-8222-222222222222"
	subnoteCommentID   = "c0de000c-2222-4222-8222-222222222222"
)

// derivedWant is the expected pair of derived columns for one task.
type derivedWant struct{ comments, subnotes int }

func subnoteTaskLine(id string) string {
	return "- [ ] task <!-- id: " + id + " -->\n"
}

func subnoteNoteLine(text, id string) string {
	return "    - " + text + " <!-- id: " + id + " -->\n"
}

// TestTaskDerivedCounts_SubnotePresence pins the classification matrix: the
// comment predicate owns the comments column, its complement owns the
// sub-notes column, and neither ever reports an owner the other would not.
func TestTaskDerivedCounts_SubnotePresence(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want map[string]derivedWant
	}{
		{
			name: "prose only",
			doc:  subnoteTaskLine(subnoteRootTaskID) + subnoteNoteLine("prose body", subnoteProseID),
			want: map[string]derivedWant{subnoteRootTaskID: {comments: 0, subnotes: 1}},
		},
		{
			name: "marker-only comment",
			doc:  subnoteTaskLine(subnoteRootTaskID) + subnoteNoteLine("marked body [comment::]", subnoteMarkerID),
			want: map[string]derivedWant{subnoteRootTaskID: {comments: 1, subnotes: 0}},
		},
		{
			name: "attribution-only comment",
			doc:  subnoteTaskLine(subnoteRootTaskID) + subnoteNoteLine("legacy body [author:: Dana]", subnoteAttribID),
			want: map[string]derivedWant{subnoteRootTaskID: {comments: 1, subnotes: 0}},
		},
		{
			name: "one of each",
			doc: subnoteTaskLine(subnoteRootTaskID) +
				subnoteNoteLine("prose body", subnoteProseID) +
				subnoteNoteLine("marked body [comment::]", subnoteMarkerID),
			want: map[string]derivedWant{subnoteRootTaskID: {comments: 1, subnotes: 1}},
		},
		{
			name: "prose under a NOTE chain",
			doc: subnoteTaskLine(subnoteRootTaskID) +
				subnoteNoteLine("chain reply [comment::]", subnoteChainAID) +
				"        - nested prose <!-- id: " + subnoteChainBID + " -->\n",
			want: map[string]derivedWant{subnoteRootTaskID: {comments: 1, subnotes: 1}},
		},
		{
			// Nearest-TASK ownership: the parent must claim neither column for
			// its sub-task's prose note.
			name: "prose under a sub-task",
			doc: subnoteTaskLine(subnoteRootTaskID) +
				subnoteNoteLine("parent prose", subnoteProseID) +
				"    - [ ] sub task <!-- id: " + subnoteSubTaskID + " -->\n" +
				"        - sub prose <!-- id: " + subnoteSubProseID + " -->\n",
			want: map[string]derivedWant{
				subnoteRootTaskID: {comments: 0, subnotes: 1},
				subnoteSubTaskID:  {comments: 0, subnotes: 1},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			blocks, _, _, _, err := parser.ParseFileContent(tc.doc, "nb", "sec", "pg", "2026-01-01", 4)
			if err != nil {
				t.Fatalf("ParseFileContent failed: %v", err)
			}
			comments, _, _, subnotes := taskDerivedCounts(blocks)
			for id, want := range tc.want {
				if got := comments[id]; got != want.comments {
					t.Errorf("comments[%s] = %d, want %d", id, got, want.comments)
				}
				if got := subnotes[id]; got != want.subnotes {
					t.Errorf("subnotes[%s] = %d, want %d", id, got, want.subnotes)
				}
			}
			// Every owner in either map must be one the case expects, so a
			// miscounted owner cannot hide behind an unchecked task.
			commentOwners, subnoteOwners := 0, 0
			for _, want := range tc.want {
				if want.comments > 0 {
					commentOwners++
				}
				if want.subnotes > 0 {
					subnoteOwners++
				}
			}
			if len(comments) != commentOwners {
				t.Errorf("comments owners = %v, want %d owner(s)", comments, commentOwners)
			}
			if len(subnotes) != subnoteOwners {
				t.Errorf("subnote owners = %v, want %d owner(s)", subnotes, subnoteOwners)
			}
		})
	}
}

// TestTaskDerivedCounts_CyclicParentGraphTerminates pins the 64-step cap on
// the shared ownership walk: a hand-edited A→B→A graph (and a self-parented
// note) must resolve to "no owner" instead of spinning, and must not attribute
// either column to anyone.
func TestTaskDerivedCounts_CyclicParentGraphTerminates(t *testing.T) {
	const (
		cycleAID = "c0de0101-2222-4222-8222-222222222222"
		cycleBID = "c0de0102-2222-4222-8222-222222222222"
		childID  = "c0de0103-2222-4222-8222-222222222222"
		selfID   = "c0de0104-2222-4222-8222-222222222222"
		taskID   = "c0de0105-2222-4222-8222-222222222222"
	)
	blocks := []parser.ParsedBlock{
		{ID: cycleAID, Type: parser.BlockNote, ParentID: cycleBID, RawText: "a", CleanText: "a"},
		{ID: cycleBID, Type: parser.BlockNote, ParentID: cycleAID, RawText: "b", CleanText: "b"},
		{ID: childID, Type: parser.BlockNote, ParentID: cycleAID, RawText: "prose", CleanText: "prose"},
		{ID: selfID, Type: parser.BlockNote, ParentID: selfID, RawText: "prose", CleanText: "prose"},
		{ID: taskID, Type: parser.BlockTask, Status: "TODO", RawText: "- [ ] task", CleanText: "task"},
	}

	comments, _, _, subnotes := taskDerivedCounts(blocks)
	if len(comments) != 0 {
		t.Errorf("comments = %v, want none (the cycle has no TASK ancestor)", comments)
	}
	if len(subnotes) != 0 {
		t.Errorf("subnotes = %v, want none (the cycle has no TASK ancestor)", subnotes)
	}
}

// TestTaskDerivedCounts_DepthCapBoundary pins the inclusive end of the shared
// ownership walk's cap: the 64th ancestor is examined, the 65th is not, and
// both NOTE-side caches must flip together because they ride that one walk.
func TestTaskDerivedCounts_DepthCapBoundary(t *testing.T) {
	const (
		proseLeafID   = "c0de0201-2222-4222-8222-222222222222"
		commentLeafID = "c0de0202-2222-4222-8222-222222222222"
		deepTaskID    = "c0de0203-2222-4222-8222-222222222222"
	)
	tests := []struct {
		name      string
		taskDepth int // 1-based ancestor position of the TASK below the leaves
		wantOwned bool
	}{
		{name: "TASK at the 64th ancestor", taskDepth: 64, wantOwned: true},
		{name: "TASK at the 65th ancestor", taskDepth: 65, wantOwned: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chain := make([]string, tc.taskDepth)
			for i := range chain {
				chain[i] = fmt.Sprintf("c0de1%03d-2222-4222-8222-222222222222", i)
			}
			chain[len(chain)-1] = deepTaskID

			// One prose leaf and one comment leaf share the chain so both
			// derived columns are exercised at the boundary.
			blocks := []parser.ParsedBlock{
				{ID: proseLeafID, Type: parser.BlockNote, ParentID: chain[0], RawText: "prose", CleanText: "prose"},
				{ID: commentLeafID, Type: parser.BlockNote, ParentID: chain[0], IsComment: true, RawText: "marked", CleanText: "marked"},
			}
			// Inert ancestors: only their ParentID edges matter, and a non-NOTE
			// type keeps them out of the derived caches themselves.
			for i, id := range chain {
				hop := parser.ParsedBlock{ID: id, Type: parser.BlockHeader, RawText: "hop", CleanText: "hop"}
				if i+1 < len(chain) {
					hop.ParentID = chain[i+1]
				} else {
					hop.Type = parser.BlockTask
				}
				blocks = append(blocks, hop)
			}

			comments, _, _, subnotes := taskDerivedCounts(blocks)
			wantComments, wantSubnotes := 0, 0
			if tc.wantOwned {
				wantComments, wantSubnotes = 1, 1
			}
			if got := comments[deepTaskID]; got != wantComments {
				t.Errorf("comments[%s] = %d, want %d", deepTaskID, got, wantComments)
			}
			if got := subnotes[deepTaskID]; got != wantSubnotes {
				t.Errorf("subnotes[%s] = %d, want %d", deepTaskID, got, wantSubnotes)
			}
			// No other owner may exist, so a shifted cap cannot hide behind the
			// task's own cell.
			if len(comments) != wantComments || len(subnotes) != wantSubnotes {
				t.Errorf("owners = %v / %v, want %d comment and %d subnote owner(s)",
					comments, subnotes, wantComments, wantSubnotes)
			}
		})
	}
}

// TestIndexFileBlocks_HasSubnotesRoundTrip pins index → read: the prose note's
// task reports presence with a zero comment count, the marker-only comment's
// task reports the inverse, and the tag-index reader hydrates the same values
// as the task query.
func TestIndexFileBlocks_HasSubnotesRoundTrip(t *testing.T) {
	dm := newTestDB(t)
	doc := "- [ ] prose owner #work <!-- id: " + subnoteTaggedTask + " -->\n" +
		subnoteNoteLine("prose body", subnoteTaggedNote) +
		"- [ ] comment owner #work <!-- id: " + subnoteCommentTask + " -->\n" +
		subnoteNoteLine("marked body [comment::]", subnoteCommentID)
	blocks, _, _, _, err := parser.ParseFileContent(doc, "NB", "Sec", "RoundTrip", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("ParseFileContent failed: %v", err)
	}
	if err := dm.IndexFileBlocks("vault", "NB", "Sec", "RoundTrip", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	results, err := dm.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasksWithFilters: %v", err)
	}
	byID := make(map[string]parser.TaskResult, len(results))
	for _, r := range results {
		byID[r.ID] = r
	}
	prose, ok := byID[subnoteTaggedTask]
	if !ok {
		t.Fatalf("task %s missing from QueryTasksWithFilters results", subnoteTaggedTask)
	}
	if !prose.HasSubnotes || prose.CommentsCount != 0 {
		t.Errorf("prose owner: has_subnotes=%v comments_count=%d, want true/0", prose.HasSubnotes, prose.CommentsCount)
	}
	comment, ok := byID[subnoteCommentTask]
	if !ok {
		t.Fatalf("task %s missing from QueryTasksWithFilters results", subnoteCommentTask)
	}
	if comment.HasSubnotes || comment.CommentsCount != 1 {
		t.Errorf("comment owner: has_subnotes=%v comments_count=%d, want false/1", comment.HasSubnotes, comment.CommentsCount)
	}

	tagged, err := dm.QueryBlocksByTag("work")
	if err != nil {
		t.Fatalf("QueryBlocksByTag: %v", err)
	}
	if len(tagged) != 2 {
		t.Fatalf("QueryBlocksByTag returned %d rows, want 2", len(tagged))
	}
	for _, r := range tagged {
		if want := byID[r.ID]; r.HasSubnotes != want.HasSubnotes || r.CommentsCount != want.CommentsCount {
			t.Errorf("tag reader disagrees for %s: has_subnotes=%v comments_count=%d, task reader said %v/%d",
				r.ID, r.HasSubnotes, r.CommentsCount, want.HasSubnotes, want.CommentsCount)
		}
	}
}

// TestTasks_HasSubnotesColumnSurvivesReopen pins the try-ignore migration: an
// index built before the column existed gains it on the next open, its rows
// read the 0 default, a re-index repopulates the cache, and a second open
// swallows "duplicate column name" instead of failing startup.
func TestTasks_HasSubnotesColumnSurvivesReopen(t *testing.T) {
	dm, dbPath := newOnDiskDB(t)
	doc := subnoteTaskLine(subnoteTaggedTask) + subnoteNoteLine("prose body", subnoteTaggedNote)
	blocks, _, _, _, err := parser.ParseFileContent(doc, "NB", "Sec", "Mig", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("ParseFileContent failed: %v", err)
	}
	if err := dm.IndexFileBlocks("vault", "NB", "Sec", "Mig", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
	if got := taskHasSubnotes(t, dm, subnoteTaggedTask); !got {
		t.Fatal("fresh index did not populate has_subnotes")
	}

	// Simulate a pre-#1173 index: the column does not exist at all.
	if _, err := dm.SQLDB().Exec("ALTER TABLE tasks DROP COLUMN has_subnotes"); err != nil {
		t.Fatalf("simulate pre-migration schema: %v", err)
	}
	if err := dm.Close(); err != nil {
		t.Fatalf("close after schema simulation: %v", err)
	}

	// First reopen: initSchema's ALTER adds the column; the existing row reads
	// the DEFAULT 0 → false.
	dm2, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("reopen (migration): %v", err)
	}
	if !taskColumnExists(t, dm2, "has_subnotes") {
		dm2.Close()
		t.Fatal("has_subnotes column missing after reopen")
	}
	if got := taskHasSubnotes(t, dm2, subnoteTaggedTask); got {
		dm2.Close()
		t.Error("pre-migration row read has_subnotes = true, want the 0 default")
	}
	if err := dm2.Close(); err != nil {
		t.Fatalf("close after migration: %v", err)
	}

	// Second reopen: the ALTER now hits "duplicate column name", which the
	// try-ignore loop must swallow. A re-index repopulates the cache.
	dm3, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("second reopen (duplicate-column idempotence): %v", err)
	}
	defer dm3.Close()
	if !taskColumnExists(t, dm3, "has_subnotes") {
		t.Fatal("has_subnotes column missing after second reopen")
	}
	if err := dm3.IndexFileBlocks("vault", "NB", "Sec", "Mig", blocks, nil); err != nil {
		t.Fatalf("re-index after migration: %v", err)
	}
	if got := taskHasSubnotes(t, dm3, subnoteTaggedTask); !got {
		t.Error("re-index did not repopulate has_subnotes after the migration")
	}
}

// taskHasSubnotes reads the presence flag through the production read path.
func taskHasSubnotes(t *testing.T, dm *DatabaseManager, taskID string) bool {
	t.Helper()
	results, err := dm.QueryTasksWithFilters(parser.TaskQueryFilter{BlockIDs: []string{taskID}})
	if err != nil {
		t.Fatalf("QueryTasksWithFilters(%s): %v", taskID, err)
	}
	if len(results) != 1 {
		t.Fatalf("QueryTasksWithFilters(%s) returned %d rows, want 1", taskID, len(results))
	}
	return results[0].HasSubnotes
}

func taskColumnExists(t *testing.T, dm *DatabaseManager, column string) bool {
	t.Helper()
	rows, err := dm.SQLDB().Query("PRAGMA table_info(tasks)")
	if err != nil {
		t.Fatalf("PRAGMA table_info(tasks): %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notNull   int
			dfltValue interface{}
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info row: %v", err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info rows: %v", err)
	}
	return false
}

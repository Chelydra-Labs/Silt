package main

import (
	"path/filepath"
	"testing"

	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/vault"
)

// sampleParsedNote builds a minimal NOTE block for index-path tests in this
// package (the db package's sampleNoteBlock is test-scoped there).
func sampleParsedNote(id string, line int) parser.ParsedBlock {
	return parser.ParsedBlock{
		ID:         id,
		Type:       parser.BlockNote,
		RawText:    "a note <!-- id: " + id + " -->",
		CleanText:  "a note",
		LineNumber: line,
	}
}

// TestOnDuplicateIDWarning_EmitsEvent pins the live path: a duplicate-id
// warning handed to the App sink comes out as the typed
// index:duplicate-id-warning event carrying the structured payload.
func TestOnDuplicateIDWarning_EmitsEvent(t *testing.T) {
	app := newTestApp(t)
	app.frontendReady = true

	var gotName string
	var gotPayload db.DuplicateIDWarning
	app.eventEmit = func(name string, data ...any) {
		gotName = name
		if len(data) > 0 {
			if w, ok := data[0].(db.DuplicateIDWarning); ok {
				gotPayload = w
			}
		}
	}

	app.onDuplicateIDWarning(db.DuplicateIDWarning{
		BlockID: "11111111-1111-1111-1111-111111111111",
		Pages: []db.DuplicateIDPage{
			{Notebook: "Work", Section: "Sprint", Page: "Board"},
			{Notebook: "Work", Section: "Projects", Page: "Roadmap"},
		},
	})

	if gotName != string(EventIndexDuplicateIDWarning) {
		t.Fatalf("emitted event = %q, want %q", gotName, EventIndexDuplicateIDWarning)
	}
	if gotPayload.BlockID != "11111111-1111-1111-1111-111111111111" || len(gotPayload.Pages) != 2 {
		t.Errorf("payload = %+v, want the block id and both carrier pages", gotPayload)
	}
}

// TestOnDuplicateIDWarning_QueuedPreReady pins the startup path: the vault
// scan runs before the frontend mounts, so the warning must land in the
// startup-event backlog (replayed via the router's duplicate-id case) rather
// than being emitted into the void.
func TestOnDuplicateIDWarning_QueuedPreReady(t *testing.T) {
	app := newTestApp(t)
	// frontendReady stays false — the pre-mount state.

	var emitted bool
	app.eventEmit = func(name string, data ...any) { emitted = true }

	app.onDuplicateIDWarning(db.DuplicateIDWarning{
		BlockID: "11111111-1111-1111-1111-111111111111",
		Pages:   []db.DuplicateIDPage{{Notebook: "Work", Page: "Board"}},
	})

	if emitted {
		t.Error("warning was emitted live pre-ready; it must queue instead")
	}
	app.startupEventsMu.Lock()
	defer app.startupEventsMu.Unlock()
	if len(app.startupEvents) != 1 {
		t.Fatalf("queued startup events = %d, want 1", len(app.startupEvents))
	}
	if app.startupEvents[0].Name != EventIndexDuplicateIDWarning {
		t.Errorf("queued event name = %q, want %q", app.startupEvents[0].Name, EventIndexDuplicateIDWarning)
	}
}

// TestDuplicateIDWarning_HandlerChain proves the db→App chain end to end: a
// within-file duplicate indexed through the db layer surfaces as the App's
// typed event once the handler is installed (the production wiring lives in
// initializeVaultServices).
func TestDuplicateIDWarning_HandlerChain(t *testing.T) {
	app := newTestApp(t)
	app.frontendReady = true
	app.db.SetDuplicateIDWarningHandler(app.onDuplicateIDWarning)

	var got []db.DuplicateIDWarning
	app.eventEmit = func(name string, data ...any) {
		if name == string(EventIndexDuplicateIDWarning) {
			if w, ok := data[0].(db.DuplicateIDWarning); ok {
				got = append(got, w)
			}
		}
	}

	const dup = "11111111-1111-1111-1111-111111111111"
	blocks := []parser.ParsedBlock{
		sampleParsedNote(dup, 1),
		sampleParsedNote(dup, 2),
	}
	if err := app.db.IndexFileBlocks("vault", "Work", "Sprint", "Board", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("duplicate-id events = %d, want 1", len(got))
	}
	if !got[0].WithinPage || got[0].BlockID != dup {
		t.Errorf("event payload = %+v, want within-page warning for %s", got[0], dup)
	}
}

// TestDuplicateIDWarning_StartupBatchReachesQueue pins the production
// registration order inside initializeVaultServices: the handler must be on
// the DatabaseManager BEFORE the startup batch index, so a within-file
// duplicate in a hand-edited file at launch surfaces through the real wiring
// (startup queue) with no manual registration. Before the fix, registration
// happened after the scan and these warnings vanished silently.
func TestDuplicateIDWarning_StartupBatchReachesQueue(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	hostConfigDir := t.TempDir()
	t.Setenv("APPDATA", hostConfigDir)
	t.Setenv("XDG_CONFIG_HOME", hostConfigDir)

	vaultPath := t.TempDir()
	if err := vault.ScaffoldVault(vaultPath); err != nil {
		t.Fatalf("ScaffoldVault: %v", err)
	}
	const dup = "11111111-1111-1111-1111-111111111111"
	writeFile(t, filepath.Join(vaultPath, "Work", "Board.md"), "---\n"+
		"notebook: \"Work\"\n"+
		"section: \"\"\n"+
		"page: \"Board\"\n"+
		"---\n"+
		"# Board\n\n"+
		"one <!-- id: "+dup+" -->\n\n"+
		"two <!-- id: "+dup+" -->\n")

	// Bare App, exactly like a cold launch: initializeVaultServices alone
	// must install the handler before its scan indexes the hand-edited file.
	app := &App{spacesPerTab: 4}
	if err := app.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("initializeVaultServices: %v", err)
	}
	defer func() { _ = app.CloseVault() }()

	// Pre-mount state (frontendReady false): the warning must sit in the
	// replay queue, not be emitted into the void.
	app.startupEventsMu.Lock()
	defer app.startupEventsMu.Unlock()
	var got *db.DuplicateIDWarning
	for _, ev := range app.startupEvents {
		if ev.Name != EventIndexDuplicateIDWarning {
			continue
		}
		if w, ok := ev.Payload.(db.DuplicateIDWarning); ok {
			got = &w
		}
	}
	if got == nil {
		t.Fatalf("startup queue has no duplicate-id warning (handler registered after the scan?); queued events = %+v", app.startupEvents)
	}
	if got.BlockID != dup || !got.WithinPage {
		t.Errorf("queued warning = %+v, want within-page warning for %s", *got, dup)
	}
}

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"silt/backend/config"
	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/types"
)

// reservedPropRenameNotice is stamped into ui.dismissed_tips when the #900
// reserved-property migration rewrites at least one type schema. The frontend
// shows a dismissible banner while this stamp is present and the ack stamp is
// not (mirrors hotkeys_defaults_v1_notice).
const reservedPropRenameNotice = "reserved_prop_rename_v1_notice"

// reservedPropRenameAck is appended by the frontend when the user dismisses
// the migration notice.
const reservedPropRenameAck = "reserved_prop_rename_v1_ack"

// reservedPropRenamesSettingsKey holds the structured rename list under
// plugins.plugin_settings so the UI can name concrete from→to pairs.
const reservedPropRenamesSettingsKey = "_reserved_prop_renames_v1"

// migrateReservedTypeProperties is the vault-open #900 compatibility pass:
// rename legacy type-schema properties that collide with core metadata
// (created/aliases), rewrite matching page frontmatter keys, stamp a
// dismissible notice, invalidate the type cache, and enqueue reprojection.
//
// Content-driven and idempotent: a clean types dir is a no-op. Caller holds
// vaultMu.Lock (initializeVaultServices). Best-effort — individual file
// failures are logged and do not abort vault open.
func (a *App) migrateReservedTypeProperties() {
	if a.vaultPath == "" {
		return
	}
	typesDir := a.typesDir()
	var arm types.SelfWriteArmer
	if a.typeWatcher != nil {
		arm = func(path string, content []byte) {
			a.typeWatcher.RegisterSelfWrite(path, content)
		}
	}
	renames, err := types.MigrateReservedPropertyNames(typesDir, arm)
	if err != nil {
		log.Printf("types: reserved-prop migrate: %v", err)
		return
	}
	if len(renames) == 0 {
		return
	}

	// Group renames by type id for page rewrites + scoped reprojection.
	byType := map[string][]types.ReservedPropRename{}
	typeIDs := make([]string, 0, len(renames))
	seenID := map[string]bool{}
	for _, r := range renames {
		byType[r.TypeID] = append(byType[r.TypeID], r)
		if !seenID[r.TypeID] {
			seenID[r.TypeID] = true
			typeIDs = append(typeIDs, r.TypeID)
		}
	}

	// Collect locators from the typed-page index when available.
	var locators []db.TypedPageLocator
	if a.db != nil {
		if locs, lerr := a.db.GetTypedPageLocatorsByIDs(typeIDs); lerr != nil {
			log.Printf("types: reserved-prop migrate: locator query: %v", lerr)
		} else {
			locators = locs
		}
	}

	// Build a set of type refs (id + lower name) → renames for FM matching.
	refToRenames := map[string][]types.ReservedPropRename{}
	for id, rs := range byType {
		refToRenames[strings.ToLower(id)] = rs
		if len(rs) > 0 && rs[0].TypeName != "" {
			refToRenames[strings.ToLower(rs[0].TypeName)] = rs
		}
	}

	// Rewrite pages discovered via the typed-page index.
	rewritten := 0
	for _, loc := range locators {
		rs := byType[loc.TypeName]
		if len(rs) == 0 {
			// type_name may be a display-name fallback.
			rs = refToRenames[strings.ToLower(loc.TypeName)]
		}
		if len(rs) == 0 {
			continue
		}
		if a.rewritePageReservedProps(loc, rs) {
			rewritten++
		}
	}

	// Also walk vault pages that may not yet be in page_types (cold index
	// edge) by scanning page_core for matching type strings.
	if a.db != nil {
		if cores, cerr := a.db.ListPageCoreTypeMatches(typeIDs, typeNamesFromRenames(renames)); cerr != nil {
			log.Printf("types: reserved-prop migrate: page_core scan: %v", cerr)
		} else {
			seen := map[string]bool{}
			for _, loc := range locators {
				seen[locKey(loc)] = true
			}
			for _, loc := range cores {
				if seen[locKey(loc)] {
					continue
				}
				rs := refToRenames[strings.ToLower(loc.TypeName)]
				if len(rs) == 0 {
					continue
				}
				if a.rewritePageReservedProps(loc, rs) {
					rewritten++
				}
			}
		}
	}

	log.Printf("types: reserved-prop migrate: %d schema rename(s), %d page file(s) rewritten", len(renames), rewritten)

	types.InvalidateTypesCache()
	a.stampReservedPropRenameNotice(renames)
	a.enqueueReprojection(false, typeIDs...)
}

func typeNamesFromRenames(renames []types.ReservedPropRename) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range renames {
		if r.TypeName == "" || seen[r.TypeName] {
			continue
		}
		seen[r.TypeName] = true
		out = append(out, r.TypeName)
	}
	return out
}

func locKey(loc db.TypedPageLocator) string {
	return loc.Source + "\x00" + loc.Notebook + "\x00" + loc.Section + "\x00" + loc.Page
}

// rewritePageReservedProps renames frontmatter keys on one page according to
// renames. Returns true when the file was modified.
func (a *App) rewritePageReservedProps(loc db.TypedPageLocator, renames []types.ReservedPropRename) bool {
	safeNotebook := sanitizePathSegment(loc.Notebook)
	safeSection, sectionErr := validateSectionPath(loc.Section, true)
	safePage := sanitizePathSegment(loc.Page)
	if sectionErr != nil || safeNotebook == "" || safePage == "" {
		return false
	}
	notebookDir, err := a.resolveNotebookDir(safeNotebook, loc.Source)
	if err != nil {
		log.Printf("types: reserved-prop migrate: resolve %s/%s: %v", loc.Source, loc.Notebook, err)
		return false
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return false
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("types: reserved-prop migrate: read %s: %v", filePath, err)
		}
		return false
	}
	content := string(raw)
	_, meta, _, _, perr := parser.ParseFileContent(content, safeNotebook, safeSection, safePage, fileOrDefaultDate(filePath), a.spacesPerTab)
	if perr != nil {
		log.Printf("types: reserved-prop migrate: parse %s: %v", filePath, perr)
		return false
	}
	// Confirm the page's type: matches this rename set (id or display name).
	rawType := strings.TrimSpace(meta.Type)
	if rawType == "" {
		return false
	}
	match := false
	lowerRaw := strings.ToLower(rawType)
	for _, r := range renames {
		if lowerRaw == strings.ToLower(r.TypeID) || lowerRaw == strings.ToLower(r.TypeName) {
			match = true
			break
		}
	}
	if !match {
		return false
	}

	changed := false
	// Apply renames in a stable order; each step reads the current content.
	for _, r := range renames {
		val, ok := types.LookupFrontmatterValue(meta.Frontmatter, r.From)
		if !ok || val == nil {
			continue
		}
		// If target already exists with a different value, pick a free suffix
		// on the page only when the schema already used that suffix — schema
		// and page must agree. Prefer skipping with a log over clobbering.
		if existing, eok := types.LookupFrontmatterValue(meta.Frontmatter, r.To); eok && existing != nil {
			// Target already present: only clear the old key if values are
			// equal (idempotent re-run); otherwise leave both and log.
			if fmt.Sprint(existing) != fmt.Sprint(val) {
				log.Printf("types: reserved-prop migrate: %s has both %q and %q; leaving for manual review", filePath, r.From, r.To)
				continue
			}
			// Same value under both keys — drop the reserved old key.
			next, cerr := parser.ClearFrontmatterField(content, r.From)
			if cerr != nil {
				log.Printf("types: reserved-prop migrate: clear %s %q: %v", filePath, r.From, cerr)
				continue
			}
			if next != content {
				content = next
				changed = true
				// Refresh meta map for subsequent renames on this page.
				_, meta, _, _, _ = parser.ParseFileContent(content, safeNotebook, safeSection, safePage, fileOrDefaultDate(filePath), a.spacesPerTab)
			}
			continue
		}
		next, serr := parser.SetFrontmatterField(content, r.To, val)
		if serr != nil {
			log.Printf("types: reserved-prop migrate: set %s %q: %v", filePath, r.To, serr)
			continue
		}
		next, cerr := parser.ClearFrontmatterField(next, r.From)
		if cerr != nil {
			log.Printf("types: reserved-prop migrate: clear %s %q: %v", filePath, r.From, cerr)
			continue
		}
		if next != content {
			content = next
			changed = true
			_, meta, _, _, _ = parser.ParseFileContent(content, safeNotebook, safeSection, safePage, fileOrDefaultDate(filePath), a.spacesPerTab)
		}
	}
	if !changed {
		return false
	}

	// Atomic write + self-write arm + reindex (blocks + projections).
	if a.tracker != nil {
		a.tracker.RegisterWrite(filePath)
	}
	if err := parser.WriteFileAtomic(filePath, []byte(content)); err != nil {
		log.Printf("types: reserved-prop migrate: write %s: %v", filePath, err)
		return false
	}
	if err := a.reindexFileContent(filePath, loc.Source, loc.Notebook, loc.Section, loc.Page, []byte(content), false); err != nil {
		log.Printf("types: reserved-prop migrate: reindex %s: %v", filePath, err)
		// File is already rewritten; projection will catch up on next open /
		// touch. Still count as rewritten.
	}
	return true
}

// stampReservedPropRenameNotice persists the rename list + notice tip so the
// frontend can surface a one-time banner. Uses the same configMu +
// saveConfigTracked path as other vault-open config mutations.
func (a *App) stampReservedPropRenameNotice(renames []types.ReservedPropRename) {
	if len(renames) == 0 || a.vaultPath == "" {
		return
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()

	// Prefer a fresh disk read so we don't clobber concurrent external edits.
	cfg, err := config.Load(a.vaultPath)
	if err != nil {
		// Fall back to in-memory cfg rather than dropping the notice entirely.
		cfg = a.cfg
		log.Printf("types: reserved-prop migrate: config reload failed, using in-memory: %v", err)
	}
	if cfg.Plugins.PluginSettings == nil {
		cfg.Plugins.PluginSettings = map[string]any{}
	}
	// Serialize renames as []any of map[string]any for YAML/JSON round-trip
	// through the opaque plugin_settings blob.
	list := make([]any, 0, len(renames))
	for _, r := range renames {
		list = append(list, map[string]any{
			"type_id":   r.TypeID,
			"type_name": r.TypeName,
			"file":      r.File,
			"from":      r.From,
			"to":        r.To,
		})
	}
	// Merge with any prior list (re-open after partial ack) — replace with
	// the latest successful pass so the banner stays accurate.
	cfg.Plugins.PluginSettings[reservedPropRenamesSettingsKey] = list

	if cfg.UI.DismissedTips == nil {
		cfg.UI.DismissedTips = []string{}
	}
	// Stamp notice if neither notice nor ack is present. If the user already
	// acked a previous pass, don't re-show unless we want to — plan says show
	// when migration runs. Re-stamp notice and clear ack only when new renames
	// happened (this call implies renames > 0).
	tips := cfg.UI.DismissedTips
	filtered := tips[:0]
	for _, t := range tips {
		if t == reservedPropRenameNotice || t == reservedPropRenameAck {
			continue
		}
		filtered = append(filtered, t)
	}
	cfg.UI.DismissedTips = append(filtered, reservedPropRenameNotice)

	a.cfg = cfg
	if err := a.saveConfigTracked(cfg); err != nil {
		log.Printf("types: reserved-prop migrate: stamp notice failed: %v", err)
	}
}

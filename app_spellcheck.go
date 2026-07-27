package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"silt/backend/spellcheck"
)

// App spellcheck bindings (#196 + Sprint 34 #336/#337/#338). The per-vault
// custom dictionary lives in editor.custom_dictionary in config.yaml (the
// per-vault UI tier — ARCHITECTURE §0 rule 2; NOT a new file tier and NOT
// SQLite). Language/domain pack *bytes* live in a user-global cache under
// UserConfigDir/silt/dictionaries/ (re-derivable). Enabled domain IDs live in
// editor.spellcheck_domains (YAML).
//
// These bindings do atomic read-modify-write under configMu + RegisterSelfWrite
// for config mutations. The editor's spellcheck layer (dictionary.ts) reads
// lists reactively from settings.config.

// Active pack download cancel (UI only allows one at a time).
var (
	spellPackCancelMu sync.Mutex
	spellPackCancel   context.CancelFunc
)

func setSpellPackCancel(cancel context.CancelFunc) {
	spellPackCancelMu.Lock()
	defer spellPackCancelMu.Unlock()
	if spellPackCancel != nil {
		spellPackCancel()
	}
	spellPackCancel = cancel
}

func clearSpellPackCancel() {
	spellPackCancelMu.Lock()
	defer spellPackCancelMu.Unlock()
	spellPackCancel = nil
}

// CancelSpellcheckDownload aborts the in-flight language/domain pack download
// if any. Safe when idle (no-op).
func (a *App) CancelSpellcheckDownload() {
	spellPackCancelMu.Lock()
	defer spellPackCancelMu.Unlock()
	if spellPackCancel != nil {
		spellPackCancel()
		spellPackCancel = nil
	}
}

// SetTypewriterMode atomically toggles editor.typewriter_mode (#187), mirroring
// SetFocusMode: vaultMu.RLock + configMu.Lock + saveConfigTracked. Used by the
// toggle_typewriter_mode hotkey (default Ctrl+Shift+Y) so a single-field write
// doesn't clobber an unsaved Settings draft.
func (a *App) SetTypewriterMode(value bool) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.cfg.Editor.TypewriterMode = &value
	return a.saveConfigTracked(a.cfg)
}

// GetCustomDictionary returns the per-vault custom spellcheck word list. Empty
// (non-nil) when none have been added yet — normalize guarantees non-nil.
func (a *App) GetCustomDictionary() ([]string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return nil, fmt.Errorf("vault not loaded")
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	if a.cfg.Editor.CustomDictionary == nil {
		return []string{}, nil
	}
	out := make([]string, len(a.cfg.Editor.CustomDictionary))
	copy(out, a.cfg.Editor.CustomDictionary)
	return out, nil
}

// AddCustomDictionaryWord appends a word to the per-vault custom dictionary.
// The word is trimmed + lowercased; empty/whitespace-only input is rejected.
// config.Save runs normalize (de-dup + sort + lowercase), so the on-disk list
// stays canonical. Returns the resolved list.
func (a *App) AddCustomDictionaryWord(word string) ([]string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return nil, fmt.Errorf("vault not loaded")
	}
	w := strings.ToLower(strings.TrimSpace(word))
	if w == "" {
		return nil, fmt.Errorf("word is required")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	for _, existing := range a.cfg.Editor.CustomDictionary {
		if existing == w {
			out := make([]string, len(a.cfg.Editor.CustomDictionary))
			copy(out, a.cfg.Editor.CustomDictionary)
			return out, nil
		}
	}
	a.cfg.Editor.CustomDictionary = append(a.cfg.Editor.CustomDictionary, w)
	if err := a.saveConfigTracked(a.cfg); err != nil {
		return nil, fmt.Errorf("save custom dictionary: %w", err)
	}
	out := make([]string, len(a.cfg.Editor.CustomDictionary))
	copy(out, a.cfg.Editor.CustomDictionary)
	return out, nil
}

// RemoveCustomDictionaryWord removes a word from the per-vault custom
// dictionary. The word is trimmed + lowercased to match normalize's casing.
// Removing a word that isn't present is a no-op (idempotent). Returns the
// resolved list.
func (a *App) RemoveCustomDictionaryWord(word string) ([]string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return nil, fmt.Errorf("vault not loaded")
	}
	w := strings.ToLower(strings.TrimSpace(word))
	a.configMu.Lock()
	defer a.configMu.Unlock()
	next := make([]string, 0, len(a.cfg.Editor.CustomDictionary))
	for _, existing := range a.cfg.Editor.CustomDictionary {
		if existing != w {
			next = append(next, existing)
		}
	}
	a.cfg.Editor.CustomDictionary = next
	if err := a.saveConfigTracked(a.cfg); err != nil {
		return nil, fmt.Errorf("save custom dictionary: %w", err)
	}
	out := make([]string, len(a.cfg.Editor.CustomDictionary))
	copy(out, a.cfg.Editor.CustomDictionary)
	return out, nil
}

// ---------------------------------------------------------------------------
// Language / domain packs (#336 / #337)
// ---------------------------------------------------------------------------

// ListLanguagePacks returns the frozen language catalog with installed status.
// Does not require a vault (cache is user-global).
func (a *App) ListLanguagePacks() ([]spellcheck.LanguagePackInfo, error) {
	return spellcheck.ListLanguages()
}

// EnsureLanguagePack downloads a language pack into the user-global cache if
// needed. Bundled languages (en-US) are a no-op. Progress is emitted on
// spellcheck:download:progress. Fails loudly on network/unknown-id errors —
// never silently falls back to another language. Cancellable via
// CancelSpellcheckDownload.
func (a *App) EnsureLanguagePack(lang string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	setSpellPackCancel(cancel)
	defer func() {
		cancel()
		clearSpellPackCancel()
	}()
	emit := func(received, total int64, stage string) {
		a.emit(EventSpellcheckDownloadProgress, map[string]any{
			"kind":     "language",
			"id":       lang,
			"received": received,
			"total":    total,
			"file":     stage,
		})
	}
	return spellcheck.EnsureLanguage(ctx, lang, emit)
}

// GetLanguagePackContent returns aff+dic text for a non-bundled installed
// language pack so the frontend can construct a Typo instance. Bundled
// languages must be loaded from /dictionaries/<lang>/ static assets.
func (a *App) GetLanguagePackContent(lang string) (spellcheck.LanguagePackContent, error) {
	aff, dic, err := spellcheck.ReadLanguageFiles(lang)
	if err != nil {
		return spellcheck.LanguagePackContent{}, err
	}
	return spellcheck.LanguagePackContent{Aff: aff, Dic: dic}, nil
}

// ListDomainPacks returns the frozen domain catalog with installed status.
func (a *App) ListDomainPacks() ([]spellcheck.DomainPackInfo, error) {
	return spellcheck.ListDomains()
}

// EnsureDomainPack downloads a domain word list if needed. Bundled packs are a
// no-op. Cancellable via CancelSpellcheckDownload.
func (a *App) EnsureDomainPack(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	setSpellPackCancel(cancel)
	defer func() {
		cancel()
		clearSpellPackCancel()
	}()
	emit := func(received, total int64, stage string) {
		a.emit(EventSpellcheckDownloadProgress, map[string]any{
			"kind":     "domain",
			"id":       id,
			"received": received,
			"total":    total,
			"file":     stage,
		})
	}
	return spellcheck.EnsureDomain(ctx, id, emit)
}

// GetDomainPackWords returns the parsed word list for a domain pack (bundled
// or cached). Caller must EnsureDomainPack first for downloadable packs.
func (a *App) GetDomainPackWords(id string) ([]string, error) {
	return spellcheck.ReadDomainWords(id)
}

// ---------------------------------------------------------------------------
// Custom dictionary import / export (#338)
// ---------------------------------------------------------------------------

// PickCustomDictionaryExportPath opens a native save dialog for dictionary.txt.
// Returns "" when the user cancels.
func (a *App) PickCustomDictionaryExportPath() (string, error) {
	return a.saveFileDialog("Export custom dictionary", "dictionary.txt", []FileFilter{
		{DisplayName: "Text files (*.txt)", Pattern: "*.txt"},
	})
}

// PickCustomDictionaryImportFile opens a native open dialog for a .txt word list.
// Returns "" when the user cancels.
func (a *App) PickCustomDictionaryImportFile() (string, error) {
	return a.openFileDialog("Import custom dictionary", []FileFilter{
		{DisplayName: "Text files (*.txt)", Pattern: "*.txt"},
	})
}

// ExportCustomDictionary writes the vault custom dictionary to path (UTF-8,
// one word per line, sorted). Requires a loaded vault.
func (a *App) ExportCustomDictionary(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("export path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("export path must be absolute")
	}
	words, err := a.GetCustomDictionary()
	if err != nil {
		return err
	}
	data := []byte(spellcheck.FormatWordList(words))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write dictionary: %w", err)
	}
	return nil
}

// ImportCustomDictionary reads a .txt word list, validates it, and merges into
// editor.custom_dictionary atomically. Returns a summary of new vs already-present.
func (a *App) ImportCustomDictionary(path string) (spellcheck.ImportSummary, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return spellcheck.ImportSummary{}, fmt.Errorf("import path is required")
	}
	if !filepath.IsAbs(path) {
		return spellcheck.ImportSummary{}, fmt.Errorf("import path must be absolute")
	}

	info, err := os.Stat(path)
	if err != nil {
		return spellcheck.ImportSummary{}, fmt.Errorf("read dictionary: %w", err)
	}
	if info.Size() > spellcheck.MaxImportBytes {
		return spellcheck.ImportSummary{}, fmt.Errorf("import file exceeds %d byte limit", spellcheck.MaxImportBytes)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return spellcheck.ImportSummary{}, fmt.Errorf("read dictionary: %w", err)
	}
	// Re-check after read (TOCTOU: file may grow between Stat and ReadFile).
	if int64(len(data)) > spellcheck.MaxImportBytes {
		return spellcheck.ImportSummary{}, fmt.Errorf("import file exceeds %d byte limit", spellcheck.MaxImportBytes)
	}
	incoming := spellcheck.ParseWordList(string(data))
	if len(incoming) > spellcheck.MaxImportWords {
		return spellcheck.ImportSummary{}, fmt.Errorf("import exceeds %d word limit", spellcheck.MaxImportWords)
	}

	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return spellcheck.ImportSummary{}, fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()

	existing := make(map[string]struct{}, len(a.cfg.Editor.CustomDictionary))
	for _, w := range a.cfg.Editor.CustomDictionary {
		existing[w] = struct{}{}
	}
	added := 0
	skipped := 0
	for _, w := range incoming {
		if _, ok := existing[w]; ok {
			skipped++
			continue
		}
		a.cfg.Editor.CustomDictionary = append(a.cfg.Editor.CustomDictionary, w)
		existing[w] = struct{}{}
		added++
	}
	if added > 0 {
		if err := a.saveConfigTracked(a.cfg); err != nil {
			return spellcheck.ImportSummary{}, fmt.Errorf("save custom dictionary: %w", err)
		}
	}
	return spellcheck.ImportSummary{
		Added:     added,
		Skipped:   skipped,
		TotalRead: len(incoming),
	}, nil
}

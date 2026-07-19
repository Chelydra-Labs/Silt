package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"silt/backend/config"
	"silt/backend/parser"
)

// mutateConfig is the only navigation/config write primitive. It holds the
// lifecycle read lock and config write lock through the atomic save, so every
// caller merges against the latest in-memory config rather than persisting a
// stale frontend snapshot.
func (a *App) mutateConfig(mut func(*config.SystemConfig) error) error {
	_, _, err := a.mutateConfigWithResult(mut)
	return err
}

// mutateConfigWithResult is the serialized navigation/config mutation path.
// The returned snapshot is only valid when changed is true and is emitted by
// callers after the config and lifecycle locks have been released.
func (a *App) mutateConfigWithResult(mut func(*config.SystemConfig) error) (config.SystemConfig, bool, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return config.SystemConfig{}, false, fmt.Errorf("vault not loaded")
	}

	a.configMu.Lock()
	defer a.configMu.Unlock()
	next := config.Clone(a.cfg)
	if err := mut(&next); err != nil {
		return config.SystemConfig{}, false, err
	}
	next = config.Normalize(next)
	if reflect.DeepEqual(next, a.cfg) {
		return config.SystemConfig{}, false, nil
	}
	if err := a.saveConfigTracked(next); err != nil {
		return config.SystemConfig{}, false, err
	}
	a.cfg = next
	return next, true, nil
}

// mutateConfigLocked is the lifecycle-locked form used by content mutations
// that already hold vaultMu.RLock. Re-entering an RWMutex read lock is unsafe
// when a lifecycle writer is queued, so those callers use this variant.
func (a *App) mutateConfigLocked(mut func(*config.SystemConfig) error) error {
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}

	a.configMu.Lock()
	defer a.configMu.Unlock()
	next := config.Clone(a.cfg)
	if err := mut(&next); err != nil {
		return err
	}
	next = config.Normalize(next)
	if reflect.DeepEqual(next, a.cfg) {
		return nil
	}
	if err := a.saveConfigTracked(next); err != nil {
		return err
	}
	a.cfg = next
	return nil
}

// NavigationPreferences is the narrow IPC view of sidebar preferences. It
// deliberately excludes unrelated system settings and accepts no snapshot
// write from the frontend.
type NavigationPreferences struct {
	ExpandedSections     []config.NavigationSectionRef `json:"expanded_sections"`
	RecentPages          []config.RecentPage           `json:"recent_pages"`
	Favorites            []config.NavigationPageRef    `json:"favorites"`
	QuickAccessCollapsed bool                          `json:"quick_access_collapsed"`
}

func (a *App) GetNavigationPreferences() (NavigationPreferences, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	quickAccessCollapsed := true
	if a.cfg.UI.QuickAccessCollapsed != nil {
		quickAccessCollapsed = *a.cfg.UI.QuickAccessCollapsed
	}
	return NavigationPreferences{
		ExpandedSections:     append([]config.NavigationSectionRef(nil), a.cfg.UI.ExpandedSections...),
		RecentPages:          append([]config.RecentPage(nil), a.cfg.UI.RecentPages...),
		Favorites:            append([]config.NavigationPageRef(nil), a.cfg.UI.Favorites...),
		QuickAccessCollapsed: quickAccessCollapsed,
	}, nil
}

// SetQuickAccessCollapsed persists the quiet Quick Access disclosure state
// through the serialized navigation-preferences mutation path.
func (a *App) SetQuickAccessCollapsed(collapsed bool) error {
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		cfg.UI.QuickAccessCollapsed = &collapsed
		return nil
	})
}

// SetNavigationSectionExpanded changes one canonical notebook/path entry.
func (a *App) SetNavigationSectionExpanded(notebook, sectionPath string, expanded bool) error {
	safeNotebook, err := validatePageActionSegment(notebook, "notebook name")
	if err != nil {
		return err
	}
	notebook = safeNotebook
	sectionPath = strings.TrimSpace(strings.ReplaceAll(sectionPath, `\`, "/"))
	if _, err := validateSectionPath(sectionPath, false); err != nil {
		return invalidNavigationPath(err)
	}
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		items := append([]config.NavigationSectionRef(nil), cfg.UI.ExpandedSections...)
		found := -1
		for i, item := range items {
			if item.Notebook == notebook && item.Path == sectionPath {
				found = i
				break
			}
		}
		if expanded && found < 0 {
			items = append(items, config.NavigationSectionRef{Notebook: notebook, Path: sectionPath})
		} else if !expanded && found >= 0 {
			items = append(items[:found], items[found+1:]...)
		}
		cfg.UI.ExpandedSections = items
		return nil
	})
}

// RecordRecentPage records a successful activation/save. The timestamp is
// backend-owned so callers cannot inject an unbounded or future history.
func (a *App) RecordRecentPage(notebook, section, page string) error {
	notebook, section, page, err := validateNavigationPageInput(notebook, section, page)
	if err != nil {
		return err
	}
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		ref := config.NavigationPageRef{Notebook: notebook, Section: section, Page: page}
		now := time.Now().Unix()
		// Already most-recent: bump OpenedAt on a fresh slice so DeepEqual
		// detects the mutation without reordering the list.
		if len(cfg.UI.RecentPages) > 0 && pageRefMatches(cfg.UI.RecentPages[0].NavigationPageRef, notebook, section, page) {
			items := append([]config.RecentPage(nil), cfg.UI.RecentPages...)
			items[0].OpenedAt = now
			cfg.UI.RecentPages = items
			return nil
		}
		items := make([]config.RecentPage, 0, len(cfg.UI.RecentPages)+1)
		items = append(items, config.RecentPage{NavigationPageRef: ref, OpenedAt: now})
		for _, item := range cfg.UI.RecentPages {
			if item.Notebook == notebook && item.Section == section && item.Page == page {
				continue
			}
			items = append(items, item)
		}
		cfg.UI.RecentPages = items
		return nil
	})
}

// RecordTagUsage moves tag to the front of the recent_tags list (MRU). The
// list is capped at MaxRecentTags by normalize(). Follows the same atomic
// narrow-mutation path as RecordRecentPage.
func (a *App) RecordTagUsage(tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil
	}
	changedCfg, changed, err := a.mutateConfigWithResult(func(cfg *config.SystemConfig) error {
		lower := strings.ToLower(tag)
		// Case-insensitive dedupe: remove any existing entry matching this tag.
		items := make([]string, 0, len(cfg.UI.RecentTags)+1)
		for _, existing := range cfg.UI.RecentTags {
			if strings.ToLower(existing) == lower {
				continue
			}
			items = append(items, existing)
		}
		// Prepend: most-recent first.
		items = append([]string{tag}, items...)
		cfg.UI.RecentTags = items
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		a.emit("config:changed", changedCfg)
	}
	return nil
}

// SetFavoritePage toggles one canonical page locator.
func (a *App) SetFavoritePage(notebook, section, page string, favorite bool) error {
	notebook, section, page, err := validateNavigationPageInput(notebook, section, page)
	if err != nil {
		return err
	}
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		ref := config.NavigationPageRef{Notebook: notebook, Section: section, Page: page}
		items := make([]config.NavigationPageRef, 0, len(cfg.UI.Favorites)+1)
		for _, item := range cfg.UI.Favorites {
			if item.Notebook == notebook && item.Section == section && item.Page == page {
				continue
			}
			items = append(items, item)
		}
		if favorite {
			items = append(items, ref)
		}
		cfg.UI.Favorites = items
		return nil
	})
}

func validateNavigationPageInput(notebook, section, page string) (string, string, string, error) {
	safeNotebook, err := validatePageActionSegment(notebook, "notebook name")
	if err != nil {
		return "", "", "", err
	}
	safeSection, err := validateSectionPath(section, true)
	if err != nil {
		return "", "", "", invalidNavigationPath(err)
	}
	safePage, err := validatePageActionSegment(page, "page name")
	if err != nil {
		return "", "", "", err
	}
	return safeNotebook, safeSection, safePage, nil
}

func pageRefMatches(ref config.NavigationPageRef, notebook, section, page string) bool {
	return ref.Notebook == notebook && ref.Section == section && ref.Page == page
}

func sectionPathHasPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// reconcileNavigationSection updates or removes every persisted locator below
// a renamed/deleted section. It is called after the filesystem mutation and is
// saved as one config transaction.
func (a *App) reconcileNavigationSection(notebook, oldPath, newPath string, remove bool) error {
	return a.mutateConfigLocked(func(cfg *config.SystemConfig) error {
		if cfg.UI.NavOrder.Sections == nil {
			cfg.UI.NavOrder.Sections = map[string][]string{}
		}
		if cfg.UI.NavOrder.Pages == nil {
			cfg.UI.NavOrder.Pages = map[string][]string{}
		}
		oldParent, oldLeaf := sectionParentLeaf(oldPath)
		newParent, newLeaf := sectionParentLeaf(newPath)
		oldParentKey := notebook
		if oldParent != "" {
			oldParentKey += "/" + oldParent
		}
		newParentKey := notebook
		if newParent != "" {
			newParentKey += "/" + newParent
		}
		if remove {
			cfg.UI.NavOrder.Sections[oldParentKey] = removeNavOrderValue(cfg.UI.NavOrder.Sections[oldParentKey], oldLeaf)
			for key := range cfg.UI.NavOrder.Sections {
				if key == notebook+"/"+oldPath || strings.HasPrefix(key, notebook+"/"+oldPath+"/") {
					delete(cfg.UI.NavOrder.Sections, key)
				}
			}
			for key := range cfg.UI.NavOrder.Pages {
				prefix := notebook + "/" + oldPath
				if key == prefix || strings.HasPrefix(key, prefix+"/") {
					delete(cfg.UI.NavOrder.Pages, key)
				}
			}
		} else if oldPath != newPath {
			cfg.UI.NavOrder.Sections[oldParentKey] = replaceNavOrderValue(cfg.UI.NavOrder.Sections[oldParentKey], oldLeaf, newLeaf)
			if oldParentKey != newParentKey {
				cfg.UI.NavOrder.Sections[oldParentKey] = removeNavOrderValue(cfg.UI.NavOrder.Sections[oldParentKey], newLeaf)
				cfg.UI.NavOrder.Sections[newParentKey] = appendUniqueNavOrderValue(cfg.UI.NavOrder.Sections[newParentKey], newLeaf)
			}
			prefix := notebook + "/" + oldPath
			newPrefix := notebook + "/" + newPath
			migrateNavOrderKeys(cfg.UI.NavOrder.Sections, prefix, newPrefix)
			migrateNavOrderKeys(cfg.UI.NavOrder.Pages, prefix, newPrefix)
		}

		expanded := make([]config.NavigationSectionRef, 0, len(cfg.UI.ExpandedSections))
		for _, ref := range cfg.UI.ExpandedSections {
			if ref.Notebook != notebook || !sectionPathHasPrefix(ref.Path, oldPath) {
				expanded = append(expanded, ref)
				continue
			}
			if !remove {
				ref.Path = newPath + strings.TrimPrefix(ref.Path, oldPath)
				expanded = append(expanded, ref)
			}
		}
		cfg.UI.ExpandedSections = expanded

		if remove {
			filterPages := func(in []config.NavigationPageRef) []config.NavigationPageRef {
				out := make([]config.NavigationPageRef, 0, len(in))
				for _, ref := range in {
					if ref.Notebook == notebook && sectionPathHasPrefix(ref.Section, oldPath) {
						continue
					}
					out = append(out, ref)
				}
				return out
			}
			cfg.UI.Favorites = filterPages(cfg.UI.Favorites)
			recent := make([]config.RecentPage, 0, len(cfg.UI.RecentPages))
			for _, item := range cfg.UI.RecentPages {
				if item.Notebook == notebook && sectionPathHasPrefix(item.Section, oldPath) {
					continue
				}
				recent = append(recent, item)
			}
			cfg.UI.RecentPages = recent
		}
		if !remove && oldPath != newPath {
			for i := range cfg.UI.Favorites {
				if cfg.UI.Favorites[i].Notebook == notebook && sectionPathHasPrefix(cfg.UI.Favorites[i].Section, oldPath) {
					cfg.UI.Favorites[i].Section = newPath + strings.TrimPrefix(cfg.UI.Favorites[i].Section, oldPath)
				}
			}
			for i := range cfg.UI.RecentPages {
				if cfg.UI.RecentPages[i].Notebook == notebook && sectionPathHasPrefix(cfg.UI.RecentPages[i].Section, oldPath) {
					cfg.UI.RecentPages[i].Section = newPath + strings.TrimPrefix(cfg.UI.RecentPages[i].Section, oldPath)
				}
			}
			if cfg.UI.ActiveTab != nil && cfg.UI.ActiveTab.Notebook == notebook && sectionPathHasPrefix(cfg.UI.ActiveTab.Section, oldPath) {
				cfg.UI.ActiveTab.Section = newPath + strings.TrimPrefix(cfg.UI.ActiveTab.Section, oldPath)
			}
			for i := range cfg.UI.OpenTabs {
				if cfg.UI.OpenTabs[i].Notebook == notebook && sectionPathHasPrefix(cfg.UI.OpenTabs[i].Section, oldPath) {
					cfg.UI.OpenTabs[i].Section = newPath + strings.TrimPrefix(cfg.UI.OpenTabs[i].Section, oldPath)
				}
			}
		} else if remove {
			if cfg.UI.ActiveTab != nil && cfg.UI.ActiveTab.Notebook == notebook && sectionPathHasPrefix(cfg.UI.ActiveTab.Section, oldPath) {
				cfg.UI.ActiveTab = nil
			}
			kept := cfg.UI.OpenTabs[:0]
			for _, tab := range cfg.UI.OpenTabs {
				if tab.Notebook == notebook && sectionPathHasPrefix(tab.Section, oldPath) {
					continue
				}
				kept = append(kept, tab)
			}
			cfg.UI.OpenTabs = kept
		}
		return nil
	})
}

func sectionParentLeaf(path string) (string, string) {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "", path
	}
	return path[:idx], path[idx+1:]
}

func removeNavOrderValue(values []string, target string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func replaceNavOrderValue(values []string, oldValue, newValue string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == oldValue {
			value = newValue
		}
		if !containsString(out, value) {
			out = append(out, value)
		}
	}
	return out
}

func appendUniqueNavOrderValue(values []string, value string) []string {
	if containsString(values, value) {
		return values
	}
	return append(values, value)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// migrateNavOrderKeys snapshots matching keys before changing the map. A
// migrated source deliberately overwrites an existing destination key, while
// snapshotting makes that overwrite independent of map iteration behavior.
func migrateNavOrderKeys(order map[string][]string, oldPrefix, newPrefix string) {
	keys := make([]string, 0)
	for key := range order {
		if key == oldPrefix || strings.HasPrefix(key, oldPrefix+"/") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	entries := make([]struct {
		key    string
		values []string
	}, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, struct {
			key    string
			values []string
		}{key: newPrefix + strings.TrimPrefix(key, oldPrefix), values: order[key]})
		delete(order, key)
	}
	for _, entry := range entries {
		order[entry.key] = entry.values
	}
}

func (a *App) reconcileNavigationPage(notebook, section, oldPage, newPage string, remove bool) error {
	return a.mutateConfigLocked(func(cfg *config.SystemConfig) error {
		if cfg.UI.NavOrder.Pages != nil {
			key := notebook + "/" + section
			if pages, ok := cfg.UI.NavOrder.Pages[key]; ok {
				out := make([]string, 0, len(pages))
				for _, candidate := range pages {
					if candidate == oldPage {
						if !remove {
							out = append(out, newPage)
						}
						continue
					}
					out = append(out, candidate)
				}
				cfg.UI.NavOrder.Pages[key] = out
			}
		}
		for i := range cfg.UI.Favorites {
			if pageRefMatches(cfg.UI.Favorites[i], notebook, section, oldPage) {
				if remove {
					cfg.UI.Favorites = append(cfg.UI.Favorites[:i], cfg.UI.Favorites[i+1:]...)
				} else {
					cfg.UI.Favorites[i].Page = newPage
				}
				break
			}
		}
		recent := make([]config.RecentPage, 0, len(cfg.UI.RecentPages))
		for _, item := range cfg.UI.RecentPages {
			if pageRefMatches(item.NavigationPageRef, notebook, section, oldPage) {
				if remove {
					continue
				}
				item.Page = newPage
			}
			recent = append(recent, item)
		}
		cfg.UI.RecentPages = recent
		if cfg.UI.ActiveTab != nil && pageRefMatches(config.NavigationPageRef{Notebook: cfg.UI.ActiveTab.Notebook, Section: cfg.UI.ActiveTab.Section, Page: cfg.UI.ActiveTab.Page}, notebook, section, oldPage) {
			if remove {
				cfg.UI.ActiveTab = nil
			} else {
				cfg.UI.ActiveTab.Page = newPage
			}
		}
		kept := cfg.UI.OpenTabs[:0]
		for _, tab := range cfg.UI.OpenTabs {
			if pageRefMatches(config.NavigationPageRef{Notebook: tab.Notebook, Section: tab.Section, Page: tab.Page}, notebook, section, oldPage) {
				if remove {
					continue
				}
				tab.Page = newPage
			}
			kept = append(kept, tab)
		}
		cfg.UI.OpenTabs = kept
		return nil
	})
}

func (a *App) reconcileNavigationMove(notebook, fromSection, toSection, page string) error {
	return a.mutateConfigLocked(func(cfg *config.SystemConfig) error {
		if cfg.UI.NavOrder.Pages == nil {
			cfg.UI.NavOrder.Pages = map[string][]string{}
		}
		fromKey, toKey := notebook+"/"+fromSection, notebook+"/"+toSection
		if pages := cfg.UI.NavOrder.Pages[fromKey]; pages != nil {
			out := make([]string, 0, len(pages))
			for _, candidate := range pages {
				if candidate != page {
					out = append(out, candidate)
				}
			}
			cfg.UI.NavOrder.Pages[fromKey] = out
		}
		found := false
		for _, candidate := range cfg.UI.NavOrder.Pages[toKey] {
			if candidate == page {
				found = true
				break
			}
		}
		if !found {
			cfg.UI.NavOrder.Pages[toKey] = append(cfg.UI.NavOrder.Pages[toKey], page)
		}
		for i := range cfg.UI.Favorites {
			if pageRefMatches(cfg.UI.Favorites[i], notebook, fromSection, page) {
				cfg.UI.Favorites[i].Section = toSection
			}
		}
		for i := range cfg.UI.RecentPages {
			if pageRefMatches(cfg.UI.RecentPages[i].NavigationPageRef, notebook, fromSection, page) {
				cfg.UI.RecentPages[i].Section = toSection
			}
		}
		if cfg.UI.ActiveTab != nil && pageRefMatches(config.NavigationPageRef{Notebook: cfg.UI.ActiveTab.Notebook, Section: cfg.UI.ActiveTab.Section, Page: cfg.UI.ActiveTab.Page}, notebook, fromSection, page) {
			cfg.UI.ActiveTab.Section = toSection
		}
		for i := range cfg.UI.OpenTabs {
			if pageRefMatches(config.NavigationPageRef{Notebook: cfg.UI.OpenTabs[i].Notebook, Section: cfg.UI.OpenTabs[i].Section, Page: cfg.UI.OpenTabs[i].Page}, notebook, fromSection, page) {
				cfg.UI.OpenTabs[i].Section = toSection
			}
		}
		return nil
	})
}

func (a *App) reconcileNavigationNotebook(oldName, newName string, remove bool) error {
	return a.mutateConfigLocked(func(cfg *config.SystemConfig) error {
		if cfg.UI.NavOrder.Sections == nil {
			cfg.UI.NavOrder.Sections = map[string][]string{}
		}
		if cfg.UI.NavOrder.Pages == nil {
			cfg.UI.NavOrder.Pages = map[string][]string{}
		}
		if remove {
			for key := range cfg.UI.NavOrder.Pages {
				if key == oldName || strings.HasPrefix(key, oldName+"/") {
					delete(cfg.UI.NavOrder.Pages, key)
				}
			}
			for key := range cfg.UI.NavOrder.Sections {
				if key == oldName || strings.HasPrefix(key, oldName+"/") {
					delete(cfg.UI.NavOrder.Sections, key)
				}
			}
		} else if oldName != newName {
			migrateNavOrderKeys(cfg.UI.NavOrder.Pages, oldName, newName)
			migrateNavOrderKeys(cfg.UI.NavOrder.Sections, oldName, newName)
		}
		pages := func(ref config.NavigationPageRef) (config.NavigationPageRef, bool) {
			if ref.Notebook != oldName {
				return ref, true
			}
			if remove {
				return ref, false
			}
			ref.Notebook = newName
			return ref, true
		}
		favorites := make([]config.NavigationPageRef, 0, len(cfg.UI.Favorites))
		for _, ref := range cfg.UI.Favorites {
			if ref, ok := pages(ref); ok {
				favorites = append(favorites, ref)
			}
		}
		cfg.UI.Favorites = favorites
		recent := make([]config.RecentPage, 0, len(cfg.UI.RecentPages))
		for _, item := range cfg.UI.RecentPages {
			if ref, ok := pages(item.NavigationPageRef); ok {
				item.NavigationPageRef = ref
				recent = append(recent, item)
			}
		}
		cfg.UI.RecentPages = recent
		expanded := make([]config.NavigationSectionRef, 0, len(cfg.UI.ExpandedSections))
		for _, ref := range cfg.UI.ExpandedSections {
			if ref.Notebook == oldName {
				if remove {
					continue
				}
				ref.Notebook = newName
			}
			expanded = append(expanded, ref)
		}
		cfg.UI.ExpandedSections = expanded
		if cfg.UI.ActiveTab != nil && cfg.UI.ActiveTab.Notebook == oldName {
			if remove {
				cfg.UI.ActiveTab = nil
			} else {
				cfg.UI.ActiveTab.Notebook = newName
			}
		}
		kept := cfg.UI.OpenTabs[:0]
		for _, tab := range cfg.UI.OpenTabs {
			if tab.Notebook == oldName {
				if remove {
					continue
				}
				tab.Notebook = newName
			}
			kept = append(kept, tab)
		}
		cfg.UI.OpenTabs = kept
		return nil
	})
}

func (a *App) reconcileNavigationAgainstTree(tree parser.NavigationTree) error {
	validPages := navPageSet(tree)
	validSections := make(map[string]struct{})
	disconnectedNotebooks := make(map[string]struct{})
	var collect func(string, []parser.NavigationSection)
	collect = func(notebook string, sections []parser.NavigationSection) {
		for _, section := range sections {
			if section.Path != "" {
				validSections[notebook+"\x00"+section.Path] = struct{}{}
			}
			collect(notebook, section.Children)
		}
	}
	for _, notebook := range tree.Notebooks {
		if notebook.Disconnected {
			disconnectedNotebooks[notebook.Name] = struct{}{}
		}
		collect(notebook.Name, notebook.Sections)
	}
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		expanded := make([]config.NavigationSectionRef, 0, len(cfg.UI.ExpandedSections))
		for _, ref := range cfg.UI.ExpandedSections {
			if _, disconnected := disconnectedNotebooks[ref.Notebook]; disconnected {
				expanded = append(expanded, ref)
			} else if _, ok := validSections[ref.Notebook+"\x00"+ref.Path]; ok {
				expanded = append(expanded, ref)
			}
		}
		cfg.UI.ExpandedSections = expanded
		favorites := make([]config.NavigationPageRef, 0, len(cfg.UI.Favorites))
		for _, ref := range cfg.UI.Favorites {
			if _, disconnected := disconnectedNotebooks[ref.Notebook]; disconnected || validPages[ref.Notebook+"\x00"+ref.Section+"\x00"+ref.Page] {
				favorites = append(favorites, ref)
			}
		}
		cfg.UI.Favorites = favorites
		recent := make([]config.RecentPage, 0, len(cfg.UI.RecentPages))
		for _, item := range cfg.UI.RecentPages {
			if _, disconnected := disconnectedNotebooks[item.Notebook]; disconnected || validPages[item.Notebook+"\x00"+item.Section+"\x00"+item.Page] {
				recent = append(recent, item)
			}
		}
		cfg.UI.RecentPages = recent
		return nil
	})
}

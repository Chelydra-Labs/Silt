package main

import (
	"fmt"
	"silt/backend/config"
	"silt/backend/parser"
)

// --- Sidebar width / nav order IPC (#63, #68) -----------------------------

// GetSidebarWidth returns the persisted sidebar width from config.yaml.
// Defaults to 256 when unset or below the minimum.
func (a *App) GetSidebarWidth() int {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	w := a.cfg.UI.SidebarWidth
	if w < 200 {
		return 256
	}
	return w
}

// SetSidebarWidth persists a new sidebar width to config.yaml, clamped to
// [200, 480]. Uses RegisterSelfWrite to suppress the config watcher's
// self-write loop.
func (a *App) SetSidebarWidth(px int) error {
	if px < 200 {
		px = 200
	}
	if px > 480 {
		px = 480
	}
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		cfg.UI.SidebarWidth = px
		return nil
	})
}

// GetNavOrder returns the persisted navigation ordering from config.yaml.
func (a *App) GetNavOrder() (config.NavOrder, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.cfg.UI.NavOrder, nil
}

// SetNavNotebookOrder replaces only the notebook ordering. The current config
// is read inside the serialized mutation, so an older client cannot overwrite
// section or page ordering keys added by another client.
func (a *App) SetNavNotebookOrder(order []string) error {
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		cfg.UI.NavOrder.Notebooks = append([]string(nil), order...)
		return nil
	})
}

// SetNavSectionOrder sets one parent-qualified section order key. An empty
// parentPath addresses the notebook root; otherwise it is a full relative
// section path.
func (a *App) SetNavSectionOrder(notebook, parentPath string, order []string) error {
	key, err := navOrderParentKey(notebook, parentPath)
	if err != nil {
		return err
	}
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		if cfg.UI.NavOrder.Sections == nil {
			cfg.UI.NavOrder.Sections = map[string][]string{}
		}
		cfg.UI.NavOrder.Sections[key] = append([]string(nil), order...)
		return nil
	})
}

// SetNavPageOrder sets one section-qualified page order key. An empty
// sectionPath addresses pages directly under the notebook root.
func (a *App) SetNavPageOrder(notebook, sectionPath string, order []string) error {
	key, err := navOrderSectionKey(notebook, sectionPath)
	if err != nil {
		return err
	}
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		if cfg.UI.NavOrder.Pages == nil {
			cfg.UI.NavOrder.Pages = map[string][]string{}
		}
		cfg.UI.NavOrder.Pages[key] = append([]string(nil), order...)
		return nil
	})
}

// ClearNavNotebookOrder clears only the explicit notebook-order list.
func (a *App) ClearNavNotebookOrder() error {
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		cfg.UI.NavOrder.Notebooks = []string{}
		return nil
	})
}

// ClearNavSectionOrder removes one parent-qualified section order key.
func (a *App) ClearNavSectionOrder(notebook, parentPath string) error {
	key, err := navOrderParentKey(notebook, parentPath)
	if err != nil {
		return err
	}
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		delete(cfg.UI.NavOrder.Sections, key)
		return nil
	})
}

// ClearNavPageOrder removes one section-qualified page order key.
func (a *App) ClearNavPageOrder(notebook, sectionPath string) error {
	key, err := navOrderSectionKey(notebook, sectionPath)
	if err != nil {
		return err
	}
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		delete(cfg.UI.NavOrder.Pages, key)
		return nil
	})
}

func navOrderParentKey(notebook, parentPath string) (string, error) {
	notebook = sanitizePathSegment(notebook)
	parentPath, pathErr := validateSectionPath(parentPath, true)
	if notebook == "" {
		return "", fmt.Errorf("notebook name is required")
	}
	if pathErr != nil {
		return "", invalidNavigationPath(pathErr)
	}
	if parentPath == "" {
		return notebook, nil
	}
	return notebook + "/" + parentPath, nil
}

func navOrderSectionKey(notebook, sectionPath string) (string, error) {
	sectionPath, pathErr := validateSectionPath(sectionPath, true)
	if pathErr != nil {
		return "", invalidNavigationPath(pathErr)
	}
	notebook = sanitizePathSegment(notebook)
	if notebook == "" {
		return "", fmt.Errorf("notebook name is required")
	}
	if sectionPath == "" {
		return notebook + "/", nil
	}
	parentKey, err := navOrderParentKey(notebook, sectionPath)
	if err != nil {
		return "", err
	}
	return parentKey, nil
}

// --- Open tabs IPC (#142) ------------------------------------------------

// OpenTabsResult is the GetOpenTabs IPC envelope. Wails v2 generates
// bindings for only the first non-error return value, so a multi-return
// Go signature (tabs, active, err) would lose the active tab on the JS side.
// A single struct return serializes cleanly to a JSON object.
type OpenTabsResult struct {
	OpenTabs  []config.TabRef `json:"open_tabs"`
	ActiveTab *config.TabRef  `json:"active_tab"`
}

// GetOpenTabs returns the persisted open-tab set + active tab from
// config.yaml. Only pinned tabs are persisted (preview tabs are ephemeral —
// industry-standard parity). Stale tabs — references to pages that no longer exist on
// disk (deleted/renamed since last launch) — are pruned silently against the
// live ListNavigation tree before returning, so the frontend never mounts a
// tab for a missing page. The on-disk tree is the source of truth, not the
// persisted tab list (same philosophy as the SQLite index).
func (a *App) GetOpenTabs() (OpenTabsResult, error) {
	// Lock order: vaultMu before configMu (app.go invariant). Read vaultPath
	// first under vaultMu, then the persisted tabs under configMu. The two
	// reads are independent, so the order is free — establish it consistently
	// to honour the invariant and avoid an AB-BA inversion with any binding
	// that takes both locks.
	a.vaultMu.RLock()
	vaultPath := a.vaultPath
	a.vaultMu.RUnlock()

	a.configMu.RLock()
	tabs := append([]config.TabRef(nil), a.cfg.UI.OpenTabs...)
	active := a.cfg.UI.ActiveTab
	a.configMu.RUnlock()

	if vaultPath == "" {
		return OpenTabsResult{OpenTabs: []config.TabRef{}}, nil
	}
	if len(tabs) == 0 {
		return OpenTabsResult{OpenTabs: tabs, ActiveTab: active}, nil
	}

	// Prune stale tabs against the live navigation tree. A tab is stale if
	// its (notebook, section, page) triple does not appear anywhere in the
	// tree. Best-effort: if ListNavigation fails (e.g. a temporarily
	// unreadable directory), return the persisted set unpruned rather than
	// blocking the UI. ListNavigation acquires its own vaultMu.RLock, so we
	// must not hold it here (no recursive RLock — deadlock risk if a writer
	// is waiting).
	tree, navErr := a.ListNavigation()
	if navErr != nil {
		return OpenTabsResult{OpenTabs: tabs, ActiveTab: active}, nil
	}
	validPages := navPageSet(tree)

	pruned := make([]config.TabRef, 0, len(tabs))
	for _, t := range tabs {
		// Drop entries with an empty Page (malformed YAML) or a missing
		// page on disk.
		if t.Page == "" {
			continue
		}
		key := t.Notebook + "\x00" + t.Section + "\x00" + t.Page
		if !validPages[key] {
			continue
		}
		pruned = append(pruned, t)
	}

	// If the active tab was pruned, clear it.
	if active != nil && active.Page != "" {
		key := active.Notebook + "\x00" + active.Section + "\x00" + active.Page
		if !validPages[key] {
			active = nil
		}
	}

	return OpenTabsResult{OpenTabs: pruned, ActiveTab: active}, nil
}

// SetOpenTabs persists the open-tab set + active tab to config.yaml. The
// frontend filters to pinned tabs before calling (preview tabs are
// ephemeral). Holds vaultMu.RLock across the entire call to block lifecycle
// transitions (CloseVault / MoveVault) during the disk write — matching the
// SetSidebarWidth / narrow nav-order sibling pattern. RLock does not block other
// RLock readers, so ListNavigation and GetOpenTabs continue to operate
// concurrently. The vaultMu → configMu ordering is preserved.
func (a *App) SetOpenTabs(openTabs []config.TabRef, activeTab *config.TabRef) error {
	if openTabs == nil {
		openTabs = []config.TabRef{}
	}
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		cfg.UI.OpenTabs = append([]config.TabRef(nil), openTabs...)
		if activeTab == nil {
			cfg.UI.ActiveTab = nil
		} else {
			copy := *activeTab
			cfg.UI.ActiveTab = &copy
		}
		return nil
	})
}

// navPageSet flattens the NavigationTree into a set of
// "notebook\x00section\x00page" strings for O(1) existence checks. The
// section key mirrors the frontend's sectionKey derivation: section.path if
// present, otherwise section.name. Section-less pages match section "".
func navPageSet(tree parser.NavigationTree) map[string]bool {
	out := make(map[string]bool)
	for _, nb := range tree.Notebooks {
		collectPages(out, nb.Name, nb.Sections)
	}
	return out
}

func collectPages(out map[string]bool, notebook string, sections []parser.NavigationSection) {
	for _, sec := range sections {
		sectionKey := sec.Path
		if sectionKey == "" {
			sectionKey = sec.Name
		}
		for _, pg := range sec.Pages {
			out[notebook+"\x00"+sectionKey+"\x00"+pg.Name] = true
		}
		if len(sec.Children) > 0 {
			collectPages(out, notebook, sec.Children)
		}
	}
}

package main

import (
	"fmt"
	"strings"
	"time"

	"silt/backend/config"
)

// NavigationPreferences is the narrow IPC view of sidebar preferences. It
// deliberately excludes unrelated system settings and accepts no snapshot
// write from the frontend.
type NavigationPreferences struct {
	ExpandedSections []config.NavigationSectionRef `json:"expanded_sections"`
	RecentPages      []config.RecentPage           `json:"recent_pages"`
	Favorites        []config.NavigationPageRef    `json:"favorites"`
	SidebarView      string                        `json:"sidebar_view"`
}

func (a *App) GetNavigationPreferences() (NavigationPreferences, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	// SidebarView is nil only on the raw Defaults() error-path returns that
	// skip normalize (vault not loaded); treat that as the "tree" default so
	// the frontend never renders an empty sidebar.
	sidebarView := "tree"
	if a.cfg.UI.SidebarView != nil {
		sidebarView = *a.cfg.UI.SidebarView
	}
	return NavigationPreferences{
		ExpandedSections: append([]config.NavigationSectionRef(nil), a.cfg.UI.ExpandedSections...),
		RecentPages:      append([]config.RecentPage(nil), a.cfg.UI.RecentPages...),
		Favorites:        append([]config.NavigationPageRef(nil), a.cfg.UI.Favorites...),
		SidebarView:      sidebarView,
	}, nil
}

// SetSidebarView persists the active sidebar view mode ("tree" | "quick")
// through the serialized navigation-preferences mutation path.
func (a *App) SetSidebarView(view string) error {
	if view != "tree" && view != "quick" {
		return fmt.Errorf("invalid sidebar view %q: must be \"tree\" or \"quick\"", view)
	}
	return a.mutateConfig(func(cfg *config.SystemConfig) error {
		cfg.UI.SidebarView = &view
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
// narrow-mutation path as RecordRecentPage. Invalid tag paths (wrong
// character contract or exceeding MaxTagPathBytes) are rejected so the config
// file can never store a tag that the indexer wouldn't recognize.
func (a *App) RecordTagUsage(tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil
	}
	if !config.IsValidTagPath(tag) {
		return fmt.Errorf("invalid tag path %q: must match %s and be at most %d bytes",
			tag, `^[a-zA-Z][a-zA-Z0-9_/-]*$`, config.MaxTagPathBytes)
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
		a.emit(EventConfigChanged, changedCfg)
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

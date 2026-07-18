package config

// Clone returns an independent snapshot of cfg. Config mutations run callbacks
// before persistence, so sharing maps, slices, or pointers with the live config
// would let a failed callback or save partially mutate the current state.
func Clone(cfg SystemConfig) SystemConfig {
	out := cfg
	out.Editor.CustomDictionary = cloneStrings(cfg.Editor.CustomDictionary)
	out.Editor.SpellcheckDomains = cloneStrings(cfg.Editor.SpellcheckDomains)
	out.Editor.ShowWordCount = clonePtr(cfg.Editor.ShowWordCount)
	out.Editor.FocusMode = clonePtr(cfg.Editor.FocusMode)
	out.Editor.DefaultViewMode = clonePtr(cfg.Editor.DefaultViewMode)
	out.Editor.SpellcheckEnabled = clonePtr(cfg.Editor.SpellcheckEnabled)
	out.Editor.SpellcheckLanguage = clonePtr(cfg.Editor.SpellcheckLanguage)
	out.Editor.TypewriterMode = clonePtr(cfg.Editor.TypewriterMode)
	out.Editor.TypewriterModeRatio = clonePtr(cfg.Editor.TypewriterModeRatio)

	out.Hotkeys = cloneStringMap(cfg.Hotkeys)
	out.Plugins.Active = cloneStrings(cfg.Plugins.Active)
	out.Plugins.Disabled = cloneStrings(cfg.Plugins.Disabled)
	out.Plugins.PluginSettings = clonePluginSettings(cfg.Plugins.PluginSettings)

	out.UI.NavOrder = CloneNavOrder(cfg.UI.NavOrder)
	out.UI.OpenTabs = append([]TabRef(nil), cfg.UI.OpenTabs...)
	out.UI.ActiveTab = clonePtr(cfg.UI.ActiveTab)
	out.UI.ExpandedSections = append([]NavigationSectionRef(nil), cfg.UI.ExpandedSections...)
	out.UI.RecentPages = append([]RecentPage(nil), cfg.UI.RecentPages...)
	out.UI.Favorites = append([]NavigationPageRef(nil), cfg.UI.Favorites...)
	out.UI.EnablePreviewTabs = clonePtr(cfg.UI.EnablePreviewTabs)
	out.UI.ShowFormatToolbar = clonePtr(cfg.UI.ShowFormatToolbar)
	out.UI.ShowTabDirtyIndicators = clonePtr(cfg.UI.ShowTabDirtyIndicators)
	out.UI.DismissedTips = cloneStrings(cfg.UI.DismissedTips)
	out.UI.QuickAccessCollapsed = clonePtr(cfg.UI.QuickAccessCollapsed)
	out.UI.OpenDevtoolsOnStartup = clonePtr(cfg.UI.OpenDevtoolsOnStartup)
	out.UI.Formatting = FormattingConfig{
		TypographyEnabled: clonePtr(cfg.UI.Formatting.TypographyEnabled),
		ColorEnabled:      clonePtr(cfg.UI.Formatting.ColorEnabled),
		MathEnabled:       clonePtr(cfg.UI.Formatting.MathEnabled),
	}

	out.AI.UseKeyring = clonePtr(cfg.AI.UseKeyring)
	out.AI.Chat = cloneProvider(cfg.AI.Chat)
	out.AI.Embedding = cloneProvider(cfg.AI.Embedding)
	out.LinkedNotebooks = append([]LinkedNotebook(nil), cfg.LinkedNotebooks...)
	return out
}

func cloneProvider(provider AIProviderConfig) AIProviderConfig {
	provider.Temperature = clonePtr(provider.Temperature)
	provider.MaxTokens = clonePtr(provider.MaxTokens)
	provider.ReasoningEffort = clonePtr(provider.ReasoningEffort)
	provider.TimeoutMs = clonePtr(provider.TimeoutMs)
	provider.Dimensions = clonePtr(provider.Dimensions)
	return provider
}

func clonePtr[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

// CloneNavOrder returns an independent copy of order, preserving nil
// Sections/Pages maps (empty maps stay empty).
func CloneNavOrder(order NavOrder) NavOrder {
	out := NavOrder{
		Notebooks: cloneStrings(order.Notebooks),
		Sections:  make(map[string][]string, len(order.Sections)),
		Pages:     make(map[string][]string, len(order.Pages)),
	}
	for key, values := range order.Sections {
		out.Sections[key] = cloneStrings(values)
	}
	for key, values := range order.Pages {
		out.Pages[key] = cloneStrings(values)
	}
	if order.Sections == nil {
		out.Sections = nil
	}
	if order.Pages == nil {
		out.Pages = nil
	}
	return out
}

func clonePluginSettings(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = cloneValue(value)
	}
	return out
}

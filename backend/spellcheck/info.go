package spellcheck

// LanguagePackInfo is the IPC-facing language catalog row (status included).
type LanguagePackInfo struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	License      string `json:"license"`
	ApproxBytes  int64  `json:"approx_bytes"`
	Bundled      bool   `json:"bundled"`
	Downloadable bool   `json:"downloadable"`
	Installed    bool   `json:"installed"`
	Version      string `json:"version"`
}

// DomainPackInfo is the IPC-facing domain catalog row.
type DomainPackInfo struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	License      string `json:"license"`
	ApproxBytes  int64  `json:"approx_bytes"`
	Bundled      bool   `json:"bundled"`
	Downloadable bool   `json:"downloadable"`
	Installed    bool   `json:"installed"`
	DefaultOn    bool   `json:"default_on"`
	Version      string `json:"version"`
}

// LanguagePackContent is aff+dic text for the frontend Typo constructor.
type LanguagePackContent struct {
	Aff string `json:"aff"`
	Dic string `json:"dic"`
}

// ImportSummary reports custom-dictionary import results.
type ImportSummary struct {
	Added     int `json:"added"`
	Skipped   int `json:"skipped"`
	TotalRead int `json:"total_read"`
}

// ListLanguages returns catalog rows with installed status from the cache.
func ListLanguages() ([]LanguagePackInfo, error) {
	root, err := CacheRoot()
	if err != nil {
		return nil, err
	}
	out := make([]LanguagePackInfo, 0, len(Languages))
	for _, spec := range Languages {
		out = append(out, LanguagePackInfo{
			ID:           spec.ID,
			Label:        spec.Label,
			License:      spec.License,
			ApproxBytes:  spec.ApproxBytes,
			Bundled:      spec.Bundled,
			Downloadable: spec.Downloadable,
			Installed:    languageInstalled(root, spec),
			Version:      spec.Version,
		})
	}
	return out, nil
}

// ListDomains returns catalog rows with installed status from the cache.
func ListDomains() ([]DomainPackInfo, error) {
	root, err := CacheRoot()
	if err != nil {
		return nil, err
	}
	out := make([]DomainPackInfo, 0, len(Domains))
	for _, spec := range Domains {
		out = append(out, DomainPackInfo{
			ID:           spec.ID,
			Label:        spec.Label,
			License:      spec.License,
			ApproxBytes:  spec.ApproxBytes,
			Bundled:      spec.Bundled,
			Downloadable: spec.Downloadable,
			Installed:    domainInstalled(root, spec),
			DefaultOn:    spec.DefaultOn,
			Version:      spec.Version,
		})
	}
	return out, nil
}

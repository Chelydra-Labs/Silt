// Package spellcheck manages downloadable language and domain dictionary packs
// for the inline spellcheck feature (Sprint 34 / #336 / #337).
//
// Language packs are Hunspell .aff/.dic pairs from wooorm/dictionaries npm
// packages, fetched via version-pinned jsDelivr URLs (not GitHub raw/main).
// Domain packs are plain word lists from @cspell/dict-* packages, merged as
// Set layers on top of typo-js (which has no public addWord).
//
// License policy: prefer MIT/BSD for defaults. MPL/GPL languages may be offered
// with their license file cached alongside the pack. AGPL and GPL medical packs
// are excluded from the v1 catalog.
package spellcheck

import "strings"

// LanguageSpec describes one Hunspell language pack in the catalog.
type LanguageSpec struct {
	ID           string // Silt tag, e.g. "en-GB" (matches editor.spellcheck_language)
	Label        string
	NPMPackage   string // wooorm package name, e.g. "dictionary-en-gb"
	Version      string // pinned npm version
	License      string // SPDX
	ApproxBytes  int64
	Bundled      bool // shipped under frontend/public/dictionaries/<id>/
	Downloadable bool
}

// DomainSpec describes one technical word-list pack.
type DomainSpec struct {
	ID           string
	Label        string
	NPMPackage   string // empty when Bundled-only curated list
	Version      string
	License      string
	WordURL      string // full jsDelivr URL (may end in .gz); empty when Bundled-only
	ApproxBytes  int64
	Bundled      bool
	Downloadable bool
	DefaultOn    bool // included in editor.spellcheck_domains default
}

// Languages is the frozen v1 language catalog. en-US is bundled; others download
// on demand from jsDelivr npm mirrors of wooorm/dictionaries.
var Languages = []LanguageSpec{
	{
		ID: "en-US", Label: "English (US)", NPMPackage: "dictionary-en",
		Version: "3.0.0", License: "MIT AND BSD", ApproxBytes: 605_000,
		Bundled: true, Downloadable: false,
	},
	{
		ID: "en-GB", Label: "English (UK)", NPMPackage: "dictionary-en-gb",
		Version: "3.0.0", License: "MIT AND BSD", ApproxBytes: 555_000,
		Bundled: false, Downloadable: true,
	},
	{
		ID: "de", Label: "German", NPMPackage: "dictionary-de",
		Version: "3.0.0", License: "GPL-2.0 OR GPL-3.0", ApproxBytes: 1_100_000,
		Bundled: false, Downloadable: true,
	},
	{
		ID: "fr", Label: "French", NPMPackage: "dictionary-fr",
		Version: "3.0.0", License: "MPL-2.0", ApproxBytes: 1_400_000,
		Bundled: false, Downloadable: true,
	},
	{
		ID: "es", Label: "Spanish", NPMPackage: "dictionary-es",
		Version: "4.0.0", License: "GPL-3.0 OR LGPL-3.0 OR MPL-1.1", ApproxBytes: 850_000,
		Bundled: false, Downloadable: true,
	},
}

// Domains is the frozen v1 domain catalog. software-terms is a bundled curated
// MIT subset enabled by default; others download from @cspell/dict-* packages.
var Domains = []DomainSpec{
	{
		ID: "software-terms", Label: "Software terms",
		License: "MIT", ApproxBytes: 8_000,
		Bundled: true, Downloadable: false, DefaultOn: true,
	},
	{
		ID: "typescript", Label: "TypeScript / JavaScript",
		NPMPackage: "@cspell/dict-typescript", Version: "3.2.3",
		License:     "MIT",
		WordURL:     "https://cdn.jsdelivr.net/npm/@cspell/dict-typescript@3.2.3/dict/typescript.txt",
		ApproxBytes: 34_000, Bundled: false, Downloadable: true, DefaultOn: false,
	},
	{
		ID: "python", Label: "Python",
		NPMPackage: "@cspell/dict-python", Version: "4.2.27",
		License:     "MIT",
		WordURL:     "https://cdn.jsdelivr.net/npm/@cspell/dict-python@4.2.27/dict/python.txt",
		ApproxBytes: 105_000, Bundled: false, Downloadable: true, DefaultOn: false,
	},
	{
		ID: "data-science", Label: "Data science",
		NPMPackage: "@cspell/dict-data-science", Version: "2.0.14",
		License:     "MIT",
		WordURL:     "https://cdn.jsdelivr.net/npm/@cspell/dict-data-science@2.0.14/dict/data-science.txt",
		ApproxBytes: 20_000, Bundled: false, Downloadable: true, DefaultOn: false,
	},
}

// LanguageByID returns the catalog entry for id, or nil.
func LanguageByID(id string) *LanguageSpec {
	for i := range Languages {
		if Languages[i].ID == id {
			return &Languages[i]
		}
	}
	return nil
}

// DomainByID returns the catalog entry for id, or nil.
func DomainByID(id string) *DomainSpec {
	for i := range Domains {
		if Domains[i].ID == id {
			return &Domains[i]
		}
	}
	return nil
}

// DefaultDomainIDs returns domain IDs that should be on by default.
func DefaultDomainIDs() []string {
	out := make([]string, 0, 2)
	for _, d := range Domains {
		if d.DefaultOn {
			out = append(out, d.ID)
		}
	}
	return out
}

// IsKnownDomainID reports whether id is in the frozen domain catalog.
// Used by config normalize so known IDs stay single-sourced in this package.
func IsKnownDomainID(id string) bool {
	return DomainByID(id) != nil
}

// LanguageDownloadBase overrides the CDN base for language pack downloads.
// Empty in production (jsDelivr). Tests set this to an httptest URL.
var LanguageDownloadBase string

// LanguageURLs returns version-pinned jsDelivr URLs for a downloadable language.
func LanguageURLs(spec LanguageSpec) (aff, dic, license string) {
	base := LanguageDownloadBase
	if base == "" {
		base = "https://cdn.jsdelivr.net/npm/" + spec.NPMPackage + "@" + spec.Version
	}
	base = strings.TrimRight(base, "/")
	return base + "/index.aff", base + "/index.dic", base + "/license"
}

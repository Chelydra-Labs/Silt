package spellcheck

import "testing"

func TestLanguageByID(t *testing.T) {
	if LanguageByID("en-GB") == nil {
		t.Fatal("en-GB missing")
	}
	if LanguageByID("nope") != nil {
		t.Fatal("expected nil")
	}
}

func TestDefaultDomainIDs(t *testing.T) {
	ids := DefaultDomainIDs()
	if len(ids) != 1 || ids[0] != "software-terms" {
		t.Errorf("defaults = %v", ids)
	}
}

func TestLanguageURLsPinned(t *testing.T) {
	spec := *LanguageByID("en-GB")
	aff, dic, lic := LanguageURLs(spec)
	if aff != "https://cdn.jsdelivr.net/npm/dictionary-en-gb@3.0.0/index.aff" {
		t.Errorf("aff = %s", aff)
	}
	if dic != "https://cdn.jsdelivr.net/npm/dictionary-en-gb@3.0.0/index.dic" {
		t.Errorf("dic = %s", dic)
	}
	if lic != "https://cdn.jsdelivr.net/npm/dictionary-en-gb@3.0.0/license" {
		t.Errorf("lic = %s", lic)
	}
}

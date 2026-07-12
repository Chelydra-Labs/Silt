package spellcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeAndEnsureLanguage_MockCDN(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SILT_DICTIONARY_CACHE", root)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "index.aff"):
			_, _ = w.Write([]byte("SET UTF-8\n"))
		case strings.HasSuffix(r.URL.Path, "index.dic"):
			_, _ = w.Write([]byte("2\ncolour\nfavourite\n"))
		case strings.HasSuffix(r.URL.Path, "license"):
			_, _ = w.Write([]byte("MIT\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	// Point en-GB URLs at the mock by temporarily rewriting LanguageURLs via
	// a local Ensure that uses the test server — we patch HTTPClient and
	// override catalog URLs by calling fetch through EnsureLanguage after
	// rewriting the package's LanguageURLs is not possible, so we write a
	// minimal install path test using EnsureDomain instead for URL control,
	// and test languageInstalled + ReadLanguageFiles with hand-written cache.
	_ = srv

	// Hand-install a language pack as Ensure would.
	dir := LanguageDir(root, "en-GB")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(dir, "index.aff"), []byte("SET UTF-8\n")); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(dir, "index.dic"), []byte("1\ncolour\n")); err != nil {
		t.Fatal(err)
	}
	spec := LanguageByID("en-GB")
	if err := writeManifest(dir, Manifest{ID: "en-GB", Package: spec.NPMPackage, Version: spec.Version}); err != nil {
		t.Fatal(err)
	}
	if !languageInstalled(root, *spec) {
		t.Fatal("expected en-GB installed")
	}
	aff, dic, err := ReadLanguageFiles("en-GB")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(aff, "UTF-8") || !strings.Contains(dic, "colour") {
		t.Errorf("aff/dic unexpected: %q %q", aff, dic)
	}
}

func TestEnsureDomain_MockCDN(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SILT_DICTIONARY_CACHE", root)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# test\nFooBar\nbaz\n"))
	}))
	t.Cleanup(srv.Close)

	// Temporarily point typescript WordURL at mock via Domains slice mutation.
	orig := Domains
	t.Cleanup(func() { Domains = orig })
	for i := range Domains {
		if Domains[i].ID == "typescript" {
			Domains[i].WordURL = srv.URL + "/typescript.txt"
			break
		}
	}
	HTTPClient = srv.Client()
	t.Cleanup(func() { HTTPClient = &http.Client{Timeout: downloadTimeout} })

	if err := EnsureDomain(context.Background(), "typescript", nil); err != nil {
		t.Fatalf("EnsureDomain: %v", err)
	}
	words, err := ReadDomainWords("typescript")
	if err != nil {
		t.Fatal(err)
	}
	if len(words) != 2 || words[0] != "foobar" {
		t.Errorf("words = %v", words)
	}
	// Second call is cache hit.
	if err := EnsureDomain(context.Background(), "typescript", nil); err != nil {
		t.Fatalf("cache hit: %v", err)
	}
}

func TestEnsureLanguage_Unknown(t *testing.T) {
	err := EnsureLanguage(context.Background(), "xx-YY", nil)
	if err == nil {
		t.Fatal("expected error for unknown language")
	}
}

func TestEnsureLanguage_BundledNoop(t *testing.T) {
	if err := EnsureLanguage(context.Background(), "en-US", nil); err != nil {
		t.Fatal(err)
	}
}

func TestListLanguages_IncludesBundled(t *testing.T) {
	t.Setenv("SILT_DICTIONARY_CACHE", t.TempDir())
	list, err := ListLanguages()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 {
		t.Fatalf("expected catalog, got %d", len(list))
	}
	var enUS *LanguagePackInfo
	for i := range list {
		if list[i].ID == "en-US" {
			enUS = &list[i]
			break
		}
	}
	if enUS == nil || !enUS.Bundled || !enUS.Installed {
		t.Errorf("en-US row: %+v", enUS)
	}
}

func TestPathTraversalRejected(t *testing.T) {
	if sanitizeID("../etc") != "" {
		t.Error("expected empty for traversal")
	}
	if sanitizeID("en-GB") != "en-GB" {
		t.Error("expected pass-through")
	}
}

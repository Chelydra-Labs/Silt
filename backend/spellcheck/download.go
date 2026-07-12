package spellcheck

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	downloadTimeout = 2 * time.Minute
	maxLanguageFile = 8 << 20 // 8 MiB per aff/dic/license
	maxDomainFile   = 4 << 20 // 4 MiB per word list
	userAgent       = "Silt-spellcheck"
	downloadChunk   = 64 * 1024
)

// HTTPClient is overridable in tests.
var HTTPClient = &http.Client{Timeout: downloadTimeout}

// ProgressFunc is called with (bytesReceived, totalBytes) during downloads.
// total may be -1 when Content-Length is unknown.
type ProgressFunc func(received, total int64)

// EnsureLanguage downloads a language pack into the cache if missing or stale.
// Bundled languages are a no-op. Unknown IDs and path-unsafe IDs fail loudly.
// Concurrent calls for the same langID are serialized. ctx cancellation aborts
// the HTTP transfer and leaves no partial install (temp files cleaned).
func EnsureLanguage(ctx context.Context, langID string, onProgress ProgressFunc) error {
	return withEnsureLock("lang:"+langID, func() error {
		return ensureLanguageLocked(ctx, langID, onProgress)
	})
}

func ensureLanguageLocked(ctx context.Context, langID string, onProgress ProgressFunc) error {
	spec := LanguageByID(langID)
	if spec == nil {
		return fmt.Errorf("unknown language pack %q", langID)
	}
	if sanitizeID(langID) == "" {
		return fmt.Errorf("invalid language pack id %q", langID)
	}
	if spec.Bundled {
		return nil
	}
	if !spec.Downloadable {
		return fmt.Errorf("language pack %q is not downloadable", langID)
	}

	root, err := CacheRoot()
	if err != nil {
		return err
	}
	if languageInstalled(root, *spec) {
		return nil
	}

	dir := LanguageDir(root, spec.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create language cache: %w", err)
	}

	affURL, dicURL, licURL := LanguageURLs(*spec)
	var affData, dicData []byte
	var totalBytes int64
	for _, item := range []struct {
		url, name string
		dest      *[]byte
	}{
		{affURL, "index.aff", &affData},
		{dicURL, "index.dic", &dicData},
		{licURL, "license", nil},
	} {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("download cancelled: %w", err)
		}
		data, err := fetchBytes(ctx, item.url, maxLanguageFile, onProgress)
		if err != nil {
			if item.name == "license" {
				continue
			}
			return fmt.Errorf("download %s for %s: %w", item.name, langID, err)
		}
		if len(bytes.TrimSpace(data)) == 0 && item.name != "license" {
			return fmt.Errorf("download %s for %s: empty body", item.name, langID)
		}
		if err := atomicWrite(filepath.Join(dir, item.name), data); err != nil {
			return fmt.Errorf("write %s: %w", item.name, err)
		}
		if item.dest != nil {
			*item.dest = data
		}
		totalBytes += int64(len(data))
	}

	return writeManifest(dir, Manifest{
		ID:        spec.ID,
		Package:   spec.NPMPackage,
		Version:   spec.Version,
		FetchedAt: time.Now().UTC(),
		Bytes:     totalBytes,
		SHA256:    contentSHA256(affData, dicData),
	})
}

// EnsureDomain downloads a domain word list into the cache if missing or stale.
// Bundled domains are a no-op. Concurrent calls for the same id are serialized.
func EnsureDomain(ctx context.Context, domainID string, onProgress ProgressFunc) error {
	return withEnsureLock("domain:"+domainID, func() error {
		return ensureDomainLocked(ctx, domainID, onProgress)
	})
}

func ensureDomainLocked(ctx context.Context, domainID string, onProgress ProgressFunc) error {
	spec := DomainByID(domainID)
	if spec == nil {
		return fmt.Errorf("unknown domain pack %q", domainID)
	}
	if sanitizeID(domainID) == "" {
		return fmt.Errorf("invalid domain pack id %q", domainID)
	}
	if spec.Bundled {
		return nil
	}
	if !spec.Downloadable || spec.WordURL == "" {
		return fmt.Errorf("domain pack %q is not downloadable", domainID)
	}

	root, err := CacheRoot()
	if err != nil {
		return err
	}
	if domainInstalled(root, *spec) {
		return nil
	}

	dir := DomainDir(root, spec.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create domain cache: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("download cancelled: %w", err)
	}
	data, err := fetchBytes(ctx, spec.WordURL, maxDomainFile, onProgress)
	if err != nil {
		return fmt.Errorf("download domain %s: %w", domainID, err)
	}
	if strings.HasSuffix(strings.ToLower(spec.WordURL), ".gz") {
		data, err = gunzip(data)
		if err != nil {
			return fmt.Errorf("decompress domain %s: %w", domainID, err)
		}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("download domain %s: empty body", domainID)
	}
	if err := atomicWrite(filepath.Join(dir, "words.txt"), data); err != nil {
		return err
	}
	return writeManifest(dir, Manifest{
		ID:        spec.ID,
		Package:   spec.NPMPackage,
		Version:   spec.Version,
		FetchedAt: time.Now().UTC(),
		Bytes:     int64(len(data)),
		SHA256:    contentSHA256(data),
	})
}

// ReadLanguageFiles returns aff + dic text for a cached (or fails if missing).
// Bundled languages are not stored here — the frontend loads them from public assets.
func ReadLanguageFiles(langID string) (aff, dic string, err error) {
	spec := LanguageByID(langID)
	if spec == nil {
		return "", "", fmt.Errorf("unknown language pack %q", langID)
	}
	if spec.Bundled {
		return "", "", fmt.Errorf("language pack %q is bundled; load from app assets", langID)
	}
	root, err := CacheRoot()
	if err != nil {
		return "", "", err
	}
	if !languageInstalled(root, *spec) {
		return "", "", fmt.Errorf("language pack %q is not installed; call EnsureLanguagePack first", langID)
	}
	dir := LanguageDir(root, spec.ID)
	affBytes, err := os.ReadFile(filepath.Join(dir, "index.aff"))
	if err != nil {
		return "", "", err
	}
	dicBytes, err := os.ReadFile(filepath.Join(dir, "index.dic"))
	if err != nil {
		return "", "", err
	}
	return string(affBytes), string(dicBytes), nil
}

// ReadDomainWords returns parsed words for a domain pack.
// Bundled software-terms is read from BundledSoftwareTerms (set by embed in app).
func ReadDomainWords(domainID string) ([]string, error) {
	spec := DomainByID(domainID)
	if spec == nil {
		return nil, fmt.Errorf("unknown domain pack %q", domainID)
	}
	if spec.Bundled {
		if BundledSoftwareTerms == "" {
			return nil, fmt.Errorf("bundled domain %q content not available", domainID)
		}
		return ParseWordList(BundledSoftwareTerms), nil
	}
	root, err := CacheRoot()
	if err != nil {
		return nil, err
	}
	if !domainInstalled(root, *spec) {
		return nil, fmt.Errorf("domain pack %q is not installed; call EnsureDomainPack first", domainID)
	}
	data, err := os.ReadFile(filepath.Join(DomainDir(root, spec.ID), "words.txt"))
	if err != nil {
		return nil, err
	}
	return ParseWordList(string(data)), nil
}

// BundledSoftwareTerms holds the curated default software-terms word list.
// Set from //go:embed in the main package (or tests).
var BundledSoftwareTerms string

func fetchBytes(ctx context.Context, url string, maxBytes int64, onProgress ProgressFunc) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := HTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("download cancelled: %w", ctx.Err())
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}

	total := resp.ContentLength
	buf := make([]byte, 0, 64*1024)
	chunk := make([]byte, downloadChunk)
	var received int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("download cancelled: %w", err)
		}
		n, readErr := resp.Body.Read(chunk)
		if n > 0 {
			received += int64(n)
			if received > maxBytes {
				return nil, fmt.Errorf("response exceeds %d byte limit", maxBytes)
			}
			buf = append(buf, chunk[:n]...)
			if onProgress != nil {
				onProgress(received, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("download cancelled: %w", ctx.Err())
			}
			return nil, readErr
		}
	}
	return buf, nil
}

func gunzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, maxDomainFile+1))
}

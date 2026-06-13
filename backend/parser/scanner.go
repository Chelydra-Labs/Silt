package parser

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

var DateFileRegex = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.md$`)

type ScanResult struct {
	Path     string
	Notebook string
	Section  string
	Page     string
	Date     string
	Blocks   []ParsedBlock
	Tags     []string
	Warnings []string
	Err      error
}

// ScanWorkspace scans the vault directory recursively and returns all parsed file blocks and metadata.
func ScanWorkspace(vaultPath string, spacesPerTab int) ([]ScanResult, error) {
	// 1. Gather all markdown files
	var files []string
	err := filepath.WalkDir(vaultPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip system and hidden directories
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Only parse markdown files
		if strings.ToLower(filepath.Ext(path)) == ".md" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan directories: %w", err)
	}

	if len(files) == 0 {
		return nil, nil
	}

	// 2. Parse files in parallel using a worker pool
	numWorkers := runtime.NumCPU()
	if numWorkers > 8 {
		numWorkers = 8
	}
	if numWorkers > len(files) {
		numWorkers = len(files)
	}

	pathsChan := make(chan string, len(files))
	for _, f := range files {
		pathsChan <- f
	}
	close(pathsChan)

	resultsChan := make(chan ScanResult, len(files))
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range pathsChan {
				res := parseSingleFile(path, vaultPath, spacesPerTab)
				resultsChan <- res
			}
		}()
	}

	wg.Wait()
	close(resultsChan)

	// Collect results
	var results []ScanResult
	for res := range resultsChan {
		results = append(results, res)
	}

	return results, nil
}

func parseSingleFile(path string, vaultPath string, spacesPerTab int) ScanResult {
	res := ScanResult{Path: path}

	// 1. Resolve default notebook, section, page, and date from file path.
	//
	// Hierarchy: <vault>/<notebook>/<section>/<page>/<file>.md
	//   - page     = parent dir of the file (the streaming unit)
	//   - section  = page's parent dir
	//   - notebook = section's parent dir (a child of the vault root)
	relPath, err := filepath.Rel(vaultPath, path)
	if err != nil {
		res.Err = err
		return res
	}

	relPathClean := filepath.ToSlash(relPath)
	parts := strings.Split(relPathClean, "/")
	filename := parts[len(parts)-1]

	var notebook, section, page string
	// ancestors are the path segments excluding the filename itself.
	ancestors := parts[:len(parts)-1]
	switch {
	case len(ancestors) >= 3:
		notebook = ancestors[len(ancestors)-3]
		section = ancestors[len(ancestors)-2]
		page = ancestors[len(ancestors)-1]
	default:
		// Files must live three levels beneath the vault root
		// (notebook/section/page/file). Anything shallower is a layout
		// error we surface rather than silently mis-bucket.
		res.Warnings = append(res.Warnings, fmt.Sprintf("skipped %q: expected to live under <vault>/<notebook>/<section>/<page>/", relPathClean))
		res.Err = nil
		return res
	}

	// Extract date from filename if possible, otherwise use modification date
	dateStr := ""
	if matches := DateFileRegex.FindStringSubmatch(filename); len(matches) > 1 {
		dateStr = matches[1]
	} else {
		// Use modification time
		info, err := os.Stat(path)
		if err == nil {
			dateStr = info.ModTime().Format("2006-01-02")
		} else {
			dateStr = time.Now().Format("2006-01-02")
		}
	}

	// 2. Read and parse file content
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		res.Err = err
		return res
	}

	blocks, meta, newContent, modified, err := ParseFileContent(string(contentBytes), notebook, section, page, dateStr, spacesPerTab)
	if err != nil {
		res.Err = err
		return res
	}
	res.Warnings = meta.Warnings

	// 3. Write back atomically if modified (i.e. UUIDs injected)
	if modified {
		if err := WriteFileAtomic(path, []byte(newContent)); err != nil {
			res.Err = fmt.Errorf("failed to write file atomically: %w", err)
			return res
		}
	}

	res.Notebook = meta.Notebook
	res.Section = meta.Section
	res.Page = meta.Page
	res.Date = meta.Date
	res.Blocks = blocks
	res.Tags = meta.Tags

	return res
}

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

	"notes-sharp/backend/db"
)

var DateFileRegex = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.md$`)

type ScanResult struct {
	Path     string
	Notebook string
	Section  string
	Date     string
	Blocks   []ParsedBlock
	Tags     []string
	Err      error
}

// ScanAndIndexWorkspace scans the vault directory recursively and updates the SQLite index.
func ScanAndIndexWorkspace(vaultPath string, dm *db.DatabaseManager, spacesPerTab int) (time.Duration, int, error) {
	startTime := time.Now()

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
		return 0, 0, fmt.Errorf("failed to scan directories: %w", err)
	}

	if len(files) == 0 {
		return time.Since(startTime), 0, nil
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

	// 3. Collect results and insert into SQLite in a single transaction
	tx, err := dm.DB.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to start index transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare SQLite statements
	stmtBlock, err := tx.Prepare("INSERT INTO blocks (id, parent_id, notebook, section, file_date, depth, type, raw_content, clean_content, line_number) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return 0, 0, err
	}
	defer stmtBlock.Close()

	stmtTask, err := tx.Prepare("INSERT INTO tasks (block_id, status, owner, start_date, due_date, priority) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return 0, 0, err
	}
	defer stmtTask.Close()

	stmtTag, err := tx.Prepare("INSERT INTO tags (block_id, raw_path, level_0, level_1, level_2) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return 0, 0, err
	}
	defer stmtTag.Close()

	indexedCount := 0

	for res := range resultsChan {
		if res.Err != nil {
			// Log error but continue with other files
			fmt.Printf("Error parsing file %s: %v\n", res.Path, res.Err)
			continue
		}

		// Clear old blocks for this file first
		if err := dm.ClearFileBlocks(tx, res.Notebook, res.Section, res.Date); err != nil {
			return 0, 0, fmt.Errorf("failed to clear blocks for %s: %w", res.Path, err)
		}

		for _, block := range res.Blocks {
			var parentID interface{}
			if block.ParentID != "" {
				parentID = block.ParentID
			}

			_, err = stmtBlock.Exec(block.ID, parentID, res.Notebook, res.Section, res.Date, block.Depth, string(block.Type), block.RawText, block.CleanText, block.LineNumber)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to insert block %s: %w", block.ID, err)
			}

			if block.Type == BlockTask {
				var owner, startDate, dueDate interface{}
				if block.Owner != "" {
					owner = block.Owner
				}
				if block.StartDate != "" {
					startDate = block.StartDate
				}
				if block.DueDate != "" {
					dueDate = block.DueDate
				}
				_, err = stmtTask.Exec(block.ID, block.Status, owner, startDate, dueDate, block.Priority)
				if err != nil {
					return 0, 0, fmt.Errorf("failed to insert task for block %s: %w", block.ID, err)
				}
			}

			// Extract tags from this block
			tags := db.ExtractTags(block.RawText)
			// Associate file frontmatter tags to the first block
			if block.LineNumber == 1 || len(res.Blocks) == 1 {
				for _, ft := range res.Tags {
					trimmedFT := strings.TrimPrefix(ft, "#")
					found := false
					for _, t := range tags {
						if t == trimmedFT {
							found = true
							break
						}
					}
					if !found && trimmedFT != "" {
						tags = append(tags, trimmedFT)
					}
				}
			}

			for _, tagPath := range tags {
				parts := strings.Split(tagPath, "/")
				var level0, level1, level2 interface{}
				if len(parts) > 0 {
					level0 = parts[0]
				}
				if len(parts) > 1 {
					level1 = parts[1]
				}
				if len(parts) > 2 {
					level2 = parts[2]
				}
				_, err = stmtTag.Exec(block.ID, tagPath, level0, level1, level2)
				if err != nil {
					continue
				}
			}
		}

		indexedCount++
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return time.Since(startTime), indexedCount, nil
}

func parseSingleFile(path string, vaultPath string, spacesPerTab int) ScanResult {
	res := ScanResult{Path: path}

	// 1. Resolve default notebook, section, and date from file path
	relPath, err := filepath.Rel(vaultPath, path)
	if err != nil {
		res.Err = err
		return res
	}

	// Clean path separators to forward slash
	relPathClean := filepath.ToSlash(relPath)
	parts := strings.Split(relPathClean, "/")

	notebook := "General"
	section := "General"
	filename := parts[len(parts)-1]

	if len(parts) >= 3 {
		notebook = parts[0]
		section = strings.Join(parts[1:len(parts)-1], "/")
	} else if len(parts) == 2 {
		notebook = parts[0]
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

	blocks, meta, newContent, modified, err := ParseFileContent(string(contentBytes), notebook, section, dateStr, spacesPerTab)
	if err != nil {
		res.Err = err
		return res
	}

	// 3. Write back atomically if modified (i.e. UUIDs injected)
	if modified {
		if err := WriteFileAtomic(path, []byte(newContent)); err != nil {
			res.Err = fmt.Errorf("failed to write file atomically: %w", err)
			return res
		}
	}

	res.Notebook = meta.Notebook
	res.Section = meta.Section
	res.Date = meta.Date
	res.Blocks = blocks
	res.Tags = meta.Tags

	return res
}

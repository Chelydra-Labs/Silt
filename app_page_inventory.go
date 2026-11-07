package main

import (
	"silt/backend/db"
	"silt/backend/parser"
)

// scanInventory derives the page inventory one scan pass publishes, plus
// whether that pass saw every indexed file.
//
// complete is DEFAULT-DENY and only ever means "the walk entered every
// directory and every discovered file parsed": a walk error (a subtree the
// walker could not enter) or any per-file read/parse error means the scan is
// blind to pages that may still exist, so ReconcilePageInventory must not
// delete on it. Deliberate skips (symlink not followed, dot-directories,
// attachments/) never make a pass incomplete — their contents are outside the
// index by definition, so treating them as blindness would suppress healing
// forever in any vault that contains one.
//
// A file the path rules cannot place (a root-level .md, warned but not an
// error) is never indexable either, so its absence proves nothing about a
// live page and does not arm the destructive half.
func scanInventory(results []parser.ScanResult, walkErrs []string, source string) (live []db.PageLoc, complete bool) {
	live = make([]db.PageLoc, 0, len(results))
	complete = len(walkErrs) == 0
	for _, res := range results {
		if res.Err != nil {
			complete = false
			continue
		}
		if res.Notebook == "" || res.Page == "" {
			continue
		}
		live = append(live, db.PageLoc{
			Source:   source,
			Notebook: res.Notebook,
			Section:  res.Section,
			Page:     res.Page,
		})
	}
	return live, complete
}

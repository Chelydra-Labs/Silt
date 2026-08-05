package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"silt/backend/dependencies"
)

// TaskCheckboxRegex matches the GFM task list prefix: optional
// indentation, a checkbox marker (`[ ]`, `[x]`, or `[/]`), and the
// remainder of the line. This is the ONLY structural regex for tasks —
// all metadata (owner, dates, priority, pin, progress) is extracted by
// the [key:: value] token scanner (scanTaskTokens) from the remainder,
// not by positional regex groups.
//
// This drops the legacy `TASK` keyword entirely — any GFM checkbox item
// is a task, matching CommonMark/GFM convention. The token scanner
// makes the metadata order-independent and extensible (new metadata
// type = new key in the switch, no regex change). The token format
// follows the Dataview inline metadata standard ([key:: value]) so
// files are interoperable with the Dataview-compatible ecosystem.
//
// See ARCHITECTURE.md §0 "Storage-of-Truth Tiers" for the design
// rationale: task metadata is file-resident user intent, and the
// [key:: value] format is the de facto standard for per-block metadata
// in markdown.
var TaskCheckboxRegex = regexp.MustCompile(`^([\s]*)-\s\[([ x/])\]\s+(.*)$`)

// TaskTokenRegex captures a single Dataview [key:: value] inline metadata
// token. The double-colon `::` is the signature that distinguishes a
// metadata field from a markdown link `[text](url)` or regular bracketed
// text — no other markdown syntax uses `::`.
//
// Supported keys (see scanTaskTokens for the dispatch table):
//
//	[due:: DATE]       — due date (YYYY-MM-DD)
//	[start:: DATE]     — start date (YYYY-MM-DD)
//	[owner:: name]     — owner/assignee
//	[priority:: N]     — priority (1=critical, 2=normal, 3=low)
//	[p:: N]            — priority shorthand (alias for [priority:: N])
//	[pin:: true]       — pinned (boolean; presence also implies true)
//	[progress:: N]     — progress (0-100)
//	[prog:: N]         — progress shorthand
//	[recur:: RULE]     — recurrence rule (e.g. `every week`, `every 2 months`)
//
// The scanner is the single source of truth for token → ParsedBlock
// field mapping; adding a new metadata type is a one-line addition to
// the switch in scanTaskTokens. Keys are case-insensitive.
var TaskTokenRegex = regexp.MustCompile(`\[([\w]+)::\s*([^\]]*)\]`)

// whitespaceRun collapses consecutive whitespace into a single space. Used
// in scanTaskTokens to normalize the description after token stripping.
// Hoisted to package level so the regex is compiled once, not per line.
var whitespaceRun = regexp.MustCompile(`\s+`)

// IDRegex captures the trailing block-identity comment. The format is:
//
//	<!-- id: uuid -->
//
// or (with per-block file_date, post per-day-file-model removal):
//
//	<!-- id: uuid @ YYYY-MM-DD -->
//
// The date suffix is optional for backward compatibility with notes created
// under the old per-day-file model (it is assigned during migration).
var IDRegex = regexp.MustCompile(`<!-- id: ([a-f0-9\-]{36})(?:\s*@\s*(\d{4}-\d{2}-\d{2}))?\s*-->\s*$`)

// BlockRefRegex matches a global block reference ((uuid)). Read-only detector
// used by the resolver; it never injects IDs (code-fence protection in
// ParseFileContent already prevents ID injection inside ``` blocks).
var BlockRefRegex = regexp.MustCompile(`\(\(([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\)\)`)

// EmbedRegex matches a live block embed {{embed:uuid}}.
var EmbedRegex = regexp.MustCompile(`\{\{embed:([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\}\}`)

// PageLinkRegex matches a wiki / page link [[target]] with optional
// #heading and |alias (#545). Obsidian-compatible grammar. Read-only detector
// used by the resolver and the reverse-index extractor; the frontend converter
// owns the editor-side tokenization. Capture groups: 1=target, 2=heading,
// 3=alias. The target excludes brackets, pipe, and hash so the match stops at
// the first heading/alias delimiter.
var PageLinkRegex = regexp.MustCompile(`\[\[([^\[\]\|#]+)(?:#([^\[\]\|]+))?(?:\|([^\[\]]+))?\]\]`)

// PageLinkTarget extracts the raw target string from a [[…]] match (without
// the heading/alias portions), used by the reverse-index extractor.
func PageLinkTarget(raw string) string {
	m := PageLinkRegex.FindStringSubmatch(raw)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// NumberedListRegex matches numbered list prefixes like 1. or 1) followed by space.
var NumberedListRegex = regexp.MustCompile(`^(\d+[.)]\s)`)

func generateUUIDv4() string {
	return uuid.New().String()
}

// EnsureBlockID extracts (or assigns) the block identity — both the UUID and
// the per-block file_date — from the trailing comment. Returns:
//
//	id        — the UUID ("" for empty lines)
//	fileDate  — the date from the comment, or "" if none was embedded
//	newLine   — the line with the comment preserved/assigned
//	modified  — true if a new comment was injected (caller should rewrite)
func EnsureBlockID(line string) (id, fileDate, newLine string, modified bool) {
	clean := strings.TrimSpace(line)
	if clean == "" {
		return "", "", line, false
	}
	matches := IDRegex.FindStringSubmatch(line)
	if len(matches) > 1 {
		id = matches[1]
		if len(matches) > 2 {
			fileDate = matches[2]
		}
		return id, fileDate, line, false
	}

	newID := generateUUIDv4()
	today := time.Now().Format("2006-01-02")
	cleanLine := strings.TrimRight(line, "\r\n")
	newLine = fmt.Sprintf("%s <!-- id: %s @ %s -->", cleanLine, newID, today)
	return newID, today, newLine, true
}

func CleanLineID(line string) string {
	return IDRegex.ReplaceAllString(line, "")
}

// stripInlineID removes a trailing block-identity comment from a line and
// returns the trimmed result. This handles backward compatibility with the
// old on-disk format where each line within a multi-line block (table row,
// details line) carried its own inline id comment. The unified region-block
// model puts the id on a dedicated trailing line instead, but existing files
// may still have inline ids that need to be stripped before region detection.
func stripInlineID(line string) string {
	return strings.TrimSpace(IDRegex.ReplaceAllString(line, ""))
}

// extractInlineID returns the uuid from a line's trailing id comment, or "".
func extractInlineID(line string) string {
	m := IDRegex.FindStringSubmatch(line)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// NormalizeDate canonicalizes a page date to YYYY-MM-DD. Accepts a literal
// YYYY-MM-DD or an M/D/YY (or MM/DD/YYYY) form; anything else is returned
// unchanged so the caller can validate/reject it. Exported so the core-
// metadata write path can normalize a user-entered date before writing it,
// matching the parser's read-side normalization (one source of truth).
func NormalizeDate(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return ""
	}

	// Try standard YYYY-MM-DD
	if _, err := time.Parse("2006-01-02", d); err == nil {
		return d
	}

	// Normalize M/D/YY or MM/DD/YYYY
	parts := strings.Split(d, "/")
	if len(parts) == 3 {
		m := parts[0]
		day := parts[1]
		y := parts[2]

		if len(m) == 1 {
			m = "0" + m
		}
		if len(day) == 1 {
			day = "0" + day
		}
		if len(y) == 2 {
			y = "20" + y
		}
		return fmt.Sprintf("%s-%s-%s", y, m, day)
	}

	return d
}

// recoverStringList coerces a raw frontmatter value into a []string for the
// page-level list fields (tags, aliases), tolerating the shapes a hand-authored
// YAML value can take: a YAML list (decoded by yaml.v3 as []any, or []string),
// or a bare scalar string (e.g. `aliases: foo`). Used in the raw-map recovery
// path when the typed FileMetadata decode failed on one bad field, so a
// surviving list field isn't silently dropped alongside the failing one.
// Returns nil for an empty/unknown value so the caller's len()==0 guard skips
// the assignment and preserves the typed-decode value when that succeeded.
func recoverStringList(v any) []string {
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	default:
		return nil
	}
}

func parseLeadingIndent(line string, spacesPerTab int) int {
	if spacesPerTab <= 0 {
		spacesPerTab = 4
	}
	tabs := 0
	spaces := 0
	for _, char := range line {
		if char == '\t' {
			tabs++
		} else if char == ' ' {
			spaces++
		} else {
			break
		}
	}
	return tabs + (spaces / spacesPerTab)
}

// scanTaskTokens extracts all Dataview [key:: value] inline metadata
// tokens from a task line's remainder (the text after the checkbox).
// Returns the parsed fields, the description with known tokens stripped,
// and any unrecognised tokens preserved verbatim for forward-compatible
// round-tripping (Dataview-compatible interop — SPECS.md §4.1).
//
// The function is the single source of truth for token → field mapping.
// Adding a new metadata type is a one-line addition to the switch below.
// Unknown keys are preserved in extraTokens so the file round-trips
// without data loss.
func scanTaskTokens(remainder string) (owner, startDate, dueDate string, priority int, pinned *bool, progress int, recurrence, description string, blockedBy []string, extraTokens []string, createdAt, completedAt string, manualOrder int, modifiedAt, estimate string) {
	priority = 3 // default; 0 from the regex means "not set"
	progress = 0
	matches := TaskTokenRegex.FindAllStringSubmatch(remainder, -1)
	// Strip all [key:: value] tokens from the remainder to get the
	// description. Do this on the full remainder (not per-match) so the
	// regex's global replace handles overlapping/nested brackets safely.
	description = strings.TrimSpace(TaskTokenRegex.ReplaceAllString(remainder, ""))
	// Collapse multiple spaces left by token removal (e.g. "text  more"
	// after a token between them was stripped).
	description = whitespaceRun.ReplaceAllString(description, " ")

	for _, m := range matches {
		key := strings.ToLower(m[1])
		val := strings.TrimSpace(m[2])
		switch key {
		case "due":
			dueDate = NormalizeDate(val)
		case "start":
			startDate = NormalizeDate(val)
		case "owner", "o":
			owner = val
		case "priority", "p":
			if val != "" {
				fmt.Sscanf(val, "%d", &priority)
			}
		case "pin", "pinned":
			// Tri-state (#123): the token's PRESENCE is what matters —
			// any [pin:: ...] sets a non-nil pointer so the renderer can
			// distinguish "explicitly unpinned" (&false → [pin:: false])
			// from "no pin token" (nil → omit). Only explicit truthy
			// values ("true"/"yes"/"1") set &true; anything else (false,
			// "no", "0", empty, typos) sets &false. The renderer emits
			// exactly one pin token from the pointer, so toggling via the
			// UI can never produce two competing tokens.
			v := strings.ToLower(val)
			b := v == "true" || v == "yes" || v == "1"
			pinned = &b
		case "progress", "prog":
			if val != "" {
				fmt.Sscanf(val, "%d", &progress)
				if progress < 0 {
					progress = 0
				}
				if progress > 100 {
					progress = 100
				}
			}
		case "recur", "recurrence":
			// Normalize: collapse internal whitespace runs and lowercase
			// so `[Recur::  Every  WEEK ]` and `[recur:: every week]`
			// parse identically and round-trip to a canonical form. The
			// grammar (`every <unit>` / `every N <units>`) is validated
			// by the resolver (backend/recurrence), not here — the parser
			// stores the token verbatim-ish so an unsupported rule still
			// round-trips instead of being silently dropped.
			recurrence = whitespaceRun.ReplaceAllString(strings.ToLower(val), " ")
		case "blocked_by":
			// Task dependency graph (#301): the value is one or more
			// space-separated ((uuid)) block references naming this
			// task's prerequisites. ExtractRefs de-duplicates and
			// tolerates stray prose so a hand-edited token still indexes.
			// Cycles are prevented at write time by the IPC setter; the
			// parser only round-trips what's on disk.
			blockedBy = dependencies.ExtractRefs(val)
		case "created":
			// Task lifecycle timestamp (#417): ISO 8601 local
			// (YYYY-MM-DDTHH:MM:SS), no timezone. Stored verbatim so a
			// hand-edited value round-trips; only genuinely-new tasks
			// get the token minted (no backfill of existing tasks).
			createdAt = val
		case "completed":
			// Time of the most recent DONE transition (#417). Cleared on
			// reopen; overwritten on re-complete. Same format as created.
			completedAt = val
		case "order":
			// 1-based manual sort position among all TASK blocks in the
			// file (#417). 0 means "not set" and the renderer omits the
			// token. Parsed like priority/progress: a non-integer value
			// leaves manualOrder at its zero default.
			if val != "" {
				if n, err := strconv.Atoi(val); err == nil {
					manualOrder = n
				}
			}
		case "modified":
			// Last task-line touch (#440). ISO local, no timezone. No
			// backfill — only mutation paths stamp this.
			modifiedAt = val
		case "estimate":
			// Time budget (#439). Store raw value; minutes projected at
			// index. Empty clears. Invalid values still round-trip so
			// hand-edits aren't silently dropped (UI validates on write).
			estimate = val
		default:
			// Unrecognised key — preserve the full [key:: value] token
			// verbatim so it survives the parse → render round-trip.
			extraTokens = append(extraTokens, m[0])
		}
	}
	return
}

// scanNoteTokens extracts the NOTE-block comment-attribution tokens from a
// note line's clean text (#418). It recognises ONLY two keys — `author` and
// `ts` — and is invoked exclusively from the NOTE branch of ParseLine. The
// task and NOTE token spaces are DISJOINT BY DESIGN: `scanTaskTokens` (TASK
// blocks only) has no `author`/`ts` cases, so task queries (the `tasks`
// table) never pick up comment attribution, and this function has no task
// keys (owner/due/etc.), so NOTE blocks never absorb task metadata. An
// `[author::]` or `[ts::]` on a TASK line falls through to ExtraTokens.
//
// Returns the parsed fields plus the description with ONLY the two
// recognized tokens stripped. Other `[key:: value]` text on a NOTE line is
// left in CleanText verbatim — this matches the pre-#418 behavior (NOTE
// blocks never ran the task-token scanner) and preserves byte-for-byte
// round-trip for notes carrying arbitrary Dataview tokens (e.g. a hand-typed
// `[project:: alpha]` on a note stays in the rendered text, not ExtraTokens,
// since NOTE has no ExtraTokens path).
func scanNoteTokens(cleanText string) (author, timestamp, description string) {
	description = cleanText
	for _, m := range TaskTokenRegex.FindAllStringSubmatch(cleanText, -1) {
		key := strings.ToLower(m[1])
		val := strings.TrimSpace(m[2])
		switch key {
		case "author":
			author = val
			description = strings.Replace(description, m[0], "", 1)
		case "ts":
			timestamp = val
			description = strings.Replace(description, m[0], "", 1)
		}
	}
	description = strings.TrimSpace(description)
	description = whitespaceRun.ReplaceAllString(description, " ")
	return
}

func ParseLine(line string, lineNumber int, spacesPerTab int) (ParsedBlock, string, bool) {
	blockID, blockFileDate, newLine, modified := EnsureBlockID(line)
	if blockID == "" {
		// Empty line, return empty note block
		return ParsedBlock{
			ID:         "",
			Type:       BlockNote,
			RawText:    line,
			CleanText:  "",
			LineNumber: lineNumber,
		}, line, false
	}

	cleanLine := CleanLineID(newLine)
	cleanLineTrimmed := strings.TrimSpace(cleanLine)

	// Check if it matches the GFM task checkbox pattern: `- [ ]`, `- [/]`, `- [x]`.
	// Apply to cleanLine (ID comment stripped) so the remainder fed to
	// scanTaskTokens does not contain the trailing <!-- id: ... --> comment.
	if matches := TaskCheckboxRegex.FindStringSubmatch(cleanLine); matches != nil {
		indent := matches[1]
		checkbox := matches[2]
		remainder := matches[3]

		// Determine status from checkbox state (GFM convention + Silt's [/] for DOING)
		status := "TODO"
		if checkbox == "/" {
			status = "DOING"
		} else if checkbox == "x" {
			status = "DONE"
		}

		// Scan for [key:: value] metadata tokens in the remainder.
		owner, startDate, dueDate, priority, pinned, progress, recurrence, description, blockedBy, extraTokens, createdAt, completedAt, manualOrder, modifiedAt, estimate := scanTaskTokens(remainder)

		depth := parseLeadingIndent(indent, spacesPerTab)

		return ParsedBlock{
			ID:          blockID,
			Type:        BlockTask,
			Depth:       depth,
			RawText:     newLine,
			CleanText:   description,
			Status:      status,
			Owner:       owner,
			StartDate:   startDate,
			DueDate:     dueDate,
			Priority:    priority,
			Pinned:      pinned,
			Progress:    progress,
			Recurrence:  recurrence,
			BlockedBy:   blockedBy,
			CreatedAt:   createdAt,
			CompletedAt: completedAt,
			ManualOrder: manualOrder,
			ModifiedAt:  modifiedAt,
			Estimate:    estimate,
			ExtraTokens: extraTokens,
			LineNumber:  lineNumber,
			FileDate:    blockFileDate,
		}, newLine, modified
	}

	// Check if it's a Header
	if strings.HasPrefix(cleanLineTrimmed, "#") {
		// Count header level
		level := 0
		for level < len(cleanLineTrimmed) && cleanLineTrimmed[level] == '#' {
			level++
		}
		// Must be followed by space or end of string
		if level < len(cleanLineTrimmed) && cleanLineTrimmed[level] == ' ' {
			headerText := cleanLineTrimmed[level+1:]
			return ParsedBlock{
				ID:         blockID,
				Type:       BlockHeader,
				Depth:      level,
				RawText:    newLine,
				CleanText:  strings.TrimSpace(headerText),
				LineNumber: lineNumber,
				FileDate:   blockFileDate,
			}, newLine, modified
		}
	}

	// Bullet note check (optional cleaning of bullet markers like "- ", "* ", "+ ", or numbered list prefixes "1. ", "1) ")
	// Detect bullet prefix on cleanLine (pre-TrimSpace). cleanLineTrimmed is
	// TrimSpace'd for header/task detection above, but for bullet stripping we
	// need the trailing space to survive — a trailing-id-only line like
	// "- <!-- id: -->" becomes "- " after CleanLineID, which TrimSpace reduces
	// to "-", making HasPrefix fail (#570).
	depth := parseLeadingIndent(newLine, spacesPerTab)
	rawCleaned := cleanLineTrimmed
	bulletBase := strings.TrimLeft(cleanLine, " \t")
	if strings.HasPrefix(bulletBase, "- ") || strings.HasPrefix(bulletBase, "* ") || strings.HasPrefix(bulletBase, "+ ") {
		rawCleaned = bulletBase[2:]
	} else if m := NumberedListRegex.FindString(bulletBase); m != "" {
		rawCleaned = bulletBase[len(m):]
	}

	// NOTE-block comment attribution (#418): scan the cleaned text for
	// [author::] / [ts::] tokens (NOTE-only — scanTaskTokens stays
	// TASK-only, so the token spaces are disjoint). The recognized tokens
	// are stripped from CleanText and mapped to dedicated fields; they do
	// NOT fall through to ExtraTokens.
	author, timestamp, noteDescription := scanNoteTokens(strings.TrimSpace(rawCleaned))

	return ParsedBlock{
		ID:         blockID,
		Type:       BlockNote,
		Depth:      depth,
		RawText:    newLine,
		CleanText:  noteDescription,
		Author:     author,
		Timestamp:  timestamp,
		LineNumber: lineNumber,
		FileDate:   blockFileDate,
	}, newLine, modified
}

// codeFenceLen returns the number of leading backticks on a (already
// TrimSpace'd) line, or 0 if it is not a fenced code boundary. GFM requires
// at least three backticks for a fence, and a closing fence must carry at
// least as many backticks as the opening fence — so callers compare
// `codeFenceLen(closer) >= openerLen` to find a matching close (this is what
// lets a code sample that itself contains a ``` line round-trip behind a
// longer outer fence).
func codeFenceLen(trimmedLine string) int {
	n := 0
	for n < len(trimmedLine) && trimmedLine[n] == '`' {
		n++
	}
	if n >= 3 {
		return n
	}
	return 0
}

// isCodeFence reports whether a (already TrimSpace'd) line is a GFM fenced
// code boundary — three or more backticks. Used by both ParseFileContent and
// RenderFileContent so the two paths agree on what counts as a fence.
func isCodeFence(trimmedLine string) bool {
	return codeFenceLen(trimmedLine) >= 3
}

// isClosingFence reports whether a (TrimSpace'd) line is a valid GFM CLOSING
// fence for an opener of openerLen backticks. A closer must have at least
// openerLen backticks AND nothing but whitespace after them — an info string
// (e.g. ```js) is allowed on an OPENER but disqualifies a closer. Without this
// check a 3-backtick block documenting another fence (```js) would close
// prematurely, silently corrupting the file. Openers are still detected with
// plain codeFenceLen (info strings are legal there).
func isClosingFence(trimmed string, openerLen int) bool {
	n := codeFenceLen(trimmed)
	return n >= openerLen && strings.TrimSpace(trimmed[n:]) == ""
}

// ---- GFM table detection (#310) -------------------------------------------
// A GFM table is a run of pipe-prefixed lines: a header row immediately
// followed by a separator row (|---|), then zero or more data rows. The
// separator accepts alignment markers (:---:, :---, ---:). Ported from the
// frontend converter so the parser is the single source of truth for table
// detection (the unified region-block model).
var gfmRowRe = regexp.MustCompile(`^\|.*\|$`)
var gfmSepRe = regexp.MustCompile(`^\|[\s:|-]+\|$`)

func isGfmRow(s string) bool       { return gfmRowRe.MatchString(stripInlineID(s)) }
func isGfmSeparator(s string) bool { return gfmSepRe.MatchString(stripInlineID(s)) }

// isGfmTableStart reports whether lines[idx] starts a GFM table run: a pipe
// row immediately followed by a separator row (the two-line minimum that
// distinguishes a table from a stray pipe-prefixed note).
func isGfmTableStart(lines []string, idx int) bool {
	return idx+1 < len(lines) && isGfmRow(lines[idx]) && isGfmSeparator(lines[idx+1])
}

// ---- <details> HTML detection (#310) --------------------------------------
// A foldable <details> region runs from <details> to the matching </details>,
// depth-counted for nesting. Ported from the frontend converter.
var detailsOpenRe = regexp.MustCompile(`(?i)^<details(?:\s+[^>]*)?>$`)
var detailsCloseRe = regexp.MustCompile(`(?i)^</details>$`)

func isDetailsOpen(s string) bool  { return detailsOpenRe.MatchString(stripInlineID(s)) }
func isDetailsClose(s string) bool { return detailsCloseRe.MatchString(stripInlineID(s)) }

// ---- Callout detection (#308) ---------------------------------------------
// An Obsidian-style callout is a `>` line whose body starts with `[!variant]`.
// The region absorbs all subsequent `>` lines (including bare `>` for paragraph
// breaks) and ends at the first non-`>` line. A plain `> text` (no `[!`) is NOT
// a callout — it stays a NOTE with a quote prefix.
var calloutOpenRe = regexp.MustCompile(`(?i)^>\s*\[!(note|info|tip|warning|danger|success|quote)\]`)
var gtPrefixRe = regexp.MustCompile(`^>\s?`)

func isCalloutOpen(s string) bool { return calloutOpenRe.MatchString(strings.TrimSpace(s)) }
func hasGtPrefix(s string) bool   { return gtPrefixRe.MatchString(strings.TrimSpace(s)) }

// ---- Region kind discriminator --------------------------------------------

// regionKind identifies which multi-line region shape was detected at a given
// line. Used by accumulateRegion to dispatch to the right closer logic.
type regionKind int

const (
	regionNone    regionKind = iota
	regionCode               // ``` fence
	regionTable              // GFM pipe table
	regionDetails            // <details> HTML
	regionCallout            // > [!variant] Obsidian callout
)

// detectRegionKind checks what kind of managed multi-line region starts at
// lines[idx], if any. Code fences take priority (a ``` line inside what looks
// like a table row is still a fence). Returns regionNone for non-region lines.
func detectRegionKind(lines []string, idx int) regionKind {
	trimmed := strings.TrimSpace(lines[idx])
	if isCodeFence(trimmed) {
		return regionCode
	}
	if isGfmTableStart(lines, idx) {
		return regionTable
	}
	if isDetailsOpen(trimmed) {
		return regionDetails
	}
	if isCalloutOpen(lines[idx]) {
		return regionCallout
	}
	return regionNone
}

// findRegionCloser returns the index of the last line that is PART OF the
// region starting at openIdx, or -1 if the region is unterminated. The return
// value is the inclusive end of the region content (the closer itself for
// code/details; the last pipe row for tables).
func findRegionCloser(lines []string, openIdx int, kind regionKind) int {
	trimmed := strings.TrimSpace(lines[openIdx])
	switch kind {
	case regionCode:
		openerLen := codeFenceLen(trimmed)
		for j := openIdx + 1; j < len(lines); j++ {
			if isClosingFence(strings.TrimSpace(lines[j]), openerLen) {
				return j
			}
		}
		return -1
	case regionTable:
		endIdx := openIdx
		for endIdx < len(lines) && isGfmRow(lines[endIdx]) {
			endIdx++
		}
		return endIdx - 1
	case regionDetails:
		depth := 1
		for j := openIdx + 1; j < len(lines); j++ {
			if isDetailsOpen(lines[j]) {
				depth++
			} else if isDetailsClose(lines[j]) {
				depth--
				if depth == 0 {
					return j
				}
			}
		}
		return -1
	case regionCallout:
		// The callout region absorbs all consecutive `>` lines (including bare
		// `>` paragraph breaks). It ends at the first non-`>` line.
		endIdx := openIdx + 1
		for endIdx < len(lines) && hasGtPrefix(lines[endIdx]) {
			endIdx++
		}
		return endIdx - 1
	}
	return -1
}

// resolveTrailingID peeks at the line after the region closer for a dedicated
// block-identity comment. Returns the id, file date, the index of the id line
// (or -1 if none), and whether it was consumed.
func resolveTrailingID(lines []string, afterIdx int, meta *FileMetadata) (id, fileDate string, idLineIdx int, consumed bool) {
	idLineIdx = -1
	if afterIdx >= len(lines) {
		return "", "", -1, false
	}
	cand := strings.TrimSpace(lines[afterIdx])
	if !strings.HasPrefix(cand, "<!-- id:") {
		return "", "", -1, false
	}
	m := IDRegex.FindStringSubmatch(lines[afterIdx])
	if len(m) < 2 {
		return "", "", -1, false
	}
	id = m[1]
	if len(m) > 2 {
		fileDate = m[2]
	}
	if fileDate == "" {
		fileDate = meta.Date
	}
	return id, fileDate, afterIdx, true
}

// accumulateRegion reads a multi-line managed region starting at lines[openIdx]
// and returns:
//   - consumedTo: the index of the last consumed input line (the region closer,
//     or the trailing id-comment line if one is present).
//   - block: the assembled ParsedBlock (nil if the region is unterminated).
//   - emitLines: the lines to append to outputLines (region content [+ id]).
//   - minted: true if a fresh block id was assigned.
//   - oldIDs: map of inline ids found on region lines (old format) → the typed
//     block's id. Used by Migration B to remap ((uuid)) references.
//
// Handles four region shapes: fenced code (BlockCode), GFM table (BlockTable),
// <details> HTML (BlockDetails), and Obsidian callout (BlockCallout). The block
// id lives on its OWN dedicated trailing line after the region content so the
// on-disk format stays strictly GFM/HTML and round-trips through Obsidian /
// GitHub / VS Code unchanged.
//
// Backward compatibility: old on-disk files may have inline id comments on each
// line within a region (the pre-unified format). These are stripped from
// clean_text, and their ids are collected for ((uuid)) reference remapping.
func accumulateRegion(lines []string, openIdx, lineNumber int, meta *FileMetadata) (consumedTo int, block *ParsedBlock, emitLines []string, minted bool, oldIDs map[string]string) {
	kind := detectRegionKind(lines, openIdx)
	if kind == regionNone {
		return openIdx, nil, []string{lines[openIdx]}, false, nil
	}

	closer := findRegionCloser(lines, openIdx, kind)
	if closer == -1 {
		// Unterminated region: emit the opener verbatim, produce no block.
		return openIdx, nil, []string{lines[openIdx]}, false, nil
	}

	// Build clean_text. For code blocks, the content between fences is literal
	// — never strip id comments from code (they're part of the code). For
	// table/details/callout, old-format files may have inline id comments on
	// each line; strip them and collect old ids for Migration B.
	var blockType BlockType
	var inner string
	openerTrim := stripInlineID(lines[openIdx])
	oldIDs = make(map[string]string)

	switch kind {
	case regionCode:
		blockType = BlockCode
		inner = strings.Join(lines[openIdx+1:closer], "\n")
		// Code blocks never have inline ids in the old format (the parser
		// skips id injection inside fences), so no oldIDs to collect.
	case regionTable, regionDetails, regionCallout:
		switch kind {
		case regionTable:
			blockType = BlockTable
		case regionDetails:
			blockType = BlockDetails
		case regionCallout:
			blockType = BlockCallout
		}
		var cleanLines []string
		var primaryInlineID string
		for j := openIdx; j <= closer; j++ {
			cleaned := stripInlineID(lines[j])
			inlineID := extractInlineID(lines[j])
			if inlineID != "" {
				oldIDs[inlineID] = "" // placeholder
				if kind == regionTable {
					primaryInlineID = inlineID // last wins
				} else if j == openIdx {
					primaryInlineID = inlineID
				}
			}
			cleanLines = append(cleanLines, cleaned)
		}
		inner = strings.Join(cleanLines, "\n")
		// If no trailing id line exists (checked below), fall back to the
		// primary inline id from the old format.
		if primaryInlineID != "" {
			oldIDs["__primary__"] = primaryInlineID
		}
	}

	// Resolve the block id: first check for a dedicated trailing id line
	// (new format), then fall back to the primary inline id (old format),
	// then mint a new one.
	blockID, blockFileDate, idLineIdx, consumedIDLine := resolveTrailingID(lines, closer+1, meta)
	if blockID == "" {
		if p, ok := oldIDs["__primary__"]; ok {
			blockID = p
		}
	}
	if blockID == "" {
		blockID = generateUUIDv4()
		minted = true
	}
	if blockFileDate == "" {
		blockFileDate = meta.Date
	}

	// Fill the oldID map: each old inline id → the typed block's id.
	// Remove the __primary__ marker — it's not a real id to remap.
	delete(oldIDs, "__primary__")
	for k := range oldIDs {
		oldIDs[k] = blockID
	}

	pb := ParsedBlock{
		ID:         blockID,
		Type:       blockType,
		CleanText:  inner,
		LineNumber: lineNumber,
		FileDate:   blockFileDate,
	}
	if kind == regionCode {
		pb.Language = strings.TrimSpace(openerTrim[codeFenceLen(openerTrim):])
	}

	// Emit lines: for code blocks, emit the raw fence + content verbatim.
	// For table/details/callout, emit cleaned lines (inline ids stripped) +
	// trailing id line.
	if kind == regionCode {
		emitLines = append(emitLines, lines[openIdx:closer+1]...)
	} else {
		// Rebuild cleanLines for emission (same logic as clean_text above).
		for j := openIdx; j <= closer; j++ {
			emitLines = append(emitLines, stripInlineID(lines[j]))
		}
	}
	if consumedIDLine {
		emitLines = append(emitLines, lines[idLineIdx])
		return idLineIdx, &pb, emitLines, minted, oldIDs
	}
	emitLines = append(emitLines, fmt.Sprintf("<!-- id: %s @ %s -->", blockID, blockFileDate))
	return closer, &pb, emitLines, minted, oldIDs
}

// skipManagedRegion checks whether bodyLines[idx] starts a managed multi-line
// region (code fence, GFM table, <details> HTML, or Obsidian callout) and, if
// so, returns the index of the line AFTER the region (including any trailing
// id-comment line). Returns -1 if the line does not start a region, or if the
// region is unterminated (caller should preserve verbatim).
func skipManagedRegion(bodyLines []string, idx int) int {
	kind := detectRegionKind(bodyLines, idx)
	if kind == regionNone {
		return -1
	}
	closer := findRegionCloser(bodyLines, idx, kind)
	if closer == -1 {
		return -1
	}
	consumed := closer + 1
	if consumed < len(bodyLines) {
		cand := strings.TrimSpace(bodyLines[consumed])
		if strings.HasPrefix(cand, "<!-- id:") {
			if m := IDRegex.FindStringSubmatch(bodyLines[consumed]); len(m) > 1 {
				consumed++
			}
		}
	}
	return consumed
}

func ParseFileContent(content string, defaultNotebook, defaultSection, defaultPage, defaultDate string, spacesPerTab int) ([]ParsedBlock, FileMetadata, string, bool, error) {
	if spacesPerTab <= 0 {
		spacesPerTab = 4
	}

	// Externally-edited files (Obsidian / OneDrive / Dropbox sync) may carry a
	// leading UTF-8 BOM; strings.TrimSpace does not strip U+FEFF, so peel it
	// once here or the opening --- would not be recognized. Capture its
	// presence so the block-ID rewrite below can re-prepend it — otherwise a
	// BOM file that needs minting loses its BOM on write (sync diff).
	hadBOM := strings.HasPrefix(content, "\uFEFF")
	content = strings.TrimPrefix(content, "\uFEFF")
	lines := strings.Split(content, "\n")
	var meta FileMetadata
	meta.Notebook = defaultNotebook
	meta.Section = defaultSection
	meta.Page = defaultPage
	meta.Date = defaultDate

	hasFrontmatter := false
	frontmatterEndIdx := -1

	// Check for frontmatter
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		var fmLines []string
		for i := 1; i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed == "---" {
				hasFrontmatter = true
				frontmatterEndIdx = i
				break
			}
			fmLines = append(fmLines, lines[i])
		}

		if hasFrontmatter {
			fmStr := strings.Join(fmLines, "\n")
			// Parse the frontmatter ONCE into a node tree and Decode it into
			// both the typed struct (known fields) and a raw map (all keys,
			// for the type projection). A second yaml.Unmarshal would re-parse
			// the same bytes — doubling parse cost — and its failure was
			// swallowed, silently producing an empty Frontmatter.
			var node yaml.Node
			if err := yaml.Unmarshal([]byte(fmStr), &node); err != nil {
				// Surface the parse failure so the caller can warn the
				// user. Falling through with path-derived defaults would
				// silently lose the user's authored metadata.
				meta.Warnings = append(meta.Warnings, "yaml frontmatter parse error: "+err.Error())
			} else {
				var parsedMeta FileMetadata
				if err := node.Decode(&parsedMeta); err != nil {
					// Typed decode can fail on a single bad field (e.g. page: [1,2])
					// while the rest of the map is fine. Still pull the raw map so
					// type:/property projection is not silently emptied (AC5).
					meta.Warnings = append(meta.Warnings, "yaml frontmatter parse error: "+err.Error())
				} else {
					if parsedMeta.Notebook != "" {
						meta.Notebook = parsedMeta.Notebook
					}
					if parsedMeta.Section != "" {
						meta.Section = parsedMeta.Section
					}
					if parsedMeta.Page != "" {
						meta.Page = parsedMeta.Page
					}
					if parsedMeta.Date != "" {
						meta.Date = NormalizeDate(parsedMeta.Date)
					}
					if len(parsedMeta.Tags) > 0 {
						meta.Tags = parsedMeta.Tags
					}
					// type: is the page's note-type id (typed-notes feature). The parser
					// stores the raw frontmatter value; canonical-id resolution happens
					// at the indexing/UI layer, which has access to the type schema.
					if parsedMeta.Type != "" {
						meta.Type = parsedMeta.Type
					}
					// Core page-level metadata (#867): aliases (string array) and
					// created (timestamp) are type-independent fields every page can
					// expose. The parser preserves them as it does date/tags/type so
					// an edit round-trips byte-for-byte through the frontmatter.
					if len(parsedMeta.Aliases) > 0 {
						meta.Aliases = parsedMeta.Aliases
					}
					if parsedMeta.Created != "" {
						meta.Created = parsedMeta.Created
					}
				}
				// Always decode the raw map — even when the typed struct failed —
				// so schema-declared properties and type: remain projectable.
				var rawFM map[string]any
				if err := node.Decode(&rawFM); err != nil {
					meta.Warnings = append(meta.Warnings, "yaml frontmatter decode error: "+err.Error())
				} else {
					meta.Frontmatter = rawFM
					// Typed decode can fail on a single bad field (e.g. a scalar
					// `aliases: foo` failing the []string decode) while the rest of
					// the map is fine. Recover every known page-level field from the
					// raw map so a bad aliases/type value doesn't silently drop the
					// file's date, tags, created, or type (which would corrupt
					// blocks.file_date, the tag index, and page_core on reparse).
					if meta.Type == "" {
						if t, ok := rawFM["type"].(string); ok {
							meta.Type = t
						}
					}
					// Recover the frontmatter date unconditionally. yaml.v3 FAILS
					// the typed FileMetadata decode on a scalar `aliases: foo`
					// (cannot unmarshal !!str into []string), so when frontmatter
					// carries such a field the typed-success block above never
					// runs and `meta.Date` keeps the defaultDate pre-fill from
					// the top of the function. A `if meta.Date == ""` guard here
					// would never fire (defaultDate is always non-empty in the
					// production callers) and the frontmatter date would be
					// silently dropped. On typed success this just re-normalizes
					// the same value, so running it unconditionally is safe.
					// An UNQUOTED `date: 2026-08-05` resolves to a time.Time in
					// the raw map; a quoted one is a string — handle both.
					switch d := rawFM["date"].(type) {
					case string:
						if d != "" {
							meta.Date = NormalizeDate(d)
						}
					case time.Time:
						meta.Date = NormalizeDate(d.Format("2006-01-02"))
					}
					if meta.Created == "" {
						// created is a full timestamp, so format as RFC3339 when
						// yaml handed us a time.Time (unquoted scalar form).
						switch c := rawFM["created"].(type) {
						case string:
							if c != "" {
								meta.Created = c
							}
						case time.Time:
							meta.Created = c.Format(time.RFC3339)
						}
					}
					if len(meta.Tags) == 0 {
						if t := recoverStringList(rawFM["tags"]); len(t) > 0 {
							meta.Tags = t
						}
					}
					if len(meta.Aliases) == 0 {
						if a := recoverStringList(rawFM["aliases"]); len(a) > 0 {
							meta.Aliases = a
						}
					}
				}
			}
		}
	}

	var blocks []ParsedBlock
	var outputLines []string
	modifiedAny := false
	// subsumedIDs collects old inline ids from pre-unified-format files.
	// After parsing, ((uuid)) references to these ids are remapped to the
	// typed block's id (Migration B).
	subsumedIDs := make(map[string]string)

	startIndex := 0
	if hasFrontmatter {
		startIndex = frontmatterEndIdx + 1
		// Add frontmatter lines back to output unmodified
		for i := 0; i <= frontmatterEndIdx; i++ {
			outputLines = append(outputLines, lines[i])
		}
	}

	// activeIDs tracks the most recent block ID at each indent level so we
	// can wire parent_id for nested blocks. We grow it dynamically instead
	// of fixing the size at 100, which previously caused silent parent_id
	// loss for any block past depth 99.
	activeIDs := []string{}

	// taskCounter is the running 1-based count of TASK blocks seen so far
	// in the file. It backs ManualOrder for newly-minted tasks (#417): a
	// genuinely-new task (one whose line lacked a block-identity comment,
	// flagged by `modified`) is stamped with its 1-based position among all
	// TASK blocks so the UI has a stable sort key from the first save.
	taskCounter := 0

	for i := startIndex; i < len(lines); i++ {
		line := lines[i]
		lineNumber := i + 1

		// If it's the last line and empty, avoid creating a block but keep the line
		if i == len(lines)-1 && strings.TrimSpace(line) == "" {
			outputLines = append(outputLines, line)
			continue
		}

		// Multi-line managed regions (#189 code, #310 table/details, #308
		// callout): each region type becomes ONE managed ParsedBlock (the
		// unified region-block model). Content is preserved byte-for-byte;
		// the block identity comment lives on its OWN line after the region
		// so the on-disk format stays strictly GFM/HTML/Obsidian callout
		// syntax (interoperable with Obsidian / GitHub / VS Code).
		if detectRegionKind(lines, i) != regionNone {
			consumedTo, regionBlock, emitLines, minted, oldIDs := accumulateRegion(
				lines, i, lineNumber, &meta,
			)
			if regionBlock != nil {
				if regionBlock.FileDate == "" {
					regionBlock.FileDate = meta.Date
				}
				blocks = append(blocks, *regionBlock)
				// Collect old inline ids for Migration B remapping. Any
				// old-format inline ids mean the file was modified (migrated).
				if len(oldIDs) > 0 {
					modifiedAny = true
					for oldID, newID := range oldIDs {
						if oldID != newID {
							subsumedIDs[oldID] = newID
						}
					}
				}
			}
			if minted {
				modifiedAny = true
				meta.MintedCount++ // one region block got a fresh id this parse
			}
			outputLines = append(outputLines, emitLines...)
			i = consumedTo
			continue
		}

		block, newLine, modified := ParseLine(line, lineNumber, spacesPerTab)
		if modified {
			modifiedAny = true
			// ParseLine mints a fresh id exactly when the line lacked a
			// block-identity comment; count it for the #443 re-mint
			// heuristic. A block with an empty ID (frontmatter prose) is
			// never minted, so guard on ID presence.
			if block.ID != "" {
				meta.MintedCount++
			}
		}

		if block.ID != "" {
			// Backward-compat: blocks whose comment predates the per-block
			// file_date format (<!-- id: uuid --> with no @ date) inherit the
			// file-level default date (from frontmatter or path-derived).
			if block.FileDate == "" {
				block.FileDate = meta.Date
			}

			// Task lifecycle minting (#417): a TASK block whose line was just
			// minted with a fresh id (`modified`) is treated as new. The minting
			// path also fires when an external editor/sync stripped the
			// `<!-- id: ... -->` comment from an existing task while leaving the
			// `[created::]`/`[order::]` tokens intact in the line — in that case
			// scanTaskTokens has ALREADY populated the surviving values into the
			// block, and overwriting them with time.Now()/taskCounter would
			// silently destroy the original creation timestamp (data-loss bug).
			// Guard each stamp so an already-present value wins; a truly-new task
			// has neither token (both empty/0) and gets stamped.
			//
			// The Go SDK create paths mint the id themselves and set these fields
			// directly, so on re-parse the id is already present and this branch
			// is correctly skipped.
			if block.Type == BlockTask {
				taskCounter++
				if modified {
					if block.CreatedAt == "" {
						block.CreatedAt = time.Now().Format("2006-01-02T15:04:05")
					}
					if block.ManualOrder == 0 {
						block.ManualOrder = taskCounter
					}
					// A freshly-minted task whose checkbox is already DONE (e.g.
					// the user typed `- [x] ship it`) must also carry
					// [completed::] — PluginUpdateBlockState only stamps it on a
					// TODO→DONE *transition*, not on initial DONE detection, so
					// without this the 'completed' filter would miss the task.
					// The empty-guard mirrors the created/order guards: a
					// surviving token wins.
					if block.Status == "DONE" && block.CompletedAt == "" {
						block.CompletedAt = time.Now().Format("2006-01-02T15:04:05")
					}
				}
			}

			// Resolve Parent ID
			depth := block.Depth
			if depth > 0 && depth-1 < len(activeIDs) {
				block.ParentID = activeIDs[depth-1]
			}

			// Grow the stack so depth is always a valid index.
			if depth >= 0 {
				for len(activeIDs) <= depth {
					activeIDs = append(activeIDs, "")
				}
				activeIDs[depth] = block.ID
				// Clear deeper active IDs
				for d := depth + 1; d < len(activeIDs); d++ {
					activeIDs[d] = ""
				}
			}

			blocks = append(blocks, block)
		}

		// Emit the output line. For a newly-minted TASK block (#417) the
		// in-memory CreatedAt/ManualOrder must reach the file — the scanner
		// writes `newContent` back to disk when modified=true, and using the
		// raw `newLine` (id appended only) would lose the lifecycle tokens
		// (data loss on next re-scan, since the task now has an id and won't
		// be re-minted). Re-rendering through renderBlock lands the tokens
		// in the canonical form. Every other line uses `newLine` verbatim
		// so non-minted content is preserved byte-for-byte.
		if block.ID != "" && block.Type == BlockTask && modified {
			outputLines = append(outputLines, renderBlock(block, spacesPerTab))
		} else {
			outputLines = append(outputLines, newLine)
		}
	}

	// Migration B: remap ((uuid)) references to old per-line ids that were
	// subsumed into typed blocks. Old-format files had inline id comments on
	// each line of a table/details/callout; those ids are gone in the unified
	// model. Any ((uuid)) reference pointing to a vanished id is remapped to
	// the typed block's id so references still resolve.
	if len(subsumedIDs) > 0 {
		for i := range blocks {
			matches := BlockRefRegex.FindAllStringSubmatch(blocks[i].CleanText, -1)
			for _, m := range matches {
				oldID := m[1]
				if newID, ok := subsumedIDs[oldID]; ok {
					blocks[i].CleanText = strings.ReplaceAll(
						blocks[i].CleanText,
						"(("+oldID+"))",
						"(("+newID+"))",
					)
				}
			}
		}
	}

	newContent := strings.Join(outputLines, "\n")
	// outputLines reconstructs the full file (frontmatter opening --- … body),
	// so re-prepending the BOM lands it before the opening fence — matching the
	// input byte-for-byte when the rewrite modified anything.
	if hadBOM {
		newContent = "\uFEFF" + newContent
	}
	return blocks, meta, newContent, modifiedAny, nil
}

// RenderFileContent is the canonical serializer for a Silt note file — the
// single source of truth for turning ParsedBlocks (plus frontmatter and any
// unmanaged prose) back into file content. Every writer (SaveFileBlocks,
// MutateBlock, CreatePage) goes through this function so the on-disk block
// format has exactly one definition and cannot drift between serializers.
//
//   - frontmatter is emitted verbatim. Pass the full frontmatter block
//     including its trailing newline (e.g. "---\n...\n---\n"), or "" for none.
//   - blocks is the authoritative ordered list of managed blocks to write.
//     Blocks without an ID are assigned a fresh UUIDv4 before rendering, so a
//     brand-new editor block reaches disk with a stable identity.
//   - originalBody is the file body with frontmatter already stripped, used
//     to preserve unmanaged lines (fenced code blocks, blank lines, prose
//     that never carried a managed block ID) in their relative position to
//     the managed blocks. Pass "" when there is nothing to preserve (e.g. a
//     brand-new page). Unmanaged lines attach to the managed block that
//     follows them; trailing unmanaged lines are appended after the last
//     block. Managed lines from originalBody whose IDs are no longer in
//     `blocks` are dropped (the block was deleted); lines that merely look
//     like a UUID comment but never parsed as a managed block are preserved.
//
// The per-block line format is produced by the package-internal renderBlock,
// which lives next to ParseLine so a format change has exactly one place to
// update. The round-trip identity tests in parser_test.go guarantee that
// ParseFileContent(RenderFileContent(ParseFileContent(src))) is stable.
func RenderFileContent(blocks []ParsedBlock, originalBody, frontmatter string, spacesPerTab int) string {
	if spacesPerTab <= 0 {
		spacesPerTab = 4
	}

	// Ensure every block reaches disk with a stable ID.
	for i := range blocks {
		if blocks[i].ID == "" {
			blocks[i].ID = generateUUIDv4()
		}
	}

	orderedByID := make(map[string]ParsedBlock, len(blocks))
	for _, b := range blocks {
		orderedByID[b.ID] = b
	}

	// Determine which IDs were managed in the original body so we can tell
	// "this UUID line was a managed block the user deleted" (drop it) from
	// "this UUID-shaped HTML comment is just prose the user typed" (keep it).
	// Without this distinction, quoting a commit hash in a note would silently
	// delete the line on the next save.
	oldManagedIDs := map[string]bool{}
	if originalBody != "" {
		oldBlocks, _, _, _, parseErr := ParseFileContent(originalBody, "", "", "", "", spacesPerTab)
		if parseErr == nil {
			for _, b := range oldBlocks {
				oldManagedIDs[b.ID] = true
			}
		}
	}

	// Walk the original body, bucketing unmanaged lines (code fences, blanks,
	// prose) by the managed block ID that follows them. This mirrors the
	// algorithm SaveFileBlocks used to inline, now centralized here so every
	// writer benefits from preserved user content.
	preservedBefore := make(map[string][]string)
	var pendingPreserved []string
	if originalBody != "" {
		bodyLines0 := strings.Split(originalBody, "\n")
		// Managed multi-line regions (code fence, GFM table, <details> HTML,
		// Obsidian callout) are re-emitted from `blocks` via renderBlock, so a
		// region in the original body must NOT be preserved — otherwise it
		// would double-emit. skipManagedRegion handles all region types
		// uniformly, returning the line index after the region (+ optional
		// trailing id-comment line).
		// Unterminated regions fall back to verbatim preservation (-1).
		for idx := 0; idx < len(bodyLines0); idx++ {
			line := bodyLines0[idx]
			trimmed := strings.TrimSpace(line)
			if consumed := skipManagedRegion(bodyLines0, idx); consumed > 0 {
				idx = consumed - 1
				continue
			}
			if trimmed == "" {
				pendingPreserved = append(pendingPreserved, line)
				continue
			}
			matches := IDRegex.FindStringSubmatch(line)
			if len(matches) > 1 {
				blockID := matches[1]
				if _, ok := orderedByID[blockID]; ok {
					if _, assigned := preservedBefore[blockID]; !assigned {
						preservedBefore[blockID] = append(preservedBefore[blockID], pendingPreserved...)
						pendingPreserved = nil
						continue
					}
				}
				if oldManagedIDs[blockID] {
					// Deleted managed block: drop it. Its pending unmanaged
					// lines stay pending for the next surviving block.
					continue
				}
			}
			pendingPreserved = append(pendingPreserved, line)
		}
	}

	// Emit frontmatter (verbatim) + woven body (preserved + rendered blocks).
	var bodyLines []string
	for _, b := range blocks {
		if pre, ok := preservedBefore[b.ID]; ok {
			bodyLines = append(bodyLines, pre...)
		}
		bodyLines = append(bodyLines, renderBlock(b, spacesPerTab))
	}
	bodyLines = append(bodyLines, pendingPreserved...)

	return frontmatter + strings.Join(bodyLines, "\n")
}

// renderBlock converts a single ParsedBlock back into its canonical markdown
// line. It is the sole block→line code path in the codebase (the only thing
// that produces on-disk block syntax), kept next to ParseLine so any format
// tweak has one place to update.
//
// Newly created editor blocks arrive with an empty RawText; blocks that also
// have empty content are emitted without a bullet so blank pages round-trip as
// blank (not as "- "). Blocks with fresh content but no RawText still get the
// "- " outliner default. Existing notes preserve their original bullet marker
// ("- ", "* ", "+ ") or plain-text style.
func renderBlock(block ParsedBlock, spacesPerTab int) string {
	if spacesPerTab <= 0 {
		spacesPerTab = 4
	}
	indent := strings.Repeat(" ", block.Depth*spacesPerTab)

	// Build ID suffix — includes per-block file_date if present:
	//   <!-- id: uuid @ YYYY-MM-DD -->
	idSuffix := ""
	if block.ID != "" {
		if block.FileDate != "" {
			idSuffix = fmt.Sprintf(" <!-- id: %s @ %s -->", block.ID, block.FileDate)
		} else {
			idSuffix = fmt.Sprintf(" <!-- id: %s -->", block.ID)
		}
	}

	// BlockCode is multi-line: the code body keeps its internal newlines (it
	// is NOT run through the `\n`→space collapse the prose blocks use). The
	// identity comment goes on its own line after the closing fence so the
	// fence stays strictly GFM (no trailing content) and the block is
	// interoperable with Obsidian / GitHub / VS Code (#189).
	if block.Type == BlockCode {
		// idSuffix for a code block is a leading "\n" + the comment (the
		// comment lives on its own line, not appended to the fence).
		idLine := ""
		if idSuffix != "" {
			idLine = "\n" + strings.TrimSpace(idSuffix)
		}
		// Fence length grows to 4 backticks if the body itself contains a
		// ``` line, so a code sample that includes a triple-backtick fence
		// round-trips without prematurely closing. Rare; correct.
		fence := "```"
		if strings.Contains("\n"+block.CleanText+"\n", "\n"+fence) {
			fence = "````"
			for strings.Contains("\n"+block.CleanText+"\n", "\n"+fence) {
				fence += "`"
			}
		}
		return fmt.Sprintf("%s%s\n%s\n%s%s", fence, block.Language, block.CleanText, fence, idLine)
	}

	// BlockTable and BlockDetails are multi-line: the clean_text IS the
	// on-disk content (GFM pipe rows / <details> HTML), emitted verbatim.
	// The identity comment goes on its own trailing line so the content stays
	// strictly GFM/HTML (#310 — unified region-block model).
	if block.Type == BlockTable || block.Type == BlockDetails || block.Type == BlockCallout {
		idLine := ""
		if idSuffix != "" {
			idLine = "\n" + strings.TrimSpace(idSuffix)
		}
		return block.CleanText + idLine
	}

	if block.Type == BlockTask {
		checkbox := " "
		if block.Status == "DOING" {
			checkbox = "/"
		} else if block.Status == "DONE" {
			checkbox = "x"
		}

		// Build [key:: value] metadata tokens (Dataview inline metadata
		// format — see ARCHITECTURE.md §0 "Storage-of-Truth Tiers").
		// Each metadata field that is set gets its own [key:: value] token
		// appended after the description. The order is fixed: priority,
		// start, due, recur, owner, pin, progress, blocked_by, created,
		// completed, order — matching the canonical field order so a
		// parse → render round trip is byte-stable.
		var tokens []string
		if block.Priority > 0 && block.Priority != 3 {
			tokens = append(tokens, fmt.Sprintf("[priority:: %d]", block.Priority))
		}
		if block.StartDate != "" {
			tokens = append(tokens, fmt.Sprintf("[start:: %s]", block.StartDate))
		}
		if block.DueDate != "" {
			tokens = append(tokens, fmt.Sprintf("[due:: %s]", block.DueDate))
		}
		if block.Recurrence != "" {
			tokens = append(tokens, fmt.Sprintf("[recur:: %s]", block.Recurrence))
		}
		if block.Owner != "" {
			tokens = append(tokens, fmt.Sprintf("[owner:: %s]", block.Owner))
		}
		if block.Pinned != nil {
			if *block.Pinned {
				tokens = append(tokens, "[pin:: true]")
			} else {
				tokens = append(tokens, "[pin:: false]")
			}
		}
		if block.Progress > 0 {
			tokens = append(tokens, fmt.Sprintf("[progress:: %d]", block.Progress))
		}
		if refStr := dependencies.FormatRefs(block.BlockedBy); refStr != "" {
			tokens = append(tokens, fmt.Sprintf("[blocked_by:: %s]", refStr))
		}
		// Lifecycle metadata (#417): created/completed timestamps and the
		// manual sort order. Emitted after blocked_by and before unknown
		// ExtraTokens so the position is deterministic (scanTaskTokens is
		// order-independent, but a fixed render order is what makes the
		// parse → render round-trip byte-identical). Each is omitted when
		// at its zero value (empty string / 0) so existing tasks without
		// these tokens stay exactly as they were (no backfill on render).
		if block.CreatedAt != "" {
			tokens = append(tokens, fmt.Sprintf("[created:: %s]", block.CreatedAt))
		}
		if block.CompletedAt != "" {
			tokens = append(tokens, fmt.Sprintf("[completed:: %s]", block.CompletedAt))
		}
		if block.ModifiedAt != "" {
			tokens = append(tokens, fmt.Sprintf("[modified:: %s]", block.ModifiedAt))
		}
		if block.ManualOrder > 0 {
			tokens = append(tokens, fmt.Sprintf("[order:: %d]", block.ManualOrder))
		}
		// Estimate (#439): raw duration string after order, before ExtraTokens.
		if block.Estimate != "" {
			tokens = append(tokens, fmt.Sprintf("[estimate:: %s]", block.Estimate))
		}
		// Append unknown Dataview tokens verbatim so they survive the
		// round-trip (Dataview-compatible interop — SPECS.md §4.1).
		tokens = append(tokens, block.ExtraTokens...)

		tokenStr := ""
		if len(tokens) > 0 {
			tokenStr = " " + strings.Join(tokens, " ")
		}

		// - [checkbox] description [key:: value]... <!-- id: id -->
		return fmt.Sprintf("%s- [%s] %s%s%s",
			indent, checkbox,
			strings.ReplaceAll(block.CleanText, "\n", " "),
			tokenStr, idSuffix)
	} else if block.Type == BlockHeader {
		hashes := strings.Repeat("#", block.Depth)
		if hashes == "" {
			hashes = "#"
		}
		return fmt.Sprintf("%s %s%s", hashes, block.CleanText, idSuffix)
	} else {
		// BlockNote. Newly created blocks arrive with an empty RawText.
		// Default to "- " only when there is fresh content to show — an
		// empty RawText + empty CleanText is a blank placeholder that must
		// round-trip without a bullet (otherwise blank pages regain "- "
		// on the first autosave).
		prefix := ""
		trimmedRaw := strings.TrimSpace(block.RawText)
		trimmedClean := strings.TrimSpace(block.CleanText)
		if trimmedRaw == "" && trimmedClean == "" {
			// Empty-empty: no bullet, no content — bare id line only.
		} else if trimmedRaw == "" {
			// Fresh content with no raw text: outliner default "- ".
			prefix = "- "
		} else if trimmedRaw != "" {
			// Detect bullet prefix on RawText WITHOUT trailing TrimSpace.
			// trimmedRaw collapses "- " to "-", making HasPrefix fail (#570).
			bulletRaw := strings.TrimLeft(block.RawText, " \t")
			if strings.HasPrefix(bulletRaw, "- ") {
				prefix = "- "
			} else if strings.HasPrefix(bulletRaw, "* ") {
				prefix = "* "
			} else if strings.HasPrefix(bulletRaw, "+ ") {
				prefix = "+ "
			} else if m := NumberedListRegex.FindString(bulletRaw); m != "" {
				prefix = m
			} else {
				prefix = ""
			}
		}
		// NOTE-block comment attribution (#418): emit [author::]/[ts::]
		// when populated, omit-when-empty (mirrors the task-token pattern).
		// Fixed order — author then ts — so the parse → render round-trip
		// is byte-stable regardless of the order the parser saw. NOTE-only
		// by construction: TASK blocks use the task render path above.
		// Both values are sanitized before rendering: a ']' would truncate the
		// token and a newline would split the NOTE block across lines (breaking
		// the parse on next read). The SDK always passes a well-formed
		// YYYY-MM-DDTHH:MM:SS ts, but a buggy/hostile plugin calling the
		// append-comment binding directly can't corrupt the token stream.
		var noteTokens []string
		if block.Author != "" {
			author := strings.ReplaceAll(block.Author, "]", "")
			author = strings.ReplaceAll(author, "\n", " ")
			author = strings.ReplaceAll(author, "\r", "")
			noteTokens = append(noteTokens, fmt.Sprintf("[author:: %s]", author))
		}
		if block.Timestamp != "" {
			ts := strings.ReplaceAll(block.Timestamp, "]", "")
			ts = strings.ReplaceAll(ts, "\n", " ")
			ts = strings.ReplaceAll(ts, "\r", "")
			noteTokens = append(noteTokens, fmt.Sprintf("[ts:: %s]", ts))
		}
		noteTokenStr := ""
		if len(noteTokens) > 0 {
			noteTokenStr = " " + strings.Join(noteTokens, " ")
		}
		cleanOut := strings.ReplaceAll(block.CleanText, "\n", " ")
		// When CleanText is empty, the bullet prefix's trailing space
		// collides with the leading space in noteTokenStr/idSuffix,
		// producing "-  <!-- id -->" (#570 round-trip fidelity gap).
		// Drop it so the separator comes from the following segment.
		if cleanOut == "" && (noteTokenStr != "" || idSuffix != "") {
			prefix = strings.TrimRight(prefix, " ")
		}
		return fmt.Sprintf("%s%s%s%s%s", indent, prefix, cleanOut, noteTokenStr, idSuffix)
	}
}

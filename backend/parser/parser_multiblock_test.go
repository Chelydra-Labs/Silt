package parser

import (
	"strings"
	"testing"
)

// Sprint 14 (#189) — multi-line managed code blocks. The single-line block
// model (`renderBlock` collapses `\n`→space for prose) must NOT touch code:
// blank lines, leading indentation, tabs, a literal `|`, and the info-string
// language all survive a parse → render → parse round trip byte-for-byte.
// These cases complement the broader TestRenderFileContent_RoundTripIdentity
// suite by exercising the content shapes that previously distinguished
// "unmanaged verbatim pass-through" from "managed block".

func TestCodeBlock_MultilineRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		lang string
		body string
	}{
		{
			name: "blank lines preserved",
			lang: "text",
			body: "first\n\nthird",
		},
		{
			name: "leading indentation preserved",
			lang: "go",
			body: "func main() {\n\tinner := func() {\n\t\tx := 1\n\t}\n\t_ = inner\n}",
		},
		{
			name: "literal pipe survives",
			lang: "text",
			body: "a | b\n| c",
		},
		{
			name: "empty body",
			lang: "",
			body: "",
		},
		{
			name: "single line",
			lang: "ts",
			body: "const x: number = 1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block := ParsedBlock{
				ID:        "dddddddd-1111-1111-1111-111111111111",
				Type:      BlockCode,
				Language:  tc.lang,
				CleanText: tc.body,
				FileDate:  "2026-06-14",
			}
			rendered := RenderFileContent([]ParsedBlock{block}, "", "", 4)
			reparsed, _, _, _, err := ParseFileContent(rendered, "NB", "", "PG", "2026-06-14", 4)
			if err != nil {
				t.Fatalf("reparse: %v\nrendered:\n%s", err, rendered)
			}
			if len(reparsed) != 1 || reparsed[0].Type != BlockCode {
				t.Fatalf("expected one BlockCode, got %+v", reparsed)
			}
			got := reparsed[0]
			if got.CleanText != tc.body {
				t.Errorf("CleanText drifted\nwant: %q\n got: %q", tc.body, got.CleanText)
			}
			if got.Language != tc.lang {
				t.Errorf("Language drifted: want %q got %q", tc.lang, got.Language)
			}
			// Byte-stable across a second render pass.
			rendered2 := RenderFileContent(reparsed, "", "", 4)
			if rendered != rendered2 {
				t.Errorf("not byte-stable\n--- pass1 ---\n%s\n--- pass2 ---\n%s", rendered, rendered2)
			}
		})
	}
}

func TestCodeBlock_ExternalFileGetsIdOnFirstParse(t *testing.T) {
	// A code block authored externally (Obsidian/VS Code) carries no id.
	// First parse mints one on its own line after the closing fence; the
	// code body is untouched. Second parse is stable (no further change).
	src := "# H <!-- id: aaaaaaaa-1111-1111-1111-111111111111 -->\n" +
		"```js\nconst x = 1\n```\n" +
		"- note <!-- id: aaaaaaaa-2222-2222-2222-111111111111 -->\n"

	blocks, _, _, modified, err := ParseFileContent(src, "NB", "", "PG", "2026-06-14", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !modified {
		t.Errorf("expected the external code block to be assigned an id on first parse")
	}
	var code *ParsedBlock
	for i := range blocks {
		if blocks[i].Type == BlockCode {
			code = &blocks[i]
		}
	}
	if code == nil {
		t.Fatalf("expected a BlockCode, got %+v", blocks)
	}
	if code.CleanText != "const x = 1" {
		t.Errorf("code body corrupted: %q", code.CleanText)
	}
	if code.Language != "js" {
		t.Errorf("language lost: %q", code.Language)
	}
}

func TestCodeBlock_NestedBacktickFence(t *testing.T) {
	// A code sample that itself contains a ``` line must round-trip. The
	// renderer grows the outer fence to four backticks so the inner fence
	// does not prematurely close the block.
	inner := "outer line\n```\nstill code\n```"
	block := ParsedBlock{
		ID:        "eeeeeeee-1111-1111-1111-111111111111",
		Type:      BlockCode,
		Language:  "markdown",
		CleanText: inner,
		FileDate:  "2026-06-14",
	}
	rendered := RenderFileContent([]ParsedBlock{block}, "", "", 4)
	if !strings.Contains(rendered, "````") {
		t.Errorf("expected a 4-backtick outer fence for nested content, got:\n%s", rendered)
	}
	reparsed, _, _, _, err := ParseFileContent(rendered, "NB", "", "PG", "2026-06-14", 4)
	if err != nil {
		t.Fatalf("reparse: %v\nrendered:\n%s", err, rendered)
	}
	if len(reparsed) != 1 || reparsed[0].Type != BlockCode {
		t.Fatalf("expected one BlockCode, got %+v", reparsed)
	}
	if reparsed[0].CleanText != inner {
		t.Errorf("nested-fence body drifted\nwant: %q\n got: %q", inner, reparsed[0].CleanText)
	}
}

// A closing fence must be backticks-only (GFM): a ```js line inside the block
// is an inner fence WITH an info string, NOT a closer. Without this rule a
// 3-backtick block that documents another fence closes prematurely and the
// spill becomes prose — silent corruption that contradicts the GFM/Obsidian/
// GitHub interop guarantee. Must render as ONE block matching GitHub.
func TestCodeBlock_InfoStringLineIsNotACloser(t *testing.T) {
	src := "```markdown\n" +
		"Example:\n" +
		"```js\n" +
		"foo()\n" +
		"```\n" +
		"done\n"
	first, _, _, _, err := ParseFileContent(src, "NB", "", "PG", "2026-06-14", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var code *ParsedBlock
	for i := range first {
		if first[i].Type == BlockCode {
			code = &first[i]
		}
	}
	if code == nil {
		t.Fatalf("expected one BlockCode, got %+v", first)
	}
	wantBody := "Example:\n```js\nfoo()"
	if code.CleanText != wantBody {
		t.Errorf("info-string-fence body drifted (block split prematurely?)\nwant: %q\n got: %q",
			wantBody, code.CleanText)
	}
	// Round-trip is byte-stable.
	rendered := RenderFileContent(first, "", "", 4)
	second, _, _, _, err := ParseFileContent(rendered, "NB", "", "PG", "2026-06-14", 4)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	var code2 *ParsedBlock
	for i := range second {
		if second[i].Type == BlockCode {
			code2 = &second[i]
		}
	}
	if code2 == nil || code2.CleanText != wantBody {
		t.Errorf("round-trip lost the info-string-fence body\nrendered:\n%s", rendered)
	}
}

// ---- #310: GFM table managed blocks ----------------------------------------
// Tables become ONE managed ParsedBlock (Type: TABLE), parallel to BlockCode.
// The clean_text is the raw GFM rows (header + separator + data); renderBlock
// emits them verbatim + a trailing id line. parse → render → parse is
// byte-stable.

func TestTable_MultilineRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "simple 3-col table",
			body: "| a | b | c |\n|---|---|---|\n| 1 | 2 | 3 |",
		},
		{
			name: "5-row table",
			body: "| name | age | city |\n|---|---|---|\n| Alice | 30 | NYC |\n| Bob | 25 | LA |\n| Carol | 35 | SF |",
		},
		{
			name: "header-only (no data rows)",
			body: "| a | b |\n|---|---|",
		},
		{
			name: "literal pipe escaped in cell",
			body: "| a | b |\n|---|---|\n| 1 \\| 2 | 3 |",
		},
		{
			name: "alignment markers",
			body: "| left | center | right |\n|:---|:---:|---:|\n| a | b | c |",
		},
		{
			name: "uuid reference in cell",
			body: "| ref | val |\n|---|---|\n| ((11111111-1111-1111-1111-111111111111)) | 42 |",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block := ParsedBlock{
				ID:        "33333333-1111-1111-1111-111111111111",
				Type:      BlockTable,
				CleanText: tc.body,
				FileDate:  "2026-06-20",
			}
			rendered := RenderFileContent([]ParsedBlock{block}, "", "", 4)
			reparsed, _, _, _, err := ParseFileContent(rendered, "NB", "", "PG", "2026-06-20", 4)
			if err != nil {
				t.Fatalf("reparse: %v\nrendered:\n%s", err, rendered)
			}
			if len(reparsed) != 1 || reparsed[0].Type != BlockTable {
				t.Fatalf("expected one BlockTable, got %+v", reparsed)
			}
			if reparsed[0].CleanText != tc.body {
				t.Errorf("CleanText drifted\nwant: %q\n got: %q", tc.body, reparsed[0].CleanText)
			}
			// Byte-stable across a second render pass.
			rendered2 := RenderFileContent(reparsed, "", "", 4)
			if rendered != rendered2 {
				t.Errorf("not byte-stable\n--- pass1 ---\n%s\n--- pass2 ---\n%s", rendered, rendered2)
			}
		})
	}
}

func TestTable_NoSeparatorStaysNotes(t *testing.T) {
	// A pipe-prefixed line NOT followed by a separator is NOT a table — it
	// stays a regular NOTE block. This prevents false positives from stray
	// pipe characters.
	src := "| not a table <!-- id: 44444444-1111-1111-1111-111111111111 -->\n"
	blocks, _, _, _, err := ParseFileContent(src, "NB", "", "PG", "2026-06-20", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, b := range blocks {
		if b.Type == BlockTable {
			t.Fatalf("pipe-prefixed note without separator was falsely detected as a table: %+v", b)
		}
	}
}

func TestTable_UnterminatedRegion(t *testing.T) {
	// A table run that goes to EOF is still a valid TABLE block (the region
	// simply consumes all remaining pipe rows).
	src := "| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |"
	blocks, _, _, _, err := ParseFileContent(src, "NB", "", "PG", "2026-06-20", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var tbl *ParsedBlock
	for i := range blocks {
		if blocks[i].Type == BlockTable {
			tbl = &blocks[i]
		}
	}
	if tbl == nil {
		t.Fatalf("expected one BlockTable, got %+v", blocks)
	}
	if !strings.Contains(tbl.CleanText, "| 3 | 4 |") {
		t.Errorf("last data row lost: %q", tbl.CleanText)
	}
}

func TestTable_ExternalFileGetsIdOnFirstParse(t *testing.T) {
	// A table authored externally (Obsidian/VS Code) carries no id. First
	// parse mints one on its own trailing line; second parse is stable.
	src := "| a | b |\n|---|---|\n| 1 | 2 |\n"
	blocks, _, _, modified, err := ParseFileContent(src, "NB", "", "PG", "2026-06-20", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !modified {
		t.Errorf("expected the external table to be assigned an id on first parse")
	}
	var tbl *ParsedBlock
	for i := range blocks {
		if blocks[i].Type == BlockTable {
			tbl = &blocks[i]
		}
	}
	if tbl == nil {
		t.Fatalf("expected a BlockTable, got %+v", blocks)
	}
	if tbl.ID == "" {
		t.Errorf("table has no id")
	}
	// Second parse is stable (no further modification).
	rendered := RenderFileContent(blocks, "", "", 4)
	_, _, _, modified2, err := ParseFileContent(rendered, "NB", "", "PG", "2026-06-20", 4)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if modified2 {
		t.Errorf("second parse should be stable, but was modified")
	}
}

// ---- #310: <details> managed blocks ----------------------------------------
// Foldable <details> regions become ONE managed ParsedBlock (Type: DETAILS).
// The clean_text is the full <details>...</details> HTML; renderBlock emits it
// verbatim + a trailing id line.

func TestDetails_MultilineRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "simple details",
			body: "<details>\n<summary>Title</summary>\nbody text\n</details>",
		},
		{
			name: "nested details depth 2",
			body: "<details>\n<summary>Outer</summary>\n<details>\n<summary>Inner</summary>\ninner body\n</details>\n</details>",
		},
		{
			name: "body contains code fence",
			body: "<details>\n<summary>Code</summary>\n```\ncode here\n```\n</details>",
		},
		{
			name: "summary with inline content",
			body: "<details>\n<summary>Click **here**</summary>\nsome content\n</details>",
		},
		{
			name: "details open with attributes",
			body: "<details open>\n<summary>Open by default</summary>\nvisible content\n</details>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block := ParsedBlock{
				ID:        "55555555-1111-1111-1111-111111111111",
				Type:      BlockDetails,
				CleanText: tc.body,
				FileDate:  "2026-06-20",
			}
			rendered := RenderFileContent([]ParsedBlock{block}, "", "", 4)
			reparsed, _, _, _, err := ParseFileContent(rendered, "NB", "", "PG", "2026-06-20", 4)
			if err != nil {
				t.Fatalf("reparse: %v\nrendered:\n%s", err, rendered)
			}
			if len(reparsed) != 1 || reparsed[0].Type != BlockDetails {
				t.Fatalf("expected one BlockDetails, got %+v", reparsed)
			}
			if reparsed[0].CleanText != tc.body {
				t.Errorf("CleanText drifted\nwant: %q\n got: %q", tc.body, reparsed[0].CleanText)
			}
			// Byte-stable across a second render pass.
			rendered2 := RenderFileContent(reparsed, "", "", 4)
			if rendered != rendered2 {
				t.Errorf("not byte-stable\n--- pass1 ---\n%s\n--- pass2 ---\n%s", rendered, rendered2)
			}
		})
	}
}

func TestDetails_UnterminatedRegion(t *testing.T) {
	// An unclosed <details> emits the opener as a NOTE, not a DETAILS block.
	src := "<details>\n<summary>Oops</summary>\nno closing tag\n"
	blocks, _, _, _, err := ParseFileContent(src, "NB", "", "PG", "2026-06-20", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, b := range blocks {
		if b.Type == BlockDetails {
			t.Fatalf("unterminated <details> should not produce a DETAILS block: %+v", b)
		}
	}
}

func TestDetails_ExternalFileGetsIdOnFirstParse(t *testing.T) {
	// A <details> authored externally carries no id. First parse mints one.
	src := "<details>\n<summary>External</summary>\nfrom VS Code\n</details>\n"
	blocks, _, _, modified, err := ParseFileContent(src, "NB", "", "PG", "2026-06-20", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !modified {
		t.Errorf("expected the external details to be assigned an id on first parse")
	}
	var det *ParsedBlock
	for i := range blocks {
		if blocks[i].Type == BlockDetails {
			det = &blocks[i]
		}
	}
	if det == nil {
		t.Fatalf("expected a BlockDetails, got %+v", blocks)
	}
	if det.ID == "" {
		t.Errorf("details has no id")
	}
	// Second parse is stable.
	rendered := RenderFileContent(blocks, "", "", 4)
	_, _, _, modified2, err := ParseFileContent(rendered, "NB", "", "PG", "2026-06-20", 4)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if modified2 {
		t.Errorf("second parse should be stable")
	}
}

// ---- Mixed regions in one file --------------------------------------------
// Code blocks, tables, and details coexist in one file without interfering.

func TestMixedRegions_CoexistInOneFile(t *testing.T) {
	src := "# Heading <!-- id: 66666666-1111-1111-1111-111111111111 -->\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n" +
		"- note <!-- id: 77777777-1111-1111-1111-111111111111 -->\n" +
		"```go\nfunc main() {}\n```\n" +
		"<details>\n<summary>Fold</summary>\nfolded\n</details>\n"
	blocks, _, _, _, err := ParseFileContent(src, "NB", "", "PG", "2026-06-20", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Expect: HEADER, TABLE, NOTE, CODE, DETAILS (all as managed blocks).
	types := make(map[BlockType]int)
	for _, b := range blocks {
		types[b.Type]++
	}
	if types[BlockHeader] != 1 {
		t.Errorf("expected 1 HEADER, got %d", types[BlockHeader])
	}
	if types[BlockTable] != 1 {
		t.Errorf("expected 1 TABLE, got %d", types[BlockTable])
	}
	if types[BlockNote] != 1 {
		t.Errorf("expected 1 NOTE, got %d", types[BlockNote])
	}
	if types[BlockCode] != 1 {
		t.Errorf("expected 1 CODE, got %d", types[BlockCode])
	}
	if types[BlockDetails] != 1 {
		t.Errorf("expected 1 DETAILS, got %d", types[BlockDetails])
	}
	// Round-trip the rendered output — must be byte-stable.
	rendered := RenderFileContent(blocks, "", "", 4)
	_, _, _, _, err = ParseFileContent(rendered, "NB", "", "PG", "2026-06-20", 4)
	if err != nil {
		t.Fatalf("reparse: %v\nrendered:\n%s", err, rendered)
	}
}

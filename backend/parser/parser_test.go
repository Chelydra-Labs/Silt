package parser

import (
	"strings"
	"testing"
)

func TestEnsureBlockID(t *testing.T) {
	// Line without ID
	line1 := "- [ ] TODO TASK Draft README definition file"
	id1, newLine1, modified1 := EnsureBlockID(line1)
	if !modified1 {
		t.Errorf("Expected line to be modified")
	}
	if id1 == "" {
		t.Errorf("Expected an ID to be generated")
	}
	if !strings.Contains(newLine1, "<!-- id: "+id1+" -->") {
		t.Errorf("Expected new line to contain ID comment")
	}

	// Line with ID
	line2 := "- [ ] TODO TASK Draft README <!-- id: 8fa72c3b-d1e5-4b0d-8ea2-bfcfd2ee7f8a -->"
	id2, newLine2, modified2 := EnsureBlockID(line2)
	if modified2 {
		t.Errorf("Expected line not to be modified")
	}
	if id2 != "8fa72c3b-d1e5-4b0d-8ea2-bfcfd2ee7f8a" {
		t.Errorf("Expected matched ID, got: %s", id2)
	}
	if newLine2 != line2 {
		t.Errorf("Expected output line to equal input line")
	}
}

func TestNormalizeDate(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2026-06-13", "2026-06-13"},
		{"6/13/26", "2026-06-13"},
		{"06/13/2026", "2026-06-13"},
		{" 6/13/2026 ", "2026-06-13"},
		{"", ""},
	}

	for _, tc := range tests {
		actual := normalizeDate(tc.input)
		if actual != tc.expected {
			t.Errorf("For %q expected %q, got %q", tc.input, tc.expected, actual)
		}
	}
}

func TestParseLine(t *testing.T) {
	// Test task line
	taskLine := "- [ ] TODO TASK [Chris](2026-06-13, 2026-08-03)#1 Draft README <!-- id: 8fa72c3b-d1e5-4b0d-8ea2-bfcfd2ee7f8a -->"
	block, _, _ := ParseLine(taskLine, 1, 4)

	if block.Type != BlockTask {
		t.Errorf("Expected BlockTask, got %s", block.Type)
	}
	if block.Owner != "Chris" {
		t.Errorf("Expected owner Chris, got %s", block.Owner)
	}
	if block.StartDate != "2026-06-13" || block.DueDate != "2026-08-03" {
		t.Errorf("Expected start 2026-06-13 and due 2026-08-03, got start: %s, due: %s", block.StartDate, block.DueDate)
	}
	if block.Priority != 1 {
		t.Errorf("Expected priority 1, got %d", block.Priority)
	}
	if block.CleanText != "Draft README" {
		t.Errorf("Expected clean text 'Draft README', got '%s'", block.CleanText)
	}

	// Test header line
	headerLine := "## General Info <!-- id: 2a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8a -->"
	block2, _, _ := ParseLine(headerLine, 2, 4)
	if block2.Type != BlockHeader {
		t.Errorf("Expected BlockHeader, got %s", block2.Type)
	}
	if block2.Depth != 2 {
		t.Errorf("Expected header depth 2, got %d", block2.Depth)
	}
	if block2.CleanText != "General Info" {
		t.Errorf("Expected clean text 'General Info', got '%s'", block2.CleanText)
	}

	// Test note line
	noteLine := "    - An bullet list note <!-- id: 3a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8b -->"
	block3, _, _ := ParseLine(noteLine, 3, 4)
	if block3.Type != BlockNote {
		t.Errorf("Expected BlockNote, got %s", block3.Type)
	}
	if block3.Depth != 1 {
		t.Errorf("Expected depth 1, got %d", block3.Depth)
	}
	if block3.CleanText != "An bullet list note" {
		t.Errorf("Expected clean text 'An bullet list note', got '%s'", block3.CleanText)
	}
}

func TestParseFileContent(t *testing.T) {
	doc := `---
notebook: Engineering
section: Architecture
date: 2026-06-13
tags: [work/sogav, systems/specs]
---
# Saturday, June 13, 2026 <!-- id: 0a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8a -->

## Stream Logging <!-- id: 1a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8b -->
- [ ] TODO TASK [Chris](2026-06-13)#1 Draft README <!-- id: 2a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8c -->
    - [/] DOING TASK [Jenny]#2 Research subtasks <!-- id: 3a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8d -->
- A general note <!-- id: 4a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8e -->`

	blocks, meta, newContent, modified, err := ParseFileContent(doc, "DefaultNB", "DefaultSec", "2026-06-01", 4)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if meta.Notebook != "Engineering" || meta.Section != "Architecture" || meta.Date != "2026-06-13" {
		t.Errorf("Metadata mismatch: %+v", meta)
	}
	if len(meta.Tags) != 2 || meta.Tags[0] != "work/sogav" {
		t.Errorf("Tags mismatch: %+v", meta.Tags)
	}

	if modified {
		t.Errorf("Expected no modification since all blocks have IDs")
	}
	if len(blocks) != 5 {
		t.Errorf("Expected 5 blocks, got %d", len(blocks))
	}

	// Verify parent-child
	// Check header-id-1 (depth 1)
	if blocks[0].ID != "0a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8a" || blocks[0].Type != BlockHeader {
		t.Errorf("Expected block 0 to be 0a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8a")
	}

	// Check task-id-1 (depth 0)
	if blocks[2].ID != "2a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8c" || blocks[2].ParentID != "" {
		t.Errorf("Expected block 2 2a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8c to have no parent, got: %s", blocks[2].ParentID)
	}

	// Check task-id-2 (depth 1, child of task-id-1)
	if blocks[3].ID != "3a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8d" {
		t.Fatalf("Expected block 3 to be 3a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8d")
	}
	if blocks[3].ParentID != "2a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8c" {
		t.Errorf("Expected parent to be 2a10b1a0-d1e5-4b0d-8ea2-bfcfd2ee7f8c, got: %s", blocks[3].ParentID)
	}

	// Verify that the rewritten content remains identical since no modifications were needed
	if newContent != doc {
		t.Errorf("Content mismatch. Expected:\n%s\nGot:\n%s", doc, newContent)
	}
}

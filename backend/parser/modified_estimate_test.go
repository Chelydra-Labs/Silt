package parser

import (
	"strings"
	"testing"
)

// TestParseLine_ModifiedAndEstimate covers [modified::] and [estimate::] as
// first-class task fields (#439/#440): parse into ParsedBlock, not ExtraTokens,
// and round-trip through render.
func TestParseLine_ModifiedAndEstimate(t *testing.T) {
	t.Run("modified and estimate parsed into fields", func(t *testing.T) {
		line := "- [ ] Plan sprint [modified:: 2026-07-06T15:30:00] [estimate:: 2h] <!-- id: aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa -->"
		block, _, _ := ParseLine(line, 1, 4)
		if block.ModifiedAt != "2026-07-06T15:30:00" {
			t.Errorf("ModifiedAt=%q want 2026-07-06T15:30:00", block.ModifiedAt)
		}
		if block.Estimate != "2h" {
			t.Errorf("Estimate=%q want 2h", block.Estimate)
		}
		if block.CleanText != "Plan sprint" {
			t.Errorf("CleanText=%q want Plan sprint", block.CleanText)
		}
	})

	t.Run("tokens not duplicated into ExtraTokens", func(t *testing.T) {
		line := "- [ ] Task [modified:: 2026-07-01T09:00:00] [estimate:: 30m] [due:: 2026-08-01] <!-- id: bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb -->"
		block, _, _ := ParseLine(line, 1, 4)
		for _, tok := range block.ExtraTokens {
			lower := strings.ToLower(tok)
			if strings.Contains(lower, "modified") || strings.Contains(lower, "estimate") {
				t.Errorf("first-class token must not appear in ExtraTokens, found %q", tok)
			}
		}
	})

	t.Run("round-trip render preserves fields", func(t *testing.T) {
		line := "- [ ] Task [modified:: 2026-07-06T12:00:00] [order:: 2] [estimate:: 1d] <!-- id: cccccccc-cccc-cccc-cccc-cccccccccccc -->"
		parsed, _, _ := ParseLine(line, 1, 4)
		rendered := renderBlock(parsed, 4)
		if !strings.Contains(rendered, "[modified:: 2026-07-06T12:00:00]") {
			t.Errorf("render missing modified: %s", rendered)
		}
		if !strings.Contains(rendered, "[estimate:: 1d]") {
			t.Errorf("render missing estimate: %s", rendered)
		}
		reparsed, _, _ := ParseLine(rendered, 1, 4)
		if reparsed.ModifiedAt != parsed.ModifiedAt {
			t.Errorf("ModifiedAt drifted: %q → %q", parsed.ModifiedAt, reparsed.ModifiedAt)
		}
		if reparsed.Estimate != parsed.Estimate {
			t.Errorf("Estimate drifted: %q → %q", parsed.Estimate, reparsed.Estimate)
		}
	})

	t.Run("omit when empty (no backfill)", func(t *testing.T) {
		block := ParsedBlock{
			ID:        "dddddddd-dddd-dddd-dddd-dddddddddddd",
			Type:      BlockTask,
			Status:    "TODO",
			CleanText: "legacy",
			FileDate:  "2026-07-06",
		}
		out := renderBlock(block, 4)
		if strings.Contains(out, "[modified::") {
			t.Errorf("expected no [modified::], got: %s", out)
		}
		if strings.Contains(out, "[estimate::") {
			t.Errorf("expected no [estimate::], got: %s", out)
		}
	})
}

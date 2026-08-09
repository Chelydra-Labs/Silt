package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestRunStdioProxy_RequiresReaderWriter(t *testing.T) {
	ctx := context.Background()
	err := RunStdioProxy(ctx, nil, StdioProxyOptions{})
	if err == nil {
		t.Fatal("expected error for nil Reader/Writer")
	}
	if !strings.Contains(err.Error(), "Reader and Writer are required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

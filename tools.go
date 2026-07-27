//go:build tools

// Package tools pins analyzer-only dependencies in go.mod so go mod tidy
// does not drop them. The //go:build tools tag keeps these imports out of
// the production binary.
package tools

import (
	_ "golang.org/x/tools/go/analysis"
	_ "golang.org/x/tools/go/analysis/passes/inspect"
	_ "golang.org/x/tools/go/analysis/singlechecker"
	_ "golang.org/x/tools/go/ast/inspector"
	_ "golang.org/x/tools/go/types/typeutil"

	_ "silt/backend/analysis/eventnameliteral"
	_ "silt/backend/analysis/withaipreflightdefer"
)

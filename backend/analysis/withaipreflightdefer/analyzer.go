// Package withaipreflightdefer defines a Go analyzer that enforces the
// drain-contract of (*App).withAIPreflight: the 3rd return value (a done func)
// MUST be the argument of a defer statement in the enclosing function body.
//
// withAIPreflight bundles vaultClosingWG.Add(1) under one RLock hold and
// returns a done func that calls vaultClosingWG.Done(). A caller that drops the
// defer leaks an unbalanced increment, deadlocking the next vault close/switch.
// Go's vet does not flag an unused function-return value, so this analyzer
// makes the convention a CI-time error.
package withaipreflightdefer

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// Analyzer flags a call to (*App).withAIPreflight whose 3rd return value
// (done func()) is not deferred in the enclosing function body.
var Analyzer = &analysis.Analyzer{
	Name:     "withaipreflightdefer",
	Doc:      "check that withAIPreflight's done func is deferred",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// withAIPreflight returns (ai.AIProvider, string, func(), error). The done
// func is the 3rd value (index 2).
const doneResultIndex = 2

func run(pass *analysis.Pass) (any, error) {
	inst := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// Walk assignment statements — that's where multi-value returns are bound.
	// We find assignments whose RHS is a call to withAIPreflight, capture the
	// identifier bound to the done return value, then verify a defer in the
	// enclosing function calls it.
	nodeFilter := []ast.Node{
		(*ast.AssignStmt)(nil),
		(*ast.FuncLit)(nil),
	}

	inst.Nodes(nodeFilter, func(node ast.Node, push bool) bool {
		if !push {
			return false
		}
		// Skip _test.go files: the drain-contract test intentionally calls
		// done() directly (not deferred) to control the WaitGroup lifecycle
		// and prove the Add/Done balance. Production callers are the target.
		pos := pass.Fset.Position(node.Pos())
		if strings.HasSuffix(pos.Filename, "_test.go") {
			return false
		}
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true // descend into FuncLit bodies too
		}
		if len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		// Resolve the callee statically so renamed/embedded methods are not
		// confused with the real target.
		callee := typeutil.Callee(pass.TypesInfo, call)
		fn, ok := callee.(*types.Func)
		if !ok {
			return true
		}
		if fn.Name() != "withAIPreflight" {
			return true
		}
		// The done func is the 3rd return value (index 2). Some callers use a
		// blank identifier for returns they don't need (e.g. `_, _, _, err`),
		// but the done value is never blank-discarded in correct code — that
		// would be the exact leak this analyzer catches. If the assignment
		// binds fewer than 3 identifiers, the done value is discarded: flag it.
		if len(assign.Lhs) <= doneResultIndex {
			pass.Reportf(call.Pos(), "withAIPreflight's done func must be captured and deferred (result discarded)")
			return true
		}
		doneIdent, ok := assign.Lhs[doneResultIndex].(*ast.Ident)
		if !ok {
			// Not a simple identifier (e.g. a selector or index) — skip; the
			// contract is about the local-variable binding pattern.
			return true
		}
		if doneIdent.Name == "_" {
			pass.Reportf(call.Pos(), "withAIPreflight's done func must be deferred (assigned to blank identifier)")
			return true
		}
		// Find the enclosing function body and verify a defer references doneIdent.
		enclosing := enclosingFuncBody(pass, node)
		if enclosing == nil || !isDeferred(enclosing, doneIdent.Name) {
			pass.Reportf(call.Pos(), "withAIPreflight's done func %q must be deferred", doneIdent.Name)
		}
		return true
	})

	return nil, nil
}

// enclosingFuncBody returns the body block of the function (FuncDecl or
// FuncLit) that contains node, or nil if it cannot be determined.
func enclosingFuncBody(pass *analysis.Pass, node ast.Node) *ast.BlockStmt {
	// Walk the file's AST to find the innermost function containing node.
	for _, file := range pass.Files {
		var found *ast.BlockStmt
		stop := false
		ast.Inspect(file, func(n ast.Node) bool {
			if stop || n == nil {
				return false
			}
			// Track function bodies that contain the target node.
			var body *ast.BlockStmt
			switch fn := n.(type) {
			case *ast.FuncDecl:
				body = fn.Body
			case *ast.FuncLit:
				body = fn.Body
			default:
				return true
			}
			if body == nil || !containsPos(body, node.Pos()) {
				return true
			}
			// This function body contains the node. Descend to find a tighter
			// enclosing FuncLit (defer done() inside a closure is valid even
			// if the closure is nested).
			found = body
			return true
		})
		if found != nil {
			return found
		}
		_ = stop
	}
	return nil
}

// containsPos reports whether the node's textual extent includes pos.
func containsPos(n ast.Node, pos token.Pos) bool {
	return pos >= n.Pos() && pos <= n.End()
}

// isDeferred reports whether body contains a defer statement whose call
// references name (directly `defer name()` or inside `defer func(){ name() }()`).
func isDeferred(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		deferStmt, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		if referencesIdent(deferStmt.Call, name) {
			found = true
			return false
		}
		return true
	})
	return found
}

// referencesIdent reports whether expr (a deferred call) invokes the identifier
// `name`, either directly (`name()`) or as the only call inside a closure
// (`func(){ name() }()`).
func referencesIdent(call *ast.CallExpr, name string) bool {
	// Direct: defer name()
	if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == name {
		return true
	}
	// Wrapped in a closure: defer func(){ name() }()
	if lit, ok := call.Fun.(*ast.FuncLit); ok {
		closureCallsIdent := false
		ast.Inspect(lit.Body, func(n ast.Node) bool {
			if closureCallsIdent || n == nil {
				return false
			}
			if inner, ok := n.(*ast.CallExpr); ok {
				if id, ok := inner.Fun.(*ast.Ident); ok && id.Name == name {
					closureCallsIdent = true
					return false
				}
			}
			return true
		})
		if closureCallsIdent {
			return true
		}
	}
	return false
}

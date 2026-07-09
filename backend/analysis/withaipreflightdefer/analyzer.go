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
		if !ok || fn.Name() != "withAIPreflight" {
			return true
		}
		// The done func is the 3rd return value (index 2). If the assignment
		// binds fewer than 3 identifiers, the done value is discarded: flag it.
		if len(assign.Lhs) <= doneResultIndex {
			pass.Reportf(call.Pos(), "withAIPreflight's done func must be captured and deferred (result discarded)")
			return true
		}
		doneIdent, ok := assign.Lhs[doneResultIndex].(*ast.Ident)
		if !ok {
			return true
		}
		if doneIdent.Name == "_" {
			pass.Reportf(call.Pos(), "withAIPreflight's done func must be deferred (assigned to blank identifier)")
			return true
		}
		// Resolve the done identifier to its types.Object so we compare by
		// identity (not name), avoiding false negatives from variable shadowing.
		doneObj := pass.TypesInfo.Uses[doneIdent]
		enclosing := enclosingFuncBody(pass, node)
		if enclosing == nil || !isDeferred(enclosing, doneObj, doneIdent.Name, pass.TypesInfo) {
			pass.Reportf(call.Pos(), "withAIPreflight's done func %q must be deferred", doneIdent.Name)
		}
		return true
	})

	return nil, nil
}

// enclosingFuncBody returns the body block of the innermost function (FuncDecl
// or FuncLit) that contains node, or nil if it cannot be determined.
func enclosingFuncBody(pass *analysis.Pass, node ast.Node) *ast.BlockStmt {
	for _, file := range pass.Files {
		var best *ast.BlockStmt
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				return false
			}
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
			// Descend to find the tightest enclosing function (a nested
			// closure is a valid defer location).
			best = body
			return true
		})
		if best != nil {
			return best
		}
	}
	return nil
}

// containsPos reports whether the node's textual extent includes pos.
func containsPos(n ast.Node, pos token.Pos) bool {
	return pos >= n.Pos() && pos <= n.End()
}

// isDeferred reports whether body contains a defer statement whose call invokes
// the same variable as the done return value. Matching prefers types.Object
// identity (shadow-safe); falls back to name matching when type info is nil.
func isDeferred(body *ast.BlockStmt, target types.Object, name string, info *types.Info) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		deferStmt, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		if referencesDone(deferStmt.Call, target, name, info) {
			found = true
			return false
		}
		return true
	})
	return found
}

// referencesDone reports whether expr (a deferred call) invokes the done
// variable, either directly (`done()`) or inside a closure
// (`func(){ done() }()`).
func referencesDone(call *ast.CallExpr, target types.Object, name string, info *types.Info) bool {
	if ident, ok := call.Fun.(*ast.Ident); ok && sameIdent(ident, target, name, info) {
		return true
	}
	if lit, ok := call.Fun.(*ast.FuncLit); ok {
		closureMatch := false
		ast.Inspect(lit.Body, func(n ast.Node) bool {
			if closureMatch || n == nil {
				return false
			}
			if inner, ok := n.(*ast.CallExpr); ok {
				if id, ok := inner.Fun.(*ast.Ident); ok && sameIdent(id, target, name, info) {
					closureMatch = true
					return false
				}
			}
			return true
		})
		if closureMatch {
			return true
		}
	}
	return false
}

// sameIdent reports whether ident refers to the same variable as target.
// Prefers types.Object identity (shadow-safe); falls back to name comparison.
func sameIdent(ident *ast.Ident, target types.Object, name string, info *types.Info) bool {
	if target != nil && info != nil {
		if use := info.Uses[ident]; use != nil {
			return use == target
		}
	}
	return ident.Name == name
}

// Package eventnameliteral defines a Go analyzer that rejects bare string
// literals (and EventName("…") conversions) as the first argument to emit /
// emitOrQueue. Event names must come from declared EventName consts so renames
// and typos are CI-time failures rather than silent runtime mismatches with
// the frontend.
//
// Scope is emit-site literals only: n := EventName("typo"); a.emit(n) is
// intentionally allowed (no SSA). Prefer events.go consts at construction.
package eventnameliteral

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// Analyzer flags emit / emitOrQueue calls whose event-name argument is a bare
// string literal or an EventName("literal") conversion.
var Analyzer = &analysis.Analyzer{
	Name:     "eventnameliteral",
	Doc:      "check that emit/emitOrQueue use EventName consts, not string literals",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inst := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}

	inst.Preorder(nodeFilter, func(node ast.Node) {
		// Skip _test.go: wails_runtime_test.go uses bare strings intentionally.
		pos := pass.Fset.Position(node.Pos())
		if strings.HasSuffix(pos.Filename, "_test.go") {
			return
		}

		call := node.(*ast.CallExpr)
		callee := typeutil.Callee(pass.TypesInfo, call)
		fn, ok := callee.(*types.Func)
		if !ok {
			return
		}
		name := fn.Name()
		if name != "emit" && name != "emitOrQueue" {
			return
		}
		if len(call.Args) < 1 {
			return
		}
		if msg, bad := forbiddenEventNameMsg(call.Args[0], pass.TypesInfo); bad {
			pass.Reportf(call.Args[0].Pos(), "%s", msg)
		}
	})

	return nil, nil
}

// forbiddenEventNameMsg reports whether expr is a forbidden emit-site event
// name and returns a diagnostic that quotes the bad literal when possible.
// Allowed: EventName-typed consts, aiStreamEventName(const, …), params/locals.
func forbiddenEventNameMsg(expr ast.Expr, info *types.Info) (msg string, bad bool) {
	expr = ast.Unparen(expr)

	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		return fmt.Sprintf("emit/emitOrQueue: use an EventName const from events.go, not bare string literal %s", e.Value), true

	case *ast.CallExpr:
		// EventName("literal") — type conversion or call with string lit arg.
		if isEventNameFun(e.Fun, info) && len(e.Args) == 1 {
			arg := ast.Unparen(e.Args[0])
			if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				return fmt.Sprintf("emit/emitOrQueue: use an EventName const from events.go, not EventName(%s) conversion", lit.Value), true
			}
		}
		// aiStreamEventName(first, …): reject only if first arg is forbidden.
		if isAIStreamEventNameFun(e.Fun, info) && len(e.Args) >= 1 {
			return forbiddenEventNameMsg(e.Args[0], info)
		}
		// Other calls (helpers returning EventName) are allowed.
		return "", false

	default:
		// Ident/Selector consts, params, locals, etc. — allowed (no SSA).
		return "", false
	}
}

func isEventNameFun(fun ast.Expr, info *types.Info) bool {
	fun = ast.Unparen(fun)
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name == "EventName"
	case *ast.SelectorExpr:
		return f.Sel.Name == "EventName"
	default:
		return false
	}
}

func isAIStreamEventNameFun(fun ast.Expr, info *types.Info) bool {
	fun = ast.Unparen(fun)
	// Prefer resolved callee name when available.
	if call, ok := fun.(*ast.Ident); ok && call.Name == "aiStreamEventName" {
		return true
	}
	if sel, ok := fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "aiStreamEventName" {
		return true
	}
	// typeutil path for method values / package-qualified forms used as Fun
	// of an outer call is handled by the CallExpr case via name on Fun only.
	_ = info
	return false
}

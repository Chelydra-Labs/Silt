// Package eventnameliteral defines a Go analyzer that rejects bare string
// literals (and EventName("…") conversions) as the first argument to emit /
// emitOrQueue. Event names must come from declared EventName consts so renames
// and typos are CI-time failures rather than silent runtime mismatches with
// the frontend.
//
// Tracks EventName literals through locals and same-package single-literal
// helpers at emit sites. Dynamic compositions (concatenation, params,
// cross-package helpers) remain allowed.
package eventnameliteral

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/types/typeutil"
)

// Analyzer flags emit / emitOrQueue calls whose event-name argument is a bare
// string literal, an EventName("literal") conversion, or such a value carried
// through a local or same-package helper.
var Analyzer = &analysis.Analyzer{
	Name:     "eventnameliteral",
	Doc:      "check that emit/emitOrQueue use EventName consts, not string literals",
	Requires: []*analysis.Analyzer{inspect.Analyzer, buildssa.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inst := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	ssainput := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)

	rsv := resolver{valid: eventNameConstValues(pass.Pkg)}

	// Index SSA Call instructions by source position so each emit callsite can
	// be matched to its *ssa.Call and the EventName argument resolved.
	callsByPos := make(map[token.Pos]*ssa.Call)
	for _, fn := range ssainput.SrcFuncs {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				if c, ok := instr.(*ssa.Call); ok {
					callsByPos[c.Pos()] = c
				}
			}
		}
	}

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
		arg := call.Args[0]

		// Fast path: inline string literal / EventName("…") conversion.
		if msg, bad := forbiddenEventNameMsg(arg, pass.TypesInfo); bad {
			pass.Reportf(arg.Pos(), "%s", msg)
			return
		}

		// Slow path: arg is an ident/local/helper-call — resolve via SSA to a
		// literal carried indirectly into the emit site. SSA Call.Pos() reports
		// the AST CallExpr.Lparen, so match on that.
		ssaCall := callsByPos[call.Lparen]
		if ssaCall == nil {
			return
		}
		ssaArgs := ssaCall.Common().Args
		// Method calls prepend the receiver; drop it to align with AST call.Args.
		if len(ssaArgs) == len(call.Args)+1 {
			ssaArgs = ssaArgs[1:]
		}
		if len(ssaArgs) < 1 {
			return
		}
		lit, ok := rsv.eventNameLiteralFromValue(ssaArgs[0])
		if !ok {
			return
		}
		pass.Reportf(arg.Pos(),
			"emit/emitOrQueue: use an EventName const from events.go, not an EventName(%q) value carried through %s",
			lit, carrierDescription(arg))
	})

	return nil, nil
}

// carrierDescription describes how the literal reached the emit site, based on
// the AST shape of the callsite argument.
func carrierDescription(arg ast.Expr) string {
	if _, ok := ast.Unparen(arg).(*ast.CallExpr); ok {
		return "a helper call"
	}
	return "a local"
}

// eventNameConstValues returns the string values of package-level consts whose
// type is EventName. buildssa folds both `n := EventFoo` and
// `n := EventName("x")` into identical *ssa.Const instructions, so a folded
// Const whose value matches one of these is treated as a const reference
// (allowed), not a stray literal.
func eventNameConstValues(pkg *types.Package) map[string]bool {
	out := make(map[string]bool)
	if pkg == nil || pkg.Scope() == nil {
		return out
	}
	for _, name := range pkg.Scope().Names() {
		c, ok := pkg.Scope().Lookup(name).(*types.Const)
		if !ok || !isEventNameType(c.Type()) {
			continue
		}
		if c.Val() != nil && c.Val().Kind() == constant.String {
			out[constant.StringVal(c.Val())] = true
		}
	}
	return out
}

// resolver threads the declared-const value set through the recursive resolver
// so every Const leaf can distinguish const references from stray literals.
type resolver struct {
	valid map[string]bool
}

// eventNameLiteralFromValue reports whether v provably traces to a string
// literal backing an EventName value. Conservative: anything it cannot prove is
// a stray literal (params, concatenations, cross-package helpers, declared
// EventName consts) returns ("", false).
func (r *resolver) eventNameLiteralFromValue(v ssa.Value) (lit string, ok bool) {
	switch v := v.(type) {
	case *ssa.Const:
		if v.Value != nil && v.Value.Kind() == constant.String {
			s := constant.StringVal(v.Value)
			if r.valid[s] {
				return "", false // a declared EventName const folded into a Const
			}
			return s, true
		}
		return "", false
	case *ssa.Convert:
		// EventName("typo") where EventName is `type EventName string`.
		return r.eventNameLiteralFromValue(v.X)
	case *ssa.MakeInterface:
		return r.eventNameLiteralFromValue(v.X)
	case *ssa.UnOp:
		// *Alloc — a load of a local slot. Find the dominating Store.
		if alloc, ok := v.X.(*ssa.Alloc); ok {
			return r.literalFromAlloc(alloc)
		}
		return "", false
	case *ssa.Call:
		common := v.Common()
		callee, _ := common.Value.(*ssa.Function)
		// aiStreamEventName(base, id): recurse into first arg only, mirroring
		// the AST fast path's isAIStreamEventNameFun handling.
		if isAIStreamSSA(callee) {
			if len(common.Args) >= 1 {
				return r.eventNameLiteralFromValue(common.Args[0])
			}
			return "", false
		}
		// Same-package helper whose every EventName return is the same literal.
		return r.helperReturnsLiteral(common.Value)
	default:
		// *ssa.Parameter, *ssa.FreeVar, *ssa.BinOp (concat), *ssa.Global load,
		// *ssa.Lookup, *ssa.Extract, … → ALLOW.
		return "", false
	}
}

// literalFromAlloc finds the last Store into alloc within its enclosing
// function; last write wins. No store, or a store this resolver can't prove is
// a literal, → ALLOW.
func (r *resolver) literalFromAlloc(alloc *ssa.Alloc) (string, bool) {
	var storeVal ssa.Value
	parent := alloc.Parent()
	if parent == nil {
		return "", false
	}
	for _, b := range parent.Blocks {
		for _, instr := range b.Instrs {
			if s, ok := instr.(*ssa.Store); ok && s.Addr == alloc {
				storeVal = s.Val
			}
		}
	}
	if storeVal == nil {
		return "", false
	}
	return r.eventNameLiteralFromValue(storeVal)
}

// helperReturnsLiteral chases a same-package helper only if every EventName-typed
// return result resolves to the SAME literal. Any dynamic/param/multi-literal
// return → ALLOW the whole helper (false-positive avoidance).
func (r *resolver) helperReturnsLiteral(callee ssa.Value) (string, bool) {
	fn, ok := callee.(*ssa.Function)
	if !ok || fn == nil || fn.Blocks == nil {
		return "", false
	}
	var only string
	saw := false
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			ret, isRet := instr.(*ssa.Return)
			if !isRet || len(ret.Results) == 0 {
				continue
			}
			var rv ssa.Value
			for _, res := range ret.Results {
				if isEventNameType(res.Type()) {
					rv = res
					break
				}
			}
			if rv == nil {
				return "", false // returns no EventName — not our shape
			}
			l, ok := r.eventNameLiteralFromValue(rv)
			if !ok {
				return "", false // a return path we can't prove → ALLOW whole fn
			}
			if saw && l != only {
				return "", false // two different literals → ALLOW (conservative)
			}
			only, saw = l, true
		}
	}
	return only, saw
}

func isEventNameType(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	b, ok := n.Underlying().(*types.Basic)
	return ok && b.Kind() == types.String && n.Obj().Name() == "EventName"
}

func isAIStreamSSA(v *ssa.Function) bool {
	return v != nil && v.Name() == "aiStreamEventName"
}

// forbiddenEventNameMsg reports whether expr is a forbidden inline emit-site
// event name and returns a diagnostic that quotes the bad literal when
// possible. Allowed inline: EventName-typed consts, aiStreamEventName(const, …),
// params/locals (those are handled by the SSA slow path when they carry a literal).
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
		// Other calls (helpers returning EventName) are handled by the SSA path.
		return "", false

	default:
		// Ident/Selector consts, params, locals — SSA slow path decides.
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

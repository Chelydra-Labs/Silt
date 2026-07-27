//go:build tools

// Command inventory scans the Silt codebase and emits a deterministic JSON
// inventory of the Wails IPC surface: exported *App methods, Go-side
// a.emit / a.wailsApp.Event.Emit event names, frontend imports of the
// generated App bindings and the @wailsio/runtime package, and frontend
// Events.On subscriptions.
//
// The "tools" build tag keeps this binary out of the production build.
//
// Modes (selected by flag):
//
//	# Print the live inventory to stdout (ad-hoc inspection):
//	go run -tags tools ./cmd/inventory/
//
//	# Compare the live surface against the checked-in fixture. CI's binding
//	# parity gate runs this — it fails on any missing/extra/changed method
//	# signature (the IPC contract) and reports drift in the other sections.
//	go run -tags tools ./cmd/inventory/ -compare ./cmd/inventory/current-approved-v3.json
//
//	# Regenerate the checked-in fixture after an INTENTIONAL surface change
//	# (new/removed/renamed binding). Re-run -compare afterwards to confirm.
//	go run -tags tools ./cmd/inventory/ -update ./cmd/inventory/current-approved-v3.json
//
// All three modes derive the surface purely from source (AST scans + repo-
// relative, forward-slashed paths), so the fixture is byte-identical across
// Windows / Linux / macOS checkouts — no platform-specific path noise.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const defaultFrontendSrc = "frontend/src"

// MethodInfo captures a single exported *App method.
type MethodInfo struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
}

// Inventory is the full IPC surface report. The first five fields are the
// sections requested by the spec; plugin_method_count is reported separately
// as a sanity check on the Plugin* subset of methods.
type Inventory struct {
	Methods                []MethodInfo `json:"methods"`
	GoEvents               []string     `json:"go_events"`
	FrontendBindingImports []string     `json:"frontend_binding_imports"`
	FrontendRuntimeImports []string     `json:"frontend_runtime_imports"`
	FrontendEvents         []string     `json:"frontend_events"`
	PluginMethodCount      int          `json:"plugin_method_count"`
}

func main() {
	comparePath := flag.String("compare", "", "compare the live surface against the fixture at this path; exit 1 on any method-signature drift")
	updatePath := flag.String("update", "", "write the live inventory into the fixture at this path (regenerate after an intentional surface change)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(),
			"usage: inventory [flags] [repo-root]\n\n  flags:\n    -compare <path>\n    -update <path>\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	root := flag.Arg(0)
	if root == "" {
		root = "."
	}

	switch {
	case *updatePath != "":
		inv := buildInventory(root)
		if err := writeFixture(*updatePath, inv); err != nil {
			fmt.Fprintln(os.Stderr, "update:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "inventory: fixture written to %s (%d methods)\n", *updatePath, len(inv.Methods))
	case *comparePath != "":
		inv := buildInventory(root)
		want, err := readFixture(*comparePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "compare:", err)
			os.Exit(1)
		}
		diff := compareInventories(want, inv)
		fmt.Print(renderDiff(diff))
		if !diff.MethodsEqual() {
			fmt.Fprintf(os.Stderr, "inventory: METHOD-SIGNATURE DRIFT — regenerate with: go run -tags tools ./cmd/inventory/ -update %s\n", *comparePath)
			os.Exit(1)
		}
	default:
		// No flag: emit the live inventory to stdout (ad-hoc inspection).
		inv := buildInventory(root)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(inv); err != nil {
			fmt.Fprintln(os.Stderr, "encode:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr,
			"inventory: methods=%d (plugin=%d) go_events=%d frontend_binding_imports=%d frontend_runtime_imports=%d frontend_events=%d\n",
			len(inv.Methods), inv.PluginMethodCount, len(inv.GoEvents), len(inv.FrontendBindingImports), len(inv.FrontendRuntimeImports), len(inv.FrontendEvents),
		)
	}
}

// buildInventory composes the full IPC-surface report for the repo at root.
// The scanning functions are deterministic and source-derived only, so the
// returned Inventory is byte-stable across platforms (see renderSignature /
// scanFrontend for the path-normalization details).
func buildInventory(root string) Inventory {
	methods, pluginCount := scanMethods(root)
	goEvents := scanGoEvents(root)
	bindImports, runtimeImports, feEvents := scanFrontend(filepath.Join(root, defaultFrontendSrc))

	return Inventory{
		Methods:                methods,
		GoEvents:               goEvents,
		FrontendBindingImports: bindImports,
		FrontendRuntimeImports: runtimeImports,
		FrontendEvents:         feEvents,
		PluginMethodCount:      pluginCount,
	}
}

// scanMethods walks only the top-level .go files in root (no subdirectories,
// no _test.go) and collects exported methods declared on *App.
func scanMethods(root string) ([]MethodInfo, int) {
	entries, err := os.ReadDir(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read root:", err)
		os.Exit(1)
	}

	var methods []MethodInfo
	pluginCount := 0
	fset := token.NewFileSet()

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(root, name)
		src, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read", path, ":", err)
			continue
		}
		file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			fmt.Fprintln(os.Stderr, "parse", path, ":", err)
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 || fn.Name == nil {
				continue
			}
			star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			id, ok := star.X.(*ast.Ident)
			if !ok || id.Name != "App" {
				continue
			}
			if !fn.Name.IsExported() {
				continue
			}
			methodName := fn.Name.Name
			methods = append(methods, MethodInfo{
				Name:      methodName,
				Signature: renderSignature(fset, src, fn),
			})
			if strings.HasPrefix(methodName, "Plugin") {
				pluginCount++
			}
		}
	}

	sort.Slice(methods, func(i, j int) bool { return methods[i].Name < methods[j].Name })
	return methods, pluginCount
}

// renderSignature slices the source bytes from the method name through the
// closing paren of the results list (or params if there are no results) and
// collapses internal whitespace so multi-line signatures render on one line.
// The receiver is intentionally excluded — only name(params) (results).
func renderSignature(fset *token.FileSet, src []byte, fn *ast.FuncDecl) string {
	if len(src) == 0 || fn.Type == nil {
		return fn.Name.Name + "()"
	}
	startOff, err := offsetFor(fset, fn.Name.Pos(), len(src))
	if err != nil {
		return fn.Name.Name + "()"
	}
	var endPos token.Pos
	if fn.Type.Results != nil {
		endPos = fn.Type.Results.End()
	} else {
		endPos = fn.Type.Params.End()
	}
	stopOff, err := offsetFor(fset, endPos, len(src))
	if err != nil {
		return fn.Name.Name + "()"
	}
	if stopOff < startOff {
		stopOff = startOff
	}
	return collapseWS(string(src[startOff:stopOff]))
}

func offsetFor(fset *token.FileSet, pos token.Pos, max int) (int, error) {
	if !pos.IsValid() {
		return 0, fmt.Errorf("invalid position")
	}
	off := fset.Position(pos).Offset
	if off < 0 || off > max {
		return 0, fmt.Errorf("offset %d out of range [0,%d]", off, max)
	}
	return off, nil
}

func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// scanGoEvents reads the canonical EventName const block in events.go and
// returns the sorted set of declared event-name string values. Every emit site
// now references an EventName const (not a bare string literal), so the
// declared const block IS the authoritative event surface — scanning call sites
// for literals would find nothing. Returns an empty slice (not an error) if
// events.go is absent or unparseable so the gate degrades gracefully.
func scanGoEvents(root string) []string {
	seen := map[string]struct{}{}
	path := filepath.Join(root, "events.go")
	src, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.AllErrors)
	if err != nil {
		return nil // skip unparseable rather than aborting
	}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		// lastType tracks the inherited type across specs in a parenthesized
		// const block (a ValueSpec that omits the type inherits the preceding
		// spec's type), matching Go's const-declaration semantics.
		lastType := ""
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if vs.Type != nil {
				if id, ok := vs.Type.(*ast.Ident); ok {
					lastType = id.Name
				}
			}
			if lastType != "EventName" || len(vs.Names) == 0 || len(vs.Values) == 0 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			if name, err := strconv.Unquote(lit.Value); err == nil {
				seen[name] = struct{}{}
			}
		}
	}
	return sortedKeys(seen)
}

var (
	// A string literal containing the respective v3 binding/runtime import
	// path. Covers both the extensionless and .js import forms used in the
	// frontend.
	bindingPathRE = regexp.MustCompile(`['"\x60][^'"\x60]*bindings/silt/app[^'"\x60]*['"\x60]`)
	runtimePathRE = regexp.MustCompile(`['"\x60][^'"\x60]*@wailsio/runtime[^'"\x60]*['"\x60]`)
	// eventsOnRE captures the event-name string literal handed to Events.On
	// (the v3 runtime API; v2 used EventsOn). \s* spans the newlines seen in
	// this codebase's multi-line calls.
	eventsOnRE = regexp.MustCompile(`Events\.On\s*\(\s*['"\x60]([^'"\x60]+)['"\x60]`)
)

// scanFrontend walks .ts/.svelte files under frontendRoot and records which
// files import the App bindings / @wailsio/runtime, plus the set of Events.On
// event-name string literals. Paths are repo-relative with forward slashes.
func scanFrontend(frontendRoot string) (bindingImports, runtimeImports, frontendEvents []string) {
	bindSet := map[string]struct{}{}
	runtimeSet := map[string]struct{}{}
	eventsSet := map[string]struct{}{}

	_ = filepath.Walk(frontendRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".ts" && ext != ".svelte" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(".", path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		s := string(src)
		if bindingPathRE.MatchString(s) {
			bindSet[rel] = struct{}{}
		}
		if runtimePathRE.MatchString(s) {
			runtimeSet[rel] = struct{}{}
		}
		for _, m := range eventsOnRE.FindAllStringSubmatch(s, -1) {
			if len(m) >= 2 {
				eventsSet[m[1]] = struct{}{}
			}
		}
		return nil
	})

	return sortedKeys(bindSet), sortedKeys(runtimeSet), sortedKeys(eventsSet)
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

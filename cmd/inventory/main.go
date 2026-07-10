//go:build tools

// Command inventory scans the Silt codebase and emits a deterministic JSON
// inventory of the Wails IPC surface: exported *App methods, Go-side
// runtime.EventsEmit event names, frontend imports of the generated App
// bindings and the wails runtime, and frontend EventsOn subscriptions.
//
// The "tools" build tag keeps this binary out of the production build. Run with:
//
//	go run -tags tools ./cmd/inventory/
package main

import (
	"encoding/json"
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
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	methods, pluginCount := scanMethods(root)
	goEvents := scanGoEvents(root)
	bindImports, runtimeImports, feEvents := scanFrontend(filepath.Join(root, defaultFrontendSrc))

	inv := Inventory{
		Methods:                methods,
		GoEvents:               goEvents,
		FrontendBindingImports: bindImports,
		FrontendRuntimeImports: runtimeImports,
		FrontendEvents:         feEvents,
		PluginMethodCount:      pluginCount,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(inv); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr,
		"inventory: methods=%d (plugin=%d) go_events=%d frontend_binding_imports=%d frontend_runtime_imports=%d frontend_events=%d\n",
		len(methods), pluginCount, len(goEvents), len(bindImports), len(runtimeImports), len(feEvents),
	)
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

// scanGoEvents walks every .go file under root (skipping _test.go and the
// usual non-source directories) and collects the string-literal event names
// passed to runtime.EventsEmit.
func scanGoEvents(root string) []string {
	seen := map[string]struct{}{}
	fset := token.NewFileSet()

	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if path != root {
				switch filepath.Base(path) {
				case "vendor", "node_modules", ".git", "build":
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		file, err := parser.ParseFile(fset, path, src, parser.AllErrors)
		if err != nil {
			return nil // skip unparseable rather than aborting
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "EventsEmit" {
				return true
			}
			// Receiver ident should be "runtime"; we don't resolve imports
			// (would pull in go/types), so an ident-name check is enough.
			if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "runtime" {
				return true
			}
			// Wails signature is EventsEmit(ctx, eventName, optionalData...),
			// so the name is normally Args[1]. Scanning Args[1:] for the first
			// string literal tolerates unusual shapes per the spec ("first or
			// second argument after ctx") and silently skips calls where the
			// name is a variable (e.g. updateProgressEvent).
			for _, arg := range call.Args[1:] {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if name, err := strconv.Unquote(lit.Value); err == nil {
					seen[name] = struct{}{}
				}
				break
			}
			return true
		})
		return nil
	})

	return sortedKeys(seen)
}

var (
	// A string literal containing the respective wails path. Covers both the
	// extensionless and .js import forms used in the frontend.
	bindingPathRE = regexp.MustCompile(`['"\x60][^'"\x60]*wailsjs/go/main/App[^'"\x60]*['"\x60]`)
	runtimePathRE = regexp.MustCompile(`['"\x60][^'"\x60]*wailsjs/runtime/runtime[^'"\x60]*['"\x60]`)
	// eventsOnRE captures the event-name string literal handed to EventsOn.
	// \s* spans the newlines seen in this codebase's multi-line calls.
	eventsOnRE = regexp.MustCompile(`EventsOn\(\s*['"\x60]([^'"\x60]+)['"\x60]`)
)

// scanFrontend walks .ts/.svelte files under frontendRoot and records which
// files import the App bindings / wails runtime, plus the set of EventsOn
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

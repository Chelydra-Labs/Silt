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
	// this codebase's multi-line calls. Kept for legacy / test fixtures.
	eventsOnRE = regexp.MustCompile(`Events\.On\s*\(\s*['"\x60]([^'"\x60]+)['"\x60]`)
	// eventsOnEventNameRE matches Events.On(EventName.<Ident>...) — the
	// post-centralization form used by real subscriptions.
	eventsOnEventNameRE = regexp.MustCompile(`Events\.On\s*\(\s*EventName\.([A-Za-z0-9_]+)`)
	// eventsOnTemplateRE matches template compositions like
	// Events.On(`${EventName.X}:${pluginId}`) and records only the base
	// EventName value (not the dynamic suffix).
	eventsOnTemplateRE = regexp.MustCompile("Events\\.On\\s*\\(\\s*`\\$\\{EventName\\.([A-Za-z0-9_]+)\\}")
	// eventNameTemplateInterpRE matches `${EventName.X}` interpolations used to
	// build owner-scoped names before Events.On (e.g. AI stream:
	// const deltaEv = `${EventName.EventAICompleteDelta}:${pluginID}`).
	// Records the base wire string only.
	eventNameTemplateInterpRE = regexp.MustCompile(`\$\{EventName\.([A-Za-z0-9_]+)\}`)
	// eventNameConstBlockRE isolates the export const EventName = { ... } block
	// in frontend/src/generated/enums.ts.
	eventNameConstBlockRE = regexp.MustCompile(`export\s+const\s+EventName\s*=\s*\{([^}]*)\}`)
	// eventNameEntryRE extracts Key: 'value' pairs inside that block.
	eventNameEntryRE = regexp.MustCompile(`([A-Za-z0-9_]+)\s*:\s*'([^']*)'`)
	// Comment strippers applied to frontend source before the scans above run,
	// so prose or dead code in comments can't seed false positives (e.g. a
	// test's "// Events.On('menu:save')" mention).
	blockCommentRE = regexp.MustCompile(`(?s)/\*.*?\*/`)
	lineCommentRE  = regexp.MustCompile(`//.*`)
)

// stripComments removes JS/TS/Svelte comments from src so the frontend scans
// never harvest matches from commented-out code or prose. Block comments are
// stripped non-greedy across newlines; line comments run to end-of-line.
func stripComments(src string) string {
	src = blockCommentRE.ReplaceAllString(src, "")
	return lineCommentRE.ReplaceAllString(src, "")
}

// parseEventNameMap extracts Key → wire-string pairs from the generated
// enums.ts EventName const object:
//
//	export const EventName = {
//	  EventFoo: 'foo:bar',
//	  ...
//	} as const
//
// Returns an empty map if the block is missing or has no entries.
func parseEventNameMap(enumsTS string) map[string]string {
	out := map[string]string{}
	m := eventNameConstBlockRE.FindStringSubmatch(enumsTS)
	if len(m) < 2 {
		return out
	}
	for _, entry := range eventNameEntryRE.FindAllStringSubmatch(m[1], -1) {
		if len(entry) >= 3 {
			out[entry[1]] = entry[2]
		}
	}
	return out
}

// collectFrontendEvents harvests Events.On subscription names from stripped
// frontend source. Resolves EventName.<Ident> and template compositions
// `${EventName.X}:...` via nameMap; also keeps bare string literals.
// Unknown EventName members are skipped. Template forms record the base wire
// string only (not the dynamic suffix).
func collectFrontendEvents(stripped string, nameMap map[string]string, eventsSet map[string]struct{}) {
	for _, m := range eventsOnRE.FindAllStringSubmatch(stripped, -1) {
		if len(m) < 2 {
			continue
		}
		// Skip template literals (handled by eventsOnTemplateRE); bare
		// eventsOnRE would otherwise record the unexpanded `${...}` text.
		if strings.Contains(m[1], "${") {
			continue
		}
		eventsSet[m[1]] = struct{}{}
	}
	for _, m := range eventsOnEventNameRE.FindAllStringSubmatch(stripped, -1) {
		if len(m) < 2 {
			continue
		}
		if wire, ok := nameMap[m[1]]; ok {
			eventsSet[wire] = struct{}{}
		}
	}
	for _, m := range eventsOnTemplateRE.FindAllStringSubmatch(stripped, -1) {
		if len(m) < 2 {
			continue
		}
		if wire, ok := nameMap[m[1]]; ok {
			eventsSet[wire] = struct{}{}
		}
	}
	// Standalone template interpolations (assigned to locals then passed to
	// Events.On) — same base-only recording as eventsOnTemplateRE.
	for _, m := range eventNameTemplateInterpRE.FindAllStringSubmatch(stripped, -1) {
		if len(m) < 2 {
			continue
		}
		if wire, ok := nameMap[m[1]]; ok {
			eventsSet[wire] = struct{}{}
		}
	}
}

// scanFrontend walks .ts/.svelte files under frontendRoot and records which
// files import the App bindings / @wailsio/runtime, plus the set of Events.On
// subscriptions. Paths are repo-relative with forward slashes. Comments are
// stripped (see stripComments) before matching, so prose mentions like a
// test's "// Events.On('menu:save')" can't seed false positives.
//
// frontend_events resolves four forms after comment strip:
//  1. Events.On('literal') — legacy / tests
//  2. Events.On(EventName.<Ident>) — post-centralization const member
//  3. Events.On(`${EventName.<Ident>}:...`) — inline template composition
//  4. `${EventName.<Ident>}` interpolations used to build names before
//     Events.On (AI stream owner-scoped events) — base wire string only
//
// EventName keys are loaded from frontend/src/generated/enums.ts (sibling of
// frontendRoot's parent when frontendRoot is frontend/src). The canonical
// event surface for the IPC gate remains go_events (events.go const block);
// frontend_events is informational subscription coverage.
func scanFrontend(frontendRoot string) (bindingImports, runtimeImports, frontendEvents []string) {
	bindSet := map[string]struct{}{}
	runtimeSet := map[string]struct{}{}
	eventsSet := map[string]struct{}{}

	// enums.ts lives at frontend/src/generated/enums.ts; frontendRoot is
	// typically <repo>/frontend/src.
	enumsPath := filepath.Join(frontendRoot, "generated", "enums.ts")
	nameMap := map[string]string{}
	if enumsSrc, err := os.ReadFile(enumsPath); err == nil {
		nameMap = parseEventNameMap(string(enumsSrc))
	}

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

		s := stripComments(string(src))
		if bindingPathRE.MatchString(s) {
			bindSet[rel] = struct{}{}
		}
		if runtimePathRE.MatchString(s) {
			runtimeSet[rel] = struct{}{}
		}
		collectFrontendEvents(s, nameMap, eventsSet)
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

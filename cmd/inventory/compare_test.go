//go:build tools

package main

import (
	"strings"
	"testing"
)

func inv(methods []MethodInfo, pluginCount int) Inventory {
	cp := make([]MethodInfo, len(methods))
	copy(cp, methods)
	return Inventory{Methods: cp, PluginMethodCount: pluginCount}
}

func m(name, sig string) MethodInfo { return MethodInfo{Name: name, Signature: sig} }

func TestCompareInventories_IdenticalPasses(t *testing.T) {
	want := inv([]MethodInfo{m("Foo", "Foo() error"), m("Bar", "Bar(x int) string")}, 0)
	got := inv([]MethodInfo{m("Bar", "Bar(x int) string"), m("Foo", "Foo() error")}, 0)

	d := compareInventories(want, got)
	if !d.MethodsEqual() {
		t.Fatalf("expected MethodsEqual, got missing=%v extra=%v changed=%v",
			d.MissingMethods, d.ExtraMethods, d.ChangedMethods)
	}
	if d.WantMethodCount != 2 || d.GotMethodCount != 2 {
		t.Errorf("counts: want=%d got=%d", d.WantMethodCount, d.GotMethodCount)
	}
	if out := renderDiff(d); !strings.Contains(out, "PASSED") {
		t.Errorf("renderDiff on clean diff should report PASSED, got: %s", out)
	}
}

func TestCompareInventories_ExtraMethodDetected(t *testing.T) {
	// Mirrors the real GetCloseToTray addition: a method appears on the live
	// side that is not in the fixture. The gate must flag it so the addition
	// forces a deliberate, review-visible fixture regeneration. (The live
	// signature is GetCloseToTray() bool, no error.)
	want := inv([]MethodInfo{m("SetCloseToTray", "SetCloseToTray(enabled bool) error")}, 0)
	got := inv([]MethodInfo{
		m("SetCloseToTray", "SetCloseToTray(enabled bool) error"),
		m("GetCloseToTray", "GetCloseToTray() bool"),
	}, 0)

	d := compareInventories(want, got)
	if d.MethodsEqual() {
		t.Fatal("expected ExtraMethods, gate passed")
	}
	if len(d.ExtraMethods) != 1 || d.ExtraMethods[0].Name != "GetCloseToTray" {
		t.Fatalf("expected GetCloseToTray extra, got %v", d.ExtraMethods)
	}
	if d.ExtraMethods[0].Got != "GetCloseToTray() bool" {
		t.Errorf("extra Got signature = %q", d.ExtraMethods[0].Got)
	}
	if d.GotMethodCount != 2 || d.WantMethodCount != 1 {
		t.Errorf("counts wrong: want=%d got=%d", d.WantMethodCount, d.GotMethodCount)
	}
	out := renderDiff(d)
	if !strings.Contains(out, "extra:") || !strings.Contains(out, "GetCloseToTray") {
		t.Errorf("renderDiff should surface the extra method, got: %s", out)
	}
}

func TestCompareInventories_MissingMethodDetected(t *testing.T) {
	// A binding removed from the Go side without regenerating the fixture.
	want := inv([]MethodInfo{m("Foo", "Foo() error"), m("Bar", "Bar() error")}, 0)
	got := inv([]MethodInfo{m("Foo", "Foo() error")}, 0)

	d := compareInventories(want, got)
	if d.MethodsEqual() {
		t.Fatal("expected MissingMethods, gate passed")
	}
	if len(d.MissingMethods) != 1 || d.MissingMethods[0].Name != "Bar" {
		t.Fatalf("expected Bar missing, got %v", d.MissingMethods)
	}
	if d.MissingMethods[0].Want != "Bar() error" {
		t.Errorf("missing Want signature = %q", d.MissingMethods[0].Want)
	}
}

func TestCompareInventories_ChangedSignatureDetected(t *testing.T) {
	// Same method name, different params/results — the most subtle drift
	// (the old >=180 count gate would have missed this entirely).
	want := inv([]MethodInfo{m("Save", "Save(path string) error")}, 0)
	got := inv([]MethodInfo{m("Save", "Save(path string, opts SaveOpts) error")}, 0)

	d := compareInventories(want, got)
	if d.MethodsEqual() {
		t.Fatal("expected ChangedMethods, gate passed")
	}
	if len(d.ChangedMethods) != 1 || d.ChangedMethods[0].Name != "Save" {
		t.Fatalf("expected Save changed, got %v", d.ChangedMethods)
	}
	if d.ChangedMethods[0].Want != "Save(path string) error" {
		t.Errorf("changed Want = %q", d.ChangedMethods[0].Want)
	}
	if d.ChangedMethods[0].Got != "Save(path string, opts SaveOpts) error" {
		t.Errorf("changed Got = %q", d.ChangedMethods[0].Got)
	}
	out := renderDiff(d)
	if !strings.Contains(out, "changed:") || !strings.Contains(out, "SaveOpts") {
		t.Errorf("renderDiff should show the before/after signature, got: %s", out)
	}
}

func TestCompareInventories_SectionDriftReportedButDoesNotFailGate(t *testing.T) {
	// Non-method sections (events, frontend imports) churn on routine
	// feature work. They are reported for review visibility but must NOT
	// fail the method-signature gate.
	want := Inventory{
		Methods:                []MethodInfo{m("Foo", "Foo() error")},
		GoEvents:               []string{"vault:closed"},
		FrontendEvents:         []string{"navigate"},
		FrontendBindingImports: []string{"frontend/src/a.ts"},
	}
	got := Inventory{
		Methods:                []MethodInfo{m("Foo", "Foo() error")},
		GoEvents:               []string{"vault:closed", "vault:opened"},
		FrontendEvents:         []string{"navigate"},
		FrontendBindingImports: []string{"frontend/src/a.ts", "frontend/src/b.ts"},
	}

	d := compareInventories(want, got)
	if !d.MethodsEqual() {
		t.Fatalf("section drift must not fail the method gate: %v", d.ChangedMethods)
	}
	if len(d.GoEventsAdded) != 1 || d.GoEventsAdded[0] != "vault:opened" {
		t.Errorf("GoEventsAdded = %v", d.GoEventsAdded)
	}
	if len(d.BindingImportsAdded) != 1 || d.BindingImportsAdded[0] != "frontend/src/b.ts" {
		t.Errorf("BindingImportsAdded = %v", d.BindingImportsAdded)
	}
	out := renderDiff(d)
	if !strings.Contains(out, "informational") {
		t.Errorf("expected informational drift block, got: %s", out)
	}
}

func TestSetDiff(t *testing.T) {
	added, removed := setDiff([]string{"a", "b", "c"}, []string{"b", "c", "d"})
	if len(added) != 1 || added[0] != "d" {
		t.Errorf("added = %v, want [d]", added)
	}
	if len(removed) != 1 || removed[0] != "a" {
		t.Errorf("removed = %v, want [a]", removed)
	}

	// No drift.
	added, removed = setDiff([]string{"a"}, []string{"a"})
	if len(added) != 0 || len(removed) != 0 {
		t.Errorf("expected no drift, got added=%v removed=%v", added, removed)
	}
}

// TestStripComments_NoCommentFalsePositive pins the menu:save fix: a literal
// Events.On call that lives only in a comment must not be harvested, while a
// real literal call on the next line still is. Mirrors the exact prose in
// App.menu-save.test.ts that previously seeded the false positive.
func TestStripComments_NoCommentFalsePositive(t *testing.T) {
	src := "// onMount registers the Events.On('menu:save') handler; let it flush.\n" +
		"Events.On('real:thing', () => {})\n"
	stripped := stripComments(src)
	matches := eventsOnRE.FindAllStringSubmatch(stripped, -1)
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 match (the real call) after stripping, got %d: %v", len(matches), matches)
	}
	if matches[0][1] != "real:thing" {
		t.Errorf("expected 'real:thing', got %q", matches[0][1])
	}
}

// TestStripComments_BlockComment covers the /* ... */ form, including a
// multi-line block (the (?s) non-greedy case).
func TestStripComments_BlockComment(t *testing.T) {
	src := "/* Events.On('blocked:one') */\n" +
		"/*\n  Events.On('blocked:two')\n*/\n" +
		"Events.On('kept', () => {})\n"
	stripped := stripComments(src)
	matches := eventsOnRE.FindAllStringSubmatch(stripped, -1)
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 match (the real call), got %d: %v", len(matches), matches)
	}
	if matches[0][1] != "kept" {
		t.Errorf("expected 'kept', got %q", matches[0][1])
	}
}

func TestParseEventNameMap(t *testing.T) {
	enums := `
export const EventName = {
  EventMenuSave: 'menu:save',
  EventBlockChanged: 'block:changed',
  EventAICompleteDelta: 'ai:complete:delta'
} as const
`
	got := parseEventNameMap(enums)
	want := map[string]string{
		"EventMenuSave":        "menu:save",
		"EventBlockChanged":    "block:changed",
		"EventAICompleteDelta": "ai:complete:delta",
	}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("got[%q]=%q want %q", k, got[k], v)
		}
	}
	if len(parseEventNameMap("export const Other = { X: 'y' } as const")) != 0 {
		t.Error("expected empty map when EventName block missing")
	}
}

func TestCollectFrontendEvents_ConstMemberAndTemplate(t *testing.T) {
	nameMap := map[string]string{
		"EventMenuSave":        "menu:save",
		"EventAICompleteDelta": "ai:complete:delta",
		"EventBlockChanged":    "block:changed",
	}
	src := `
Events.On(EventName.EventMenuSave, () => {})
Events.On(` + "`${EventName.EventAICompleteDelta}:${pluginId}`" + `, handler)
Events.On('legacy:literal', () => {})
// Events.On(EventName.EventBlockChanged, dead)
Events.On(EventName.UnknownMember, () => {})
const deltaEv = ` + "`${EventName.EventAICompleteDelta}:${pluginID}`" + `
Events.On(deltaEv, handler)
`
	eventsSet := map[string]struct{}{}
	collectFrontendEvents(stripComments(src), nameMap, eventsSet)
	got := sortedKeys(eventsSet)
	want := []string{"ai:complete:delta", "legacy:literal", "menu:save"}
	if len(got) != len(want) {
		t.Fatalf("events=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("events[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestCollectFrontendEvents_AllowlistArrayAndVarCall(t *testing.T) {
	nameMap := map[string]string{
		"EventFoo": "foo",
		"EventBar": "bar", // declared in an unreferenced array → must NOT appear
		"EventBaz": "baz",
	}
	// Synthetic isolated case (issue #778 / F4): each allowlist member is
	// reachable ONLY through its array, not via any other resolvable form, so
	// this exercises the allowlist pass directly. In the real codebase both
	// current allowlist members are already covered by direct const-member
	// subscriptions in other files, so this isolation is what proves the pass
	// finds members on its own. hostEvents is consumed via a `.includes(ev)`
	// guard (the real plugins/events.ts shape); directEvents is passed straight
	// to Events.On (covers the other branch of step C). `mixed` has a
	// non-EventName element → skipped entirely (conservative: no partial emit).
	// `unreferenced` is a clean all-EventName.* array but never referenced by
	// any Events.On(ident) or .includes(param) → its member must NOT leak
	// (pins the "emit only on reference" contract).
	src := `
const hostEvents: PluginEventName[] = [EventName.EventFoo]
const directEvents: PluginEventName[] = [EventName.EventBaz]
const unreferenced: PluginEventName[] = [EventName.EventBar]
const mixed: PluginEventName[] = [EventName.EventFoo, "literal"]
if (hostEvents.includes(ev)) {
  Events.On(ev, () => {})
}
Events.On(directEvents, () => {})
`
	eventsSet := map[string]struct{}{}
	collectFrontendEvents(stripComments(src), nameMap, eventsSet)
	got := sortedKeys(eventsSet)
	want := []string{"baz", "foo"}
	if len(got) != len(want) {
		t.Fatalf("events=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("events[%d]=%q want %q", i, got[i], want[i])
		}
	}
	// Negative assertion: a declared-but-unreferenced allowlist must not emit.
	for _, w := range got {
		if w == "bar" {
			t.Errorf("unreferenced allowlist leaked `bar`: events=%v", got)
		}
	}
}

// TestCollectFrontendEvents_AllowlistDeclVariants pins #784 broader decl
// shapes: let/var, readonly T[], ReadonlyArray<T>, and trailing `as const`.
func TestCollectFrontendEvents_AllowlistDeclVariants(t *testing.T) {
	nameMap := map[string]string{
		"EventA": "a",
		"EventB": "b",
		"EventC": "c",
		"EventD": "d",
	}
	src := `
let letEvents: readonly PluginEventName[] = [EventName.EventA]
var varEvents: ReadonlyArray<PluginEventName> = [EventName.EventB]
const asConstEvents = [EventName.EventC] as const
const plain: PluginEventName[] = [EventName.EventD]
Events.On(letEvents, () => {})
Events.On(varEvents, () => {})
Events.On(asConstEvents, () => {})
Events.On(plain, () => {})
`
	eventsSet := map[string]struct{}{}
	collectFrontendEvents(stripComments(src), nameMap, eventsSet)
	got := sortedKeys(eventsSet)
	want := []string{"a", "b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("events=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("events[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

// TestCollectFrontendEvents_IncludesScopeAware pins #784: .includes binding is
// not file-global. Unrelated arrays, cross-function guards, and unreferenced
// allowlists must not leak; same-scope hostEvents+includes still resolves.
func TestCollectFrontendEvents_IncludesScopeAware(t *testing.T) {
	nameMap := map[string]string{
		"EventFoo":  "foo",
		"EventBar":  "bar",
		"EventBaz":  "baz",
		"EventQuux": "quux",
	}
	src := `
const hostEvents: PluginEventName[] = [EventName.EventFoo]
const other: PluginEventName[] = [EventName.EventBar]
const unreferenced: PluginEventName[] = [EventName.EventQuux]
const directEvents: PluginEventName[] = [EventName.EventBaz]

function subscribeHost(ev) {
  if (hostEvents.includes(ev)) {
    Events.On(ev, () => {})
  }
}

function unrelatedIncludes(ev) {
  if (other.includes(ev)) {
    // not an Events.On — must not bind other → ev for other functions
  }
}

function otherFn(ev) {
  // Guard lives in subscribeHost / unrelatedIncludes; must NOT resolve here.
  Events.On(ev, () => {})
}

Events.On(directEvents, () => {})
`
	eventsSet := map[string]struct{}{}
	collectFrontendEvents(stripComments(src), nameMap, eventsSet)
	got := sortedKeys(eventsSet)
	want := []string{"baz", "foo"}
	if len(got) != len(want) {
		t.Fatalf("events=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("events[%d]=%q want %q", i, got[i], want[i])
		}
	}
	for _, leak := range []string{"bar", "quux"} {
		for _, w := range got {
			if w == leak {
				t.Errorf("leaked %q: events=%v", leak, got)
			}
		}
	}
}

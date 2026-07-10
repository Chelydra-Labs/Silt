//go:build tools

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// fixtureSchema is stamped into every generated fixture so a schema-level
// change to this file is itself review-visible in the checked-in artifact.
// Bump the trailing integer only when the fixture's shape or the gating
// semantics change in a way that invalidates older fixtures.
const fixtureSchema = "current-approved-v3"

// Fixture is the on-disk artifact checked in at
// cmd/inventory/current-approved-v3.json. The wrapper carries the schema
// version and the exact regeneration command so a reviewer opening the file
// sees, in-band, how it was produced and how to refresh it. The Inventory
// payload is the full IPC surface snapshot.
type Fixture struct {
	Schema      string    `json:"schema"`
	RegenCmd    string    `json:"regen_cmd"`
	Description string    `json:"description"`
	Inventory   Inventory `json:"inventory"`
}

const fixtureDescription = "Approved Wails v3 IPC surface snapshot. The CI parity gate fails on any missing/extra/changed *App method signature versus this file. Other sections (events, frontend imports) are tracked for review visibility and refreshed whenever the fixture is regenerated."

// MethodDiff is a single method whose signature drifted between the fixture
// and the live surface (same name, different params/results).
type MethodDiff struct {
	Name string `json:"name"`
	Want string `json:"want"` // fixture signature
	Got  string `json:"got"`  // live signature
}

// Diff is the result of comparing the approved fixture (want) against the
// live inventory (got). The method fields are the CI gate — any non-empty
// method field fails the parity check. The section-drift fields are
// informational: they are reported so surface churn is review-visible in CI
// logs, but they do not by themselves fail the gate (frontend imports churn
// on every new component, which is expected and not a contract break).
type Diff struct {
	MissingMethods []MethodDiff `json:"missing_methods"` // in fixture, gone from live (a removed binding)
	ExtraMethods   []MethodDiff `json:"extra_methods"`   // in live, not in fixture (a new binding)
	ChangedMethods []MethodDiff `json:"changed_methods"` // same name, signature changed

	GoEventsAdded         []string `json:"go_events_added"`
	GoEventsRemoved       []string `json:"go_events_removed"`
	FrontendEventsAdded   []string `json:"frontend_events_added"`
	FrontendEventsRemoved []string `json:"frontend_events_removed"`
	BindingImportsAdded   []string `json:"binding_imports_added"`
	BindingImportsRemoved []string `json:"binding_imports_removed"`
	RuntimeImportsAdded   []string `json:"runtime_imports_added"`
	RuntimeImportsRemoved []string `json:"runtime_imports_removed"`
	PluginMethodCountWant int      `json:"plugin_method_count_want"`
	PluginMethodCountGot  int      `json:"plugin_method_count_got"`
	WantMethodCount       int      `json:"want_method_count"`
	GotMethodCount        int      `json:"got_method_count"`
}

// MethodsEqual reports whether the method-signature gate passes: no missing,
// no extra, no changed methods. This is the single condition CI fails on.
func (d Diff) MethodsEqual() bool {
	return len(d.MissingMethods) == 0 && len(d.ExtraMethods) == 0 && len(d.ChangedMethods) == 0
}

// compareInventories diffs the approved fixture inventory (want) against the
// live inventory (got). Methods are compared by name → signature; everything
// else by set membership. The comparison is pure (no I/O), which keeps it
// unit-testable without touching the filesystem.
func compareInventories(want, got Inventory) Diff {
	d := Diff{
		WantMethodCount:       len(want.Methods),
		GotMethodCount:        len(got.Methods),
		PluginMethodCountWant: want.PluginMethodCount,
		PluginMethodCountGot:  got.PluginMethodCount,
	}

	wantBy := map[string]string{}
	for _, m := range want.Methods {
		wantBy[m.Name] = m.Signature
	}
	gotBy := map[string]string{}
	for _, m := range got.Methods {
		gotBy[m.Name] = m.Signature
	}
	for name, sig := range wantBy {
		gotSig, ok := gotBy[name]
		if !ok {
			d.MissingMethods = append(d.MissingMethods, MethodDiff{Name: name, Want: sig})
			continue
		}
		if gotSig != sig {
			d.ChangedMethods = append(d.ChangedMethods, MethodDiff{Name: name, Want: sig, Got: gotSig})
		}
	}
	for name, sig := range gotBy {
		if _, ok := wantBy[name]; !ok {
			d.ExtraMethods = append(d.ExtraMethods, MethodDiff{Name: name, Got: sig})
		}
	}

	sortMethodDiffs(d.MissingMethods)
	sortMethodDiffs(d.ExtraMethods)
	sortMethodDiffs(d.ChangedMethods)

	d.GoEventsAdded, d.GoEventsRemoved = setDiff(want.GoEvents, got.GoEvents)
	d.FrontendEventsAdded, d.FrontendEventsRemoved = setDiff(want.FrontendEvents, got.FrontendEvents)
	d.BindingImportsAdded, d.BindingImportsRemoved = setDiff(want.FrontendBindingImports, got.FrontendBindingImports)
	d.RuntimeImportsAdded, d.RuntimeImportsRemoved = setDiff(want.FrontendRuntimeImports, got.FrontendRuntimeImports)
	return d
}

func sortMethodDiffs(s []MethodDiff) {
	sort.Slice(s, func(i, j int) bool { return s[i].Name < s[j].Name })
}

// setDiff returns (added, removed): added = in got but not want, removed = in
// want but not got. Both slices are sorted. Inputs are assumed already sorted
// (the scanner guarantees this), but the diff is computed from sets so order
// doesn't affect correctness — only output stability.
func setDiff(want, got []string) (added, removed []string) {
	ws := map[string]struct{}{}
	for _, v := range want {
		ws[v] = struct{}{}
	}
	gs := map[string]struct{}{}
	for _, v := range got {
		gs[v] = struct{}{}
	}
	for v := range gs {
		if _, ok := ws[v]; !ok {
			added = append(added, v)
		}
	}
	for v := range ws {
		if _, ok := gs[v]; !ok {
			removed = append(removed, v)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// renderDiff produces the human-readable comparison report. Method drift is
// printed with a leading "FAIL:" so it stands out in CI logs; section drift
// is printed as an informational "drift:" block.
func renderDiff(d Diff) string {
	if d.MethodsEqual() && noSectionDrift(d) {
		return fmt.Sprintf("inventory: method-signature gate PASSED (%d methods, plugin=%d); no section drift.\n",
			d.GotMethodCount, d.PluginMethodCountGot)
	}
	var b strings.Builder
	if !d.MethodsEqual() {
		fmt.Fprintf(&b, "FAIL: method-signature drift detected (the IPC contract changed).\n")
		for _, m := range d.MissingMethods {
			fmt.Fprintf(&b, "  - missing:   %s   (was: %s)\n", m.Name, m.Want)
		}
		for _, m := range d.ExtraMethods {
			fmt.Fprintf(&b, "  + extra:     %s   (now: %s)\n", m.Name, m.Got)
		}
		for _, m := range d.ChangedMethods {
			fmt.Fprintf(&b, "  ~ changed:   %s\n      was: %s\n      now: %s\n", m.Name, m.Want, m.Got)
		}
	}
	renderSectionDrift(&b, d)
	return b.String()
}

func noSectionDrift(d Diff) bool {
	return len(d.GoEventsAdded)+len(d.GoEventsRemoved)+
		len(d.FrontendEventsAdded)+len(d.FrontendEventsRemoved)+
		len(d.BindingImportsAdded)+len(d.BindingImportsRemoved)+
		len(d.RuntimeImportsAdded)+len(d.RuntimeImportsRemoved) == 0
}

func renderSectionDrift(b *strings.Builder, d Diff) {
	type sect struct {
		name    string
		added   []string
		removed []string
	}
	sects := []sect{
		{"go_events", d.GoEventsAdded, d.GoEventsRemoved},
		{"frontend_events", d.FrontendEventsAdded, d.FrontendEventsRemoved},
		{"frontend_binding_imports", d.BindingImportsAdded, d.BindingImportsRemoved},
		{"frontend_runtime_imports", d.RuntimeImportsAdded, d.RuntimeImportsRemoved},
	}
	any := false
	for _, s := range sects {
		if len(s.added)+len(s.removed) == 0 {
			continue
		}
		if !any {
			fmt.Fprintf(b, "drift (informational, does not fail the gate):\n")
			any = true
		}
		fmt.Fprintf(b, "  %s: +%d -%d\n", s.name, len(s.added), len(s.removed))
		for _, v := range s.removed {
			fmt.Fprintf(b, "    - %s\n", v)
		}
		for _, v := range s.added {
			fmt.Fprintf(b, "    + %s\n", v)
		}
	}
}

// readFixture loads and decodes the approved-surface fixture. The fixture's
// schema is checked against fixtureSchema so a stale fixture (from before a
// tooling change that altered the gating semantics) fails loudly rather than
// silently passing against a differently-shaped artifact.
func readFixture(path string) (Inventory, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Inventory{}, fmt.Errorf("read fixture %s: %w", path, err)
	}
	var f Fixture
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return Inventory{}, fmt.Errorf("decode fixture %s: %w", path, err)
	}
	if f.Schema != fixtureSchema {
		return Inventory{}, fmt.Errorf("fixture schema %q != expected %q (regenerate: go run -tags tools ./cmd/inventory/ -update %s)", f.Schema, fixtureSchema, path)
	}
	return f.Inventory, nil
}

// writeFixture serializes the live inventory into the on-disk fixture format,
// stamping the schema and regeneration command so the file is self-describing.
func writeFixture(path string, inv Inventory) error {
	f := Fixture{
		Schema:      fixtureSchema,
		RegenCmd:    fmt.Sprintf("go run -tags tools ./cmd/inventory/ -update %s", path),
		Description: fixtureDescription,
		Inventory:   inv,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(f); err != nil {
		return fmt.Errorf("encode fixture: %w", err)
	}
	// json.Encoder appends a trailing newline; trim it so the file ends with
	// a single newline (consistent with gofmt'd JSON), then add one back.
	out := bytes.TrimRight(buf.Bytes(), "\n")
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write fixture %s: %w", path, err)
	}
	return nil
}

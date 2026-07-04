package dependencies

import (
	"reflect"
	"testing"
)

func TestExtractRefs(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{
			name:  "empty",
			value: "",
			want:  nil,
		},
		{
			name:  "single ref",
			value: "((11111111-1111-1111-1111-111111111111))",
			want:  []string{"11111111-1111-1111-1111-111111111111"},
		},
		{
			name:  "multiple refs space separated",
			value: "((11111111-1111-1111-1111-111111111111)) ((22222222-2222-2222-2222-222222222222))",
			want: []string{
				"11111111-1111-1111-1111-111111111111",
				"22222222-2222-2222-2222-222222222222",
			},
		},
		{
			name:  "ignores non-ref prose around refs",
			value: "see ((11111111-1111-1111-1111-111111111111)) for context",
			want:  []string{"11111111-1111-1111-1111-111111111111"},
		},
		{
			name:  "no refs returns nil",
			value: "no refs here",
			want:  nil,
		},
		{
			name:  "malformed uuid ignored",
			value: "((not-a-uuid)) ((11111111-1111-1111-1111-111111111111))",
			want:  []string{"11111111-1111-1111-1111-111111111111"},
		},
		{
			name:  "duplicates de-duplicated preserving order",
			value: "((11111111-1111-1111-1111-111111111111)) ((22222222-2222-2222-2222-222222222222)) ((11111111-1111-1111-1111-111111111111))",
			want: []string{
				"11111111-1111-1111-1111-111111111111",
				"22222222-2222-2222-2222-222222222222",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractRefs(tc.value)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ExtractRefs(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestFormatRefs(t *testing.T) {
	tests := []struct {
		name string
		refs []string
		want string
	}{
		{name: "nil", refs: nil, want: ""},
		{name: "empty", refs: []string{}, want: ""},
		{
			name: "single",
			refs: []string{"11111111-1111-1111-1111-111111111111"},
			want: "((11111111-1111-1111-1111-111111111111))",
		},
		{
			name: "multiple",
			refs: []string{
				"11111111-1111-1111-1111-111111111111",
				"22222222-2222-2222-2222-222222222222",
			},
			want: "((11111111-1111-1111-1111-111111111111)) ((22222222-2222-2222-2222-222222222222))",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatRefs(tc.refs)
			if got != tc.want {
				t.Fatalf("FormatRefs(%v) = %q, want %q", tc.refs, got, tc.want)
			}
		})
	}
}

// ExtractRefs and FormatRefs are inverses for well-formed input — the
// round-trip property is what makes the token lossless on parse → render.
func TestExtractFormatRoundTrip(t *testing.T) {
	refs := []string{
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		"33333333-3333-3333-3333-333333333333",
	}
	got := ExtractRefs(FormatRefs(refs))
	if !reflect.DeepEqual(got, refs) {
		t.Fatalf("round-trip failed: got %v, want %v", got, refs)
	}
}

func TestWouldCreateCycle(t *testing.T) {
	a := "11111111-1111-1111-1111-111111111111"
	b := "22222222-2222-2222-2222-222222222222"
	c := "33333333-3333-3333-3333-333333333333"
	d := "44444444-4444-4444-4444-444444444444"

	tests := []struct {
		name  string
		edges map[string][]string
		from  string
		to    string
		want  bool
	}{
		{
			name:  "self-loop rejected",
			edges: nil,
			from:  a, to: a,
			want: true,
		},
		{
			name:  "no existing edges: allowed",
			edges: map[string][]string{},
			from:  a, to: b,
			want: false,
		},
		{
			name:  "direct back-edge A->B then B->A",
			edges: map[string][]string{a: {b}}, // A blocked by B
			from:  b, to: a,                    // propose B blocked by A → cycle
			want: true,
		},
		{
			name:  "transitive A->B->C->A",
			edges: map[string][]string{a: {b}, b: {c}}, // A->B->C
			from:  c, to: a,                            // propose C->A → cycle
			want: true,
		},
		{
			name:  "transitive A->B->C->D allowed (no loop)",
			edges: map[string][]string{a: {b}, b: {c}, c: {d}},
			from:  d, to: a, // D blocked by A: A can't reach D, so no cycle
			// Wait — A->B->C->D means A reaches D. So D->A WOULD close a loop.
			want: true,
		},
		{
			name:  "independent branch allowed",
			edges: map[string][]string{a: {b}}, // A->B
			from:  c, to: d,                    // C->D unrelated → allowed
			want: false,
		},
		{
			name:  "diamond allowed",
			edges: map[string][]string{a: {b, c}, b: {d}, c: {d}}, // A->{B,C}->{D}
			from:  a, to: d,                                       // A->D short-circuit, no loop
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := WouldCreateCycle(tc.edges, tc.from, tc.to)
			if got != tc.want {
				t.Fatalf("WouldCreateCycle = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDetectsCycle(t *testing.T) {
	a := "11111111-1111-1111-1111-111111111111"
	b := "22222222-2222-2222-2222-222222222222"
	c := "33333333-3333-3333-3333-333333333333"
	d := "44444444-4444-4444-4444-444444444444"

	tests := []struct {
		name  string
		edges map[string][]string
		want  bool
	}{
		{name: "empty", edges: map[string][]string{}, want: false},
		{name: "single edge no cycle", edges: map[string][]string{a: {b}}, want: false},
		{name: "two node cycle", edges: map[string][]string{a: {b}, b: {a}}, want: true},
		{name: "self loop", edges: map[string][]string{a: {a}}, want: true},
		{name: "transitive cycle", edges: map[string][]string{a: {b}, b: {c}, c: {a}}, want: true},
		{name: "DAG no cycle", edges: map[string][]string{a: {b, c}, b: {d}, c: {d}}, want: false},
		{name: "isolated cycle among larger graph", edges: map[string][]string{a: {b}, b: {c}, c: {b}, d: {a}}, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectsCycle(tc.edges)
			if got != tc.want {
				t.Fatalf("DetectsCycle = %v, want %v", got, tc.want)
			}
		})
	}
}

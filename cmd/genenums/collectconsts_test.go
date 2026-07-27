//go:build tools

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// parseConsts parses src (a small Go file fragment) and returns every
// top-level const GenDecl, mirroring what generate() hands to collectConsts.
func parseConsts(t *testing.T, src string) []*ast.GenDecl {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var blocks []*ast.GenDecl
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		blocks = append(blocks, gd)
	}
	return blocks
}

func TestCollectConsts(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		goType  string
		want    []constEntry
		wantErr string // substring; empty means no error expected
	}{
		{
			name: "single-name string spec",
			src: `package p

type Foo string

const (
	A Foo = "a"
	B Foo = "b"
)`,
			goType: "Foo",
			want: []constEntry{
				{Name: "A", Value: "a"},
				{Name: "B", Value: "b"},
			},
		},
		{
			// Parallel Names/Values must be paired: both A→"x" and B→"y".
			name: "multi-name pairwise spec",
			src: `package p

type Foo string

const (
	A, B Foo = "x", "y"
)`,
			goType: "Foo",
			want: []constEntry{
				{Name: "A", Value: "x"},
				{Name: "B", Value: "y"},
			},
		},
		{
			// A ValueSpec that omits Type inherits the previous spec's type
			// within the same block, so B and C are still Foo.
			name: "inherited type across specs in one block",
			src: `package p

type Foo string

const (
	A Foo = "a"
	B     = "b"
	C     = "c"
)`,
			goType: "Foo",
			want: []constEntry{
				{Name: "A", Value: "a"},
				{Name: "B", Value: "b"},
				{Name: "C", Value: "c"},
			},
		},
		{
			// A string-typed enum with a non-string-literal value (iota,
			// const alias, …) cannot be faithfully emitted → hard error.
			name: "non-string-literal value returns error",
			src: `package p

type Foo string

const (
	A Foo = iota
)`,
			goType:  "Foo",
			wantErr: "A",
		},
		{
			// Type-only spec (no value list) is the iota-inheritance form;
			// it has nothing to emit and must NOT error.
			name: "type-only spec with no value is skipped",
			src: `package p

type Foo string

const (
	A Foo = "a"
	B Foo
	C     = "c"
)`,
			goType: "Foo",
			want: []constEntry{
				{Name: "A", Value: "a"},
				{Name: "C", Value: "c"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := parseConsts(t, tt.src)
			got, err := collectConsts(blocks, tt.goType)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (entries=%+v)", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d entries, want %d (%+v)", len(got), len(tt.want), got)
			}
			for i, e := range got {
				if e != tt.want[i] {
					t.Errorf("entry %d: got %+v, want %+v", i, e, tt.want[i])
				}
			}
		})
	}
}

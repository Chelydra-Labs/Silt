package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestRenameTargetsFromMarkdownUnder(t *testing.T) {
	root := filepath.Join("vault", "Work", "Projects")
	nb := "Work"
	prefix := "Projects"

	cases := []struct {
		name   string
		paths  []string
		prefix string
		root   string
		want   []renameTarget
	}{
		{
			name:   "nested section pages",
			root:   root,
			prefix: prefix,
			paths: []string{
				filepath.Join(root, "Active", "Site.md"),
				filepath.Join(root, "Notes.md"),
			},
			want: []renameTarget{
				{nb, "Projects/Active", "Site"},
				{nb, "Projects", "Notes"},
			},
		},
		{
			name:   "skips non-md",
			root:   root,
			prefix: prefix,
			paths: []string{
				filepath.Join(root, "Notes.md"),
				filepath.Join(root, "image.png"),
			},
			want: []renameTarget{{nb, "Projects", "Notes"}},
		},
		{
			name:   "skips paths outside root",
			root:   root,
			prefix: prefix,
			paths: []string{
				filepath.Join("vault", "Other", "X.md"),
				filepath.Join(root, "In.md"),
			},
			want: []renameTarget{{nb, "Projects", "In"}},
		},
		{
			name:   "empty sectionPrefix notebook root",
			root:   filepath.Join("vault", "Work"),
			prefix: "",
			paths: []string{
				filepath.Join("vault", "Work", "RootPage.md"),
				filepath.Join("vault", "Work", "Sec", "Nested.md"),
			},
			want: []renameTarget{
				{nb, "", "RootPage"},
				{nb, "Sec", "Nested"},
			},
		},
		{
			name:   "empty lock set",
			root:   root,
			prefix: prefix,
			paths:  nil,
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renameTargetsFromMarkdownUnder(tc.root, nb, tc.prefix, tc.paths)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

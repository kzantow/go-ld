package ld_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_decode(t *testing.T) {
	tests := []struct {
		name  string
		graph l
		want  func() a
	}{
		{
			graph: l{
				o{"@type": "software_Package", "@id": "pkg-1", "name": "pkg 1"},
				o{"@type": "software_File", "@id": "file-1", "contents": "file 1"},
				o{"@type": "relationship", "from": "file-1", "to": l{"pkg-1"}},
			},
			want: func() a {
				p := &Package{Element: Element{ID: "pkg-1", Name: "pkg 1"}}
				f := &File{Element: Element{ID: "file-1"}, Contents: "file 1"}
				r := &Relationship{From: f, To: l{p}}
				return l{p, f, r}
			},
		},
		{
			name: "top level named individual",
			graph: l{
				"/dev/null", // named individual
			},
			want: func() a {
				return l{File_DevNull}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph := testGraph(t, tt.graph)
			got := toJSON(t, graph)

			want := tt.want()
			expected := toJSON(t, want)
			require.JSONEq(t, expected, got)
		})
	}
}

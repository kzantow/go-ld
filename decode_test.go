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
				o{"@type": "package", "@id": "pkg-1", "name": "pkg 1"},
				o{"@type": "file", "@id": "file-1", "contents": "file 1"},
				o{"@type": "relationship", "from": "file-1", "to": l{"pkg-1"}},
			},
			want: func() a {
				p := &Package{ID: "pkg-1", Name: "pkg 1"}
				f := &File{ID: "file-1", Contents: "file 1"}
				r := &Relationship{From: f, To: l{p}}
				return l{p, f, r}
			},
		},
		{
			graph: l{
				"/dev/null", // named individual
			},
			want: func() a {
				p := &Package{ID: "pkg-1", Name: "pkg 1"}
				f := &File{ID: "file-1", Contents: "file 1"}
				r := &Relationship{From: f, To: l{p}}
				return l{p, f, r}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph := testGraph(t, tt.graph)
			got := toJSON(t, graph)

			expected := toJSON(t, tt.want())
			require.JSONEq(t, expected, got)
		})
	}
}

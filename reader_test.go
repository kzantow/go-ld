package ld

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

func Test_readerAliasFields(t *testing.T) {
	type typ struct {
		_     Type      `iri:"https://example.org/test-iri"`
		Id    string    `iri:"@id"`
		Str   string    `iri:"https://example.org/test-iri/str-iri"`
		Bool  bool      `iri:"https://example.org/test-iri/bool-iri"`
		Int   int       `iri:"https://example.org/test-iri/int-iri"`
		Float float64   `iri:"https://example.org/test-iri/float-iri"`
		Time  time.Time `iri:"https://example.org/test-iri/time-iri"`
	}

	typContext := o{
		"t":  "https://example.org/test-iri",
		"s":  "https://example.org/test-iri/str-iri",
		"b":  "https://example.org/test-iri/bool-iri",
		"i":  "https://example.org/test-iri/int-iri",
		"f":  "https://example.org/test-iri/float-iri",
		"tm": "https://example.org/test-iri/time-iri",
	}

	idTypeAliasedContext := merge(typContext, o{
		"myId":   "@id",
		"myType": "@type",
	})

	tests := []struct {
		name     string
		context  o
		graph    any
		expected func() any
		wantErr  require.ErrorAssertionFunc
	}{
		{
			name: "all aliases, @context prop",
			context: o{
				"@context": typContext,
			},
			graph: o{
				"@type": "t",
				"s":     "joe",
				"b":     true,
				"i":     12,
				"f":     39.11,
				"tm":    mar25noon.Format(time.RFC3339),
			},
			expected: func() any {
				return &typ{
					Str:   "joe",
					Bool:  true,
					Int:   12,
					Float: 39.11,
					Time:  mar25noon,
				}
			},
		},
		{
			name:    "all aliases, no @context prop",
			context: typContext,
			graph: o{
				"@type": "t",
				"s":     "joe",
				"b":     true,
				"i":     12,
				"f":     39.11,
				"tm":    mar25noon.Format(time.RFC3339),
			},
			expected: func() any {
				return &typ{
					Str:   "joe",
					Bool:  true,
					Int:   12,
					Float: 39.11,
					Time:  mar25noon,
				}
			},
		},
		{
			name:    "full IRI, no aliases",
			context: o{},
			graph: o{
				"@type":                                  "https://example.org/test-iri",
				"https://example.org/test-iri/str-iri":   "joe",
				"https://example.org/test-iri/bool-iri":  true,
				"https://example.org/test-iri/int-iri":   12,
				"https://example.org/test-iri/float-iri": 39.11,
				"https://example.org/test-iri/time-iri":  mar25noon.Format(time.RFC3339),
			},
			expected: func() any {
				return &typ{
					Str:   "joe",
					Bool:  true,
					Int:   12,
					Float: 39.11,
					Time:  mar25noon,
				}
			},
		},
		{
			name:    "full IRI, all aliases",
			context: typContext,
			graph: o{
				"@type":                                  "https://example.org/test-iri",
				"https://example.org/test-iri/str-iri":   "joe",
				"https://example.org/test-iri/bool-iri":  true,
				"https://example.org/test-iri/int-iri":   12,
				"https://example.org/test-iri/float-iri": 39.11,
				"https://example.org/test-iri/time-iri":  mar25noon.Format(time.RFC3339),
			},
			expected: func() any {
				return &typ{
					Str:   "joe",
					Bool:  true,
					Int:   12,
					Float: 39.11,
					Time:  mar25noon,
				}
			},
		},
		{
			name:    "mixed IRI and aliases",
			context: typContext,
			graph: o{
				"@type":                                 "t",
				"https://example.org/test-iri/str-iri":  "joe",
				"https://example.org/test-iri/bool-iri": true,
				"https://example.org/test-iri/int-iri":  12,
				"f":                                     39.11,
				"https://example.org/test-iri/time-iri": mar25noon.Format(time.RFC3339),
			},
			expected: func() any {
				return &typ{
					Str:   "joe",
					Bool:  true,
					Int:   12,
					Float: 39.11,
					Time:  mar25noon,
				}
			},
		},
		{
			name:    "id and type aliases",
			context: idTypeAliasedContext,
			graph: o{
				"myType":   "t",
				"str-iri":  "joe",
				"bool-iri": true,
				"int-iri":  12,
				"f":        39.11,
				"time-iri": mar25noon.Format(time.RFC3339),
			},
			expected: func() any {
				return &typ{
					Str:   "joe",
					Bool:  true,
					Int:   12,
					Float: 39.11,
					Time:  mar25noon,
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected := tt.expected()
			contextURI := "http://example.org/uri"

			ctx := NewContext().Register(contextURI, tt.context,
				// register an empty instance of the returned type:
				reflect.New(reflect.TypeOf(expected).Elem()).Interface())
			graph := tt.graph
			if _, ok := tt.graph.(l); !ok {
				graph = l{graph}
			}
			var got any
			gotList, err := ctx.(*context).fromMaps(o{
				"@context": contextURI,
				"@graph":   graph,
			})

			wantErr := require.NoError
			if tt.wantErr != nil {
				wantErr = tt.wantErr
			}
			wantErr(t, err)

			got = gotList
			if _, ok := tt.graph.(l); !ok {
				got = gotList[0]
			}

			d := cmp.Diff(expected, got)
			if d != "" {
				t.Fatal(d)
			}
		})
	}
}

var mar25noon = get(time.Parse(time.RFC3339, "2025-03-25T12:00:00Z"))

func get[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

type o = map[string]any
type l = []any

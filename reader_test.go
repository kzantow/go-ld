package ld

import (
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
	"time"
)

func Test_readerAliasFields(t *testing.T) {
	type typ struct {
		_     Type      `iri:"t-iri"`
		Id    string    `iri:"@id"`
		Str   string    `iri:"str-iri"`
		Bool  bool      `iri:"bool-iri"`
		Int   int       `iri:"int-iri"`
		Float float64   `iri:"float-iri"`
		Time  time.Time `iri:"time-iri"`
	}

	typContext := o{
		"t":  "t-iri",
		"s":  "str-iri",
		"b":  "bool-iri",
		"i":  "int-iri",
		"f":  "float-iri",
		"tm": "time-iri",
	}

	idTypeAliasedContext := o{
		"myId":   "@id",
		"myType": "@type",
		"t":      "t-iri",
		"s":      "str-iri",
		"b":      "bool-iri",
		"i":      "int-iri",
		"f":      "float-iri",
		"tm":     "time-iri",
	}

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
				"@type":     "t-iri",
				"str-iri":   "joe",
				"bool-iri":  true,
				"int-iri":   12,
				"float-iri": 39.11,
				"time-iri":  mar25noon.Format(time.RFC3339),
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
				"@type":     "t-iri",
				"str-iri":   "joe",
				"bool-iri":  true,
				"int-iri":   12,
				"float-iri": 39.11,
				"time-iri":  mar25noon.Format(time.RFC3339),
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
				"@type":    "t",
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
			ctx := Context{}
			contextURI := "http://example.org/uri"
			ctx.Register(contextURI, tt.context,
				// register an empty instance of the returned type:
				reflect.New(reflect.TypeOf(expected).Elem()).Interface())
			graph := tt.graph
			if _, ok := tt.graph.(l); !ok {
				graph = l{graph}
			}
			var got any
			gotList, err := ctx.FromMaps(o{
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

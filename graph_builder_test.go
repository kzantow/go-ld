package ld

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

func Test_graphBuilder(t *testing.T) {
	type typ struct {
		_     Type      `iri:"t-iri"`
		Id    string    `iri:"@id"`
		Str   string    `iri:"str-iri"`
		Bool  bool      `iri:"bool-iri"`
		Int   int       `iri:"int-iri"`
		Float float64   `iri:"float-iri"`
		Time  time.Time `iri:"time-iri"`
	}

	type typ2 struct {
		_          Type   `iri:"t2-iri"`
		Identifier string `iri:"@id"`
		T1         *typ   `iri:""`
	}

	typContext := o{
		"t":    "t-iri",
		"t2":   "t2-iri",
		"s":    "str-iri",
		"b":    "bool-iri",
		"i":    "int-iri",
		"f":    "float-iri",
		"tm":   "time-iri",
		"t2t1": "t2-to-t1-iri",
	}

	contextWithIdTypeOverride := merge(typContext, o{
		"aliased-type": "@type",
		"aliased-id":   "@id",
	})

	localId := func(typ any, i int) string {
		return "_:" + reflect.TypeOf(typ).Name() + "-" + strconv.Itoa(i)
	}

	tests := []struct {
		name     string
		context  o
		graph    func() any
		expected any
		wantErr  require.ErrorAssertionFunc
	}{
		{
			name:    "basic no context full iri",
			context: o{},
			graph: func() any {
				return &typ{
					Str:   "str-val",
					Bool:  true,
					Int:   101,
					Float: 940.33,
					Time:  mar25noon,
				}
			},
			expected: o{
				"@type":     "t-iri",
				"str-iri":   "str-val",
				"bool-iri":  true,
				"int-iri":   101,
				"float-iri": 940.33,
				"time-iri":  mar25noon.Format(time.RFC3339),
			},
		},
		{
			name:    "basic all aliases context",
			context: typContext,
			graph: func() any {
				return &typ{
					Str:   "str-val",
					Bool:  true,
					Int:   101,
					Float: 940.33,
					Time:  mar25noon,
				}
			},
			expected: o{
				"@type": "t",
				//"@id":   localId(typ{}, 1),
				"s":  "str-val",
				"b":  true,
				"i":  101,
				"f":  940.33,
				"tm": mar25noon.Format(time.RFC3339),
			},
		},
		{
			name:    "all aliases overridden id type",
			context: contextWithIdTypeOverride,
			graph: func() any {
				return &typ{}
			},
			expected: o{
				"aliased-type": "t",
			},
		},
		{
			name:    "multiple refs gets id",
			context: contextWithIdTypeOverride,
			graph: func() any {
				t1 := &typ{
					Str: "a-val",
				}
				return l{
					&typ2{
						T1: t1,
					},
					&typ2{
						T1: t1,
					},
				}
			},
			expected: l{
				o{
					"aliased-type": "t2",
					"t2t1":         localId(typ{}, 1),
				},
				o{
					"aliased-type": "t2",
					"t2t1":         localId(typ{}, 1),
				},
				o{
					"aliased-type": "t",
					"aliased-id":   localId(typ{}, 1),
					"s":            "a-val",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph := tt.graph()
			ctx := NewContext()
			contextURI := "http://example.org/uri"
			ctx.Register(contextURI, tt.context, typ{}, typ2{})
			expected := tt.expected
			if _, ok := tt.expected.(l); !ok {
				expected = l{expected}
			}
			expected = o{
				"@context": contextURI,
				"@graph":   expected,
			}

			var graphs l
			if _, ok := graph.(l); ok {
				graphs = graph.(l)
			}
			var got any
			got, err := ctx.(*context).toMaps(graphs...)

			wantErr := require.NoError
			if tt.wantErr != nil {
				wantErr = tt.wantErr
			}
			wantErr(t, err)

			d := cmp.Diff(expected, got)
			if d != "" {
				t.Fatal(d)
			}
		})
	}
}

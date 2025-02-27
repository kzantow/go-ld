package ld

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

type o = map[string]any
type l = []any

func Test_MultiRegistration(t *testing.T) {
	type AType struct {
		_    Type   `iri:"http://example.org/context/thing-a"`
		Name string `iri:"http://example.org/context/name"`
	}

	contextName := "http://example.org/context"
	ctx := Context{}.Register(contextName, o{"@context": o{
		"name": "http://example.org/context/name",
	}}, AType{})
	require.Len(t, ctx, 1)

	type BType struct {
		_    Type   `iri:"http://example.org/context/thing-b"`
		Name string `iri:"http://example.org/context/name"`
	}

	ctx = ctx.Register(contextName, o{"@context": o{}}, BType{})
	require.Len(t, ctx, 1)

	sc := ctx[contextName]
	require.NotNil(t, sc)
	require.Len(t, sc.typeToContext, 2)
	require.Equal(t, "name", sc.iriToAlias["http://example.org/context/name"])

	maps, err := ctx.ToMaps(AType{
		Name: "A",
	}, BType{
		Name: "B",
	})
	require.NoError(t, err)

	diff := cmp.Diff(o{
		"@context": "http://example.org/context",
		"@graph": l{
			o{"@type": "http://example.org/context/thing-a", "name": "A"},
			o{"@type": "http://example.org/context/thing-b", "name": "B"},
		},
	}, maps)
	if diff != "" {
		t.Fatal(diff)
	}
}

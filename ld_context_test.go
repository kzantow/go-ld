package ld

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

func Test_registeredInstances(t *testing.T) {
	type AType struct {
		_     Type   `iri:"http://example.org/context/thing-a"`
		ID    string `iri:"@id"`
		Name  string `iri:"http://example.org/context/name"`
		thing []any
	}

	inst := &AType{
		ID:   "http://example.org/context/an-instance",
		Name: "an-instance",
	}

	contextName := "http://example.org/context"
	ctx := NewContext().Register(contextName, o{"@context": o{
		"name": "http://example.org/context/name",
	}}, inst)
	require.Len(t, ctx, 1)

	g, err := ctx.(*context).fromMaps(o{
		"@context": contextName,
		"@graph": l{
			"http://example.org/context/an-instance",
		},
	})
	require.NoError(t, err)
	require.Equal(t, inst, g[0])
}

func Test_MultiRegistration(t *testing.T) {
	type AType struct {
		_    Type   `iri:"http://example.org/context/thing-a"`
		Name string `iri:"http://example.org/context/name"`
	}

	contextName := "http://example.org/context"
	ctx := NewContext().Register(contextName, o{"@context": o{
		"name": "http://example.org/context/name",
	}}, AType{})
	require.Len(t, ctx, 1)

	type BType struct {
		_    Type   `iri:"http://example.org/context/thing-b"`
		Name string `iri:"http://example.org/context/name"`
	}

	ctx = ctx.Register(contextName, o{"@context": o{}}, BType{})
	require.Len(t, ctx, 1)

	maps, err := ctx.(*context).toMaps(AType{
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

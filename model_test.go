package ld_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/kzantow/go-ld"
	"github.com/stretchr/testify/require"
)

// convenience for writing json as code:

type o = map[string]any // object
type l = []any          // list
type a = any            // any

type Package struct {
	_    ld.Type `iri:"package"`
	ID   string  `iri:"@id" iri-compact:"spdxId"`
	Name string  `iri:"name"`
	Size int64   `iri:"https://example.com/iri/size" iri-compact:"size"`
}

type File struct {
	_        ld.Type `iri:"file"`
	ID       string  `iri:"@id"`
	Contents string  `iri:"contents"`
}

type Relationship struct {
	_    ld.Type `iri:"relationship"`
	ID   string  `iri:"@id"`
	From any     `iri:"from"`
	To   []any   `iri:"to"`
}

type AnyRelationship interface {
	asRelationship() *Relationship
}

func (r *Relationship) asRelationship() *Relationship {
	return r
}

// SubRelationship implements inheritance by embedding
type SubRelationship struct {
	_ ld.Type `iri:"sub-relationship"`
	Relationship
}

type ExternalIRI struct {
	ExternalID string `iri:"@id"`
}

func (r *ExternalIRI) asRelationship() *Relationship {
	return nil
}

func testGraph(t *testing.T, graph l) []any {
	ctx, contextURL := testContext()

	in := o{
		"@context": contextURL,
		"@graph":   graph,
	}

	graph, err := ctx.FromMaps(in)
	if err != nil {
		t.Fatal(err)
	}

	return graph
}

func testContext() (ld.Context, string) {
	contextURL := "test"
	return ld.Context{}.Register(contextURL, []ld.TypeAlias{
		{
			Type: File{},
			Aliases: map[string]string{
				"https://example.org/iri/file/dev/null": "/dev/null",
			},
		},
	},
		Package{},
		File{},
		Relationship{},
	), contextURL
}

func toJSON(t *testing.T, o any) string {
	buf := bytes.Buffer{}
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(o)
	require.NoError(t, err)
	return buf.String()
}

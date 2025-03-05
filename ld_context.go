package ld

import (
	"encoding/json"
	"github.com/davecgh/go-spew/spew"
	"io"
	"reflect"
)

// Type is a 0-size data holder property type for type-level ld information
type Type struct{}

// Context is the holder for all known LD contexts and required definitions
type Context struct {
	contextMap map[string]*serializationContext
	// iriToType contains full IRIs and aliases to the appropriate typeContext
	iriToType map[string]*typeContext
	// typeToContext contains references from the go type(s) to appropriate typeContext
	typeToContext map[reflect.Type]*typeContext
	// iriToInstance are directly registered instances in code
	iriToInstance map[string]reflect.Value
	// typeToExternalIriFunc holds registered functions to construct external placeholders IRIs
	typeToExternalIriFunc map[reflect.Type]func(string) reflect.Value
}

func NewContext() *Context {
	return &Context{
		contextMap:            map[string]*serializationContext{},
		iriToType:             map[string]*typeContext{},
		typeToContext:         map[reflect.Type]*typeContext{},
		iriToInstance:         map[string]reflect.Value{},
		typeToExternalIriFunc: map[reflect.Type]func(string) reflect.Value{},
	}
}

// Merge returns a new context, with the values from both contexts merged together
func (c *Context) Merge(ctx *Context) *Context {
	return &Context{
		contextMap:    merge(c.contextMap, ctx.contextMap),
		iriToType:     merge(c.iriToType, ctx.iriToType),
		typeToContext: merge(c.typeToContext, ctx.typeToContext),
		iriToInstance: merge(c.iriToInstance, ctx.iriToInstance),
	}
}

// Register registers types and aliases to be used when serializing/deserializing documents
func (c *Context) Register(contextURI string, ldContext map[string]any, types ...any) *Context {
	ctx := c.getContext(contextURI)
	ctx.ldContext = merge(ctx.ldContext, ldContext)
	c.registerContextAliases(ctx, ldContext)
	for _, typ := range types {
		switch {
		case isFunc(typ):
			registerFunc(c, typ)
		default:
			registerType(c, ctx, typ)
		}
	}
	return c
}

// registerContextAliases registers compact name aliases for the given IRIs in the given context
func (c *Context) registerContextAliases(ctx *serializationContext, ldContext map[string]any) {
	subContext, _ := ldContext[JsonContextProp].(map[string]any)
	if subContext != nil {
		c.registerContextAliases(ctx, subContext)
		return
	}
	for alias, v := range ldContext {
		if alias == JsonContextProp {
			continue
		}
		switch v := v.(type) {
		case string:
			c.registerContextAlias(ctx, v, alias)
		case map[string]any:
			iri, _ := v[JsonIdProp].(string)
			if iri != "" {
				c.registerContextAlias(ctx, iri, alias)
			}
			// should this be checked? if v[JsonTypeProp] == JsonVocabProp {
			subContext, _ = v[JsonContextProp].(map[string]any)
			if subContext != nil {
				contextPrefix, _ := subContext[JsonVocabProp].(string)
				ctx.aliasContext[alias] = contextPrefix
			}
		}
	}
}

// registerContextAlias registers compact name aliases for the given IRIs in the given context
func (c *Context) registerContextAlias(ctx *serializationContext, iri, alias string) {
	if ctx.aliasToIri[alias] != "" {
		panic("duplicate alias set globally: " + alias + "; iri: " + iri + "; existing: " + ctx.aliasToIri[alias])
	}
	ctx.aliasToIri[alias] = iri
	//if ctx.iriToAlias[iri] != "" {
	//	panic("duplicate iri alias set globally: " + iri + "; alias: " + alias + "; existing: " + ctx.iriToAlias[iri])
	//}
	//ctx.iriToAlias[iri] = alias
}

func (c *Context) getContext(contextUrl string) *serializationContext {
	ctx := c.contextMap[contextUrl]
	if ctx == nil {
		ctx = &serializationContext{
			contextUrl:   contextUrl,
			aliasToIri:   map[string]string{},
			aliasContext: map[string]string{},
			//iriToAlias:   map[string]string{},
		}
		c.contextMap[contextUrl] = ctx
	}
	return ctx
}

func (c *Context) ToJSON(writer io.Writer, graph ...any) error {
	out, err := c.ToMaps(graph...)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(writer)
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

func (c *Context) ToMaps(graph ...any) (values map[string]any, errors error) {
	// the ld graph is referenced here
	// traverse the go map[string]anys to output to the graph
	builder := graphBuilder{
		ctx:      c,
		prefixes: map[*serializationContext]string{},
		ids:      map[reflect.Value]string{},
	}
	return builder.toCompactMaps(graph...)
}

func (c *Context) FromMaps(values map[string]any) ([]any, error) {
	rdr := mapReader{ctx: c}
	return rdr.FromMaps(values)
}

func (c *Context) FromJSON(reader io.Reader) ([]any, error) {
	vals := map[string]any{}
	dec := json.NewDecoder(reader)
	err := dec.Decode(&vals)
	if err != nil {
		return nil, err
	}
	return c.FromMaps(vals)
}

const (
	JsonIdProp      = "@id"
	JsonTypeProp    = "@type"
	JsonValueProp   = "@value"
	JsonContextProp = "@context"
	JsonGraphProp   = "@graph"
	JsonVocabProp   = "@vocab"
	GoTypeField     = "_"
	GoIdField       = "ID"
	GoIriTagName    = "iri"
)

type typeContext struct {
	ctx     *serializationContext
	typ     reflect.Type
	iri     string
	alias   string
	setters map[string]func(instance reflect.Value, value reflect.Value)
}

type serializationContext struct {
	contextUrl string
	// the full JSON LD context provided
	ldContext map[string]any
	// aliasToIri contains field aliases to the respective IRIs
	aliasToIri map[string]string
	// aliasContext
	aliasContext map[string]string
	//iriToAlias   map[string]string
}

func registerFunc(c *Context, fn any) {
	f := reflect.ValueOf(fn)
	t := f.Type()

	if t.NumIn() != 1 || t.In(0).Kind() != reflect.String {
		panic("external IRI functions must have one parameter, accepting an IRI string")
	}

	if t.NumOut() != 1 {
		panic("external IRI functions must have one return value")
	}

	rVal := t.Out(0)
	c.typeToExternalIriFunc[rVal] = func(s string) reflect.Value {
		out := f.Call([]reflect.Value{reflect.ValueOf(s)})
		return out[0]
	}
}

func registerType(c *Context, ctx *serializationContext, instancePointer any) {
	t := reflect.TypeOf(instancePointer)
	instance := reflect.ValueOf(instancePointer)
	t = baseType(t) // types may be passed as pointers, but we want the base types

	tc := c.typeToContext[t]
	if tc == nil {
		meta, ok := fieldByType[Type](t)
		if ok {
			iri := meta.Tag.Get(GoIriTagName)
			if iri == "" {
				panic("no type IRI specified for: " + spew.Sdump(instancePointer))
			}
			tc = &typeContext{
				iri:     iri,
				ctx:     ctx,
				typ:     t,
				setters: map[string]func(instance reflect.Value, value reflect.Value){},
			}
			c.iriToType[tc.iri] = tc
			//ctx.iriToAlias[tc.iri] = tc.compact
			c.typeToContext[t] = tc
		}
	}

	// capture all the registered types
	id, err := getID(instance)
	if err != nil {
		// we should not have invalid types registered
		panic(err)
	}
	if id != "" {
		switch instance.Type().Kind() {
		case reflect.Pointer, reflect.Struct:
		default:
			panic("expected instance registration to be a pointer or a struct, got: " + spew.Sdump(instance))
		}
		c.iriToInstance[id] = instance
	}
}

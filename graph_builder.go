package ld

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"

	"github.com/davecgh/go-spew/spew"
	"github.com/piprate/json-gold/ld"
)

type graphBuilder struct {
	ctx         *context
	input       reflect.Value
	graph       []any // graph stores all the serialized objects in the graph
	nextID      map[reflect.Type]int
	ids         map[reflect.Value]string
	pointerRefs map[reflect.Value]map[string]any // pointerRefs stores references to each serialized pointer
}

func (b *graphBuilder) toCompactMaps(graph ...any) (map[string]any, error) {
	expanded, err := b.toExpandedMaps(graph...)
	if err != nil {
		return nil, err
	}

	f, _ := os.OpenFile("spdx-encoding-test.expanded.json", os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0777)
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(expanded)

	proc := ld.NewJsonLdProcessor()
	opts := ld.NewJsonLdOptions("")
	// all options:
	//opts.Base
	opts.CompactArrays = true
	//opts.ExpandContext
	opts.ProcessingMode = ld.JsonLd_1_1
	opts.DocumentLoader = offlineDocumentLoader{ctx: b.ctx}
	//opts.Embed
	//opts.Explicit
	//opts.RequireAll
	//opts.FrameDefault
	//opts.OmitDefault
	//opts.OmitGraph
	//opts.UseRdfType
	//opts.UseNativeTypes
	//opts.ProduceGeneralizedRdf
	//opts.InputFormat
	//opts.Format
	//opts.Algorithm
	//opts.UseNamespaces
	//opts.OutputForm
	//opts.SafeMode

	var compactionContext map[string]any
	switch len(b.ctx.contextMap) {
	case 0:
		return nil, fmt.Errorf("no contexts defined, unable to serialize")
	case 1:
		compactionContext = map[string]interface{}{
			"@context": firstKey(b.ctx.contextMap),
		}
	default:
		prefixes := map[string]any{}
		for i, url := range sortedKeys(b.ctx.contextMap) {
			prefixes["ns"+strconv.Itoa(i)] = url
		}
		compactionContext = map[string]interface{}{
			"@context": prefixes,
		}
	}

	compact, err := proc.Compact(expanded, compactionContext, opts)
	return compact, err
}

func (b *graphBuilder) toExpandedMaps(graph ...any) ([]any, error) {
	b.input = reflect.ValueOf(graph)
	b.graph = nil
	for _, v := range graph {
		val := reflect.ValueOf(v)
		value, err := b.serializeNode(val)
		if err != nil {
			return nil, err
		}
		b.graph = append(b.graph, value)
	}
	return b.graph, nil
}

// serializeNode outputs the top-level nodes in the graph; these have the behavior that they are always returned in
// serialized form rather than potentially returning an ID reference. pointers with multiple references will also
// ensure the @id field is set in order to be referenced later
func (b *graphBuilder) serializeNode(v reflect.Value) (map[string]any, error) {
	if !v.IsValid() {
		return nil, nil
	}
	switch v.Kind() {
	case reflect.Interface:
		// serialize the underlying data
		return b.serializeNode(v.Elem())
	case reflect.Pointer:
		// output an ID for every top-level element in case they are referenced in multiple locations within the graph
		id, err := b.getID(v)
		if err != nil {
			return nil, err
		}
		// serialize the object to the graph
		value, err := b.serializeSingleValue(v.Elem())
		if err != nil {
			return nil, err
		}
		value[JsonIdProp] = id
		return value, nil
	case reflect.Slice:
		panic("unexpected slice value")
	default:
		return b.serializeSingleValue(v)
	}
}

// serializeSingleValue serializes the provided value to an expanded value form, for example:
//
//	 {
//	  "@type": "http://www.w3.org/2001/XMLSchema#string",
//	  "@value": "Some Value"
//	}
//
// or:
//
//	{
//	 "@id": "_:CreationInfo-1",
//	 "@type": [
//	   "https://spdx.org/rdf/3.0.1/terms/Core/CreationInfo"
//	 ],
//	 "https://spdx.org/rdf/3.0.1/terms/Core/created": [
//	 ...
func (b *graphBuilder) serializeSingleValue(v reflect.Value) (map[string]any, error) {
	if !v.IsValid() {
		return nil, nil
	}

	t := v.Type()

	// check for known primitive conversions first, since some types may be structs
	if typeToConverter[t] != nil {
		return b.serializePrimitiveValue(v)
	}

	switch t.Kind() {
	case reflect.Interface:
		return b.serializeSingleValue(v.Elem())
	case reflect.Pointer:
		return b.serializeSingleValue(v.Elem())
	case reflect.Struct:
		return b.serializeStruct(v)
	default:
		panic("unexpected serialization value: " + stringify(v))
	}
}

func (b *graphBuilder) serializeSlice(slice reflect.Value) ([]any, error) {
	if slice.Kind() != reflect.Slice {
		panic("expected slice")
	}
	var out []any
	for i := 0; i < slice.Len(); i++ {
		value, err := b.getValueOrID(slice.Index(i))
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

func (b *graphBuilder) findContext(t reflect.Type) *serializationContext {
	t = baseType(t) // map[string]any may be a pointer, but we want the base types
	tc := b.ctx.typeToContext[t]
	if tc != nil {
		return tc.ctx
	}
	return nil
}

func (b *graphBuilder) serializeStruct(v reflect.Value) (value map[string]any, err error) {
	t := v.Type()
	if t.Kind() != reflect.Struct {
		panic("expected struct, got: " + stringify(v))
	}

	// some structs like ExternalIRI do not have type information, but these types must have an @id field,
	// we will just output this value
	id, _ := getID(v)

	out := map[string]any{}

	tc := b.ctx.typeToContext[t]
	if tc != nil {
		err = b.writeStructProperties(tc.ctx, tc, v, out)
		if err != nil {
			return out, err
		}

		// always append the type unless the only value we have is an external IRI reference
		if len(out) > 0 || id == "" || isBlankNodeID(id) {
			out[JsonTypeProp] = []any{
				tc.iri,
			}
		}
	}

	if id != "" {
		out[JsonIdProp] = id
	}
	// skip objects with no properties whatsoever
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func (b *graphBuilder) writeStructProperties(context *serializationContext, tc *typeContext, v reflect.Value, out map[string]any) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if skipField(f) || isIdField(f) { // ID is set outside of this function
			continue
		}

		// embedded struct, recursively call this function to get all struct values
		if f.Anonymous {
			err := b.writeStructProperties(context, tc, v.Field(i), out)
			if err != nil {
				return err
			}
			continue
		}

		optional := !isRequired(f)
		fieldV := v.Field(i)

		if optional && isEmpty(fieldV) {
			continue
		}

		val, err := b.serializeFieldValue(f, fieldV)
		if err != nil {
			return err
		}

		if val == nil && optional {
			continue
		}

		prop := f.Tag.Get(GoIriTagName)
		out[prop] = val
	}

	return nil
}

// serializeFieldValue returns a serialized field value, which must be a slice in expanded form
func (b *graphBuilder) serializeFieldValue(f reflect.StructField, v reflect.Value) ([]any, error) {
	if !v.IsValid() {
		return nil, nil
	}
	switch v.Type().Kind() {
	case reflect.Slice:
		var out []any
		for i := 0; i < v.Len(); i++ {
			val, err := b.getValueOrID(v.Index(i))
			if err != nil {
				return nil, err
			}
			out = append(out, val)
		}
		return []any{out}, nil
	default:
		val, err := b.getValueOrID(v)
		if err != nil {
			return nil, err
		}
		return []any{
			val,
		}, nil
	}
}

func (b *graphBuilder) serializePrimitiveValue(v reflect.Value) (map[string]any, error) {
	if !v.IsValid() || !v.CanInterface() {
		return nil, nil
	}

	value := v.Interface()
	c := typeToConverter[v.Type()]
	if c != nil {
		if c.Serialize != nil {
			value = c.Serialize(value)
		}
		return map[string]any{
			JsonTypeProp:  c.IRI,
			JsonValueProp: value,
		}, nil
	}

	return nil, fmt.Errorf("unsupported value type: %s: %v", typeName(v.Type()), value)
}

// getValueOrID will return an ID for the given struct pointer, creating one if needed
// and appending the serialized object reference to the top-level graph
func (b *graphBuilder) getValueOrID(v reflect.Value) (map[string]any, error) {
	if !v.IsValid() {
		return nil, nil
	}

	switch v.Type().Kind() {
	case reflect.Interface:
		return b.getValueOrID(v.Elem())
	case reflect.Pointer:
		// continue
	default:
		// if not a pointer, we don't need to handle ID reference checks
		return b.serializeSingleValue(v)
	}

	// if there's an ID set, just output
	if id, ok := b.ids[v]; ok {
		// if we have a reference to the initial instance embedded, replace it with an ID and
		// move the instance to the top-level graph
		val := b.pointerRefs[v]
		if val != nil {
			b.graph = append(b.graph, val)
			clear(val)
			val[JsonIdProp] = id
			delete(b.pointerRefs, v)
		}

		// finally return an ID reference to the object we appended to the graph
		return map[string]any{
			JsonIdProp: id,
		}, nil
	}

	// otherwise we need to get a valid id and output the object to the graph
	id, err := b.getID(v)
	if err != nil {
		return nil, err
	}

	val, err := b.serializeSingleValue(v)
	val[JsonIdProp] = id // ensure the ID
	if err != nil {
		return nil, err
	}

	const outputFirstReferenceInline = false
	if outputFirstReferenceInline {
		return val, nil
	}

	b.graph = append(b.graph, val)

	// track a reference to this map to update it when we have a second reference
	//b.pointerRefs[v] = val
	//return val, nil
	return map[string]any{
		JsonIdProp: id,
	}, nil
}

// getID will return an ID for the given struct pointer, creating one if needed
// it does not append structs to the graph
func (b *graphBuilder) getID(ptr reflect.Value) (string, error) {
	if ptr.Type().Kind() != reflect.Pointer {
		panic("expected pointer, got: " + stringify(ptr))
	}
	if id, ok := b.ids[ptr]; ok {
		return id, nil
	}

	v := ptr.Elem()
	t := v.Type()

	// check if the struct has an ID set directly, and use that if so
	id, err := getID(v)
	if err != nil {
		return "", err
	}
	if id == "" {
		nextID := b.nextID[t] + 1
		b.nextID[t] = nextID
		id = fmt.Sprintf("_:%s-%v", t.Name(), nextID)
	}
	b.ids[ptr] = id
	return id, nil
}

func stringify(o any) string {
	switch o := o.(type) {
	case reflect.Value:
		if !o.IsValid() {
			return "<invalid reflect value>"
		}
		if o.CanInterface() {
			return typeName(o.Type()) + ": " + stringify(o.Interface())
		}
	case reflect.Type:
		return fmt.Sprintf("%s.%s", o.PkgPath(), o.Name())
	}
	return spew.Sdump(o)
}

func isIdField(f reflect.StructField) bool {
	return f.Tag.Get(GoIriTagName) == JsonIdProp
}

func isEmpty(v reflect.Value) bool {
	return !v.IsValid() || v.IsZero()
}

func isRequired(f reflect.StructField) bool {
	return f.Tag.Get("required") == "true"
}

package ld

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"reflect"
	"strings"
	"time"
)

// Type is a 0-size data holder property type for type-level linked data
type Type struct{}

// Context is the holder for all known LD contexts and required definitions
type Context map[string]*serializationContext

// Merge allows convenient merging of multiple contexts together
func (c Context) Merge(ctx Context) Context {
	for k, v := range ctx {
		if _, ok := c[k]; ok {
			panic("Context key already defined: " + k)
		}
		c[k] = v
	}
	return c
}

// Register registers types and aliases to be used when serializing/deserializing documents
func (c Context) Register(contextUrl string, ldContext map[string]any, types ...any) Context {
	ctx := c.getContext(contextUrl)
	c.registerContextAliases(ctx, ldContext)
	for _, typ := range types {
		ctx.registerType(typ)
	}
	return c
}

// registerContextAliases registers compact name aliases for the given IRIs in the given context
func (c Context) registerContextAliases(ctx *serializationContext, ldContext map[string]any) {
	subContext, _ := ldContext[JsonContextProp].(map[string]any)
	if subContext != nil {
		c.registerContextAliases(ctx, subContext)
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
func (c Context) registerContextAlias(ctx *serializationContext, iri, alias string) {
	if ctx.aliasToIri[alias] != "" {
		panic("duplicate alias set globally: " + alias + "; iri: " + iri + "; existing: " + ctx.aliasToIri[alias])
	}
	ctx.aliasToIri[alias] = iri
	if ctx.iriToAlias[iri] != "" {
		panic("duplicate iri alias set globally: " + iri + "; alias: " + alias + "; existing: " + ctx.iriToAlias[iri])
	}
	ctx.iriToAlias[iri] = alias
}

func (c Context) getContext(contextUrl string) *serializationContext {
	ctx := c[contextUrl]
	if ctx == nil {
		ctx = &serializationContext{
			contextUrl: contextUrl,
			//ldContext:     map[string]any{},
			aliasToIri:    map[string]string{},
			aliasContext:  map[string]string{},
			iriToAlias:    map[string]string{},
			iriToType:     map[string]*typeContext{},
			typeToContext: map[reflect.Type]*typeContext{},
		}
		c[contextUrl] = ctx
	}
	return ctx
}

func (c Context) ToJSON(writer io.Writer, value any) error {
	vals, err := c.ToMaps(value)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(writer)
	enc.SetEscapeHTML(false)
	return enc.Encode(vals)
}

func (c Context) ToMaps(o ...any) (values map[string]any, errors error) {
	// the ld graph is referenced here
	// traverse the go objects to output to the graph
	builder := graphBuilder{
		ldc:   c,
		input: o,
		ids:   map[reflect.Value]string{},
	}

	var err error
	var context *serializationContext
	for _, o := range builder.input {
		context, err = builder.add(o)
		if err != nil {
			return nil, err
		}
	}

	if context == nil {
		return nil, fmt.Errorf("")
	}

	return map[string]any{
		JsonContextProp: context.contextUrl,
		JsonGraphProp:   builder.toGraph(),
	}, nil
}

func (c Context) FromJSON(reader io.Reader) ([]any, error) {
	vals := map[string]any{}
	dec := json.NewDecoder(reader)
	err := dec.Decode(&vals)
	if err != nil {
		return nil, err
	}
	return c.FromMaps(vals)
}

func (c Context) FromMaps(values map[string]any) ([]any, error) {
	instances := map[string]reflect.Value{}

	var errs error
	var graph []any

	context, _ := values[JsonContextProp].(string)
	currentContext := c[context]
	if currentContext == nil {
		return nil, fmt.Errorf("unknown %s: '%s' must be in %v", JsonContextProp, context, maps.Keys(c))
	}

	nodes, _ := values[JsonGraphProp].([]any)
	if nodes == nil {
		return nil, fmt.Errorf("%s not found", JsonGraphProp)
	}

	// one pass to create all the instances
	for _, node := range nodes {
		_, err := c.getOrCreateInstance(currentContext, instances, anyType, node)
		errs = appendErr(errs, err)
	}

	// second pass to fill in all refs
	for _, node := range nodes {
		got, err := c.getOrCreateInstance(currentContext, instances, anyType, node)
		errs = appendErr(errs, err)
		if err == nil && got.IsValid() && got.CanInterface() {
			graph = append(graph, got.Interface())
		}
	}

	return graph, errs
}

func (c Context) getOrCreateInstance(currentContext *serializationContext, instances map[string]reflect.Value, expectedType reflect.Type, incoming any) (reflect.Value, error) {
	if isPrimitive(expectedType) {
		if convertedVal := convertTo(incoming, expectedType); convertedVal != emptyValue {
			return convertedVal, nil
		}
		return emptyValue, fmt.Errorf("unable to convert incoming value to type %v: %+v", typeName(expectedType), incoming)
	}
	switch incoming := incoming.(type) {
	case string:
		instance := c.findById(currentContext, instances, incoming)
		if instance != emptyValue {
			return instance, nil
		}
		// not found: have a complex type with string indicates an IRI or other primitive
		switch expectedType.Kind() {
		case reflect.Pointer:
			expectedType = expectedType.Elem()
			if isPrimitive(expectedType) {
				val, err := c.getOrCreateInstance(currentContext, instances, expectedType, incoming)
				if err != nil {
					return emptyValue, err
				}
				instance = reflect.New(expectedType)
				instance.Elem().Set(val)
				return instance, nil
			}
			if expectedType.Kind() == reflect.Struct {
				return emptyValue, fmt.Errorf("unexpected pointer reference external IRI reference: %v", incoming)
			}
			fallthrough
		case reflect.Struct:
			instance = reflect.New(expectedType)
			instance = instance.Elem()
			err := c.setInstanceByIRI(currentContext, instances, instance, incoming)
			return instance, err
		case reflect.Interface:
			// an IRI with an interface is a reference to an unknown type, so use the closest type
			newType, found := c.findExternalReferenceType(currentContext, expectedType)
			if found {
				instance = reflect.New(newType)
				// try to return the appropriately assignable instance
				if !instance.Type().AssignableTo(expectedType) {
					instance = instance.Elem()
				}
				err := c.setInstanceByIRI(currentContext, instances, instance, incoming)
				return instance, err
			}
			return emptyValue, fmt.Errorf("unable to determine external reference type while populating %v for IRI reference: %v", typeName(expectedType), incoming)
		default:
		}
	case map[string]any:
		return c.getOrCreateFromMap(currentContext, instances, incoming)
	}
	return emptyValue, fmt.Errorf("unexpected data type: %#v", incoming)
}

func convertTo(incoming any, typ reflect.Type) reflect.Value {
	v := reflect.ValueOf(incoming)
	if v.CanConvert(typ) {
		return v.Convert(typ)
	}
	return emptyValue
}

func (c Context) setInstanceByIRI(ctx *serializationContext, instances map[string]reflect.Value, instance reflect.Value, incoming string) error {
	return c.setStructProps(ctx, instances, instance, map[string]any{
		JsonIdProp: incoming,
	})
}

func (c Context) findById(_ *serializationContext, instances map[string]reflect.Value, incoming string) reflect.Value {
	inst, ok := instances[incoming]
	if ok {
		return inst
	}
	return emptyValue
}

func (c Context) getOrCreateFromMap(currentContext *serializationContext, instances map[string]reflect.Value, incoming map[string]any) (reflect.Value, error) {
	typ, ok := incoming[JsonTypeProp].(string)
	if !ok && currentContext.iriToAlias[JsonTypeProp] != "" {
		typ, ok = incoming[currentContext.iriToAlias[JsonTypeProp]].(string)
	}
	if !ok {
		return emptyValue, fmt.Errorf("not a string")
	}

	tc, ok := currentContext.iriToType[typ]
	if !ok {
		return emptyValue, fmt.Errorf("don't have type: %v", typ)
	}

	id, _ := incoming[JsonIdProp].(string)
	if id == "" && tc.ctx.iriToAlias[JsonIdProp] != "" {
		id, _ = incoming[tc.ctx.iriToAlias[JsonIdProp]].(string)
	}
	inst, ok := instances[id]
	if !ok {
		inst = reflect.New(baseType(tc.typ)) // New(T) returns *T
		if id != "" {
			// only set instance references when an ID is provided
			instances[id] = inst
		}
	}

	// valid type, make a new one and fill it from the incoming maps
	return inst, c.fill(currentContext, instances, inst, incoming)
}

func (c Context) fill(currentContext *serializationContext, instances map[string]reflect.Value, instance reflect.Value, incoming any) error {
	switch incoming := incoming.(type) {
	case string:
		inst := c.findById(currentContext, instances, incoming)
		if inst != emptyValue {
			return c.setValue(currentContext, instances, instance, inst)
		}
		// should be an incoming ID if string
		return c.setValue(currentContext, instances, instance, map[string]any{
			JsonIdProp: incoming,
		})
	case map[string]any:
		return c.setStructProps(currentContext, instances, instance, incoming)
	}
	return fmt.Errorf("unsupported incoming data type: %#v attempting to set instance: %#v", incoming, instance.Interface())
}

func (c Context) setValue(currentContext *serializationContext, instances map[string]reflect.Value, target reflect.Value, incoming any) error {
	var errs error
	typ := target.Type()
	// special decoding for timestamp properties to time.Time objects
	if typ == timeTimeType {
		switch incoming := incoming.(type) {
		case string:
			v, err := time.Parse(time.RFC3339, incoming)
			if err != nil {
				// FIXME more lenient parsing
				return err
			}
			target.Set(reflect.ValueOf(v))
			return nil
		}
		return fmt.Errorf("attempting to decode time, expected string; got: %#v", incoming)
	}

	switch typ.Kind() {
	case reflect.Slice:
		switch incoming := incoming.(type) {
		case []any:
			return c.setSliceValue(currentContext, instances, target, incoming)
		}
		// try mapping a single value to an incoming slice
		return c.setValue(currentContext, instances, target, []any{incoming})
	case reflect.Struct:
		switch incoming := incoming.(type) {
		case map[string]any:
			return c.setStructProps(currentContext, instances, target, incoming)
		case string:
			// named individuals just need an object with the iri set
			return c.setStructProps(currentContext, instances, target, map[string]any{
				JsonIdProp: incoming,
			})
		}
	case reflect.Interface, reflect.Pointer:
		switch incoming := incoming.(type) {
		case string, map[string]any:
			inst, err := c.getOrCreateInstance(currentContext, instances, typ, incoming)
			errs = appendErr(errs, err)
			if inst != emptyValue {
				target.Set(inst)
				return nil
			}
		}
	default:
		if newVal := convertTo(incoming, typ); newVal != emptyValue {
			target.Set(newVal)
		} else {
			errs = appendErr(errs, fmt.Errorf("unable to convert %#v to %s, dropping", incoming, typeName(typ)))
		}
	}
	return nil
}

func (c Context) setStructProps(currentContext *serializationContext, instances map[string]reflect.Value, instance reflect.Value, incoming map[string]any) error {
	var errs error
	typ := instance.Type()
	for typ.Kind() == reflect.Pointer {
		instance = instance.Elem()
		typ = instance.Type()
	}
	if typ.Kind() != reflect.Struct {
		return fmt.Errorf("unable to set struct properties on non-struct type: %#v", instance.Interface())
	}
	//tc := currentContext.typeToContext[typ]
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if skipField(f) {
			continue
		}

		fieldVal := instance.Field(i)

		// embedded struct, recursively call this function to get all struct values
		if f.Anonymous {
			err := c.setStructProps(currentContext, instances, fieldVal, incoming)
			if err != nil {
				return err
			}
		}

		propIRI := f.Tag.Get(GoIriTagName)
		if propIRI == "" {
			continue
		}
		incomingVal, ok := incoming[propIRI]
		if !ok {
			compactIRI := currentContext.iriToAlias[propIRI]
			if compactIRI != "" {
				incomingVal, ok = incoming[compactIRI]
			}
		}
		if !ok {
			continue
		}
		// don't set blank node IDs, these will be regenerated on output
		if propIRI == JsonIdProp {
			if str, ok := incomingVal.(string); ok {
				if fullIRI, ok := currentContext.aliasToIri[str]; ok {
					incomingVal = fullIRI
				}
			}
			if isBlankNodeID(incomingVal) {
				continue
			}
		}
		errs = appendErr(errs, c.setValue(currentContext, instances, fieldVal, incomingVal))
	}
	return errs
}

func (c Context) setSliceValue(currentContext *serializationContext, instances map[string]reflect.Value, target reflect.Value, incoming []any) error {
	var errs error
	sliceType := target.Type()
	if sliceType.Kind() != reflect.Slice {
		return fmt.Errorf("expected slice, got: %#v", target)
	}
	sz := len(incoming)
	if sz > 0 {
		elemType := sliceType.Elem()
		newSlice := reflect.MakeSlice(sliceType, 0, sz)
		for i := 0; i < sz; i++ {
			incomingValue := incoming[i]
			if incomingValue == nil {
				continue // don't allow null values
			}
			newItemValue, err := c.getOrCreateInstance(currentContext, instances, elemType, incomingValue)
			errs = appendErr(errs, err)
			if newItemValue != emptyValue {
				// validate we can actually set the type
				if newItemValue.Type().AssignableTo(elemType) {
					newSlice = reflect.Append(newSlice, newItemValue)
				}
			}
		}
		target.Set(newSlice)
	}
	return errs
}

func (c Context) findExternalReferenceType(currentContext *serializationContext, expectedType reflect.Type) (reflect.Type, bool) {
	tc := currentContext.typeToContext[expectedType]
	if tc != nil {
		return tc.typ, true
	}
	bestMatch := anyType
	for t := range currentContext.typeToContext {
		if t.Kind() != reflect.Struct {
			continue
		}
		// the type with the fewest fields assignable to the target is a good candidate to be an abstract type
		if reflect.PointerTo(t).AssignableTo(expectedType) && (bestMatch == anyType || bestMatch.NumField() > t.NumField()) {
			bestMatch = t
		}
	}
	if bestMatch != anyType {
		currentContext.typeToContext[expectedType] = &typeContext{
			typ: bestMatch,
		}
		return bestMatch, true
	}
	return anyType, false
}

func (c Context) typeContextForIri(iri string) *typeContext {
	for _, ctx := range c {
		tc := ctx.iriToType[iri]
		if tc != nil {
			return tc
		}
	}
	return nil
}

func skipField(field reflect.StructField) bool {
	return field.Type.Size() == 0
}

func typeName(t reflect.Type) string {
	switch {
	case isPointer(t):
		return "*" + typeName(t.Elem())
	case isSlice(t):
		return "[]" + typeName(t.Elem())
	case isMap(t):
		return "map[" + typeName(t.Key()) + "]" + typeName(t.Elem())
	case isPrimitive(t):
		return t.Name()
	}
	return path.Base(t.PkgPath()) + "." + t.Name()
}

func isSlice(t reflect.Type) bool {
	return t.Kind() == reflect.Slice
}

func isMap(t reflect.Type) bool {
	return t.Kind() == reflect.Map
}

func isPointer(t reflect.Type) bool {
	return t.Kind() == reflect.Pointer
}

func isPrimitive(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String,
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Float32,
		reflect.Float64,
		reflect.Bool:
		return true
	default:
		return false
	}
}

const (
	JsonIdProp      = "@id"
	JsonTypeProp    = "@type"
	JsonContextProp = "@context"
	JsonGraphProp   = "@graph"
	JsonVocabProp   = "@vocab"
	GoTypeField     = "_"
	GoIdField       = "ID"
	GoIriTagName    = "iri"
)

var (
	emptyValue reflect.Value
	anyType    = reflect.TypeOf((*any)(nil)).Elem()
)

type typeContext struct {
	ctx     *serializationContext
	typ     reflect.Type
	iri     string
	compact string
}

type serializationContext struct {
	contextUrl string
	//ldContext     map[string]any
	iriToType     map[string]*typeContext
	typeToContext map[reflect.Type]*typeContext
	aliasToIri    map[string]string
	aliasContext  map[string]string
	iriToAlias    map[string]string
}

func fieldByType[T any](t reflect.Type) (reflect.StructField, bool) {
	var v T
	typ := reflect.TypeOf(v)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Type == typ {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

func (ctx *serializationContext) registerType(instancePointer any) {
	t := reflect.TypeOf(instancePointer)
	t = baseType(t) // types may be passed as pointers, but we want the base types

	tc := ctx.typeToContext[t]
	if tc != nil {
		return // already registered
	}
	tc = &typeContext{
		ctx: ctx,
		typ: t,
	}
	meta, ok := fieldByType[Type](t)
	if ok {
		tc.iri = meta.Tag.Get(GoIriTagName)
		tc.compact = ctx.iriToAlias[tc.iri]
	}
	ctx.iriToType[tc.iri] = tc
	//ctx.iriToType[tc.compact] = tc
	ctx.typeToContext[t] = tc
}

// appendErr appends errors, flattening joined errors
func appendErr(err error, errs ...error) error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return errors.Join(append(joined.Unwrap(), errs...)...)
	}
	if err == nil {
		return errors.Join(errs...)
	}
	return errors.Join(append([]error{err}, errs...)...)
}

// baseType returns the base type if this is a pointer or interface
func baseType(t reflect.Type) reflect.Type {
	switch t.Kind() {
	case reflect.Pointer, reflect.Interface:
		return baseType(t.Elem())
	default:
		return t
	}
}

// isBlankNodeID indicates this is a blank node ID, e.g. _:CreationInfo-1
func isBlankNodeID(val any) bool {
	if val, ok := val.(string); ok {
		return strings.HasPrefix(val, "_:")
	}
	return false
}

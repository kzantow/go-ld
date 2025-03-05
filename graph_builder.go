package ld

import (
	"fmt"
	"github.com/piprate/json-gold/ld"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
)

type graphBuilder struct {
	ctx      *Context
	prefixes map[*serializationContext]string
	input    []any
	graph    []any // graph stores all the serialized object in the graph
	nextID   map[reflect.Type]int
	ids      map[reflect.Value]string
}

func (b *graphBuilder) toCompactMaps(graph ...any) (map[string]any, error) {
	expanded, err := b.toGraph(graph...)
	if err != nil {
		return nil, err
	}

	proc := ld.NewJsonLdProcessor()
	opts := ld.NewJsonLdOptions("")
	opts.DocumentLoader = offlineDocumentLoader{ctx: b.ctx}

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

	return proc.Compact(expanded, compactionContext, opts)
}

func (b *graphBuilder) toGraph(graph ...any) (map[string]any, error) {
	b.input = graph
	b.graph = nil
	// find the top-level context(s)
	var contextUrls []string
	for _, o := range graph {
		ctx := b.findContext(reflect.TypeOf(o))
		if ctx == nil {
			return nil, fmt.Errorf("unable to find context for: " + typeName(reflect.TypeOf(o)))
		}
		if _, ok := b.prefixes[ctx]; ok {
			continue
		}
		b.prefixes[ctx] = ""
		contextUrls = append(contextUrls, ctx.contextUrl)
	}

	if len(b.prefixes) == 0 {
		return nil, fmt.Errorf("no contexts found for graph")
	}

	var context any
	if len(b.prefixes) > 1 {
		// if there are multiple top-level contexts, set prefixes to be used
		slices.Sort(contextUrls)
		contextMap := map[string]string{}
		for i, u := range contextUrls {
			for c := range b.prefixes {
				if c.contextUrl == u {
					prefix := "ns" + strconv.Itoa(i)
					b.prefixes[c] = prefix
					contextMap[prefix] = c.contextUrl
				}
				break
			}
		}
		context = contextMap
	} else {
		for c := range b.prefixes {
			context = c.contextUrl
		}
	}

	for _, o := range graph {
		err := b.serializeToGraph(o)
		if err != nil {
			return nil, err
		}
	}

	return map[string]any{
		JsonContextProp: context,
		JsonGraphProp:   b.graph,
	}, nil
}

// serializeToGraph is the top-level call to serialize an map[string]any
func (b *graphBuilder) serializeToGraph(o any) (err error) {
	v := reflect.ValueOf(o)
	val, err := b.toValue(v)
	b.graph = append(b.graph, val)
	return err
}

func (b *graphBuilder) findContext(t reflect.Type) *serializationContext {
	t = baseType(t) // map[string]any may be a pointer, but we want the base types
	tc := b.ctx.typeToContext[t]
	if tc != nil {
		return tc.ctx
	}
	return nil
}

func (b *graphBuilder) toStructMap(v reflect.Value) (value any, err error) {
	t := v.Type()
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct type, got: %v", stringify(v))
	}

	// some structs like ExternalIRI do not have type information, but these types must have an @id field,
	// we will just output this value
	id, _ := getID(v)

	out := map[string]any{}
	if id != "" {
		out[JsonIdProp] = id
	}

	tc := b.ctx.typeToContext[t]

	if tc != nil {
		hasValues, err := b.writeStructProperties(tc.ctx, tc, v, out)
		// if we _only_ have an ID set and no other values just output the ID
		if !hasValues || err != nil {
			return out, err
		}
		out[JsonTypeProp] = tc.iri
	}
	return out, nil
}

func (b *graphBuilder) writeStructProperties(context *serializationContext, tc *typeContext, v reflect.Value, out map[string]any) (bool, error) {
	hasValues := false
	id := ""

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if skipField(f) {
			continue
		}

		// embedded struct, recursively call this function to get all struct values
		if f.Anonymous {
			superHasValues, err := b.writeStructProperties(context, tc, v.Field(i), out)
			if superHasValues {
				hasValues = true
			}
			if err != nil {
				return hasValues, err
			}
			continue
		}

		fieldV := v.Field(i)

		isId := isIdField(f)
		if !isId && !isRequired(f) && isEmpty(fieldV) {
			continue
		}

		val, err := b.toValue(fieldV)
		if err != nil {
			return hasValues, err
		}

		if isId {
			id, _ = val.(string)
			if id == "" {
				// if this struct does not have an ID set, and does not have multiple references,
				// it is output inline, it does not need an ID, but does need an ID
				// when it is moved to the top-level graph and referenced elsewhere
				if !b.hasMultipleReferences(v.Addr()) {
					continue
				}
				val, _ = b.ensureID(v.Addr())
				//} else {
				//	// compact named IRIs
				//	if alias := context.iriToAlias[id]; alias != "" {
				//		id = alias
				//	}
			}
		} else {
			hasValues = true
		}

		prop := f.Tag.Get(GoIriTagName)
		//alias := context.iriToAlias[prop]
		//if alias != "" {
		//	prop = alias
		//}

		out[prop] = val
	}

	return hasValues, nil
}

func isIdField(f reflect.StructField) bool {
	return f.Tag.Get(GoIriTagName) == JsonIdProp
}

func isEmpty(v reflect.Value) bool {
	return !v.IsValid() || v.IsZero()
}

func isRequired(f reflect.StructField) bool {
	return slices.Contains(strings.Split(f.Tag.Get("json"), ","), "omitempty")
}

func (b *graphBuilder) toValue(v reflect.Value) (any, error) {
	if !v.IsValid() {
		return nil, nil
	}

	t := v.Type()

	switch t {
	case timeType:
		return formatTime(v.Interface().(time.Time)), nil
	}

	switch t.Kind() {
	case reflect.Interface:
		return b.toValue(v.Elem())
	case reflect.Pointer:
		if v.IsNil() {
			return nil, nil
		}
		if !b.hasMultipleReferences(v) {
			return b.toValue(v.Elem())
		}
		return b.ensureID(v)
	case reflect.Struct:
		return b.toStructMap(v)
	case reflect.Slice:
		var out []any
		for i := 0; i < v.Len(); i++ {
			val, err := b.toValue(v.Index(i))
			if err != nil {
				return nil, err
			}
			out = append(out, val)
		}
		return out, nil
	case reflect.String:
		return v.String(), nil
	default:
		if v.CanInterface() {
			return v.Interface(), nil
		}
		return nil, fmt.Errorf("unable to convert value to maps: %v", stringify(v))
	}
}

func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

func (b *graphBuilder) ensureID(ptr reflect.Value) (string, error) {
	if ptr.Type().Kind() != reflect.Pointer {
		return "", fmt.Errorf("expected pointer, got: %v", stringify(ptr))
	}
	if id, ok := b.ids[ptr]; ok {
		return id, nil
	}

	v := ptr.Elem()
	t := v.Type()

	// check if the map[string]any has an ID set directly, and use that if so
	id, err := getID(v)
	if err != nil {
		return "", err
	}
	if id == "" {
		if b.nextID == nil {
			b.nextID = map[reflect.Type]int{}
		}
		nextID := b.nextID[t] + 1
		b.nextID[t] = nextID
		id = fmt.Sprintf("_:%s-%v", t.Name(), nextID)
	}
	b.ids[ptr] = id
	val, err := b.toValue(v)
	if err != nil {
		return "", err
	}
	b.graph = append(b.graph, val)
	return id, nil
}

// hasMultipleReferences returns true if the ptr value has multiple references in the input slice
func (b *graphBuilder) hasMultipleReferences(ptr reflect.Value) bool {
	if !ptr.IsValid() {
		return false
	}
	count := 0
	visited := map[reflect.Value]struct{}{}
	for _, v := range b.input {
		count += refCountR(ptr, visited, reflect.ValueOf(v))
		if count > 1 {
			return true
		}
	}
	return false
}

// refCount returns the reference count of the value in the container map[string]any
func refCount(find any, container any) int {
	visited := map[reflect.Value]struct{}{}
	ptrV := reflect.ValueOf(find)
	if !ptrV.IsValid() {
		return 0
	}
	return refCountR(ptrV, visited, reflect.ValueOf(container))
}

// refCountR recursively searches for the value, find, in the value v
func refCountR(find reflect.Value, visited map[reflect.Value]struct{}, v reflect.Value) int {
	if !v.IsValid() {
		return 0
	}
	if _, ok := visited[v]; ok {
		return 0
	}
	visited[v] = struct{}{}
	switch v.Kind() {
	case reflect.Interface:
		return refCountR(find, visited, v.Elem())
	case reflect.Pointer:
		if v.IsNil() {
			return 0
		}
		count := refCountR(find, visited, v.Elem())
		if find.Equal(v) {
			return count + 1
		}
		return count
	case reflect.Struct:
		count := 0
		for i := 0; i < v.NumField(); i++ {
			count += refCountR(find, visited, v.Field(i))
		}
		return count
	case reflect.Slice:
		count := 0
		for i := 0; i < v.Len(); i++ {
			count += refCountR(find, visited, v.Index(i))
		}
		return count
	default:
		return 0
	}
}

func stringify(o any) string {
	switch o := o.(type) {
	case reflect.Value:
		if !o.IsValid() {
			return "<invalid reflect value>"
		}
		if o.CanInterface() {
			return stringify(o.Interface())
		}
	case reflect.Type:
		return fmt.Sprintf("%s.%s", o.PkgPath(), o.Name())
	}
	return spew.Sdump(o)
	//if v, ok := o.(reflect.Value); ok {
	//	if !v.IsValid() {
	//		return "invalid value"
	//	}
	//	if !v.IsZero() && v.CanInterface() {
	//		o = v.Interface()
	//	}
	//}
	//return fmt.Sprintf("%#v", o)
}

var timeType = reflect.TypeOf(time.Time{})

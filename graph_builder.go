package ld

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

type graphBuilder struct {
	ldc      Context
	input    []any
	graph    []any
	idPrefix string
	nextID   map[reflect.Type]int
	ids      map[reflect.Value]string
}

func (b *graphBuilder) toGraph() []any {
	return b.graph
}

func (b *graphBuilder) add(o any) (context *serializationContext, err error) {
	v := reflect.ValueOf(o)
	if v.Type().Kind() != reflect.Pointer {
		if v.CanAddr() {
			v = v.Addr()
		} else {
			newV := reflect.New(v.Type())
			newV.Elem().Set(v)
			v = newV
		}
	}
	val, err := b.toValue(v)
	// objects with IDs get added to the graph during object traversal
	if _, isTopLevel := val.(map[string]any); isTopLevel && err == nil {
		b.graph = append(b.graph, val)
	}
	ctx := b.findContext(v.Type())
	return ctx, err
}

func (b *graphBuilder) findContext(t reflect.Type) *serializationContext {
	t = baseType(t) // object may be a pointer, but we want the base types
	for _, context := range b.ldc {
		for _, typ := range context.iriToType {
			if t == typ.typ {
				return context
			}
		}
	}
	return nil
}

func (b *graphBuilder) toStructMap(v reflect.Value) (value any, err error) {
	t := v.Type()
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct type, got: %v", stringify(v))
	}

	meta, ok := fieldByType[Type](t)
	if !ok {
		return nil, fmt.Errorf("struct does not have ld.Type metadata: %v", stringify(v))
	}

	iri := meta.Tag.Get(typeIriCompactTag)
	if iri == "" {
		iri = meta.Tag.Get(typeIriTag)
	}

	context := b.findContext(t)
	tc := context.typeToContext[t]

	typeProp := ldTypeProp
	if context.iriToAlias[ldTypeProp] != "" {
		typeProp = context.iriToAlias[ldTypeProp]
	}
	out := map[string]any{
		typeProp: iri,
	}

	id, hasValues, err := b.writeStructProperties(context, tc, v, out)
	// if we _only_ have an ID set and no other values just output the ID
	if err != nil {
		return id, err
	}
	if !hasValues && id != "" {
		return id, nil
	}
	return out, nil
}

func (b *graphBuilder) writeStructProperties(context *serializationContext, tc *typeContext, v reflect.Value, out map[string]any) (string, bool, error) {
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
			_, superHasValues, err := b.writeStructProperties(context, tc, v.Field(i), out)
			if superHasValues {
				hasValues = true
			}
			if err != nil {
				return "", hasValues, err
			}
			continue
		}

		prop := f.Tag.Get(propIriCompactTag)
		if prop == "" {
			prop = f.Tag.Get(propIriTag)
		}

		fieldV := v.Field(i)

		if !isRequired(f) && isEmpty(fieldV) {
			continue
		}

		val, err := b.toValue(fieldV)
		if err != nil {
			return "", hasValues, err
		}

		if isIdField(f) {
			id, _ = val.(string)
			if id == "" {
				// if this struct does not have an ID set, and does not have multiple references,
				// it is output inline, it does not need an ID, but does need an ID
				// when it is moved to the top-level graph and referenced elsewhere
				if !b.hasMultipleReferences(v.Addr()) {
					continue
				}
				val, _ = b.ensureID(v.Addr())
			} else if tc != nil {
				// compact named IRIs
				if _, ok := tc.iriToAlias[id]; ok {
					id = tc.iriToAlias[id]
				} else if _, ok := context.iriToAlias[id]; ok {
					id = context.iriToAlias[id]
				}
			}
		} else {
			hasValues = true
		}

		out[prop] = val
	}

	return id, hasValues, nil
}

func isIdField(f reflect.StructField) bool {
	return f.Tag.Get(propIriTag) == ldIDProp
}

func isEmpty(v reflect.Value) bool {
	return !v.IsValid() || v.IsZero()
}

func isRequired(f reflect.StructField) bool {
	if isIdField(f) {
		return true
	}
	required := f.Tag.Get(propIsRequiredTag)
	return required != "" && !strings.EqualFold(required, "false")
}

var timeTimeType = reflect.TypeOf(time.Time{})

func (b *graphBuilder) toValue(v reflect.Value) (any, error) {
	if !v.IsValid() {
		return nil, nil
	}

	t := v.Type()

	switch t {
	case timeTimeType:
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

	id, err := b.getID(v)
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

func (b *graphBuilder) getID(v reflect.Value) (string, error) {
	t := v.Type()
	if t.Kind() != reflect.Struct {
		return "", fmt.Errorf("expected struct, got: %v", stringify(v))
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if isIdField(f) {
			fv := v.Field(i)
			if f.Type.Kind() != reflect.String {
				return "", fmt.Errorf("invalid type for ID field %v in: %v", f, stringify(v))
			}
			return fv.String(), nil
		}
	}
	return "", nil
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

// refCount returns the reference count of the value in the container object
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
	if find.Equal(v) {
		return 1
	}
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
		return refCountR(find, visited, v.Elem())
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
	if v, ok := o.(reflect.Value); ok {
		if !v.IsValid() {
			return "invalid value"
		}
		if !v.IsZero() && v.CanInterface() {
			o = v.Interface()
		}
	}
	return fmt.Sprintf("%#v", o)
}

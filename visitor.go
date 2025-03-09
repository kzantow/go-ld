package ld

import (
	"fmt"
	"reflect"
	"strconv"
)

var StopTraversing = fmt.Errorf("stop-traversing-graph")

// VisitObjectGraph traverses the object graph, taking into account cycles, calling the visitor function for each
// step along the traversal, including field properties, pointer and subsequent struct values, elements in
// slices and both keys and values of maps, as well as some context such as the path within the graph and any
// containing struct field. The value is always able to have Interface() and Set() called.
func VisitObjectGraph(graph any, visitor func(path []string, field reflect.StructField, value reflect.Value) error) error {
	return visitObjectGraph(map[reflect.Value]struct{}{}, nil, reflect.StructField{}, reflect.ValueOf(graph), visitor)
}

func visitObjectGraph(visited map[reflect.Value]struct{}, path []string, field reflect.StructField, v reflect.Value, visitor func([]string, reflect.StructField, reflect.Value) error) error {
	if !v.IsValid() {
		return nil
	}
	if _, ok := visited[v]; ok {
		return nil
	}
	visited[v] = struct{}{}

	var err error
	if v.CanInterface() {
		err = visitor(path, field, v)
		if err == StopTraversing {
			return nil
		} else if err != nil {
			return err
		}
	}

	t := v.Type()

	//// do not double-visit values, certain types will result in `.Interface().(<thing>)` being satisfied
	//switch t.Kind() {
	//case reflect.Interface:
	//default:
	//	// only visit values that the visitor can call `.Interface()` on
	//	if v.CanInterface() {
	//	}
	//}

	switch t.Kind() {
	case reflect.Interface:
		return visitObjectGraph(visited, path, field, v.Elem(), visitor)
	case reflect.Pointer:
		return visitObjectGraph(visited, path, field, v.Elem(), visitor)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := t.Field(i)
			err = visitObjectGraph(visited, append(path, f.Name), f, v.Field(i), visitor)
			if err != nil {
				return err
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		if iter == nil {
			return nil
		}
		for iter.Next() {
			path := append(path, fmt.Sprintf("%v", iter.Key().Interface()))
			err = visitObjectGraph(visited, path, field, iter.Key(), visitor)
			if err != nil {
				return err
			}
			err = visitObjectGraph(visited, path, field, iter.Value(), visitor)
			if err != nil {
				return err
			}
		}
		//keys := v.MapKeys()
		//for i := 0; i < len(keys); i++ {
		//	key := keys[0]
		//	value := v.MapIndex(key)
		//	settableKey := reflect.New(reflect.TypeOf(key))
		//	settableKey = settableKey.Elem()
		//	settableKey.Set(key)
		//	err = visitObjectGraph(visited, settableKey, visitor)
		//	if err != nil {
		//		return err
		//	}
		//	if settableKey != key {
		//
		//	}
		//
		//	settableValue := reflect.New(reflect.TypeOf(key))
		//	settableValue = settableValue.Elem()
		//	settableValue.Set(value)
		//	err = visitObjectGraph(visited, settableValue, visitor)
		//	if err != nil {
		//		return err
		//	}
		//	if settableValue != value {
		//		v.SetMapIndex(key, settableValue)
		//	}
		//}
		//for i := 0; i < ; i++ {
		//	err = visitObjectGraph(visited, v.Index(i), visitor)
		//	if err != nil {
		//		return err
		//	}
		//}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			err = visitObjectGraph(visited, append(path, strconv.Itoa(i)), field, v.Index(i), visitor)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

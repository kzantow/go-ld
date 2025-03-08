package ld

import "reflect"

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

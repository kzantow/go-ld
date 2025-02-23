package gosh

import (
	"cmp"
	"maps"
	"slices"
)

func in[K comparable, V any](values map[K]V, value K) bool {
	_, ok := values[value]
	return ok
}

func keys[K cmp.Ordered, V any](values map[K]V) []K {
	out := make([]K, 0, len(values))
	for v := range maps.Keys(values) {
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

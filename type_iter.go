package ld

import "iter"

func TypeIter[T any, E any](values []E, cast func(any) *T) iter.Seq2[E, *T] {
	if values == nil {
		return func(yield func(E, *T) bool) {}
	}
	return func(yield func(E, *T) bool) {
		for _, value := range values {
			v := cast(value)
			if v != nil {
				if !yield(value, v) {
					return
				}
			}
		}
	}
}

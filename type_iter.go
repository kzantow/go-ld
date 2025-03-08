package ld

import "iter"

type TypeSeq[Element, View any] iter.Seq2[Element, View]

func (s TypeSeq[Element, View]) Elements() []Element {
	var out []Element
	for e := range s {
		out = append(out, e)
	}
	return out
}

func (s TypeSeq[Element, View]) Views() []View {
	var out []View
	for _, v := range s {
		out = append(out, v)
	}
	return out
}

func (s TypeSeq[Element, View]) Len() int {
	cnt := 0
	for _ = range s {
		cnt++
	}
	return cnt
}

func NewTypeSeq[T any, E any](values []E, cast func(any) *T) TypeSeq[E, *T] {
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

package ld

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
)

func Test_visitor(t *testing.T) {
	type typ struct {
		Name  string
		Slice []int
		Map   map[string]int
	}

	v := typ{
		Name:  "a name",
		Slice: []int{2, 4, 6},
		Map: map[string]int{
			"key1": 10,
			"key2": 20,
		},
	}

	err := VisitObjectGraph(v, func(path []string, field reflect.StructField, value reflect.Value) error {
		fmt.Println(path, field)
		if intSlice, ok := value.Interface().([]int); ok {
			fmt.Println(intSlice)
			intSlice = append(intSlice, 12)
			value.Set(reflect.ValueOf(intSlice))
		}
		if mapVal, ok := value.Interface().(map[string]int); ok {
			fmt.Println(mapVal)
			mapVal["new"] = 100
		}
		return nil
	})
	fmt.Println(v)
	require.NoError(t, err)
}

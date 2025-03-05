package ld

import (
	"fmt"
	"reflect"
	"time"
)

func getPrimitiveValue(typeIRI string, value any) (reflect.Value, error) {
	if value == nil {
		return emptyValue, nil
	}
	switch typeIRI {
	case "http://www.w3.org/2001/XMLSchema#string", "http://www.w3.org/2001/XMLSchema#anyURI":
		out := reflect.ValueOf(value)
		if out.Kind() == reflect.String {
			return out, nil
		}
		return emptyValue, fmt.Errorf("expected string, got: %v", value)

	case "http://www.w3.org/2001/XMLSchema#integer", "http://www.w3.org/2001/XMLSchema#positiveInteger", "http://www.w3.org/2001/XMLSchema#nonNegativeInteger":
		out := reflect.ValueOf(value)
		if out.Kind() == reflect.Int {
			return out, nil
		}
		return emptyValue, fmt.Errorf("expected int, got: %v", value)

	case "http://www.w3.org/2001/XMLSchema#boolean":
		out := reflect.ValueOf(value)
		if out.Kind() == reflect.Bool {
			return out, nil
		}
		return emptyValue, fmt.Errorf("expected bool, got: %v", value)

	case "http://www.w3.org/2001/XMLSchema#decimal":
		out := reflect.ValueOf(value)
		if out.Kind() == reflect.Float64 {
			return out, nil
		}
		return emptyValue, fmt.Errorf("expected float, got: %v", value)

	case "http://www.w3.org/2001/XMLSchema#dateTime", "http://www.w3.org/2001/XMLSchema#dateTimeStamp":
		val, ok := value.(string)
		if ok {
			v, err := time.Parse(time.RFC3339, val)
			if err != nil {
				// TODO more lenient parsing ?
				return emptyValue, err
			}
			return reflect.ValueOf(v), nil
		}
		return emptyValue, fmt.Errorf("expected RFC3339 formatted time, got: %v", value)
	}
	return emptyValue, nil
}

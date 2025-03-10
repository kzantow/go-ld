package ld

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
)

// Validator interface should be implemented by
type Validator interface {
	Validate() error
}

// ValidateGraph recursively calls all Validators on all values in the graph and returns a joined error
// of all the errors found
func ValidateGraph(graph any) error {
	var errs []error
	err := VisitObjectGraph(graph, func(path []any, value reflect.Value) error {
		// don't double-validate, this will be called with both pointer and struct references
		if elemIs[Validator](value) {
			return nil
		}
		if value.Type().Implements(validatorInterface) {
			if validator, ok := value.Interface().(Validator); ok {
				for _, err := range flattenErrors(validator.Validate()) {
					errs = append(errs, newValidationError(err, append(path[:], baseType(value.Type()))...))
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return JoinErrors(errs...)
}

type Validation[T any] func(value T) error

// JoinErrors returns errors.Join'd errors, taking into account nested joined errors, flattening these to a single joined set
func JoinErrors(errs ...error) error {
	var out []error
	for _, err := range errs {
		out = append(out, flattenErrors(err)...)
	}
	switch len(out) {
	case 0:
		return nil
	case 1:
		return out[0]
	default:
		return errors.Join(out...)
	}
}

func flattenErrors(err error) []error {
	var out []error
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range joined.Unwrap() {
			out = append(out, flattenErrors(e)...)
		}
	} else {
		if err != nil {
			return []error{err}
		}
	}
	return out
}

var validatorInterface = reflect.TypeOf((*Validator)(nil)).Elem()

type validationError struct {
	Path []any
	Err  error
}

func (v *validationError) String() string {
	path := ""
	for i := 0; i < len(v.Path); i++ {
		part := v.Path[i]
		switch p := part.(type) {
		case int:
			path += "[" + strconv.Itoa(p) + "]"
		case reflect.StructField:
			if !p.Anonymous {
				path += "." + p.Name
			}
		case reflect.Type:
			path += "<" + p.Name() + ">"
		default:
			path += "/" + fmt.Sprint(p)
		}
	}
	return path + ": " + v.Err.Error()
}

func (v *validationError) Error() string {
	return v.String()
}

func newValidationError(err error, path ...any) *validationError {
	// if the error is a validation error, prepend the path
	if vErr, ok := err.(*validationError); ok {
		vErr.Path = append(path, vErr.Path...)
		return vErr
	}
	return &validationError{
		Path: path,
		Err:  err,
	}
}

func ValidateValues[T comparable](values ...T) Validation[T] {
	return func(value T) error {
		if slices.Contains(values, value) {
			return nil
		}
		return fmt.Errorf("invalid value: '%v', expected: %v", value, values)
	}
}

func ValidateSlice[T any](validation Validation[T]) Validation[[]T] {
	return func(values []T) error {
		var errs []error
		for i, value := range values {
			err := validation(value)
			if err != nil {
				errs = append(errs, newValidationError(err, i))
			}
		}
		return JoinErrors(errs...)
	}
}

// ValidateProp is used by generated code, typically, to validate a specific property against all defined validations
// it might have such as whether it is required, matches a pattern, has enough elements, or is in a specific defined set.
// This function needs to be called with a struct reference and a pointer to the property, e.g. ValidateProp(obj, &obj.Prop, ...)
// due to looking up field tags and names based on the property reference. This will panic if called any other way.
func ValidateProp[T any](object any, property *T, validations ...Validation[T]) error {
	value := reflect.ValueOf(property)

	// object is always a pointer to the base struct
	o := reflect.ValueOf(object).Elem()
	var f reflect.StructField
	for i := 0; i < o.NumField(); i++ {
		if o.Field(i).Addr() == value {
			f = o.Type().Field(i)
			break
		}
	}
	if f.Name == "" {
		panic(fmt.Sprintf("property: %v not found in object: %v", property, object))
	}

	var errs []error
	if f.Anonymous { // inherited type validation
		if validator, ok := any(property).(Validator); ok {
			errs = flattenErrors(validator.Validate())
		}
	} else {
		// value is pointer to field, which always points to a valid elem
		if value.Elem().IsZero() {
			if f.Tag.Get("required") == "true" {
				return newValidationError(fmt.Errorf("required"), f)
			}
			return nil // don't process other validators, this is simply not set and not required
		}
	}

	for _, validation := range validations {
		err := validation(*property)
		if err != nil {
			errs = append(errs, newValidationError(err, f))
		}
	}
	return JoinErrors(errs...)
}

func ValidateID[T any](validation Validation[string]) Validation[T] {
	return func(value T) error {
		id, err := GetID(value)
		if err != nil {
			return err
		}
		return validation(id)
	}
}

func ValidateMinCount[S ~[]T, T any](minCount int) Validation[S] {
	return func(values S) error {
		if minCount > len(values) {
			return fmt.Errorf("must have at least: %v item(s), got: %v", minCount, len(values))
		}
		return nil
	}
}

func ValidateMaxCount[T any](maxCount int) Validation[[]T] {
	return func(values []T) error {
		if maxCount < len(values) {
			return fmt.Errorf("must have fewer than: %v item(s), got: %v", maxCount, len(values))
		}
		return nil
	}
}

func ValidateExpression(expression string) Validation[string] {
	return func(value string) error {
		r, err := regexp.Compile(expression)
		if err != nil {
			return fmt.Errorf("invalid expression: %s", expression)
		}
		s, _ := stringValue(value)
		if !r.MatchString(s) {
			return fmt.Errorf("must match expression: %s: value: %v", expression, value)
		}
		return nil
	}
}

func ValidateURI(property string, value any) Validation[string] {
	return func(value string) error {
		v := reflect.ValueOf(value)
		if !v.IsValid() {
			return fmt.Errorf("invalid value")
		}
		s, _ := stringValue(value)
		_, err := url.Parse(s)
		return err
	}
}

func stringValue(value any) (string, error) {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return "", fmt.Errorf("invalid value")
	}
	if v.Type().Kind() == reflect.String {
		return v.String(), nil
	}
	return "", fmt.Errorf("unable to get string")
}

func elemIs[T any](v reflect.Value) bool {
	switch v.Type().Kind() {
	case reflect.Pointer, reflect.Interface:
		e := v.Elem()
		if !e.IsValid() {
			return false
		}
		if !e.CanInterface() {
			return false
		}
		_, ok := e.Interface().(T)
		return ok
	}
	return false
}

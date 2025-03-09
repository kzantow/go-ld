package ld

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Validator interface should be implemented by
type Validator interface {
	Validate() error
}

func Validate(graph any) error {
	var errs []error
	err := VisitObjectGraph(graph, func(path []string, field reflect.StructField, value reflect.Value) error {
		// don't double-validate
		if elemIs[Validator](value) {
			return nil
		}
		if value.Type().Implements(validatorInterface) {
			if validator, ok := value.Interface().(Validator); ok {
				err := validator.Validate()
				if err != nil {
					errs = appendJoinedErrs(errs, newValidationError(err, append(path, field.Name)...))
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

// JoinErrors returns errors.Join'd errors, taking into account nested, flattening these to a single joined set
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

func appendJoinedErrs(errs []error, err error) []error {
	if err == nil {
		return errs
	}
	if e, ok := err.(interface{ Unwrap() []error }); ok {
		return append(errs, e.Unwrap()...)
	}
	return append(errs, err)
}

type validationError struct {
	Path []string
	Err  error
}

func (v validationError) String() string {
	return strings.Join(v.Path, "/") + ": " + v.Err.Error()
}

func (v validationError) Error() string {
	return v.String()
}

func newValidationError(err error, path ...string) validationError {
	return validationError{
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
				errs = append(errs, newValidationError(err, strconv.Itoa(i)))
			}
		}
		return JoinErrors(errs...)
	}
}

func ValidateProp[T any](object any, property *T, validations ...Validation[T]) error {
	value := reflect.ValueOf(property)
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

	if value.IsZero() {
		if f.Tag.Get("required") != "true" {
			return fmt.Errorf("%s is required", f.Name)
		}
		return nil // don't process other validators, this is simply not set
	}

	var errs []error
	for _, validation := range validations {
		err := validation(*property)
		if err != nil {
			errs = append(errs, err)
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

type slice[T any] interface {
	~[]T
}

func ValidateMinCount[S slice[T], T any](minCount int) Validation[S] {
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

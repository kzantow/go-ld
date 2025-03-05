package ld

import (
	"fmt"
	"slices"
	"strings"
)

type ValidationError struct {
	Path []string
	Err  error
}

func (v ValidationError) String() string {
	return strings.Join(v.Path, ".") + ": " + v.Err.Error()
}

func (v ValidationError) Error() string {
	return v.String()
}

func NewValidationError(path []string, err error) ValidationError {
	return ValidationError{
		Path: path,
		Err:  err,
	}
}

type Validator interface {
	Validate(visited map[any]struct{}, path ...string) []ValidationError
}

func ValidateInValues(path []string, value string, valid ...string) []ValidationError {
	if slices.Contains(valid, value) {
		return nil
	}
	return []ValidationError{NewValidationError(path, fmt.Errorf("invalid value: '%v', expected: %v", value, valid))}
}

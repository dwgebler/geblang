package formatter

import (
	"reflect"

	"geblang/internal/token"
)

var tokenType = reflect.TypeOf(token.Token{})

// ignoredASTFields carry layout or presentation only; formatting may legitimately change them.
var ignoredASTFields = map[string]bool{
	"Doc":                true,
	"Parenthesized":      true,
	"DataclassDesugared": true,
}

// astEquivalent compares two AST subtrees field by field, so a node the printer drops is caught even when its String() form is lossy.
func astEquivalent(a, b reflect.Value) bool {
	if a.IsValid() != b.IsValid() {
		return false
	}
	if !a.IsValid() {
		return true
	}
	if a.Kind() == reflect.Interface || a.Kind() == reflect.Ptr {
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil() || isEmptyValue(a) && isEmptyValue(b)
		}
		if a.Kind() == reflect.Interface && a.Elem().Type() != b.Elem().Type() {
			return false
		}
		return astEquivalent(a.Elem(), b.Elem())
	}
	if a.Type() != b.Type() {
		return false
	}
	switch a.Kind() {
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			field := a.Type().Field(i)
			if skipASTField(field) {
				continue
			}
			if !astEquivalent(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array:
		if a.Len() != b.Len() {
			return false
		}
		for i := 0; i < a.Len(); i++ {
			if !astEquivalent(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if a.Len() != b.Len() {
			return false
		}
		for _, k := range a.MapKeys() {
			if !astEquivalent(a.MapIndex(k), b.MapIndex(k)) {
				return false
			}
		}
		return true
	default:
		return a.Interface() == b.Interface()
	}
}

// isEmptyValue treats a nil pointer and an empty node (e.g. a nil vs empty block) as the same shape.
func isEmptyValue(v reflect.Value) bool {
	if v.IsNil() {
		return true
	}
	e := v.Elem()
	if e.Kind() == reflect.Struct {
		for i := 0; i < e.NumField(); i++ {
			field := e.Type().Field(i)
			if skipASTField(field) {
				continue
			}
			if !e.Field(i).IsZero() && !(e.Field(i).Kind() == reflect.Slice && e.Field(i).Len() == 0) {
				return false
			}
		}
		return true
	}
	return false
}

func skipASTField(field reflect.StructField) bool {
	return !field.IsExported() || field.Type == tokenType || ignoredASTFields[field.Name]
}

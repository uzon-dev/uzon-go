// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"math/big"
	"reflect"
	"strings"
)

// Marshal converts a Go value to UZON document text.
// If the Go value maps to a struct, the result is top-level bindings
// without surrounding braces. Otherwise it is a single expression.
func Marshal(v any) ([]byte, error) {
	val, err := ValueOf(v)
	if err != nil {
		return nil, err
	}
	if val.Kind == KindStruct {
		e := &emitter{indent: 0}
		e.emitDocument(val)
		return []byte(e.sb.String()), nil
	}
	return val.Marshal()
}

// ValueOf converts a Go value to a *Value using reflection.
// Supported Go types: bool, integers, floats, string, slice/array, map, struct.
func ValueOf(v any) (*Value, error) {
	return goToValue(reflect.ValueOf(v))
}

func goToValue(rv reflect.Value) (*Value, error) {
	if !rv.IsValid() {
		return Null(), nil
	}
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return Null(), nil
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Bool:
		return Bool(rv.Bool()), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return Int(rv.Int()), nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Uint(rv.Uint()), nil

	case reflect.Float32:
		f := new(big.Float).SetPrec(24).SetFloat64(rv.Float())
		return &Value{Kind: KindFloat, Float: f, Type: &TypeInfo{BaseType: "f32", BitSize: 32}}, nil

	case reflect.Float64:
		return Float64(rv.Float()), nil

	case reflect.String:
		return String(rv.String()), nil

	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return NewList(nil, nil), nil
		}
		elems := make([]*Value, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			val, err := goToValue(rv.Index(i))
			if err != nil {
				return nil, err
			}
			elems[i] = val
		}
		return NewList(elems, nil), nil

	case reflect.Map:
		if rv.IsNil() {
			return NewStruct(), nil
		}
		var fields []Field
		iter := rv.MapRange()
		for iter.Next() {
			key := fmt.Sprint(iter.Key().Interface())
			val, err := goToValue(iter.Value())
			if err != nil {
				return nil, err
			}
			fields = append(fields, Field{Name: key, Value: val})
		}
		sv := &StructValue{Fields: fields}
		return &Value{Kind: KindStruct, Struct: sv}, nil

	case reflect.Struct:
		return structToValue(rv)

	default:
		return nil, fmt.Errorf("unsupported Go type: %s", rv.Type())
	}
}

// structToValue converts a Go struct to a UZON struct Value.
// Field naming follows the "uzon" tag, then "json" tag, then snake_case.
func structToValue(rv reflect.Value) (*Value, error) {
	rt := rv.Type()
	var fields []Field

	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		name := fieldName(sf)
		if name == "-" {
			continue
		}

		fv := rv.Field(i)

		// Handle omitempty: skip zero-valued fields
		tag := sf.Tag.Get("uzon")
		if tag != "" {
			parts := strings.Split(tag, ",")
			for _, p := range parts[1:] {
				if p == "omitempty" && fv.IsZero() {
					name = ""
				}
			}
		}
		if name == "" {
			continue
		}

		val, err := goToValue(fv)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", sf.Name, err)
		}
		fields = append(fields, Field{Name: name, Value: val})
	}

	sv := &StructValue{Fields: fields}
	return &Value{Kind: KindStruct, Struct: sv}, nil
}

// fieldName determines the UZON field name for a struct field.
// Priority: uzon tag > json tag > snake_case of field name.
func fieldName(sf reflect.StructField) string {
	tag := sf.Tag.Get("uzon")
	if tag != "" {
		name := strings.Split(tag, ",")[0]
		if name != "" {
			return name
		}
	}
	tag = sf.Tag.Get("json")
	if tag != "" {
		name := strings.Split(tag, ",")[0]
		if name != "" {
			return name
		}
	}
	return toSnakeCase(sf.Name)
}

// toSnakeCase converts CamelCase to snake_case for struct field naming.
func toSnakeCase(s string) string {
	runes := []rune(s)
	var result strings.Builder
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				prev := runes[i-1]
				if prev >= 'a' && prev <= 'z' {
					result.WriteByte('_')
				} else if prev >= 'A' && prev <= 'Z' && i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z' {
					result.WriteByte('_')
				}
			}
			result.WriteRune(r + ('a' - 'A'))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

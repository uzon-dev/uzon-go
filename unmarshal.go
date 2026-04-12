// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"reflect"
)

// Unmarshal parses UZON text and decodes it into a Go value.
// The target must be a non-nil pointer.
func Unmarshal(data []byte, v any) error {
	val, err := Parse(data)
	if err != nil {
		return err
	}
	return val.Decode(v)
}

// UnmarshalFile reads a .uzon file, parses, and decodes it into a Go value.
func UnmarshalFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	val, err := parseAndEval(data, path)
	if err != nil {
		return err
	}
	return val.Decode(v)
}

// Decode decodes a *Value into a Go value.
// The target must be a non-nil pointer.
func (val *Value) Decode(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("uzon.Decode: argument must be a non-nil pointer")
	}
	return valueToGo(val, rv.Elem())
}

// valueToGo recursively decodes a *Value into a reflect.Value target.
func valueToGo(val *Value, rv reflect.Value) error {
	if val.Kind == KindNull {
		if rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface ||
			rv.Kind() == reflect.Slice || rv.Kind() == reflect.Map {
			rv.Set(reflect.Zero(rv.Type()))
		}
		return nil
	}
	if val.Kind == KindUndefined {
		return nil // leave as zero value
	}

	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		return valueToGo(val, rv.Elem())
	}

	// interface{} — convert to native Go types
	if rv.Kind() == reflect.Interface {
		goVal := valueToInterface(val)
		rv.Set(reflect.ValueOf(goVal))
		return nil
	}

	switch val.Kind {
	case KindBool:
		if rv.Kind() == reflect.Bool {
			rv.SetBool(val.Bool)
			return nil
		}
	case KindInt:
		return setInt(val.Int, rv)
	case KindFloat:
		return setFloat(val, rv)
	case KindString:
		if rv.Kind() == reflect.String {
			rv.SetString(val.Str)
			return nil
		}
	case KindStruct:
		return decodeStruct(val, rv)
	case KindList:
		return decodeList(val, rv)
	case KindTuple:
		return decodeTuple(val, rv)
	case KindEnum:
		if rv.Kind() == reflect.String {
			rv.SetString(val.Enum.Variant)
			return nil
		}
	case KindTaggedUnion:
		return valueToGo(val.TaggedUnion.Inner, rv)
	case KindUnion:
		return valueToGo(val.Union.Inner, rv)
	}
	return fmt.Errorf("cannot decode %s into %s", val.Kind, rv.Type())
}

func setInt(n *big.Int, rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if !n.IsInt64() {
			return fmt.Errorf("value %s overflows %s", n, rv.Type())
		}
		rv.SetInt(n.Int64())
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if !n.IsUint64() || n.Sign() < 0 {
			return fmt.Errorf("value %s overflows %s", n, rv.Type())
		}
		rv.SetUint(n.Uint64())
		return nil
	case reflect.Float32, reflect.Float64:
		f, _ := new(big.Float).SetInt(n).Float64()
		rv.SetFloat(f)
		return nil
	}
	return fmt.Errorf("cannot decode integer into %s", rv.Type())
}

func setFloat(val *Value, rv reflect.Value) error {
	if val.FloatIsNaN {
		switch rv.Kind() {
		case reflect.Float32, reflect.Float64:
			rv.SetFloat(math.NaN())
			return nil
		default:
			return fmt.Errorf("cannot decode NaN into %s", rv.Type())
		}
	}
	fv, _ := val.Float.Float64()
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		rv.SetFloat(fv)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		rv.SetInt(int64(fv))
		return nil
	}
	return fmt.Errorf("cannot decode float into %s", rv.Type())
}

func decodeStruct(val *Value, rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Struct:
		return decodeStructToStruct(val, rv)
	case reflect.Map:
		return decodeStructToMap(val, rv)
	}
	return fmt.Errorf("cannot decode struct into %s", rv.Type())
}

func decodeStructToStruct(val *Value, rv reflect.Value) error {
	rt := rv.Type()
	fieldMap := buildFieldMap(rt)

	for _, f := range val.Struct.Fields {
		idx, ok := fieldMap[f.Name]
		if !ok {
			continue
		}
		if err := valueToGo(f.Value, rv.Field(idx)); err != nil {
			return fmt.Errorf("field %s: %w", f.Name, err)
		}
	}
	return nil
}

func buildFieldMap(rt reflect.Type) map[string]int {
	m := make(map[string]int, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		name := fieldName(sf)
		if name != "-" {
			m[name] = i
		}
	}
	return m
}

func decodeStructToMap(val *Value, rv reflect.Value) error {
	if rv.IsNil() {
		rv.Set(reflect.MakeMap(rv.Type()))
	}
	keyType := rv.Type().Key()
	valType := rv.Type().Elem()

	for _, f := range val.Struct.Fields {
		key := reflect.New(keyType).Elem()
		if keyType.Kind() == reflect.String {
			key.SetString(f.Name)
		} else {
			return fmt.Errorf("map key type %s not supported", keyType)
		}
		elem := reflect.New(valType).Elem()
		if err := valueToGo(f.Value, elem); err != nil {
			return fmt.Errorf("map key %q: %w", f.Name, err)
		}
		rv.SetMapIndex(key, elem)
	}
	return nil
}

func decodeList(val *Value, rv reflect.Value) error {
	if rv.Kind() != reflect.Slice {
		return fmt.Errorf("cannot decode list into %s", rv.Type())
	}
	slice := reflect.MakeSlice(rv.Type(), len(val.List.Elements), len(val.List.Elements))
	for i, elem := range val.List.Elements {
		if err := valueToGo(elem, slice.Index(i)); err != nil {
			return fmt.Errorf("list[%d]: %w", i, err)
		}
	}
	rv.Set(slice)
	return nil
}

func decodeTuple(val *Value, rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Slice:
		slice := reflect.MakeSlice(rv.Type(), len(val.Tuple.Elements), len(val.Tuple.Elements))
		for i, elem := range val.Tuple.Elements {
			if err := valueToGo(elem, slice.Index(i)); err != nil {
				return fmt.Errorf("tuple[%d]: %w", i, err)
			}
		}
		rv.Set(slice)
		return nil
	case reflect.Array:
		for i := 0; i < rv.Len() && i < len(val.Tuple.Elements); i++ {
			if err := valueToGo(val.Tuple.Elements[i], rv.Index(i)); err != nil {
				return fmt.Errorf("tuple[%d]: %w", i, err)
			}
		}
		return nil
	case reflect.Struct:
		for i := 0; i < rv.NumField() && i < len(val.Tuple.Elements); i++ {
			if rv.Type().Field(i).IsExported() {
				if err := valueToGo(val.Tuple.Elements[i], rv.Field(i)); err != nil {
					return fmt.Errorf("tuple[%d]: %w", i, err)
				}
			}
		}
		return nil
	}
	return fmt.Errorf("cannot decode tuple into %s", rv.Type())
}

// valueToInterface converts a Value to a plain Go interface{} value.
func valueToInterface(val *Value) any {
	switch val.Kind {
	case KindNull, KindUndefined:
		return nil
	case KindBool:
		return val.Bool
	case KindInt:
		if val.Int.IsInt64() {
			return val.Int.Int64()
		}
		return val.Int
	case KindFloat:
		if val.FloatIsNaN {
			return math.NaN()
		}
		f, _ := val.Float.Float64()
		return f
	case KindString:
		return val.Str
	case KindStruct:
		m := make(map[string]any, len(val.Struct.Fields))
		for _, f := range val.Struct.Fields {
			m[f.Name] = valueToInterface(f.Value)
		}
		return m
	case KindList:
		s := make([]any, len(val.List.Elements))
		for i, e := range val.List.Elements {
			s[i] = valueToInterface(e)
		}
		return s
	case KindTuple:
		s := make([]any, len(val.Tuple.Elements))
		for i, e := range val.Tuple.Elements {
			s[i] = valueToInterface(e)
		}
		return s
	case KindEnum:
		return val.Enum.Variant
	case KindTaggedUnion:
		return map[string]any{
			"_tag":   val.TaggedUnion.Tag,
			"_value": valueToInterface(val.TaggedUnion.Inner),
		}
	case KindUnion:
		return valueToInterface(val.Union.Inner)
	default:
		return nil
	}
}
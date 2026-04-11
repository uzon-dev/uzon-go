// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import "fmt"

// Bind creates a Field with automatic Go-to-Value conversion.
// If v is already a *Value, it is used directly. Otherwise, v is converted
// using the same rules as ValueOf (bool, int, float, string, slice, map, struct).
// Panics if v cannot be converted.
//
//	uzon.Bind("host", "localhost")          // string
//	uzon.Bind("port", 8080)                 // int
//	uzon.Bind("server", uzon.NewStruct(...)) // existing *Value
func Bind(name string, v any) Field {
	return Field{Name: name, Value: autoWrap(v)}
}

// ListOf creates a list Value with automatic Go-to-Value conversion
// of each element.
//
//	uzon.ListOf("a", "b", "c")   // list of strings
//	uzon.ListOf(1, 2, 3)         // list of ints
func ListOf(elems ...any) *Value {
	vals := make([]*Value, len(elems))
	for i, e := range elems {
		vals[i] = autoWrap(e)
	}
	return NewList(vals, nil)
}

// TupleOf creates a tuple Value with automatic Go-to-Value conversion
// of each element.
//
//	uzon.TupleOf("hello", 42, true)  // mixed-type tuple
func TupleOf(elems ...any) *Value {
	vals := make([]*Value, len(elems))
	for i, e := range elems {
		vals[i] = autoWrap(e)
	}
	return NewTuple(vals...)
}

// autoWrap converts a Go value to *Value. If v is already a *Value, it is
// returned directly. Otherwise, ValueOf is used. Panics on unsupported types.
func autoWrap(v any) *Value {
	if v == nil {
		return Null()
	}
	if val, ok := v.(*Value); ok {
		return val
	}
	val, err := ValueOf(v)
	if err != nil {
		panic(fmt.Sprintf("uzon: cannot convert %T to Value: %v", v, err))
	}
	return val
}

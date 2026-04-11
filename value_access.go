// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// --- Type predicates ---

// IsNull reports whether v is a null value.
func (v *Value) IsNull() bool { return v.Kind == KindNull }

// IsUndefined reports whether v is an undefined value.
func (v *Value) IsUndefined() bool { return v.Kind == KindUndefined }

// --- Unwrap accessors ---

// AsInt returns the integer value as int64.
// Returns (0, false) if v is not KindInt or the value overflows int64.
func (v *Value) AsInt() (int64, bool) {
	if v.Kind != KindInt {
		return 0, false
	}
	if !v.Int.IsInt64() {
		return 0, false
	}
	return v.Int.Int64(), true
}

// AsFloat returns the float value as float64.
// Returns (NaN, true) for NaN values. Returns (0, false) if v is not KindFloat.
func (v *Value) AsFloat() (float64, bool) {
	if v.Kind != KindFloat {
		return 0, false
	}
	if v.FloatIsNaN {
		return math.NaN(), true
	}
	f, _ := v.Float.Float64()
	return f, true
}

// AsString returns the string value.
// Returns ("", false) if v is not KindString.
func (v *Value) AsString() (string, bool) {
	if v.Kind != KindString {
		return "", false
	}
	return v.Str, true
}

// AsBool returns the boolean value.
// Returns (false, false) if v is not KindBool.
func (v *Value) AsBool() (bool, bool) {
	if v.Kind != KindBool {
		return false, false
	}
	return v.Bool, true
}

// --- Undefined coalescing ---

// OrElse returns v if it is not undefined, otherwise returns fallback.
func (v *Value) OrElse(fallback *Value) *Value {
	if v.Kind == KindUndefined {
		return fallback
	}
	return v
}

// --- Collection accessors ---

// Len returns the number of elements or fields in a collection value.
// Returns the string length (in runes) for KindString.
// Returns 0 for non-collection kinds.
func (v *Value) Len() int {
	switch v.Kind {
	case KindStruct:
		return len(v.Struct.Fields)
	case KindList:
		return len(v.List.Elements)
	case KindTuple:
		return len(v.Tuple.Elements)
	case KindString:
		return len([]rune(v.Str))
	default:
		return 0
	}
}

// Keys returns the field names of a struct value in source order.
// Returns nil for non-struct kinds.
func (v *Value) Keys() []string {
	if v.Kind != KindStruct {
		return nil
	}
	keys := make([]string, len(v.Struct.Fields))
	for i, f := range v.Struct.Fields {
		keys[i] = f.Name
	}
	return keys
}

// --- Path-based access ---

// GetPath returns the nested value at the given dot-separated path.
// Supports struct field names, and numeric indices for tuples and lists.
// Tagged unions and unions are transparently unwrapped during traversal.
// Returns nil if any segment is not found or the path is invalid.
//
//	v.GetPath("server.host")        // struct field access
//	v.GetPath("pair.0")             // tuple index access
//	v.GetPath("items.2")            // list index access
func (v *Value) GetPath(path string) *Value {
	if path == "" {
		return v
	}
	parts := strings.Split(path, ".")
	cur := v
	for _, p := range parts {
		if cur == nil {
			return nil
		}
		// Transparently unwrap tagged unions and unions
		for cur.Kind == KindTaggedUnion {
			cur = cur.TaggedUnion.Inner
		}
		for cur.Kind == KindUnion {
			cur = cur.Union.Inner
		}

		switch cur.Kind {
		case KindStruct:
			cur = cur.Struct.Get(p)
		case KindTuple:
			idx, err := strconv.Atoi(p)
			if err != nil || idx < 0 || idx >= len(cur.Tuple.Elements) {
				return nil
			}
			cur = cur.Tuple.Elements[idx]
		case KindList:
			idx, err := strconv.Atoi(p)
			if err != nil || idx < 0 || idx >= len(cur.List.Elements) {
				return nil
			}
			cur = cur.List.Elements[idx]
		default:
			return nil
		}
	}
	return cur
}

// --- Merge ---

// Merge returns a new struct with all fields from a, overridden by fields from b.
// Fields from a appear first in their original order; new fields from b are appended.
// Both operands must be KindStruct.
func Merge(a, b *Value) (*Value, error) {
	if a.Kind != KindStruct || b.Kind != KindStruct {
		return nil, fmt.Errorf("Merge requires struct operands, got %s and %s", a.Kind, b.Kind)
	}
	result := &StructValue{}
	for _, f := range a.Struct.Fields {
		result.Set(f.Name, f.Value)
	}
	for _, f := range b.Struct.Fields {
		result.Set(f.Name, f.Value)
	}
	return &Value{Kind: KindStruct, Struct: result}, nil
}

// --- fmt.Stringer ---

// String returns the UZON text representation of the value.
// Implements fmt.Stringer.
func (v *Value) String() string {
	b, err := v.Marshal()
	if err != nil {
		return fmt.Sprintf("<%s>", v.Kind)
	}
	return string(b)
}

// --- encoding.TextMarshaler ---

// MarshalText returns the UZON text representation as bytes.
// Implements encoding.TextMarshaler.
func (v *Value) MarshalText() ([]byte, error) {
	return v.Marshal()
}

// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"math"
	"math/big"
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
// Returns the string length in Unicode scalar values (codepoints) for KindString (§5.16.1).
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
// Both operands must be KindStruct. Operands may be *Value or Go values (auto-wrapped).
// Field values are cloned so the result is independent of the originals.
func Merge(a, b any) (*Value, error) {
	av, bv, err := coerceTwo("Merge", a, b)
	if err != nil {
		return nil, err
	}
	if av.Kind != KindStruct || bv.Kind != KindStruct {
		return nil, fmt.Errorf("Merge requires struct operands, got %s and %s", av.Kind, bv.Kind)
	}
	result := &StructValue{}
	for _, f := range av.Struct.Fields {
		result.Set(f.Name, Clone(f.Value))
	}
	for _, f := range bv.Struct.Fields {
		result.Set(f.Name, Clone(f.Value))
	}
	return &Value{Kind: KindStruct, Struct: result}, nil
}

// --- DeepMerge ---

// DeepMerge returns a new struct with all fields from a, deep-merged with fields from b.
// When both a and b have a field with the same name and both values are structs,
// the values are recursively deep-merged. Tagged unions and unions wrapping structs
// are transparently unwrapped. Otherwise, b's value takes precedence.
// Both operands must be KindStruct. Operands may be *Value or Go values (auto-wrapped).
func DeepMerge(a, b any) (*Value, error) {
	av, bv, err := coerceTwo("DeepMerge", a, b)
	if err != nil {
		return nil, err
	}
	return deepMergeValues(av, bv)
}

func deepMergeValues(a, b *Value) (*Value, error) {
	if a.Kind != KindStruct || b.Kind != KindStruct {
		return nil, fmt.Errorf("DeepMerge requires struct operands, got %s and %s", a.Kind, b.Kind)
	}
	result := &StructValue{}
	for _, f := range a.Struct.Fields {
		result.Set(f.Name, Clone(f.Value))
	}
	for _, f := range b.Struct.Fields {
		existing := result.Get(f.Name)
		eInner := unwrapToStruct(existing)
		fInner := unwrapToStruct(f.Value)
		if eInner != nil && fInner != nil {
			merged, err := deepMergeValues(eInner, fInner)
			if err != nil {
				return nil, err
			}
			result.Set(f.Name, merged)
		} else {
			result.Set(f.Name, Clone(f.Value))
		}
	}
	return &Value{Kind: KindStruct, Struct: result}, nil
}

// unwrapToStruct returns the value if it is a struct, unwrapping tagged unions
// and unions. Returns nil if the inner value is not a struct.
func unwrapToStruct(v *Value) *Value {
	if v == nil {
		return nil
	}
	for v.Kind == KindTaggedUnion {
		v = v.TaggedUnion.Inner
	}
	for v.Kind == KindUnion {
		v = v.Union.Inner
	}
	if v.Kind == KindStruct {
		return v
	}
	return nil
}

// --- SetPath ---

// SetPath sets the nested value at the given dot-separated path.
// If val is not already a *Value, it is auto-wrapped using the same rules as Bind.
// Intermediate path segments must already exist. Struct fields are created if
// they don't exist at the leaf level.
//
//	v.SetPath("server.host", "newhost")
//	v.SetPath("items.0", uzon.Int(99))
func (v *Value) SetPath(path string, val any) error {
	var target *Value
	if vp, ok := val.(*Value); ok {
		target = vp
	} else if val == nil {
		target = Null()
	} else {
		var err error
		target, err = ValueOf(val)
		if err != nil {
			return fmt.Errorf("SetPath: %w", err)
		}
	}

	if path == "" {
		return fmt.Errorf("SetPath: empty path")
	}

	parts := strings.Split(path, ".")
	cur := v
	for _, p := range parts[:len(parts)-1] {
		if cur == nil {
			return fmt.Errorf("SetPath: nil value at path segment %q", p)
		}
		for cur.Kind == KindTaggedUnion {
			cur = cur.TaggedUnion.Inner
		}
		for cur.Kind == KindUnion {
			cur = cur.Union.Inner
		}

		switch cur.Kind {
		case KindStruct:
			next := cur.Struct.Get(p)
			if next == nil {
				return fmt.Errorf("SetPath: field %q not found", p)
			}
			cur = next
		case KindTuple:
			idx, err := strconv.Atoi(p)
			if err != nil || idx < 0 || idx >= len(cur.Tuple.Elements) {
				return fmt.Errorf("SetPath: invalid tuple index %q", p)
			}
			cur = cur.Tuple.Elements[idx]
		case KindList:
			idx, err := strconv.Atoi(p)
			if err != nil || idx < 0 || idx >= len(cur.List.Elements) {
				return fmt.Errorf("SetPath: invalid list index %q", p)
			}
			cur = cur.List.Elements[idx]
		default:
			return fmt.Errorf("SetPath: cannot traverse %s at %q", cur.Kind, p)
		}
	}

	last := parts[len(parts)-1]
	for cur.Kind == KindTaggedUnion {
		cur = cur.TaggedUnion.Inner
	}
	for cur.Kind == KindUnion {
		cur = cur.Union.Inner
	}

	switch cur.Kind {
	case KindStruct:
		cur.Struct.Set(last, target)
	case KindTuple:
		idx, err := strconv.Atoi(last)
		if err != nil || idx < 0 || idx >= len(cur.Tuple.Elements) {
			return fmt.Errorf("SetPath: invalid tuple index %q", last)
		}
		cur.Tuple.Elements[idx] = target
	case KindList:
		idx, err := strconv.Atoi(last)
		if err != nil || idx < 0 || idx >= len(cur.List.Elements) {
			return fmt.Errorf("SetPath: invalid list index %q", last)
		}
		cur.List.Elements[idx] = target
	default:
		return fmt.Errorf("SetPath: cannot set on %s", cur.Kind)
	}
	return nil
}

// --- Clone ---

// Clone returns a deep copy of the value tree.
// The cloned value is completely independent of the original.
// TypeInfo is shared (read-only). Functions are shared (closures).
func Clone(v *Value) *Value {
	if v == nil {
		return nil
	}
	c := &Value{
		Kind:       v.Kind,
		Type:       v.Type,
		Adoptable:  v.Adoptable,
		Bool:       v.Bool,
		FloatIsNaN: v.FloatIsNaN,
		Str:        v.Str,
	}
	switch v.Kind {
	case KindInt:
		c.Int = new(big.Int).Set(v.Int)
	case KindFloat:
		if v.Float != nil {
			c.Float = new(big.Float).Copy(v.Float)
		}
	case KindStruct:
		fields := make([]Field, len(v.Struct.Fields))
		for i, f := range v.Struct.Fields {
			fields[i] = Field{Name: f.Name, Value: Clone(f.Value)}
		}
		c.Struct = &StructValue{Fields: fields}
	case KindTuple:
		elems := make([]*Value, len(v.Tuple.Elements))
		for i, e := range v.Tuple.Elements {
			elems[i] = Clone(e)
		}
		c.Tuple = &TupleValue{Elements: elems}
	case KindList:
		elems := make([]*Value, len(v.List.Elements))
		for i, e := range v.List.Elements {
			elems[i] = Clone(e)
		}
		c.List = &ListValue{Elements: elems, ElementType: v.List.ElementType}
	case KindEnum:
		variants := make([]string, len(v.Enum.Variants))
		copy(variants, v.Enum.Variants)
		c.Enum = &EnumValue{Variant: v.Enum.Variant, Variants: variants}
	case KindUnion:
		memberTypes := make([]*TypeInfo, len(v.Union.MemberTypes))
		copy(memberTypes, v.Union.MemberTypes)
		c.Union = &UnionValue{Inner: Clone(v.Union.Inner), MemberTypes: memberTypes}
	case KindTaggedUnion:
		variants := make([]TaggedVariant, len(v.TaggedUnion.Variants))
		copy(variants, v.TaggedUnion.Variants)
		c.TaggedUnion = &TaggedUnionValue{
			Tag: v.TaggedUnion.Tag, Inner: Clone(v.TaggedUnion.Inner), Variants: variants,
		}
	case KindFunction:
		c.Function = v.Function
	}
	return c
}

// --- Walk ---

// WalkFunc is called for each value during tree traversal.
// The path argument is the dot-separated path to the current value
// (empty string for the root). Return a non-nil error to stop the walk.
type WalkFunc func(path string, v *Value) error

// Walk traverses the value tree depth-first, calling fn for each value.
// Compound values (struct, list, tuple) are visited before their children.
// Tagged unions and unions are visited, then their inner values are traversed.
func Walk(v *Value, fn WalkFunc) error {
	return walk("", v, fn)
}

func walk(path string, v *Value, fn WalkFunc) error {
	if err := fn(path, v); err != nil {
		return err
	}
	switch v.Kind {
	case KindStruct:
		for _, f := range v.Struct.Fields {
			if err := walk(joinPath(path, f.Name), f.Value, fn); err != nil {
				return err
			}
		}
	case KindList:
		for i, e := range v.List.Elements {
			if err := walk(joinPath(path, strconv.Itoa(i)), e, fn); err != nil {
				return err
			}
		}
	case KindTuple:
		for i, e := range v.Tuple.Elements {
			if err := walk(joinPath(path, strconv.Itoa(i)), e, fn); err != nil {
				return err
			}
		}
	case KindTaggedUnion:
		if err := walk(path, v.TaggedUnion.Inner, fn); err != nil {
			return err
		}
	case KindUnion:
		if err := walk(path, v.Union.Inner, fn); err != nil {
			return err
		}
	}
	return nil
}

func joinPath(base, segment string) string {
	if base == "" {
		return segment
	}
	return base + "." + segment
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

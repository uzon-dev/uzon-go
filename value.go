// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

// Package uzon implements a parser, evaluator, and serializer for the UZON
// data expression format. UZON is a typed, human-readable configuration
// language where every document evaluates to a single immutable value.
//
// See https://uzon.dev for the full specification.
package uzon

import (
	"fmt"
	"math/big"
)

// ValueKind identifies the kind of a UZON value.
// UZON defines primitive kinds (null, bool, int, float, string) and
// compound kinds (struct, tuple, list, enum, union, tagged union, function).
// "undefined" is a special state indicating a missing value (§3.1).
type ValueKind int

const (
	KindNull      ValueKind = iota // intentionally empty value
	KindUndefined                  // missing value; flows through operators, resolved with "or else"
	KindBool                       // true or false
	KindInt                        // arbitrary-precision integer (i8..i65535, u8..u65535)
	KindFloat                      // IEEE 754 float (f16, f32, f64, f80, f128)
	KindString                     // UTF-8 string
	KindStruct                     // named field collection with "is" bindings
	KindTuple                      // fixed-length heterogeneous ordered sequence
	KindList                       // variable-length homogeneous sequence
	KindEnum                       // named variant from a fixed set
	KindUnion                      // value that may be one of several member types
	KindTaggedUnion                // union with variant labels
	KindFunction                   // first-class callable value
)

// String returns the human-readable name of the value kind.
func (k ValueKind) String() string {
	switch k {
	case KindNull:
		return "null"
	case KindUndefined:
		return "undefined"
	case KindBool:
		return "bool"
	case KindInt:
		return "integer"
	case KindFloat:
		return "float"
	case KindString:
		return "string"
	case KindStruct:
		return "struct"
	case KindTuple:
		return "tuple"
	case KindList:
		return "list"
	case KindEnum:
		return "enum"
	case KindUnion:
		return "union"
	case KindTaggedUnion:
		return "tagged_union"
	case KindFunction:
		return "function"
	default:
		return fmt.Sprintf("ValueKind(%d)", int(k))
	}
}

// Value represents a fully evaluated UZON value.
// Exactly one of the typed fields (Bool, Int, Float, ...) is meaningful,
// determined by the Kind field.
type Value struct {
	Kind ValueKind
	Type *TypeInfo // optional type annotation or named type

	// Adoptable marks numeric literals that lack an explicit "as" annotation.
	// Per §5, untyped literals adopt the type of a typed counterpart in
	// same-type contexts (arithmetic, comparison). Bound value references
	// are never adoptable.
	Adoptable bool

	// Exactly one of the following is set, determined by Kind.
	Bool  bool
	Int   *big.Int
	Float *big.Float
	// FloatIsNaN is true when Kind==KindFloat and the value is NaN.
	// big.Float cannot represent NaN natively, so this flag is used instead.
	FloatIsNaN  bool
	Str         string
	Struct      *StructValue
	Tuple       *TupleValue
	List        *ListValue
	Enum        *EnumValue
	Union       *UnionValue
	TaggedUnion *TaggedUnionValue
	Function    *FunctionValue
}

// TypeInfo holds type metadata for a value.
type TypeInfo struct {
	Name     string   // type name from "called", empty if anonymous
	BaseType string   // primitive type name: "i32", "f64", "bool", "string", etc.
	BitSize  int      // for numeric types: bit width (e.g. 32 for i32)
	Signed   bool     // for integer types: true if signed (iN), false if unsigned (uN)
	Path     []string // qualified type path segments (e.g. ["inner", "RGB"])
}

// StructValue represents a UZON struct — a collection of named fields.
// Fields preserve insertion order and support O(1) lookup by name.
type StructValue struct {
	Fields     []Field
	fieldIndex map[string]int // lazy-built name→index map
}

// Field is a single binding in a struct.
type Field struct {
	Name  string
	Value *Value
}

// Get returns the value of a field by name, or nil if not found.
func (s *StructValue) Get(name string) *Value {
	if s.fieldIndex == nil {
		s.buildIndex()
	}
	if idx, ok := s.fieldIndex[name]; ok {
		return s.Fields[idx].Value
	}
	return nil
}

// Set sets or adds a field in the struct.
// If a field with the given name already exists, its value is replaced.
func (s *StructValue) Set(name string, v *Value) {
	if s.fieldIndex == nil {
		s.buildIndex()
	}
	if idx, ok := s.fieldIndex[name]; ok {
		s.Fields[idx].Value = v
		return
	}
	s.fieldIndex[name] = len(s.Fields)
	s.Fields = append(s.Fields, Field{Name: name, Value: v})
}

// Delete removes a field by name. Returns true if the field was found and removed.
func (s *StructValue) Delete(name string) bool {
	if s.fieldIndex == nil {
		s.buildIndex()
	}
	idx, ok := s.fieldIndex[name]
	if !ok {
		return false
	}
	s.Fields = append(s.Fields[:idx], s.Fields[idx+1:]...)
	delete(s.fieldIndex, name)
	for i := idx; i < len(s.Fields); i++ {
		s.fieldIndex[s.Fields[i].Name] = i
	}
	return true
}

func (s *StructValue) buildIndex() {
	s.fieldIndex = make(map[string]int, len(s.Fields))
	for i, f := range s.Fields {
		s.fieldIndex[f.Name] = i
	}
}

// TupleValue represents a UZON tuple — a fixed-length, heterogeneous
// ordered sequence. Elements are accessed by index (.0, .1, etc.).
type TupleValue struct {
	Elements []*Value
}

// ListValue represents a UZON list — a variable-length, homogeneous
// sequence. All elements must share the same type (§3.3).
type ListValue struct {
	Elements    []*Value
	ElementType *TypeInfo // type of elements, nil if untyped
}

// Push appends elements to the end of the list.
func (l *ListValue) Push(elems ...*Value) {
	l.Elements = append(l.Elements, elems...)
}

// Pop removes and returns the last element. Returns (nil, false) if empty.
func (l *ListValue) Pop() (*Value, bool) {
	if len(l.Elements) == 0 {
		return nil, false
	}
	last := l.Elements[len(l.Elements)-1]
	l.Elements = l.Elements[:len(l.Elements)-1]
	return last, true
}

// EnumValue represents a UZON enum — a named variant chosen from a
// fixed set of alternatives defined with "from ... called" (§3.4).
type EnumValue struct {
	Variant  string   // the selected variant
	Variants []string // all valid variants
}

// UnionValue represents a UZON untagged union — a value that may be
// one of several member types defined with "from union" (§3.5).
type UnionValue struct {
	Inner       *Value     // the actual value
	MemberTypes []*TypeInfo // allowed member types
}

// TaggedUnionValue represents a UZON tagged union — a union where each
// variant has a label, defined with "named" (§3.6).
type TaggedUnionValue struct {
	Tag      string          // the active variant's label
	Inner    *Value          // the variant's value
	Variants []TaggedVariant // all variant definitions
}

// TaggedVariant defines one variant of a tagged union.
type TaggedVariant struct {
	Name string
	Type *TypeInfo
}

// FunctionValue represents a UZON function — a first-class callable
// value with parameters, optional return type, and a body expression (§3.7).
type FunctionValue struct {
	Params     []FuncParam
	ReturnType *TypeInfo
	Body       any // *ast.Node, set during evaluation
	Scope      any // *Scope, captured lexical scope
}

// FuncParam defines a function parameter.
type FuncParam struct {
	Name    string
	Type    *TypeInfo
	Default *Value // nil if no default value
}

// Convenience constructors for creating UZON values.

// Null creates a null value.
func Null() *Value {
	return &Value{Kind: KindNull}
}

// Undefined creates an undefined value.
func Undefined() *Value {
	return &Value{Kind: KindUndefined}
}

// Bool creates a boolean value.
func Bool(b bool) *Value {
	return &Value{Kind: KindBool, Bool: b}
}

// Int creates a signed integer value from an int64.
func Int(n int64) *Value {
	return &Value{Kind: KindInt, Int: big.NewInt(n)}
}

// Uint creates an unsigned integer value from a uint64.
func Uint(n uint64) *Value {
	return &Value{Kind: KindInt, Int: new(big.Int).SetUint64(n)}
}

// BigInt creates an integer value from an arbitrary-precision big.Int.
func BigInt(n *big.Int) *Value {
	return &Value{Kind: KindInt, Int: n}
}

// Float64 creates a float value from a float64.
func Float64(f float64) *Value {
	return &Value{Kind: KindFloat, Float: big.NewFloat(f)}
}

// BigFloat creates a float value from an arbitrary-precision big.Float.
func BigFloat(f *big.Float) *Value {
	return &Value{Kind: KindFloat, Float: f}
}

// String creates a string value.
func String(s string) *Value {
	return &Value{Kind: KindString, Str: s}
}

// NewStruct creates a struct value from the given fields.
func NewStruct(fields ...Field) *Value {
	sv := &StructValue{Fields: fields}
	return &Value{Kind: KindStruct, Struct: sv}
}

// NewTuple creates a tuple value from the given elements.
func NewTuple(elems ...*Value) *Value {
	return &Value{Kind: KindTuple, Tuple: &TupleValue{Elements: elems}}
}

// NewList creates a list value with the given elements and element type.
func NewList(elems []*Value, elemType *TypeInfo) *Value {
	return &Value{Kind: KindList, List: &ListValue{Elements: elems, ElementType: elemType}}
}

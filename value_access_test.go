// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"math"
	"math/big"
	"testing"
)

// --- Predicate tests ---

func TestIsNull(t *testing.T) {
	if !Null().IsNull() {
		t.Error("Null should be null")
	}
	if Int(0).IsNull() {
		t.Error("Int(0) should not be null")
	}
}

func TestIsUndefined(t *testing.T) {
	if !Undefined().IsUndefined() {
		t.Error("Undefined should be undefined")
	}
	if Null().IsUndefined() {
		t.Error("Null should not be undefined")
	}
}

// --- Unwrap accessor tests ---

func TestAsInt(t *testing.T) {
	n, ok := Int(42).AsInt()
	if !ok || n != 42 {
		t.Errorf("AsInt: got (%d, %v), want (42, true)", n, ok)
	}

	_, ok = String("42").AsInt()
	if ok {
		t.Error("AsInt on string should return false")
	}

	// Overflow
	big := BigInt(new(big.Int).Lsh(big.NewInt(1), 64))
	_, ok = big.AsInt()
	if ok {
		t.Error("AsInt on overflow should return false")
	}
}

func TestAsFloat(t *testing.T) {
	f, ok := Float64(3.14).AsFloat()
	if !ok || f != 3.14 {
		t.Errorf("AsFloat: got (%v, %v), want (3.14, true)", f, ok)
	}

	// NaN
	nan := &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true}
	f, ok = nan.AsFloat()
	if !ok || !math.IsNaN(f) {
		t.Errorf("AsFloat NaN: got (%v, %v), want (NaN, true)", f, ok)
	}

	_, ok = Int(1).AsFloat()
	if ok {
		t.Error("AsFloat on int should return false")
	}
}

func TestAsString(t *testing.T) {
	s, ok := String("hello").AsString()
	if !ok || s != "hello" {
		t.Errorf("AsString: got (%q, %v), want (\"hello\", true)", s, ok)
	}

	_, ok = Int(1).AsString()
	if ok {
		t.Error("AsString on int should return false")
	}
}

func TestAsBool(t *testing.T) {
	b, ok := Bool(true).AsBool()
	if !ok || !b {
		t.Errorf("AsBool: got (%v, %v), want (true, true)", b, ok)
	}

	b, ok = Bool(false).AsBool()
	if !ok || b {
		t.Errorf("AsBool false: got (%v, %v), want (false, true)", b, ok)
	}

	_, ok = Int(1).AsBool()
	if ok {
		t.Error("AsBool on int should return false")
	}
}

// --- OrElse tests ---

func TestOrElse(t *testing.T) {
	fallback := Int(99)

	v := Int(42).OrElse(fallback)
	n, _ := v.AsInt()
	if n != 42 {
		t.Errorf("OrElse on defined: got %d, want 42", n)
	}

	v = Undefined().OrElse(fallback)
	n, _ = v.AsInt()
	if n != 99 {
		t.Errorf("OrElse on undefined: got %d, want 99", n)
	}

	v = Null().OrElse(fallback)
	if !v.IsNull() {
		t.Error("OrElse on null should return null, not fallback")
	}
}

// --- Len tests ---

func TestLen(t *testing.T) {
	tests := []struct {
		name string
		v    *Value
		want int
	}{
		{"struct", NewStruct(Field{Name: "a", Value: Int(1)}, Field{Name: "b", Value: Int(2)}), 2},
		{"list", NewList([]*Value{Int(1), Int(2), Int(3)}, nil), 3},
		{"tuple", NewTuple(Int(1), String("a")), 2},
		{"string", String("hello"), 5},
		{"string unicode", String("한글"), 2},
		{"empty list", NewList(nil, nil), 0},
		{"int", Int(42), 0},
		{"null", Null(), 0},
	}
	for _, tt := range tests {
		got := tt.v.Len()
		if got != tt.want {
			t.Errorf("%s: Len() = %d, want %d", tt.name, got, tt.want)
		}
	}
}

// --- Keys tests ---

func TestKeys(t *testing.T) {
	v := NewStruct(
		Field{Name: "host", Value: String("localhost")},
		Field{Name: "port", Value: Int(8080)},
	)
	keys := v.Keys()
	if len(keys) != 2 || keys[0] != "host" || keys[1] != "port" {
		t.Errorf("Keys: got %v, want [host port]", keys)
	}

	if Int(1).Keys() != nil {
		t.Error("Keys on non-struct should return nil")
	}
}

// --- GetPath tests ---

func TestGetPathStruct(t *testing.T) {
	v := NewStruct(
		Field{Name: "server", Value: NewStruct(
			Field{Name: "host", Value: String("localhost")},
			Field{Name: "port", Value: Int(8080)},
		)},
		Field{Name: "debug", Value: Bool(true)},
	)

	// Nested access
	host := v.GetPath("server.host")
	if host == nil {
		t.Fatal("GetPath server.host returned nil")
	}
	s, _ := host.AsString()
	if s != "localhost" {
		t.Errorf("got %q, want %q", s, "localhost")
	}

	// Single level
	debug := v.GetPath("debug")
	if debug == nil {
		t.Fatal("GetPath debug returned nil")
	}
	b, _ := debug.AsBool()
	if !b {
		t.Error("expected true")
	}

	// Not found
	if v.GetPath("nonexistent") != nil {
		t.Error("expected nil for missing path")
	}
	if v.GetPath("server.missing") != nil {
		t.Error("expected nil for missing nested path")
	}
}

func TestGetPathTuple(t *testing.T) {
	v := NewStruct(
		Field{Name: "pair", Value: NewTuple(String("a"), Int(42))},
	)

	elem := v.GetPath("pair.1")
	if elem == nil {
		t.Fatal("GetPath pair.1 returned nil")
	}
	n, _ := elem.AsInt()
	if n != 42 {
		t.Errorf("got %d, want 42", n)
	}

	if v.GetPath("pair.5") != nil {
		t.Error("expected nil for out of bounds tuple index")
	}
}

func TestGetPathList(t *testing.T) {
	v := NewStruct(
		Field{Name: "items", Value: NewList([]*Value{String("a"), String("b"), String("c")}, nil)},
	)

	elem := v.GetPath("items.2")
	if elem == nil {
		t.Fatal("GetPath items.2 returned nil")
	}
	s, _ := elem.AsString()
	if s != "c" {
		t.Errorf("got %q, want %q", s, "c")
	}
}

func TestGetPathEmpty(t *testing.T) {
	v := Int(42)
	if v.GetPath("") != v {
		t.Error("empty path should return the value itself")
	}
}

func TestGetPathTaggedUnion(t *testing.T) {
	inner := NewStruct(Field{Name: "x", Value: Int(10)})
	tu := &Value{
		Kind:        KindTaggedUnion,
		TaggedUnion: &TaggedUnionValue{Tag: "point", Inner: inner},
	}
	v := NewStruct(Field{Name: "data", Value: tu})

	// Should unwrap tagged union and access inner struct
	x := v.GetPath("data.x")
	if x == nil {
		t.Fatal("GetPath through tagged union returned nil")
	}
	n, _ := x.AsInt()
	if n != 10 {
		t.Errorf("got %d, want 10", n)
	}
}

// --- Merge tests ---

func TestMerge(t *testing.T) {
	a := NewStruct(
		Field{Name: "host", Value: String("localhost")},
		Field{Name: "port", Value: Int(8080)},
	)
	b := NewStruct(
		Field{Name: "port", Value: Int(9090)},
		Field{Name: "debug", Value: Bool(true)},
	)

	merged, err := Merge(a, b)
	if err != nil {
		t.Fatal(err)
	}

	// Check field count
	if merged.Len() != 3 {
		t.Errorf("Len: got %d, want 3", merged.Len())
	}

	// host from a
	s, _ := merged.GetPath("host").AsString()
	if s != "localhost" {
		t.Errorf("host: got %q, want %q", s, "localhost")
	}

	// port overridden by b
	n, _ := merged.GetPath("port").AsInt()
	if n != 9090 {
		t.Errorf("port: got %d, want 9090", n)
	}

	// debug from b
	bv, _ := merged.GetPath("debug").AsBool()
	if !bv {
		t.Error("debug: expected true")
	}

	// Field order: host, port, debug
	keys := merged.Keys()
	expected := []string{"host", "port", "debug"}
	for i, k := range keys {
		if k != expected[i] {
			t.Errorf("key[%d]: got %q, want %q", i, k, expected[i])
		}
	}
}

func TestMergeNonStruct(t *testing.T) {
	_, err := Merge(Int(1), NewStruct())
	if err == nil {
		t.Fatal("expected error for non-struct merge")
	}
}

// --- String (fmt.Stringer) tests ---

func TestStringStringer(t *testing.T) {
	tests := []struct {
		v    *Value
		want string
	}{
		{Null(), "null"},
		{Bool(true), "true"},
		{Int(42), "42"},
		{String("hello"), `"hello"`},
	}
	for _, tt := range tests {
		got := tt.v.String()
		if got != tt.want {
			t.Errorf("String(): got %q, want %q", got, tt.want)
		}
	}
}

// --- MarshalText tests ---

func TestMarshalText(t *testing.T) {
	v := Int(42)
	b, err := v.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "42" {
		t.Errorf("MarshalText: got %q, want %q", string(b), "42")
	}
}

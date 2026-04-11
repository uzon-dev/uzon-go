// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
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

func TestMergeIndependent(t *testing.T) {
	a := NewStruct(Bind("x", 1))
	b := NewStruct(Bind("y", 2))
	merged, err := Merge(a, b)
	if err != nil {
		t.Fatal(err)
	}
	// Mutating original should not affect merged
	a.Struct.Set("x", Int(99))
	n, _ := merged.GetPath("x").AsInt()
	if n != 1 {
		t.Errorf("merged was mutated: got %d, want 1", n)
	}
}

func TestMergeNonStruct(t *testing.T) {
	_, err := Merge(Int(1), NewStruct())
	if err == nil {
		t.Fatal("expected error for non-struct merge")
	}
}

// --- SetPath tests ---

func TestSetPathStruct(t *testing.T) {
	v := NewStruct(
		Bind("server", NewStruct(
			Bind("host", "localhost"),
			Bind("port", 8080),
		)),
	)

	if err := v.SetPath("server.host", "newhost"); err != nil {
		t.Fatal(err)
	}
	s, _ := v.GetPath("server.host").AsString()
	if s != "newhost" {
		t.Errorf("got %q, want %q", s, "newhost")
	}
}

func TestSetPathNewField(t *testing.T) {
	v := NewStruct(Bind("x", 1))
	if err := v.SetPath("y", 2); err != nil {
		t.Fatal(err)
	}
	n, _ := v.GetPath("y").AsInt()
	if n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

func TestSetPathList(t *testing.T) {
	v := NewStruct(Bind("items", ListOf(1, 2, 3)))
	if err := v.SetPath("items.1", 99); err != nil {
		t.Fatal(err)
	}
	n, _ := v.GetPath("items.1").AsInt()
	if n != 99 {
		t.Errorf("got %d, want 99", n)
	}
}

func TestSetPathError(t *testing.T) {
	v := NewStruct(Bind("x", 1))

	if err := v.SetPath("missing.field", 1); err == nil {
		t.Error("expected error for missing intermediate path")
	}
	if err := v.SetPath("", 1); err == nil {
		t.Error("expected error for empty path")
	}
}

// --- Clone tests ---

func TestClonePrimitive(t *testing.T) {
	original := Int(42)
	cloned := Clone(original)

	if !Equal(original, cloned) {
		t.Error("clone should equal original")
	}

	// Mutation should not affect clone
	original.Int.SetInt64(99)
	n, _ := cloned.AsInt()
	if n != 42 {
		t.Errorf("clone was mutated: got %d, want 42", n)
	}
}

func TestCloneStruct(t *testing.T) {
	original := NewStruct(
		Bind("host", "localhost"),
		Bind("port", 8080),
	)
	cloned := Clone(original)

	if !Equal(original, cloned) {
		t.Error("clone should equal original")
	}

	// Mutate original
	original.Struct.Set("host", String("newhost"))
	s, _ := cloned.GetPath("host").AsString()
	if s != "localhost" {
		t.Errorf("clone was mutated: got %q, want %q", s, "localhost")
	}
}

func TestCloneList(t *testing.T) {
	original := NewList([]*Value{Int(1), Int(2), Int(3)}, nil)
	cloned := Clone(original)

	original.List.Elements[0].Int.SetInt64(99)
	n, _ := cloned.List.Elements[0].AsInt()
	if n != 1 {
		t.Errorf("clone was mutated: got %d, want 1", n)
	}
}

func TestCloneTaggedUnion(t *testing.T) {
	inner := NewStruct(Bind("x", 10))
	original := &Value{
		Kind:        KindTaggedUnion,
		TaggedUnion: &TaggedUnionValue{Tag: "point", Inner: inner},
	}
	cloned := Clone(original)

	// Mutate original inner
	inner.Struct.Set("x", Int(99))
	n, _ := cloned.TaggedUnion.Inner.GetPath("x").AsInt()
	if n != 10 {
		t.Errorf("clone was mutated: got %d, want 10", n)
	}
	if cloned.TaggedUnion.Tag != "point" {
		t.Errorf("tag: got %q, want %q", cloned.TaggedUnion.Tag, "point")
	}
}

func TestCloneNil(t *testing.T) {
	if Clone(nil) != nil {
		t.Error("Clone(nil) should return nil")
	}
}

// --- Walk tests ---

func TestWalk(t *testing.T) {
	v := NewStruct(
		Bind("a", 1),
		Bind("b", NewStruct(
			Bind("c", 2),
		)),
	)

	var paths []string
	err := Walk(v, func(path string, _ *Value) error {
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{"", "a", "b", "b.c"}
	if len(paths) != len(expected) {
		t.Fatalf("got %d paths, want %d: %v", len(paths), len(expected), paths)
	}
	for i, p := range paths {
		if p != expected[i] {
			t.Errorf("path[%d] = %q, want %q", i, p, expected[i])
		}
	}
}

func TestWalkList(t *testing.T) {
	v := NewStruct(
		Bind("items", ListOf("a", "b")),
	)

	var paths []string
	Walk(v, func(path string, _ *Value) error {
		paths = append(paths, path)
		return nil
	})

	expected := []string{"", "items", "items.0", "items.1"}
	if len(paths) != len(expected) {
		t.Fatalf("got %v, want %v", paths, expected)
	}
	for i, p := range paths {
		if p != expected[i] {
			t.Errorf("path[%d] = %q, want %q", i, p, expected[i])
		}
	}
}

func TestWalkEarlyStop(t *testing.T) {
	v := NewStruct(
		Bind("a", 1),
		Bind("b", 2),
		Bind("c", 3),
	)

	count := 0
	err := Walk(v, func(path string, _ *Value) error {
		count++
		if path == "b" {
			return fmt.Errorf("stop")
		}
		return nil
	})
	if err == nil {
		t.Error("expected error from early stop")
	}
	if count != 3 { // root, a, b (stops at b)
		t.Errorf("visited %d nodes, want 3", count)
	}
}

func TestWalkCollectStrings(t *testing.T) {
	v := NewStruct(
		Bind("name", "alice"),
		Bind("nested", NewStruct(
			Bind("title", "engineer"),
		)),
		Bind("count", 42),
	)

	var strings []string
	Walk(v, func(path string, val *Value) error {
		if s, ok := val.AsString(); ok {
			strings = append(strings, s)
		}
		return nil
	})

	if len(strings) != 2 || strings[0] != "alice" || strings[1] != "engineer" {
		t.Errorf("got %v, want [alice engineer]", strings)
	}
}

// --- Mutation tests ---

func TestStructDelete(t *testing.T) {
	v := NewStruct(Bind("a", 1), Bind("b", 2), Bind("c", 3))
	if !v.Struct.Delete("b") {
		t.Fatal("Delete should return true for existing field")
	}
	if v.Len() != 2 {
		t.Errorf("Len after delete: got %d, want 2", v.Len())
	}
	keys := v.Keys()
	if keys[0] != "a" || keys[1] != "c" {
		t.Errorf("Keys after delete: got %v, want [a c]", keys)
	}
}

func TestStructDeleteNotFound(t *testing.T) {
	v := NewStruct(Bind("a", 1))
	if v.Struct.Delete("z") {
		t.Error("Delete should return false for missing field")
	}
}

func TestStructDeleteThenGet(t *testing.T) {
	v := NewStruct(Bind("a", 1), Bind("b", 2))
	v.Struct.Delete("a")
	if v.Struct.Get("a") != nil {
		t.Error("Get after delete should return nil")
	}
	n, _ := v.Struct.Get("b").AsInt()
	if n != 2 {
		t.Errorf("Get(b) after delete(a): got %d, want 2", n)
	}
}

func TestListPush(t *testing.T) {
	v := NewList([]*Value{Int(1)}, nil)
	v.List.Push(Int(2), Int(3))
	if len(v.List.Elements) != 3 {
		t.Errorf("Len after push: got %d, want 3", len(v.List.Elements))
	}
	n, _ := v.List.Elements[2].AsInt()
	if n != 3 {
		t.Errorf("last element: got %d, want 3", n)
	}
}

func TestListPop(t *testing.T) {
	v := NewList([]*Value{Int(1), Int(2), Int(3)}, nil)
	last, ok := v.List.Pop()
	if !ok {
		t.Fatal("Pop should return true")
	}
	n, _ := last.AsInt()
	if n != 3 {
		t.Errorf("popped: got %d, want 3", n)
	}
	if len(v.List.Elements) != 2 {
		t.Errorf("Len after pop: got %d, want 2", len(v.List.Elements))
	}
}

func TestListPopEmpty(t *testing.T) {
	v := NewList(nil, nil)
	_, ok := v.List.Pop()
	if ok {
		t.Error("Pop on empty list should return false")
	}
}

// --- DeepMerge tests ---

func TestDeepMerge(t *testing.T) {
	base := NewStruct(
		Bind("server", NewStruct(
			Bind("host", "localhost"),
			Bind("port", 8080),
		)),
		Bind("debug", false),
	)
	override := NewStruct(
		Bind("server", NewStruct(
			Bind("port", 9090),
			Bind("tls", true),
		)),
		Bind("debug", true),
	)

	merged, err := DeepMerge(base, override)
	if err != nil {
		t.Fatal(err)
	}

	// host preserved from base
	s, _ := merged.GetPath("server.host").AsString()
	if s != "localhost" {
		t.Errorf("server.host: got %q, want %q", s, "localhost")
	}

	// port overridden by override
	n, _ := merged.GetPath("server.port").AsInt()
	if n != 9090 {
		t.Errorf("server.port: got %d, want 9090", n)
	}

	// tls added from override
	b, _ := merged.GetPath("server.tls").AsBool()
	if !b {
		t.Error("server.tls: expected true")
	}

	// debug overridden
	b, _ = merged.GetPath("debug").AsBool()
	if !b {
		t.Error("debug: expected true")
	}
}

func TestDeepMergeIndependent(t *testing.T) {
	base := NewStruct(Bind("x", 1))
	override := NewStruct(Bind("x", 2))

	merged, err := DeepMerge(base, override)
	if err != nil {
		t.Fatal(err)
	}

	// Mutating merged should not affect base
	merged.Struct.Set("x", Int(99))
	n, _ := base.GetPath("x").AsInt()
	if n != 1 {
		t.Errorf("base was mutated: got %d, want 1", n)
	}
}

func TestDeepMergeNonStruct(t *testing.T) {
	_, err := DeepMerge(Int(1), NewStruct())
	if err == nil {
		t.Fatal("expected error for non-struct deep merge")
	}
}

func TestDeepMergeTaggedUnion(t *testing.T) {
	inner := NewStruct(Bind("host", "localhost"), Bind("port", 8080))
	tu := &Value{
		Kind:        KindTaggedUnion,
		TaggedUnion: &TaggedUnionValue{Tag: "primary", Inner: inner},
	}
	base := NewStruct(Bind("server", tu))
	override := NewStruct(Bind("server", NewStruct(Bind("port", 9090))))

	merged, err := DeepMerge(base, override)
	if err != nil {
		t.Fatal(err)
	}

	// port overridden, host preserved through tagged union unwrap
	n, _ := merged.GetPath("server.port").AsInt()
	if n != 9090 {
		t.Errorf("server.port: got %d, want 9090", n)
	}
	s, _ := merged.GetPath("server.host").AsString()
	if s != "localhost" {
		t.Errorf("server.host: got %q, want %q", s, "localhost")
	}
}

// --- String (fmt.Stringer) tests ---

func TestStringStringer(t *testing.T) {
	tests := []struct {
		name string
		v    *Value
		want string
	}{
		{"null", Null(), "null"},
		{"bool", Bool(true), "true"},
		{"int", Int(42), "42"},
		{"string", String("hello"), `"hello"`},
		{"list", NewList([]*Value{Int(1), Int(2)}, nil), "[ 1, 2 ]"},
		{"empty list", NewList(nil, nil), "[]"},
		{"tuple", NewTuple(Int(1), String("a")), `(1, "a")`},
		{"struct", NewStruct(Bind("x", 1), Bind("y", 2)), "{ x is 1, y is 2 }"},
	}
	for _, tt := range tests {
		got := tt.v.String()
		if got != tt.want {
			t.Errorf("%s: String() = %q, want %q", tt.name, got, tt.want)
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

// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"encoding/json"
	"math/big"
	"testing"
)

func TestMarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		v    *Value
		want string
	}{
		{"null", Null(), "null"},
		{"true", Bool(true), "true"},
		{"false", Bool(false), "false"},
		{"int", Int(42), "42"},
		{"float", Float64(3.14), "3.14"},
		{"string", String("hello"), `"hello"`},
		{"empty list", NewList(nil, nil), "[]"},
		{"list", NewList([]*Value{Int(1), Int(2)}, nil), "[1,2]"},
		{"tuple", NewTuple(String("a"), Int(1)), `["a",1]`},
		{"enum", &Value{Kind: KindEnum, Enum: &EnumValue{Variant: "red"}}, `"red"`},
		{"nan", &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true}, "null"},
		{"undefined", Undefined(), "null"},
	}
	for _, tt := range tests {
		b, err := json.Marshal(tt.v)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if string(b) != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, b, tt.want)
		}
	}
}

func TestMarshalJSONStruct(t *testing.T) {
	v := NewStruct(
		Field{Name: "host", Value: String("localhost")},
		Field{Name: "port", Value: Int(8080)},
	)
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"host":"localhost","port":8080}`
	if string(b) != want {
		t.Errorf("got %s, want %s", b, want)
	}
}

func TestMarshalJSONFieldOrder(t *testing.T) {
	// Struct field order should be preserved
	v := NewStruct(
		Field{Name: "z", Value: Int(1)},
		Field{Name: "a", Value: Int(2)},
		Field{Name: "m", Value: Int(3)},
	)
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"z":1,"a":2,"m":3}`
	if string(b) != want {
		t.Errorf("field order not preserved: got %s, want %s", b, want)
	}
}

func TestMarshalJSONTaggedUnion(t *testing.T) {
	v := &Value{
		Kind: KindTaggedUnion,
		TaggedUnion: &TaggedUnionValue{
			Tag:   "circle",
			Inner: NewStruct(Field{Name: "radius", Value: Int(5)}),
		},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"_tag":"circle","_value":{"radius":5}}`
	if string(b) != want {
		t.Errorf("got %s, want %s", b, want)
	}
}

func TestMarshalJSONBigInt(t *testing.T) {
	n := new(big.Int)
	n.SetString("99999999999999999999", 10)
	v := BigInt(n)
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "99999999999999999999" {
		t.Errorf("big int: got %s", b)
	}
}

func TestFromJSON(t *testing.T) {
	data := []byte(`{"name":"test","count":42,"tags":["a","b"],"active":true,"data":null}`)
	v, err := FromJSON(data)
	if err != nil {
		t.Fatal(err)
	}

	if v.Kind != KindStruct {
		t.Fatalf("Kind = %v, want struct", v.Kind)
	}

	name := v.GetPath("name")
	s, _ := name.AsString()
	if s != "test" {
		t.Errorf("name = %q, want %q", s, "test")
	}

	count := v.GetPath("count")
	n, _ := count.AsInt()
	if n != 42 {
		t.Errorf("count = %d, want 42", n)
	}

	tags := v.GetPath("tags")
	if tags.Kind != KindList || tags.Len() != 2 {
		t.Errorf("tags: Kind=%v Len=%d", tags.Kind, tags.Len())
	}

	active := v.GetPath("active")
	b, _ := active.AsBool()
	if !b {
		t.Error("active should be true")
	}

	if !v.GetPath("data").IsNull() {
		t.Error("data should be null")
	}
}

func TestFromJSONPreservesKeyOrder(t *testing.T) {
	data := []byte(`{"z":1,"a":2,"m":3}`)
	v, err := FromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	keys := v.Keys()
	expected := []string{"z", "a", "m"}
	for i, k := range keys {
		if k != expected[i] {
			t.Errorf("key[%d] = %q, want %q", i, k, expected[i])
		}
	}
}

func TestFromJSONFloat(t *testing.T) {
	data := []byte(`3.14`)
	v, err := FromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := v.AsFloat()
	if !ok || f != 3.14 {
		t.Errorf("got %v, want 3.14", f)
	}
}

func TestFromJSONNested(t *testing.T) {
	data := []byte(`{"a":{"b":{"c":42}}}`)
	v, err := FromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	c := v.GetPath("a.b.c")
	if c == nil {
		t.Fatal("a.b.c is nil")
	}
	n, _ := c.AsInt()
	if n != 42 {
		t.Errorf("a.b.c = %d, want 42", n)
	}
}

func TestJSONRoundtrip(t *testing.T) {
	original := NewStruct(
		Bind("name", "test"),
		Bind("values", ListOf(1, 2, 3)),
		Bind("nested", NewStruct(
			Bind("x", true),
			Bind("y", 3.14),
		)),
	)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var parsed Value
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	// Verify round-trip
	s, _ := parsed.GetPath("name").AsString()
	if s != "test" {
		t.Errorf("name = %q", s)
	}
	n, _ := parsed.GetPath("values.1").AsInt()
	if n != 2 {
		t.Errorf("values.1 = %d", n)
	}
	b, _ := parsed.GetPath("nested.x").AsBool()
	if !b {
		t.Error("nested.x should be true")
	}
}

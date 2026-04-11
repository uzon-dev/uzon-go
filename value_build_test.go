// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import "testing"

func TestBind(t *testing.T) {
	tests := []struct {
		name string
		v    any
		kind ValueKind
	}{
		{"string", "hello", KindString},
		{"int", 42, KindInt},
		{"float", 3.14, KindFloat},
		{"bool", true, KindBool},
		{"nil", nil, KindNull},
		{"*Value", Int(99), KindInt},
	}
	for _, tt := range tests {
		f := Bind(tt.name, tt.v)
		if f.Name != tt.name {
			t.Errorf("%s: Name = %q, want %q", tt.name, f.Name, tt.name)
		}
		if f.Value.Kind != tt.kind {
			t.Errorf("%s: Kind = %v, want %v", tt.name, f.Value.Kind, tt.kind)
		}
	}
}

func TestBindNestedStruct(t *testing.T) {
	v := NewStruct(
		Bind("server", NewStruct(
			Bind("host", "localhost"),
			Bind("port", 8080),
		)),
		Bind("debug", true),
	)

	host := v.GetPath("server.host")
	if host == nil {
		t.Fatal("server.host is nil")
	}
	s, _ := host.AsString()
	if s != "localhost" {
		t.Errorf("host = %q, want %q", s, "localhost")
	}

	port := v.GetPath("server.port")
	n, _ := port.AsInt()
	if n != 8080 {
		t.Errorf("port = %d, want 8080", n)
	}
}

func TestBindSlice(t *testing.T) {
	f := Bind("tags", []string{"a", "b", "c"})
	if f.Value.Kind != KindList {
		t.Fatalf("Kind = %v, want list", f.Value.Kind)
	}
	if f.Value.Len() != 3 {
		t.Errorf("Len = %d, want 3", f.Value.Len())
	}
}

func TestListOf(t *testing.T) {
	v := ListOf("a", "b", "c")
	if v.Kind != KindList {
		t.Fatalf("Kind = %v, want list", v.Kind)
	}
	if v.Len() != 3 {
		t.Errorf("Len = %d, want 3", v.Len())
	}
	s, _ := v.List.Elements[1].AsString()
	if s != "b" {
		t.Errorf("elem[1] = %q, want %q", s, "b")
	}
}

func TestListOfEmpty(t *testing.T) {
	v := ListOf()
	if v.Kind != KindList || v.Len() != 0 {
		t.Errorf("empty ListOf: Kind=%v Len=%d", v.Kind, v.Len())
	}
}

func TestListOfMixedValues(t *testing.T) {
	// *Value and Go primitives can be mixed
	v := ListOf(Int(1), 2, 3)
	if v.Len() != 3 {
		t.Errorf("Len = %d, want 3", v.Len())
	}
	n, _ := v.List.Elements[0].AsInt()
	if n != 1 {
		t.Errorf("elem[0] = %d, want 1", n)
	}
}

func TestTupleOf(t *testing.T) {
	v := TupleOf("hello", 42, true)
	if v.Kind != KindTuple {
		t.Fatalf("Kind = %v, want tuple", v.Kind)
	}
	if v.Len() != 3 {
		t.Errorf("Len = %d, want 3", v.Len())
	}
	s, _ := v.Tuple.Elements[0].AsString()
	if s != "hello" {
		t.Errorf("elem[0] = %q, want %q", s, "hello")
	}
	n, _ := v.Tuple.Elements[1].AsInt()
	if n != 42 {
		t.Errorf("elem[1] = %d, want 42", n)
	}
	b, _ := v.Tuple.Elements[2].AsBool()
	if !b {
		t.Error("elem[2] should be true")
	}
}

func TestTupleOfEmpty(t *testing.T) {
	v := TupleOf()
	if v.Kind != KindTuple || v.Len() != 0 {
		t.Errorf("empty TupleOf: Kind=%v Len=%d", v.Kind, v.Len())
	}
}

func TestAutoWrapPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unsupported type")
		}
	}()
	autoWrap(make(chan int))
}

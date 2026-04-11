// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"math/big"
	"testing"
)

func TestAddInt(t *testing.T) {
	r, err := Add(Int(3), Int(7))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != KindInt || r.Int.Int64() != 10 {
		t.Errorf("3+7 = %v, want 10", r.Int)
	}
}

func TestAddFloat(t *testing.T) {
	r, err := Add(Float64(1.5), Float64(2.5))
	if err != nil {
		t.Fatal(err)
	}
	f, _ := r.Float.Float64()
	if r.Kind != KindFloat || f != 4.0 {
		t.Errorf("1.5+2.5 = %v, want 4.0", f)
	}
}

func TestAddTypeMismatch(t *testing.T) {
	_, err := Add(Int(1), Float64(2.0))
	if err == nil {
		t.Fatal("expected error for int + float")
	}
}

func TestSubInt(t *testing.T) {
	r, err := Sub(Int(10), Int(3))
	if err != nil {
		t.Fatal(err)
	}
	if r.Int.Int64() != 7 {
		t.Errorf("10-3 = %v, want 7", r.Int)
	}
}

func TestMulInt(t *testing.T) {
	r, err := Mul(Int(6), Int(7))
	if err != nil {
		t.Fatal(err)
	}
	if r.Int.Int64() != 42 {
		t.Errorf("6*7 = %v, want 42", r.Int)
	}
}

func TestDivInt(t *testing.T) {
	r, err := Div(Int(15), Int(4))
	if err != nil {
		t.Fatal(err)
	}
	if r.Int.Int64() != 3 {
		t.Errorf("15/4 = %v, want 3", r.Int)
	}
}

func TestDivByZero(t *testing.T) {
	_, err := Div(Int(1), Int(0))
	if err == nil {
		t.Fatal("expected division by zero error")
	}
}

func TestDivFloatByZero(t *testing.T) {
	r, err := Div(Float64(1.0), Float64(0.0))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Float.IsInf() {
		t.Error("1.0/0.0 should be inf")
	}
}

func TestModInt(t *testing.T) {
	r, err := Mod(Int(17), Int(5))
	if err != nil {
		t.Fatal(err)
	}
	if r.Int.Int64() != 2 {
		t.Errorf("17%%5 = %v, want 2", r.Int)
	}
}

func TestPowInt(t *testing.T) {
	r, err := Pow(Int(2), Int(10))
	if err != nil {
		t.Fatal(err)
	}
	if r.Int.Int64() != 1024 {
		t.Errorf("2^10 = %v, want 1024", r.Int)
	}
}

func TestPowNegativeExponent(t *testing.T) {
	_, err := Pow(Int(2), Int(-1))
	if err == nil {
		t.Fatal("expected error for negative exponent")
	}
}

func TestNegate(t *testing.T) {
	r, err := Negate(Int(42))
	if err != nil {
		t.Fatal(err)
	}
	if r.Int.Int64() != -42 {
		t.Errorf("-42 = %v, want -42", r.Int)
	}
}

func TestNegateFloat(t *testing.T) {
	r, err := Negate(Float64(3.14))
	if err != nil {
		t.Fatal(err)
	}
	f, _ := r.Float.Float64()
	if f != -3.14 {
		t.Errorf("-3.14 = %v, want -3.14", f)
	}
}

func TestNotBool(t *testing.T) {
	r, err := Not(Bool(true))
	if err != nil {
		t.Fatal(err)
	}
	if r.Bool != false {
		t.Error("not true should be false")
	}
}

func TestNotNonBool(t *testing.T) {
	_, err := Not(Int(1))
	if err == nil {
		t.Fatal("expected error for Not on int")
	}
}

func TestEqual(t *testing.T) {
	tests := []struct {
		a, b *Value
		want bool
	}{
		{Int(42), Int(42), true},
		{Int(1), Int(2), false},
		{String("hello"), String("hello"), true},
		{String("a"), String("b"), false},
		{Bool(true), Bool(true), true},
		{Bool(true), Bool(false), false},
		{Null(), Null(), true},
		{Int(1), String("1"), false},
		{Float64(3.14), Float64(3.14), true},
	}
	for _, tt := range tests {
		got := Equal(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("Equal(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestEqualNaN(t *testing.T) {
	nan := &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true}
	if Equal(nan, nan) {
		t.Error("NaN should not equal NaN")
	}
}

func TestCompare(t *testing.T) {
	cmp, err := Compare(Int(3), Int(5))
	if err != nil {
		t.Fatal(err)
	}
	if cmp >= 0 {
		t.Errorf("Compare(3, 5) = %d, want < 0", cmp)
	}

	cmp, err = Compare(String("b"), String("a"))
	if err != nil {
		t.Fatal(err)
	}
	if cmp <= 0 {
		t.Errorf("Compare(b, a) = %d, want > 0", cmp)
	}
}

func TestCompareNaN(t *testing.T) {
	nan := &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true}
	_, err := Compare(nan, Float64(1.0))
	if err == nil {
		t.Fatal("expected error comparing NaN")
	}
}

func TestConcatStrings(t *testing.T) {
	r, err := Concat(String("hello"), String(" world"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Str != "hello world" {
		t.Errorf("got %q, want %q", r.Str, "hello world")
	}
}

func TestConcatLists(t *testing.T) {
	a := NewList([]*Value{Int(1), Int(2)}, nil)
	b := NewList([]*Value{Int(3)}, nil)
	r, err := Concat(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.List.Elements) != 3 {
		t.Errorf("got %d elements, want 3", len(r.List.Elements))
	}
}

func TestRepeatString(t *testing.T) {
	r, err := Repeat(String("ab"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if r.Str != "ababab" {
		t.Errorf("got %q, want %q", r.Str, "ababab")
	}
}

func TestRepeatList(t *testing.T) {
	v := NewList([]*Value{Int(1)}, nil)
	r, err := Repeat(v, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.List.Elements) != 3 {
		t.Errorf("got %d elements, want 3", len(r.List.Elements))
	}
}

func TestRepeatNegative(t *testing.T) {
	_, err := Repeat(String("x"), -1)
	if err == nil {
		t.Fatal("expected error for negative repeat")
	}
}

func TestContains(t *testing.T) {
	list := NewList([]*Value{Int(1), Int(2), Int(3)}, nil)
	found, err := Contains(list, Int(2))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("expected to find 2 in [1,2,3]")
	}

	found, err = Contains(list, Int(5))
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("did not expect to find 5 in [1,2,3]")
	}
}

func TestContainsNonList(t *testing.T) {
	_, err := Contains(Int(1), Int(1))
	if err == nil {
		t.Fatal("expected error for Contains on non-list")
	}
}

// --- Type conversion tests ---

func TestToStringFromTypes(t *testing.T) {
	tests := []struct {
		name string
		v    *Value
		want string
	}{
		{"string", String("hello"), "hello"},
		{"bool true", Bool(true), "true"},
		{"bool false", Bool(false), "false"},
		{"int", Int(42), "42"},
		{"float", Float64(3.14), "3.14"},
		{"null", Null(), "null"},
	}
	for _, tt := range tests {
		r, err := ToString(tt.v)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if r.Str != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, r.Str, tt.want)
		}
	}
}

func TestToStringNaN(t *testing.T) {
	nan := &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true}
	r, err := ToString(nan)
	if err != nil {
		t.Fatal(err)
	}
	if r.Str != "nan" {
		t.Errorf("got %q, want %q", r.Str, "nan")
	}
}

func TestToStringUnsupported(t *testing.T) {
	_, err := ToString(NewList(nil, nil))
	if err == nil {
		t.Fatal("expected error for list → string")
	}
}

func TestToIntFromFloat(t *testing.T) {
	r, err := ToInt(Float64(3.9))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != KindInt || r.Int.Int64() != 3 {
		t.Errorf("got %v, want 3 (truncated)", r.Int)
	}
}

func TestToIntFromString(t *testing.T) {
	tests := []struct {
		s    string
		want int64
	}{
		{"42", 42},
		{"0xff", 255},
		{"0o77", 63},
		{"0b1010", 10},
		{"1_000", 1000},
	}
	for _, tt := range tests {
		r, err := ToInt(String(tt.s))
		if err != nil {
			t.Errorf("%s: %v", tt.s, err)
			continue
		}
		if r.Int.Int64() != tt.want {
			t.Errorf("%s: got %d, want %d", tt.s, r.Int.Int64(), tt.want)
		}
	}
}

func TestToIntFromNaN(t *testing.T) {
	nan := &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true}
	_, err := ToInt(nan)
	if err == nil {
		t.Fatal("expected error for NaN → int")
	}
}

func TestToIntIdentity(t *testing.T) {
	v := Int(99)
	r, err := ToInt(v)
	if err != nil {
		t.Fatal(err)
	}
	if r != v {
		t.Error("ToInt on int should return same value")
	}
}

func TestToIntInvalidString(t *testing.T) {
	_, err := ToInt(String("not_a_number"))
	if err == nil {
		t.Fatal("expected error for invalid string → int")
	}
}

func TestToFloatFromInt(t *testing.T) {
	r, err := ToFloat(Int(42))
	if err != nil {
		t.Fatal(err)
	}
	f, _ := r.Float.Float64()
	if r.Kind != KindFloat || f != 42.0 {
		t.Errorf("got %v, want 42.0", f)
	}
}

func TestToFloatFromString(t *testing.T) {
	tests := []struct {
		s    string
		want float64
		nan  bool
		inf  int // -1, 0, +1
	}{
		{"3.14", 3.14, false, 0},
		{"nan", 0, true, 0},
		{"-inf", 0, false, -1},
		{"inf", 0, false, 1},
	}
	for _, tt := range tests {
		r, err := ToFloat(String(tt.s))
		if err != nil {
			t.Errorf("%s: %v", tt.s, err)
			continue
		}
		if tt.nan {
			if !r.FloatIsNaN {
				t.Errorf("%s: expected NaN", tt.s)
			}
			continue
		}
		if tt.inf != 0 {
			if !r.Float.IsInf() {
				t.Errorf("%s: expected inf", tt.s)
			}
			continue
		}
		f, _ := r.Float.Float64()
		if f != tt.want {
			t.Errorf("%s: got %v, want %v", tt.s, f, tt.want)
		}
	}
}

func TestToFloatIdentity(t *testing.T) {
	v := Float64(1.5)
	r, err := ToFloat(v)
	if err != nil {
		t.Fatal(err)
	}
	if r != v {
		t.Error("ToFloat on float should return same value")
	}
}

func TestToFloatUnsupported(t *testing.T) {
	_, err := ToFloat(Bool(true))
	if err == nil {
		t.Fatal("expected error for bool → float")
	}
}

func TestEqualStruct(t *testing.T) {
	a := NewStruct(Field{Name: "x", Value: Int(1)}, Field{Name: "y", Value: Int(2)})
	b := NewStruct(Field{Name: "x", Value: Int(1)}, Field{Name: "y", Value: Int(2)})
	if !Equal(a, b) {
		t.Error("identical structs should be equal")
	}

	c := NewStruct(Field{Name: "x", Value: Int(1)}, Field{Name: "y", Value: Int(3)})
	if Equal(a, c) {
		t.Error("structs with different values should not be equal")
	}
}

// --- Auto-wrapping tests ---

func TestAddPrimitive(t *testing.T) {
	r, err := Add(Int(3), 7)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := r.AsInt()
	if n != 10 {
		t.Errorf("Int(3) + 7 = %d, want 10", n)
	}
}

func TestAddBothPrimitive(t *testing.T) {
	r, err := Add(10, 20)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := r.AsInt()
	if n != 30 {
		t.Errorf("10 + 20 = %d, want 30", n)
	}
}

func TestSubPrimitive(t *testing.T) {
	r, err := Sub(Int(10), 3)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := r.AsInt()
	if n != 7 {
		t.Errorf("Int(10) - 3 = %d, want 7", n)
	}
}

func TestMulPrimitive(t *testing.T) {
	r, err := Mul(6, 7)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := r.AsInt()
	if n != 42 {
		t.Errorf("6 * 7 = %d, want 42", n)
	}
}

func TestDivPrimitive(t *testing.T) {
	r, err := Div(Int(15), 4)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := r.AsInt()
	if n != 3 {
		t.Errorf("Int(15) / 4 = %d, want 3", n)
	}
}

func TestModPrimitive(t *testing.T) {
	r, err := Mod(Int(17), 5)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := r.AsInt()
	if n != 2 {
		t.Errorf("Int(17) %% 5 = %d, want 2", n)
	}
}

func TestPowPrimitive(t *testing.T) {
	r, err := Pow(2, 10)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := r.AsInt()
	if n != 1024 {
		t.Errorf("2 ^ 10 = %d, want 1024", n)
	}
}

func TestEqualPrimitive(t *testing.T) {
	if !Equal(Int(42), 42) {
		t.Error("Int(42) should equal 42")
	}
	if !Equal(String("hello"), "hello") {
		t.Error(`String("hello") should equal "hello"`)
	}
	if Equal(Int(42), 43) {
		t.Error("Int(42) should not equal 43")
	}
}

func TestEqualToMethod(t *testing.T) {
	v := Int(42)
	if !v.EqualTo(42) {
		t.Error("Int(42).EqualTo(42) should be true")
	}
	if v.EqualTo("hello") {
		t.Error("Int(42).EqualTo(\"hello\") should be false")
	}
}

func TestComparePrimitive(t *testing.T) {
	cmp, err := Compare(Int(3), 5)
	if err != nil {
		t.Fatal(err)
	}
	if cmp >= 0 {
		t.Errorf("Compare(Int(3), 5) = %d, want < 0", cmp)
	}
}

func TestConcatPrimitive(t *testing.T) {
	r, err := Concat("hello", " world")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := r.AsString()
	if s != "hello world" {
		t.Errorf("got %q, want %q", s, "hello world")
	}
}

func TestContainsPrimitive(t *testing.T) {
	list := ListOf(1, 2, 3)
	found, err := Contains(list, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("expected to find 2 in [1,2,3]")
	}
}

func TestEqualList(t *testing.T) {
	a := NewList([]*Value{Int(1), Int(2)}, nil)
	b := NewList([]*Value{Int(1), Int(2)}, nil)
	if !Equal(a, b) {
		t.Error("identical lists should be equal")
	}

	c := NewList([]*Value{Int(1), Int(3)}, nil)
	if Equal(a, c) {
		t.Error("lists with different values should not be equal")
	}
}

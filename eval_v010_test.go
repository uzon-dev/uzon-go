// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"testing"
)

// --- v0.10 §3.7 variant shorthand ---

func TestV010VariantShorthand(t *testing.T) {
	v := evalSrc(t, `Event is tagged union pressed as string, released as string, cleared as null
e1 is pressed "enter" as Event
e2 is cleared as Event`)

	e1 := getField(t, v, "e1")
	if e1.Kind != KindTaggedUnion {
		t.Fatalf("e1: want tagged_union, got %s", e1.Kind)
	}
	if e1.TaggedUnion.Tag != "pressed" {
		t.Errorf("e1 tag: want pressed, got %s", e1.TaggedUnion.Tag)
	}
	if e1.TaggedUnion.Inner.Str != "enter" {
		t.Errorf("e1 inner: want \"enter\", got %q", e1.TaggedUnion.Inner.Str)
	}

	e2 := getField(t, v, "e2")
	if e2.TaggedUnion.Tag != "cleared" {
		t.Errorf("e2 tag: want cleared, got %s", e2.TaggedUnion.Tag)
	}
	if e2.TaggedUnion.Inner.Kind != KindNull {
		t.Errorf("e2 inner: want null, got %s", e2.TaggedUnion.Inner.Kind)
	}
}

func TestV010VariantShorthandRequiresContext(t *testing.T) {
	_, err := Parse([]byte(`x is pressed "enter"`))
	if err == nil {
		t.Fatal("expected error for shorthand without type context")
	}
}

func TestV010VariantShorthandNested(t *testing.T) {
	v := evalSrc(t, `Inner is tagged union a as i32, b as string
Outer is tagged union wrap as Inner, other as bool
o is wrap a 42 as Outer`)

	o := getField(t, v, "o")
	if o.TaggedUnion.Tag != "wrap" {
		t.Fatalf("o tag: want wrap, got %s", o.TaggedUnion.Tag)
	}
	inner := o.TaggedUnion.Inner
	if inner.Kind != KindTaggedUnion {
		t.Fatalf("o inner: want tagged_union, got %s", inner.Kind)
	}
	if inner.TaggedUnion.Tag != "a" {
		t.Errorf("o inner tag: want a, got %s", inner.TaggedUnion.Tag)
	}
	if inner.TaggedUnion.Inner.Int.Int64() != 42 {
		t.Errorf("o inner int: want 42, got %s", inner.TaggedUnion.Inner.Int)
	}
}

// --- v0.10 §3.5 rule 4 — enum variant from struct field type context ---

func TestV010EnumVariantFromFieldType(t *testing.T) {
	v := evalSrc(t, `Color is enum red, green, blue
Config is struct {
    bg is red as Color
    fg is green as Color
}
active is { bg is blue, fg is red } as Config`)

	active := getField(t, v, "active")
	bg := getField(t, active, "bg")
	if bg.Kind != KindEnum || bg.Enum.Variant != "blue" {
		t.Errorf("active.bg: want enum blue, got %v %v", bg.Kind, bg.Enum)
	}
}

// --- v0.10 §3.5 rule 4 — function return type context ---

func TestV010ReturnTypeContext(t *testing.T) {
	v := evalSrc(t, `Status is enum active, idle, error
default_status is function returns Status {
    active
}
s is default_status()`)

	s := getField(t, v, "s")
	if s.Kind != KindEnum || s.Enum.Variant != "active" {
		t.Errorf("s: want enum active, got %v %v", s.Kind, s.Enum)
	}
}

// --- v0.10 §3.5 rule 4 — function argument type context ---

func TestV010ArgTypeContext(t *testing.T) {
	v := evalSrc(t, `Color is enum red, green, blue
identity is function c as Color returns Color { c }
chosen is identity(blue)`)

	chosen := getField(t, v, "chosen")
	if chosen.Kind != KindEnum || chosen.Enum.Variant != "blue" {
		t.Errorf("chosen: want enum blue, got %v %v", chosen.Kind, chosen.Enum)
	}
}

// --- v0.10 §3.2 struct field defaults ---

func TestV010StructFieldDefaults(t *testing.T) {
	v := evalSrc(t, `Modifiers is struct {
    mod4 is false
    shift is false
    ctrl is false
}
plain is {} as Modifiers
super is { mod4 is true } as Modifiers
super_shift is { mod4 is true, shift is true } as Modifiers`)

	plain := getField(t, v, "plain")
	if getField(t, plain, "mod4").Bool != false {
		t.Errorf("plain.mod4: want false")
	}
	if getField(t, plain, "shift").Bool != false {
		t.Errorf("plain.shift: want false")
	}
	if getField(t, plain, "ctrl").Bool != false {
		t.Errorf("plain.ctrl: want false")
	}

	super := getField(t, v, "super")
	if getField(t, super, "mod4").Bool != true {
		t.Errorf("super.mod4: want true")
	}
	if getField(t, super, "shift").Bool != false {
		t.Errorf("super.shift: want false (default)")
	}

	ss := getField(t, v, "super_shift")
	if getField(t, ss, "mod4").Bool != true {
		t.Errorf("super_shift.mod4: want true")
	}
	if getField(t, ss, "shift").Bool != true {
		t.Errorf("super_shift.shift: want true")
	}
	if getField(t, ss, "ctrl").Bool != false {
		t.Errorf("super_shift.ctrl: want false (default)")
	}
}

func TestV010StructFieldDefaultsRecursive(t *testing.T) {
	v := evalSrc(t, `Point is struct {
    x is 0
    y is 0
}
Shape is struct {
    origin is {} as Point
    radius is 1
}
default_shape is {} as Shape
moved is { origin is { x is 5 } as Point } as Shape`)

	ds := getField(t, v, "default_shape")
	origin := getField(t, ds, "origin")
	if getField(t, origin, "x").Int.Int64() != 0 {
		t.Errorf("default_shape.origin.x: want 0")
	}

	mv := getField(t, v, "moved")
	mvOrigin := getField(t, mv, "origin")
	if getField(t, mvOrigin, "x").Int.Int64() != 5 {
		t.Errorf("moved.origin.x: want 5")
	}
	if getField(t, mvOrigin, "y").Int.Int64() != 0 {
		t.Errorf("moved.origin.y: want 0 (default)")
	}
}

// --- v0.10 §3.7 — nullary variant shorthand from context ---

func TestV010NullaryShorthandFromContext(t *testing.T) {
	v := evalSrc(t, `Event is tagged union pressed as string, cleared as null
e is cleared as Event`)

	e := getField(t, v, "e")
	if e.TaggedUnion.Tag != "cleared" {
		t.Errorf("e tag: want cleared, got %s", e.TaggedUnion.Tag)
	}
	if e.TaggedUnion.Inner.Kind != KindNull {
		t.Errorf("e inner: want null, got %s", e.TaggedUnion.Inner.Kind)
	}
}

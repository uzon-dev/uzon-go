// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import "testing"

// §6.3: Re-annotating an already-tagged value with its own type via `as`
// (without `named`) is a no-op — the tagged-union value keeps its variant.
func TestSpecAsTaggedUnionReannotationNoOp(t *testing.T) {
	v := evalSrc(t, `Event is tagged union pressed as string, cleared as null
e1 is pressed "enter" as Event
e2 is e1 as Event`)

	e2 := getField(t, v, "e2")
	if e2.Kind != KindTaggedUnion {
		t.Fatalf("e2: want tagged_union, got %s", e2.Kind)
	}
	if e2.TaggedUnion.Tag != "pressed" {
		t.Errorf("e2 tag: want pressed, got %s", e2.TaggedUnion.Tag)
	}
	if e2.TaggedUnion.Inner.Str != "enter" {
		t.Errorf("e2 inner: want %q, got %q", "enter", e2.TaggedUnion.Inner.Str)
	}
}

// §6.3: `as TaggedUnion` without `named` on a non-tagged value is still an error.
func TestSpecAsTaggedUnionBareValueRejected(t *testing.T) {
	_, err := Parse([]byte(`Event is tagged union pressed as string, cleared as null
x is "enter" as Event`))
	if err == nil {
		t.Fatal("expected error for bare `as TaggedUnion` on non-tagged value")
	}
}

// §3.8: `fn is null` and `fn is undefined` are permitted and return bool,
// even though function-to-function equality is a type error.
func TestSpecFunctionNullComparison(t *testing.T) {
	v := evalSrc(t, `f is function x as i32 returns i32 { x }
r1 is f is null
r2 is f is not null`)

	if r1 := getField(t, v, "r1"); r1.Kind != KindBool || r1.Bool != false {
		t.Errorf("f is null: want false, got %v", r1)
	}
	if r2 := getField(t, v, "r2"); r2.Kind != KindBool || r2.Bool != true {
		t.Errorf("f is not null: want true, got %v", r2)
	}
}

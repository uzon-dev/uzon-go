// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import "testing"

// §3.5 rule 4: a bare enum variant name in a parameter default resolves
// against the parameter's enum type (type-context inference position).
func TestSpecParamDefaultBareEnumVariant(t *testing.T) {
	v := evalSrc(t, `Color is enum red, green, blue
paint is function c as Color default red returns Color { c }
result is paint()`)

	r := getField(t, v, "result")
	if r.Kind != KindEnum {
		t.Fatalf("result: want enum, got %s", r.Kind)
	}
	if r.Enum.Variant != "red" {
		t.Errorf("result variant: want red, got %s", r.Enum.Variant)
	}
}

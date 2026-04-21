// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"strings"
	"testing"
)

// §5.16.6: std.upper uses simple (one-to-one) Unicode case mapping.
// ß has no single-codepoint uppercase; simple mapping leaves it unchanged.
// Full mapping would produce "SS", which is forbidden.
func TestSpecStdUpperSimpleMapping(t *testing.T) {
	v := evalSrc(t, `s is std.upper("straße")`)
	got := getField(t, v, "s").Str
	if strings.Contains(got, "SS") {
		t.Errorf("std.upper: simple mapping must not expand ß → SS; got %q", got)
	}
}

// §5.16.6: std.lower uses simple (one-to-one) Unicode case mapping.
func TestSpecStdLowerSimpleMapping(t *testing.T) {
	v := evalSrc(t, `s is std.lower("STRASSE")`)
	got := getField(t, v, "s").Str
	if got != "strasse" {
		t.Errorf("std.lower(STRASSE): want %q, got %q", "strasse", got)
	}
}

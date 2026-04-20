// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCrossFileNominalIdentity exercises §7.3: named types declared in
// different files are distinct nominal types even when structurally
// identical. Casting a value of a.Point to b.Point must error.
func TestCrossFileNominalIdentity(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.uzon", `Point is struct { x is 0 as i32, y is 0 as i32 }
p is { x is 1, y is 2 } as Point
`)
	mustWrite(t, dir, "b.uzon", `Point is struct { x is 0 as i32, y is 0 as i32 }
`)
	main := filepath.Join(dir, "main.uzon")
	mustWriteFull(t, main, `a is struct "./a"
b is struct "./b"
q is a.p as b.Point
`)

	_, err := ParseFile(main)
	if err == nil {
		t.Fatalf("expected error for cross-file Point mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "nominal identity") {
		t.Fatalf("expected nominal identity error, got: %v", err)
	}
}

// TestSameFilePointRoundtrip confirms the Origin-aware nominal check does
// not break in-file `as` casts.
func TestSameFilePointRoundtrip(t *testing.T) {
	src := `Point is struct { x is 0 as i32, y is 0 as i32 }
p is { x is 1, y is 2 } as Point
q is p as Point
`
	if _, err := Parse([]byte(src)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCrossFileSameImportCompatible confirms that accessing the same
// imported file's type twice (through two aliases) still shares identity
// via file deduplication (§7.1).
func TestCrossFileSameImportCompatible(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "shared.uzon", `Point is struct { x is 0 as i32, y is 0 as i32 }
p is { x is 1, y is 2 } as Point
`)
	main := filepath.Join(dir, "main.uzon")
	mustWriteFull(t, main, `s1 is struct "./shared"
s2 is struct "./shared"
q is s1.p as s2.Point
`)

	if _, err := ParseFile(main); err != nil {
		t.Fatalf("unexpected error on same-file dedup cast: %v", err)
	}
}

func mustWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	mustWriteFull(t, filepath.Join(dir, name), content)
}

func mustWriteFull(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uzon-dev/uzon-go/ast"
)

const conformanceDir = "../conformance"

// TestConformanceParseValid ensures every .uzon file under parse/valid
// parses and evaluates without error.
func TestConformanceParseValid(t *testing.T) {
	forEachUZON(t, filepath.Join(conformanceDir, "parse", "valid"), func(t *testing.T, path string, src []byte) {
		doc, err := parseSource(src, path)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		ev := evalForDir(filepath.Dir(path))
		if _, err := ev.EvalDocument(doc); err != nil {
			t.Fatalf("eval: %v", err)
		}
	})
}

// TestConformanceParseInvalid ensures every .uzon file under parse/invalid
// produces a parse or evaluation error.
func TestConformanceParseInvalid(t *testing.T) {
	forEachUZON(t, filepath.Join(conformanceDir, "parse", "invalid"), func(t *testing.T, path string, src []byte) {
		doc, err := parseSource(src, path)
		if err != nil {
			return
		}
		ev := evalForDir(filepath.Dir(path))
		if _, err := ev.EvalDocument(doc); err != nil {
			return
		}
		t.Fatal("expected error but got none")
	})
}

// TestConformanceEval evaluates each .uzon file and compares the result
// against its .expected.uzon companion.
func TestConformanceEval(t *testing.T) {
	dir := filepath.Join(conformanceDir, "eval")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("eval dir not found: %s", dir)
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".uzon") || strings.HasSuffix(name, ".expected.uzon") {
			continue
		}
		base := strings.TrimSuffix(name, ".uzon")
		t.Run(base, func(t *testing.T) {
			got, err := ParseFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("parse input: %v", err)
			}
			want, err := ParseFile(filepath.Join(dir, base+".expected.uzon"))
			if err != nil {
				t.Fatalf("parse expected: %v", err)
			}
			if d := compareValues("", got, want); d != "" {
				t.Fatalf("mismatch:\n%s", d)
			}
		})
	}
}

// TestConformanceRoundtrip verifies parse → emit → re-parse identity.
func TestConformanceRoundtrip(t *testing.T) {
	dir := filepath.Join(conformanceDir, "roundtrip")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("roundtrip dir not found: %s", dir)
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".uzon") {
			continue
		}
		t.Run(strings.TrimSuffix(name, ".uzon"), func(t *testing.T) {
			original, err := ParseFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			em := &emitter{}
			em.emitDocument(original)
			text := em.sb.String()

			reparsed, err := Parse([]byte(text))
			if err != nil {
				t.Fatalf("re-parse failed:\n%s\nerror: %v", text, err)
			}
			if d := compareValues("", reparsed, original); d != "" {
				t.Fatalf("roundtrip mismatch:\nemitted:\n%s\ndiff:\n%s", text, d)
			}
		})
	}
}

// --- helpers ---

func parseSource(src []byte, file string) (*ast.Document, error) {
	return ast.NewParser(src, file).Parse()
}

func evalForDir(dir string) *Evaluator {
	ev := NewEvaluator()
	ev.baseDir = dir
	return ev
}

// forEachUZON recursively walks dir and calls fn for every .uzon file.
func forEachUZON(t *testing.T, dir string, fn func(*testing.T, string, []byte)) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("dir not found: %s", dir)
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			t.Run(name, func(t *testing.T) {
				forEachUZON(t, filepath.Join(dir, name), fn)
			})
			continue
		}
		if filepath.Ext(name) != ".uzon" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			fn(t, path, src)
		})
	}
}

// compareValues recursively compares got and want, returning a
// human-readable diff or "" on match. NaN == NaN for testing.
// For structs, only fields present in want are checked (expected output
// may omit function bindings).
func compareValues(path string, got, want *Value) string {
	if got.Kind != want.Kind {
		return fmt.Sprintf("  %s: kind %s != %s", path, got.Kind, want.Kind)
	}
	switch got.Kind {
	case KindBool:
		if got.Bool != want.Bool {
			return fmt.Sprintf("  %s: %v != %v", path, got.Bool, want.Bool)
		}
	case KindInt:
		if got.Int.Cmp(want.Int) != 0 {
			return fmt.Sprintf("  %s: %s != %s", path, got.Int, want.Int)
		}
	case KindFloat:
		if got.FloatIsNaN && want.FloatIsNaN {
			return ""
		}
		if got.FloatIsNaN != want.FloatIsNaN {
			return fmt.Sprintf("  %s: NaN=%v != NaN=%v", path, got.FloatIsNaN, want.FloatIsNaN)
		}
		if got.Float.Cmp(want.Float) != 0 {
			return fmt.Sprintf("  %s: %s != %s", path, got.Float.Text('g', -1), want.Float.Text('g', -1))
		}
	case KindString:
		if got.Str != want.Str {
			return fmt.Sprintf("  %s: %q != %q", path, got.Str, want.Str)
		}
	case KindNull, KindUndefined:
		// always equal
	case KindEnum:
		if got.Enum.Variant != want.Enum.Variant {
			return fmt.Sprintf("  %s: enum %s != %s", path, got.Enum.Variant, want.Enum.Variant)
		}
	case KindStruct:
		var diffs []string
		for _, f := range want.Struct.Fields {
			gv := got.Struct.Get(f.Name)
			if gv == nil {
				diffs = append(diffs, fmt.Sprintf("  %s.%s: missing", path, f.Name))
				continue
			}
			if d := compareValues(path+"."+f.Name, gv, f.Value); d != "" {
				diffs = append(diffs, d)
			}
		}
		return strings.Join(diffs, "\n")
	case KindList:
		if len(got.List.Elements) != len(want.List.Elements) {
			return fmt.Sprintf("  %s: list len %d != %d", path, len(got.List.Elements), len(want.List.Elements))
		}
		var diffs []string
		for i := range want.List.Elements {
			if d := compareValues(fmt.Sprintf("%s[%d]", path, i), got.List.Elements[i], want.List.Elements[i]); d != "" {
				diffs = append(diffs, d)
			}
		}
		return strings.Join(diffs, "\n")
	case KindTuple:
		if len(got.Tuple.Elements) != len(want.Tuple.Elements) {
			return fmt.Sprintf("  %s: tuple len %d != %d", path, len(got.Tuple.Elements), len(want.Tuple.Elements))
		}
		var diffs []string
		for i := range want.Tuple.Elements {
			if d := compareValues(fmt.Sprintf("%s(%d)", path, i), got.Tuple.Elements[i], want.Tuple.Elements[i]); d != "" {
				diffs = append(diffs, d)
			}
		}
		return strings.Join(diffs, "\n")
	case KindTaggedUnion:
		if got.TaggedUnion.Tag != want.TaggedUnion.Tag {
			return fmt.Sprintf("  %s: tag %s != %s", path, got.TaggedUnion.Tag, want.TaggedUnion.Tag)
		}
		return compareValues(path+"."+got.TaggedUnion.Tag, got.TaggedUnion.Inner, want.TaggedUnion.Inner)
	case KindUnion:
		return compareValues(path+".<union>", got.Union.Inner, want.Union.Inner)
	}
	return ""
}

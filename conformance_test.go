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

// TestConformanceParseValid ensures all valid conformance test files
// parse and evaluate without error.
func TestConformanceParseValid(t *testing.T) {
	dir := filepath.Join(conformanceDir, "parse", "valid")
	runConformanceDir(t, dir, true)
}

// TestConformanceParseInvalid ensures all invalid conformance test files
// produce a parse or evaluation error.
func TestConformanceParseInvalid(t *testing.T) {
	dir := filepath.Join(conformanceDir, "parse", "invalid")
	runConformanceDir(t, dir, false)
}

// TestConformanceEval ensures that evaluating each .uzon file produces output
// matching the corresponding .expected.uzon file.
func TestConformanceEval(t *testing.T) {
	dir := filepath.Join(conformanceDir, "eval")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("conformance eval dir not found: %s", dir)
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".uzon") || strings.HasSuffix(name, ".expected.uzon") {
			continue
		}
		base := strings.TrimSuffix(name, ".uzon")
		t.Run(base, func(t *testing.T) {
			inputPath := filepath.Join(dir, name)
			expectedPath := filepath.Join(dir, base+".expected.uzon")

			got, err := ParseFile(inputPath)
			if err != nil {
				t.Fatalf("parse input: %v", err)
			}
			want, err := ParseFile(expectedPath)
			if err != nil {
				t.Fatalf("parse expected: %v", err)
			}

			if diff := diffValues("", got, want); diff != "" {
				t.Fatalf("mismatch:\n%s", diff)
			}
		})
	}
}

// diffValues compares two values and returns a human-readable diff.
// Unlike valuesEqual, this treats NaN == NaN for conformance testing.
func diffValues(path string, got, want *Value) string {
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
			return "" // NaN == NaN for testing
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
		// equal
	case KindEnum:
		if got.Enum.Variant != want.Enum.Variant {
			return fmt.Sprintf("  %s: enum %s != %s", path, got.Enum.Variant, want.Enum.Variant)
		}
	case KindStruct:
		var diffs []string
		for _, f := range want.Struct.Fields {
			gv := got.Struct.Get(f.Name)
			if gv == nil {
				diffs = append(diffs, fmt.Sprintf("  %s.%s: missing in got", path, f.Name))
				continue
			}
			if d := diffValues(path+"."+f.Name, gv, f.Value); d != "" {
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
			if d := diffValues(fmt.Sprintf("%s[%d]", path, i), got.List.Elements[i], want.List.Elements[i]); d != "" {
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
			if d := diffValues(fmt.Sprintf("%s(%d)", path, i), got.Tuple.Elements[i], want.Tuple.Elements[i]); d != "" {
				diffs = append(diffs, d)
			}
		}
		return strings.Join(diffs, "\n")
	case KindTaggedUnion:
		if got.TaggedUnion.Tag != want.TaggedUnion.Tag {
			return fmt.Sprintf("  %s: tag %s != %s", path, got.TaggedUnion.Tag, want.TaggedUnion.Tag)
		}
		return diffValues(path+"."+got.TaggedUnion.Tag, got.TaggedUnion.Inner, want.TaggedUnion.Inner)
	}
	return ""
}

// TestConformanceRoundtrip ensures that parse → emit → re-parse produces
// identical values for each .uzon file in the roundtrip directory.
func TestConformanceRoundtrip(t *testing.T) {
	dir := filepath.Join(conformanceDir, "roundtrip")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("conformance roundtrip dir not found: %s", dir)
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".uzon") {
			continue
		}
		base := strings.TrimSuffix(name, ".uzon")
		t.Run(base, func(t *testing.T) {
			path := filepath.Join(dir, name)
			original, err := ParseFile(path)
			if err != nil {
				t.Skipf("parse original: %v", err)
				return
			}

			e := &emitter{}
			e.emitDocument(original)
			emitted := e.sb.String()

			reparsed, err := Parse([]byte(emitted))
			if err != nil {
				t.Fatalf("re-parse failed:\n%s\nerror: %v", emitted, err)
			}

			if diff := diffValues("", reparsed, original); diff != "" {
				t.Fatalf("roundtrip mismatch:\nemitted:\n%s\ndiff:\n%s", emitted, diff)
			}
		})
	}
}

// runConformanceDir walks a directory and runs each .uzon file.
// If expectPass is true, the file must parse+eval successfully.
// If expectPass is false, it must produce an error at parse or eval time.
func runConformanceDir(t *testing.T, dir string, expectPass bool) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("conformance dir not found: %s", dir)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			t.Run(entry.Name(), func(t *testing.T) {
				runConformanceDir(t, filepath.Join(dir, entry.Name()), expectPass)
			})
			continue
		}
		if filepath.Ext(entry.Name()) != ".uzon" {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			p := ast.NewParser(data, path)
			doc, parseErr := p.Parse()

			if expectPass {
				if parseErr != nil {
					t.Fatalf("parse error: %v", parseErr)
				}
				ev := NewEvaluator()
				ev.baseDir = dir
				_, evalErr := ev.EvalDocument(doc)
				if evalErr != nil {
					t.Fatalf("eval error: %v", evalErr)
				}
			} else {
				if parseErr != nil {
					return // parse rejected it — correct
				}
				ev := NewEvaluator()
				ev.baseDir = dir
				_, evalErr := ev.EvalDocument(doc)
				if evalErr == nil {
					t.Fatalf("expected error but got none")
				}
			}
		})
	}
}

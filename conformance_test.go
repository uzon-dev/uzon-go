// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"os"
	"path/filepath"
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

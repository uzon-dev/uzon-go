// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

// Package uzon implements a parser, evaluator, and converter for the UZON
// data format (spec v0.10). It provides two modes of operation:
//
// UZON-native types (lossless):
//
//	uzon.Parse(data)         → *uzon.Value
//	uzon.ParseFile(path)     → *uzon.Value
//	(*Value).Marshal()       → []byte
//
// Go native types (reflection-based, encoding/json style):
//
//	uzon.Marshal(v)          → []byte
//	uzon.Unmarshal(data, v)  → error
//	uzon.UnmarshalFile(path, v) → error
//
// Bridging between the two:
//
//	uzon.ValueOf(v)          → *uzon.Value
//	(*Value).Decode(v)       → error
package uzon

import (
	"os"
	"path/filepath"

	"github.com/uzon-dev/uzon-go/ast"
)

// Parse parses UZON source text and evaluates it, returning a *Value.
func Parse(data []byte) (*Value, error) {
	return parseAndEval(data, "")
}

// ParseFile reads a .uzon file, parses, and evaluates it.
func ParseFile(path string) (*Value, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseAndEval(data, path)
}

// parseAndEval is the shared parse-then-evaluate pipeline.
func parseAndEval(data []byte, file string) (*Value, error) {
	p := ast.NewParser(data, file)
	doc, err := p.Parse()
	if err != nil {
		return nil, err
	}
	ev := NewEvaluator()
	if file != "" {
		ev.baseDir = filepath.Dir(file)
	}
	return ev.EvalDocument(doc)
}

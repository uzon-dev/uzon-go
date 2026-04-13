// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"strconv"
	"strings"

	"github.com/uzon-dev/uzon-go/ast"
)

// --- Type resolution helpers ---

// resolveTypeExpr converts an AST TypeExpr to a TypeInfo.
func (ev *Evaluator) resolveTypeExpr(te *ast.TypeExpr) *TypeInfo {
	if te == nil {
		return nil
	}
	if te.IsNull {
		return &TypeInfo{BaseType: "null"}
	}
	if te.ListElem != nil {
		return &TypeInfo{BaseType: "list"}
	}
	if len(te.TupleElems) > 0 {
		return &TypeInfo{BaseType: "tuple"}
	}
	if len(te.Path) > 0 {
		name := strings.Join(te.Path, ".")
		ti := parseBuiltinType(name)
		if ti != nil {
			return ti
		}
		if registered, ok := ev.types.get(te.Path); ok {
			return registered
		}
		return &TypeInfo{Name: name, Path: te.Path}
	}
	return &TypeInfo{}
}

func parseBuiltinType(name string) *TypeInfo {
	if strings.HasPrefix(name, "i") {
		if bits, err := strconv.Atoi(name[1:]); err == nil {
			return &TypeInfo{BaseType: name, BitSize: bits, Signed: true}
		}
	}
	if strings.HasPrefix(name, "u") {
		if bits, err := strconv.Atoi(name[1:]); err == nil {
			return &TypeInfo{BaseType: name, BitSize: bits, Signed: false}
		}
	}
	if strings.HasPrefix(name, "f") {
		if bits, err := strconv.Atoi(name[1:]); err == nil {
			return &TypeInfo{BaseType: name, BitSize: bits}
		}
	}
	switch name {
	case "bool":
		return &TypeInfo{BaseType: "bool"}
	case "string":
		return &TypeInfo{BaseType: "string"}
	case "null":
		return &TypeInfo{BaseType: "null"}
	case "list", "tuple", "struct":
		return &TypeInfo{BaseType: name}
	}
	return nil
}

func (ev *Evaluator) inferType(v *Value) *TypeInfo {
	if v.Type != nil {
		return v.Type
	}
	switch v.Kind {
	case KindBool:
		return &TypeInfo{BaseType: "bool"}
	case KindInt:
		return &TypeInfo{BaseType: "i64", BitSize: 64, Signed: true}
	case KindFloat:
		return &TypeInfo{BaseType: "f64", BitSize: 64}
	case KindString:
		return &TypeInfo{BaseType: "string"}
	case KindNull:
		return &TypeInfo{BaseType: "null"}
	default:
		return &TypeInfo{}
	}
}

func isIntegerType(name string) bool {
	return (strings.HasPrefix(name, "i") || strings.HasPrefix(name, "u")) && len(name) > 1
}

func isFloatType(name string) bool {
	return strings.HasPrefix(name, "f") && len(name) > 1
}

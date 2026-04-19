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
		elemTi := ev.resolveTypeExpr(te.ListElem)
		return &TypeInfo{BaseType: "list", ListElemType: elemTi}
	}
	if te.TupleElems != nil {
		var elemTypes []*TypeInfo
		for _, elem := range te.TupleElems {
			elemTypes = append(elemTypes, ev.resolveTypeExpr(elem))
		}
		return &TypeInfo{BaseType: "tuple", TupleElemTypes: elemTypes}
	}
	if len(te.Path) > 0 {
		name := strings.Join(te.Path, ".")
		ti := parseBuiltinType(name)
		if ti != nil {
			return ti
		}
		if registered, ok := ev.types.get(te.Path); ok {
			if len(te.Path) > 1 && len(registered.Path) == 0 {
				cp := *registered
				cp.Path = te.Path
				return &cp
			}
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
	case KindFunction:
		return &TypeInfo{BaseType: "function"}
	default:
		return &TypeInfo{}
	}
}

func isIntegerType(name string) bool {
	if len(name) < 2 || (name[0] != 'i' && name[0] != 'u') {
		return false
	}
	for _, c := range name[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isFloatType(name string) bool {
	if len(name) < 2 || name[0] != 'f' {
		return false
	}
	for _, c := range name[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// typeAdmitsNull reports whether type T accepts the null value (§6.1 R6).
// T admits null when it is the null type, a union containing null, or a
// tagged union with a null-inner variant.
func (ev *Evaluator) typeAdmitsNull(te *ast.TypeExpr, ti *TypeInfo, scope *Scope) bool {
	if te != nil && te.IsNull {
		return true
	}
	if ti != nil && ti.BaseType == "null" {
		return true
	}
	if te == nil || len(te.Path) == 0 {
		return false
	}
	// Tagged union: §5.2 makes null compatible with any inner variant type
	// in a `named` construction context. The actual tag/variant check is
	// performed in evalNamed; here we only need to permit the cast.
	if _, ok := ev.taggedVariants.get(te.Path[0]); ok {
		return true
	}
	// Named union — look up the binding value to inspect MemberTypes.
	if scope != nil {
		if bv, ok := scope.get(te.Path[0]); ok && bv.Kind == KindUnion && bv.Union != nil {
			for _, mt := range bv.Union.MemberTypes {
				if mt != nil && mt.BaseType == "null" {
					return true
				}
			}
		}
	}
	return false
}

// hasConstructibleDefault reports whether a default value can be constructed
// for the given type (§3.6 transitive default rule). Used to validate
// standalone union/tuple declarations whose first member determines the
// default. Returns false for function types and for compound types whose
// first/element types are themselves not constructible.
func (ev *Evaluator) hasConstructibleDefault(ti *TypeInfo) bool {
	if ti == nil {
		return false
	}
	if ti.BaseType == "function" {
		return false
	}
	// Tuple: every element must have a constructible default.
	if ti.BaseType == "tuple" || len(ti.TupleElemTypes) > 0 {
		for _, et := range ti.TupleElemTypes {
			if !ev.hasConstructibleDefault(et) {
				return false
			}
		}
		return true
	}
	// List: empty list always constructible.
	if ti.BaseType == "list" || ti.ListElemType != nil {
		return true
	}
	// Builtins: bool/string/null/numeric all have defaults.
	if ti.BaseType != "" {
		return true
	}
	// Named type: look up the registered TypeInfo and the binding value.
	if ti.Name != "" {
		// Tagged union: first variant's inner type must be constructible.
		variants, ok := ev.taggedVariants.get(ti.Name)
		if ok {
			if len(variants) == 0 {
				return false
			}
			return ev.hasConstructibleDefault(variants[0].Type)
		}
		// Lookup in type registry; recurse on the registered TypeInfo
		// only if it differs from ti (to avoid infinite recursion).
		if reg, ok := ev.types.get(ti.Path); ok {
			if reg != ti && reg.BaseType != "" {
				return ev.hasConstructibleDefault(reg)
			}
		}
		// Anything else (struct, enum, etc.) is treated as constructible.
		return true
	}
	return true
}

// typeExprName produces a human-readable name for an error message.
func typeExprName(te *ast.TypeExpr, ti *TypeInfo) string {
	if te == nil {
		if ti != nil && ti.Name != "" {
			return ti.Name
		}
		return "<unknown>"
	}
	if te.IsNull {
		return "null"
	}
	if te.ListElem != nil {
		return "[" + typeExprName(te.ListElem, nil) + "]"
	}
	if te.TupleElems != nil {
		s := "("
		for i, el := range te.TupleElems {
			if i > 0 {
				s += ", "
			}
			s += typeExprName(el, nil)
		}
		return s + ")"
	}
	if len(te.Path) > 0 {
		return strings.Join(te.Path, ".")
	}
	if ti != nil && ti.Name != "" {
		return ti.Name
	}
	return "<unknown>"
}

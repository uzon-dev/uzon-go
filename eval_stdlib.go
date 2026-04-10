// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/uzon-dev/uzon-go/ast"
)

// --- Standard library (§5.16) ---

func (ev *Evaluator) evalStdCall(name string, args []ast.Expr, scope *Scope) (*Value, error) {
	evalArgs := func() ([]*Value, error) {
		var vals []*Value
		for _, a := range args {
			v, err := ev.evalExpr(a, scope)
			if err != nil {
				return nil, err
			}
			vals = append(vals, v)
		}
		return vals, nil
	}

	switch name {
	case "len":
		return ev.stdLen(evalArgs)
	case "has":
		return ev.stdHas(evalArgs)
	case "get":
		return ev.stdGet(evalArgs)
	case "keys":
		return ev.stdKeys(evalArgs)
	case "values":
		return ev.stdValues(evalArgs)
	case "map":
		return ev.stdMap(evalArgs, scope)
	case "filter":
		return ev.stdFilter(evalArgs, scope)
	case "reduce":
		return ev.stdReduce(evalArgs, scope)
	case "sort":
		return ev.stdSort(evalArgs, scope)
	case "isNan":
		return ev.stdIsNan(evalArgs)
	case "isInf":
		return ev.stdIsInf(evalArgs)
	case "isFinite":
		return ev.stdIsFinite(evalArgs)
	case "join":
		return ev.stdJoin(evalArgs)
	case "replace":
		return ev.stdReplace(evalArgs)
	case "split":
		return ev.stdSplit(evalArgs)
	case "trim":
		return ev.stdTrim(evalArgs)
	case "lower":
		return ev.stdLower(evalArgs)
	case "upper":
		return ev.stdUpper(evalArgs)
	default:
		return nil, fmt.Errorf("unknown std function: %s", name)
	}
}

func (ev *Evaluator) stdLen(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 {
		return nil, fmt.Errorf("std.len expects 1 argument, got %d", len(vals))
	}
	switch vals[0].Kind {
	case KindList:
		return Int(int64(len(vals[0].List.Elements))), nil
	case KindTuple:
		return Int(int64(len(vals[0].Tuple.Elements))), nil
	case KindStruct:
		return Int(int64(len(vals[0].Struct.Fields))), nil
	case KindString:
		return Int(int64(len([]rune(vals[0].Str)))), nil
	default:
		return nil, fmt.Errorf("std.len: expected collection or string, got %s", vals[0].Kind)
	}
}

func (ev *Evaluator) stdHas(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.has expects 2 arguments, got %d", len(vals))
	}
	coll, key := vals[0], vals[1]
	switch coll.Kind {
	case KindList:
		for _, e := range coll.List.Elements {
			if ev.valuesEqual(e, key) {
				return Bool(true), nil
			}
		}
		return Bool(false), nil
	case KindStruct:
		if key.Kind != KindString {
			return nil, fmt.Errorf("std.has: struct key must be string")
		}
		return Bool(coll.Struct.Get(key.Str) != nil), nil
	default:
		return nil, fmt.Errorf("std.has: expected collection, got %s", coll.Kind)
	}
}

func (ev *Evaluator) stdGet(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.get expects 2 arguments, got %d", len(vals))
	}
	coll, key := vals[0], vals[1]
	switch coll.Kind {
	case KindList:
		if key.Kind != KindInt {
			return nil, fmt.Errorf("std.get: list index must be integer")
		}
		idx := int(key.Int.Int64())
		if idx >= 0 && idx < len(coll.List.Elements) {
			return coll.List.Elements[idx], nil
		}
		return Undefined(), nil
	case KindStruct:
		if key.Kind != KindString {
			return nil, fmt.Errorf("std.get: struct key must be string")
		}
		v := coll.Struct.Get(key.Str)
		if v == nil {
			return Undefined(), nil
		}
		return v, nil
	default:
		return nil, fmt.Errorf("std.get: expected collection, got %s", coll.Kind)
	}
}

func (ev *Evaluator) stdKeys(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 {
		return nil, fmt.Errorf("std.keys expects 1 argument, got %d", len(vals))
	}
	if vals[0].Kind != KindStruct {
		return nil, fmt.Errorf("std.keys: expected struct, got %s", vals[0].Kind)
	}
	var elems []*Value
	for _, f := range vals[0].Struct.Fields {
		elems = append(elems, String(f.Name))
	}
	return NewList(elems, &TypeInfo{BaseType: "string"}), nil
}

func (ev *Evaluator) stdValues(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 {
		return nil, fmt.Errorf("std.values expects 1 argument, got %d", len(vals))
	}
	if vals[0].Kind != KindStruct {
		return nil, fmt.Errorf("std.values: expected struct, got %s", vals[0].Kind)
	}
	var elems []*Value
	for _, f := range vals[0].Struct.Fields {
		elems = append(elems, f.Value)
	}
	return NewTuple(elems...), nil
}

func (ev *Evaluator) stdMap(evalArgs func() ([]*Value, error), scope *Scope) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.map expects 2 arguments, got %d", len(vals))
	}
	list, fn := vals[0], vals[1]
	if list.Kind != KindList {
		return nil, fmt.Errorf("std.map: first argument must be list")
	}
	if fn.Kind != KindFunction {
		return nil, fmt.Errorf("std.map: second argument must be function")
	}
	var results []*Value
	for _, elem := range list.List.Elements {
		r, err := ev.callFunction(fn, []*Value{elem}, scope)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return NewList(results, nil), nil
}

func (ev *Evaluator) stdFilter(evalArgs func() ([]*Value, error), scope *Scope) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.filter expects 2 arguments, got %d", len(vals))
	}
	list, fn := vals[0], vals[1]
	if list.Kind != KindList {
		return nil, fmt.Errorf("std.filter: first argument must be list")
	}
	if fn.Kind != KindFunction {
		return nil, fmt.Errorf("std.filter: second argument must be function")
	}
	if fn.Function.ReturnType != nil && fn.Function.ReturnType.BaseType != "bool" {
		return nil, fmt.Errorf("std.filter: function must return bool, got %s", fn.Function.ReturnType.BaseType)
	}
	var results []*Value
	for _, elem := range list.List.Elements {
		r, err := ev.callFunction(fn, []*Value{elem}, scope)
		if err != nil {
			return nil, err
		}
		if r.Kind != KindBool {
			return nil, fmt.Errorf("std.filter: function must return bool, got %s", r.Kind)
		}
		if r.Bool {
			results = append(results, elem)
		}
	}
	return NewList(results, list.List.ElementType), nil
}

func (ev *Evaluator) stdReduce(evalArgs func() ([]*Value, error), scope *Scope) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 3 {
		return nil, fmt.Errorf("std.reduce expects 3 arguments, got %d", len(vals))
	}
	list, initial, fn := vals[0], vals[1], vals[2]
	if list.Kind != KindList {
		return nil, fmt.Errorf("std.reduce: first argument must be list")
	}
	// §5.16.2: initial value type must match function return type
	if fn.Kind == KindFunction && fn.Function.ReturnType != nil {
		retBase := fn.Function.ReturnType.BaseType
		if retBase != "" {
			ok := true
			switch retBase {
			case "i32", "i64", "u8", "u16", "u32", "u64":
				ok = initial.Kind == KindInt
			case "f32", "f64":
				ok = initial.Kind == KindFloat
			case "string":
				ok = initial.Kind == KindString
			case "bool":
				ok = initial.Kind == KindBool
			}
			if !ok {
				return nil, fmt.Errorf("std.reduce: initial value type %s doesn't match function return type %s", initial.Kind, retBase)
			}
		}
	}
	acc := initial
	for _, elem := range list.List.Elements {
		r, err := ev.callFunction(fn, []*Value{acc, elem}, scope)
		if err != nil {
			return nil, err
		}
		acc = r
	}
	return acc, nil
}

func (ev *Evaluator) stdSort(evalArgs func() ([]*Value, error), scope *Scope) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.sort expects 2 arguments, got %d", len(vals))
	}
	list, fn := vals[0], vals[1]
	if list.Kind != KindList {
		return nil, fmt.Errorf("std.sort: first argument must be list")
	}
	if fn.Kind != KindFunction {
		return nil, fmt.Errorf("std.sort: second argument must be function")
	}
	if len(fn.Function.Params) != 2 {
		return nil, fmt.Errorf("std.sort: comparator must take exactly 2 parameters, got %d", len(fn.Function.Params))
	}
	if fn.Function.ReturnType != nil && fn.Function.ReturnType.BaseType != "bool" {
		return nil, fmt.Errorf("std.sort: comparator must return bool, got %s", fn.Function.ReturnType.BaseType)
	}
	sorted := make([]*Value, len(list.List.Elements))
	copy(sorted, list.List.Elements)
	var sortErr error
	sort.SliceStable(sorted, func(i, j int) bool {
		if sortErr != nil {
			return false
		}
		r, err := ev.callFunction(fn, []*Value{sorted[i], sorted[j]}, scope)
		if err != nil {
			sortErr = err
			return false
		}
		if r.Kind != KindBool {
			sortErr = fmt.Errorf("std.sort: comparator must return bool, got %s", r.Kind)
			return false
		}
		return r.Bool
	})
	if sortErr != nil {
		return nil, sortErr
	}
	return NewList(sorted, list.List.ElementType), nil
}

func (ev *Evaluator) stdIsNan(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 || vals[0].Kind != KindFloat {
		return nil, fmt.Errorf("std.isNan: expected float argument")
	}
	return Bool(vals[0].FloatIsNaN), nil
}

func (ev *Evaluator) stdIsInf(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 || vals[0].Kind != KindFloat {
		return nil, fmt.Errorf("std.isInf: expected float argument")
	}
	return Bool(!vals[0].FloatIsNaN && vals[0].Float.IsInf()), nil
}

func (ev *Evaluator) stdIsFinite(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 || vals[0].Kind != KindFloat {
		return nil, fmt.Errorf("std.isFinite: expected float argument")
	}
	return Bool(!vals[0].FloatIsNaN && !vals[0].Float.IsInf()), nil
}

func (ev *Evaluator) stdJoin(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.join expects 2 arguments, got %d", len(vals))
	}
	if vals[0].Kind != KindList {
		return nil, fmt.Errorf("std.join: first argument must be [string]")
	}
	if vals[1].Kind != KindString {
		return nil, fmt.Errorf("std.join: separator must be string")
	}
	var parts []string
	for _, elem := range vals[0].List.Elements {
		if elem.Kind != KindString {
			return nil, fmt.Errorf("std.join: list element must be string, got %s", elem.Kind)
		}
		parts = append(parts, elem.Str)
	}
	return String(strings.Join(parts, vals[1].Str)), nil
}

func (ev *Evaluator) stdReplace(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 3 {
		return nil, fmt.Errorf("std.replace expects 3 arguments, got %d", len(vals))
	}
	if vals[0].Kind != KindString || vals[1].Kind != KindString || vals[2].Kind != KindString {
		return nil, fmt.Errorf("std.replace: all arguments must be string")
	}
	if vals[1].Str == "" {
		return vals[0], nil
	}
	return String(strings.ReplaceAll(vals[0].Str, vals[1].Str, vals[2].Str)), nil
}

func (ev *Evaluator) stdSplit(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.split expects 2 arguments, got %d", len(vals))
	}
	if vals[0].Kind != KindString || vals[1].Kind != KindString {
		return nil, fmt.Errorf("std.split: both arguments must be string")
	}
	parts := strings.Split(vals[0].Str, vals[1].Str)
	elems := make([]*Value, len(parts))
	for i, p := range parts {
		elems[i] = String(p)
	}
	return NewList(elems, &TypeInfo{BaseType: "string"}), nil
}

func (ev *Evaluator) stdTrim(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 {
		return nil, fmt.Errorf("std.trim expects 1 argument, got %d", len(vals))
	}
	if vals[0].Kind != KindString {
		return nil, fmt.Errorf("std.trim: expected string argument")
	}
	return String(strings.TrimSpace(vals[0].Str)), nil
}

func (ev *Evaluator) stdLower(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 {
		return nil, fmt.Errorf("std.lower expects 1 argument, got %d", len(vals))
	}
	if vals[0].Kind == KindUndefined {
		return Undefined(), nil
	}
	if vals[0].Kind != KindString {
		return nil, fmt.Errorf("std.lower: expected string argument")
	}
	return String(strings.ToLower(vals[0].Str)), nil
}

func (ev *Evaluator) stdUpper(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 {
		return nil, fmt.Errorf("std.upper expects 1 argument, got %d", len(vals))
	}
	if vals[0].Kind == KindUndefined {
		return Undefined(), nil
	}
	if vals[0].Kind != KindString {
		return nil, fmt.Errorf("std.upper: expected string argument")
	}
	return String(strings.ToUpper(vals[0].Str)), nil
}

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

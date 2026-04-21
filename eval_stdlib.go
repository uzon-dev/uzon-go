// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"sort"
	"strings"

	"github.com/uzon-dev/uzon-go/ast"
)

// unwrapUnion transparently unwraps union and tagged union values (§3.7.1).
func unwrapUnion(v *Value) *Value {
	if v.Kind == KindTaggedUnion {
		return v.TaggedUnion.Inner
	}
	if v.Kind == KindUnion {
		return v.Union.Inner
	}
	return v
}

// --- Standard library (§5.16) ---

func (ev *Evaluator) evalStdCall(name string, args []ast.Expr, scope *Scope) (*Value, error) {
	evalArgs := func() ([]*Value, error) {
		var vals []*Value
		for _, a := range args {
			v, err := ev.evalExpr(a, scope)
			if err != nil {
				return nil, err
			}
			// §D.2: undefined as argument is a runtime error
			if v.Kind == KindUndefined || isUnresolvedIdent(v) {
				return nil, fmt.Errorf("std.%s: undefined argument", name)
			}
			vals = append(vals, v)
		}
		return vals, nil
	}

	switch name {
	case "len":
		return ev.stdLen(evalArgs)
	case "hasKey":
		return ev.stdHasKey(evalArgs)
	case "get":
		return ev.stdGet(evalArgs)
	case "keys":
		return ev.stdKeys(evalArgs)
	case "values":
		return ev.stdValues(evalArgs)
	case "map":
		return ev.stdMap(evalArgs)
	case "filter":
		return ev.stdFilter(evalArgs)
	case "reduce":
		return ev.stdReduce(evalArgs)
	case "sort":
		return ev.stdSort(evalArgs)
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
	case "reverse":
		return ev.stdReverse(evalArgs)
	case "all":
		return ev.stdAll(evalArgs)
	case "any":
		return ev.stdAny(evalArgs)
	case "contains":
		return ev.stdContains(evalArgs)
	case "startsWith":
		return ev.stdStartsWith(evalArgs)
	case "endsWith":
		return ev.stdEndsWith(evalArgs)
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
	vals[0] = unwrapUnion(vals[0])
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
		return nil, typeErrorf("std.len: expected collection or string, got %s", vals[0].Kind)
	}
}

// stdHasKey implements std.hasKey(struct, key) — key existence check (§v0.8).
// List value membership is now handled by the `in` operator.
func (ev *Evaluator) stdHasKey(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.hasKey expects 2 arguments, got %d", len(vals))
	}
	coll, key := unwrapUnion(vals[0]), vals[1]
	if coll.Kind != KindStruct {
		return nil, typeErrorf("std.hasKey: expected struct, got %s", coll.Kind)
	}
	if key.Kind != KindString {
		return nil, typeErrorf("std.hasKey: key must be string")
	}
	return Bool(coll.Struct.Get(key.Str) != nil), nil
}

func (ev *Evaluator) stdGet(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.get expects 2 arguments, got %d", len(vals))
	}
	coll, key := unwrapUnion(vals[0]), vals[1]
	switch coll.Kind {
	case KindList:
		if key.Kind != KindInt {
			return nil, typeErrorf("std.get: list index must be integer, got %s", key.Kind)
		}
		idx := int(key.Int.Int64())
		if idx >= 0 && idx < len(coll.List.Elements) {
			return coll.List.Elements[idx], nil
		}
		return Undefined(), nil
	case KindTuple:
		if key.Kind != KindInt {
			return nil, typeErrorf("std.get: tuple index must be integer, got %s", key.Kind)
		}
		idx := int(key.Int.Int64())
		if idx >= 0 && idx < len(coll.Tuple.Elements) {
			return coll.Tuple.Elements[idx], nil
		}
		return Undefined(), nil
	case KindStruct:
		if key.Kind != KindString {
			return nil, typeErrorf("std.get: struct key must be string, got %s", key.Kind)
		}
		v := coll.Struct.Get(key.Str)
		if v == nil {
			return Undefined(), nil
		}
		return v, nil
	default:
		return nil, typeErrorf("std.get: expected collection, got %s", coll.Kind)
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
		return nil, typeErrorf("std.keys: expected struct, got %s", vals[0].Kind)
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
		return nil, typeErrorf("std.values: expected struct, got %s", vals[0].Kind)
	}
	var elems []*Value
	for _, f := range vals[0].Struct.Fields {
		elems = append(elems, f.Value)
	}
	return NewTuple(elems...), nil
}

func (ev *Evaluator) stdMap(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.map expects 2 arguments, got %d", len(vals))
	}
	list, fn := vals[0], vals[1]
	if list.Kind != KindList {
		return nil, typeErrorf("std.map: first argument must be list")
	}
	if fn.Kind != KindFunction {
		return nil, typeErrorf("std.map: second argument must be function")
	}
	// §5.16.4: std.map requires a 1-parameter function.
	if len(fn.Function.Params) != 1 {
		return nil, typeErrorf("std.map: function must take 1 parameter, got %d", len(fn.Function.Params))
	}
	var results []*Value
	for _, elem := range list.List.Elements {
		r, err := ev.callFunction(fn, []*Value{elem})
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return NewList(results, nil), nil
}

func (ev *Evaluator) stdFilter(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.filter expects 2 arguments, got %d", len(vals))
	}
	list, fn := vals[0], vals[1]
	if list.Kind != KindList {
		return nil, typeErrorf("std.filter: first argument must be list")
	}
	if fn.Kind != KindFunction {
		return nil, typeErrorf("std.filter: second argument must be function")
	}
	if len(fn.Function.Params) != 1 {
		return nil, typeErrorf("std.filter: function must take 1 parameter, got %d", len(fn.Function.Params))
	}
	if fn.Function.ReturnType != nil && fn.Function.ReturnType.BaseType != "bool" {
		return nil, fmt.Errorf("std.filter: function must return bool, got %s", fn.Function.ReturnType.BaseType)
	}
	var results []*Value
	for _, elem := range list.List.Elements {
		r, err := ev.callFunction(fn, []*Value{elem})
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
	out := NewList(results, list.List.ElementType)
	out.Type = list.Type
	return out, nil
}

func (ev *Evaluator) stdReduce(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 3 {
		return nil, fmt.Errorf("std.reduce expects 3 arguments, got %d", len(vals))
	}
	list, initial, fn := vals[0], vals[1], vals[2]
	if list.Kind != KindList {
		return nil, typeErrorf("std.reduce: first argument must be list")
	}
	if fn.Kind != KindFunction {
		return nil, typeErrorf("std.reduce: third argument must be function")
	}
	if len(fn.Function.Params) != 2 {
		return nil, typeErrorf("std.reduce: function must take 2 parameters, got %d", len(fn.Function.Params))
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
		r, err := ev.callFunction(fn, []*Value{acc, elem})
		if err != nil {
			return nil, err
		}
		acc = r
	}
	return acc, nil
}

func (ev *Evaluator) stdSort(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.sort expects 2 arguments, got %d", len(vals))
	}
	list, fn := vals[0], vals[1]
	if list.Kind != KindList {
		return nil, typeErrorf("std.sort: first argument must be list")
	}
	if fn.Kind != KindFunction {
		return nil, typeErrorf("std.sort: second argument must be function")
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
		r, err := ev.callFunction(fn, []*Value{sorted[i], sorted[j]})
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
	out := NewList(sorted, list.List.ElementType)
	out.Type = list.Type
	return out, nil
}

func (ev *Evaluator) stdIsNan(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 || vals[0].Kind != KindFloat {
		return nil, typeErrorf("std.isNan: expected float argument")
	}
	return Bool(vals[0].FloatIsNaN), nil
}

func (ev *Evaluator) stdIsInf(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 || vals[0].Kind != KindFloat {
		return nil, typeErrorf("std.isInf: expected float argument")
	}
	return Bool(!vals[0].FloatIsNaN && vals[0].Float.IsInf()), nil
}

func (ev *Evaluator) stdIsFinite(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 || vals[0].Kind != KindFloat {
		return nil, typeErrorf("std.isFinite: expected float argument")
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
		return nil, typeErrorf("std.join: first argument must be [string]")
	}
	if vals[1].Kind != KindString {
		return nil, typeErrorf("std.join: separator must be string")
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
		return nil, typeErrorf("std.replace: all arguments must be string")
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
		return nil, typeErrorf("std.split: both arguments must be string")
	}
	// §5.16.4: rules checked in order — first match wins
	// 1. delimiter not in input → [input]
	// 2. empty input → [""]
	// 3. empty delimiter → split into Unicode scalar values
	if !strings.Contains(vals[0].Str, vals[1].Str) {
		return NewList([]*Value{String(vals[0].Str)}, &TypeInfo{BaseType: "string"}), nil
	}
	if vals[0].Str == "" {
		return NewList([]*Value{String("")}, &TypeInfo{BaseType: "string"}), nil
	}
	if vals[1].Str == "" {
		runes := []rune(vals[0].Str)
		elems := make([]*Value, len(runes))
		for i, r := range runes {
			elems[i] = String(string(r))
		}
		return NewList(elems, &TypeInfo{BaseType: "string"}), nil
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
		return nil, typeErrorf("std.trim: expected string argument")
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
	if vals[0].Kind != KindString {
		return nil, typeErrorf("std.lower: expected string argument")
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
	if vals[0].Kind != KindString {
		return nil, typeErrorf("std.upper: expected string argument")
	}
	// §5.16.6: Unicode simple (default) case folding — codepoint-by-codepoint
	// Uppercase_Mapping. Only one-to-one mappings apply; full mappings that
	// produce multiple codepoints (e.g. ß → SS) are NOT performed.
	return String(strings.ToUpper(vals[0].Str)), nil
}

func (ev *Evaluator) stdReverse(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 1 {
		return nil, fmt.Errorf("std.reverse expects 1 argument, got %d", len(vals))
	}
	vals[0] = unwrapUnion(vals[0])
	switch vals[0].Kind {
	case KindList:
		n := len(vals[0].List.Elements)
		reversed := make([]*Value, n)
		for i, el := range vals[0].List.Elements {
			reversed[n-1-i] = el
		}
		out := NewList(reversed, vals[0].List.ElementType)
		out.Type = vals[0].Type
		return out, nil
	case KindString:
		runes := []rune(vals[0].Str)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return String(string(runes)), nil
	default:
		return nil, typeErrorf("std.reverse: expected list or string, got %s", vals[0].Kind)
	}
}

func (ev *Evaluator) stdAll(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.all expects 2 arguments, got %d", len(vals))
	}
	list, fn := vals[0], vals[1]
	if list.Kind != KindList {
		return nil, typeErrorf("std.all: first argument must be list")
	}
	if fn.Kind != KindFunction {
		return nil, typeErrorf("std.all: second argument must be function")
	}
	for _, elem := range list.List.Elements {
		r, err := ev.callFunction(fn, []*Value{elem})
		if err != nil {
			return nil, err
		}
		if r.Kind != KindBool {
			return nil, fmt.Errorf("std.all: predicate must return bool, got %s", r.Kind)
		}
		if !r.Bool {
			return Bool(false), nil
		}
	}
	return Bool(true), nil // empty list → true
}

func (ev *Evaluator) stdAny(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.any expects 2 arguments, got %d", len(vals))
	}
	list, fn := vals[0], vals[1]
	if list.Kind != KindList {
		return nil, typeErrorf("std.any: first argument must be list")
	}
	if fn.Kind != KindFunction {
		return nil, typeErrorf("std.any: second argument must be function")
	}
	for _, elem := range list.List.Elements {
		r, err := ev.callFunction(fn, []*Value{elem})
		if err != nil {
			return nil, err
		}
		if r.Kind != KindBool {
			return nil, fmt.Errorf("std.any: predicate must return bool, got %s", r.Kind)
		}
		if r.Bool {
			return Bool(true), nil
		}
	}
	return Bool(false), nil // empty list → false
}

func (ev *Evaluator) stdContains(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.contains expects 2 arguments, got %d", len(vals))
	}
	if vals[0].Kind != KindString || vals[1].Kind != KindString {
		return nil, typeErrorf("std.contains: both arguments must be string")
	}
	return Bool(strings.Contains(vals[0].Str, vals[1].Str)), nil
}

func (ev *Evaluator) stdStartsWith(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.startsWith expects 2 arguments, got %d", len(vals))
	}
	if vals[0].Kind != KindString || vals[1].Kind != KindString {
		return nil, typeErrorf("std.startsWith: both arguments must be string")
	}
	return Bool(strings.HasPrefix(vals[0].Str, vals[1].Str)), nil
}

func (ev *Evaluator) stdEndsWith(evalArgs func() ([]*Value, error)) (*Value, error) {
	vals, err := evalArgs()
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("std.endsWith expects 2 arguments, got %d", len(vals))
	}
	if vals[0].Kind != KindString || vals[1].Kind != KindString {
		return nil, typeErrorf("std.endsWith: both arguments must be string")
	}
	return Bool(strings.HasSuffix(vals[0].Str, vals[1].Str)), nil
}


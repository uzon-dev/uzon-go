// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/uzon-dev/uzon-go/ast"
	"github.com/uzon-dev/uzon-go/token"
)

// --- Binary and unary operations ---

func (ev *Evaluator) evalBinary(e *ast.BinaryExpr, scope *Scope) (*Value, error) {
	// Short-circuit evaluation for logical operators
	if e.Op == token.And || e.Op == token.Or {
		return ev.evalLogical(e, scope)
	}

	left, err := ev.evalExpr(e.Left, scope)
	if err != nil {
		return nil, err
	}
	right, err := ev.evalExpr(e.Right, scope)
	if err != nil {
		return nil, err
	}

	switch e.Op {
	case token.OrElse:
		if left.Kind == KindUndefined {
			return right, nil
		}
		return left, nil
	case token.Is:
		return ev.evalEquality(left, right, false)
	case token.IsNot:
		return ev.evalEquality(left, right, true)
	case token.In:
		return ev.evalIn(left, right)
	case token.Plus, token.Minus, token.Star, token.Slash, token.Percent, token.Caret:
		return ev.evalArithmetic(e.Op, left, right)
	case token.PlusPlus:
		return ev.evalConcat(left, right)
	case token.StarStar:
		return ev.evalRepeat(left, right)
	case token.Lt, token.LtEq, token.Gt, token.GtEq:
		return ev.evalComparison(e.Op, left, right)
	default:
		return nil, fmt.Errorf("unknown binary operator: %v", e.Op)
	}
}

// evalLogical implements short-circuit AND/OR (§5.1).
func (ev *Evaluator) evalLogical(e *ast.BinaryExpr, scope *Scope) (*Value, error) {
	left, err := ev.evalExpr(e.Left, scope)
	if err != nil {
		return nil, err
	}
	if left.Kind != KindBool {
		return nil, fmt.Errorf("logical %v requires bool, got %s", e.Op, left.Kind)
	}
	if e.Op == token.And && !left.Bool {
		return Bool(false), nil
	}
	if e.Op == token.Or && left.Bool {
		return Bool(true), nil
	}
	right, err := ev.evalExpr(e.Right, scope)
	if err != nil {
		return nil, err
	}
	if right.Kind != KindBool {
		return nil, fmt.Errorf("logical %v requires bool, got %s", e.Op, right.Kind)
	}
	return right, nil
}

// evalEquality implements "is" and "is not" comparison (§5.2).
func (ev *Evaluator) evalEquality(left, right *Value, negated bool) (*Value, error) {
	// null and undefined are comparable with any type
	if left.Kind == KindNull || left.Kind == KindUndefined ||
		right.Kind == KindNull || right.Kind == KindUndefined {
		eq := left.Kind == right.Kind
		if negated {
			return Bool(!eq), nil
		}
		return Bool(eq), nil
	}
	// §3.8: function equality is a type error
	if left.Kind == KindFunction || right.Kind == KindFunction {
		return nil, fmt.Errorf("functions cannot be compared for equality")
	}
	// §5.2: comparing structs with different shapes
	if left.Kind == KindStruct && right.Kind == KindStruct {
		if len(left.Struct.Fields) != len(right.Struct.Fields) {
			return nil, fmt.Errorf("cannot compare structs with different shapes")
		}
		for _, f := range left.Struct.Fields {
			if right.Struct.Get(f.Name) == nil {
				return nil, fmt.Errorf("cannot compare structs with different shapes")
			}
		}
	}

	eq := valuesEqual(left, right)
	if negated {
		return Bool(!eq), nil
	}
	return Bool(eq), nil
}

// valuesEqual performs deep equality comparison.
func valuesEqual(a, b *Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KindBool:
		return a.Bool == b.Bool
	case KindInt:
		return a.Int.Cmp(b.Int) == 0
	case KindFloat:
		if a.FloatIsNaN || b.FloatIsNaN {
			return false // NaN != NaN per IEEE 754
		}
		return a.Float.Cmp(b.Float) == 0
	case KindString:
		return a.Str == b.Str
	case KindNull:
		return true
	case KindEnum:
		return a.Enum.Variant == b.Enum.Variant
	case KindStruct:
		if len(a.Struct.Fields) != len(b.Struct.Fields) {
			return false
		}
		for _, f := range a.Struct.Fields {
			bv := b.Struct.Get(f.Name)
			if bv == nil || !valuesEqual(f.Value, bv) {
				return false
			}
		}
		return true
	case KindList:
		if len(a.List.Elements) != len(b.List.Elements) {
			return false
		}
		for i := range a.List.Elements {
			if !valuesEqual(a.List.Elements[i], b.List.Elements[i]) {
				return false
			}
		}
		return true
	case KindTuple:
		if len(a.Tuple.Elements) != len(b.Tuple.Elements) {
			return false
		}
		for i := range a.Tuple.Elements {
			if !valuesEqual(a.Tuple.Elements[i], b.Tuple.Elements[i]) {
				return false
			}
		}
		return true
	case KindTaggedUnion:
		return a.TaggedUnion.Tag == b.TaggedUnion.Tag &&
			valuesEqual(a.TaggedUnion.Inner, b.TaggedUnion.Inner)
	default:
		return false
	}
}

// evalIn implements the "in" membership operator (§5.8.1).
func (ev *Evaluator) evalIn(left, right *Value) (*Value, error) {
	if right.Kind != KindList {
		return nil, fmt.Errorf("'in' requires a list on the right side, got %s", right.Kind)
	}
	if len(right.List.Elements) > 0 && left.Kind != KindNull {
		var listElem *Value
		for _, el := range right.List.Elements {
			if el.Kind != KindNull {
				listElem = el
				break
			}
		}
		if listElem != nil && left.Kind != listElem.Kind {
			return nil, fmt.Errorf("'in' type mismatch: searching for %s in list of %s", left.Kind, listElem.Kind)
		}
		if listElem != nil && left.Kind == KindEnum && listElem.Kind == KindEnum {
			lt, rt := left.Type, listElem.Type
			if lt != nil && rt != nil && lt.Name != "" && rt.Name != "" && lt.Name != rt.Name {
				return nil, fmt.Errorf("'in' enum type mismatch: %s vs %s", lt.Name, rt.Name)
			}
		}
		if listElem != nil && left.Kind == KindInt && listElem.Kind == KindInt {
			lt := left.Type
			rt := listElem.Type
			if lt == nil {
				lt = &TypeInfo{BaseType: "i64", BitSize: 64, Signed: true}
			}
			if rt == nil {
				rt = &TypeInfo{BaseType: "i64", BitSize: 64, Signed: true}
			}
			if lt.BaseType != rt.BaseType {
				return nil, fmt.Errorf("'in' numeric type mismatch: %s vs %s", lt.BaseType, rt.BaseType)
			}
		}
	}
	for _, elem := range right.List.Elements {
		if valuesEqual(left, elem) {
			return Bool(true), nil
		}
	}
	return Bool(false), nil
}

// unwrapTaggedUnion transparently unwraps tagged unions (§3.7.1).
func unwrapTaggedUnion(v *Value) *Value {
	if v.Kind == KindTaggedUnion {
		return v.TaggedUnion.Inner
	}
	return v
}

// reconcileNumericTypes enforces same-type arithmetic/comparison (§5, §5.3).
// Untyped (adoptable) literals adopt the other operand's type.
func reconcileNumericTypes(left, right *Value) (*TypeInfo, error) {
	lt := left.Type
	rt := right.Type
	if lt == nil {
		lt = &TypeInfo{BaseType: "i64", BitSize: 64, Signed: true}
	}
	if rt == nil {
		rt = &TypeInfo{BaseType: "i64", BitSize: 64, Signed: true}
	}
	if lt.BaseType == rt.BaseType {
		return lt, nil
	}
	if left.Adoptable && !right.Adoptable {
		if left.Kind == KindInt && isIntegerType(rt.BaseType) {
			if err := checkIntRange(left.Int, rt.BitSize, rt.Signed); err != nil {
				return nil, fmt.Errorf("literal adoption to %s: %w", rt.BaseType, err)
			}
		}
		left.Type = rt
		left.Adoptable = false
		return rt, nil
	}
	if right.Adoptable && !left.Adoptable {
		if right.Kind == KindInt && isIntegerType(lt.BaseType) {
			if err := checkIntRange(right.Int, lt.BitSize, lt.Signed); err != nil {
				return nil, fmt.Errorf("literal adoption to %s: %w", lt.BaseType, err)
			}
		}
		right.Type = lt
		right.Adoptable = false
		return lt, nil
	}
	if left.Adoptable && right.Adoptable {
		return lt, nil
	}
	return nil, fmt.Errorf("type mismatch: %s and %s", lt.BaseType, rt.BaseType)
}

func (ev *Evaluator) evalArithmetic(op token.Type, left, right *Value) (*Value, error) {
	left = unwrapTaggedUnion(left)
	right = unwrapTaggedUnion(right)
	if left.Kind == KindUndefined || right.Kind == KindUndefined {
		return nil, fmt.Errorf("arithmetic on undefined")
	}
	if left.Kind == KindInt && right.Kind == KindInt {
		ti, err := reconcileNumericTypes(left, right)
		if err != nil {
			return nil, fmt.Errorf("arithmetic: %w", err)
		}
		return ev.intArith(op, left.Int, right.Int, ti)
	}
	if left.Kind == KindFloat && right.Kind == KindFloat {
		ti, err := reconcileNumericTypes(left, right)
		if err != nil {
			return nil, fmt.Errorf("arithmetic: %w", err)
		}
		return ev.floatArith(op, left, right, ti)
	}
	return nil, fmt.Errorf("arithmetic requires same numeric type, got %s and %s", left.Kind, right.Kind)
}

func (ev *Evaluator) intArith(op token.Type, a, b *big.Int, ti *TypeInfo) (*Value, error) {
	r := new(big.Int)
	switch op {
	case token.Plus:
		r.Add(a, b)
	case token.Minus:
		r.Sub(a, b)
	case token.Star:
		r.Mul(a, b)
	case token.Slash:
		if b.Sign() == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		r.Quo(a, b)
	case token.Percent:
		if b.Sign() == 0 {
			return nil, fmt.Errorf("modulo by zero")
		}
		r.Rem(a, b)
	case token.Caret:
		if b.Sign() < 0 {
			return nil, fmt.Errorf("negative exponent in integer exponentiation")
		}
		if !b.IsInt64() || b.Int64() > 10000 {
			return nil, fmt.Errorf("exponent too large")
		}
		r.Exp(a, b, nil)
	default:
		return nil, fmt.Errorf("unknown arithmetic op: %v", op)
	}
	// §5.3: check overflow for typed integer arithmetic
	if ti != nil && ti.BitSize > 0 && isIntegerType(ti.BaseType) {
		if err := checkIntRange(r, ti.BitSize, ti.Signed); err != nil {
			return nil, fmt.Errorf("integer overflow: %w", err)
		}
	}
	return &Value{Kind: KindInt, Int: r, Type: ti}, nil
}

func (ev *Evaluator) floatArith(op token.Type, left, right *Value, ti *TypeInfo) (*Value, error) {
	if ti == nil {
		ti = &TypeInfo{BaseType: "f64", BitSize: 64}
	}
	nanResult := func() *Value {
		return &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true, Type: ti}
	}
	if left.FloatIsNaN || right.FloatIsNaN {
		return nanResult(), nil
	}
	a, b := left.Float, right.Float
	r := new(big.Float).SetPrec(53)
	switch op {
	case token.Plus:
		r.Add(a, b)
	case token.Minus:
		r.Sub(a, b)
	case token.Star:
		r.Mul(a, b)
	case token.Slash:
		if b.Sign() == 0 {
			if a.Sign() == 0 {
				return nanResult(), nil
			} else if a.Sign() > 0 {
				r.SetInf(false)
			} else {
				r.SetInf(true)
			}
		} else {
			r.Quo(a, b)
		}
	case token.Percent:
		af, _ := a.Float64()
		bf, _ := b.Float64()
		result := math.Mod(af, bf)
		if math.IsNaN(result) {
			return nanResult(), nil
		}
		r.SetFloat64(result)
	case token.Caret:
		af, _ := a.Float64()
		bf, _ := b.Float64()
		result := math.Pow(af, bf)
		if math.IsNaN(result) {
			return nanResult(), nil
		}
		r.SetFloat64(result)
	default:
		return nil, fmt.Errorf("unknown arithmetic op: %v", op)
	}
	return &Value{Kind: KindFloat, Float: r, Type: ti}, nil
}

// evalConcat implements "++" string/list concatenation (§5.8.2).
func (ev *Evaluator) evalConcat(left, right *Value) (*Value, error) {
	left = unwrapTaggedUnion(left)
	right = unwrapTaggedUnion(right)
	if left.Kind == KindString && right.Kind == KindString {
		return String(left.Str + right.Str), nil
	}
	if left.Kind == KindList && right.Kind == KindList {
		if len(left.List.Elements) > 0 && len(right.List.Elements) > 0 {
			lk := left.List.Elements[0].Kind
			rk := right.List.Elements[0].Kind
			if lk != KindNull && rk != KindNull && lk != rk {
				return nil, fmt.Errorf("++ list element type mismatch: %s vs %s", lk, rk)
			}
		}
		elems := make([]*Value, 0, len(left.List.Elements)+len(right.List.Elements))
		elems = append(elems, left.List.Elements...)
		elems = append(elems, right.List.Elements...)
		return NewList(elems, left.List.ElementType), nil
	}
	return nil, fmt.Errorf("++ requires string or list operands, got %s and %s", left.Kind, right.Kind)
}

// evalRepeat implements "**" string/list repetition.
func (ev *Evaluator) evalRepeat(left, right *Value) (*Value, error) {
	left = unwrapTaggedUnion(left)
	right = unwrapTaggedUnion(right)
	if right.Kind != KindInt {
		return nil, fmt.Errorf("** requires integer right operand, got %s", right.Kind)
	}
	n := right.Int.Int64()
	if n < 0 {
		return nil, fmt.Errorf("** requires non-negative integer, got %d", n)
	}
	if left.Kind == KindString {
		return String(strings.Repeat(left.Str, int(n))), nil
	}
	if left.Kind == KindList {
		var elems []*Value
		for i := int64(0); i < n; i++ {
			elems = append(elems, left.List.Elements...)
		}
		return NewList(elems, left.List.ElementType), nil
	}
	return nil, fmt.Errorf("** requires string or list left operand, got %s", left.Kind)
}

// evalComparison implements ordered comparison operators (§5.3).
func (ev *Evaluator) evalComparison(op token.Type, left, right *Value) (*Value, error) {
	if left.Kind == KindTaggedUnion && right.Kind == KindTaggedUnion {
		return nil, fmt.Errorf("ordered comparison between two tagged union values is not allowed")
	}
	left = unwrapTaggedUnion(left)
	right = unwrapTaggedUnion(right)

	if left.Kind == KindInt && right.Kind == KindInt {
		if _, err := reconcileNumericTypes(left, right); err != nil {
			return nil, fmt.Errorf("comparison: %w", err)
		}
		return Bool(cmpResult(op, left.Int.Cmp(right.Int))), nil
	}
	if left.Kind == KindFloat && right.Kind == KindFloat {
		if _, err := reconcileNumericTypes(left, right); err != nil {
			return nil, fmt.Errorf("comparison: %w", err)
		}
		if left.FloatIsNaN || right.FloatIsNaN {
			return Bool(false), nil
		}
		return Bool(cmpResult(op, left.Float.Cmp(right.Float))), nil
	}
	if left.Kind == KindString && right.Kind == KindString {
		return Bool(cmpResult(op, strings.Compare(left.Str, right.Str))), nil
	}
	return nil, fmt.Errorf("comparison requires same numeric or string type, got %s and %s", left.Kind, right.Kind)
}

func cmpResult(op token.Type, cmp int) bool {
	switch op {
	case token.Lt:
		return cmp < 0
	case token.LtEq:
		return cmp <= 0
	case token.Gt:
		return cmp > 0
	case token.GtEq:
		return cmp >= 0
	}
	return false
}

func (ev *Evaluator) evalUnary(e *ast.UnaryExpr, scope *Scope) (*Value, error) {
	operand, err := ev.evalExpr(e.Operand, scope)
	if err != nil {
		return nil, err
	}
	switch e.Op {
	case token.Minus:
		if operand.Kind == KindInt {
			return &Value{Kind: KindInt, Int: new(big.Int).Neg(operand.Int), Type: operand.Type}, nil
		}
		if operand.Kind == KindFloat {
			return &Value{Kind: KindFloat, Float: new(big.Float).Neg(operand.Float), Type: operand.Type}, nil
		}
		return nil, fmt.Errorf("unary minus requires numeric operand, got %s", operand.Kind)
	case token.Not:
		if operand.Kind != KindBool {
			return nil, fmt.Errorf("'not' requires bool, got %s", operand.Kind)
		}
		return Bool(!operand.Bool), nil
	}
	return nil, fmt.Errorf("unknown unary operator: %v", e.Op)
}

// --- Control flow ---

// evalIf implements "if cond then a else b" (§5.9).
// Both branches are speculatively evaluated for type checking.
func (ev *Evaluator) evalIf(e *ast.IfExpr, scope *Scope) (*Value, error) {
	cond, err := ev.evalExpr(e.Cond, scope)
	if err != nil {
		return nil, err
	}
	if cond.Kind != KindBool {
		return nil, fmt.Errorf("if condition must be bool, got %s", cond.Kind)
	}
	if cond.Bool {
		thenVal, err := ev.evalExpr(e.Then, scope)
		if err != nil {
			return nil, err
		}
		if elseVal, elseErr := ev.evalExpr(e.Else, scope); elseErr == nil {
			if thenVal.Kind != elseVal.Kind {
				return nil, fmt.Errorf("if/else branch type mismatch: then is %s, else is %s", thenVal.Kind, elseVal.Kind)
			}
		}
		return thenVal, nil
	}
	elseVal, err := ev.evalExpr(e.Else, scope)
	if err != nil {
		return nil, err
	}
	if thenVal, thenErr := ev.evalExpr(e.Then, scope); thenErr == nil {
		if thenVal.Kind != elseVal.Kind {
			return nil, fmt.Errorf("if/else branch type mismatch: then is %s, else is %s", thenVal.Kind, elseVal.Kind)
		}
	}
	return elseVal, nil
}

// evalCase implements pattern matching (§5.10).
// All branches are speculatively evaluated for type consistency.
func (ev *Evaluator) evalCase(e *ast.CaseExpr, scope *Scope) (*Value, error) {
	scrutinee, err := ev.evalExpr(e.Scrutinee, scope)
	if err != nil {
		return nil, err
	}
	if scrutinee.Kind == KindUndefined {
		return nil, fmt.Errorf("case scrutinee is undefined")
	}

	var result *Value
	var branchTypes []ValueKind

	for _, w := range e.Whens {
		if !w.IsNamed {
			if _, ok := w.Value.(*ast.UndefinedExpr); ok {
				return nil, fmt.Errorf("'when undefined' is not allowed in case expressions")
			}
		}
		matched := false
		if w.IsNamed {
			if scrutinee.Kind == KindTaggedUnion && scrutinee.TaggedUnion.Tag == w.VariantName {
				matched = true
			}
		} else {
			wVal, err := ev.evalExpr(w.Value, scope)
			if err != nil {
				return nil, err
			}
			// §5.10: scrutinee and when values must be the same type
			if wVal.Kind != scrutinee.Kind &&
				wVal.Kind != KindNull && scrutinee.Kind != KindNull &&
				wVal.Kind != KindUndefined && scrutinee.Kind != KindUndefined &&
				!(wVal.Type != nil && wVal.Type.Name == "__ident__") {
				return nil, fmt.Errorf("case type mismatch: scrutinee is %s but when value is %s", scrutinee.Kind, wVal.Kind)
			}
			if scrutinee.Kind == KindEnum && wVal.Type != nil && wVal.Type.Name == "__ident__" {
				if wVal.Str == scrutinee.Enum.Variant {
					matched = true
				}
			} else if valuesEqual(scrutinee, wVal) {
				matched = true
			}
		}

		thenVal, thenErr := ev.evalExpr(w.Then, scope)
		if thenErr == nil {
			branchTypes = append(branchTypes, thenVal.Kind)
		}
		if matched && result == nil {
			if thenErr != nil {
				return nil, thenErr
			}
			result = thenVal
		}
	}

	elseVal, elseErr := ev.evalExpr(e.Else, scope)
	if elseErr == nil {
		branchTypes = append(branchTypes, elseVal.Kind)
	}

	// §5.9/§5.10: all branches must have the same type
	if len(branchTypes) > 1 {
		for i := 1; i < len(branchTypes); i++ {
			if branchTypes[i] != branchTypes[0] {
				return nil, fmt.Errorf("case branch type mismatch: got %s and %s", branchTypes[0], branchTypes[i])
			}
		}
	}

	if result != nil {
		return result, nil
	}
	if elseErr != nil {
		return nil, elseErr
	}
	return elseVal, nil
}

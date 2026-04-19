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

// isUnresolvedIdent returns true if the value is a bare identifier that was not
// resolved by scope lookup or enum variant resolution. Per §5.12, such identifiers
// behave as undefined in non-enum contexts.
func isUnresolvedIdent(v *Value) bool {
	return v != nil && v.Type != nil && v.Type.Name == "__ident__"
}

// checkUndefinedOperands returns a PosError pointing to the undefined
// operand(s) in a binary expression, so error locations reference the source of
// the undefined value (e.g. "price") rather than the operator (e.g. "*").
func checkUndefinedOperands(e *ast.BinaryExpr, left, right *Value) error {
	leftUndef := left.Kind == KindUndefined || isUnresolvedIdent(left)
	rightUndef := right.Kind == KindUndefined || isUnresolvedIdent(right)
	if leftUndef && rightUndef {
		return &PosError{Pos: e.Left.Pos(), Msg: "undefined value in expression",
			Cause: &PosError{Pos: e.Right.Pos(), Msg: "undefined value in expression"}}
	}
	if leftUndef {
		return &PosError{Pos: e.Left.Pos(), Msg: "undefined value in expression"}
	}
	if rightUndef {
		return &PosError{Pos: e.Right.Pos(), Msg: "undefined value in expression"}
	}
	return nil
}

// promoteIntToFloat promotes an adoptable integer literal to a float value.
// §5: "An integer literal may also adopt a float type when combined with a
// float operand. This cross-category promotion applies only from integer
// literals to float types, never the reverse."
func promoteIntToFloat(intVal *Value, floatType *TypeInfo) *Value {
	f := new(big.Float).SetPrec(53).SetInt(intVal.Int)
	ti := floatType
	if ti == nil {
		ti = &TypeInfo{BaseType: "f64", BitSize: 64}
	}
	return &Value{Kind: KindFloat, Float: f, Type: ti}
}

// --- Binary and unary operations ---

func (ev *Evaluator) evalBinary(e *ast.BinaryExpr, scope *Scope) (*Value, error) {
	// Short-circuit evaluation for logical operators
	if e.Op == token.And || e.Op == token.Or {
		return ev.evalLogical(e, scope)
	}
	// §5.7: or else has special speculative evaluation
	if e.Op == token.OrElse {
		return ev.evalOrElse(e, scope)
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
	case token.Is:
		return ev.evalEquality(left, right, false)
	case token.IsNot:
		return ev.evalEquality(left, right, true)
	case token.In:
		return ev.evalIn(left, right)
	case token.Plus, token.Minus, token.Star, token.Slash, token.Percent, token.Caret:
		if err := checkUndefinedOperands(e, left, right); err != nil {
			return nil, err
		}
		return ev.evalArithmetic(e.Op, left, right)
	case token.PlusPlus:
		if err := checkUndefinedOperands(e, left, right); err != nil {
			return nil, err
		}
		return ev.evalConcat(left, right)
	case token.StarStar:
		if err := checkUndefinedOperands(e, left, right); err != nil {
			return nil, err
		}
		return ev.evalRepeat(left, right)
	case token.Lt, token.LtEq, token.Gt, token.GtEq:
		if err := checkUndefinedOperands(e, left, right); err != nil {
			return nil, err
		}
		return ev.evalComparison(e.Op, left, right)
	default:
		return nil, fmt.Errorf("unknown binary operator: %v", e.Op)
	}
}

// evalOrElse implements "or else" with speculative evaluation (§5.7, §D.5).
// When left is defined, right is still evaluated for type checking but
// runtime errors are suppressed; type errors always propagate.
func (ev *Evaluator) evalOrElse(e *ast.BinaryExpr, scope *Scope) (*Value, error) {
	left, err := ev.evalExpr(e.Left, scope)
	if err != nil {
		return nil, err
	}
	if left.Kind == KindUndefined || isUnresolvedIdent(left) {
		right, err := ev.evalExpr(e.Right, scope)
		if err != nil {
			return nil, err
		}
		// §5.7: validate type compatibility even when left is undefined,
		// using any type info propagated through `to` / annotations.
		if left.Kind == KindUndefined && left.Type != nil && left.Type.BaseType != "" &&
			right.Kind != KindUndefined && right.Kind != KindNull && !isUnresolvedIdent(right) {
			if !leftTypeMatchesRightKind(left.Type, right) {
				return nil, typeErrorf("or else type mismatch: %s and %s", left.Type.BaseType, right.Kind)
			}
		}
		return right, nil
	}
	// Left is defined — speculatively evaluate right for type checking
	right, rightErr := ev.evalExpr(e.Right, scope)
	if rightErr != nil {
		if isTypeError(rightErr) {
			return nil, rightErr
		}
		// suppress runtime error
	} else {
		if !operandTypesCompatible(left, right) {
			return nil, typeErrorf("or else type mismatch: %s and %s", left.Kind, right.Kind)
		}
		if left.Kind == KindNull && left.Type == nil && right.Type != nil && right.Type.Name != "" {
			left.Type = right.Type
		}
	}
	return left, nil
}

// evalLogical implements short-circuit AND/OR with speculative evaluation (§5.6, §D.5).
// When short-circuiting, the skipped side is still evaluated for type checking.
func (ev *Evaluator) evalLogical(e *ast.BinaryExpr, scope *Scope) (*Value, error) {
	left, err := ev.evalExpr(e.Left, scope)
	if err != nil {
		return nil, err
	}
	if left.Kind != KindBool {
		return nil, typeErrorf("logical %v requires bool, got %s", e.Op, left.Kind)
	}
	if e.Op == token.And && !left.Bool {
		// §D.5: speculatively evaluate right for type checking
		if right, rightErr := ev.evalExpr(e.Right, scope); rightErr != nil {
			if isTypeError(rightErr) {
				return nil, rightErr
			}
		} else if right.Kind != KindBool {
			return nil, typeErrorf("logical %v requires bool, got %s", e.Op, right.Kind)
		}
		return Bool(false), nil
	}
	if e.Op == token.Or && left.Bool {
		// §D.5: speculatively evaluate right for type checking
		if right, rightErr := ev.evalExpr(e.Right, scope); rightErr != nil {
			if isTypeError(rightErr) {
				return nil, rightErr
			}
		} else if right.Kind != KindBool {
			return nil, typeErrorf("logical %v requires bool, got %s", e.Op, right.Kind)
		}
		return Bool(true), nil
	}
	right, err := ev.evalExpr(e.Right, scope)
	if err != nil {
		return nil, err
	}
	if right.Kind != KindBool {
		return nil, typeErrorf("logical %v requires bool, got %s", e.Op, right.Kind)
	}
	return right, nil
}

// evalEquality implements "is" and "is not" comparison (§5.2).
func (ev *Evaluator) evalEquality(left, right *Value, negated bool) (*Value, error) {
	// Treat unresolved identifiers as undefined for comparison
	if isUnresolvedIdent(left) {
		left = Undefined()
	}
	if isUnresolvedIdent(right) {
		right = Undefined()
	}
	// §v0.8: untagged union comparison rules
	if left.Kind == KindUnion && right.Kind == KindUnion {
		// Different member type sets → type error
		if !sameUnionMemberSets(left.Union.MemberTypes, right.Union.MemberTypes) {
			return nil, typeErrorf("cannot compare unions with different member types")
		}
		// Same union type, different runtime type → false
		if left.Union.Inner.Kind != right.Union.Inner.Kind {
			return Bool(negated), nil
		}
		// Same runtime type → deep value comparison
		eq := valuesEqual(left.Union.Inner, right.Union.Inner)
		if negated {
			return Bool(!eq), nil
		}
		return Bool(eq), nil
	}
	// Union vs non-union: transparent — unwrap union
	if left.Kind == KindUnion {
		left = left.Union.Inner
	}
	if right.Kind == KindUnion {
		right = right.Union.Inner
	}
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
		return nil, typeErrorf("functions cannot be compared for equality")
	}
	// §3.7.2: tagged union vs non-tagged-union comparison is a type error
	if left.Kind == KindTaggedUnion && right.Kind != KindTaggedUnion {
		return nil, typeErrorf("cannot compare tagged union with %s", right.Kind)
	}
	if left.Kind != KindTaggedUnion && right.Kind == KindTaggedUnion {
		return nil, typeErrorf("cannot compare %s with tagged union", left.Kind)
	}
	// §5.2: comparing structs with different shapes
	if left.Kind == KindStruct && right.Kind == KindStruct {
		if len(left.Struct.Fields) != len(right.Struct.Fields) {
			return nil, typeErrorf("cannot compare structs with different shapes")
		}
		for _, f := range left.Struct.Fields {
			if right.Struct.Get(f.Name) == nil {
				return nil, typeErrorf("cannot compare structs with different shapes")
			}
		}
	}
	// §5.2: tuples of different length are a type error
	if left.Kind == KindTuple && right.Kind == KindTuple {
		if len(left.Tuple.Elements) != len(right.Tuple.Elements) {
			return nil, typeErrorf("cannot compare tuples of different length (%d vs %d)",
				len(left.Tuple.Elements), len(right.Tuple.Elements))
		}
	}
	// §5.2: lists with different element types are a type error
	if left.Kind == KindList && right.Kind == KindList {
		if left.List.ElementType != nil && right.List.ElementType != nil &&
			left.List.ElementType.BaseType != right.List.ElementType.BaseType {
			return nil, typeErrorf("cannot compare lists with different element types (%s vs %s)",
				left.List.ElementType.BaseType, right.List.ElementType.BaseType)
		}
	}

	// §5: integer literal adopts float type for equality
	if left.Kind == KindInt && right.Kind == KindFloat && left.Adoptable {
		left = promoteIntToFloat(left, right.Type)
	} else if left.Kind == KindFloat && right.Kind == KindInt && right.Adoptable {
		right = promoteIntToFloat(right, left.Type)
	}

	// §5.2: comparing different types is a type error
	if left.Kind != right.Kind {
		return nil, typeErrorf("cannot compare %s with %s", left.Kind, right.Kind)
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
	case KindUnion:
		return valuesEqual(a.Union.Inner, b.Union.Inner)
	default:
		return false
	}
}

// evalIn implements the "in" membership operator (§5.8.1).
// v0.8: extended to list, tuple, and struct.
func (ev *Evaluator) evalIn(left, right *Value) (*Value, error) {
	// §3.1: undefined as operand is a runtime error
	if left.Kind == KindUndefined || right.Kind == KindUndefined {
		return nil, fmt.Errorf("'in' on undefined")
	}
	if isUnresolvedIdent(right) {
		return nil, fmt.Errorf("'in' on undefined")
	}

	switch right.Kind {
	case KindList:
		return ev.evalInList(left, right)
	case KindTuple:
		return ev.evalInTuple(left, right)
	case KindStruct:
		return ev.evalInStruct(left, right)
	default:
		return nil, fmt.Errorf("'in' requires list, tuple, or struct on the right side, got %s", right.Kind)
	}
}

// evalInList handles "x in [list]" with type checking.
func (ev *Evaluator) evalInList(left, right *Value) (*Value, error) {
	// §3.5 type-context inference: bare ident in "x in [enum_list]" resolves as variant
	if isUnresolvedIdent(left) && len(right.List.Elements) > 0 {
		resolved := false
		for _, el := range right.List.Elements {
			if el.Kind == KindEnum {
				left = &Value{Kind: KindEnum, Enum: &EnumValue{
					Variant:  left.Str,
					Variants: el.Enum.Variants,
				}, Type: el.Type}
				resolved = true
				break
			}
		}
		if !resolved {
			return nil, fmt.Errorf("'in' on undefined")
		}
	}
	if isUnresolvedIdent(left) {
		return nil, fmt.Errorf("'in' on undefined")
	}
	// §v0.8: empty list — always false, element type inferred from left
	if len(right.List.Elements) == 0 {
		return Bool(false), nil
	}
	if left.Kind != KindNull {
		var listElem *Value
		for _, el := range right.List.Elements {
			if el.Kind != KindNull && el.Kind != KindUndefined {
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
		if elem.Kind == KindUndefined {
			continue // undefined elements → skip
		}
		if valuesEqual(left, elem) {
			return Bool(true), nil
		}
	}
	return Bool(false), nil
}

// evalInTuple handles "x in (tuple)" — heterogeneous, type mismatch skipped.
func (ev *Evaluator) evalInTuple(left, right *Value) (*Value, error) {
	if isUnresolvedIdent(left) {
		return nil, fmt.Errorf("'in' on undefined")
	}
	for _, elem := range right.Tuple.Elements {
		if elem.Kind == KindUndefined {
			continue // undefined elements → skip
		}
		if valuesEqual(left, elem) {
			return Bool(true), nil
		}
	}
	return Bool(false), nil
}

// evalInStruct handles "x in {struct}" — value membership (not key).
func (ev *Evaluator) evalInStruct(left, right *Value) (*Value, error) {
	if isUnresolvedIdent(left) {
		return nil, fmt.Errorf("'in' on undefined")
	}
	for _, field := range right.Struct.Fields {
		if field.Value.Kind == KindUndefined {
			continue // undefined values → skip
		}
		if valuesEqual(left, field.Value) {
			return Bool(true), nil
		}
	}
	return Bool(false), nil
}

// unwrapTaggedUnion transparently unwraps tagged and untagged unions (§3.6, §3.7.1).
func unwrapTaggedUnion(v *Value) *Value {
	if v.Kind == KindTaggedUnion {
		return v.TaggedUnion.Inner
	}
	if v.Kind == KindUnion {
		return v.Union.Inner
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
	if left.Kind == KindUndefined || right.Kind == KindUndefined ||
		isUnresolvedIdent(left) || isUnresolvedIdent(right) {
		return nil, fmt.Errorf("arithmetic on undefined")
	}
	// When both operands are adoptable (literals), the result remains
	// adoptable so it can be narrowed to a typed context (e.g., i32 parameter).
	bothAdoptable := left.Adoptable && right.Adoptable
	if left.Kind == KindInt && right.Kind == KindInt {
		ti, err := reconcileNumericTypes(left, right)
		if err != nil {
			return nil, fmt.Errorf("arithmetic: %w", err)
		}
		result, err := ev.intArith(op, left.Int, right.Int, ti)
		if err != nil {
			return nil, err
		}
		result.Adoptable = bothAdoptable
		return result, nil
	}
	if left.Kind == KindFloat && right.Kind == KindFloat {
		ti, err := reconcileNumericTypes(left, right)
		if err != nil {
			return nil, fmt.Errorf("arithmetic: %w", err)
		}
		result, err := ev.floatArith(op, left, right, ti)
		if err != nil {
			return nil, err
		}
		result.Adoptable = bothAdoptable
		return result, nil
	}
	// §5: integer literal adopts float type (cross-category promotion)
	if left.Kind == KindInt && right.Kind == KindFloat && left.Adoptable {
		promoted := promoteIntToFloat(left, right.Type)
		ti, err := reconcileNumericTypes(promoted, right)
		if err != nil {
			return nil, fmt.Errorf("arithmetic: %w", err)
		}
		return ev.floatArith(op, promoted, right, ti)
	}
	if left.Kind == KindFloat && right.Kind == KindInt && right.Adoptable {
		promoted := promoteIntToFloat(right, left.Type)
		ti, err := reconcileNumericTypes(left, promoted)
		if err != nil {
			return nil, fmt.Errorf("arithmetic: %w", err)
		}
		return ev.floatArith(op, left, promoted, ti)
	}
	return nil, typeErrorf("arithmetic requires same numeric type, got %s and %s", left.Kind, right.Kind)
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
		// §5.3: negative base with non-integer exponent is a runtime error.
		if af < 0 && bf != math.Trunc(bf) && !math.IsInf(bf, 0) && !math.IsNaN(bf) {
			return nil, fmt.Errorf("negative base with non-integer exponent (%g ^ %g) is undefined in real numbers", af, bf)
		}
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
	// §3.1: undefined in concatenation is a runtime error
	if left.Kind == KindUndefined || right.Kind == KindUndefined ||
		isUnresolvedIdent(left) || isUnresolvedIdent(right) {
		return nil, fmt.Errorf("'++' on undefined")
	}
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
	return nil, typeErrorf("++ requires string or list operands, got %s and %s", left.Kind, right.Kind)
}

// evalRepeat implements "**" string/list repetition.
func (ev *Evaluator) evalRepeat(left, right *Value) (*Value, error) {
	// §3.1: undefined in repetition is a runtime error
	if left.Kind == KindUndefined || right.Kind == KindUndefined ||
		isUnresolvedIdent(left) || isUnresolvedIdent(right) {
		return nil, fmt.Errorf("'**' on undefined")
	}
	left = unwrapTaggedUnion(left)
	right = unwrapTaggedUnion(right)
	if right.Kind != KindInt {
		return nil, typeErrorf("** requires integer right operand, got %s", right.Kind)
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
		// Preserve element type info so `[x] ** 0` retains inferred type (§3.4).
		elemType := left.List.ElementType
		if elemType == nil && len(left.List.Elements) > 0 {
			elemType = left.List.Elements[0].Type
		}
		return NewList(elems, elemType), nil
	}
	return nil, typeErrorf("** requires string or list left operand, got %s", left.Kind)
}

// evalComparison implements ordered comparison operators (§5.3).
func (ev *Evaluator) evalComparison(op token.Type, left, right *Value) (*Value, error) {
	// §3.1: undefined in comparison is a runtime error
	if left.Kind == KindUndefined || right.Kind == KindUndefined ||
		isUnresolvedIdent(left) || isUnresolvedIdent(right) {
		return nil, fmt.Errorf("comparison on undefined")
	}
	// §v0.8: ordered comparison on functions, unions, and tagged unions is a type error
	if left.Kind == KindFunction || right.Kind == KindFunction {
		return nil, typeErrorf("ordered comparison on functions is not allowed")
	}
	if left.Kind == KindUnion || right.Kind == KindUnion {
		return nil, typeErrorf("ordered comparison on untagged unions is not allowed")
	}
	if left.Kind == KindTaggedUnion && right.Kind == KindTaggedUnion {
		return nil, typeErrorf("ordered comparison between two tagged union values is not allowed")
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
	// §5: integer literal adopts float type for comparison
	if left.Kind == KindInt && right.Kind == KindFloat && left.Adoptable {
		promoted := promoteIntToFloat(left, right.Type)
		if promoted.FloatIsNaN || right.FloatIsNaN {
			return Bool(false), nil
		}
		return Bool(cmpResult(op, promoted.Float.Cmp(right.Float))), nil
	}
	if left.Kind == KindFloat && right.Kind == KindInt && right.Adoptable {
		promoted := promoteIntToFloat(right, left.Type)
		if left.FloatIsNaN || promoted.FloatIsNaN {
			return Bool(false), nil
		}
		return Bool(cmpResult(op, left.Float.Cmp(promoted.Float))), nil
	}
	if left.Kind == KindString && right.Kind == KindString {
		return Bool(cmpResult(op, strings.Compare(left.Str, right.Str))), nil
	}
	return nil, typeErrorf("comparison requires same numeric or string type, got %s and %s", left.Kind, right.Kind)
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
			negated := new(big.Int).Neg(operand.Int)
			if operand.Type != nil && operand.Type.BitSize > 0 && isIntegerType(operand.Type.BaseType) {
				if err := checkIntRange(negated, operand.Type.BitSize, operand.Type.Signed); err != nil {
					return nil, fmt.Errorf("unary negation: %w", err)
				}
			}
			return &Value{Kind: KindInt, Int: negated, Type: operand.Type, Adoptable: operand.Adoptable}, nil
		}
		if operand.Kind == KindFloat {
			if operand.FloatIsNaN {
				return &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true, Type: operand.Type}, nil
			}
			return &Value{Kind: KindFloat, Float: new(big.Float).Neg(operand.Float), Type: operand.Type}, nil
		}
		return nil, fmt.Errorf("unary minus requires numeric operand, got %s", operand.Kind)
	case token.Not:
		if operand.Kind != KindBool {
			return nil, typeErrorf("'not' requires bool, got %s", operand.Kind)
		}
		return Bool(!operand.Bool), nil
	}
	return nil, fmt.Errorf("unknown unary operator: %v", e.Op)
}

// --- Control flow ---

// evalIf implements "if cond then a else b" (§5.9, §D.5).
// Both branches are speculatively evaluated for type checking.
// Type errors always propagate; runtime errors in non-selected branches are suppressed.
func (ev *Evaluator) evalIf(e *ast.IfExpr, scope *Scope) (*Value, error) {
	cond, err := ev.evalExpr(e.Cond, scope)
	if err != nil {
		return nil, err
	}
	if cond.Kind != KindBool {
		return nil, typeErrorf("if condition must be bool, got %s", cond.Kind)
	}
	thenScope, elseScope := ev.narrowIfBranches(e.Cond, scope)
	if cond.Bool {
		thenVal, err := ev.evalExpr(e.Then, thenScope)
		if err != nil {
			return nil, err
		}
		// §D.5: speculatively evaluate else branch
		elseVal, elseErr := ev.evalExpr(e.Else, elseScope)
		if elseErr != nil {
			if isTypeError(elseErr) {
				return nil, elseErr
			}
		} else {
			if !branchTypesCompatible(thenVal, elseVal) {
				return nil, typeErrorf("if/else branch type mismatch: then is %s, else is %s", thenVal.Kind, elseVal.Kind)
			}
			adoptNamedType(thenVal, elseVal)
		}
		return thenVal, nil
	}
	elseVal, err := ev.evalExpr(e.Else, elseScope)
	if err != nil {
		return nil, err
	}
	// §D.5: speculatively evaluate then branch
	thenVal, thenErr := ev.evalExpr(e.Then, thenScope)
	if thenErr != nil {
		if isTypeError(thenErr) {
			return nil, thenErr
		}
	} else {
		if !branchTypesCompatible(thenVal, elseVal) {
			return nil, typeErrorf("if/else branch type mismatch: then is %s, else is %s", thenVal.Kind, elseVal.Kind)
		}
		adoptNamedType(elseVal, thenVal)
	}
	return elseVal, nil
}

// narrowIfBranches inspects the condition and returns scopes for the
// then/else branches with the narrowing target rebound (§5.9 R8). When
// the condition is not a recognized narrowing predicate, the original
// scope is reused for both branches.
func (ev *Evaluator) narrowIfBranches(cond ast.Expr, scope *Scope) (*Scope, *Scope) {
	name, posVal, negVal, ok := ev.detectIfNarrowing(cond, scope)
	if !ok {
		return scope, scope
	}
	thenScope := scope
	elseScope := scope
	if posVal != nil {
		thenScope = newScope(scope)
		thenScope.set(name, posVal)
	}
	if negVal != nil {
		elseScope = newScope(scope)
		elseScope.set(name, negVal)
	}
	return thenScope, elseScope
}

// detectIfNarrowing returns the identifier name and narrowed values for
// the then/else branches of supported predicates: `x is [not] type T`,
// `x is [not] named V`, `x is [not] null`, `x is [not] undefined`.
func (ev *Evaluator) detectIfNarrowing(cond ast.Expr, scope *Scope) (name string, posVal, negVal *Value, ok bool) {
	switch c := cond.(type) {
	case *ast.IsTypeExpr:
		ident, ok2 := c.Value.(*ast.IdentExpr)
		if !ok2 {
			return "", nil, nil, false
		}
		val, ok2 := scope.get(ident.Name)
		if !ok2 {
			return "", nil, nil, false
		}
		ti := ev.resolveTypeExpr(c.TypeExpr)
		matchVal, otherVal := narrowByType(val, ti)
		if c.Negated {
			matchVal, otherVal = otherVal, matchVal
		}
		return ident.Name, matchVal, otherVal, true
	case *ast.IsNamedExpr:
		ident, ok2 := c.Value.(*ast.IdentExpr)
		if !ok2 {
			return "", nil, nil, false
		}
		val, ok2 := scope.get(ident.Name)
		if !ok2 || val.Kind != KindTaggedUnion {
			return "", nil, nil, false
		}
		matchVal, otherVal := narrowByVariant(val, c.Variant)
		if c.Negated {
			matchVal, otherVal = otherVal, matchVal
		}
		return ident.Name, matchVal, otherVal, true
	case *ast.BinaryExpr:
		if c.Op != token.Is && c.Op != token.IsNot {
			return "", nil, nil, false
		}
		ident, _ := c.Left.(*ast.IdentExpr)
		if ident == nil {
			return "", nil, nil, false
		}
		val, ok2 := scope.get(ident.Name)
		if !ok2 {
			return "", nil, nil, false
		}
		switch c.Right.(type) {
		case *ast.UndefinedExpr:
			matchVal, otherVal := narrowByUndefined(val)
			if c.Op == token.IsNot {
				matchVal, otherVal = otherVal, matchVal
			}
			return ident.Name, matchVal, otherVal, true
		}
		if lit, isLit := c.Right.(*ast.LiteralExpr); isLit && lit.Token.Type == token.Null {
			matchVal, otherVal := narrowByNull(val)
			if c.Op == token.IsNot {
				matchVal, otherVal = otherVal, matchVal
			}
			return ident.Name, matchVal, otherVal, true
		}
	}
	return "", nil, nil, false
}

// narrowByType returns (matchedValue, unmatchedValue). For a union value,
// the matched branch unwraps to the inner if it matches T; otherwise a
// zero of T. The unmatched branch keeps the inner if it doesn't match T;
// otherwise a zero of the single remaining member type (if any).
func narrowByType(val *Value, ti *TypeInfo) (*Value, *Value) {
	if ti == nil {
		return val, val
	}
	if val.Kind == KindUnion && val.Union != nil {
		inner := val.Union.Inner
		matchedInner := inner
		if !valueMatchesType(inner, ti) {
			matchedInner = zeroValueForType(ti)
		}
		var otherVal *Value = inner
		if valueMatchesType(inner, ti) {
			var remaining []*TypeInfo
			for _, mt := range val.Union.MemberTypes {
				if !typeInfosMatch(mt, ti) {
					remaining = append(remaining, mt)
				}
			}
			if len(remaining) == 1 {
				otherVal = zeroValueForType(remaining[0])
			} else {
				otherVal = Undefined()
			}
		}
		return matchedInner, otherVal
	}
	return val, val
}

func narrowByVariant(val *Value, variant string) (*Value, *Value) {
	tu := val.TaggedUnion
	if tu == nil {
		return val, val
	}
	matchedInner := tu.Inner
	if tu.Tag != variant {
		for _, v := range tu.Variants {
			if v.Name == variant {
				matchedInner = zeroValueForType(v.Type)
				break
			}
		}
	}
	var otherVal *Value = tu.Inner
	if tu.Tag == variant {
		var remaining []TaggedVariant
		for _, v := range tu.Variants {
			if v.Name != variant {
				remaining = append(remaining, v)
			}
		}
		if len(remaining) == 1 {
			otherVal = zeroValueForType(remaining[0].Type)
		} else {
			otherVal = Undefined()
		}
	}
	return matchedInner, otherVal
}

func narrowByUndefined(val *Value) (*Value, *Value) {
	matched := Undefined()
	other := val
	if val.Kind == KindUndefined {
		other = Undefined()
	}
	return matched, other
}

func narrowByNull(val *Value) (*Value, *Value) {
	matched := Null()
	other := val
	if val.Kind == KindUnion && val.Union != nil {
		var remaining []*TypeInfo
		for _, mt := range val.Union.MemberTypes {
			if mt == nil || mt.BaseType != "null" {
				remaining = append(remaining, mt)
			}
		}
		if val.Union.Inner != nil && val.Union.Inner.Kind != KindNull {
			other = val.Union.Inner
		} else if len(remaining) == 1 {
			other = zeroValueForType(remaining[0])
		}
	}
	return matched, other
}

// valueMatchesType reports whether a value's type matches the given type.
func valueMatchesType(v *Value, ti *TypeInfo) bool {
	if v == nil || ti == nil {
		return false
	}
	if v.Type != nil && typeInfosMatch(v.Type, ti) {
		return true
	}
	switch ti.BaseType {
	case "bool":
		return v.Kind == KindBool
	case "string":
		return v.Kind == KindString
	case "null":
		return v.Kind == KindNull
	}
	if isIntegerType(ti.BaseType) {
		return v.Kind == KindInt
	}
	if isFloatType(ti.BaseType) {
		return v.Kind == KindFloat
	}
	return false
}

func typeInfosMatch(a, b *TypeInfo) bool {
	if a == nil || b == nil {
		return false
	}
	if a.BaseType != "" && a.BaseType == b.BaseType {
		return true
	}
	if a.Name != "" && a.Name == b.Name {
		return true
	}
	return false
}

// branchTypesCompatible checks if two branch values have compatible types.
// null is compatible with any type (§5.9, §5.10).
// undefined is compatible with any type (§5.9 R8 — symmetric narrowing
// permits a branch to surface the undefined inhabitant).
// §5: adoptable int literal is compatible with float (cross-category adoption).
func branchTypesCompatible(a, b *Value) bool {
	if a.Kind == b.Kind {
		return true
	}
	if a.Kind == KindNull || b.Kind == KindNull {
		return true
	}
	if a.Kind == KindUndefined || b.Kind == KindUndefined {
		return true
	}
	// §5: int literal can adopt float type
	if a.Kind == KindInt && b.Kind == KindFloat && a.Adoptable {
		return true
	}
	if a.Kind == KindFloat && b.Kind == KindInt && b.Adoptable {
		return true
	}
	return false
}

// leftTypeMatchesRightKind checks whether a left-hand TypeInfo (typically
// propagated through `to`) is compatible with a right-hand value's Kind.
// Used by `or else` to validate type compatibility when the left side is
// undefined but still carries type information.
func leftTypeMatchesRightKind(lt *TypeInfo, right *Value) bool {
	if right.Adoptable {
		if (isIntegerType(lt.BaseType) || isFloatType(lt.BaseType)) &&
			(right.Kind == KindInt || right.Kind == KindFloat) {
			return true
		}
	}
	switch {
	case isIntegerType(lt.BaseType):
		return right.Kind == KindInt
	case isFloatType(lt.BaseType):
		return right.Kind == KindFloat
	case lt.BaseType == "string":
		return right.Kind == KindString
	case lt.BaseType == "bool":
		return right.Kind == KindBool
	case lt.BaseType == "null":
		return right.Kind == KindNull
	}
	return true
}

// operandTypesCompatible checks if two values have compatible types for operators
// like "or else" that require same type. null and undefined are exempt.
func operandTypesCompatible(left, right *Value) bool {
	if right.Kind == KindUndefined || isUnresolvedIdent(right) {
		return true
	}
	if left.Kind == KindNull || right.Kind == KindNull {
		return true
	}
	if left.Kind == right.Kind {
		return true
	}
	// §5: int literal can adopt float type
	if left.Kind == KindInt && right.Kind == KindFloat && left.Adoptable {
		return true
	}
	if left.Kind == KindFloat && right.Kind == KindInt && right.Adoptable {
		return true
	}
	return false
}

// adoptNamedType propagates a named type from other to result when result is
// null without a type. This ensures null + named type preserves the named type.
func adoptNamedType(result, other *Value) {
	if result.Kind == KindNull && result.Type == nil && other.Type != nil && other.Type.Name != "" {
		result.Type = other.Type
	}
}

// sameUnionMemberSets checks if two union member type sets are structurally
// equivalent (order irrelevant). §v0.8: anonymous unions use structural identity.
func sameUnionMemberSets(a, b []*TypeInfo) bool {
	if len(a) != len(b) {
		return false
	}
	aKeys := make(map[string]bool, len(a))
	for _, t := range a {
		aKeys[t.TypeKey()] = true
	}
	for _, t := range b {
		if !aKeys[t.TypeKey()] {
			return false
		}
	}
	return true
}

// unionHasMemberType checks if a type is among the union's declared member types.
func unionHasMemberType(members []*TypeInfo, ti *TypeInfo) bool {
	if ti == nil {
		return false
	}
	key := ti.TypeKey()
	for _, m := range members {
		if m.TypeKey() == key {
			return true
		}
		if m.Name != "" && m.Name == ti.Name {
			return true
		}
	}
	return false
}

// narrowScrutinee returns the narrowed value for a case type branch (§5.10).
// For the matched branch of a union, returns the inner value.
// For non-matched branches, returns a zero value of the when type for
// speculative evaluation. Returns nil if narrowing is not applicable.
func (ev *Evaluator) narrowScrutinee(scrutinee *Value, ti *TypeInfo, matched bool) *Value {
	if ti == nil {
		return nil
	}
	if matched {
		// Selected branch: use the actual inner value.
		switch scrutinee.Kind {
		case KindUnion:
			return scrutinee.Union.Inner
		case KindTaggedUnion:
			return scrutinee.TaggedUnion.Inner
		default:
			return scrutinee
		}
	}
	// Non-selected branch: create a zero value for type checking.
	return zeroValueForType(ti)
}

// zeroValueForType creates a zero value for the given type, used for
// speculative evaluation of non-selected case type branches.
func zeroValueForType(ti *TypeInfo) *Value {
	if ti == nil {
		return nil
	}
	switch {
	case ti.BaseType == "null":
		return Null()
	case ti.BaseType == "bool":
		return Bool(false)
	case ti.BaseType == "string":
		return String("")
	case isIntegerType(ti.BaseType):
		v := Int(0)
		v.Type = ti
		return v
	case isFloatType(ti.BaseType):
		v := Float64(0)
		v.Type = ti
		return v
	}
	// Tuple: recursively construct (default(T1), default(T2), ...) per §3.6.
	if ti.BaseType == "tuple" || len(ti.TupleElemTypes) > 0 {
		elems := make([]*Value, len(ti.TupleElemTypes))
		for i, et := range ti.TupleElemTypes {
			ev := zeroValueForType(et)
			if ev == nil {
				ev = Undefined()
			}
			elems[i] = ev
		}
		return &Value{Kind: KindTuple, Tuple: &TupleValue{Elements: elems}, Type: ti}
	}
	// List: empty list typed by the element type.
	if ti.BaseType == "list" || ti.ListElemType != nil {
		return &Value{Kind: KindList, List: &ListValue{Elements: nil, ElementType: ti.ListElemType}, Type: ti}
	}
	// Named/struct types: return undefined to suppress speculative errors.
	return Undefined()
}

// narrowScrutineeNamed returns the narrowed value for a case named branch (§5.10).
// For the matched branch, returns the inner value of the tagged union.
// For non-matched branches, returns a zero value of the variant's inner type
// for speculative evaluation. Returns nil if narrowing is not applicable.
func (ev *Evaluator) narrowScrutineeNamed(scrutinee *Value, variantName string, matched bool) *Value {
	if scrutinee.Kind != KindTaggedUnion {
		return nil
	}
	if matched {
		return scrutinee.TaggedUnion.Inner
	}
	// Non-selected branch: find the variant's inner type and create a zero value.
	for _, v := range scrutinee.TaggedUnion.Variants {
		if v.Name == variantName {
			return zeroValueForType(v.Type)
		}
	}
	return Undefined()
}

// narrowElseType narrows the else branch of case type to the remaining types
// not matched by any when clause (§5.10). When the else branch is selected
// (elseSelected=true), uses the actual inner value; otherwise creates a zero
// value for speculative evaluation.
func (ev *Evaluator) narrowElseType(scrutinee *Value, whens []*ast.WhenClause, elseSelected bool) *Value {
	if scrutinee.Kind == KindUnion {
		if elseSelected {
			return scrutinee.Union.Inner
		}
		coveredTypes := make(map[string]bool)
		for _, w := range whens {
			if w.TypeExpr != nil {
				ti := ev.resolveTypeExpr(w.TypeExpr)
				if ti != nil {
					coveredTypes[ti.BaseType] = true
				}
			}
		}
		var remaining []*TypeInfo
		for _, mt := range scrutinee.Union.MemberTypes {
			if !coveredTypes[mt.BaseType] {
				remaining = append(remaining, mt)
			}
		}
		if len(remaining) == 1 {
			return zeroValueForType(remaining[0])
		}
	}
	if scrutinee.Kind == KindTaggedUnion {
		if elseSelected {
			return scrutinee.TaggedUnion.Inner
		}
		coveredTypes := make(map[string]bool)
		for _, w := range whens {
			if w.TypeExpr != nil {
				ti := ev.resolveTypeExpr(w.TypeExpr)
				if ti != nil {
					coveredTypes[ti.BaseType] = true
				}
			}
		}
		var remaining []*TypeInfo
		seen := make(map[string]bool)
		for _, v := range scrutinee.TaggedUnion.Variants {
			if v.Type != nil && !coveredTypes[v.Type.BaseType] && !seen[v.Type.BaseType] {
				remaining = append(remaining, v.Type)
				seen[v.Type.BaseType] = true
			}
		}
		if len(remaining) == 1 {
			return zeroValueForType(remaining[0])
		}
	}
	// For non-union values or when multiple remaining types: use the
	// scrutinee directly if selected, else Undefined for speculative.
	if elseSelected {
		return scrutinee
	}
	return Undefined()
}

// narrowElseNamed narrows the else branch of case named to the remaining
// variants not matched by any when clause (§5.10).
func (ev *Evaluator) narrowElseNamed(scrutinee *Value, whens []*ast.WhenClause, elseSelected bool) *Value {
	if scrutinee.Kind != KindTaggedUnion {
		return nil
	}
	if elseSelected {
		return scrutinee.TaggedUnion.Inner
	}
	coveredVariants := make(map[string]bool)
	for _, w := range whens {
		if w.VariantName != "" {
			coveredVariants[w.VariantName] = true
		}
	}
	var remaining []TaggedVariant
	for _, v := range scrutinee.TaggedUnion.Variants {
		if !coveredVariants[v.Name] {
			remaining = append(remaining, v)
		}
	}
	if len(remaining) == 1 {
		return zeroValueForType(remaining[0].Type)
	}
	// Multiple remaining: use undefined for speculative evaluation.
	return Undefined()
}

// taggedUnionHasInnerType checks whether any variant of a tagged union has
// the given inner type. Used for case type member validation (§5.10).
func taggedUnionHasInnerType(variants []TaggedVariant, ti *TypeInfo) bool {
	if ti == nil {
		return false
	}
	for _, v := range variants {
		if v.Type == nil {
			continue
		}
		if v.Type.BaseType == ti.BaseType {
			return true
		}
		if v.Type.Name != "" && v.Type.Name == ti.Name {
			return true
		}
	}
	return false
}

// evalCase implements pattern matching (§5.10).
// Three forms: value matching (Mode=""), type dispatch (Mode="type"), variant dispatch (Mode="named").
// All branches are speculatively evaluated for type consistency.
func (ev *Evaluator) evalCase(e *ast.CaseExpr, scope *Scope) (*Value, error) {
	scrutinee, err := ev.evalExpr(e.Scrutinee, scope)
	if err != nil {
		return nil, err
	}
	if scrutinee.Kind == KindUndefined || isUnresolvedIdent(scrutinee) {
		return nil, fmt.Errorf("case scrutinee is undefined")
	}

	// §5.10: validate scrutinee type for dispatch modes
	switch e.Mode {
	case "type":
		// case type works on any value (§5.10), consistent with is type.
	case "named":
		if scrutinee.Kind != KindTaggedUnion {
			return nil, typeErrorf("case named: scrutinee must be a tagged union, got %s", scrutinee.Kind)
		}
	default:
		// §5.10: value matching — union scrutinees are ill-defined (use case type instead)
		if scrutinee.Kind == KindUnion || scrutinee.Kind == KindTaggedUnion {
			return nil, typeErrorf("case value: cannot match on %s (use 'case type' or 'case named' instead)", scrutinee.Kind)
		}
	}

	// §5.10 branch narrowing: detect scrutinee identifier for case type / case named.
	var narrowName string
	if e.Mode == "type" || e.Mode == "named" {
		if ident, ok := e.Scrutinee.(*ast.IdentExpr); ok {
			narrowName = ident.Name
		}
	}

	var result *Value
	var branchValues []*Value

	for _, w := range e.Whens {
		matched := false

		switch e.Mode {
		case "type":
			// Type dispatch: works on any value (§5.10).
			// For unions, checks inner value; for others, checks concrete type.
			// §v0.8: compound types ([T], (T, T)) are supported in when clauses.
			ti := ev.resolveTypeExpr(w.TypeExpr)
			// §5.10: when scrutinee is a union (tagged or untagged), validate
			// when types against the union's member types.
			if scrutinee.Kind == KindUnion {
				if !unionHasMemberType(scrutinee.Union.MemberTypes, ti) {
					return nil, typeErrorf("case type: %s is not a member of the union", ti.TypeKey())
				}
			}
			if scrutinee.Kind == KindTaggedUnion && ti.ListElemType == nil && len(ti.TupleElemTypes) == 0 {
				if !taggedUnionHasInnerType(scrutinee.TaggedUnion.Variants, ti) {
					return nil, typeErrorf("case type: %s is not a member type of the tagged union", ti.BaseType)
				}
			}
			if ti.ListElemType != nil || len(ti.TupleElemTypes) > 0 {
				matched = ev.valueMatchesCompoundType(scrutinee, ti)
			} else {
				matched = ev.valueMatchesType(scrutinee, ti)
			}
		case "named":
			// §5.10: undefined cannot be used as a when value
			if w.VariantName == "undefined" {
				return nil, typeErrorf("'when undefined' is not allowed in case expressions")
			}
			// Variant dispatch: match tagged union's tag
			// §5.10: validate variant name against tagged union's variant list
			variantValid := false
			for _, v := range scrutinee.TaggedUnion.Variants {
				if v.Name == w.VariantName {
					variantValid = true
					break
				}
			}
			if !variantValid {
				return nil, typeErrorf("case named: '%s' is not a variant of this tagged union", w.VariantName)
			}
			if scrutinee.TaggedUnion.Tag == w.VariantName {
				matched = true
			}
		default:
			// Value matching
			if _, ok := w.Value.(*ast.UndefinedExpr); ok {
				return nil, typeErrorf("'when undefined' is not allowed in case expressions")
			}
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

		// §5.10 branch narrowing for case type / case named:
		// inside each when branch, the scrutinee is narrowed.
		branchScope := scope
		if narrowName != "" {
			switch e.Mode {
			case "type":
				ti := ev.resolveTypeExpr(w.TypeExpr)
				if narrowed := ev.narrowScrutinee(scrutinee, ti, matched); narrowed != nil {
					branchScope = newScope(scope)
					branchScope.set(narrowName, narrowed)
				}
			case "named":
				if narrowed := ev.narrowScrutineeNamed(scrutinee, w.VariantName, matched); narrowed != nil {
					branchScope = newScope(scope)
					branchScope.set(narrowName, narrowed)
				}
			}
		}

		thenVal, thenErr := ev.evalExpr(w.Then, branchScope)
		if thenErr != nil {
			// §D.5: type errors always propagate, runtime errors suppressed in non-selected
			if isTypeError(thenErr) {
				return nil, thenErr
			}
		} else {
			branchValues = append(branchValues, thenVal)
		}
		if matched && result == nil {
			if thenErr != nil {
				return nil, thenErr
			}
			result = thenVal
		}
	}

	// §5.10: else branch narrowing — narrow to remaining types/variants.
	elseScope := scope
	if narrowName != "" {
		elseSelected := result == nil
		switch e.Mode {
		case "type":
			if narrowed := ev.narrowElseType(scrutinee, e.Whens, elseSelected); narrowed != nil {
				elseScope = newScope(scope)
				elseScope.set(narrowName, narrowed)
			}
		case "named":
			if narrowed := ev.narrowElseNamed(scrutinee, e.Whens, elseSelected); narrowed != nil {
				elseScope = newScope(scope)
				elseScope.set(narrowName, narrowed)
			}
		}
	}
	elseVal, elseErr := ev.evalExpr(e.Else, elseScope)
	if elseErr != nil {
		if isTypeError(elseErr) {
			return nil, elseErr
		}
	} else {
		branchValues = append(branchValues, elseVal)
	}

	// §5.9/§5.10: all branches must produce the same result type.
	// Uses branchTypesCompatible to handle cross-category int→float adoption.
	if len(branchValues) > 1 {
		ref := branchValues[0]
		for _, bv := range branchValues[1:] {
			if !branchTypesCompatible(ref, bv) {
				return nil, typeErrorf("case branch type mismatch: got %s and %s", ref.Kind, bv.Kind)
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

// evalIsType implements "expr is type T" and "expr is not type T" (§5.2).
// §v0.8: supports compound type expressions ([T], (T, T)).
func (ev *Evaluator) evalIsType(e *ast.IsTypeExpr, scope *Scope) (*Value, error) {
	val, err := ev.evalExpr(e.Value, scope)
	if err != nil {
		return nil, err
	}
	if val.Kind == KindUndefined {
		return nil, fmt.Errorf("'is type' on undefined")
	}
	ti := ev.resolveTypeExpr(e.TypeExpr)
	var matched bool
	if ti.ListElemType != nil || len(ti.TupleElemTypes) > 0 {
		matched = ev.valueMatchesCompoundType(val, ti)
	} else {
		matched = ev.valueMatchesType(val, ti)
	}
	if e.Negated {
		return Bool(!matched), nil
	}
	return Bool(matched), nil
}

// valueMatchesType checks if a value matches a type expression (for "is type" / "case type").
// For union/tagged union values, the inner value is checked against the type (§5.2).
// §v0.8: supports compound type matching: [T] for lists, (T, T) for tuples.
func (ev *Evaluator) valueMatchesType(val *Value, ti *TypeInfo) bool {
	if val == nil || ti == nil {
		return false
	}
	// Unwrap union/tagged union: check the inner value's type
	if val.Kind == KindUnion {
		return ev.valueMatchesType(val.Union.Inner, ti)
	}
	if val.Kind == KindTaggedUnion {
		return ev.valueMatchesType(val.TaggedUnion.Inner, ti)
	}
	switch val.Kind {
	case KindNull:
		return ti.BaseType == "null" || ti.Name == "null"
	case KindBool:
		return ti.BaseType == "bool" || ti.Name == "bool"
	case KindInt:
		valType := val.Type
		if valType == nil {
			valType = &TypeInfo{BaseType: "i64", BitSize: 64, Signed: true}
		}
		return valType.BaseType == ti.BaseType
	case KindFloat:
		valType := val.Type
		if valType == nil {
			valType = &TypeInfo{BaseType: "f64", BitSize: 64}
		}
		return valType.BaseType == ti.BaseType
	case KindString:
		return ti.BaseType == "string" || ti.Name == "string"
	case KindStruct:
		if val.Type != nil && val.Type.Name != "" {
			return val.Type.Name == ti.Name
		}
		return ti.Name == "struct"
	case KindList:
		if val.Type != nil && val.Type.Name != "" && val.Type.Name == ti.Name {
			return true
		}
		return ti.BaseType == "list" || ti.Name == "list"
	case KindTuple:
		if val.Type != nil && val.Type.Name != "" && val.Type.Name == ti.Name {
			return true
		}
		return ti.BaseType == "tuple" || ti.Name == "tuple"
	case KindEnum:
		if val.Type != nil && val.Type.Name != "" {
			return val.Type.Name == ti.Name
		}
		return false
	case KindFunction:
		return ti.Name == "function"
	}
	return false
}

// valueMatchesCompoundType checks if a value matches a compound TypeInfo
// (e.g. [i32], (i32, string)) for case type dispatch (§v0.8).
func (ev *Evaluator) valueMatchesCompoundType(val *Value, ti *TypeInfo) bool {
	if val == nil || ti == nil {
		return false
	}
	// Unwrap union/tagged union
	if val.Kind == KindUnion {
		return ev.valueMatchesCompoundType(val.Union.Inner, ti)
	}
	if val.Kind == KindTaggedUnion {
		return ev.valueMatchesCompoundType(val.TaggedUnion.Inner, ti)
	}
	// List type: [ElemType]
	if ti.ListElemType != nil {
		if val.Kind != KindList {
			return false
		}
		valElemTi := val.List.ElementType
		if valElemTi == nil {
			if len(val.List.Elements) > 0 {
				valElemTi = ev.inferType(val.List.Elements[0])
			} else {
				return true // empty list matches any list type
			}
		}
		return valElemTi.BaseType == ti.ListElemType.BaseType
	}
	// Tuple type: (Type, Type, ...)
	if len(ti.TupleElemTypes) > 0 {
		if val.Kind != KindTuple {
			return false
		}
		if len(val.Tuple.Elements) != len(ti.TupleElemTypes) {
			return false
		}
		for i, elem := range val.Tuple.Elements {
			valElemTi := ev.inferType(elem)
			if valElemTi.BaseType != ti.TupleElemTypes[i].BaseType {
				return false
			}
		}
		return true
	}
	// Simple type
	return ev.valueMatchesType(val, ti)
}

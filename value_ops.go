// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"math"
	"math/big"
	"strings"
)

// Add returns a + b. Both operands must be the same numeric kind
// (KindInt or KindFloat). Operands may be *Value or Go primitives (auto-wrapped).
func Add(a, b any) (*Value, error) {
	av, bv, err := coerceTwo("Add", a, b)
	if err != nil {
		return nil, err
	}
	return simpleNumericOp("Add", av, bv,
		func(x, y *big.Int) *big.Int { return new(big.Int).Add(x, y) },
		func(x, y *big.Float) *big.Float { return new(big.Float).SetPrec(53).Add(x, y) },
	)
}

// Sub returns a - b. Both operands must be the same numeric kind.
func Sub(a, b any) (*Value, error) {
	av, bv, err := coerceTwo("Sub", a, b)
	if err != nil {
		return nil, err
	}
	return simpleNumericOp("Sub", av, bv,
		func(x, y *big.Int) *big.Int { return new(big.Int).Sub(x, y) },
		func(x, y *big.Float) *big.Float { return new(big.Float).SetPrec(53).Sub(x, y) },
	)
}

// Mul returns a * b. Both operands must be the same numeric kind.
func Mul(a, b any) (*Value, error) {
	av, bv, err := coerceTwo("Mul", a, b)
	if err != nil {
		return nil, err
	}
	return simpleNumericOp("Mul", av, bv,
		func(x, y *big.Int) *big.Int { return new(big.Int).Mul(x, y) },
		func(x, y *big.Float) *big.Float { return new(big.Float).SetPrec(53).Mul(x, y) },
	)
}

// Div returns a / b. Both operands must be the same numeric kind.
// For integers, returns an error if b is zero.
// For floats, division by zero follows IEEE 754 (returns ±inf or NaN).
func Div(a, b any) (*Value, error) {
	av, bv, err := coerceTwo("Div", a, b)
	if err != nil {
		return nil, err
	}
	av, bv = unwrapTaggedUnion(av), unwrapTaggedUnion(bv)
	if av.Kind == KindInt && bv.Kind == KindInt {
		if bv.Int.Sign() == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		r := new(big.Int).Quo(av.Int, bv.Int)
		if err := checkIntOverflow(r, av.Type); err != nil {
			return nil, fmt.Errorf("Div: %w", err)
		}
		return &Value{Kind: KindInt, Int: r, Type: av.Type}, nil
	}
	if av.Kind == KindFloat && bv.Kind == KindFloat {
		if av.FloatIsNaN || bv.FloatIsNaN {
			return nanFloat(av.Type), nil
		}
		r := new(big.Float).SetPrec(53)
		if bv.Float.Sign() == 0 {
			if av.Float.Sign() == 0 {
				return nanFloat(av.Type), nil
			} else if av.Float.Sign() > 0 {
				r.SetInf(false)
			} else {
				r.SetInf(true)
			}
		} else {
			r.Quo(av.Float, bv.Float)
		}
		return &Value{Kind: KindFloat, Float: r, Type: av.Type}, nil
	}
	return nil, fmt.Errorf("Div requires same numeric type, got %s and %s", av.Kind, bv.Kind)
}

// Mod returns a %% b. Both operands must be the same numeric kind.
// For integers, returns an error if b is zero.
func Mod(a, b any) (*Value, error) {
	av, bv, err := coerceTwo("Mod", a, b)
	if err != nil {
		return nil, err
	}
	av, bv = unwrapTaggedUnion(av), unwrapTaggedUnion(bv)
	if av.Kind == KindInt && bv.Kind == KindInt {
		if bv.Int.Sign() == 0 {
			return nil, fmt.Errorf("modulo by zero")
		}
		r := new(big.Int).Rem(av.Int, bv.Int)
		if err := checkIntOverflow(r, av.Type); err != nil {
			return nil, fmt.Errorf("Mod: %w", err)
		}
		return &Value{Kind: KindInt, Int: r, Type: av.Type}, nil
	}
	if av.Kind == KindFloat && bv.Kind == KindFloat {
		if av.FloatIsNaN || bv.FloatIsNaN {
			return nanFloat(av.Type), nil
		}
		af, _ := av.Float.Float64()
		bf, _ := bv.Float.Float64()
		result := math.Mod(af, bf)
		if math.IsNaN(result) {
			return nanFloat(av.Type), nil
		}
		return &Value{Kind: KindFloat, Float: new(big.Float).SetPrec(53).SetFloat64(result), Type: av.Type}, nil
	}
	return nil, fmt.Errorf("Mod requires same numeric type, got %s and %s", av.Kind, bv.Kind)
}

// Pow returns a ^ b. Both operands must be the same numeric kind.
// For integers, the exponent must be non-negative and at most 10000.
func Pow(a, b any) (*Value, error) {
	av, bv, err := coerceTwo("Pow", a, b)
	if err != nil {
		return nil, err
	}
	av, bv = unwrapTaggedUnion(av), unwrapTaggedUnion(bv)
	if av.Kind == KindInt && bv.Kind == KindInt {
		if bv.Int.Sign() < 0 {
			return nil, fmt.Errorf("negative exponent in integer exponentiation")
		}
		if !bv.Int.IsInt64() || bv.Int.Int64() > 10000 {
			return nil, fmt.Errorf("exponent too large")
		}
		r := new(big.Int).Exp(av.Int, bv.Int, nil)
		if err := checkIntOverflow(r, av.Type); err != nil {
			return nil, fmt.Errorf("Pow: %w", err)
		}
		return &Value{Kind: KindInt, Int: r, Type: av.Type}, nil
	}
	if av.Kind == KindFloat && bv.Kind == KindFloat {
		if av.FloatIsNaN || bv.FloatIsNaN {
			return nanFloat(av.Type), nil
		}
		af, _ := av.Float.Float64()
		bf, _ := bv.Float.Float64()
		result := math.Pow(af, bf)
		if math.IsNaN(result) {
			return nanFloat(av.Type), nil
		}
		return &Value{Kind: KindFloat, Float: new(big.Float).SetPrec(53).SetFloat64(result), Type: av.Type}, nil
	}
	return nil, fmt.Errorf("Pow requires same numeric type, got %s and %s", av.Kind, bv.Kind)
}

// Negate returns -v. The operand must be KindInt or KindFloat.
func Negate(v any) (*Value, error) {
	val, err := toValue(v)
	if err != nil {
		return nil, fmt.Errorf("Negate: %w", err)
	}
	val = unwrapTaggedUnion(val)
	switch val.Kind {
	case KindInt:
		r := new(big.Int).Neg(val.Int)
		if err := checkIntOverflow(r, val.Type); err != nil {
			return nil, fmt.Errorf("Negate: %w", err)
		}
		return &Value{Kind: KindInt, Int: r, Type: val.Type}, nil
	case KindFloat:
		if val.FloatIsNaN {
			return nanFloat(val.Type), nil
		}
		return &Value{Kind: KindFloat, Float: new(big.Float).Neg(val.Float), Type: val.Type}, nil
	default:
		return nil, fmt.Errorf("Negate requires numeric operand, got %s", val.Kind)
	}
}

// Not returns the boolean negation of v. The operand must be KindBool.
func Not(v any) (*Value, error) {
	val, err := toValue(v)
	if err != nil {
		return nil, fmt.Errorf("Not: %w", err)
	}
	if val.Kind != KindBool {
		return nil, fmt.Errorf("Not requires bool, got %s", val.Kind)
	}
	return Bool(!val.Bool), nil
}

// Equal reports whether a and b are deeply equal.
// Values of different kinds are not equal. NaN is not equal to NaN (IEEE 754).
// Operands may be *Value or Go primitives (auto-wrapped).
func Equal(a, b any) bool {
	av, err := toValue(a)
	if err != nil {
		return false
	}
	bv, err := toValue(b)
	if err != nil {
		return false
	}
	return valuesEqual(av, bv)
}

// EqualTo reports whether v is deeply equal to other.
// other may be a *Value or a Go primitive (auto-wrapped).
func (v *Value) EqualTo(other any) bool {
	return Equal(v, other)
}

// Compare performs ordered comparison of a and b.
// Returns -1 if a < b, 0 if a == b, +1 if a > b.
// Both operands must be the same comparable kind (KindInt, KindFloat, or KindString).
// Returns an error for NaN or incompatible types.
func Compare(a, b any) (int, error) {
	av, bv, err := coerceTwo("Compare", a, b)
	if err != nil {
		return 0, err
	}
	av, bv = unwrapTaggedUnion(av), unwrapTaggedUnion(bv)
	if av.Kind == KindInt && bv.Kind == KindInt {
		return av.Int.Cmp(bv.Int), nil
	}
	if av.Kind == KindFloat && bv.Kind == KindFloat {
		if av.FloatIsNaN || bv.FloatIsNaN {
			return 0, fmt.Errorf("NaN is not comparable")
		}
		return av.Float.Cmp(bv.Float), nil
	}
	if av.Kind == KindString && bv.Kind == KindString {
		return strings.Compare(av.Str, bv.Str), nil
	}
	return 0, fmt.Errorf("Compare requires same numeric or string type, got %s and %s", av.Kind, bv.Kind)
}

// Concat concatenates two strings or two lists (the ++ operator in UZON).
func Concat(a, b any) (*Value, error) {
	av, bv, err := coerceTwo("Concat", a, b)
	if err != nil {
		return nil, err
	}
	av, bv = unwrapTaggedUnion(av), unwrapTaggedUnion(bv)
	if av.Kind == KindString && bv.Kind == KindString {
		return String(av.Str + bv.Str), nil
	}
	if av.Kind == KindList && bv.Kind == KindList {
		elems := make([]*Value, 0, len(av.List.Elements)+len(bv.List.Elements))
		elems = append(elems, av.List.Elements...)
		elems = append(elems, bv.List.Elements...)
		return NewList(elems, av.List.ElementType), nil
	}
	return nil, fmt.Errorf("Concat requires string or list operands, got %s and %s", av.Kind, bv.Kind)
}

// Repeat repeats a string or list n times (the ** operator in UZON).
func Repeat(v any, n int) (*Value, error) {
	val, err := toValue(v)
	if err != nil {
		return nil, fmt.Errorf("Repeat: %w", err)
	}
	val = unwrapTaggedUnion(val)
	if n < 0 {
		return nil, fmt.Errorf("Repeat requires non-negative count, got %d", n)
	}
	if val.Kind == KindString {
		return String(strings.Repeat(val.Str, n)), nil
	}
	if val.Kind == KindList {
		var elems []*Value
		for i := 0; i < n; i++ {
			elems = append(elems, val.List.Elements...)
		}
		// Preserve element type info so `[x] ** 0` retains inferred type (§3.4).
		elemType := val.List.ElementType
		if elemType == nil && len(val.List.Elements) > 0 {
			elemType = val.List.Elements[0].Type
		}
		return NewList(elems, elemType), nil
	}
	return nil, fmt.Errorf("Repeat requires string or list operand, got %s", val.Kind)
}

// Contains reports whether elem is in list (the "in" operator in UZON).
func Contains(list, elem any) (bool, error) {
	lv, err := toValue(list)
	if err != nil {
		return false, fmt.Errorf("Contains: %w", err)
	}
	ev, err := toValue(elem)
	if err != nil {
		return false, fmt.Errorf("Contains: %w", err)
	}
	if lv.Kind != KindList {
		return false, fmt.Errorf("Contains requires a list, got %s", lv.Kind)
	}
	for _, e := range lv.List.Elements {
		if valuesEqual(ev, e) {
			return true, nil
		}
	}
	return false, nil
}

// --- Type conversions ---

// ToString converts v to a string Value.
// Supported source kinds: string, bool, int, float, null, enum, tagged union, union.
func ToString(v any) (*Value, error) {
	val, err := toValue(v)
	if err != nil {
		return nil, fmt.Errorf("ToString: %w", err)
	}
	return toStr(val)
}

func toStr(v *Value) (*Value, error) {
	switch v.Kind {
	case KindString:
		return v, nil
	case KindBool:
		if v.Bool {
			return String("true"), nil
		}
		return String("false"), nil
	case KindInt:
		return String(v.Int.String()), nil
	case KindFloat:
		if v.FloatIsNaN {
			return String("nan"), nil
		}
		f, _ := v.Float.Float64()
		return String(formatFloat(f)), nil
	case KindNull:
		return String("null"), nil
	case KindEnum:
		return String(v.Enum.Variant), nil
	case KindTaggedUnion:
		return toStr(v.TaggedUnion.Inner)
	case KindUnion:
		return toStr(v.Union.Inner)
	default:
		return nil, fmt.Errorf("cannot convert %s to string", v.Kind)
	}
}

// ToInt converts v to an integer Value.
// Supported source kinds: int, float (truncated), string (parsed).
// String parsing supports decimal, 0x hex, 0o octal, and 0b binary prefixes.
func ToInt(v any) (*Value, error) {
	val, err := toValue(v)
	if err != nil {
		return nil, fmt.Errorf("ToInt: %w", err)
	}
	switch val.Kind {
	case KindInt:
		return val, nil
	case KindFloat:
		if val.FloatIsNaN {
			return nil, fmt.Errorf("cannot convert NaN to integer")
		}
		if val.Float.IsInf() {
			return nil, fmt.Errorf("cannot convert infinity to integer")
		}
		n := new(big.Int)
		val.Float.Int(n)
		return &Value{Kind: KindInt, Int: n}, nil
	case KindString:
		n := new(big.Int)
		s := strings.ReplaceAll(val.Str, "_", "")
		var ok bool
		switch {
		case strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X"):
			_, ok = n.SetString(s[2:], 16)
		case strings.HasPrefix(s, "0o") || strings.HasPrefix(s, "0O"):
			_, ok = n.SetString(s[2:], 8)
		case strings.HasPrefix(s, "0b") || strings.HasPrefix(s, "0B"):
			_, ok = n.SetString(s[2:], 2)
		default:
			_, ok = n.SetString(s, 10)
		}
		if !ok {
			return nil, fmt.Errorf("cannot parse %q as integer", val.Str)
		}
		return &Value{Kind: KindInt, Int: n}, nil
	default:
		return nil, fmt.Errorf("cannot convert %s to integer", val.Kind)
	}
}

// ToFloat converts v to a float Value.
// Supported source kinds: float, int, string (parsed).
// String parsing supports decimal notation, "inf", "-inf", "nan".
func ToFloat(v any) (*Value, error) {
	val, err := toValue(v)
	if err != nil {
		return nil, fmt.Errorf("ToFloat: %w", err)
	}
	switch val.Kind {
	case KindFloat:
		return val, nil
	case KindInt:
		f := new(big.Float).SetPrec(53).SetInt(val.Int)
		return &Value{Kind: KindFloat, Float: f}, nil
	case KindString:
		s := strings.ReplaceAll(val.Str, "_", "")
		switch s {
		case "inf":
			return &Value{Kind: KindFloat, Float: new(big.Float).SetInf(false)}, nil
		case "-inf":
			return &Value{Kind: KindFloat, Float: new(big.Float).SetInf(true)}, nil
		case "nan", "-nan":
			return &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true}, nil
		default:
			f, _, err := big.ParseFloat(s, 10, 53, big.ToNearestEven)
			if err != nil {
				return nil, fmt.Errorf("cannot parse %q as float", val.Str)
			}
			return &Value{Kind: KindFloat, Float: f}, nil
		}
	default:
		return nil, fmt.Errorf("cannot convert %s to float", val.Kind)
	}
}

// simpleNumericOp is a helper for binary numeric operations that don't have
// special cases (Add, Sub, Mul).
func simpleNumericOp(name string, a, b *Value,
	intFn func(*big.Int, *big.Int) *big.Int,
	floatFn func(*big.Float, *big.Float) *big.Float,
) (*Value, error) {
	a, b = unwrapTaggedUnion(a), unwrapTaggedUnion(b)
	if a.Kind == KindInt && b.Kind == KindInt {
		r := intFn(a.Int, b.Int)
		if err := checkIntOverflow(r, a.Type); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return &Value{Kind: KindInt, Int: r, Type: a.Type}, nil
	}
	if a.Kind == KindFloat && b.Kind == KindFloat {
		if a.FloatIsNaN || b.FloatIsNaN {
			return nanFloat(a.Type), nil
		}
		r, isNaN := safeFloatOp(floatFn, a.Float, b.Float)
		if isNaN {
			return nanFloat(a.Type), nil
		}
		return &Value{Kind: KindFloat, Float: r, Type: a.Type}, nil
	}
	return nil, fmt.Errorf("%s requires same numeric type, got %s and %s", name, a.Kind, b.Kind)
}

// safeFloatOp calls fn, recovering from panics caused by big.Float on
// certain infinity operations (e.g. Inf-Inf, Inf*0). Returns isNaN=true
// when the IEEE 754 result would be NaN.
func safeFloatOp(fn func(*big.Float, *big.Float) *big.Float, a, b *big.Float) (result *big.Float, isNaN bool) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			isNaN = true
		}
	}()
	return fn(a, b), false
}

func nanFloat(ti *TypeInfo) *Value {
	return &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true, Type: ti}
}

// checkIntOverflow returns an error if n exceeds the range of the typed integer.
// Returns nil if ti is nil or has no bit size constraint.
func checkIntOverflow(n *big.Int, ti *TypeInfo) error {
	if ti == nil || ti.BitSize == 0 || !isIntegerType(ti.BaseType) {
		return nil
	}
	return checkIntRange(n, ti.BitSize, ti.Signed)
}

// coerceTwo converts two any arguments to *Value, returning a named error on failure.
func coerceTwo(op string, a, b any) (*Value, *Value, error) {
	av, err := toValue(a)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", op, err)
	}
	bv, err := toValue(b)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", op, err)
	}
	return av, bv, nil
}

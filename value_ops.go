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
// (KindInt or KindFloat).
func Add(a, b *Value) (*Value, error) {
	return simpleNumericOp("Add", a, b,
		func(x, y *big.Int) *big.Int { return new(big.Int).Add(x, y) },
		func(x, y *big.Float) *big.Float { return new(big.Float).SetPrec(53).Add(x, y) },
	)
}

// Sub returns a - b. Both operands must be the same numeric kind.
func Sub(a, b *Value) (*Value, error) {
	return simpleNumericOp("Sub", a, b,
		func(x, y *big.Int) *big.Int { return new(big.Int).Sub(x, y) },
		func(x, y *big.Float) *big.Float { return new(big.Float).SetPrec(53).Sub(x, y) },
	)
}

// Mul returns a * b. Both operands must be the same numeric kind.
func Mul(a, b *Value) (*Value, error) {
	return simpleNumericOp("Mul", a, b,
		func(x, y *big.Int) *big.Int { return new(big.Int).Mul(x, y) },
		func(x, y *big.Float) *big.Float { return new(big.Float).SetPrec(53).Mul(x, y) },
	)
}

// Div returns a / b. Both operands must be the same numeric kind.
// For integers, returns an error if b is zero.
// For floats, division by zero follows IEEE 754 (returns ±inf or NaN).
func Div(a, b *Value) (*Value, error) {
	a, b = unwrapTaggedUnion(a), unwrapTaggedUnion(b)
	if a.Kind == KindInt && b.Kind == KindInt {
		if b.Int.Sign() == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return &Value{Kind: KindInt, Int: new(big.Int).Quo(a.Int, b.Int), Type: a.Type}, nil
	}
	if a.Kind == KindFloat && b.Kind == KindFloat {
		if a.FloatIsNaN || b.FloatIsNaN {
			return nanFloat(a.Type), nil
		}
		r := new(big.Float).SetPrec(53)
		if b.Float.Sign() == 0 {
			if a.Float.Sign() == 0 {
				return nanFloat(a.Type), nil
			} else if a.Float.Sign() > 0 {
				r.SetInf(false)
			} else {
				r.SetInf(true)
			}
		} else {
			r.Quo(a.Float, b.Float)
		}
		return &Value{Kind: KindFloat, Float: r, Type: a.Type}, nil
	}
	return nil, fmt.Errorf("Div requires same numeric type, got %s and %s", a.Kind, b.Kind)
}

// Mod returns a %% b. Both operands must be the same numeric kind.
// For integers, returns an error if b is zero.
func Mod(a, b *Value) (*Value, error) {
	a, b = unwrapTaggedUnion(a), unwrapTaggedUnion(b)
	if a.Kind == KindInt && b.Kind == KindInt {
		if b.Int.Sign() == 0 {
			return nil, fmt.Errorf("modulo by zero")
		}
		return &Value{Kind: KindInt, Int: new(big.Int).Rem(a.Int, b.Int), Type: a.Type}, nil
	}
	if a.Kind == KindFloat && b.Kind == KindFloat {
		if a.FloatIsNaN || b.FloatIsNaN {
			return nanFloat(a.Type), nil
		}
		af, _ := a.Float.Float64()
		bf, _ := b.Float.Float64()
		result := math.Mod(af, bf)
		if math.IsNaN(result) {
			return nanFloat(a.Type), nil
		}
		return &Value{Kind: KindFloat, Float: new(big.Float).SetPrec(53).SetFloat64(result), Type: a.Type}, nil
	}
	return nil, fmt.Errorf("Mod requires same numeric type, got %s and %s", a.Kind, b.Kind)
}

// Pow returns a ^ b. Both operands must be the same numeric kind.
// For integers, the exponent must be non-negative and at most 10000.
func Pow(a, b *Value) (*Value, error) {
	a, b = unwrapTaggedUnion(a), unwrapTaggedUnion(b)
	if a.Kind == KindInt && b.Kind == KindInt {
		if b.Int.Sign() < 0 {
			return nil, fmt.Errorf("negative exponent in integer exponentiation")
		}
		if !b.Int.IsInt64() || b.Int.Int64() > 10000 {
			return nil, fmt.Errorf("exponent too large")
		}
		return &Value{Kind: KindInt, Int: new(big.Int).Exp(a.Int, b.Int, nil), Type: a.Type}, nil
	}
	if a.Kind == KindFloat && b.Kind == KindFloat {
		if a.FloatIsNaN || b.FloatIsNaN {
			return nanFloat(a.Type), nil
		}
		af, _ := a.Float.Float64()
		bf, _ := b.Float.Float64()
		result := math.Pow(af, bf)
		if math.IsNaN(result) {
			return nanFloat(a.Type), nil
		}
		return &Value{Kind: KindFloat, Float: new(big.Float).SetPrec(53).SetFloat64(result), Type: a.Type}, nil
	}
	return nil, fmt.Errorf("Pow requires same numeric type, got %s and %s", a.Kind, b.Kind)
}

// Negate returns -v. The operand must be KindInt or KindFloat.
func Negate(v *Value) (*Value, error) {
	v = unwrapTaggedUnion(v)
	switch v.Kind {
	case KindInt:
		return &Value{Kind: KindInt, Int: new(big.Int).Neg(v.Int), Type: v.Type}, nil
	case KindFloat:
		if v.FloatIsNaN {
			return nanFloat(v.Type), nil
		}
		return &Value{Kind: KindFloat, Float: new(big.Float).Neg(v.Float), Type: v.Type}, nil
	default:
		return nil, fmt.Errorf("Negate requires numeric operand, got %s", v.Kind)
	}
}

// Not returns the boolean negation of v. The operand must be KindBool.
func Not(v *Value) (*Value, error) {
	if v.Kind != KindBool {
		return nil, fmt.Errorf("Not requires bool, got %s", v.Kind)
	}
	return Bool(!v.Bool), nil
}

// Equal reports whether a and b are deeply equal.
// Values of different kinds are not equal. NaN is not equal to NaN (IEEE 754).
func Equal(a, b *Value) bool {
	return valuesEqual(a, b)
}

// Compare performs ordered comparison of a and b.
// Returns -1 if a < b, 0 if a == b, +1 if a > b.
// Both operands must be the same comparable kind (KindInt, KindFloat, or KindString).
// Returns an error for NaN or incompatible types.
func Compare(a, b *Value) (int, error) {
	a, b = unwrapTaggedUnion(a), unwrapTaggedUnion(b)
	if a.Kind == KindInt && b.Kind == KindInt {
		return a.Int.Cmp(b.Int), nil
	}
	if a.Kind == KindFloat && b.Kind == KindFloat {
		if a.FloatIsNaN || b.FloatIsNaN {
			return 0, fmt.Errorf("NaN is not comparable")
		}
		return a.Float.Cmp(b.Float), nil
	}
	if a.Kind == KindString && b.Kind == KindString {
		return strings.Compare(a.Str, b.Str), nil
	}
	return 0, fmt.Errorf("Compare requires same numeric or string type, got %s and %s", a.Kind, b.Kind)
}

// Concat concatenates two strings or two lists (the ++ operator in UZON).
func Concat(a, b *Value) (*Value, error) {
	a, b = unwrapTaggedUnion(a), unwrapTaggedUnion(b)
	if a.Kind == KindString && b.Kind == KindString {
		return String(a.Str + b.Str), nil
	}
	if a.Kind == KindList && b.Kind == KindList {
		elems := make([]*Value, 0, len(a.List.Elements)+len(b.List.Elements))
		elems = append(elems, a.List.Elements...)
		elems = append(elems, b.List.Elements...)
		return NewList(elems, a.List.ElementType), nil
	}
	return nil, fmt.Errorf("Concat requires string or list operands, got %s and %s", a.Kind, b.Kind)
}

// Repeat repeats a string or list n times (the ** operator in UZON).
func Repeat(v *Value, n int) (*Value, error) {
	v = unwrapTaggedUnion(v)
	if n < 0 {
		return nil, fmt.Errorf("Repeat requires non-negative count, got %d", n)
	}
	if v.Kind == KindString {
		return String(strings.Repeat(v.Str, n)), nil
	}
	if v.Kind == KindList {
		var elems []*Value
		for i := 0; i < n; i++ {
			elems = append(elems, v.List.Elements...)
		}
		return NewList(elems, v.List.ElementType), nil
	}
	return nil, fmt.Errorf("Repeat requires string or list operand, got %s", v.Kind)
}

// Contains reports whether elem is in list (the "in" operator in UZON).
func Contains(list, elem *Value) (bool, error) {
	if list.Kind != KindList {
		return false, fmt.Errorf("Contains requires a list, got %s", list.Kind)
	}
	for _, e := range list.List.Elements {
		if valuesEqual(elem, e) {
			return true, nil
		}
	}
	return false, nil
}

// --- Type conversions ---

// ToString converts v to a string Value.
// Supported source kinds: string, bool, int, float, null, enum, tagged union, union.
func ToString(v *Value) (*Value, error) {
	return toStr(v)
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
func ToInt(v *Value) (*Value, error) {
	switch v.Kind {
	case KindInt:
		return v, nil
	case KindFloat:
		if v.FloatIsNaN {
			return nil, fmt.Errorf("cannot convert NaN to integer")
		}
		if v.Float.IsInf() {
			return nil, fmt.Errorf("cannot convert infinity to integer")
		}
		n := new(big.Int)
		v.Float.Int(n)
		return &Value{Kind: KindInt, Int: n}, nil
	case KindString:
		n := new(big.Int)
		s := strings.ReplaceAll(v.Str, "_", "")
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
			return nil, fmt.Errorf("cannot parse %q as integer", v.Str)
		}
		return &Value{Kind: KindInt, Int: n}, nil
	default:
		return nil, fmt.Errorf("cannot convert %s to integer", v.Kind)
	}
}

// ToFloat converts v to a float Value.
// Supported source kinds: float, int, string (parsed).
// String parsing supports decimal notation, "inf", "-inf", "nan".
func ToFloat(v *Value) (*Value, error) {
	switch v.Kind {
	case KindFloat:
		return v, nil
	case KindInt:
		f := new(big.Float).SetPrec(53).SetInt(v.Int)
		return &Value{Kind: KindFloat, Float: f}, nil
	case KindString:
		s := strings.ReplaceAll(v.Str, "_", "")
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
				return nil, fmt.Errorf("cannot parse %q as float", v.Str)
			}
			return &Value{Kind: KindFloat, Float: f}, nil
		}
	default:
		return nil, fmt.Errorf("cannot convert %s to float", v.Kind)
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
		return &Value{Kind: KindInt, Int: intFn(a.Int, b.Int), Type: a.Type}, nil
	}
	if a.Kind == KindFloat && b.Kind == KindFloat {
		if a.FloatIsNaN || b.FloatIsNaN {
			return nanFloat(a.Type), nil
		}
		return &Value{Kind: KindFloat, Float: floatFn(a.Float, b.Float), Type: a.Type}, nil
	}
	return nil, fmt.Errorf("%s requires same numeric type, got %s and %s", name, a.Kind, b.Kind)
}

func nanFloat(ti *TypeInfo) *Value {
	return &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true, Type: ti}
}

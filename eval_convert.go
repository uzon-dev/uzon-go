// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/uzon-dev/uzon-go/ast"
)

// evalTo implements "to Type" conversion (§5.5).
func (ev *Evaluator) evalTo(e *ast.ToExpr, scope *Scope) (*Value, error) {
	val, err := ev.evalExpr(e.Value, scope)
	if err != nil {
		return nil, err
	}

	ti := ev.resolveTypeExpr(e.TypeExpr)

	// §5.11.0/§D.2: undefined propagates through "to" regardless of target type.
	// §5.13: env refs are statically typed as string — validate that the target
	// type is reachable from string even when the env var is missing.
	if val.Kind == KindUndefined || isUnresolvedIdent(val) {
		if isEnvRef(e.Value) && ti != nil {
			switch ti.BaseType {
			case "bool":
				return nil, typeErrorf("cannot convert string to bool")
			case "null":
				return nil, typeErrorf("cannot convert string to null")
			}
		}
		// Propagate target type so downstream consumers (e.g. `or else`)
		// can still validate type compatibility (§5.7).
		u := Undefined()
		u.Type = ti
		return u, nil
	}

	// §5.11: "to bool" only allows identity (bool to bool) — type error for all others.
	if ti != nil && ti.BaseType == "bool" && val.Kind != KindBool {
		return nil, typeErrorf("cannot convert %s to bool", val.Kind)
	}

	// §5.11.0/§D.1: "to null" is identity — only null can convert to null
	if ti != nil && ti.BaseType == "null" {
		if val.Kind == KindNull {
			return val, nil
		}
		return nil, typeErrorf("cannot convert %s to null", val.Kind)
	}

	return ev.convertValue(val, ti, e.TypeExpr.Path, scope)
}

func (ev *Evaluator) convertValue(val *Value, target *TypeInfo, typePath []string, scope *Scope) (*Value, error) {
	if target.BaseType == "string" {
		return ev.convertToString(val)
	}
	if isIntegerType(target.BaseType) {
		return ev.convertToInt(val, target)
	}
	if isFloatType(target.BaseType) {
		return ev.convertToFloat(val, target)
	}
	// Named enum type conversion
	if target.Name != "" {
		variants, ok := ev.enums.get(target.Name)
		if !ok && len(typePath) > 1 {
			variants, ok = ev.resolveEnumFromPath(typePath, scope)
		}
		if ok {
			if val.Kind == KindString {
				for _, v := range variants {
					if v == val.Str {
						return &Value{Kind: KindEnum, Enum: &EnumValue{Variant: val.Str, Variants: variants}, Type: target}, nil
					}
				}
				return nil, fmt.Errorf("'%s' is not a variant of %s", val.Str, target.Name)
			}
		}
	}
	return nil, typeErrorf("cannot convert %s to %s", val.Kind, target.BaseType)
}

// resolveEnumFromPath walks qualified type paths (e.g. ["shared", "RGB"])
// through struct values in scope to find enum variant definitions.
func (ev *Evaluator) resolveEnumFromPath(path []string, scope *Scope) ([]string, bool) {
	if len(path) < 2 {
		return nil, false
	}
	valuePath := path[:len(path)-1]
	typeName := path[len(path)-1]

	current, ok := scope.get(valuePath[0])
	if !ok {
		return nil, false
	}
	for _, seg := range valuePath[1:] {
		if current.Kind != KindStruct {
			return nil, false
		}
		current = current.Struct.Get(seg)
		if current == nil {
			return nil, false
		}
	}
	if current.Kind == KindEnum && current.Type != nil && current.Type.Name == typeName {
		return current.Enum.Variants, true
	}
	if current.Kind == KindStruct {
		for _, f := range current.Struct.Fields {
			if f.Value.Kind == KindEnum && f.Value.Type != nil && f.Value.Type.Name == typeName {
				return f.Value.Enum.Variants, true
			}
		}
	}
	return nil, false
}

func (ev *Evaluator) convertToString(val *Value) (*Value, error) {
	switch val.Kind {
	case KindString:
		return val, nil
	case KindBool:
		if val.Bool {
			return String("true"), nil
		}
		return String("false"), nil
	case KindInt:
		return String(val.Int.String()), nil
	case KindFloat:
		if val.FloatIsNaN {
			return String("nan"), nil
		}
		f, _ := val.Float.Float64()
		return String(formatFloat(f)), nil
	case KindNull:
		return String("null"), nil
	case KindEnum:
		return String(val.Enum.Variant), nil
	case KindTaggedUnion:
		return ev.convertToString(val.TaggedUnion.Inner)
	case KindUnion:
		return ev.convertToString(val.Union.Inner)
	default:
		return nil, typeErrorf("cannot convert %s to string", val.Kind)
	}
}

func formatFloat(f float64) string {
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	if math.IsNaN(f) {
		return "nan"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.Contains(s, ".") && !strings.Contains(s, "e") && !strings.Contains(s, "E") {
		s += ".0"
	}
	return s
}

func (ev *Evaluator) convertToInt(val *Value, target *TypeInfo) (*Value, error) {
	var n *big.Int
	switch val.Kind {
	case KindInt:
		n = new(big.Int).Set(val.Int)
	case KindFloat:
		if val.FloatIsNaN {
			return nil, fmt.Errorf("cannot convert NaN to integer")
		}
		f, _ := val.Float.Float64()
		if math.IsInf(f, 0) {
			return nil, fmt.Errorf("cannot convert %v to integer", f)
		}
		n = new(big.Int)
		val.Float.Int(n)
	case KindString:
		n = new(big.Int)
		s := strings.ReplaceAll(val.Str, "_", "")
		// Use base 0 to auto-detect prefix (0x, 0o, 0b) including negative prefixes like "-0xff"
		_, ok := n.SetString(s, 0)
		if !ok {
			return nil, fmt.Errorf("cannot parse %q as integer", val.Str)
		}
	default:
		return nil, typeErrorf("cannot convert %s to %s", val.Kind, target.BaseType)
	}
	if isIntegerType(target.BaseType) {
		if err := checkIntRange(n, target.BitSize, target.Signed); err != nil {
			return nil, err
		}
	}
	return &Value{Kind: KindInt, Int: n, Type: target}, nil
}

func checkIntRange(n *big.Int, bits int, signed bool) error {
	// §v0.8: i0/u0 are unit types — only value 0 is valid.
	if bits == 0 {
		if n.Sign() != 0 {
			if signed {
				return fmt.Errorf("value %s out of range for i0", n)
			}
			return fmt.Errorf("value %s out of range for u0", n)
		}
		return nil
	}
	if signed {
		min := new(big.Int).Lsh(big.NewInt(-1), uint(bits-1))
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits-1)), big.NewInt(1))
		if n.Cmp(min) < 0 || n.Cmp(max) > 0 {
			return fmt.Errorf("value %s out of range for i%d", n, bits)
		}
	} else {
		if n.Sign() < 0 {
			return fmt.Errorf("negative value %s for unsigned u%d", n, bits)
		}
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
		if n.Cmp(max) > 0 {
			return fmt.Errorf("value %s out of range for u%d", n, bits)
		}
	}
	return nil
}

func (ev *Evaluator) convertToFloat(val *Value, target *TypeInfo) (*Value, error) {
	if val.Kind == KindFloat && val.FloatIsNaN {
		return &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true, Type: target}, nil
	}
	var f *big.Float
	switch val.Kind {
	case KindFloat:
		f = new(big.Float).Copy(val.Float)
	case KindInt:
		f = new(big.Float).SetPrec(53).SetInt(val.Int)
	case KindString:
		s := strings.ReplaceAll(val.Str, "_", "")
		switch s {
		case "inf":
			f = new(big.Float).SetInf(false)
		case "-inf":
			f = new(big.Float).SetInf(true)
		case "nan", "-nan":
			return &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true, Type: target}, nil
		default:
			var err error
			f, _, err = big.ParseFloat(s, 10, 53, big.ToNearestEven)
			if err != nil {
				return nil, fmt.Errorf("cannot parse %q as float", val.Str)
			}
		}
	default:
		return nil, typeErrorf("cannot convert %s to %s", val.Kind, target.BaseType)
	}
	return &Value{Kind: KindFloat, Float: f, Type: target}, nil
}

// isEnvRef reports whether expr is an env member access (env.NAME).
func isEnvRef(expr ast.Expr) bool {
	m, ok := expr.(*ast.MemberExpr)
	if !ok {
		return false
	}
	_, isEnv := m.Object.(*ast.EnvExpr)
	return isEnv
}

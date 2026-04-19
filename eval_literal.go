// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/uzon-dev/uzon-go/ast"
	"github.com/uzon-dev/uzon-go/token"
)

// --- Literals and identifiers ---

func (ev *Evaluator) evalLiteral(e *ast.LiteralExpr) (*Value, error) {
	switch e.Token.Type {
	case token.IntLit:
		n := new(big.Int)
		lit := strings.ReplaceAll(e.Token.Literal, "_", "")
		var ok bool
		switch {
		case strings.HasPrefix(lit, "-0x") || strings.HasPrefix(lit, "-0X"):
			_, ok = n.SetString(lit[3:], 16)
			n.Neg(n)
		case strings.HasPrefix(lit, "0x") || strings.HasPrefix(lit, "0X"):
			_, ok = n.SetString(lit[2:], 16)
		case strings.HasPrefix(lit, "-0o") || strings.HasPrefix(lit, "-0O"):
			_, ok = n.SetString(lit[3:], 8)
			n.Neg(n)
		case strings.HasPrefix(lit, "0o") || strings.HasPrefix(lit, "0O"):
			_, ok = n.SetString(lit[2:], 8)
		case strings.HasPrefix(lit, "-0b") || strings.HasPrefix(lit, "-0B"):
			_, ok = n.SetString(lit[3:], 2)
			n.Neg(n)
		case strings.HasPrefix(lit, "0b") || strings.HasPrefix(lit, "0B"):
			_, ok = n.SetString(lit[2:], 2)
		default:
			_, ok = n.SetString(lit, 10)
		}
		if !ok {
			return nil, fmt.Errorf("invalid integer literal: %s", e.Token.Literal)
		}
		// §3.1: integer literals default to i64. Check range so that values
		// that cannot fit as i64 are rejected unless explicitly typed larger.
		if err := checkIntRange(n, 64, true); err != nil {
			return nil, fmt.Errorf("integer literal %s: %w", e.Token.Literal, err)
		}
		return &Value{Kind: KindInt, Int: n, Type: &TypeInfo{BaseType: "i64", BitSize: 64, Signed: true}, Adoptable: true}, nil

	case token.FloatLit:
		lit := strings.ReplaceAll(e.Token.Literal, "_", "")
		ti := &TypeInfo{BaseType: "f64", BitSize: 64}
		switch lit {
		case "nan", "-nan":
			return &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true, Type: ti, Adoptable: true}, nil
		case "inf":
			f := new(big.Float).SetPrec(53).SetInf(false)
			return &Value{Kind: KindFloat, Float: f, Type: ti, Adoptable: true}, nil
		case "-inf":
			f := new(big.Float).SetPrec(53).SetInf(true)
			return &Value{Kind: KindFloat, Float: f, Type: ti, Adoptable: true}, nil
		default:
			f, _, err := big.ParseFloat(lit, 10, 53, big.ToNearestEven)
			if err != nil {
				return nil, fmt.Errorf("invalid float literal: %s", e.Token.Literal)
			}
			return &Value{Kind: KindFloat, Float: f, Type: ti, Adoptable: true}, nil
		}

	case token.StringLit:
		return String(e.Token.Literal), nil
	case token.True:
		return Bool(true), nil
	case token.False:
		return Bool(false), nil
	case token.Null:
		return Null(), nil
	case token.Inf:
		f := new(big.Float).SetPrec(53).SetInf(false)
		return &Value{Kind: KindFloat, Float: f, Type: &TypeInfo{BaseType: "f64", BitSize: 64}}, nil
	case token.NaN:
		return &Value{Kind: KindFloat, Float: new(big.Float), FloatIsNaN: true, Type: &TypeInfo{BaseType: "f64", BitSize: 64}}, nil
	default:
		return nil, fmt.Errorf("unexpected literal token: %v", e.Token.Type)
	}
}

func (ev *Evaluator) evalIdent(e *ast.IdentExpr, scope *Scope) (*Value, error) {
	if v, ok := scope.get(e.Name); ok {
		return v, nil
	}
	// §5.12: bare identifier not in scope.
	// Carries the name for deferred enum variant resolution via "as EnumType".
	// Treated as undefined in non-enum contexts (or else, binding level, etc.).
	return &Value{Kind: KindString, Str: e.Name, Type: &TypeInfo{Name: "__ident__"}}, nil
}

// evalEnvObj builds a virtual struct from environment variables (§5.13).
func (ev *Evaluator) evalEnvObj() *Value {
	var fields []Field
	for k, v := range ev.env {
		fields = append(fields, Field{Name: k, Value: String(v)})
	}
	return &Value{Kind: KindStruct, Struct: &StructValue{Fields: fields}}
}

// --- Member access ---

func (ev *Evaluator) evalMember(e *ast.MemberExpr, scope *Scope) (*Value, error) {
	obj, err := ev.evalExpr(e.Object, scope)
	if err != nil {
		return nil, err
	}
	if obj.Kind == KindUndefined || isUnresolvedIdent(obj) {
		return Undefined(), nil
	}
	return ev.accessMember(obj, e.Member)
}

func (ev *Evaluator) accessMember(obj *Value, member string) (*Value, error) {
	switch obj.Kind {
	case KindStruct:
		v := obj.Struct.Get(member)
		if v == nil {
			return Undefined(), nil
		}
		return v, nil
	case KindList:
		return ev.accessIndex(obj.List.Elements, member)
	case KindTuple:
		return ev.accessIndex(obj.Tuple.Elements, member)
	case KindTaggedUnion:
		// §3.7.1: member access is transparent — access inner value's members.
		return ev.accessMember(obj.TaggedUnion.Inner, member)
	case KindUnion:
		// Untagged union: transparent member access on inner value.
		return ev.accessMember(obj.Union.Inner, member)
	case KindUndefined:
		return Undefined(), nil
	case KindNull:
		return nil, typeErrorf("member access on null")
	case KindFunction:
		// §5.12 R4: functions have no fields — member access is a type error.
		return nil, typeErrorf("member access on function value: functions have no fields")
	default:
		return Undefined(), nil
	}
}

// ordinals maps English ordinal words to zero-based indices for
// tuple/list access (§3.3, §3.4).
var ordinals = map[string]int{
	"first": 0, "second": 1, "third": 2, "fourth": 3, "fifth": 4,
	"sixth": 5, "seventh": 6, "eighth": 7, "ninth": 8, "tenth": 9,
}

func (ev *Evaluator) accessIndex(elems []*Value, member string) (*Value, error) {
	if idx, ok := ordinals[member]; ok {
		if idx >= 0 && idx < len(elems) {
			return elems[idx], nil
		}
		return Undefined(), nil
	}
	idx, err := strconv.Atoi(member)
	if err != nil {
		return Undefined(), nil
	}
	if idx >= 0 && idx < len(elems) {
		return elems[idx], nil
	}
	return Undefined(), nil
}

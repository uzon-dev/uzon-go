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
	// Bare identifier: may be an enum variant; defer resolution to "as EnumType"
	return &Value{Kind: KindString, Str: e.Name, Type: &TypeInfo{Name: "__ident__"}}, nil
}

// evalSelf builds a virtual struct from all bindings visible in the scope chain (§5.7).
func (ev *Evaluator) evalSelf(scope *Scope) *Value {
	sv := &StructValue{Fields: nil, fieldIndex: make(map[string]int)}
	ev.collectScopeBindings(scope, sv)
	return &Value{Kind: KindStruct, Struct: sv}
}

func (ev *Evaluator) collectScopeBindings(scope *Scope, sv *StructValue) {
	if scope == nil {
		return
	}
	ev.collectScopeBindings(scope.parent, sv)
	for name, val := range scope.bindings {
		if name == scope.exclude {
			continue
		}
		if _, exists := sv.fieldIndex[name]; exists {
			sv.Fields[sv.fieldIndex[name]].Value = val
		} else {
			sv.fieldIndex[name] = len(sv.Fields)
			sv.Fields = append(sv.Fields, Field{Name: name, Value: val})
		}
	}
}

// evalEnvObj builds a virtual struct from environment variables (§5.8).
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
	if obj.Kind == KindUndefined {
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
	case KindUndefined:
		return Undefined(), nil
	case KindNull:
		return nil, fmt.Errorf("member access on null")
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

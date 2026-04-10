// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"strings"

	"github.com/uzon-dev/uzon-go/ast"
)

// --- Functions ---

func (ev *Evaluator) evalFunctionDef(e *ast.FunctionExpr, scope *Scope) (*Value, error) {
	var params []FuncParam
	for _, p := range e.Params {
		params = append(params, FuncParam{
			Name: p.Name,
			Type: ev.resolveTypeExpr(p.TypeExpr),
		})
	}
	return &Value{
		Kind: KindFunction,
		Function: &FunctionValue{
			Params:     params,
			ReturnType: ev.resolveTypeExpr(e.ReturnType),
			Body:       e,
			Scope:      scope,
		},
	}, nil
}

// callFunction invokes a user-defined function with the given arguments.
// §3.8: maximum call depth is 256 to prevent unbounded recursion.
func (ev *Evaluator) callFunction(fn *Value, args []*Value, scope *Scope) (*Value, error) {
	const maxCallDepth = 256
	ev.callDepth++
	if ev.callDepth > maxCallDepth {
		ev.callDepth--
		return nil, fmt.Errorf("maximum call depth exceeded (possible recursion)")
	}
	defer func() { ev.callDepth-- }()

	fv := fn.Function
	fe, ok := fv.Body.(*ast.FunctionExpr)
	if !ok {
		return nil, fmt.Errorf("invalid function body")
	}

	fnScope := newScope(fv.Scope.(*Scope))
	for i, p := range fe.Params {
		if i < len(args) {
			arg := args[i]
			if p.TypeExpr != nil && len(p.TypeExpr.TupleElems) > 0 && arg.Kind == KindTuple {
				if len(p.TypeExpr.TupleElems) != len(arg.Tuple.Elements) {
					return nil, fmt.Errorf("argument %q: expected %d-element tuple, got %d", p.Name, len(p.TypeExpr.TupleElems), len(arg.Tuple.Elements))
				}
			}
			if p.TypeExpr != nil && arg.Adoptable {
				pti := ev.resolveTypeExpr(p.TypeExpr)
				if pti != nil && pti.BaseType != "" {
					if arg.Kind == KindInt && isIntegerType(pti.BaseType) {
						if err := checkIntRange(arg.Int, pti.BitSize, pti.Signed); err != nil {
							return nil, fmt.Errorf("argument %q: %w", p.Name, err)
						}
					}
					arg.Type = pti
					arg.Adoptable = false
				}
			}
			fnScope.set(p.Name, arg)
		} else if p.Default != nil {
			dv, err := ev.evalExpr(p.Default, fv.Scope.(*Scope))
			if err != nil {
				return nil, err
			}
			fnScope.set(p.Name, dv)
		}
	}

	for _, b := range fe.Bindings {
		v, err := ev.evalExpr(b.Value, fnScope)
		if err != nil {
			return nil, err
		}
		fnScope.set(b.Name, v)
	}

	if fe.Body == nil {
		return nil, fmt.Errorf("function body has no return expression")
	}
	return ev.evalExpr(fe.Body, fnScope)
}

// collectBindingDeps finds all sibling binding names referenced in an AST subtree.
// Used for dependency analysis in declarative binding evaluation (§5.12).
func collectBindingDeps(expr ast.Expr) map[string]bool {
	refs := make(map[string]bool)
	walkExpr(expr, func(e ast.Expr) {
		switch n := e.(type) {
		case *ast.MemberExpr:
			if _, ok := n.Object.(*ast.SelfExpr); ok {
				refs[n.Member] = true
			}
		case *ast.IdentExpr:
			refs[n.Name] = true
		}
	})
	return refs
}

// collectSelfCallRefs finds self.X names that are called as functions.
func collectSelfCallRefs(expr ast.Expr) map[string]bool {
	refs := make(map[string]bool)
	walkExpr(expr, func(e ast.Expr) {
		if ce, ok := e.(*ast.CallExpr); ok {
			if me, ok := ce.Func.(*ast.MemberExpr); ok {
				if _, ok := me.Object.(*ast.SelfExpr); ok {
					refs[me.Member] = true
				}
			}
			for _, arg := range ce.Args {
				if me, ok := arg.(*ast.MemberExpr); ok {
					if _, ok := me.Object.(*ast.SelfExpr); ok {
						refs[me.Member] = true
					}
				}
			}
		}
	})
	return refs
}

// walkExpr recursively visits all AST nodes in a subtree.
func walkExpr(expr ast.Expr, visit func(ast.Expr)) {
	if expr == nil {
		return
	}
	visit(expr)
	switch e := expr.(type) {
	case *ast.MemberExpr:
		walkExpr(e.Object, visit)
	case *ast.CallExpr:
		walkExpr(e.Func, visit)
		for _, a := range e.Args {
			walkExpr(a, visit)
		}
	case *ast.BinaryExpr:
		walkExpr(e.Left, visit)
		walkExpr(e.Right, visit)
	case *ast.UnaryExpr:
		walkExpr(e.Operand, visit)
	case *ast.IfExpr:
		walkExpr(e.Cond, visit)
		walkExpr(e.Then, visit)
		walkExpr(e.Else, visit)
	case *ast.CaseExpr:
		walkExpr(e.Scrutinee, visit)
		for _, w := range e.Whens {
			walkExpr(w.Value, visit)
			walkExpr(w.Then, visit)
		}
		walkExpr(e.Else, visit)
	case *ast.StructExpr:
		for _, b := range e.Fields {
			walkExpr(b.Value, visit)
		}
	case *ast.ListExpr:
		for _, el := range e.Elements {
			walkExpr(el, visit)
		}
	case *ast.TupleExpr:
		for _, el := range e.Elements {
			walkExpr(el, visit)
		}
	case *ast.AsExpr:
		walkExpr(e.Value, visit)
	case *ast.ToExpr:
		walkExpr(e.Value, visit)
	case *ast.WithExpr:
		walkExpr(e.Base, visit)
		if e.Override != nil {
			for _, b := range e.Override.Fields {
				walkExpr(b.Value, visit)
			}
		}
	case *ast.ExtendsExpr:
		walkExpr(e.Base, visit)
		if e.Extension != nil {
			for _, b := range e.Extension.Fields {
				walkExpr(b.Value, visit)
			}
		}
	case *ast.FromExpr:
		walkExpr(e.Value, visit)
	case *ast.UnionExpr:
		walkExpr(e.Value, visit)
	case *ast.NamedExpr:
		walkExpr(e.Value, visit)
	case *ast.IsNamedExpr:
		walkExpr(e.Value, visit)
	case *ast.OfExpr:
		walkExpr(e.Source, visit)
	case *ast.FunctionExpr:
		for _, b := range e.Bindings {
			walkExpr(b.Value, visit)
		}
		walkExpr(e.Body, visit)
	case *ast.AreExpr:
		for _, el := range e.Elements {
			walkExpr(el, visit)
		}
	case *ast.InterpolatedStringExpr:
		for _, p := range e.Parts {
			if p.IsExpr {
				walkExpr(p.Expr, visit)
			}
		}
	}
}

// --- Are and interpolation ---

func (ev *Evaluator) evalAre(e *ast.AreExpr, scope *Scope) (*Value, error) {
	var elems []*Value
	for _, elem := range e.Elements {
		v, err := ev.evalExpr(elem, scope)
		if err != nil {
			return nil, err
		}
		elems = append(elems, v)
	}
	var elemType *TypeInfo
	if e.TypeAnnotation != nil {
		elemType = ev.resolveTypeExpr(e.TypeAnnotation)
	}
	return NewList(elems, elemType), nil
}

func (ev *Evaluator) evalInterpolatedString(e *ast.InterpolatedStringExpr, scope *Scope) (*Value, error) {
	var sb strings.Builder
	for _, part := range e.Parts {
		if part.IsExpr {
			v, err := ev.evalExpr(part.Expr, scope)
			if err != nil {
				return nil, err
			}
			if v.Kind == KindUndefined {
				return nil, fmt.Errorf("undefined in string interpolation")
			}
			s, err := ev.convertToString(v)
			if err != nil {
				return nil, fmt.Errorf("string interpolation: %w", err)
			}
			sb.WriteString(s.Str)
		} else {
			sb.WriteString(part.Text)
		}
	}
	return String(sb.String()), nil
}

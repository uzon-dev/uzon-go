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
func (ev *Evaluator) callFunction(fn *Value, args []*Value) (*Value, error) {
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

	// Fill default values for missing arguments
	fullArgs := make([]*Value, 0, len(fe.Params))
	fullArgs = append(fullArgs, args...)
	for i := len(args); i < len(fe.Params); i++ {
		if fe.Params[i].Default != nil {
			dv, err := ev.evalExpr(fe.Params[i].Default, fv.Scope.(*Scope))
			if err != nil {
				return nil, err
			}
			fullArgs = append(fullArgs, dv)
		}
	}

	// Create function scope and bind parameters
	fnScope := newScope(fv.Scope.(*Scope))
	for i, p := range fe.Params {
		if i < len(fullArgs) {
			arg := fullArgs[i]
			// §3.3: tuple shape check
			if p.TypeExpr != nil && len(p.TypeExpr.TupleElems) > 0 && arg.Kind == KindTuple {
				if len(p.TypeExpr.TupleElems) != len(arg.Tuple.Elements) {
					return nil, fmt.Errorf("argument %q: expected %d-element tuple, got %d", p.Name, len(p.TypeExpr.TupleElems), len(arg.Tuple.Elements))
				}
			}
			// §5: adopt parameter type for adoptable literals
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
			// §3.8: general parameter type assertion
			if p.TypeExpr != nil && !arg.Adoptable {
				pti := ev.resolveTypeExpr(p.TypeExpr)
				if pti != nil && pti.BaseType != "" {
					if !argTypeCompatible(arg, pti) {
						return nil, typeErrorf("argument %q: expected %s, got %s", p.Name, pti.BaseType, arg.Kind)
					}
				}
			}
			// §6.3: nominal struct type check
			if p.TypeExpr != nil && arg.Kind == KindStruct {
				pti := ev.resolveTypeExpr(p.TypeExpr)
				if pti != nil && pti.Name != "" {
					if expectedFields, ok := ev.structShapes[pti.Name]; ok {
						if len(arg.Struct.Fields) != len(expectedFields) {
							return nil, typeErrorf("argument %q: expected %s (%d fields), got struct with %d fields",
								p.Name, pti.Name, len(expectedFields), len(arg.Struct.Fields))
						}
						for _, ef := range expectedFields {
							actual := arg.Struct.Get(ef.Name)
							if actual == nil {
								return nil, typeErrorf("argument %q: missing field %q for type %s",
									p.Name, ef.Name, pti.Name)
							}
							if err := ev.checkWithTypeCompat(ef.Value, actual, ef.Name); err != nil {
								return nil, typeErrorf("argument %q: %v", p.Name, err)
							}
						}
					}
				}
			}
			fnScope.set(p.Name, arg)
		}
	}

	// Evaluate intermediate bindings with dependency analysis (§5.12)
	if len(fe.Bindings) > 0 {
		nameSet := make(map[string]bool, len(fe.Bindings))
		for _, b := range fe.Bindings {
			if nameSet[b.Name] {
				return nil, fmt.Errorf("duplicate binding %q in function body", b.Name)
			}
			nameSet[b.Name] = true
		}
		deps := make(map[string][]string, len(fe.Bindings))
		for _, b := range fe.Bindings {
			refs := collectBindingDeps(b.Value)
			for ref := range refs {
				if ref != b.Name && nameSet[ref] {
					deps[b.Name] = append(deps[b.Name], ref)
				}
			}
		}
		evalOrder, err := topoSort(fe.Bindings, deps)
		if err != nil {
			return nil, err
		}
		for _, b := range evalOrder {
			v, err := ev.evalExpr(b.Value, fnScope)
			if err != nil {
				return nil, err
			}
			fnScope.set(b.Name, v)
		}
	}

	if fe.Body == nil {
		return nil, fmt.Errorf("function body has no return expression")
	}
	result, err := ev.evalExpr(fe.Body, fnScope)
	if err != nil {
		return nil, err
	}

	// §6.3: check function return type compatibility for named types
	if fv.ReturnType != nil && fv.ReturnType.Name != "" {
		if result.Type != nil && result.Type.Name != "" && result.Type.Name != fv.ReturnType.Name {
			return nil, fmt.Errorf("function return type mismatch: expected %s, got %s", fv.ReturnType.Name, result.Type.Name)
		}
	}

	return result, nil
}

// argTypeCompatible checks if an argument's runtime kind matches a declared parameter type.
func argTypeCompatible(arg *Value, pti *TypeInfo) bool {
	switch arg.Kind {
	case KindInt:
		return isIntegerType(pti.BaseType)
	case KindFloat:
		return isFloatType(pti.BaseType)
	case KindString:
		return pti.BaseType == "string"
	case KindBool:
		return pti.BaseType == "bool"
	case KindNull:
		return pti.BaseType == "null"
	case KindStruct:
		return true // struct shape checked separately
	case KindList:
		return true // list element type checked by usage
	case KindTuple:
		return true // tuple shape checked separately
	case KindEnum:
		return true // enum type checked by variant matching
	case KindFunction:
		return true // function type checked by param/return matching
	case KindTaggedUnion, KindUnion:
		return true // union inner type varies
	}
	return true
}

// collectBindingDeps finds all sibling binding names referenced in an AST subtree.
// Used for dependency analysis in declarative binding evaluation (§5.12).
func collectBindingDeps(expr ast.Expr) map[string]bool {
	refs := make(map[string]bool)
	walkExpr(expr, func(e ast.Expr) {
		if n, ok := e.(*ast.IdentExpr); ok {
			refs[n.Name] = true
		}
	})
	return refs
}

// collectCallRefs finds binding names that are called as functions.
func collectCallRefs(expr ast.Expr) map[string]bool {
	refs := make(map[string]bool)
	walkExpr(expr, func(e ast.Expr) {
		if ce, ok := e.(*ast.CallExpr); ok {
			if id, ok := ce.Func.(*ast.IdentExpr); ok {
				refs[id.Name] = true
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
	case *ast.PlusExpr:
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
	case *ast.IsTypeExpr:
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
			if v.Kind == KindUndefined || isUnresolvedIdent(v) {
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

func (ev *Evaluator) evalCall(e *ast.CallExpr, scope *Scope) (*Value, error) {
	// Check for std library calls: std.funcName(...)
	if me, ok := e.Func.(*ast.MemberExpr); ok {
		if id, ok := me.Object.(*ast.IdentExpr); ok && id.Name == "std" {
			return ev.evalStdCall(me.Member, e.Args, scope)
		}
	}

	fn, err := ev.evalExpr(e.Func, scope)
	if err != nil {
		return nil, err
	}
	if fn.Kind != KindFunction {
		return nil, typeErrorf("calling non-function value (%s)", fn.Kind)
	}

	// Evaluate arguments
	args := make([]*Value, 0, len(e.Args))
	for _, a := range e.Args {
		v, err := ev.evalExpr(a, scope)
		if err != nil {
			return nil, err
		}
		// §3.1: undefined as argument is a runtime error
		if v.Kind == KindUndefined || isUnresolvedIdent(v) {
			return nil, fmt.Errorf("undefined argument in function call")
		}
		args = append(args, v)
	}

	return ev.callFunction(fn, args)
}

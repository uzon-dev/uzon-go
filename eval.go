// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/uzon-dev/uzon-go/ast"
	"github.com/uzon-dev/uzon-go/token"
)

// PosError is an error annotated with a source position (§11.2.0).
// When Cause is a *PosError, Error() formats a stack-trace-like chain.
type PosError struct {
	Pos   token.Pos
	Msg   string
	Cause error
}

func (e *PosError) Error() string {
	s := e.Pos.String() + ": " + e.Msg
	if e.Cause == nil {
		return s
	}
	if pe, ok := e.Cause.(*PosError); ok {
		return s + "\n  " + strings.ReplaceAll(pe.Error(), "\n", "\n  ")
	}
	return s + ": " + e.Cause.Error()
}

func (e *PosError) Unwrap() error {
	return e.Cause
}

// Scope represents a lexical scope for binding resolution.
// The exclude field implements the self-exclusion rule (§3.8):
// the binding currently being evaluated cannot see itself.
type Scope struct {
	bindings map[string]*Value
	parent   *Scope
	exclude  string
}

func newScope(parent *Scope) *Scope {
	return &Scope{bindings: make(map[string]*Value), parent: parent}
}

func (s *Scope) get(name string) (*Value, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if name != cur.exclude {
			if v, ok := cur.bindings[name]; ok {
				return v, true
			}
		}
	}
	return nil, false
}

func (s *Scope) set(name string, v *Value) {
	s.bindings[name] = v
}

// TypeRegistry tracks named types declared with "called" (§6).
type TypeRegistry struct {
	types  map[string]*TypeInfo
	parent *TypeRegistry
}

func newTypeRegistry(parent *TypeRegistry) *TypeRegistry {
	return &TypeRegistry{types: make(map[string]*TypeInfo), parent: parent}
}

func (r *TypeRegistry) get(path []string) (*TypeInfo, bool) {
	key := strings.Join(path, ".")
	for cur := r; cur != nil; cur = cur.parent {
		if ti, ok := cur.types[key]; ok {
			return ti, true
		}
	}
	return nil, false
}

func (r *TypeRegistry) set(name string, ti *TypeInfo) {
	r.types[name] = ti
}

// EnumRegistry tracks named enum types and their variants (§3.4).
type EnumRegistry struct {
	enums  map[string][]string
	parent *EnumRegistry
}

func newEnumRegistry(parent *EnumRegistry) *EnumRegistry {
	return &EnumRegistry{enums: make(map[string][]string), parent: parent}
}

func (r *EnumRegistry) get(name string) ([]string, bool) {
	for cur := r; cur != nil; cur = cur.parent {
		if v, ok := cur.enums[name]; ok {
			return v, true
		}
	}
	return nil, false
}

// Evaluator evaluates UZON AST into Values.
type Evaluator struct {
	scope          *Scope
	types          *TypeRegistry
	enums          *EnumRegistry
	taggedVariants map[string][]TaggedVariant
	structShapes   map[string][]string
	env            map[string]string
	baseDir        string
	imported       map[string]*Value
	callDepth      int
}

// NewEvaluator creates a new evaluator with the process environment.
func NewEvaluator() *Evaluator {
	return &Evaluator{
		scope:          newScope(nil),
		types:          newTypeRegistry(nil),
		enums:          newEnumRegistry(nil),
		taggedVariants: make(map[string][]TaggedVariant),
		structShapes:   make(map[string][]string),
		env:            envMap(),
		imported:       make(map[string]*Value),
	}
}

func envMap() map[string]string {
	m := make(map[string]string)
	for _, e := range os.Environ() {
		if k, v, ok := strings.Cut(e, "="); ok {
			m[k] = v
		}
	}
	return m
}

// EvalDocument evaluates a parsed document and returns the top-level struct.
func (ev *Evaluator) EvalDocument(doc *ast.Document) (*Value, error) {
	return ev.evalBindings(doc.Bindings, ev.scope)
}

// evalBindings evaluates a sequence of bindings into a struct value.
// §5.12: evaluation order is determined by the dependency graph, not textual order.
func (ev *Evaluator) evalBindings(bindings []*ast.Binding, scope *Scope) (*Value, error) {
	nameSet := make(map[string]bool, len(bindings))
	bindingByName := make(map[string]*ast.Binding, len(bindings))
	for _, b := range bindings {
		// Functions and "of" bindings may re-bind
		switch b.Value.(type) {
		case *ast.FunctionExpr, *ast.OfExpr:
		default:
			if nameSet[b.Name] {
				return nil, &PosError{Pos: b.Position, Msg: fmt.Sprintf("duplicate binding %q", b.Name)}
			}
		}
		// Standalone self/env/undefined are errors
		switch b.Value.(type) {
		case *ast.SelfExpr:
			return nil, &PosError{Pos: b.Value.Pos(), Msg: "standalone self is not a value"}
		case *ast.EnvExpr:
			return nil, &PosError{Pos: b.Value.Pos(), Msg: "standalone env is not a value"}
		case *ast.UndefinedExpr:
			return nil, &PosError{Pos: b.Value.Pos(), Msg: "undefined cannot be used as a literal value"}
		}
		nameSet[b.Name] = true
		bindingByName[b.Name] = b
	}

	// Build dependency graph from self.X references
	deps := make(map[string][]string, len(bindings))
	for _, b := range bindings {
		refs := collectBindingDeps(b.Value)
		for ref := range refs {
			if ref != b.Name && nameSet[ref] {
				deps[b.Name] = append(deps[b.Name], ref)
			}
		}
	}

	// Topological sort (Kahn's algorithm)
	evalOrder, err := topoSort(bindings, deps)
	if err != nil {
		return nil, err
	}

	// Evaluate in dependency order
	evaluated := make(map[string]*Value, len(bindings))
	for _, b := range evalOrder {
		oldExclude := scope.exclude
		scope.exclude = b.Name
		v, err := ev.evalExpr(b.Value, scope)
		scope.exclude = oldExclude
		if err != nil {
			return nil, &PosError{Pos: b.Position, Msg: fmt.Sprintf("binding %q", b.Name), Cause: err}
		}

		// §3.4: bare empty list without type annotation is an error
		if _, isBareList := b.Value.(*ast.ListExpr); isBareList {
			if v.Kind == KindList && len(v.List.Elements) == 0 {
				return nil, &PosError{Pos: b.Position, Msg: fmt.Sprintf("binding %q: empty list requires type annotation (e.g. [] as [i32])", b.Name)}
			}
		}

		// Handle "called" type naming (§6)
		if b.CalledName != "" {
			if _, exists := ev.types.types[b.CalledName]; exists {
				return nil, &PosError{Pos: b.Position, Msg: fmt.Sprintf("duplicate type name %q", b.CalledName)}
			}
			ti := ev.inferType(v)
			ti.Name = b.CalledName
			v.Type = ti
			ev.types.set(b.CalledName, ti)
			if v.Kind == KindEnum {
				ev.enums.enums[b.CalledName] = v.Enum.Variants
			}
			if v.Kind == KindTaggedUnion {
				ev.taggedVariants[b.CalledName] = v.TaggedUnion.Variants
			}
			if v.Kind == KindStruct {
				var names []string
				for _, f := range v.Struct.Fields {
					names = append(names, f.Name)
				}
				ev.structShapes[b.CalledName] = names
			}
		}

		scope.set(b.Name, v)
		evaluated[b.Name] = v
	}

	// Build fields in textual (source) order
	fields := make([]Field, 0, len(bindings))
	for _, b := range bindings {
		fields = append(fields, Field{Name: b.Name, Value: evaluated[b.Name]})
	}

	// §3.8: detect direct and mutual recursion among function bindings
	funcRefs := make(map[string]map[string]bool)
	funcPos := make(map[string]token.Pos)
	for _, f := range fields {
		if f.Value.Kind == KindFunction {
			fe, ok := f.Value.Function.Body.(*ast.FunctionExpr)
			if ok {
				refs := collectSelfCallRefs(fe)
				funcRefs[f.Name] = refs
				funcPos[f.Name] = fe.Pos()
			}
		}
	}
	for name, refs := range funcRefs {
		if refs[name] {
			return nil, &PosError{Pos: funcPos[name], Msg: fmt.Sprintf("direct recursion: %q calls itself", name)}
		}
	}
	for a, aRefs := range funcRefs {
		for b := range aRefs {
			if bRefs, ok := funcRefs[b]; ok && bRefs[a] {
				return nil, &PosError{Pos: funcPos[a], Msg: fmt.Sprintf("mutual recursion between %q and %q", a, b)}
			}
		}
	}

	sv := &StructValue{Fields: fields}
	return &Value{Kind: KindStruct, Struct: sv}, nil
}

// topoSort performs Kahn's algorithm for topological ordering of bindings.
func topoSort(bindings []*ast.Binding, deps map[string][]string) ([]*ast.Binding, error) {
	byName := make(map[string]*ast.Binding, len(bindings))
	inDegree := make(map[string]int, len(bindings))
	for _, b := range bindings {
		byName[b.Name] = b
		inDegree[b.Name] = 0
	}
	for name, ds := range deps {
		if _, ok := byName[name]; ok {
			inDegree[name] = len(ds)
		}
	}

	var queue []string
	for _, b := range bindings {
		if inDegree[b.Name] == 0 {
			queue = append(queue, b.Name)
		}
	}

	// Reverse adjacency: dep → dependents
	dependents := make(map[string][]string)
	for name, ds := range deps {
		for _, d := range ds {
			dependents[d] = append(dependents[d], name)
		}
	}

	var order []*ast.Binding
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		order = append(order, byName[name])
		for _, dep := range dependents[name] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(order) < len(bindings) {
		for _, b := range bindings {
			if inDegree[b.Name] > 0 {
				return nil, &PosError{Pos: b.Position, Msg: fmt.Sprintf("circular dependency involving %q", b.Name)}
			}
		}
	}

	return order, nil
}

// evalExpr evaluates an expression, wrapping errors with source position (§11.2.0).
func (ev *Evaluator) evalExpr(expr ast.Expr, scope *Scope) (*Value, error) {
	v, err := ev.evalExprSwitch(expr, scope)
	if err != nil {
		var pe *PosError
		if !errors.As(err, &pe) {
			return nil, &PosError{Pos: expr.Pos(), Msg: err.Error()}
		}
	}
	return v, err
}

func (ev *Evaluator) evalExprSwitch(expr ast.Expr, scope *Scope) (*Value, error) {
	switch e := expr.(type) {
	case *ast.LiteralExpr:
		return ev.evalLiteral(e)
	case *ast.IdentExpr:
		return ev.evalIdent(e, scope)
	case *ast.UndefinedExpr:
		return Undefined(), nil
	case *ast.SelfExpr:
		return ev.evalSelf(scope), nil
	case *ast.EnvExpr:
		return ev.evalEnvObj(), nil
	case *ast.MemberExpr:
		return ev.evalMember(e, scope)
	case *ast.CallExpr:
		return ev.evalCall(e, scope)
	case *ast.BinaryExpr:
		return ev.evalBinary(e, scope)
	case *ast.UnaryExpr:
		return ev.evalUnary(e, scope)
	case *ast.IfExpr:
		return ev.evalIf(e, scope)
	case *ast.CaseExpr:
		return ev.evalCase(e, scope)
	case *ast.StructExpr:
		return ev.evalStruct(e, scope)
	case *ast.ListExpr:
		return ev.evalList(e, scope)
	case *ast.TupleExpr:
		return ev.evalTuple(e, scope)
	case *ast.AsExpr:
		return ev.evalAs(e, scope)
	case *ast.ToExpr:
		return ev.evalTo(e, scope)
	case *ast.WithExpr:
		return ev.evalWith(e, scope)
	case *ast.ExtendsExpr:
		return ev.evalExtends(e, scope)
	case *ast.FromExpr:
		return ev.evalFrom(e, scope)
	case *ast.UnionExpr:
		return ev.evalUnion(e, scope)
	case *ast.NamedExpr:
		return ev.evalNamed(e, scope)
	case *ast.IsNamedExpr:
		return ev.evalIsNamed(e, scope)
	case *ast.OfExpr:
		return ev.evalOf(e, scope)
	case *ast.StructImportExpr:
		return ev.evalStructImport(e)
	case *ast.FunctionExpr:
		return ev.evalFunctionDef(e, scope)
	case *ast.AreExpr:
		return ev.evalAre(e, scope)
	case *ast.InterpolatedStringExpr:
		return ev.evalInterpolatedString(e, scope)
	default:
		return nil, fmt.Errorf("unknown expression type %T", expr)
	}
}

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

// --- Member access and function calls ---

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
		return nil, fmt.Errorf("calling non-function value (%s)", fn.Kind)
	}

	fv := fn.Function
	fe, ok := fv.Body.(*ast.FunctionExpr)
	if !ok {
		return nil, fmt.Errorf("invalid function body")
	}

	// Evaluate arguments
	args := make([]*Value, 0, len(e.Args))
	for _, a := range e.Args {
		v, err := ev.evalExpr(a, scope)
		if err != nil {
			return nil, err
		}
		args = append(args, v)
	}

	// Fill default values for missing arguments
	for i := len(args); i < len(fe.Params); i++ {
		if fe.Params[i].Default != nil {
			dv, err := ev.evalExpr(fe.Params[i].Default, fv.Scope.(*Scope))
			if err != nil {
				return nil, err
			}
			args = append(args, dv)
		}
	}

	// Create function scope and bind parameters
	fnScope := newScope(fv.Scope.(*Scope))
	for i, p := range fe.Params {
		if i < len(args) {
			arg := args[i]
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
			// §6.3: nominal struct type check
			if p.TypeExpr != nil && arg.Kind == KindStruct {
				pti := ev.resolveTypeExpr(p.TypeExpr)
				if pti != nil && pti.Name != "" {
					if expectedFields, ok := ev.structShapes[pti.Name]; ok {
						if len(arg.Struct.Fields) != len(expectedFields) {
							return nil, fmt.Errorf("argument %q: expected %s (%d fields), got struct with %d fields",
								p.Name, pti.Name, len(expectedFields), len(arg.Struct.Fields))
						}
					}
				}
			}
			fnScope.set(p.Name, arg)
		}
	}

	// Evaluate intermediate bindings and body
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

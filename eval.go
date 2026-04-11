// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"errors"
	"fmt"
	"os"
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
// The exclude field implements the self-exclusion rule (§5.12):
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
		// Standalone env/undefined are errors
		switch b.Value.(type) {
		case *ast.EnvExpr:
			return nil, &PosError{Pos: b.Value.Pos(), Msg: "standalone env is not a value"}
		case *ast.UndefinedExpr:
			return nil, &PosError{Pos: b.Value.Pos(), Msg: "undefined cannot be used as a literal value"}
		}
		nameSet[b.Name] = true
		bindingByName[b.Name] = b
	}

	// Build dependency graph from identifier references
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

		// Bare identifier that was not resolved by "as"/"from" is undefined (§5.12)
		if v.Type != nil && v.Type.Name == "__ident__" {
			v = Undefined()
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
				refs := collectCallRefs(fe)
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


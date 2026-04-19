// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"

	"github.com/uzon-dev/uzon-go/ast"
	"github.com/uzon-dev/uzon-go/token"
)

// v0.10 features: variant shorthand (§3.7), enum variant type-context
// inference (§3.5 rule 4), and struct field defaults (§3.2).
//
// Type context propagates from outer annotations inward through:
//   - explicit `as Type`
//   - struct field with named-struct field type
//   - function argument and return type
//   - list element type via `as [Type]`
//
// At each context boundary, evalExprWithType is the entry point.

// evalExprWithType evaluates expr under an expected type ti. When ti supplies
// usable context, it short-circuits a few AST shapes:
//   - VariantShorthandExpr resolves against a tagged union ti
//   - bare IdentExpr resolves against an enum ti (rule 4 — bindings still win
//     via scope.get; only an unresolved name falls through to variant lookup)
//   - StructExpr fills field defaults and propagates field types as context
//   - ListExpr propagates the element type
//
// For other expressions (or when ti is nil), it defers to evalExpr.
func (ev *Evaluator) evalExprWithType(expr ast.Expr, scope *Scope, ti *TypeInfo) (*Value, error) {
	if ti == nil {
		return ev.evalExpr(expr, scope)
	}

	switch e := expr.(type) {
	case *ast.VariantShorthandExpr:
		return ev.evalVariantShorthand(e, scope, ti)

	case *ast.IdentExpr:
		if v, ok := scope.get(e.Name); ok {
			return v, nil
		}
		if v := ev.tryResolveEnumVariant(e.Name, ti, scope); v != nil {
			return v, nil
		}
		if v := ev.tryResolveNullaryShorthand(e.Name, ti); v != nil {
			return v, nil
		}
		return ev.evalExpr(expr, scope)

	case *ast.StructExpr:
		return ev.evalStructWithType(e, scope, ti)

	case *ast.ListExpr:
		if ti.ListElemType != nil {
			return ev.evalListWithType(e, scope, ti.ListElemType)
		}
		return ev.evalExpr(expr, scope)

	case *ast.AreExpr:
		if ti.ListElemType != nil {
			return ev.evalAreWithType(e, scope, ti.ListElemType)
		}
		return ev.evalExpr(expr, scope)

	case *ast.IfExpr:
		return ev.evalIfWithType(e, scope, ti)

	case *ast.CaseExpr:
		return ev.evalCaseWithType(e, scope, ti)

	case *ast.BinaryExpr:
		// or-else propagates type context to both sides so that
		// `bare_variant or else fallback_variant` resolves either branch.
		if e.Op == token.OrElse {
			left, err := ev.evalExprWithType(e.Left, scope, ti)
			if err == nil && left.Kind != KindUndefined && !isUnresolvedIdent(left) {
				return left, nil
			}
			return ev.evalExprWithType(e.Right, scope, ti)
		}

	case *ast.CallExpr:
		// `binary("Mizar", "Alcor")` parses as a call on `binary`. When ti is a
		// tagged union and `binary` is one of its variants — and not a function
		// in scope — re-interpret the call shape as variant shorthand carrying
		// either the lone arg or a tuple of args (§3.7).
		if v := ev.tryCallAsVariantShorthand(e, scope, ti); v != nil {
			return v, nil
		}
	}

	return ev.evalExpr(expr, scope)
}

// tryCallAsVariantShorthand returns nil unless the call shape matches a
// variant-shorthand-with-parens pattern (§3.7) given ti, in which case it
// evaluates and returns the resulting tagged-union value.
func (ev *Evaluator) tryCallAsVariantShorthand(e *ast.CallExpr, scope *Scope, ti *TypeInfo) *Value {
	id, ok := e.Func.(*ast.IdentExpr)
	if !ok {
		return nil
	}
	if _, bound := scope.get(id.Name); bound {
		return nil
	}
	variants, _ := ev.taggedVariantsForType(ti)
	if variants == nil {
		return nil
	}
	found := false
	for _, v := range variants {
		if v.Name == id.Name {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	var inner ast.Expr
	if len(e.Args) == 1 {
		inner = e.Args[0]
	} else {
		inner = &ast.TupleExpr{Elements: e.Args, Position: e.Position}
	}
	val, err := ev.evalVariantShorthand(&ast.VariantShorthandExpr{Name: id.Name, Inner: inner, Position: e.Position}, scope, ti)
	if err != nil {
		return nil
	}
	return val
}

// evalIfWithType mirrors evalIf (§5.9, §D.5) but propagates ti to both
// branches so bare variants or shorthand resolve, and still enforces the
// branch-type-compatibility check that evalIf performs.
func (ev *Evaluator) evalIfWithType(e *ast.IfExpr, scope *Scope, ti *TypeInfo) (*Value, error) {
	cond, err := ev.evalExpr(e.Cond, scope)
	if err != nil {
		return nil, err
	}
	if cond.Kind != KindBool {
		return nil, typeErrorf("if condition must be bool, got %s", cond.Kind)
	}
	if cond.Bool {
		thenVal, err := ev.evalExprWithType(e.Then, scope, ti)
		if err != nil {
			return nil, err
		}
		elseVal, elseErr := ev.evalExprWithType(e.Else, scope, ti)
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
	elseVal, err := ev.evalExprWithType(e.Else, scope, ti)
	if err != nil {
		return nil, err
	}
	thenVal, thenErr := ev.evalExprWithType(e.Then, scope, ti)
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

// evalCaseWithType evaluates a case expression and resolves a bare variant
// in the result against ti. Branch-by-branch type-context propagation is
// approximated by post-processing: after the matched branch yields, an
// unresolved IdentExpr value is mapped to the corresponding enum variant
// or null-named tagged union.
func (ev *Evaluator) evalCaseWithType(e *ast.CaseExpr, scope *Scope, ti *TypeInfo) (*Value, error) {
	v, err := ev.evalExpr(e, scope)
	if err != nil {
		return nil, err
	}
	return ev.resolveResultWithType(v, ti), nil
}

// resolveResultWithType maps an unresolved-ident or bare nullary value to a
// concrete enum/tagged-union value when ti supplies the missing context.
func (ev *Evaluator) resolveResultWithType(v *Value, ti *TypeInfo) *Value {
	if v == nil || ti == nil || ti.Name == "" {
		return v
	}
	if isUnresolvedIdent(v) {
		if r := ev.tryResolveEnumVariant(v.Str, ti, nil); r != nil {
			return r
		}
		if r := ev.tryResolveNullaryShorthand(v.Str, ti); r != nil {
			return r
		}
	}
	return v
}

// evalAsWithContext is the v0.10 fast path for `expr as Type`. It returns
// (value, handled=true, nil) when ti supplies usable type context for one of
// the AST shapes that benefits from it: variant shorthand, bare enum ident,
// named struct construction, or a list with a named-element type. Any other
// shape returns (nil, false, nil) so evalAs falls through to the legacy path.
func (ev *Evaluator) evalAsWithContext(e *ast.AsExpr, ti *TypeInfo, scope *Scope) (*Value, bool, error) {
	if ti == nil {
		return nil, false, nil
	}

	switch v := e.Value.(type) {
	case *ast.VariantShorthandExpr:
		if _, name := ev.taggedVariantsForType(ti); name != "" {
			res, err := ev.evalVariantShorthand(v, scope, ti)
			return res, err == nil, err
		}

	case *ast.IdentExpr:
		if ti.Name == "" {
			return nil, false, nil
		}
		if _, ok := scope.get(v.Name); ok {
			return nil, false, nil
		}
		if val := ev.tryResolveEnumVariant(v.Name, ti, scope); val != nil {
			return val, true, nil
		}
		if val := ev.tryResolveNullaryShorthand(v.Name, ti); val != nil {
			return val, true, nil
		}

	case *ast.StructExpr:
		if ti.Name == "" {
			return nil, false, nil
		}
		if _, ok := ev.structShapes.get(ti.Name); !ok {
			return nil, false, nil
		}
		val, err := ev.evalStructWithType(v, scope, ti)
		if err != nil {
			return nil, true, err
		}
		val.Type = ti
		val.Adoptable = false
		return val, true, nil

	case *ast.ListExpr:
		if e.TypeExpr.ListElem == nil {
			return nil, false, nil
		}
		elemTi := ev.resolveTypeExpr(e.TypeExpr.ListElem)
		val, err := ev.evalListWithType(v, scope, elemTi)
		if err != nil {
			return nil, true, err
		}
		val.Type = ti
		val.Adoptable = false
		return val, true, nil

	case *ast.AreExpr:
		if e.TypeExpr.ListElem == nil {
			return nil, false, nil
		}
		elemTi := ev.resolveTypeExpr(e.TypeExpr.ListElem)
		val, err := ev.evalAreWithType(v, scope, elemTi)
		if err != nil {
			return nil, true, err
		}
		val.Type = ti
		val.Adoptable = false
		return val, true, nil
	}

	return nil, false, nil
}

// evalVariantShorthand resolves `variant_name primary` (§3.7 v0.10). Without
// a tagged union ti, the shorthand has no type to attach to and is rejected.
func (ev *Evaluator) evalVariantShorthand(e *ast.VariantShorthandExpr, scope *Scope, ti *TypeInfo) (*Value, error) {
	variants, typeName := ev.taggedVariantsForType(ti)
	if variants == nil {
		return nil, fmt.Errorf("variant shorthand %q requires tagged union type context", e.Name)
	}

	var match *TaggedVariant
	for i := range variants {
		if variants[i].Name == e.Name {
			match = &variants[i]
			break
		}
	}
	if match == nil {
		return nil, fmt.Errorf("'%s' is not a variant of %s", e.Name, typeName)
	}

	inner, err := ev.evalExprWithType(e.Inner, scope, match.Type)
	if err != nil {
		return nil, err
	}
	if isUnresolvedIdent(inner) {
		return nil, fmt.Errorf("variant shorthand %q: unresolved inner value %q", e.Name, inner.Str)
	}
	if match.Type != nil && match.Type.BaseType == "null" && inner.Kind != KindNull {
		return nil, typeErrorf("variant shorthand %q: nullary variant cannot take inner value of kind %s", e.Name, inner.Kind)
	}
	if inner.Adoptable && match.Type != nil && match.Type.BaseType != "" {
		if inner.Kind == KindInt && isIntegerType(match.Type.BaseType) && match.Type.BitSize > 0 {
			if err := checkIntRange(inner.Int, match.Type.BitSize, match.Type.Signed); err != nil {
				return nil, fmt.Errorf("variant shorthand %q: %w", e.Name, err)
			}
		}
		inner.Type = match.Type
		inner.Adoptable = false
	} else if match.Type != nil && (inner.Type == nil || inner.Type.Name == "__ident__") {
		inner.Type = match.Type
	}

	result := &Value{
		Kind:        KindTaggedUnion,
		TaggedUnion: &TaggedUnionValue{Tag: e.Name, Inner: inner, Variants: variants},
	}
	if typeName != "" {
		result.Type = &TypeInfo{Name: typeName}
	}
	return result, nil
}

// tryResolveEnumVariant returns an enum value if name is a variant of ti.
func (ev *Evaluator) tryResolveEnumVariant(name string, ti *TypeInfo, scope *Scope) *Value {
	if ti == nil || ti.Name == "" {
		return nil
	}
	variants, ok := ev.enums.get(ti.Name)
	if !ok && scope != nil && len(ti.Path) > 1 {
		variants, ok = ev.resolveEnumFromPath(ti.Path, scope)
	}
	if !ok {
		return nil
	}
	for _, v := range variants {
		if v == name {
			return &Value{Kind: KindEnum, Enum: &EnumValue{Variant: v, Variants: variants}, Type: ti}
		}
	}
	return nil
}

// tryResolveNullaryShorthand handles bare `variant_name` (no inner) when ti is
// a tagged union and the named variant has inner type `null` (§3.7).
func (ev *Evaluator) tryResolveNullaryShorthand(name string, ti *TypeInfo) *Value {
	variants, typeName := ev.taggedVariantsForType(ti)
	if variants == nil {
		return nil
	}
	for _, v := range variants {
		if v.Name != name {
			continue
		}
		if v.Type == nil || v.Type.BaseType != "null" {
			return nil
		}
		result := &Value{
			Kind:        KindTaggedUnion,
			TaggedUnion: &TaggedUnionValue{Tag: name, Inner: Null(), Variants: variants},
		}
		if typeName != "" {
			result.Type = &TypeInfo{Name: typeName}
		}
		return result
	}
	return nil
}

// taggedVariantsForType returns the variant list for a tagged union type, or
// (nil, "") when ti is not a registered tagged union.
func (ev *Evaluator) taggedVariantsForType(ti *TypeInfo) ([]TaggedVariant, string) {
	if ti == nil || ti.Name == "" {
		return nil, ""
	}
	if variants, ok := ev.taggedVariants.get(ti.Name); ok {
		return variants, ti.Name
	}
	return nil, ""
}

// evalStructWithType constructs a struct of named type ti by walking the
// expected field list, evaluating each field's expression with that field's
// declared type as context, and filling in defaults for omitted fields.
func (ev *Evaluator) evalStructWithType(e *ast.StructExpr, scope *Scope, ti *TypeInfo) (*Value, error) {
	if ti == nil || ti.Name == "" {
		return ev.evalExpr(e, scope)
	}
	expected, ok := ev.structShapes.get(ti.Name)
	if !ok {
		return ev.evalExpr(e, scope)
	}

	provided := make(map[string]*ast.Binding, len(e.Fields))
	for _, b := range e.Fields {
		if _, dup := provided[b.Name]; dup {
			return nil, fmt.Errorf("duplicate field %q in struct literal", b.Name)
		}
		provided[b.Name] = b
	}
	for _, b := range e.Fields {
		matched := false
		for _, ef := range expected {
			if ef.Name == b.Name {
				matched = true
				break
			}
		}
		if !matched {
			return nil, typeErrorf("as %s: unknown field %q", ti.Name, b.Name)
		}
	}

	innerScope := newScope(scope)

	outerTypes := ev.types
	outerEnums := ev.enums
	outerTagged := ev.taggedVariants
	outerShapes := ev.structShapes
	ev.types = newTypeRegistry(outerTypes)
	ev.enums = newEnumRegistry(outerEnums)
	ev.taggedVariants = newTaggedVariantRegistry(outerTagged)
	ev.structShapes = newStructShapeRegistry(outerShapes)

	defer func() {
		ev.types = outerTypes
		ev.enums = outerEnums
		ev.taggedVariants = outerTagged
		ev.structShapes = outerShapes
	}()

	fields := make([]Field, 0, len(expected))
	for _, ef := range expected {
		var v *Value
		if b, ok := provided[ef.Name]; ok {
			fieldType := ev.fieldExpectedType(ef.Value)
			val, err := ev.evalExprWithType(b.Value, innerScope, fieldType)
			if err != nil {
				return nil, fmt.Errorf("as %s: field %q: %w", ti.Name, ef.Name, err)
			}
			val, err = ev.adoptFieldValue(ef.Value, val, ef.Name, ti.Name)
			if err != nil {
				return nil, err
			}
			v = val
		} else {
			v = cloneDefaultValue(ef.Value)
		}
		innerScope.set(ef.Name, v)
		fields = append(fields, Field{Name: ef.Name, Value: v})
	}

	return &Value{
		Kind:   KindStruct,
		Struct: &StructValue{Fields: fields},
		Type:   ti,
	}, nil
}

// evalListWithType evaluates each list element with elemTi as type context,
// then applies the element type to the resulting list.
func (ev *Evaluator) evalListWithType(e *ast.ListExpr, scope *Scope, elemTi *TypeInfo) (*Value, error) {
	elems := make([]*Value, 0, len(e.Elements))
	for _, el := range e.Elements {
		v, err := ev.evalListElemWithType(el, scope, elemTi)
		if err != nil {
			return nil, err
		}
		elems = append(elems, v)
	}
	return &Value{
		Kind: KindList,
		List: &ListValue{Elements: elems, ElementType: elemTi},
		Type: &TypeInfo{BaseType: "list", ListElemType: elemTi},
	}, nil
}

// evalAreWithType is the `are`-shorthand version of evalListWithType.
func (ev *Evaluator) evalAreWithType(e *ast.AreExpr, scope *Scope, elemTi *TypeInfo) (*Value, error) {
	elems := make([]*Value, 0, len(e.Elements))
	for _, el := range e.Elements {
		v, err := ev.evalListElemWithType(el, scope, elemTi)
		if err != nil {
			return nil, err
		}
		elems = append(elems, v)
	}
	return &Value{
		Kind: KindList,
		List: &ListValue{Elements: elems, ElementType: elemTi},
		Type: &TypeInfo{BaseType: "list", ListElemType: elemTi},
	}, nil
}

// evalListElemWithType evaluates one element under elemTi and enforces both
// adoption (untyped literal → element type) and nominal identity (a value
// already bound to a different named type cannot pose as elemTi).
func (ev *Evaluator) evalListElemWithType(el ast.Expr, scope *Scope, elemTi *TypeInfo) (*Value, error) {
	v, err := ev.evalExprWithType(el, scope, elemTi)
	if err != nil {
		return nil, err
	}
	if v.Adoptable && elemTi.BaseType != "" {
		if v.Kind == KindInt && isIntegerType(elemTi.BaseType) && elemTi.BitSize > 0 {
			if err := checkIntRange(v.Int, elemTi.BitSize, elemTi.Signed); err != nil {
				return nil, err
			}
		}
		v.Type = elemTi
		v.Adoptable = false
	} else if elemTi.Name != "" {
		if v.Type != nil && v.Type.Name != "" && v.Type.Name != "__ident__" && v.Type.Name != elemTi.Name {
			return nil, fmt.Errorf("list element type %s is not compatible with %s", v.Type.Name, elemTi.Name)
		}
		if v.Type == nil || v.Type.Name == "__ident__" {
			v.Type = elemTi
		}
	}
	return v, nil
}

// fieldExpectedType extracts the type context to use when evaluating a value
// for a field whose declared default is `def`. Named compound types (struct,
// enum, tagged union) propagate by name; primitive defaults propagate via
// their TypeInfo. Returns nil when no useful context can be derived.
func (ev *Evaluator) fieldExpectedType(def *Value) *TypeInfo {
	if def == nil {
		return nil
	}
	if def.Type != nil && def.Type.Name != "" && def.Type.Name != "__ident__" {
		return def.Type
	}
	switch def.Kind {
	case KindEnum:
		if def.Type != nil {
			return def.Type
		}
	case KindTaggedUnion:
		if def.Type != nil {
			return def.Type
		}
	case KindStruct:
		if def.Type != nil {
			return def.Type
		}
	case KindList:
		if def.List != nil && def.List.ElementType != nil {
			return &TypeInfo{BaseType: "list", ListElemType: def.List.ElementType}
		}
	}
	if def.Type != nil {
		return def.Type
	}
	return nil
}

// adoptFieldValue performs the per-field adoption checks that happen during
// `as TypeName` construction: untyped literals adopt the declared field type,
// shape compatibility is enforced, and named struct fields recurse via
// evalStructWithType (which handles defaults).
func (ev *Evaluator) adoptFieldValue(def *Value, val *Value, fieldName, typeName string) (*Value, error) {
	if val.Adoptable && def.Type != nil && def.Type.BaseType != "" {
		if val.Kind == KindInt && isIntegerType(def.Type.BaseType) && def.Type.BitSize > 0 {
			if err := checkIntRange(val.Int, def.Type.BitSize, def.Type.Signed); err != nil {
				return nil, fmt.Errorf("as %s: field %q: %w", typeName, fieldName, err)
			}
			val.Type = def.Type
			val.Adoptable = false
		} else if val.Kind == KindFloat && isFloatType(def.Type.BaseType) {
			val.Type = def.Type
			val.Adoptable = false
		}
	}
	if def.Type != nil && def.Type.BaseType != "" && (val.Type == nil || val.Type.Name == "__ident__") {
		val.Type = def.Type
	}
	if def.Kind == KindStruct && val.Kind == KindStruct && def.Type != nil && def.Type.Name != "" {
		if val.Type == nil || val.Type.Name == "" {
			adopted, err := ev.applyStructAs(val, def.Type)
			if err != nil {
				return nil, fmt.Errorf("as %s: field %q: %w", typeName, fieldName, err)
			}
			val = adopted
		}
	}
	if err := ev.checkWithTypeCompat(def, val, fieldName); err != nil {
		return nil, fmt.Errorf("as %s: %w", typeName, err)
	}
	return val, nil
}

// applyStructAs is the runtime equivalent of `val as ti.Name` when val is an
// already-built anonymous struct: it fills defaults, validates, and returns a
// new struct value tagged with ti.
func (ev *Evaluator) applyStructAs(val *Value, ti *TypeInfo) (*Value, error) {
	expected, ok := ev.structShapes.get(ti.Name)
	if !ok {
		return val, nil
	}
	provided := make(map[string]*Value, len(val.Struct.Fields))
	for _, f := range val.Struct.Fields {
		provided[f.Name] = f.Value
	}
	for _, f := range val.Struct.Fields {
		matched := false
		for _, ef := range expected {
			if ef.Name == f.Name {
				matched = true
				break
			}
		}
		if !matched {
			return nil, typeErrorf("unknown field %q for type %s", f.Name, ti.Name)
		}
	}
	fields := make([]Field, 0, len(expected))
	for _, ef := range expected {
		v, ok := provided[ef.Name]
		if !ok {
			v = cloneDefaultValue(ef.Value)
		} else {
			adopted, err := ev.adoptFieldValue(ef.Value, v, ef.Name, ti.Name)
			if err != nil {
				return nil, err
			}
			v = adopted
		}
		fields = append(fields, Field{Name: ef.Name, Value: v})
	}
	return &Value{
		Kind:   KindStruct,
		Struct: &StructValue{Fields: fields},
		Type:   ti,
	}, nil
}

// cloneDefaultValue produces an independent copy of a struct field's default,
// so per-construction mutation does not bleed back into the type declaration.
// Primitives and named types are shallow-copied; struct values clone their
// field list so adoption-time type tagging is local to each instance.
func cloneDefaultValue(v *Value) *Value {
	if v == nil {
		return nil
	}
	cp := *v
	if v.Struct != nil {
		fields := make([]Field, len(v.Struct.Fields))
		for i, f := range v.Struct.Fields {
			fields[i] = Field{Name: f.Name, Value: cloneDefaultValue(f.Value)}
		}
		cp.Struct = &StructValue{Fields: fields}
	}
	if v.List != nil {
		elems := make([]*Value, len(v.List.Elements))
		for i, el := range v.List.Elements {
			elems[i] = cloneDefaultValue(el)
		}
		cp.List = &ListValue{Elements: elems, ElementType: v.List.ElementType}
	}
	if v.Tuple != nil {
		elems := make([]*Value, len(v.Tuple.Elements))
		for i, el := range v.Tuple.Elements {
			elems[i] = cloneDefaultValue(el)
		}
		cp.Tuple = &TupleValue{Elements: elems}
	}
	return &cp
}

// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/uzon-dev/uzon-go/ast"
)

// --- Compound types ---

// structTypeScope holds type registrations made inside a struct, so they
// can be re-registered with a qualified prefix in the parent scope.
type structTypeScope struct {
	types   map[string]*TypeInfo
	enums   map[string][]string
	tagged  map[string][]TaggedVariant
	shapes  map[string][]Field
}

func (ev *Evaluator) evalStruct(e *ast.StructExpr, scope *Scope) (*Value, error) {
	innerScope := newScope(scope)

	// §6.2: types defined inside a struct are scoped to that struct.
	outerTypes := ev.types
	outerEnums := ev.enums
	outerTagged := ev.taggedVariants
	outerShapes := ev.structShapes
	ev.types = newTypeRegistry(outerTypes)
	ev.enums = newEnumRegistry(outerEnums)
	ev.taggedVariants = newTaggedVariantRegistry(outerTagged)
	ev.structShapes = newStructShapeRegistry(outerShapes)

	val, err := ev.evalBindings(e.Fields, innerScope)

	// Capture inner registries before restoring.
	if err == nil && val != nil {
		val.typeScope = &structTypeScope{
			types:  ev.types.types,
			enums:  ev.enums.enums,
			tagged: ev.taggedVariants.variants,
			shapes: ev.structShapes.shapes,
		}
	}

	ev.types = outerTypes
	ev.enums = outerEnums
	ev.taggedVariants = outerTagged
	ev.structShapes = outerShapes

	return val, err
}

func (ev *Evaluator) evalList(e *ast.ListExpr, scope *Scope) (*Value, error) {
	var elems []*Value
	for i, elem := range e.Elements {
		v, err := ev.evalExpr(elem, scope)
		if err != nil {
			return nil, err
		}
		// §3.4: undefined cannot be stored as a list element.
		if v.Kind == KindUndefined {
			return nil, fmt.Errorf("list element at index %d is undefined; use 'or else' to provide a fallback (§3.4)", i)
		}
		elems = append(elems, v)
	}
	// §3.4: a non-empty list of all-null elements has no inferable type and
	// requires an explicit `as [Type]` annotation.
	if len(elems) > 0 {
		allNull := true
		for _, el := range elems {
			if el.Kind != KindNull {
				allNull = false
				break
			}
		}
		if allNull {
			return nil, fmt.Errorf("list of only null requires type annotation (e.g. [null] as [string]) (§3.4)")
		}
	}
	// §3.4: list elements must be same type
	if len(elems) > 1 {
		hasIdent := false
		for _, el := range elems {
			if el.Type != nil && el.Type.Name == "__ident__" {
				hasIdent = true
				break
			}
		}
		if !hasIdent {
			var baseKind ValueKind
			for _, el := range elems {
				if el.Kind != KindNull {
					baseKind = el.Kind
					break
				}
			}
			if baseKind != 0 {
				// §3.4: integer-to-float promotion applies in lists —
				// `[1, 2.0]` is valid because untyped int literals adopt float.
				promotable := (baseKind == KindInt || baseKind == KindFloat)
				if promotable {
					for _, el := range elems {
						if el.Kind == KindNull {
							continue
						}
						if el.Kind != KindInt && el.Kind != KindFloat {
							promotable = false
							break
						}
						if el.Kind == KindInt && !el.Adoptable {
							promotable = false
							break
						}
					}
				}
				if !promotable {
					for i, el := range elems {
						if el.Kind != baseKind && el.Kind != KindNull {
							return nil, fmt.Errorf("list elements must be same type: got %s and %s at index %d", baseKind, el.Kind, i)
						}
					}
				}
				// §3.4: numeric elements with explicit (non-adoptable) type
				// annotations must agree on width/signedness.
				if baseKind == KindInt || baseKind == KindFloat {
					var refTi *TypeInfo
					for _, el := range elems {
						if el.Type != nil && el.Type.BaseType != "" && !el.Adoptable {
							refTi = el.Type
							break
						}
					}
					if refTi != nil {
						for i, el := range elems {
							if el.Type == nil || el.Type.BaseType == "" || el.Adoptable {
								continue
							}
							if el.Type.BaseType != refTi.BaseType {
								return nil, typeErrorf("list elements must be same type: %s and %s at index %d", refTi.BaseType, el.Type.BaseType, i)
							}
						}
					}
				}
				// §3.4: struct elements must have compatible shapes and nominal types
				if baseKind == KindStruct {
					if err := checkListStructHomogeneity(elems); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return NewList(elems, nil), nil
}

// checkListStructHomogeneity validates that all struct elements in a list
// have compatible shapes: same field count, same field names, same value
// types, and same nominal type (§3.4).
func checkListStructHomogeneity(elems []*Value) error {
	// Find first non-null struct as reference
	var ref *Value
	for _, el := range elems {
		if el.Kind == KindStruct {
			ref = el
			break
		}
	}
	if ref == nil {
		return nil
	}
	refName := ""
	if ref.Type != nil {
		refName = ref.Type.Name
	}

	for i, el := range elems {
		if el.Kind != KindStruct {
			continue
		}
		if el == ref {
			continue
		}
		// Nominal type check
		elName := ""
		if el.Type != nil {
			elName = el.Type.Name
		}
		if refName != elName {
			return typeErrorf("list struct elements have different named types: %q vs %q at index %d", refName, elName, i)
		}
		// Field count check
		if len(el.Struct.Fields) != len(ref.Struct.Fields) {
			return typeErrorf("list struct elements have different field counts at index %d", i)
		}
		// Field names and value types check (order-independent)
		for _, rf := range ref.Struct.Fields {
			ev := el.Struct.Get(rf.Name)
			if ev == nil {
				return typeErrorf("list struct elements have different field names at index %d", i)
			}
			if rf.Value.Kind != ev.Kind && rf.Value.Kind != KindNull && ev.Kind != KindNull {
				return typeErrorf("list struct field %q has different types at index %d", rf.Name, i)
			}
		}
	}
	return nil
}

func (ev *Evaluator) evalTuple(e *ast.TupleExpr, scope *Scope) (*Value, error) {
	var elems []*Value
	for _, elem := range e.Elements {
		v, err := ev.evalExpr(elem, scope)
		if err != nil {
			return nil, err
		}
		elems = append(elems, v)
	}
	return NewTuple(elems...), nil
}

// --- Type operations ---

// evalAs implements "as Type" annotation (§6.1).
func (ev *Evaluator) evalAs(e *ast.AsExpr, scope *Scope) (*Value, error) {
	ti := ev.resolveTypeExpr(e.TypeExpr)

	// v0.10: type-context-aware evaluation for shapes that benefit from it.
	if ctxVal, handled, err := ev.evalAsWithContext(e, ti, scope); err != nil {
		return nil, err
	} else if handled {
		return ctxVal, nil
	}

	val, err := ev.evalExpr(e.Value, scope)
	if err != nil {
		return nil, err
	}

	// §6.1 R6: `null as T` is a type error unless T admits null.
	// T admits null when it IS null, contains null in a union, or is a
	// tagged union with a null-inner variant.
	if val.Kind == KindNull && !ev.typeAdmitsNull(e.TypeExpr, ti, scope) {
		return nil, typeErrorf("null cannot be cast to %s", typeExprName(e.TypeExpr, ti))
	}

	// §6.1: undefined propagates through "as", but type name MUST still be validated
	if val.Kind == KindUndefined {
		if ti.Name != "" && ti.BaseType == "" {
			// Named type — check it exists in the type registry or as a builtin
			if _, ok := ev.types.get(e.TypeExpr.Path); !ok {
				if parseBuiltinType(ti.Name) == nil {
					return nil, typeErrorf("unknown type %q", ti.Name)
				}
			}
		}
		return Undefined(), nil
	}

	// Enum variant resolution: bare ident "as EnumType" → variant lookup
	identName := ""
	if val.Type != nil && val.Type.Name == "__ident__" {
		identName = val.Str
	} else if ie, ok := e.Value.(*ast.IdentExpr); ok {
		identName = ie.Name
	}
	if identName != "" && ti.Name != "" {
		variants, ok := ev.enums.get(ti.Name)
		if !ok && len(e.TypeExpr.Path) > 1 {
			variants, ok = ev.resolveEnumFromPath(e.TypeExpr.Path, scope)
		}
		if ok {
			for _, v := range variants {
				if v == identName {
					return &Value{Kind: KindEnum, Enum: &EnumValue{Variant: identName, Variants: variants}, Type: ti}, nil
				}
			}
			if val.Type != nil && val.Type.Name == "__ident__" {
				return nil, fmt.Errorf("'%s' is not a variant of %s", identName, ti.Name)
			}
		}
	}

	// §3.2.1 rule 5 / §6.2: nominal identity for enums — an enum value
	// already belonging to a named enum type cannot be annotated as a
	// different named enum, even if variants overlap structurally.
	if val.Kind == KindEnum && ti.Name != "" && val.Type != nil && val.Type.Name != "" && val.Type.Name != ti.Name {
		if _, ok := ev.enums.get(ti.Name); ok {
			return nil, typeErrorf("cannot annotate enum of named type %q as %q (nominal identity)", val.Type.Name, ti.Name)
		}
	}

	// §6.3: struct shape and type compatibility
	if val.Kind == KindStruct && ti.Name != "" {
		// §3.2.1 rule 5 / §6.2: nominal identity. If the value already has
		// a different named type, the assertion is a type error.
		if val.Type != nil && val.Type.Name != "" && val.Type.Name != ti.Name {
			return nil, typeErrorf("cannot annotate value of named type %q as %q (nominal identity)", val.Type.Name, ti.Name)
		}
		if expectedFields, ok := ev.structShapes.get(ti.Name); ok {
			if len(val.Struct.Fields) != len(expectedFields) {
				return nil, fmt.Errorf("cannot cast struct to %s: different shape (%d fields vs %d)",
					ti.Name, len(val.Struct.Fields), len(expectedFields))
			}
			for _, ef := range expectedFields {
				actual := val.Struct.Get(ef.Name)
				if actual == nil {
					return nil, fmt.Errorf("cannot cast struct to %s: missing field %q", ti.Name, ef.Name)
				}
				// §6.3: untyped literals adopt the expected field type and are range-checked.
				if actual.Adoptable && ef.Value.Type != nil && ef.Value.Type.BaseType != "" {
					if actual.Kind == KindInt && isIntegerType(ef.Value.Type.BaseType) {
						if err := checkIntRange(actual.Int, ef.Value.Type.BitSize, ef.Value.Type.Signed); err != nil {
							return nil, fmt.Errorf("as %s: field %q: %w", ti.Name, ef.Name, err)
						}
						actual.Type = ef.Value.Type
						actual.Adoptable = false
					} else if actual.Kind == KindFloat && isFloatType(ef.Value.Type.BaseType) {
						actual.Type = ef.Value.Type
						actual.Adoptable = false
					}
				}
				if err := ev.checkWithTypeCompat(ef.Value, actual, ef.Name); err != nil {
					return nil, fmt.Errorf("as %s: %w", ti.Name, err)
				}
			}
			// Adopt the named type on the value.
			val.Type = ti
		}
	}

	// List type annotation: apply element type
	if val.Kind == KindList && e.TypeExpr.ListElem != nil {
		elemTi := ev.resolveTypeExpr(e.TypeExpr.ListElem)
		if elemTi.Name != "" {
			// §6.2: element type name must be resolvable.
			// Builtins are accepted via parseBuiltinType (BaseType is set).
			if elemTi.BaseType == "" {
				if _, ok := ev.types.get(e.TypeExpr.ListElem.Path); !ok {
					if _, ok := ev.enums.get(elemTi.Name); !ok {
						if _, ok := ev.taggedVariants.get(elemTi.Name); !ok {
							if parseBuiltinType(elemTi.Name) == nil {
								return nil, typeErrorf("unknown type %q", elemTi.Name)
							}
						}
					}
				}
			}
			variants, enumOK := ev.enums.get(elemTi.Name)
			if !enumOK && len(e.TypeExpr.ListElem.Path) > 1 {
				variants, enumOK = ev.resolveEnumFromPath(e.TypeExpr.ListElem.Path, scope)
			}
			if enumOK {
				var astElems []ast.Expr
				if le, ok := e.Value.(*ast.ListExpr); ok {
					astElems = le.Elements
				}
				for i, el := range val.List.Elements {
					identName := ""
					if el.Type != nil && el.Type.Name == "__ident__" {
						identName = el.Str
					} else if i < len(astElems) {
						if ie, ok := astElems[i].(*ast.IdentExpr); ok {
							identName = ie.Name
						}
					}
					if identName != "" {
						found := false
						for _, v := range variants {
							if v == identName {
								val.List.Elements[i] = &Value{Kind: KindEnum, Enum: &EnumValue{Variant: identName, Variants: variants}, Type: elemTi}
								found = true
								break
							}
						}
						if !found {
							return nil, fmt.Errorf("'%s' is not a variant of %s", identName, elemTi.Name)
						}
					} else {
						el.Type = elemTi
					}
				}
			} else {
				for _, el := range val.List.Elements {
					if el.Type != nil && el.Type.Name != "" && el.Type.Name != "__ident__" && el.Type.Name != elemTi.Name {
						return nil, fmt.Errorf("list element type %s is not compatible with %s", el.Type.Name, elemTi.Name)
					}
					el.Type = elemTi
				}
			}
			val.List.ElementType = elemTi
			val.Type = ti
			val.Adoptable = false
			return val, nil
		}
		// Builtin element type (e.g. [] as [i64])
		val.List.ElementType = elemTi
		for _, el := range val.List.Elements {
			el.Type = elemTi
		}
		val.Type = ti
		val.Adoptable = false
		return val, nil
	}

	// §6.1: cross-category annotation (e.g. float as integer, string as integer)
	// is a type error — use `to` for conversion. Allow null and unresolved idents.
	if ti.BaseType != "" && val.Kind != KindNull && !isUnresolvedIdent(val) {
		isIntT := isIntegerType(ti.BaseType)
		isFloatT := isFloatType(ti.BaseType)
		isBoolT := ti.BaseType == "bool"
		isStringT := ti.BaseType == "string"
		switch {
		case isIntT && val.Kind != KindInt:
			return nil, typeErrorf("cannot annotate %s as %s; use 'to %s' for conversion", val.Kind, ti.BaseType, ti.BaseType)
		case isFloatT && val.Kind != KindFloat && val.Kind != KindInt:
			return nil, typeErrorf("cannot annotate %s as %s; use 'to %s' for conversion", val.Kind, ti.BaseType, ti.BaseType)
		case isBoolT && val.Kind != KindBool:
			return nil, typeErrorf("cannot annotate %s as bool", val.Kind)
		case isStringT && val.Kind != KindString:
			return nil, typeErrorf("cannot annotate %s as string; use 'to string' for conversion", val.Kind)
		}
	}

	// Numeric range validation for "as" annotation
	if val.Kind == KindInt && isIntegerType(ti.BaseType) {
		if err := checkIntRange(val.Int, ti.BitSize, ti.Signed); err != nil {
			return nil, fmt.Errorf("as %s: %w", ti.BaseType, err)
		}
	}

	// §3.3 tuple shape and element-type validation: when annotating with a
	// tuple type `(T1, T2, ...)`, the value must be a tuple of matching
	// arity, and each element must be compatible with the declared type.
	if ti.BaseType == "tuple" && e.TypeExpr.TupleElems != nil {
		if val.Kind != KindTuple {
			return nil, typeErrorf("cannot annotate %s as tuple", val.Kind)
		}
		if len(val.Tuple.Elements) != len(ti.TupleElemTypes) {
			return nil, typeErrorf("tuple arity mismatch: value has %d element(s), type %s has %d",
				len(val.Tuple.Elements), typeInfoString(ti), len(ti.TupleElemTypes))
		}
		for i, expectedTi := range ti.TupleElemTypes {
			el := val.Tuple.Elements[i]
			if err := ev.checkValueAgainstType(el, expectedTi, fmt.Sprintf("tuple element %d", i)); err != nil {
				return nil, err
			}
		}
	}

	// §6.2: named type must be resolvable — reject unknown type names
	if ti.Name != "" && ti.BaseType == "" {
		if _, ok := ev.types.get(e.TypeExpr.Path); !ok {
			if parseBuiltinType(ti.Name) == nil {
				return nil, typeErrorf("unknown type %q", ti.Name)
			}
		}
	}

	// §6.3 R5/R7: when annotating against a named union, verify the value
	// is compatible with at least one member's category (e.g. a float
	// literal cannot adopt an integer-only union). The adoptable inner
	// adopts the first matching member type so subsequent narrowing
	// (`is type`, `case type`) can identify the underlying member.
	// First pass: exact category match. Second pass: integer→float promotion.
	if ti.Name != "" && len(e.TypeExpr.Path) >= 1 {
		if bv, ok := scope.get(e.TypeExpr.Path[0]); ok && bv.Kind == KindUnion && bv.Union != nil {
			var matchedType *TypeInfo
			for _, mt := range bv.Union.MemberTypes {
				if unionMemberCompatible(val, mt) {
					matchedType = mt
					break
				}
			}
			promoted := false
			if matchedType == nil && val.Kind == KindInt && val.Adoptable {
				for _, mt := range bv.Union.MemberTypes {
					if mt != nil && isFloatType(mt.BaseType) {
						matchedType = mt
						promoted = true
						break
					}
				}
			}
			if matchedType == nil {
				return nil, typeErrorf("value of kind %s is not compatible with any member of union %s", val.Kind, ti.Name)
			}
			var inner *Value
			if promoted {
				f := new(big.Float).SetInt(val.Int)
				inner = &Value{Kind: KindFloat, Float: f, Type: matchedType}
			} else if val.Adoptable || val.Type == nil {
				inner = &Value{Kind: val.Kind, Int: val.Int, Float: val.Float, Str: val.Str, Bool: val.Bool, Type: matchedType}
			} else {
				inner = val
			}
			result := &Value{
				Kind:  KindUnion,
				Union: &UnionValue{Inner: inner, MemberTypes: bv.Union.MemberTypes},
				Type:  ti,
			}
			return result, nil
		}
	}

	val.Type = ti
	val.Adoptable = false
	return val, nil
}

// --- Struct operations ---

// evalWith implements "with { overrides }" (§3.2.1).
func (ev *Evaluator) evalWith(e *ast.WithExpr, scope *Scope) (*Value, error) {
	base, err := ev.evalExpr(e.Base, scope)
	if err != nil {
		return nil, err
	}
	if base.Kind == KindUndefined {
		return nil, fmt.Errorf("with: base is undefined")
	}
	if base.Kind != KindStruct {
		return nil, typeErrorf("with: base must be a struct, got %s", base.Kind)
	}

	newFields := make([]Field, len(base.Struct.Fields))
	copy(newFields, base.Struct.Fields)

	for _, ob := range e.Override.Fields {
		v, err := ev.evalExpr(ob.Value, scope)
		if err != nil {
			return nil, err
		}
		// §3.2.1: override evaluating to undefined is a runtime error
		if v.Kind == KindUndefined || isUnresolvedIdent(v) {
			return nil, fmt.Errorf("with: override field %q evaluates to undefined", ob.Name)
		}
		found := false
		for i, f := range newFields {
			if f.Name == ob.Name {
				// §3.2.1: adoption (rule 2) and compat (rules 1/4/5) are
				// performed by checkWithTypeCompat, including recursion
				// into nested structs, lists, and tuples.
				if err := ev.checkWithTypeCompat(f.Value, v, ob.Name); err != nil {
					return nil, fmt.Errorf("with: %w", err)
				}
				if f.Value.Type != nil && f.Value.Type.BaseType != "" && (v.Type == nil || v.Type.Name == "__ident__") {
					v.Type = f.Value.Type
				}
				newFields[i].Value = v
				found = true
				break
			}
		}
		if !found {
			return nil, typeErrorf("with: unknown field %q", ob.Name)
		}
	}

	return &Value{Kind: KindStruct, Struct: &StructValue{Fields: newFields}, Type: base.Type}, nil
}

// checkWithTypeCompat validates type compatibility for with/plus overrides
// (§3.2.1). It also performs §3.2.1 rule 2 untyped-literal adoption in
// place — when override is an untyped literal and original carries a typed
// primitive, override adopts that type and is range-checked.
func (ev *Evaluator) checkWithTypeCompat(original, override *Value, fieldName string) error {
	if override.Kind == KindNull || original.Kind == KindNull {
		return nil
	}
	// §3.2.1 rule 2: untyped literals adopt the existing field type and are range-checked.
	if err := adoptUntypedLiteral(original, override, fieldName); err != nil {
		return err
	}
	// §3.2.1 rule 5: named types must match by Name.
	origName := namedStructTypeName(original.Type)
	overName := namedStructTypeName(override.Type)
	if origName != "" && overName != "" && origName != overName {
		return typeErrorf("field %q: named type %s incompatible with %s", fieldName, overName, origName)
	}
	// §3.2.1 rule 1: primitive types must match exactly.
	if original.Type != nil && original.Type.BaseType != "" && original.Type.Name != "__ident__" {
		if override.Type != nil && override.Type.BaseType != "" && override.Type.Name != "__ident__" {
			if original.Type.BaseType != override.Type.BaseType {
				return typeErrorf("field %q: type %s incompatible with %s", fieldName, override.Type.BaseType, original.Type.BaseType)
			}
		}
	}
	if original.Kind != override.Kind {
		return typeErrorf("field %q: cannot override %s with %s", fieldName, original.Kind, override.Kind)
	}
	// §3.2.1 rule 4 / "no implicit deep merge": when overriding a nested
	// struct, the replacement must have the same field set and field types.
	// Field order does not matter; extra or missing fields are errors.
	if original.Kind == KindStruct {
		if len(original.Struct.Fields) != len(override.Struct.Fields) {
			return typeErrorf("field %q: struct shape mismatch (%d fields vs %d)",
				fieldName, len(override.Struct.Fields), len(original.Struct.Fields))
		}
		for _, of := range override.Struct.Fields {
			ov := original.Struct.Get(of.Name)
			if ov == nil {
				return typeErrorf("field %q: unknown subfield %q", fieldName, of.Name)
			}
			if err := ev.checkWithTypeCompat(ov, of.Value, fieldName+"."+of.Name); err != nil {
				return err
			}
		}
	}
	// §3.2.1 rule 1 + §3.4: list type is `[T]`; overrides must keep the
	// same element type. Length may change (lists are variable-length),
	// but each element must satisfy compat against the original's
	// element type.
	if original.Kind == KindList {
		exemplar, exemplarType := listElementExemplar(original)
		for i, oe := range override.List.Elements {
			elemName := fmt.Sprintf("%s[%d]", fieldName, i)
			if exemplar != nil {
				if err := ev.checkWithTypeCompat(exemplar, oe, elemName); err != nil {
					return err
				}
			} else if exemplarType != nil {
				synth := syntheticTypedValue(exemplarType)
				if synth != nil {
					if err := ev.checkWithTypeCompat(synth, oe, elemName); err != nil {
						return err
					}
				}
			}
		}
	}
	// §3.2.1 rule 1 + §3.3: tuples have fixed arity and per-position types.
	if original.Kind == KindTuple {
		if len(original.Tuple.Elements) != len(override.Tuple.Elements) {
			return typeErrorf("field %q: tuple arity mismatch (%d vs %d)",
				fieldName, len(override.Tuple.Elements), len(original.Tuple.Elements))
		}
		for i, oe := range override.Tuple.Elements {
			if err := ev.checkWithTypeCompat(original.Tuple.Elements[i], oe, fmt.Sprintf("%s(%d)", fieldName, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// adoptUntypedLiteral mutates override in place so an untyped int/float
// literal adopts original's primitive type (with int range-check).
func adoptUntypedLiteral(original, override *Value, fieldName string) error {
	if !override.Adoptable || original.Type == nil || original.Type.BaseType == "" {
		return nil
	}
	switch {
	case override.Kind == KindInt && isIntegerType(original.Type.BaseType):
		if err := checkIntRange(override.Int, original.Type.BitSize, original.Type.Signed); err != nil {
			return fmt.Errorf("field %q: %w", fieldName, err)
		}
		override.Type = original.Type
		override.Adoptable = false
	case override.Kind == KindFloat && isFloatType(original.Type.BaseType):
		override.Type = original.Type
		override.Adoptable = false
	}
	return nil
}

// listElementExemplar returns a representative element value (used to
// recurse into shape/type checks) and/or an explicit element type.
func listElementExemplar(list *Value) (*Value, *TypeInfo) {
	var exemplarType *TypeInfo
	if list.List.ElementType != nil {
		exemplarType = list.List.ElementType
	} else if list.Type != nil && list.Type.ListElemType != nil {
		exemplarType = list.Type.ListElemType
	}
	if len(list.List.Elements) > 0 {
		return list.List.Elements[0], exemplarType
	}
	return nil, exemplarType
}

// syntheticTypedValue builds a placeholder Value carrying ti so that
// list-element compat checks have something to compare against when the
// original list is empty.
func syntheticTypedValue(ti *TypeInfo) *Value {
	if ti == nil || ti.BaseType == "" {
		return nil
	}
	switch {
	case isIntegerType(ti.BaseType):
		return &Value{Kind: KindInt, Type: ti}
	case isFloatType(ti.BaseType):
		return &Value{Kind: KindFloat, Type: ti}
	case ti.BaseType == "string":
		return &Value{Kind: KindString, Type: ti}
	case ti.BaseType == "bool":
		return &Value{Kind: KindBool, Type: ti}
	}
	return nil
}

// typeInfoString renders a TypeInfo for diagnostics (handles tuples,
// lists, and named/builtin scalars).
func typeInfoString(ti *TypeInfo) string {
	if ti == nil {
		return "<unknown>"
	}
	if ti.BaseType == "tuple" {
		s := "("
		for i, et := range ti.TupleElemTypes {
			if i > 0 {
				s += ", "
			}
			s += typeInfoString(et)
		}
		return s + ")"
	}
	if ti.BaseType == "list" || ti.ListElemType != nil {
		return "[" + typeInfoString(ti.ListElemType) + "]"
	}
	if ti.Name != "" && ti.Name != "__ident__" {
		return ti.Name
	}
	if ti.BaseType != "" {
		return ti.BaseType
	}
	return "<unknown>"
}

// checkValueAgainstType validates that v conforms to expected and adopts
// untyped literal types in place. Used for §3.3 tuple element validation
// during `as TupleType`. Recurses into nested tuples and lists.
func (ev *Evaluator) checkValueAgainstType(v *Value, expected *TypeInfo, ctx string) error {
	if expected == nil {
		return nil
	}
	if v.Kind == KindNull {
		if expected.BaseType == "null" {
			return nil
		}
		return typeErrorf("%s: null is not compatible with %s", ctx, typeInfoString(expected))
	}
	switch {
	case isIntegerType(expected.BaseType):
		if v.Kind != KindInt {
			return typeErrorf("%s: cannot use %s as %s", ctx, v.Kind, expected.BaseType)
		}
		if err := checkIntRange(v.Int, expected.BitSize, expected.Signed); err != nil {
			return fmt.Errorf("%s: %w", ctx, err)
		}
		if v.Adoptable {
			v.Type = expected
			v.Adoptable = false
		} else if v.Type != nil && v.Type.BaseType != expected.BaseType {
			return typeErrorf("%s: type %s incompatible with %s", ctx, v.Type.BaseType, expected.BaseType)
		}
		return nil
	case isFloatType(expected.BaseType):
		if v.Kind != KindFloat && v.Kind != KindInt {
			return typeErrorf("%s: cannot use %s as %s", ctx, v.Kind, expected.BaseType)
		}
		if v.Adoptable {
			v.Type = expected
			v.Adoptable = false
		}
		return nil
	case expected.BaseType == "bool":
		if v.Kind != KindBool {
			return typeErrorf("%s: cannot use %s as bool", ctx, v.Kind)
		}
		return nil
	case expected.BaseType == "string":
		if v.Kind != KindString {
			return typeErrorf("%s: cannot use %s as string", ctx, v.Kind)
		}
		return nil
	case expected.BaseType == "tuple":
		if v.Kind != KindTuple {
			return typeErrorf("%s: cannot use %s as tuple", ctx, v.Kind)
		}
		if len(v.Tuple.Elements) != len(expected.TupleElemTypes) {
			return typeErrorf("%s: tuple arity mismatch: %d vs %d",
				ctx, len(v.Tuple.Elements), len(expected.TupleElemTypes))
		}
		for i, sub := range expected.TupleElemTypes {
			if err := ev.checkValueAgainstType(v.Tuple.Elements[i], sub, fmt.Sprintf("%s[%d]", ctx, i)); err != nil {
				return err
			}
		}
		return nil
	case expected.BaseType == "list" || expected.ListElemType != nil:
		if v.Kind != KindList {
			return typeErrorf("%s: cannot use %s as list", ctx, v.Kind)
		}
		if expected.ListElemType != nil {
			for i, el := range v.List.Elements {
				if err := ev.checkValueAgainstType(el, expected.ListElemType, fmt.Sprintf("%s[%d]", ctx, i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if expected.Name != "" && expected.Name != "__ident__" {
		if v.Type == nil || v.Type.Name == "" || v.Type.Name == "__ident__" {
			return nil
		}
		if v.Type.Name != expected.Name {
			return typeErrorf("%s: named type %s incompatible with %s", ctx, v.Type.Name, expected.Name)
		}
	}
	return nil
}

// namedStructTypeName returns the user-facing nominal type name, or "" if
// the type is anonymous, a placeholder, or absent.
func namedStructTypeName(t *TypeInfo) string {
	if t == nil || t.Name == "" || t.Name == "__ident__" {
		return ""
	}
	return t.Name
}

// evalPlus implements "plus { additions }" (§3.2.2).
func (ev *Evaluator) evalPlus(e *ast.PlusExpr, scope *Scope) (*Value, error) {
	base, err := ev.evalExpr(e.Base, scope)
	if err != nil {
		return nil, err
	}
	if base.Kind == KindUndefined {
		return nil, fmt.Errorf("plus: base is undefined")
	}
	if base.Kind != KindStruct {
		return nil, typeErrorf("plus: base must be a struct, got %s", base.Kind)
	}

	newFields := make([]Field, len(base.Struct.Fields))
	copy(newFields, base.Struct.Fields)

	hasNew := false
	for _, ob := range e.Extension.Fields {
		v, err := ev.evalExpr(ob.Value, scope)
		if err != nil {
			return nil, err
		}
		// §3.2.2: field evaluating to undefined is a runtime error (both override and new)
		if v.Kind == KindUndefined || isUnresolvedIdent(v) {
			return nil, fmt.Errorf("plus: field %q evaluates to undefined", ob.Name)
		}
		found := false
		for i, f := range newFields {
			if f.Name == ob.Name {
				if err := ev.checkWithTypeCompat(f.Value, v, ob.Name); err != nil {
					return nil, fmt.Errorf("plus: %w", err)
				}
				if f.Value.Type != nil && f.Value.Type.BaseType != "" && (v.Type == nil || v.Type.Name == "__ident__") {
					v.Type = f.Value.Type
				}
				newFields[i].Value = v
				found = true
				break
			}
		}
		if !found {
			hasNew = true
			newFields = append(newFields, Field{Name: ob.Name, Value: v})
		}
	}

	if !hasNew {
		return nil, typeErrorf("plus: must add at least one new field")
	}

	// §3.2.2: plus always produces a new type — named type is NOT inherited
	return &Value{Kind: KindStruct, Struct: &StructValue{Fields: newFields}}, nil
}

// --- Enum, union, tagged union ---

func (ev *Evaluator) evalFrom(e *ast.FromExpr, scope *Scope) (*Value, error) {
	if len(e.Variants) < 2 {
		return nil, fmt.Errorf("enum must have at least 2 variants, got %d", len(e.Variants))
	}
	// §3.5/§9: duplicate variant names are a type error.
	seen := make(map[string]bool, len(e.Variants))
	for _, v := range e.Variants {
		if seen[v] {
			return nil, fmt.Errorf("duplicate variant %q in enum", v)
		}
		seen[v] = true
	}
	// §3.5 explicit variant position: the identifier immediately before `from`
	// resolves as a variant of the just-declared enum if it matches, even if a
	// same-name binding is in scope. This is structurally committed (a binding
	// reference would select a non-variant value), so variants win here just
	// like inside the variant list.
	var variant string
	if ie, ok := e.Value.(*ast.IdentExpr); ok && seen[ie.Name] {
		variant = ie.Name
	} else {
		val, err := ev.evalExpr(e.Value, scope)
		if err != nil {
			return nil, err
		}
		if val.Type != nil && val.Type.Name == "__ident__" {
			variant = val.Str
		} else if val.Kind == KindString {
			variant = val.Str
		}
	}
	// §3.5: selected variant must be in the variant list
	found := false
	for _, v := range e.Variants {
		if v == variant {
			found = true
			break
		}
	}
	if !found {
		return nil, typeErrorf("'%s' is not a variant of the enum", variant)
	}
	return &Value{Kind: KindEnum, Enum: &EnumValue{Variant: variant, Variants: e.Variants}}, nil
}

func (ev *Evaluator) evalUnion(e *ast.UnionExpr, scope *Scope) (*Value, error) {
	if len(e.MemberTypes) < 2 {
		return nil, typeErrorf("union must have at least 2 member types, got %d", len(e.MemberTypes))
	}
	val, err := ev.evalExpr(e.Value, scope)
	if err != nil {
		return nil, err
	}
	var memberTypes []*TypeInfo
	seen := make(map[string]bool, len(e.MemberTypes))
	for _, t := range e.MemberTypes {
		ti := ev.resolveTypeExpr(t)
		if ti != nil {
			key := ti.TypeKey()
			if key != "" {
				if seen[key] {
					return nil, typeErrorf("duplicate type %q in union", key)
				}
				seen[key] = true
			}
		}
		memberTypes = append(memberTypes, ti)
	}
	// Inner values adopt the first compatible member type.
	adopted := false
	for _, mt := range memberTypes {
		if unionMemberCompatible(val, mt) {
			if val.Adoptable {
				val.Type = mt
				val.Adoptable = false
			}
			// §v0.8: propagate compound type info for list/tuple
			if val.Kind == KindList && mt.ListElemType != nil {
				val.List.ElementType = mt.ListElemType
				for _, el := range val.List.Elements {
					if el.Adoptable {
						el.Type = mt.ListElemType
						el.Adoptable = false
					}
				}
			}
			adopted = true
			break
		}
	}
	_ = adopted
	return &Value{Kind: KindUnion, Union: &UnionValue{Inner: val, MemberTypes: memberTypes}}, nil
}

// unionMemberCompatible checks if a value is kind-compatible with a
// union member type (integer→integer type, float→float type, etc.).
func unionMemberCompatible(val *Value, mt *TypeInfo) bool {
	if mt == nil {
		return false
	}
	switch val.Kind {
	case KindInt:
		return isIntegerType(mt.BaseType)
	case KindFloat:
		return isFloatType(mt.BaseType)
	case KindString:
		return mt.BaseType == "string"
	case KindBool:
		return mt.BaseType == "bool"
	case KindNull:
		return mt.BaseType == "null"
	case KindList:
		if mt.BaseType != "list" {
			return false
		}
		if mt.ListElemType != nil && len(val.List.Elements) > 0 {
			for _, el := range val.List.Elements {
				if !elementCompatible(el, mt.ListElemType) {
					return false
				}
			}
		}
		return true
	case KindTuple:
		if mt.BaseType != "tuple" {
			return false
		}
		if len(mt.TupleElemTypes) > 0 {
			if len(val.Tuple.Elements) != len(mt.TupleElemTypes) {
				return false
			}
			for i, el := range val.Tuple.Elements {
				if !elementCompatible(el, mt.TupleElemTypes[i]) {
					return false
				}
			}
		}
		return true
	case KindStruct:
		return mt.BaseType == "struct"
	}
	return false
}

// elementCompatible checks if a value's kind matches a target type.
func elementCompatible(val *Value, ti *TypeInfo) bool {
	switch val.Kind {
	case KindInt:
		return isIntegerType(ti.BaseType)
	case KindFloat:
		return isFloatType(ti.BaseType)
	case KindString:
		return ti.BaseType == "string"
	case KindBool:
		return ti.BaseType == "bool"
	case KindNull:
		return ti.BaseType == "null"
	}
	return false
}

func (ev *Evaluator) evalNamed(e *ast.NamedExpr, scope *Scope) (*Value, error) {
	if len(e.Variants) == 1 {
		return nil, fmt.Errorf("tagged union must have at least 2 variants, got 1")
	}
	// Duplicate variant names are a type error.
	if len(e.Variants) >= 2 {
		seen := make(map[string]bool, len(e.Variants))
		for _, v := range e.Variants {
			if seen[v.Name] {
				return nil, fmt.Errorf("duplicate variant %q in tagged union", v.Name)
			}
			seen[v.Name] = true
		}
	}
	val, err := ev.evalExpr(e.Value, scope)
	if err != nil {
		return nil, err
	}

	var variants []TaggedVariant
	var reusedTypeName string // saved before inner type adoption overwrites it
	if len(e.Variants) == 0 {
		// Reuse registered type
		if val.Type != nil && val.Type.Name != "" {
			reusedTypeName = val.Type.Name
			if registered, ok := ev.taggedVariants.get(val.Type.Name); ok {
				variants = registered
				found := false
				for _, v := range variants {
					if v.Name == e.Tag {
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("'%s' is not a variant of %s", e.Tag, val.Type.Name)
				}
			}
		}
	} else {
		for _, v := range e.Variants {
			variants = append(variants, TaggedVariant{Name: v.Name, Type: ev.resolveTypeExpr(v.TypeExpr)})
		}
	}
	// §5: inner values adopt the active variant's declared type.
	// For adoptable literals this is type adoption; for reused types
	// (as TypeName named tag) this assigns the concrete variant type.
	if len(variants) > 0 {
		for _, v := range variants {
			if v.Name == e.Tag && v.Type != nil {
				if val.Adoptable || unionMemberCompatible(val, v.Type) {
					val.Type = v.Type
					val.Adoptable = false
				}
				break
			}
		}
	}
	result := &Value{
		Kind:        KindTaggedUnion,
		TaggedUnion: &TaggedUnionValue{Tag: e.Tag, Inner: val, Variants: variants},
	}
	if reusedTypeName != "" {
		result.Type = &TypeInfo{Name: reusedTypeName}
	}
	return result, nil
}

func (ev *Evaluator) evalIsNamed(e *ast.IsNamedExpr, scope *Scope) (*Value, error) {
	val, err := ev.evalExpr(e.Value, scope)
	if err != nil {
		return nil, err
	}
	if val.Kind == KindUndefined {
		return nil, fmt.Errorf("'is named' on undefined")
	}
	if val.Kind != KindTaggedUnion {
		return nil, typeErrorf("'is named' requires tagged union, got %s", val.Kind)
	}
	// §3.7.2: variant name must be valid
	if len(val.TaggedUnion.Variants) > 0 {
		found := false
		for _, v := range val.TaggedUnion.Variants {
			if v.Name == e.Variant {
				found = true
				break
			}
		}
		if !found {
			return nil, typeErrorf("'%s' is not a variant of this tagged union", e.Variant)
		}
	}
	result := val.TaggedUnion.Tag == e.Variant
	if e.Negated {
		return Bool(!result), nil
	}
	return Bool(result), nil
}

// evalEnumDecl evaluates a standalone enum declaration "enum red, green, blue" (§3.5, §6.2).
// The default value is the first variant. The binding-level CalledName mechanism
// then names the type after the binding.
func (ev *Evaluator) evalEnumDecl(e *ast.EnumDeclExpr) (*Value, error) {
	if len(e.Variants) < 2 {
		return nil, fmt.Errorf("enum declaration requires at least 2 variants, got %d", len(e.Variants))
	}
	seen := make(map[string]bool, len(e.Variants))
	for _, v := range e.Variants {
		if seen[v] {
			return nil, fmt.Errorf("duplicate variant %q in enum", v)
		}
		seen[v] = true
	}
	return &Value{Kind: KindEnum, Enum: &EnumValue{Variant: e.Variants[0], Variants: e.Variants}}, nil
}

// evalUnionDecl evaluates a standalone union declaration "union i32, string" (§3.6, §6.2).
// The default value is the default of the first member type. §3.6 requires
// the first member's default to be transitively constructible.
func (ev *Evaluator) evalUnionDecl(e *ast.UnionDeclExpr) (*Value, error) {
	if len(e.MemberTypes) < 2 {
		return nil, typeErrorf("union declaration requires at least 2 member types, got %d", len(e.MemberTypes))
	}
	var memberTypes []*TypeInfo
	seen := make(map[string]bool, len(e.MemberTypes))
	for _, t := range e.MemberTypes {
		ti := ev.resolveTypeExpr(t)
		if ti != nil {
			key := ti.TypeKey()
			if key != "" {
				if seen[key] {
					return nil, typeErrorf("duplicate type %q in union", key)
				}
				seen[key] = true
			}
		}
		memberTypes = append(memberTypes, ti)
	}
	if !ev.hasConstructibleDefault(memberTypes[0]) {
		return nil, typeErrorf("union default cannot be constructed: first member %q has no default", memberTypes[0].TypeKey())
	}
	inner := zeroValueForType(memberTypes[0])
	if inner == nil {
		inner = Undefined()
	}
	return &Value{Kind: KindUnion, Union: &UnionValue{Inner: inner, MemberTypes: memberTypes}}, nil
}

// evalTaggedUnionDecl evaluates "tagged union ok as i32, err as string" (§3.7, §6.2).
// The default value is the first variant's default tagged with the first variant's name.
func (ev *Evaluator) evalTaggedUnionDecl(e *ast.TaggedUnionDeclExpr, scope *Scope) (*Value, error) {
	if len(e.Variants) < 2 {
		return nil, fmt.Errorf("tagged union declaration requires at least 2 variants, got %d", len(e.Variants))
	}
	seen := make(map[string]bool, len(e.Variants))
	for _, v := range e.Variants {
		if seen[v.Name] {
			return nil, fmt.Errorf("duplicate variant %q in tagged union", v.Name)
		}
		seen[v.Name] = true
	}
	var variants []TaggedVariant
	for _, v := range e.Variants {
		variants = append(variants, TaggedVariant{Name: v.Name, Type: ev.resolveTypeExpr(v.TypeExpr)})
	}
	first := variants[0]
	inner := zeroValueForType(first.Type)
	if inner == nil {
		inner = Undefined()
	}
	return &Value{
		Kind:        KindTaggedUnion,
		TaggedUnion: &TaggedUnionValue{Tag: first.Name, Inner: inner, Variants: variants},
	}, nil
}

// evalStructDecl evaluates "struct { fields }" — a standalone struct type
// declaration (§3.2, §6.2). It produces a struct value whose binding-level
// CalledName mechanism names the type.
func (ev *Evaluator) evalStructDecl(e *ast.StructDeclExpr, scope *Scope) (*Value, error) {
	se := &ast.StructExpr{Fields: e.Fields, Position: e.Position}
	return ev.evalStruct(se, scope)
}

// evalOf implements field extraction: "name is of expr" (§5.8).
func (ev *Evaluator) evalOf(e *ast.OfExpr, scope *Scope) (*Value, error) {
	key := scope.exclude
	if key == "" {
		return nil, fmt.Errorf("'of' used outside of binding context")
	}
	src, err := ev.evalExpr(e.Source, scope)
	if err != nil {
		return nil, err
	}
	if src.Kind == KindUndefined {
		return Undefined(), nil
	}
	if src.Kind != KindStruct {
		return nil, fmt.Errorf("'of' requires a struct, got %s", src.Kind)
	}
	v := src.Struct.Get(key)
	if v == nil {
		return Undefined(), nil
	}
	return v, nil
}

// --- Import ---

func (ev *Evaluator) evalStructImport(e *ast.StructImportExpr) (*Value, error) {
	path := e.Path
	if !strings.Contains(filepath.Base(path), ".") {
		path += ".uzon"
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(ev.baseDir, path)
	}
	// §7.1: canonicalize to a physical absolute path for circular import
	// detection and diamond deduplication. Steps required by the spec:
	// (1) absolute, (2) clean, (3) symlinks resolved.
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}

	if cached, ok := ev.imported[path]; ok {
		return cached, nil
	}

	// §7: circular imports are forbidden — detect in-progress imports
	if ev.importing[path] {
		return nil, &PosError{Pos: e.Position, Msg: fmt.Sprintf("circular import detected: %q", e.Path)}
	}
	ev.importing[path] = true
	defer delete(ev.importing, path)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &PosError{Pos: e.Position, Msg: fmt.Sprintf("import %q", e.Path), Cause: err}
	}

	p := ast.NewParser(data, path)
	doc, err := p.Parse()
	if err != nil {
		return nil, &PosError{Pos: e.Position, Msg: fmt.Sprintf("import %q", e.Path), Cause: err}
	}

	subEv := &Evaluator{
		scope:          newScope(nil),
		types:          newTypeRegistry(ev.types),
		enums:          newEnumRegistry(ev.enums),
		taggedVariants: newTaggedVariantRegistry(ev.taggedVariants),
		structShapes:   newStructShapeRegistry(ev.structShapes),
		env:            ev.env,
		baseDir:        filepath.Dir(path),
		imported:       ev.imported,
		importing:      ev.importing,
	}

	val, err := subEv.EvalDocument(doc)
	if err != nil {
		return nil, &PosError{Pos: e.Position, Msg: fmt.Sprintf("import %q", e.Path), Cause: err}
	}

	// Capture inner types so they can be re-registered with qualified prefix.
	if val != nil {
		val.typeScope = &structTypeScope{
			types:  subEv.types.types,
			enums:  subEv.enums.enums,
			tagged: subEv.taggedVariants.variants,
			shapes: subEv.structShapes.shapes,
		}
	}

	ev.imported[path] = val
	return val, nil
}

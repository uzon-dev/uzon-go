// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
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
	ev.taggedVariants = make(map[string][]TaggedVariant)
	ev.structShapes = make(map[string][]Field)

	val, err := ev.evalBindings(e.Fields, innerScope)

	// Capture inner registries before restoring.
	if err == nil && val != nil {
		val.typeScope = &structTypeScope{
			types:  ev.types.types,
			enums:  ev.enums.enums,
			tagged: ev.taggedVariants,
			shapes: ev.structShapes,
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
	for _, elem := range e.Elements {
		v, err := ev.evalExpr(elem, scope)
		if err != nil {
			return nil, err
		}
		elems = append(elems, v)
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
				for i, el := range elems {
					if el.Kind != baseKind && el.Kind != KindNull {
						return nil, fmt.Errorf("list elements must be same type: got %s and %s at index %d", baseKind, el.Kind, i)
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
	val, err := ev.evalExpr(e.Value, scope)
	if err != nil {
		return nil, err
	}
	ti := ev.resolveTypeExpr(e.TypeExpr)

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

	// §6.3: struct shape and type compatibility
	if val.Kind == KindStruct && ti.Name != "" {
		if expectedFields, ok := ev.structShapes[ti.Name]; ok {
			if len(val.Struct.Fields) != len(expectedFields) {
				return nil, fmt.Errorf("cannot cast struct to %s: different shape (%d fields vs %d)",
					ti.Name, len(val.Struct.Fields), len(expectedFields))
			}
			for _, ef := range expectedFields {
				actual := val.Struct.Get(ef.Name)
				if actual == nil {
					return nil, fmt.Errorf("cannot cast struct to %s: missing field %q", ti.Name, ef.Name)
				}
				if err := ev.checkWithTypeCompat(ef.Value, actual, ef.Name); err != nil {
					return nil, fmt.Errorf("as %s: %w", ti.Name, err)
				}
			}
		}
	}

	// List type annotation: apply element type
	if val.Kind == KindList && e.TypeExpr.ListElem != nil {
		elemTi := ev.resolveTypeExpr(e.TypeExpr.ListElem)
		if elemTi.Name != "" {
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

	// Numeric range validation for "as" annotation
	if val.Kind == KindInt && ti.BitSize > 0 && isIntegerType(ti.BaseType) {
		if err := checkIntRange(val.Int, ti.BitSize, ti.Signed); err != nil {
			return nil, fmt.Errorf("as %s: %w", ti.BaseType, err)
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

// checkWithTypeCompat validates type compatibility for with/plus overrides (§3.2.1).
func (ev *Evaluator) checkWithTypeCompat(original, override *Value, fieldName string) error {
	if override.Kind == KindNull || original.Kind == KindNull {
		return nil
	}
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
	return nil
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
	val, err := ev.evalExpr(e.Value, scope)
	if err != nil {
		return nil, err
	}
	var variant string
	if val.Type != nil && val.Type.Name == "__ident__" {
		variant = val.Str
	} else if val.Kind == KindString {
		variant = val.Str
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
			if registered, ok := ev.taggedVariants[val.Type.Name]; ok {
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
	path = filepath.Clean(path)

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
		taggedVariants: make(map[string][]TaggedVariant),
		structShapes:   make(map[string][]Field),
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
			tagged: subEv.taggedVariants,
			shapes: subEv.structShapes,
		}
	}

	ev.imported[path] = val
	return val, nil
}

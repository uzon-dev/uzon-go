// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/uzon-dev/uzon-go/ast"
)

// --- Compound types ---

func (ev *Evaluator) evalStruct(e *ast.StructExpr, scope *Scope) (*Value, error) {
	innerScope := newScope(scope)
	return ev.evalBindings(e.Fields, innerScope)
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
			}
		}
	}
	return NewList(elems, nil), nil
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
	if val.Kind == KindUndefined {
		return Undefined(), nil
	}

	ti := ev.resolveTypeExpr(e.TypeExpr)

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

	// §6.3: struct shape compatibility
	if val.Kind == KindStruct && ti.Name != "" {
		if expectedFields, ok := ev.structShapes[ti.Name]; ok {
			if len(val.Struct.Fields) != len(expectedFields) {
				return nil, fmt.Errorf("cannot cast struct to %s: different shape (%d fields vs %d)",
					ti.Name, len(val.Struct.Fields), len(expectedFields))
			}
			for _, name := range expectedFields {
				if val.Struct.Get(name) == nil {
					return nil, fmt.Errorf("cannot cast struct to %s: missing field %q", ti.Name, name)
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
			val.Type = ti
			val.Adoptable = false
			return val, nil
		}
	}

	// Numeric range validation for "as" annotation
	if val.Kind == KindInt && ti.BitSize > 0 && isIntegerType(ti.BaseType) {
		if err := checkIntRange(val.Int, ti.BitSize, ti.Signed); err != nil {
			return nil, fmt.Errorf("as %s: %w", ti.BaseType, err)
		}
	}

	val.Type = ti
	val.Adoptable = false
	return val, nil
}

// evalTo implements "to Type" conversion (§5.5).
func (ev *Evaluator) evalTo(e *ast.ToExpr, scope *Scope) (*Value, error) {
	val, err := ev.evalExpr(e.Value, scope)
	if err != nil {
		return nil, err
	}

	ti := ev.resolveTypeExpr(e.TypeExpr)

	// §5.11: "to bool" only allows identity (bool to bool)
	if ti != nil && ti.BaseType == "bool" && val.Kind != KindBool {
		return nil, fmt.Errorf("cannot convert %s to bool", val.Kind)
	}

	// §5.11: undefined propagates through "to"
	if val.Kind == KindUndefined {
		return Undefined(), nil
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
	return nil, fmt.Errorf("cannot convert %s to %s", val.Kind, target.BaseType)
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
		return nil, fmt.Errorf("cannot convert %s to string", val.Kind)
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
		var ok bool
		switch {
		case strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X"):
			_, ok = n.SetString(s[2:], 16)
		case strings.HasPrefix(s, "0o") || strings.HasPrefix(s, "0O"):
			_, ok = n.SetString(s[2:], 8)
		case strings.HasPrefix(s, "0b") || strings.HasPrefix(s, "0B"):
			_, ok = n.SetString(s[2:], 2)
		default:
			_, ok = n.SetString(s, 10)
		}
		if !ok {
			return nil, fmt.Errorf("cannot parse %q as integer", val.Str)
		}
	default:
		return nil, fmt.Errorf("cannot convert %s to %s", val.Kind, target.BaseType)
	}
	if target.BitSize > 0 && target.BitSize <= 64 {
		if err := checkIntRange(n, target.BitSize, target.Signed); err != nil {
			return nil, err
		}
	}
	return &Value{Kind: KindInt, Int: n, Type: target}, nil
}

func checkIntRange(n *big.Int, bits int, signed bool) error {
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
		return nil, fmt.Errorf("cannot convert %s to %s", val.Kind, target.BaseType)
	}
	return &Value{Kind: KindFloat, Float: f, Type: target}, nil
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
		return nil, fmt.Errorf("with: base must be a struct, got %s", base.Kind)
	}

	newFields := make([]Field, len(base.Struct.Fields))
	copy(newFields, base.Struct.Fields)

	for _, ob := range e.Override.Fields {
		v, err := ev.evalExpr(ob.Value, scope)
		if err != nil {
			return nil, err
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
			return nil, fmt.Errorf("with: unknown field %q", ob.Name)
		}
	}

	return &Value{Kind: KindStruct, Struct: &StructValue{Fields: newFields}, Type: base.Type}, nil
}

// checkWithTypeCompat validates type compatibility for with/extends overrides (§3.2.1).
func (ev *Evaluator) checkWithTypeCompat(original, override *Value, fieldName string) error {
	if override.Kind == KindNull || original.Kind == KindNull {
		return nil
	}
	if original.Type != nil && original.Type.BaseType != "" && original.Type.Name != "__ident__" {
		if override.Type != nil && override.Type.BaseType != "" && override.Type.Name != "__ident__" {
			if original.Type.BaseType != override.Type.BaseType {
				return fmt.Errorf("field %q: type %s incompatible with %s", fieldName, override.Type.BaseType, original.Type.BaseType)
			}
		}
	}
	if original.Kind != override.Kind {
		return fmt.Errorf("field %q: cannot override %s with %s", fieldName, original.Kind, override.Kind)
	}
	return nil
}

// evalExtends implements "extends { additions }" (§3.2.2).
func (ev *Evaluator) evalExtends(e *ast.ExtendsExpr, scope *Scope) (*Value, error) {
	base, err := ev.evalExpr(e.Base, scope)
	if err != nil {
		return nil, err
	}
	if base.Kind == KindUndefined {
		return nil, fmt.Errorf("extends: base is undefined")
	}
	if base.Kind != KindStruct {
		return nil, fmt.Errorf("extends: base must be a struct, got %s", base.Kind)
	}

	newFields := make([]Field, len(base.Struct.Fields))
	copy(newFields, base.Struct.Fields)

	hasNew := false
	for _, ob := range e.Extension.Fields {
		v, err := ev.evalExpr(ob.Value, scope)
		if err != nil {
			return nil, err
		}
		found := false
		for i, f := range newFields {
			if f.Name == ob.Name {
				if err := ev.checkWithTypeCompat(f.Value, v, ob.Name); err != nil {
					return nil, fmt.Errorf("extends: %w", err)
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
		return nil, fmt.Errorf("extends: must add at least one new field")
	}

	return &Value{Kind: KindStruct, Struct: &StructValue{Fields: newFields}}, nil
}

// --- Enum, union, tagged union ---

func (ev *Evaluator) evalFrom(e *ast.FromExpr, scope *Scope) (*Value, error) {
	if len(e.Variants) < 2 {
		return nil, fmt.Errorf("enum must have at least 2 variants, got %d", len(e.Variants))
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
	return &Value{Kind: KindEnum, Enum: &EnumValue{Variant: variant, Variants: e.Variants}}, nil
}

func (ev *Evaluator) evalUnion(e *ast.UnionExpr, scope *Scope) (*Value, error) {
	val, err := ev.evalExpr(e.Value, scope)
	if err != nil {
		return nil, err
	}
	var memberTypes []*TypeInfo
	for _, t := range e.MemberTypes {
		memberTypes = append(memberTypes, ev.resolveTypeExpr(t))
	}
	return &Value{Kind: KindUnion, Union: &UnionValue{Inner: val, MemberTypes: memberTypes}}, nil
}

func (ev *Evaluator) evalNamed(e *ast.NamedExpr, scope *Scope) (*Value, error) {
	if len(e.Variants) == 1 {
		return nil, fmt.Errorf("tagged union must have at least 2 variants, got 1")
	}
	val, err := ev.evalExpr(e.Value, scope)
	if err != nil {
		return nil, err
	}

	var variants []TaggedVariant
	if len(e.Variants) == 0 {
		// Reuse registered type
		if val.Type != nil && val.Type.Name != "" {
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
	return &Value{
		Kind:        KindTaggedUnion,
		TaggedUnion: &TaggedUnionValue{Tag: e.Tag, Inner: val, Variants: variants},
	}, nil
}

func (ev *Evaluator) evalIsNamed(e *ast.IsNamedExpr, scope *Scope) (*Value, error) {
	val, err := ev.evalExpr(e.Value, scope)
	if err != nil {
		return nil, err
	}
	if val.Kind != KindTaggedUnion {
		return nil, fmt.Errorf("'is named' requires tagged union, got %s", val.Kind)
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
		taggedVariants: ev.taggedVariants,
		structShapes:   ev.structShapes,
		env:            ev.env,
		baseDir:        filepath.Dir(path),
		imported:       ev.imported,
	}

	val, err := subEv.EvalDocument(doc)
	if err != nil {
		return nil, &PosError{Pos: e.Position, Msg: fmt.Sprintf("import %q", e.Path), Cause: err}
	}

	ev.imported[path] = val
	return val, nil
}

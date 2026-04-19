// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"math"
	"strings"

	"github.com/uzon-dev/uzon-go/token"
)

// Marshal serializes a Value to UZON expression text.
// Structs are emitted as struct literals with braces: { name is "x" }.
func (v *Value) Marshal() ([]byte, error) {
	e := &emitter{indent: 0}
	e.emitValue(v)
	return []byte(e.sb.String()), nil
}

// emitter writes UZON text into a string builder with indentation tracking.
type emitter struct {
	sb           strings.Builder
	indent       int
	definedTypes map[string]bool // tracks type names emitted with "called"
}

// emitDocument emits top-level bindings without surrounding braces.
func (e *emitter) emitDocument(v *Value) {
	if e.definedTypes == nil {
		e.definedTypes = make(map[string]bool)
	}
	for i, f := range v.Struct.Fields {
		if i > 0 {
			e.sb.WriteByte('\n')
		}
		e.emitFieldBinding(f)
	}
}

// emitFieldBinding emits a field as a binding.
// Uses "are" for non-empty lists per style guide §E.4.
func (e *emitter) emitFieldBinding(f Field) {
	e.emitFieldName(f.Name)
	if f.Value.Kind == KindList && len(f.Value.List.Elements) > 0 {
		e.sb.WriteString(" are ")
		elems := f.Value.List.Elements
		for i, elem := range elems {
			if i > 0 {
				e.sb.WriteString(", ")
			}
			// §3.4.1 / §9: a trailing `as Type` on the final element of an
			// are-binding lifts to the list level. Wrap in parens to keep
			// the annotation element-local.
			if i == len(elems)-1 && e.emitsTrailingAs(elem) {
				e.sb.WriteByte('(')
				e.emitValue(elem)
				e.sb.WriteByte(')')
			} else {
				e.emitValue(elem)
			}
		}
	} else {
		e.sb.WriteString(" is ")
		e.emitValue(f.Value)
	}

	// Emit "called TypeName" for type definitions (first occurrence)
	if e.definedTypes != nil && f.Value.Type != nil && f.Value.Type.Name != "" &&
		f.Value.Type.Name != "__ident__" && !e.definedTypes[f.Value.Type.Name] {
		switch f.Value.Kind {
		case KindEnum, KindTaggedUnion, KindStruct:
			e.sb.WriteString(" called ")
			e.sb.WriteString(f.Value.Type.Name)
			e.definedTypes[f.Value.Type.Name] = true
		}
	}
}

// emitValue writes a Value as a UZON expression.
func (e *emitter) emitValue(v *Value) {
	e.emitValueInner(v, true)
}

// emitValueBare writes a Value without trailing type annotation.
// Used for tagged union inner values in short form where the type
// is implied by the variant definition.
func (e *emitter) emitValueBare(v *Value) {
	e.emitValueInner(v, false)
}

func (e *emitter) emitValueInner(v *Value, withAnnotation bool) {
	switch v.Kind {
	case KindNull:
		e.sb.WriteString("null")
	case KindUndefined:
		e.sb.WriteString("undefined")
	case KindBool:
		if v.Bool {
			e.sb.WriteString("true")
		} else {
			e.sb.WriteString("false")
		}
	case KindInt:
		e.sb.WriteString(v.Int.String())
	case KindFloat:
		if v.FloatIsNaN {
			e.sb.WriteString("nan")
		} else if v.Float.IsInf() {
			if v.Float.Signbit() {
				e.sb.WriteString("-inf")
			} else {
				e.sb.WriteString("inf")
			}
		} else {
			f, _ := v.Float.Float64()
			if math.IsInf(f, 0) {
				// big.Float value exceeds float64 range; use Text representation
				e.sb.WriteString(v.Float.Text('g', -1))
			} else {
				e.sb.WriteString(formatFloat(f))
			}
		}
	case KindString:
		e.emitString(v.Str)
	case KindStruct:
		e.emitStruct(v)
	case KindTuple:
		e.emitTuple(v)
	case KindList:
		e.emitList(v)
	case KindEnum:
		if e.definedTypes != nil && v.Type != nil && v.Type.Name != "" && e.definedTypes[v.Type.Name] {
			e.emitIdentName(v.Enum.Variant)
			e.sb.WriteString(" as ")
			e.sb.WriteString(v.Type.Name)
		} else {
			e.emitEnum(v)
		}
	case KindUnion:
		e.emitUnion(v)
	case KindTaggedUnion:
		if e.definedTypes != nil && v.Type != nil && v.Type.Name != "" && e.definedTypes[v.Type.Name] {
			e.emitValueBare(v.TaggedUnion.Inner)
			e.sb.WriteString(" as ")
			e.sb.WriteString(v.Type.Name)
			e.sb.WriteString(" named ")
			e.emitIdentName(v.TaggedUnion.Tag)
		} else {
			e.emitTaggedUnion(v)
		}
	case KindFunction:
		e.sb.WriteString("<function>")
	}

	// Emit non-default type annotation (§6)
	if withAnnotation && v.Type != nil && v.Type.BaseType != "" &&
		v.Kind != KindEnum && v.Kind != KindTaggedUnion && v.Kind != KindUnion {
		if v.Type.Name != "__ident__" && needsTypeAnnotation(v) {
			e.sb.WriteString(" as ")
			e.sb.WriteString(v.Type.BaseType)
		}
	}
}

// emitsTrailingAs reports whether emitValue would end with an `as Type`
// suffix that the §3.4.1/§9 lift rule could grab in an are-binding.
// Tagged unions in short form end with `named tag` (not lift-eligible), so
// they are excluded here.
func (e *emitter) emitsTrailingAs(v *Value) bool {
	if v == nil || v.Type == nil {
		return false
	}
	if v.Kind == KindEnum && e.definedTypes != nil && v.Type.Name != "" && e.definedTypes[v.Type.Name] {
		return true
	}
	return needsTypeAnnotation(v)
}

// needsTypeAnnotation returns true when the value's type differs from the default.
func needsTypeAnnotation(v *Value) bool {
	if v.Type == nil {
		return false
	}
	switch v.Kind {
	case KindInt:
		return v.Type.BaseType != "i64"
	case KindFloat:
		return v.Type.BaseType != "f64"
	}
	return false
}

// emitString writes a quoted, escaped string literal (§4.4).
func (e *emitter) emitString(s string) {
	e.sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			e.sb.WriteString(`\"`)
		case '\\':
			e.sb.WriteString(`\\`)
		case '{':
			e.sb.WriteString(`\{`)
		case '\n':
			e.sb.WriteString(`\n`)
		case '\r':
			e.sb.WriteString(`\r`)
		case '\t':
			e.sb.WriteString(`\t`)
		case 0:
			e.sb.WriteString(`\0`)
		default:
			e.sb.WriteRune(r)
		}
	}
	e.sb.WriteByte('"')
}

// emitStruct writes a struct literal. Small structs (≤3 flat fields) use
// single-line format; larger or nested ones use multi-line with indentation.
func (e *emitter) emitStruct(v *Value) {
	if len(v.Struct.Fields) == 0 {
		e.sb.WriteString("{ }")
		return
	}
	if len(v.Struct.Fields) <= 3 && !hasNestedCompound(v) {
		e.sb.WriteString("{ ")
		for i, f := range v.Struct.Fields {
			if i > 0 {
				e.sb.WriteString(", ")
			}
			e.emitFieldName(f.Name)
			e.sb.WriteString(" is ")
			e.emitValue(f.Value)
		}
		e.sb.WriteString(" }")
		return
	}
	e.sb.WriteString("{\n")
	e.indent++
	for _, f := range v.Struct.Fields {
		e.writeIndent()
		e.emitFieldBinding(f)
		e.sb.WriteByte('\n')
	}
	e.indent--
	e.writeIndent()
	e.sb.WriteByte('}')
}

// emitIdentName writes an identifier (enum variant, tagged union tag, etc.)
// with quoting or keyword escaping if necessary.
func (e *emitter) emitIdentName(name string) {
	if needsQuoting(name) {
		e.sb.WriteByte('\'')
		e.sb.WriteString(name)
		e.sb.WriteByte('\'')
	} else if isKeyword(name) {
		e.sb.WriteByte('@')
		e.sb.WriteString(name)
	} else {
		e.sb.WriteString(name)
	}
}

// emitFieldName writes a binding name, quoting or escaping if necessary (§2.5, §2.6).
func (e *emitter) emitFieldName(name string) {
	if needsQuoting(name) {
		e.sb.WriteByte('\'')
		e.sb.WriteString(name)
		e.sb.WriteByte('\'')
	} else if isKeyword(name) {
		e.sb.WriteByte('@')
		e.sb.WriteString(name)
	} else {
		e.sb.WriteString(name)
	}
}

// needsQuoting returns true when a name contains characters requiring
// single-quote wrapping (§2.5).
func needsQuoting(name string) bool {
	if name == "" {
		return true
	}
	if name[0] >= '0' && name[0] <= '9' {
		return true
	}
	for _, r := range name {
		if r < 128 {
			switch r {
			case '{', '}', '[', ']', '(', ')', ',', '.', '"', '\'', '@',
				'+', '-', '*', '/', '%', '^', '<', '>', '=', '!', '?',
				':', ';', '|', '&', '$', '~', '#', '\\',
				' ', '\t', '\n', '\r':
				return true
			}
		}
	}
	return false
}

// isKeyword checks whether a name is a UZON keyword requiring @ escape (§2.6).
func isKeyword(name string) bool {
	return token.IsKeyword(name)
}

// emitTuple writes a tuple literal. Single-element tuples include a trailing
// comma to distinguish from grouping parentheses (§3.2).
func (e *emitter) emitTuple(v *Value) {
	e.sb.WriteByte('(')
	for i, elem := range v.Tuple.Elements {
		if i > 0 {
			e.sb.WriteString(", ")
		}
		e.emitValue(elem)
	}
	if len(v.Tuple.Elements) == 1 {
		e.sb.WriteByte(',')
	}
	e.sb.WriteByte(')')
}

// emitList writes a list literal (§3.3).
func (e *emitter) emitList(v *Value) {
	if len(v.List.Elements) == 0 {
		e.sb.WriteString("[]")
		if v.List.ElementType != nil {
			typeName := v.List.ElementType.BaseType
			if typeName == "" {
				typeName = v.List.ElementType.Name
			}
			if typeName != "" {
				e.sb.WriteString(" as [")
				e.sb.WriteString(typeName)
				e.sb.WriteByte(']')
			}
		}
		return
	}
	e.sb.WriteString("[ ")
	for i, elem := range v.List.Elements {
		if i > 0 {
			e.sb.WriteString(", ")
		}
		e.emitValue(elem)
	}
	e.sb.WriteString(" ]")
}

// emitEnum writes an enum definition: "variant from v1, v2, ..." (§3.4).
// The "called TypeName" is handled by emitFieldBinding.
func (e *emitter) emitEnum(v *Value) {
	e.emitIdentName(v.Enum.Variant)
	e.sb.WriteString(" from ")
	for i, variant := range v.Enum.Variants {
		if i > 0 {
			e.sb.WriteString(", ")
		}
		e.emitIdentName(variant)
	}
}

// emitUnion writes a union expression: "value from union type1, type2, ..." (§3.5).
func (e *emitter) emitUnion(v *Value) {
	e.emitValue(v.Union.Inner)
	e.sb.WriteString(" from union ")
	for i, mt := range v.Union.MemberTypes {
		if i > 0 {
			e.sb.WriteString(", ")
		}
		if mt.BaseType != "" {
			e.sb.WriteString(mt.BaseType)
		} else if mt.Name != "" {
			e.sb.WriteString(mt.Name)
		}
	}
}

// emitTaggedUnion writes a tagged union definition: "value named tag from ..." (§3.6).
// The "called TypeName" is handled by emitFieldBinding.
func (e *emitter) emitTaggedUnion(v *Value) {
	e.emitValue(v.TaggedUnion.Inner)
	e.sb.WriteString(" named ")
	e.emitIdentName(v.TaggedUnion.Tag)
	if len(v.TaggedUnion.Variants) > 0 {
		e.sb.WriteString(" from ")
		for i, variant := range v.TaggedUnion.Variants {
			if i > 0 {
				e.sb.WriteString(", ")
			}
			e.emitIdentName(variant.Name)
			e.sb.WriteString(" as ")
			if variant.Type != nil {
				typ := variant.Type.BaseType
				if typ == "" {
					typ = variant.Type.Name
				}
				e.sb.WriteString(typ)
			} else {
				e.sb.WriteString("null")
			}
		}
	}
}

func (e *emitter) writeIndent() {
	for i := 0; i < e.indent; i++ {
		e.sb.WriteString("    ")
	}
}

// hasNestedCompound checks if any struct field contains a compound value.
func hasNestedCompound(v *Value) bool {
	for _, f := range v.Struct.Fields {
		switch f.Value.Kind {
		case KindStruct, KindList, KindTuple:
			return true
		}
	}
	return false
}

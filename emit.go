// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"fmt"
	"strings"
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
	sb     strings.Builder
	indent int
}

// emitDocument emits top-level bindings without surrounding braces.
func (e *emitter) emitDocument(v *Value) {
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
		for i, elem := range f.Value.List.Elements {
			if i > 0 {
				e.sb.WriteString(", ")
			}
			e.emitValue(elem)
		}
	} else {
		e.sb.WriteString(" is ")
		e.emitValue(f.Value)
	}
}

// emitValue writes a Value as a UZON expression.
func (e *emitter) emitValue(v *Value) {
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
			e.sb.WriteString(formatFloat(f))
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
		e.emitEnum(v)
	case KindUnion:
		e.emitUnion(v)
	case KindTaggedUnion:
		e.emitTaggedUnion(v)
	case KindFunction:
		e.sb.WriteString("<function>")
	}

	// Emit non-default type annotation (§6)
	if v.Type != nil && v.Type.BaseType != "" &&
		v.Kind != KindEnum && v.Kind != KindTaggedUnion && v.Kind != KindUnion {
		if v.Type.Name == "" && v.Type.Name != "__ident__" && needsTypeAnnotation(v) {
			e.sb.WriteString(" as ")
			e.sb.WriteString(v.Type.BaseType)
		}
	}
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
	keywords := map[string]bool{
		"true": true, "false": true, "null": true, "inf": true, "nan": true, "undefined": true,
		"is": true, "are": true, "from": true, "called": true, "as": true, "named": true,
		"with": true, "union": true, "extends": true, "to": true, "of": true,
		"and": true, "or": true, "not": true, "if": true, "then": true, "else": true,
		"case": true, "when": true, "self": true, "env": true, "struct": true, "in": true,
		"function": true, "returns": true, "default": true, "lazy": true, "type": true,
	}
	return keywords[name]
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
		if v.List.ElementType != nil && v.List.ElementType.BaseType != "" {
			e.sb.WriteString(" as [")
			e.sb.WriteString(v.List.ElementType.BaseType)
			e.sb.WriteByte(']')
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

// emitEnum writes an enum expression: "variant from v1, v2, ..." (§3.4).
func (e *emitter) emitEnum(v *Value) {
	e.sb.WriteString(v.Enum.Variant)
	e.sb.WriteString(" from ")
	for i, variant := range v.Enum.Variants {
		if i > 0 {
			e.sb.WriteString(", ")
		}
		e.sb.WriteString(variant)
	}
	if v.Type != nil && v.Type.Name != "" {
		e.sb.WriteString(" called ")
		e.sb.WriteString(v.Type.Name)
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

// emitTaggedUnion writes a tagged union: "value named tag from ..." (§3.6).
func (e *emitter) emitTaggedUnion(v *Value) {
	e.emitValue(v.TaggedUnion.Inner)
	e.sb.WriteString(" named ")
	e.sb.WriteString(v.TaggedUnion.Tag)
	if len(v.TaggedUnion.Variants) > 0 {
		e.sb.WriteString(" from ")
		for i, variant := range v.TaggedUnion.Variants {
			if i > 0 {
				e.sb.WriteString(", ")
			}
			e.sb.WriteString(variant.Name)
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
	if v.Type != nil && v.Type.Name != "" {
		fmt.Fprintf(&e.sb, " called %s", v.Type.Name)
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

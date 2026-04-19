// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

// Package ast defines the abstract syntax tree types for the UZON language.
//
// Every syntactic construct in a UZON document maps to a Node in this
// package. The top-level Document contains Bindings; each Binding associates
// a name with an Expr. Expression types cover literals, identifiers,
// operators, control flow (if/case), type operations (as/to/with/plus),
// compound values (struct/tuple/list/enum/union), and functions (§1–§7).
package ast

import "github.com/uzon-dev/uzon-go/token"

// Node is the interface implemented by all AST nodes.
// Every node carries a source position for error reporting (§11.2.0).
type Node interface {
	Pos() token.Pos
	nodeTag()
}

// Document is the top-level AST node representing a UZON file.
// A UZON file is an anonymous struct: a sequence of bindings (§1).
type Document struct {
	Bindings []*Binding
	Position token.Pos
}

func (d *Document) Pos() token.Pos { return d.Position }
func (d *Document) nodeTag()       {}

// Binding represents a "name is expr" or "name are expr, expr, ..." statement.
// Bindings associate names with values using the "is" keyword (§1).
type Binding struct {
	Name       string    // the binding's identifier
	Value      Expr      // the bound expression
	CalledName string    // type name from trailing "called Name", empty if none (§6)
	Position   token.Pos
}

func (b *Binding) Pos() token.Pos { return b.Position }
func (b *Binding) nodeTag()       {}

// Expr is the interface for all expression AST nodes.
type Expr interface {
	Node
	exprTag()
}

// LiteralExpr represents a literal value: integer, float, string, bool, null,
// inf, or nan (§4).
type LiteralExpr struct {
	Token token.Token
}

func (e *LiteralExpr) Pos() token.Pos { return e.Token.Pos }
func (e *LiteralExpr) nodeTag()       {}
func (e *LiteralExpr) exprTag()       {}

// IdentExpr represents a bare identifier reference (used for variable
// references, enum variants, etc.).
type IdentExpr struct {
	Name     string
	Position token.Pos
}

func (e *IdentExpr) Pos() token.Pos { return e.Position }
func (e *IdentExpr) nodeTag()       {}
func (e *IdentExpr) exprTag()       {}

// VariantShorthandExpr represents the v0.10 tagged union variant shorthand
// (§3.7): `variant_name primary` (e.g., `pressed "enter"`). It sits at the
// `primary` grammar production and binds tighter than every expression-level
// operator. The shorthand is resolved at evaluation time when type context
// (via `as Type`, struct field type, function parameter, function return,
// or list element type) supplies the tagged union type.
type VariantShorthandExpr struct {
	Name     string
	Inner    Expr
	Position token.Pos
}

func (e *VariantShorthandExpr) Pos() token.Pos { return e.Position }
func (e *VariantShorthandExpr) nodeTag()       {}
func (e *VariantShorthandExpr) exprTag()       {}

// UndefinedExpr represents the "undefined" literal (§3.1).
// Undefined is a special state indicating a missing value, distinct from null.
type UndefinedExpr struct {
	Position token.Pos
}

func (e *UndefinedExpr) Pos() token.Pos { return e.Position }
func (e *UndefinedExpr) nodeTag()       {}
func (e *UndefinedExpr) exprTag()       {}

// EnvExpr represents "env" — access to environment variables (§5.13).
type EnvExpr struct {
	Position token.Pos
}

func (e *EnvExpr) Pos() token.Pos { return e.Position }
func (e *EnvExpr) nodeTag()       {}
func (e *EnvExpr) exprTag()       {}

// MemberExpr represents field or element access: "expr.name" or "expr.0" (§3.2, §3.3).
type MemberExpr struct {
	Object   Expr
	Member   string // field name or numeric index as string
	Position token.Pos
}

func (e *MemberExpr) Pos() token.Pos { return e.Position }
func (e *MemberExpr) nodeTag()       {}
func (e *MemberExpr) exprTag()       {}

// CallExpr represents a function call: "expr(args...)" (§3.7).
type CallExpr struct {
	Func     Expr
	Args     []Expr
	Position token.Pos
}

func (e *CallExpr) Pos() token.Pos { return e.Position }
func (e *CallExpr) nodeTag()       {}
func (e *CallExpr) exprTag()       {}

// BinaryExpr represents a binary operation: "left op right" (§5.1–§5.4).
type BinaryExpr struct {
	Op       token.Type
	Left     Expr
	Right    Expr
	Position token.Pos
}

func (e *BinaryExpr) Pos() token.Pos { return e.Position }
func (e *BinaryExpr) nodeTag()       {}
func (e *BinaryExpr) exprTag()       {}

// UnaryExpr represents a unary operation: "not expr" or "-expr" (§5.1, §5.4).
type UnaryExpr struct {
	Op       token.Type
	Operand  Expr
	Position token.Pos
}

func (e *UnaryExpr) Pos() token.Pos { return e.Position }
func (e *UnaryExpr) nodeTag()       {}
func (e *UnaryExpr) exprTag()       {}

// IfExpr represents "if cond then a else b" (§5.9).
type IfExpr struct {
	Cond     Expr
	Then     Expr
	Else     Expr
	Position token.Pos
}

func (e *IfExpr) Pos() token.Pos { return e.Position }
func (e *IfExpr) nodeTag()       {}
func (e *IfExpr) exprTag()       {}

// CaseExpr represents pattern matching (§5.10). Three forms:
//   - "case expr when val then ..."        — value matching (Mode = "")
//   - "case type expr when T then ..."     — type dispatch (Mode = "type")
//   - "case named expr when tag then ..."  — variant dispatch (Mode = "named")
type CaseExpr struct {
	Mode      string // "", "type", or "named"
	Scrutinee Expr
	Whens     []*WhenClause
	Else      Expr
	Position  token.Pos
}

func (e *CaseExpr) Pos() token.Pos { return e.Position }
func (e *CaseExpr) nodeTag()       {}
func (e *CaseExpr) exprTag()       {}

// WhenClause represents a single "when" arm in a case expression (§5.10).
// For value matching: Value is set.
// For type dispatch (case type): TypeExpr is set.
// For variant dispatch (case named): VariantName is set.
type WhenClause struct {
	Value       Expr      // for value matching
	TypeExpr    *TypeExpr // for "case type" — type to match
	VariantName string    // for "case named" — variant tag to match
	Then        Expr
	Position    token.Pos
}

// StructExpr represents a struct literal "{ bindings... }" (§3.2).
type StructExpr struct {
	Fields   []*Binding
	Position token.Pos
}

func (e *StructExpr) Pos() token.Pos { return e.Position }
func (e *StructExpr) nodeTag()       {}
func (e *StructExpr) exprTag()       {}

// ListExpr represents a list literal "[ elements... ]" (§3.3).
type ListExpr struct {
	Elements []Expr
	Position token.Pos
}

func (e *ListExpr) Pos() token.Pos { return e.Position }
func (e *ListExpr) nodeTag()       {}
func (e *ListExpr) exprTag()       {}

// TupleExpr represents a tuple literal "(elements...)" (§3.4).
type TupleExpr struct {
	Elements []Expr
	Position token.Pos
}

func (e *TupleExpr) Pos() token.Pos { return e.Position }
func (e *TupleExpr) nodeTag()       {}
func (e *TupleExpr) exprTag()       {}

// AsExpr represents type annotation: "expr as Type" (§6.1).
// As asserts what a value already is — it does not change the value.
type AsExpr struct {
	Value    Expr
	TypeExpr *TypeExpr
	Position token.Pos
	// Parenthesized is true when the entire `expr as Type` was wrapped in
	// parens in the source. Suppresses the §3.4.1/§9 lift rule in
	// are-bindings: `xs are 1, 2, (3 as i32)` keeps `as i32` element-local.
	Parenthesized bool
}

func (e *AsExpr) Pos() token.Pos { return e.Position }
func (e *AsExpr) nodeTag()       {}
func (e *AsExpr) exprTag()       {}

// ToExpr represents type conversion: "expr to Type" (§5.5).
// To transforms a value into a different representation.
type ToExpr struct {
	Value    Expr
	TypeExpr *TypeExpr
	Position token.Pos
}

func (e *ToExpr) Pos() token.Pos { return e.Position }
func (e *ToExpr) nodeTag()       {}
func (e *ToExpr) exprTag()       {}

// WithExpr represents struct override: "expr with { overrides }" (§3.2.1).
// Copies a struct and replaces specified fields, preserving shape and type.
type WithExpr struct {
	Base     Expr
	Override *StructExpr
	Position token.Pos
}

func (e *WithExpr) Pos() token.Pos { return e.Position }
func (e *WithExpr) nodeTag()       {}
func (e *WithExpr) exprTag()       {}

// PlusExpr represents struct extension: "expr plus { additions }" (§3.2.2).
// Copies a struct, adds new fields, and optionally overrides existing ones.
type PlusExpr struct {
	Base      Expr
	Extension *StructExpr
	Position  token.Pos
}

func (e *PlusExpr) Pos() token.Pos { return e.Position }
func (e *PlusExpr) nodeTag()       {}
func (e *PlusExpr) exprTag()       {}

// FromExpr represents enum definition: "value from variant, variant, ..." (§3.5).
type FromExpr struct {
	Value    Expr
	Variants []string
	Position token.Pos
}

func (e *FromExpr) Pos() token.Pos { return e.Position }
func (e *FromExpr) nodeTag()       {}
func (e *FromExpr) exprTag()       {}

// UnionExpr represents union definition: "value from union type, type, ..." (§3.6).
type UnionExpr struct {
	Value       Expr
	MemberTypes []*TypeExpr
	Position    token.Pos
}

func (e *UnionExpr) Pos() token.Pos { return e.Position }
func (e *UnionExpr) nodeTag()       {}
func (e *UnionExpr) exprTag()       {}

// NamedExpr represents tagged union: "value named tag from variant as type, ..." (§3.7).
type NamedExpr struct {
	Value    Expr
	Tag      string
	Variants []*TaggedVariantExpr // nil when reusing an existing type via "as Type"
	Position token.Pos
}

func (e *NamedExpr) Pos() token.Pos { return e.Position }
func (e *NamedExpr) nodeTag()       {}
func (e *NamedExpr) exprTag()       {}

// TaggedVariantExpr represents "variantName as Type" within a tagged union definition.
type TaggedVariantExpr struct {
	Name     string
	TypeExpr *TypeExpr
	Position token.Pos
}

// OfExpr represents field extraction: "is of expr" (§5.8).
// The binding's own name is used as the field key.
type OfExpr struct {
	Source   Expr
	Position token.Pos
}

func (e *OfExpr) Pos() token.Pos { return e.Position }
func (e *OfExpr) nodeTag()       {}
func (e *OfExpr) exprTag()       {}

// IsNamedExpr represents tagged union testing:
// "expr is named variant" or "expr is not named variant" (§3.7).
type IsNamedExpr struct {
	Value    Expr
	Variant  string
	Negated  bool // true for "is not named"
	Position token.Pos
}

func (e *IsNamedExpr) Pos() token.Pos { return e.Position }
func (e *IsNamedExpr) nodeTag()       {}
func (e *IsNamedExpr) exprTag()       {}

// IsTypeExpr represents runtime type checking:
// "expr is type T" or "expr is not type T" (§5.2).
type IsTypeExpr struct {
	Value    Expr
	TypeExpr *TypeExpr
	Negated  bool // true for "is not type"
	Position token.Pos
}

func (e *IsTypeExpr) Pos() token.Pos { return e.Position }
func (e *IsTypeExpr) nodeTag()       {}
func (e *IsTypeExpr) exprTag()       {}

// StructImportExpr represents file import: 'struct "path"' (§7).
type StructImportExpr struct {
	Path     string
	Position token.Pos
}

func (e *StructImportExpr) Pos() token.Pos { return e.Position }
func (e *StructImportExpr) nodeTag()       {}
func (e *StructImportExpr) exprTag()       {}

// EnumDeclExpr represents a standalone enum type declaration (§3.5, §6.2):
// "X is enum red, green, blue". The binding name becomes the type name and the
// default value is the first variant. `called` is forbidden alongside this form.
type EnumDeclExpr struct {
	Variants []string
	Position token.Pos
}

func (e *EnumDeclExpr) Pos() token.Pos { return e.Position }
func (e *EnumDeclExpr) nodeTag()       {}
func (e *EnumDeclExpr) exprTag()       {}

// UnionDeclExpr represents a standalone union type declaration (§3.6, §6.2):
// "X is union i32, string". The binding name becomes the type name and the
// default value is the default of the first member type.
type UnionDeclExpr struct {
	MemberTypes []*TypeExpr
	Position    token.Pos
}

func (e *UnionDeclExpr) Pos() token.Pos { return e.Position }
func (e *UnionDeclExpr) nodeTag()       {}
func (e *UnionDeclExpr) exprTag()       {}

// TaggedUnionDeclExpr represents a standalone tagged union declaration (§3.7, §6.2):
// "X is tagged union ok as i32, err as string". The binding name becomes the type name
// and the default value is the first variant's default with the first variant's tag.
type TaggedUnionDeclExpr struct {
	Variants []*TaggedVariantExpr
	Position token.Pos
}

func (e *TaggedUnionDeclExpr) Pos() token.Pos { return e.Position }
func (e *TaggedUnionDeclExpr) nodeTag()       {}
func (e *TaggedUnionDeclExpr) exprTag()       {}

// StructDeclExpr represents a standalone struct type declaration (§3.2, §6.2):
// "X is struct { x is 0, y is 0 }". The binding name becomes the type name.
type StructDeclExpr struct {
	Fields   []*Binding
	Position token.Pos
}

func (e *StructDeclExpr) Pos() token.Pos { return e.Position }
func (e *StructDeclExpr) nodeTag()       {}
func (e *StructDeclExpr) exprTag()       {}

// FunctionExpr represents a function definition (§3.8).
type FunctionExpr struct {
	Params     []*ParamExpr
	ReturnType *TypeExpr  // nil if return type not specified
	Bindings   []*Binding // intermediate bindings in body
	Body       Expr       // final expression (return value)
	Position   token.Pos
}

func (e *FunctionExpr) Pos() token.Pos { return e.Position }
func (e *FunctionExpr) nodeTag()       {}
func (e *FunctionExpr) exprTag()       {}

// ParamExpr represents a function parameter: "name as Type [default expr]" (§3.8).
type ParamExpr struct {
	Name     string
	TypeExpr *TypeExpr
	Default  Expr // nil if no default value
	Position token.Pos
}

// AreExpr represents list sugar: "name are expr, expr, ..." (§3.3).
// Desugars to a list without requiring square brackets.
type AreExpr struct {
	Elements       []Expr
	TypeAnnotation *TypeExpr // optional trailing "as [Type]"
	Position       token.Pos
}

func (e *AreExpr) Pos() token.Pos { return e.Position }
func (e *AreExpr) nodeTag()       {}
func (e *AreExpr) exprTag()       {}

// InterpolatedStringExpr represents a string containing interpolation
// expressions: "hello {name}" (§4.4.1).
type InterpolatedStringExpr struct {
	Parts    []StringPart
	Position token.Pos
}

func (e *InterpolatedStringExpr) Pos() token.Pos { return e.Position }
func (e *InterpolatedStringExpr) nodeTag()       {}
func (e *InterpolatedStringExpr) exprTag()       {}

// StringPart is either a literal text segment or an interpolated expression
// within an InterpolatedStringExpr.
type StringPart struct {
	IsExpr bool
	Text   string // when IsExpr is false
	Expr   Expr   // when IsExpr is true
}

// TypeExpr represents a type reference in UZON (§6).
// It can be a named type path, a list type [Type], a tuple type
// (Type, Type), or the null type.
type TypeExpr struct {
	Path       []string   // type path segments, e.g. ["i32"] or ["inner", "RGB"]
	ListElem   *TypeExpr  // non-nil for list types: [ElementType]
	TupleElems []*TypeExpr // non-nil for tuple types: (Type1, Type2, ...)
	IsNull     bool       // true for the null type
	Position   token.Pos
}

func (e *TypeExpr) Pos() token.Pos { return e.Position }
func (e *TypeExpr) nodeTag()       {}

// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package ast

import (
	"fmt"

	"github.com/uzon-dev/uzon-go/token"
)

// Parser parses UZON source tokens into an AST.
type Parser struct {
	lex   *token.Lexer
	prev  token.Token // previously consumed token (for same-line checks)
	cur   token.Token
	peek  token.Token
	peek2 token.Token // 3-token lookahead (for @keyword is/are detection)
	file  string

	errors []error

	// noStringConcat suppresses adjacent string literal concatenation.
	// Set when parsing binding values inside function bodies so that the
	// return expression is not consumed as part of a preceding binding value.
	noStringConcat bool

	// inFunctionBody suppresses 'called' and 'are' inside function bodies (§3.8).
	inFunctionBody bool
}

// NewParser creates a new parser from source bytes.
func NewParser(src []byte, file string) *Parser {
	lex := token.NewLexer(src, file)
	p := &Parser{lex: lex, file: file}
	p.advance() // fill cur
	p.advance() // fill peek
	p.advance() // fill peek2 (cur now has first token)
	return p
}

// Parse parses a complete UZON document.
func (p *Parser) Parse() (*Document, error) {
	doc := &Document{Position: p.cur.Pos}
	doc.Bindings = p.parseBindings(token.EOF)
	// Surface lexical errors recorded during scanning.
	if lexErrs := p.lex.Errors(); len(lexErrs) > 0 {
		return doc, fmt.Errorf("%s", lexErrs[0].Error())
	}
	if len(p.errors) > 0 {
		return doc, p.errors[0]
	}
	return doc, nil
}

// Errors returns all accumulated parse errors.
func (p *Parser) Errors() []error {
	return p.errors
}

func (p *Parser) advance() {
	p.prev = p.cur
	p.cur = p.peek
	p.peek = p.peek2
	p.peek2 = p.lex.Next()
}

func (p *Parser) expect(t token.Type) token.Token {
	tok := p.cur
	if tok.Type != t {
		p.errorf(tok.Pos, "expected %v, got %v (%q)", t, tok.Type, tok.Literal)
	}
	p.advance()
	return tok
}

func (p *Parser) errorf(pos token.Pos, format string, args ...any) {
	p.errors = append(p.errors, fmt.Errorf("%s: %s", pos, fmt.Sprintf(format, args...)))
}

func (p *Parser) at(t token.Type) bool {
	return p.cur.Type == t
}

func (p *Parser) match(t token.Type) bool {
	if p.cur.Type == t {
		p.advance()
		return true
	}
	return false
}

// parseBindings parses a sequence of bindings until the stop token.
func (p *Parser) parseBindings(stop token.Type) []*Binding {
	var bindings []*Binding
	for !p.at(stop) && !p.at(token.EOF) {
		b := p.parseBinding()
		if b != nil {
			bindings = append(bindings, b)
		}
		p.match(token.Comma) // optional comma separator
	}
	return bindings
}

// parseBinding parses "name is expr [called name]" or "name are expr, ...".
// Also handles binding decomposition for composite keywords (§9):
//   - "x is not expr" → UnaryExpr(not, expr)
//   - "x is named ..." → IdentExpr("named") with type-decl suffix
//   - "x is not named ..." → UnaryExpr(not, IdentExpr("named") with suffix)
func (p *Parser) parseBinding() *Binding {
	pos := p.cur.Pos
	name := p.parseName()
	if name == "" {
		// §11.2: suggest @keyword escape when a keyword appears at binding position.
		if token.IsKeyword(p.cur.Literal) && p.cur.Type != token.EOF && isBindingOperator(p.peek.Type) {
			p.errorf(pos, "%q is a keyword; to use it as a binding name, write @%s",
				p.cur.Literal, p.cur.Literal)
		} else {
			p.errorf(pos, "expected binding name, got %v (%q)", p.cur.Type, p.cur.Literal)
		}
		p.advance()
		return nil
	}

	b := &Binding{Name: name, Position: pos}

	if p.at(token.Is) {
		p.advance()
		if p.at(token.Of) {
			// Field extraction: "name is of expr" (§5.8)
			p.advance()
			src := p.parseMemberAccess()
			b.Value = &OfExpr{Source: src, Position: pos}
		} else {
			b.Value = p.parseExpression()
		}
	} else if p.at(token.IsNot) {
		// §9 binding decomposition: "x is not expr" → x = (not expr)
		notPos := p.cur.Pos
		p.advance()
		operand := p.parseNot()
		left := Expr(&UnaryExpr{Op: token.Not, Operand: operand, Position: notPos})
		b.Value = p.continueFromAndLevel(left)
	} else if p.at(token.IsNamed) {
		// §9 binding decomposition: "x is named ..." → "named" becomes an identifier
		namedPos := p.cur.Pos
		p.advance()
		left := Expr(&IdentExpr{Name: "named", Position: namedPos})
		b.Value = p.continueExprFromTypeDecl(left)
	} else if p.at(token.IsNotNamed) {
		// §9 binding decomposition: "x is not named ..." → not(<ident "named" ...>)
		notPos := p.cur.Pos
		p.advance()
		left := Expr(&IdentExpr{Name: "named", Position: notPos})
		inner := p.continueExprFromTypeDecl(left)
		notExpr := Expr(&UnaryExpr{Op: token.Not, Operand: inner, Position: notPos})
		b.Value = p.continueFromAndLevel(notExpr)
	} else if p.at(token.IsType) {
		// §9 binding decomposition: "x is type ..." → "type" becomes an identifier
		typePos := p.cur.Pos
		p.advance()
		left := Expr(&IdentExpr{Name: "type", Position: typePos})
		b.Value = p.continueExprFromTypeDecl(left)
	} else if p.at(token.IsNotType) {
		// §9 binding decomposition: "x is not type ..." → not(<ident "type" ...>)
		notPos := p.cur.Pos
		p.advance()
		left := Expr(&IdentExpr{Name: "type", Position: notPos})
		inner := p.continueExprFromTypeDecl(left)
		notExpr := Expr(&UnaryExpr{Op: token.Not, Operand: inner, Position: notPos})
		b.Value = p.continueFromAndLevel(notExpr)
	} else if p.at(token.Are) {
		p.advance()
		b.Value = p.parseAreExpr()
	} else {
		p.errorf(p.cur.Pos, "expected 'is' or 'are' after binding name %q, got %v", name, p.cur.Type)
		return nil
	}

	// Optional trailing "called Name" for type naming (§6).
	// §3.8: called is not permitted inside function bodies.
	// §6.2: 'called' is forbidden alongside standalone type declarations.
	if p.at(token.Called) {
		if p.inFunctionBody {
			p.errorf(p.cur.Pos, "'called' is not permitted inside function bodies")
		} else if isStandaloneTypeDecl(b.Value) {
			p.errorf(p.cur.Pos, "'called' is not permitted with standalone type declarations (§6.2)")
		} else {
			p.advance()
			b.CalledName = p.parseName()
		}
	}

	// §6.2: standalone type declarations adopt the binding name as the type name.
	if isStandaloneTypeDecl(b.Value) {
		b.CalledName = b.Name
	}

	return b
}

// isStandaloneTypeDecl reports whether expr is a standalone type
// declaration (enum, union, tagged union, or struct literal after `struct`).
func isStandaloneTypeDecl(expr Expr) bool {
	switch expr.(type) {
	case *EnumDeclExpr, *UnionDeclExpr, *TaggedUnionDeclExpr, *StructDeclExpr:
		return true
	}
	return false
}

// parseName reads an identifier name.
// Also allows the contextual keyword "env" and keyword escapes (@keyword)
// as binding names per §9: name = identifier | keyword_escape.
func (p *Parser) parseName() string {
	if p.at(token.Ident) {
		name := p.cur.Literal
		p.advance()
		return name
	}
	// Allow contextual keywords as binding names (e.g. "env is ...")
	if p.at(token.Env) {
		name := p.cur.Literal
		p.advance()
		return name
	}
	// Keyword escape: @keyword (§9 name = identifier | keyword_escape).
	if p.at(token.At) && token.IsKeyword(p.peek.Literal) {
		name := p.peek.Literal
		p.advance() // consume @
		p.advance() // consume keyword
		return name
	}
	return ""
}

// parseNameOrKeyword reads a name that could also be a keyword.
// Keywords are valid variant names per §9 variant_name.
func (p *Parser) parseNameOrKeyword() string {
	if p.at(token.Ident) {
		name := p.cur.Literal
		p.advance()
		return name
	}
	if token.IsKeyword(p.cur.Literal) && p.cur.Type != token.EOF {
		name := p.cur.Literal
		p.advance()
		return name
	}
	return ""
}

// parseAreExpr parses "expr, expr, ... [as [Type]]" list sugar (§3.3).
func (p *Parser) parseAreExpr() Expr {
	pos := p.cur.Pos
	var elems []Expr
	elems = append(elems, p.parseExpression())

	for p.match(token.Comma) {
		if p.isBindingStart() {
			break // binding separator, not trailing comma
		}
		if p.at(token.RBrace) || p.at(token.EOF) {
			p.errorf(p.cur.Pos, "trailing comma not permitted in are binding")
			break
		}
		elems = append(elems, p.parseExpression())
	}

	// Optional list-level type annotation: "as [Type]".
	var typeAnn *TypeExpr
	if p.at(token.As) {
		p.advance()
		typeAnn = p.parseTypeExpr()
	} else if last, ok := elems[len(elems)-1].(*AsExpr); ok && last.TypeExpr != nil && last.TypeExpr.ListElem != nil {
		// parseExpression greedily absorbs "as [Type]"; the trailing list
		// annotation belongs to the are-expression, not the last element.
		typeAnn = last.TypeExpr
		elems[len(elems)-1] = last.Value
	}

	return &AreExpr{Elements: elems, TypeAnnotation: typeAnn, Position: pos}
}

// isBindingOperator reports whether t is a binding operator token
// (any token that can follow a name in a binding).
func isBindingOperator(t token.Type) bool {
	switch t {
	case token.Is, token.Are, token.IsNot, token.IsNamed, token.IsNotNamed, token.IsType, token.IsNotType:
		return true
	}
	return false
}

// isBindingStart checks if the current position looks like a new binding.
func (p *Parser) isBindingStart() bool {
	if p.cur.Type == token.Ident && isBindingOperator(p.peek.Type) {
		return true
	}
	if p.cur.Type == token.Env && isBindingOperator(p.peek.Type) {
		return true
	}
	// Keyword escape: @keyword is/are (§9), requires 3-token lookahead.
	if p.cur.Type == token.At && token.IsKeyword(p.peek.Literal) && isBindingOperator(p.peek2.Type) {
		return true
	}
	return false
}

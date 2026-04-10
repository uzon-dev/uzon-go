// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package ast

import (
	"fmt"

	"github.com/uzon-dev/uzon-go/token"
)

// Parser parses UZON source tokens into an AST.
type Parser struct {
	lex  *token.Lexer
	cur  token.Token
	peek token.Token
	file string

	errors []error

	// noStringConcat suppresses adjacent string literal concatenation.
	// Set when parsing binding values inside function bodies so that the
	// return expression is not consumed as part of a preceding binding value.
	noStringConcat bool
}

// NewParser creates a new parser from source bytes.
func NewParser(src []byte, file string) *Parser {
	lex := token.NewLexer(src, file)
	p := &Parser{lex: lex, file: file}
	p.advance() // fill cur
	p.advance() // fill peek (cur now has first token)
	return p
}

// Parse parses a complete UZON document.
func (p *Parser) Parse() (*Document, error) {
	doc := &Document{Position: p.cur.Pos}
	doc.Bindings = p.parseBindings(token.EOF)
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
	p.cur = p.peek
	p.peek = p.lex.Next()
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
		p.errorf(pos, "expected binding name, got %v (%q)", p.cur.Type, p.cur.Literal)
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
	} else if p.at(token.Are) {
		p.advance()
		b.Value = p.parseAreExpr()
	} else {
		p.errorf(p.cur.Pos, "expected 'is' or 'are' after binding name %q, got %v", name, p.cur.Type)
		return nil
	}

	// Optional trailing "called Name" for type naming (§6).
	if p.at(token.Called) {
		p.advance()
		b.CalledName = p.parseName()
	}

	return b
}

// parseName reads an identifier name.
// Also allows contextual keywords "env" and "self" as binding names.
func (p *Parser) parseName() string {
	if p.at(token.Ident) {
		name := p.cur.Literal
		p.advance()
		return name
	}
	// Allow contextual keywords as binding names (e.g. "env is ...")
	if p.at(token.Env) || p.at(token.Self) {
		name := p.cur.Literal
		p.advance()
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
		if p.isBindingStart() || p.at(token.RBrace) || p.at(token.EOF) {
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
	}

	return &AreExpr{Elements: elems, TypeAnnotation: typeAnn, Position: pos}
}

// isBindingStart checks if the current position looks like a new binding.
func (p *Parser) isBindingStart() bool {
	if p.cur.Type == token.Ident && (p.peek.Type == token.Is || p.peek.Type == token.Are) {
		return true
	}
	// Contextual keywords as binding names.
	if (p.cur.Type == token.Env || p.cur.Type == token.Self) && (p.peek.Type == token.Is || p.peek.Type == token.Are) {
		return true
	}
	return false
}

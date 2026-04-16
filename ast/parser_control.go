// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package ast

import (
	"github.com/uzon-dev/uzon-go/token"
)

// parseIfExpr parses "if cond then a else b" (§5.9).
func (p *Parser) parseIfExpr() Expr {
	pos := p.cur.Pos
	p.expect(token.If)
	cond := p.parseExpression()
	p.expect(token.Then)
	then := p.parseExpression()
	p.expect(token.Else)
	els := p.parseExpression()
	return &IfExpr{Cond: cond, Then: then, Else: els, Position: pos}
}

// parseCaseExpr parses case expressions (§5.10). Three forms:
//   - "case expr when val then ..."        — value matching
//   - "case type expr when T then ..."     — type dispatch
//   - "case named expr when tag then ..."  — variant dispatch
func (p *Parser) parseCaseExpr() Expr {
	pos := p.cur.Pos
	p.expect(token.Case)

	// Detect "case type" or "case named" form.
	var mode string
	if p.at(token.TypeKw) {
		mode = "type"
		p.advance()
	} else if p.at(token.Named) {
		mode = "named"
		p.advance()
	}

	scrutinee := p.parseExpression()

	var whens []*WhenClause
	for p.at(token.When) {
		wPos := p.cur.Pos
		p.advance()
		wc := &WhenClause{Position: wPos}
		switch mode {
		case "type":
			wc.TypeExpr = p.parseTypeExpr()
		case "named":
			wc.VariantName = p.parseNameOrKeyword()
		default:
			wc.Value = p.parseExpression()
		}
		p.expect(token.Then)
		wc.Then = p.parseExpression()
		whens = append(whens, wc)
	}

	if len(whens) == 0 {
		p.errorf(pos, "case expression requires at least one 'when' clause")
	}

	p.expect(token.Else)
	els := p.parseExpression()

	return &CaseExpr{Mode: mode, Scrutinee: scrutinee, Whens: whens, Else: els, Position: pos}
}

// parseStructImport parses 'struct "path"' (§7).
// §v0.8: interpolation is forbidden in struct import paths.
func (p *Parser) parseStructImport() Expr {
	pos := p.cur.Pos
	p.expect(token.Struct)
	if !p.at(token.StringLit) {
		p.errorf(p.cur.Pos, "expected string path after 'struct', got %v", p.cur.Type)
		return &StructImportExpr{Position: pos}
	}
	path := p.cur.Literal
	if containsUnescapedBrace(path) {
		p.errorf(p.cur.Pos, "struct import path must not contain interpolation")
	}
	p.advance()
	return &StructImportExpr{Path: path, Position: pos}
}

// parseFunctionExpr parses a function definition (§3.8):
// "function [params...] returns Type { [bindings...] body }"
func (p *Parser) parseFunctionExpr() Expr {
	pos := p.cur.Pos
	p.expect(token.Function)

	var params []*ParamExpr
	seenDefault := false
	if !p.at(token.Returns) {
		for {
			pPos := p.cur.Pos
			pName := p.parseName()
			if pName == "" {
				break
			}
			p.expect(token.As)
			pType := p.parseTypeExpr()
			var pDefault Expr
			if p.at(token.Default) {
				p.advance()
				pDefault = p.parseExpression()
				seenDefault = true
			} else if seenDefault {
				p.errorf(pPos, "required parameter %q after defaulted parameter", pName)
			}
			params = append(params, &ParamExpr{
				Name:     pName,
				TypeExpr: pType,
				Default:  pDefault,
				Position: pPos,
			})
			if !p.match(token.Comma) {
				break
			}
		}
	}

	p.expect(token.Returns)
	retType := p.parseTypeExpr()

	p.expect(token.LBrace)

	// Parse body: intermediate bindings then final expression.
	// §3.8: multiline string concatenation is suppressed in function bodies.
	var bindings []*Binding
	var body Expr

	saved := p.noStringConcat
	p.noStringConcat = true

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		// Detect intermediate bindings: only plain Is, not Are or composite
		// operators. Per §9: func_binding = name , "is" , expression.
		isBinding := false
		if (p.cur.Type == token.Ident || p.cur.Type == token.Env) &&
			p.peek.Type == token.Is {
			isBinding = true
		}
		if p.cur.Type == token.At && token.IsKeyword(p.peek.Literal) &&
			p.peek2.Type == token.Is {
			isBinding = true
		}

		if isBinding {
			b := p.parseBinding()
			if b != nil {
				bindings = append(bindings, b)
			}
			p.match(token.Comma)
		} else {
			body = p.parseExpression()
			break
		}
	}

	p.noStringConcat = saved

	p.expect(token.RBrace)

	if body == nil {
		p.errorf(pos, "function body must end with a return expression")
	}

	return &FunctionExpr{
		Params:     params,
		ReturnType: retType,
		Bindings:   bindings,
		Body:       body,
		Position:   pos,
	}
}

// parseTypeExpr parses a type expression (§6):
// named type (path), list type [T], tuple type (T, T), or null.
func (p *Parser) parseTypeExpr() *TypeExpr {
	pos := p.cur.Pos

	if p.at(token.Null) {
		p.advance()
		return &TypeExpr{IsNull: true, Position: pos}
	}

	// List type: [Type].
	if p.at(token.LBrack) {
		p.advance()
		elem := p.parseTypeExpr()
		p.expect(token.RBrack)
		return &TypeExpr{ListElem: elem, Position: pos}
	}

	// Tuple type: (Type, Type, ...).
	if p.at(token.LParen) {
		p.advance()
		var elems []*TypeExpr
		elems = append(elems, p.parseTypeExpr())
		for p.match(token.Comma) {
			if p.at(token.RParen) {
				break
			}
			elems = append(elems, p.parseTypeExpr())
		}
		p.expect(token.RParen)
		return &TypeExpr{TupleElems: elems, Position: pos}
	}

	// Named type: name or name.name.name.
	var path []string
	name := p.parseTypeName()
	if name == "" {
		// §11.2: when "is"/"are" appears where a type name is expected, the
		// preceding keyword was likely intended as a binding name.
		if (p.at(token.Is) || p.at(token.Are) || isBindingOperator(p.cur.Type)) &&
			token.IsKeyword(p.prev.Literal) {
			p.errorf(pos, "expected type name after %q, got %q; to use %q as a binding name, write @%s",
				p.prev.Literal, p.cur.Literal, p.prev.Literal, p.prev.Literal)
		} else {
			p.errorf(pos, "expected type name, got %v (%q)", p.cur.Type, p.cur.Literal)
		}
		return &TypeExpr{Position: pos}
	}
	path = append(path, name)
	for p.at(token.Dot) {
		p.advance()
		seg := p.parseTypeName()
		path = append(path, seg)
	}

	return &TypeExpr{Path: path, Position: pos}
}

func (p *Parser) parseTypeName() string {
	if p.at(token.Ident) {
		name := p.cur.Literal
		p.advance()
		return name
	}
	return ""
}

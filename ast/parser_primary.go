// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package ast

import (
	"strings"

	"github.com/uzon-dev/uzon-go/token"
)

// parsePrimary parses atomic expressions.
func (p *Parser) parsePrimary() Expr {
	pos := p.cur.Pos

	switch p.cur.Type {
	case token.IntLit, token.FloatLit:
		tok := p.cur
		p.advance()
		return &LiteralExpr{Token: tok}

	case token.StringLit:
		return p.parseStringOrInterpolation()

	case token.True, token.False, token.Null:
		tok := p.cur
		p.advance()
		return &LiteralExpr{Token: tok}

	case token.Inf, token.NaN:
		tok := p.cur
		p.advance()
		return &LiteralExpr{Token: tok}

	case token.Undefined:
		p.advance()
		return &UndefinedExpr{Position: pos}

	case token.Env:
		p.advance()
		return &EnvExpr{Position: pos}

	case token.LBrace:
		return p.parseStructLiteral()

	case token.LBrack:
		return p.parseListLiteral()

	case token.LParen:
		return p.parseTupleOrGroup()

	case token.If:
		return p.parseIfExpr()

	case token.Case:
		return p.parseCaseExpr()

	case token.Struct:
		return p.parseStructImport()

	case token.Enum:
		return p.parseEnumDecl()

	case token.Union:
		return p.parseUnionDecl()

	case token.Tagged:
		return p.parseTaggedUnionDecl()

	case token.Function:
		return p.parseFunctionExpr()

	case token.Ident:
		name := p.cur.Literal
		p.advance()
		return p.maybeVariantShorthand(name, pos)

	case token.At:
		// Keyword escape: @keyword as identifier reference (§9).
		if token.IsKeyword(p.peek.Literal) {
			name := p.peek.Literal
			p.advance() // consume @
			p.advance() // consume keyword
			return p.maybeVariantShorthand(name, pos)
		}
		p.errorf(pos, "unexpected token %v (%q)", p.cur.Type, p.cur.Literal)
		p.advance()
		return &LiteralExpr{Token: token.Token{Type: token.Illegal, Pos: pos}}

	default:
		p.errorf(pos, "unexpected token %v (%q)", p.cur.Type, p.cur.Literal)
		p.advance()
		return &LiteralExpr{Token: token.Token{Type: token.Illegal, Pos: pos}}
	}
}

// maybeVariantShorthand returns an identifier expression, or — when the next
// token starts a primary on the same physical line and is not the start of a
// new binding — wraps it in a VariantShorthandExpr per §3.7 v0.10. The
// shorthand parses right-recursively because a primary can itself be another
// VariantShorthandExpr.
//
// NEWLINE_SEP rule (spec §9): the inner primary MUST be on the same line as
// the variant name. A newline ends the shorthand and starts a new binding /
// expression. This prevents adjacent multi-line bindings from being glued
// together (e.g. `... else n` followed by `if ...` on the next line).
//
// A "(" on the same physical line as the identifier is reserved for the
// function-call postfix (parser_expr.go: parseCallOrAccess); the parser
// produces a CallExpr there even when the callee turns out to be a tagged
// variant. The evaluator re-interprets such call shapes as variant shorthand
// when type context demands it.
func (p *Parser) maybeVariantShorthand(name string, pos token.Pos) Expr {
	if !p.atPrimaryStart() {
		return &IdentExpr{Name: name, Position: pos}
	}
	if p.cur.Pos.Line != p.prev.Pos.Line {
		return &IdentExpr{Name: name, Position: pos}
	}
	if p.at(token.LParen) {
		return &IdentExpr{Name: name, Position: pos}
	}
	if p.isBindingStart() {
		return &IdentExpr{Name: name, Position: pos}
	}
	inner := p.parsePrimary()
	return &VariantShorthandExpr{Name: name, Inner: inner, Position: pos}
}

// atPrimaryStart reports whether the current token can begin a primary
// expression (§9 primary production). Used by variant_shorthand detection
// in parsePrimary.
func (p *Parser) atPrimaryStart() bool {
	switch p.cur.Type {
	case token.IntLit, token.FloatLit, token.StringLit,
		token.True, token.False, token.Null, token.Inf, token.NaN, token.Undefined,
		token.Env, token.LBrace, token.LBrack, token.LParen,
		token.If, token.Case, token.Struct, token.Enum, token.Union, token.Tagged, token.Function,
		token.Ident:
		return true
	case token.At:
		return token.IsKeyword(p.peek.Literal)
	}
	return false
}

// parseStringOrInterpolation handles string literals, multiline
// string concatenation, and string interpolation.
// §4.4.2: strings MUST appear on immediately adjacent physical lines —
// blank lines or comment-only lines between strings break the sequence.
func (p *Parser) parseStringOrInterpolation() Expr {
	pos := p.cur.Pos
	raw := p.cur.Literal
	prevLine := p.prev.Pos.Line
	if prevLine == 0 {
		prevLine = pos.Line
	} else {
		prevLine = pos.Line
	}
	p.advance()

	// Consecutive string literals form a multiline string (unless suppressed).
	// Per §4.4.2: must be on immediately adjacent physical lines, with no
	// intervening blank or comment-only lines.
	var lines []string
	lines = append(lines, raw)
	if !p.noStringConcat {
		for p.at(token.StringLit) && !p.isBindingStart() {
			if p.cur.Pos.Line != prevLine+1 {
				break // not on the immediately next physical line
			}
			prevLine = p.cur.Pos.Line
			lines = append(lines, p.cur.Literal)
			p.advance()
		}
	}

	var fullStr string
	if len(lines) > 1 {
		fullStr = strings.Join(lines, "\n")
	} else {
		fullStr = raw
	}

	// Check for interpolation markers (unescaped '{').
	if !containsUnescapedBrace(fullStr) {
		resolved := resolveStringEscapes(fullStr)
		return &LiteralExpr{Token: token.Token{Type: token.StringLit, Literal: resolved, Pos: pos}}
	}

	parts := p.parseInterpolationParts(fullStr, pos)
	if len(parts) == 1 && !parts[0].IsExpr {
		return &LiteralExpr{Token: token.Token{Type: token.StringLit, Literal: parts[0].Text, Pos: pos}}
	}
	return &InterpolatedStringExpr{Parts: parts, Position: pos}
}

// containsUnescapedBrace returns true if s contains a '{' not preceded by '\'.
func containsUnescapedBrace(s string) bool {
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' {
			i++ // skip escaped char
			continue
		}
		if runes[i] == '{' {
			return true
		}
	}
	return false
}

// resolveStringEscapes resolves preserved \\ → \ and \{ → { in token literals.
func resolveStringEscapes(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var sb strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			if next == '\\' || next == '{' {
				sb.WriteRune(next)
				i++
				continue
			}
		}
		sb.WriteRune(runes[i])
	}
	return sb.String()
}

// parseInterpolationParts splits a string into literal and expression parts.
// Handles nested string literals and brace depth inside interpolations (§4.4.1).
func (p *Parser) parseInterpolationParts(s string, pos token.Pos) []StringPart {
	var parts []StringPart
	var text strings.Builder
	runes := []rune(s)
	i := 0

	for i < len(runes) {
		if runes[i] == '\\' && i+1 < len(runes) && (runes[i+1] == '{' || runes[i+1] == '\\') {
			// Escaped brace or backslash — emit as literal character.
			text.WriteRune(runes[i+1])
			i += 2
			continue
		}
		if runes[i] == '{' {
			if text.Len() > 0 {
				parts = append(parts, StringPart{Text: text.String()})
				text.Reset()
			}
			i++ // skip '{'
			depth := 1
			var exprStr strings.Builder
			for i < len(runes) && depth > 0 {
				if runes[i] == '"' {
					// Skip nested string literal inside interpolation.
					exprStr.WriteRune(runes[i])
					i++
					for i < len(runes) && runes[i] != '"' {
						if runes[i] == '\\' && i+1 < len(runes) {
							exprStr.WriteRune(runes[i])
							i++
							exprStr.WriteRune(runes[i])
							i++
						} else {
							exprStr.WriteRune(runes[i])
							i++
						}
					}
					if i < len(runes) {
						exprStr.WriteRune(runes[i]) // closing "
						i++
					}
				} else if runes[i] == '{' {
					depth++
					exprStr.WriteRune(runes[i])
					i++
				} else if runes[i] == '}' {
					depth--
					if depth == 0 {
						break
					}
					exprStr.WriteRune(runes[i])
					i++
				} else {
					exprStr.WriteRune(runes[i])
					i++
				}
			}
			if i < len(runes) {
				i++ // skip '}'
			}
			subParser := NewParser([]byte(exprStr.String()), pos.File)
			expr := subParser.parseExpression()
			parts = append(parts, StringPart{IsExpr: true, Expr: expr})
		} else {
			text.WriteRune(runes[i])
			i++
		}
	}

	if text.Len() > 0 {
		parts = append(parts, StringPart{Text: text.String()})
	}

	return parts
}

func (p *Parser) parseStructLiteral() *StructExpr {
	pos := p.cur.Pos
	p.expect(token.LBrace)
	fields := p.parseBindings(token.RBrace)
	p.expect(token.RBrace)
	return &StructExpr{Fields: fields, Position: pos}
}

func (p *Parser) parseListLiteral() Expr {
	pos := p.cur.Pos
	p.expect(token.LBrack)
	var elems []Expr
	if !p.at(token.RBrack) {
		elems = append(elems, p.parseExpression())
		for p.match(token.Comma) {
			if p.at(token.RBrack) {
				break
			}
			elems = append(elems, p.parseExpression())
		}
	}
	p.expect(token.RBrack)
	return &ListExpr{Elements: elems, Position: pos}
}

// parseTupleOrGroup distinguishes (expr) grouping from (expr,) or (a, b) tuples.
func (p *Parser) parseTupleOrGroup() Expr {
	pos := p.cur.Pos
	p.expect(token.LParen)

	if p.at(token.RParen) {
		p.advance()
		return &TupleExpr{Elements: nil, Position: pos} // empty tuple ()
	}

	first := p.parseExpression()

	if p.at(token.RParen) {
		p.advance()
		// Mark a wrapped AsExpr so the §3.4.1/§9 lift rule in are-bindings
		// can tell `(x as T)` apart from a bare `x as T`.
		if a, ok := first.(*AsExpr); ok {
			a.Parenthesized = true
		}
		return first // grouping: (expr)
	}

	// Tuple: (expr,) or (a, b, ...).
	var elems []Expr
	elems = append(elems, first)
	if p.match(token.Comma) {
		if !p.at(token.RParen) {
			elems = append(elems, p.parseExpression())
			for p.match(token.Comma) {
				if p.at(token.RParen) {
					break
				}
				elems = append(elems, p.parseExpression())
			}
		}
	}

	p.expect(token.RParen)
	return &TupleExpr{Elements: elems, Position: pos}
}

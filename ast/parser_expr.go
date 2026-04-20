// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package ast

import (
	"github.com/uzon-dev/uzon-go/token"
)

// --- Expression Precedence (lowest to highest) ---
// or else → or → and → not → is/is not/is named → in → < <= > >= →
// ++ → + - → * / % ** → unary - → ^ → from/named → as/to →
// with/plus → call/member → primary

// parseExpression parses the full expression at "or else" level.
func (p *Parser) parseExpression() Expr {
	return p.parseOrElse()
}

func (p *Parser) parseOrElse() Expr {
	left := p.parseOr()
	for p.at(token.OrElse) {
		pos := p.cur.Pos
		p.advance()
		right := p.parseOr()
		// §4.5: literal `undefined` is not permitted as an `or else` operand.
		if _, isLeftUndef := left.(*UndefinedExpr); isLeftUndef {
			p.errorf(left.Pos(), "literal 'undefined' is not permitted as 'or else' operand")
		}
		if _, isRightUndef := right.(*UndefinedExpr); isRightUndef {
			p.errorf(right.Pos(), "literal 'undefined' is not permitted as 'or else' operand")
		}
		left = &BinaryExpr{Op: token.OrElse, Left: left, Right: right, Position: pos}
	}
	return left
}

func (p *Parser) parseOr() Expr {
	left := p.parseAnd()
	for p.at(token.Or) {
		pos := p.cur.Pos
		p.advance()
		right := p.parseAnd()
		left = &BinaryExpr{Op: token.Or, Left: left, Right: right, Position: pos}
	}
	return left
}

func (p *Parser) parseAnd() Expr {
	left := p.parseNot()
	for p.at(token.And) {
		pos := p.cur.Pos
		p.advance()
		right := p.parseNot()
		left = &BinaryExpr{Op: token.And, Left: left, Right: right, Position: pos}
	}
	return left
}

func (p *Parser) parseNot() Expr {
	if p.at(token.Not) {
		pos := p.cur.Pos
		p.advance()
		operand := p.parseNot() // right-recursive for chained "not not"
		return &UnaryExpr{Op: token.Not, Operand: operand, Position: pos}
	}
	return p.parseEquality()
}

// parseEquality handles is, is not, is named, is not named, is type, is not type.
func (p *Parser) parseEquality() Expr {
	left := p.parseMembership()

	switch p.cur.Type {
	case token.IsNamed:
		pos := p.cur.Pos
		p.advance()
		variant := p.parseNameOrKeyword()
		return &IsNamedExpr{Value: left, Variant: variant, Position: pos}
	case token.IsNotNamed:
		pos := p.cur.Pos
		p.advance()
		variant := p.parseNameOrKeyword()
		return &IsNamedExpr{Value: left, Variant: variant, Negated: true, Position: pos}
	case token.IsType:
		pos := p.cur.Pos
		p.advance()
		te := p.parseTypeExpr()
		return &IsTypeExpr{Value: left, TypeExpr: te, Position: pos}
	case token.IsNotType:
		pos := p.cur.Pos
		p.advance()
		te := p.parseTypeExpr()
		return &IsTypeExpr{Value: left, TypeExpr: te, Negated: true, Position: pos}
	case token.IsNot:
		pos := p.cur.Pos
		p.advance()
		right := p.parseMembership()
		return &BinaryExpr{Op: token.IsNot, Left: left, Right: right, Position: pos}
	case token.Is:
		pos := p.cur.Pos
		p.advance()
		right := p.parseMembership()
		return &BinaryExpr{Op: token.Is, Left: left, Right: right, Position: pos}
	}

	return left
}

func (p *Parser) parseMembership() Expr {
	left := p.parseRelational()
	if p.at(token.In) {
		pos := p.cur.Pos
		p.advance()
		right := p.parseRelational()
		return &BinaryExpr{Op: token.In, Left: left, Right: right, Position: pos}
	}
	return left
}

func (p *Parser) parseRelational() Expr {
	left := p.parseConcatenation()
	switch p.cur.Type {
	case token.Lt, token.LtEq, token.Gt, token.GtEq:
		pos := p.cur.Pos
		op := p.cur.Type
		p.advance()
		right := p.parseConcatenation()
		return &BinaryExpr{Op: op, Left: left, Right: right, Position: pos}
	}
	return left
}

func (p *Parser) parseConcatenation() Expr {
	left := p.parseAddition()
	for p.at(token.PlusPlus) {
		pos := p.cur.Pos
		p.advance()
		right := p.parseAddition()
		left = &BinaryExpr{Op: token.PlusPlus, Left: left, Right: right, Position: pos}
	}
	return left
}

func (p *Parser) parseAddition() Expr {
	left := p.parseMultiplication()
	for p.at(token.Plus) || p.at(token.Minus) {
		pos := p.cur.Pos
		op := p.cur.Type
		p.advance()
		right := p.parseMultiplication()
		left = &BinaryExpr{Op: op, Left: left, Right: right, Position: pos}
	}
	return left
}

func (p *Parser) parseMultiplication() Expr {
	left := p.parseUnary()
	for p.at(token.Star) || p.at(token.Slash) || p.at(token.Percent) || p.at(token.StarStar) {
		pos := p.cur.Pos
		op := p.cur.Type
		p.advance()
		right := p.parseUnary()
		left = &BinaryExpr{Op: op, Left: left, Right: right, Position: pos}
	}
	return left
}

func (p *Parser) parseUnary() Expr {
	if p.at(token.Minus) {
		pos := p.cur.Pos
		p.advance()
		operand := p.parsePower()
		return &UnaryExpr{Op: token.Minus, Operand: operand, Position: pos}
	}
	return p.parsePower()
}

func (p *Parser) parsePower() Expr {
	base := p.parseTypeDecl()
	if p.at(token.Caret) {
		pos := p.cur.Pos
		p.advance()
		exp := p.parseUnary() // right-associative
		return &BinaryExpr{Op: token.Caret, Left: base, Right: exp, Position: pos}
	}
	return base
}

func (p *Parser) parseTypeDecl() Expr {
	left := p.parseTypeAnnotation()
	return p.applyTypeDeclSuffix(left)
}

// applyTypeDeclSuffix checks for "from" (enum/union) or "named" (tagged union)
// suffixes and applies them.
func (p *Parser) applyTypeDeclSuffix(left Expr) Expr {
	if p.at(token.From) {
		return p.parseFromClause(left)
	}
	if p.at(token.Named) {
		return p.parseNamedClause(left)
	}
	return left
}

// continueExprFromTypeDecl continues parsing from the type-decl level
// with a pre-built left operand. Used for binding decomposition (§9).
func (p *Parser) continueExprFromTypeDecl(left Expr) Expr {
	return p.applyTypeDeclSuffix(left)
}

// continueFromAndLevel continues parsing and/or/or-else operators
// with a pre-built left operand. Used when "is not" consumes the "not".
func (p *Parser) continueFromAndLevel(left Expr) Expr {
	for p.at(token.And) {
		pos := p.cur.Pos
		p.advance()
		right := p.parseNot()
		left = &BinaryExpr{Op: token.And, Left: left, Right: right, Position: pos}
	}
	for p.at(token.Or) {
		pos := p.cur.Pos
		p.advance()
		right := p.parseAnd()
		left = &BinaryExpr{Op: token.Or, Left: left, Right: right, Position: pos}
	}
	for p.at(token.OrElse) {
		pos := p.cur.Pos
		p.advance()
		right := p.parseOr()
		left = &BinaryExpr{Op: token.OrElse, Left: left, Right: right, Position: pos}
	}
	return left
}

// parseFromClause parses "from union type, type, ..." or "from variant, variant, ...".
func (p *Parser) parseFromClause(value Expr) Expr {
	pos := p.cur.Pos
	p.advance() // consume "from"

	// "from union" → untagged union (§3.6).
	if p.at(token.Union) {
		p.advance()
		var types []*TypeExpr
		types = append(types, p.parseTypeExpr())
		for p.match(token.Comma) {
			if p.isBindingStart() || p.at(token.RBrace) || p.at(token.RParen) || p.at(token.RBrack) || p.at(token.EOF) {
				p.errorf(p.prev.Pos, "trailing comma not permitted in from-union member list (§8)")
				break
			}
			types = append(types, p.parseTypeExpr())
		}
		return &UnionExpr{Value: value, MemberTypes: types, Position: pos}
	}

	// "from variant, variant, ..." → enum (§3.5).
	var variants []string
	variants = append(variants, p.parseNameOrKeyword())
	for p.match(token.Comma) {
		if p.isBindingStart() || p.at(token.RBrace) || p.at(token.RParen) || p.at(token.RBrack) || p.at(token.EOF) || p.at(token.Called) {
			p.errorf(p.prev.Pos, "trailing comma not permitted in from-variant list (§8)")
			break
		}
		variants = append(variants, p.parseNameOrKeyword())
	}

	return &FromExpr{Value: value, Variants: variants, Position: pos}
}

// parseNamedClause parses "named tag [from variant as type, ...]" (§3.7).
func (p *Parser) parseNamedClause(value Expr) Expr {
	pos := p.cur.Pos
	p.advance() // consume "named"
	tag := p.parseNameOrKeyword()

	var variants []*TaggedVariantExpr
	if p.at(token.From) {
		p.advance()
		for {
			vPos := p.cur.Pos
			vName := p.parseNameOrKeyword()
			p.expect(token.As)
			vType := p.parseTypeExpr()
			variants = append(variants, &TaggedVariantExpr{Name: vName, TypeExpr: vType, Position: vPos})
			if !p.match(token.Comma) {
				break
			}
			if p.isBindingStart() || p.at(token.RBrace) || p.at(token.RParen) || p.at(token.RBrack) || p.at(token.EOF) || p.at(token.Called) {
				break
			}
		}
	}

	return &NamedExpr{Value: value, Tag: tag, Variants: variants, Position: pos}
}

// parseTypeAnnotation handles "as Type" and "as Type to Type" chaining (§5.11, §6.1).
func (p *Parser) parseTypeAnnotation() Expr {
	left := p.parseStructOverride()
	if p.at(token.As) {
		pos := p.cur.Pos
		p.advance()
		typeExpr := p.parseTypeExpr()
		left = &AsExpr{Value: left, TypeExpr: typeExpr, Position: pos}
	}
	// Allow "as Type to Type" chaining (§5.11).
	if p.at(token.To) {
		pos := p.cur.Pos
		p.advance()
		typeExpr := p.parseTypeExpr()
		left = &ToExpr{Value: left, TypeExpr: typeExpr, Position: pos}
	}
	return left
}

// parseStructOverride handles "with { ... }" and "plus { ... }" (§3.2.1, §3.2.2).
func (p *Parser) parseStructOverride() Expr {
	left := p.parseConversion()
	if p.at(token.With) {
		pos := p.cur.Pos
		p.advance()
		override := p.parseStructLiteral()
		return &WithExpr{Base: left, Override: override, Position: pos}
	}
	if p.at(token.PlusKw) {
		pos := p.cur.Pos
		p.advance()
		ext := p.parseStructLiteral()
		return &PlusExpr{Base: left, Extension: ext, Position: pos}
	}
	return left
}

// parseConversion handles standalone "to Type" (§5.5).
func (p *Parser) parseConversion() Expr {
	left := p.parseCallOrAccess()
	if p.at(token.To) {
		pos := p.cur.Pos
		p.advance()
		typeExpr := p.parseTypeExpr()
		return &ToExpr{Value: left, TypeExpr: typeExpr, Position: pos}
	}
	return left
}

// parseCallOrAccess handles member access (.name) and function calls (args...).
func (p *Parser) parseCallOrAccess() Expr {
	left := p.parsePrimary()
	for {
		if p.at(token.Dot) {
			pos := p.cur.Pos
			p.advance()
			member := p.parseMemberName()
			left = &MemberExpr{Object: left, Member: member, Position: pos}
		} else if p.at(token.LParen) && p.cur.Pos.Line == p.prev.Pos.Line {
			pos := p.cur.Pos
			p.advance()
			var args []Expr
			if !p.at(token.RParen) {
				args = append(args, p.parseExpression())
				for p.match(token.Comma) {
					if p.at(token.RParen) {
						break
					}
					args = append(args, p.parseExpression())
				}
			}
			p.expect(token.RParen)
			left = &CallExpr{Func: left, Args: args, Position: pos}
		} else {
			break
		}
	}
	return left
}

// parseMemberName reads a member access name (identifier, integer index,
// or keyword).
func (p *Parser) parseMemberName() string {
	if p.at(token.IntLit) {
		lit := p.cur.Literal
		p.advance()
		return lit
	}
	if p.at(token.Ident) {
		name := p.cur.Literal
		p.advance()
		return name
	}
	// Allow @keyword for member access.
	if p.at(token.At) {
		p.advance()
		if token.IsKeyword(p.cur.Literal) {
			name := p.cur.Literal
			p.advance()
			return name
		}
	}
	// Allow keywords as member names.
	if token.IsKeyword(p.cur.Literal) {
		name := p.cur.Literal
		p.advance()
		return name
	}
	p.errorf(p.cur.Pos, "expected member name, got %v (%q)", p.cur.Type, p.cur.Literal)
	return ""
}

// parseMemberAccess parses primary followed by dot-access chains only
// (no function calls). Used for "is of expr" parsing.
func (p *Parser) parseMemberAccess() Expr {
	left := p.parsePrimary()
	for p.at(token.Dot) {
		pos := p.cur.Pos
		p.advance()
		member := p.parseMemberName()
		left = &MemberExpr{Object: left, Member: member, Position: pos}
	}
	return left
}

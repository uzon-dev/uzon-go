// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package ast

import (
	"fmt"
	"strings"

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

// --- Expression Precedence (lowest to highest) ---
// or else → or → and → not → is/is not/is named → in → < <= > >= →
// ++ → + - → * / % ** → unary - → ^ → from/named → as/to →
// with/extends → call/member → primary

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

// parseEquality handles is, is not, is named, is not named.
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
	case token.IsNot:
		// §11: chained is not — bare ident looks like a new binding.
		if id, ok := left.(*IdentExpr); ok {
			p.errorf(p.cur.Pos, "chained 'is not' is not allowed; use 'self.%s is not ...' for inequality", id.Name)
		}
		pos := p.cur.Pos
		p.advance()
		right := p.parseMembership()
		return &BinaryExpr{Op: token.IsNot, Left: left, Right: right, Position: pos}
	case token.Is:
		// §11: chained is — bare ident looks like a new binding.
		if id, ok := left.(*IdentExpr); ok {
			p.errorf(p.cur.Pos, "chained 'is' is not allowed; use 'self.%s is ...' for equality", id.Name)
		}
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
				p.errorf(p.cur.Pos, "trailing comma not permitted in union member list")
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
			p.errorf(p.cur.Pos, "trailing comma not permitted in enum variant list")
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
				p.errorf(p.cur.Pos, "trailing comma not permitted in tagged union variant list")
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

// parseStructOverride handles "with { ... }" and "extends { ... }" (§3.2.1, §3.2.2).
func (p *Parser) parseStructOverride() Expr {
	left := p.parseConversion()
	if p.at(token.With) {
		pos := p.cur.Pos
		p.advance()
		override := p.parseStructLiteral()
		return &WithExpr{Base: left, Override: override, Position: pos}
	}
	if p.at(token.Extends) {
		pos := p.cur.Pos
		p.advance()
		ext := p.parseStructLiteral()
		return &ExtendsExpr{Base: left, Extension: ext, Position: pos}
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
		} else if p.at(token.LParen) {
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

	case token.Self:
		p.advance()
		return &SelfExpr{Position: pos}

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

	case token.Function:
		return p.parseFunctionExpr()

	case token.Ident:
		name := p.cur.Literal
		p.advance()
		return &IdentExpr{Name: name, Position: pos}

	default:
		p.errorf(pos, "unexpected token %v (%q)", p.cur.Type, p.cur.Literal)
		p.advance()
		return &LiteralExpr{Token: token.Token{Type: token.Illegal, Pos: pos}}
	}
}

// parseStringOrInterpolation handles string literals, multiline
// string concatenation, and string interpolation.
func (p *Parser) parseStringOrInterpolation() Expr {
	pos := p.cur.Pos
	raw := p.cur.Literal
	p.advance()

	// Consecutive string literals form a multiline string (unless suppressed).
	var lines []string
	lines = append(lines, raw)
	if !p.noStringConcat {
		for p.at(token.StringLit) && !p.isBindingStart() {
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

	// Check for interpolation markers.
	if !strings.Contains(fullStr, "{") {
		return &LiteralExpr{Token: token.Token{Type: token.StringLit, Literal: fullStr, Pos: pos}}
	}

	parts := p.parseInterpolationParts(fullStr, pos)
	if len(parts) == 1 && !parts[0].IsExpr {
		return &LiteralExpr{Token: token.Token{Type: token.StringLit, Literal: parts[0].Text, Pos: pos}}
	}
	return &InterpolatedStringExpr{Parts: parts, Position: pos}
}

// parseInterpolationParts splits a string into literal and expression parts.
// Handles nested string literals and brace depth inside interpolations (§4.4.1).
func (p *Parser) parseInterpolationParts(s string, pos token.Pos) []StringPart {
	var parts []StringPart
	var text strings.Builder
	runes := []rune(s)
	i := 0

	for i < len(runes) {
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

// parseCaseExpr parses "case expr when val then expr ... else expr" (§5.10).
func (p *Parser) parseCaseExpr() Expr {
	pos := p.cur.Pos
	p.expect(token.Case)
	scrutinee := p.parseExpression()

	var whens []*WhenClause
	for p.at(token.When) {
		wPos := p.cur.Pos
		p.advance()
		wc := &WhenClause{Position: wPos}
		if p.at(token.Named) {
			p.advance()
			wc.IsNamed = true
			wc.VariantName = p.parseNameOrKeyword()
		} else {
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

	return &CaseExpr{Scrutinee: scrutinee, Whens: whens, Else: els, Position: pos}
}

// parseStructImport parses 'struct "path"' (§7).
func (p *Parser) parseStructImport() Expr {
	pos := p.cur.Pos
	p.expect(token.Struct)
	if !p.at(token.StringLit) {
		p.errorf(p.cur.Pos, "expected string path after 'struct', got %v", p.cur.Type)
		return &StructImportExpr{Position: pos}
	}
	path := p.cur.Literal
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
	var bindings []*Binding
	var body Expr

	for !p.at(token.RBrace) && !p.at(token.EOF) {
		if p.cur.Type == token.Ident && p.peek.Type == token.Is {
			p.noStringConcat = true
			b := p.parseBinding()
			p.noStringConcat = false
			if b != nil {
				bindings = append(bindings, b)
			}
			p.match(token.Comma)
		} else {
			body = p.parseExpression()
			break
		}
	}

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
		p.errorf(pos, "expected type name, got %v (%q)", p.cur.Type, p.cur.Literal)
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

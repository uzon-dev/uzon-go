// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package token

import "unicode/utf8"

// scanMinusOrNegative decides whether "-" is a binary operator or the
// start of a negative literal. After a value-producing token, it is
// always binary subtraction. Otherwise, if followed by a digit or
// "inf"/"nan", it forms a negative literal.
func (l *Lexer) scanMinusOrNegative(pos Pos) Token {
	if l.prevTokenIsValue {
		l.advance()
		return Token{Type: Minus, Literal: "-", Pos: pos}
	}
	l.advance()
	if isDigit(l.ch) {
		return l.scanNegativeNumber(pos)
	}
	// Check for -inf or -nan.
	if l.ch == 'i' || l.ch == 'n' {
		word := l.peekWord()
		if word == "inf" || word == "nan" {
			l.advanceN(len(word))
			return Token{Type: FloatLit, Literal: "-" + word, Pos: pos}
		}
	}
	return Token{Type: Minus, Literal: "-", Pos: pos}
}

func (l *Lexer) scanNegativeNumber(pos Pos) Token {
	tok := l.scanNumber(Pos{File: pos.File, Line: pos.Line, Column: pos.Column + 1, Offset: pos.Offset + 1})
	tok.Literal = "-" + tok.Literal
	tok.Pos = pos
	return tok
}

// scanNumber scans an integer or float literal.
// Supports decimal, hex (0x), octal (0o), binary (0b), underscore
// separators, decimal points, and exponents. Per §2.3, if the resulting
// numeric token is immediately followed by identifier-continue runes
// (e.g. `1st`, `0xZZ`), the whole span is reinterpreted as an identifier.
func (l *Lexer) scanNumber(pos Pos) Token {
	start := l.pos - l.chSize

	if l.ch == '0' {
		l.advance()
		switch l.ch {
		case 'x', 'X':
			l.advance()
			l.scanHexDigits()
			if isIdentContinue(l.ch) {
				return l.continueAsIdent(pos, start)
			}
			return Token{Type: IntLit, Literal: string(l.src[start:l.litEnd()]), Pos: pos}
		case 'o', 'O':
			l.advance()
			l.scanOctDigits()
			if isIdentContinue(l.ch) {
				return l.continueAsIdent(pos, start)
			}
			return Token{Type: IntLit, Literal: string(l.src[start:l.litEnd()]), Pos: pos}
		case 'b', 'B':
			l.advance()
			l.scanBinDigits()
			if isIdentContinue(l.ch) {
				return l.continueAsIdent(pos, start)
			}
			return Token{Type: IntLit, Literal: string(l.src[start:l.litEnd()]), Pos: pos}
		}
	} else {
		l.scanDecDigits()
	}

	isFloat := false
	// Decimal point must be followed by a digit to distinguish from
	// member access (e.g. "x.field" vs "3.14").
	if l.ch == '.' && l.pos < len(l.src) {
		next, _ := utf8.DecodeRune(l.src[l.pos:])
		if isDigit(next) {
			isFloat = true
			l.advance() // consume '.'
			l.scanDecDigits()
		}
	}

	// Exponent part.
	if l.ch == 'e' || l.ch == 'E' {
		isFloat = true
		l.advance()
		if l.ch == '+' || l.ch == '-' {
			l.advance()
		}
		l.scanDecDigits()
	}

	if isIdentContinue(l.ch) {
		return l.continueAsIdent(pos, start)
	}

	lit := string(l.src[start:l.litEnd()])
	if isFloat {
		return Token{Type: FloatLit, Literal: lit, Pos: pos}
	}
	return Token{Type: IntLit, Literal: lit, Pos: pos}
}

// continueAsIdent extends the current scan position to the next token
// boundary and returns the whole span as an identifier. Used when a
// digit-starting span does not fully match numeric grammar (§2.3).
func (l *Lexer) continueAsIdent(pos Pos, start int) Token {
	for isIdentContinue(l.ch) {
		l.advance()
	}
	lit := string(l.src[start:l.litEnd()])
	if tt, ok := Keywords[lit]; ok {
		return Token{Type: tt, Literal: lit, Pos: pos}
	}
	return Token{Type: Ident, Literal: lit, Pos: pos}
}

func (l *Lexer) scanDecDigits() {
	for isDigit(l.ch) || l.ch == '_' {
		l.advance()
	}
}

func (l *Lexer) scanHexDigits() {
	for isHexDigit(l.ch) || l.ch == '_' {
		l.advance()
	}
}

func (l *Lexer) scanOctDigits() {
	for (l.ch >= '0' && l.ch <= '7') || l.ch == '_' {
		l.advance()
	}
}

func (l *Lexer) scanBinDigits() {
	for l.ch == '0' || l.ch == '1' || l.ch == '_' {
		l.advance()
	}
}

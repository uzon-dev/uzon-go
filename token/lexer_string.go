// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package token

import (
	"strings"
	"unicode"
)

// scanString scans a double-quoted string literal with escape processing.
// Supports standard escapes (\n, \t, \r, \\, \", \0, \{), hex escapes
// (\xHH restricted to 0x00–0x7F per §4.4), Unicode escapes (\u{HHHHHH}
// for valid Unicode scalar values, 1–6 hex digits per §4.4), and string
// interpolation ({expr}) with proper brace depth tracking for nested
// strings (§4.4.1).
//
// The \\ and \{ escapes are preserved in the token literal (as two-char
// sequences) so the parser can distinguish escaped braces from interpolation.
// All other escapes are resolved to their character values.
func (l *Lexer) scanString(pos Pos) Token {
	var sb strings.Builder
	l.advance() // consume opening "
	startLine := pos.Line

	for l.ch >= 0 && l.ch != '"' {
		if l.ch == '\\' {
			escPos := l.curPos()
			l.advance()
			switch l.ch {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteString("\\\\")
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case '0':
				sb.WriteByte(0)
			case '{':
				sb.WriteString("\\{")
			case 'x':
				// \xHH — restricted to 0x00–0x7F per §4.4.
				l.advance()
				h1, ok1 := safeHexVal(l.ch)
				if !ok1 {
					l.errorf(escPos, "invalid \\x escape: requires two hex digits")
					sb.WriteRune(unicode.ReplacementChar)
					continue // don't consume non-hex char
				}
				l.advance()
				h2, ok2 := safeHexVal(l.ch)
				if !ok2 {
					l.errorf(escPos, "invalid \\x escape: requires two hex digits")
					sb.WriteRune(unicode.ReplacementChar)
					continue // don't consume non-hex char
				}
				val := h1<<4 | h2
				if val > 0x7F {
					l.errorf(escPos, "\\x escape value 0x%x out of range (0x00-0x7F)", val)
					sb.WriteRune(unicode.ReplacementChar)
				} else {
					sb.WriteByte(byte(val))
				}
			case 'u':
				// \u{HHHHHH} — 1–6 hex digits, valid Unicode scalar value.
				l.advance() // consume '{'
				if l.ch != '{' {
					l.errorf(escPos, "invalid \\u escape: expected '{'")
					sb.WriteRune(unicode.ReplacementChar)
					continue
				}
				var code rune
				digits := 0
				badDigit := false
				l.advance()
				for l.ch != '}' && l.ch >= 0 {
					hv, ok := safeHexVal(l.ch)
					if !ok {
						badDigit = true
					} else {
						code = code*16 + rune(hv)
					}
					digits++
					l.advance()
				}
				if badDigit {
					l.errorf(escPos, "invalid \\u escape: non-hex digit")
					sb.WriteRune(unicode.ReplacementChar)
				} else if digits < 1 || digits > 6 {
					l.errorf(escPos, "invalid \\u escape: must have 1-6 hex digits")
					sb.WriteRune(unicode.ReplacementChar)
				} else if code > 0x10FFFF {
					l.errorf(escPos, "\\u escape value 0x%x out of Unicode range (max 0x10FFFF)", code)
					sb.WriteRune(unicode.ReplacementChar)
				} else if code >= 0xD800 && code <= 0xDFFF {
					l.errorf(escPos, "\\u escape value 0x%x is a surrogate (forbidden)", code)
					sb.WriteRune(unicode.ReplacementChar)
				} else {
					sb.WriteRune(code)
				}
			default:
				// Unrecognized escape — record error and preserve literally.
				l.errorf(escPos, "invalid escape sequence \\%c", l.ch)
				sb.WriteByte('\\')
				sb.WriteRune(l.ch)
			}
		} else if l.ch == '{' {
			// String interpolation: track brace depth and consume the full
			// expression including any nested "..." strings (§4.4.1).
			interpPos := l.curPos()
			sb.WriteRune(l.ch)
			l.advance()
			depth := 1
			for l.ch >= 0 && depth > 0 {
				if l.ch == '\\' {
					// Escape inside interpolation expression.
					l.advance()
					if l.ch == '"' {
						sb.WriteRune('"')
						l.advance()
					} else if l.ch == '{' {
						sb.WriteRune('{')
						l.advance()
					} else {
						sb.WriteRune('\\')
						sb.WriteRune(l.ch)
						l.advance()
					}
				} else if l.ch == '{' {
					depth++
					sb.WriteRune(l.ch)
					l.advance()
				} else if l.ch == '}' {
					depth--
					if depth > 0 {
						sb.WriteRune(l.ch)
						l.advance()
					} else {
						sb.WriteRune(l.ch)
						// don't advance — outer loop will
					}
				} else if l.ch == '"' {
					// Nested string inside interpolation.
					sb.WriteRune(l.ch)
					l.advance()
					for l.ch >= 0 && l.ch != '"' {
						if l.ch == '\\' {
							sb.WriteRune(l.ch)
							l.advance()
							if l.ch >= 0 {
								sb.WriteRune(l.ch)
								l.advance()
							}
						} else {
							sb.WriteRune(l.ch)
							l.advance()
						}
					}
					if l.ch == '"' {
						sb.WriteRune(l.ch)
						l.advance()
					}
				} else {
					sb.WriteRune(l.ch)
					l.advance()
				}
			}
			if depth > 0 {
				l.errorf(interpPos, "unterminated string interpolation")
			}
		} else {
			sb.WriteRune(l.ch)
		}
		l.advance()
	}

	if l.ch == '"' {
		l.advance() // consume closing "
	} else {
		l.errorf(pos, "unterminated string literal starting at line %d", startLine)
	}

	return Token{Type: StringLit, Literal: sb.String(), Pos: pos}
}

// scanQuotedIdent scans a single-quoted identifier ('Content-Type').
// Per §2.3, if the unquoted content is a keyword, it remains a keyword.
// A quoted identifier MUST be closed by ' on the same physical line —
// a newline or EOF before the closing ' is a syntax error (§2.3).
func (l *Lexer) scanQuotedIdent(pos Pos) Token {
	l.advance() // consume opening '
	var sb strings.Builder
	for l.ch >= 0 && l.ch != '\'' && l.ch != '\n' {
		sb.WriteRune(l.ch)
		l.advance()
	}
	if l.ch == '\'' {
		l.advance() // consume closing '
	} else {
		l.errorf(pos, "unterminated quoted identifier")
	}
	name := sb.String()
	if tt, ok := Keywords[name]; ok {
		return Token{Type: tt, Literal: name, Pos: pos}
	}
	return Token{Type: Ident, Literal: name, Pos: pos}
}

// scanKeywordEscape scans an @-prefixed keyword escape (@is → Ident "is").
// Per §2.4, @keyword forces the keyword to be treated as an identifier.
// @ MUST be immediately followed by a keyword (no space). Failure is
// a syntax error.
func (l *Lexer) scanKeywordEscape(pos Pos) Token {
	l.advance() // consume @
	if !isIdentStart(l.ch) {
		l.errorf(pos, "@ must be immediately followed by a keyword (no space)")
		return Token{Type: At, Literal: "@", Pos: pos}
	}
	start := l.pos - l.chSize
	for isIdentContinue(l.ch) {
		l.advance()
	}
	name := string(l.src[start:l.litEnd()])
	if !IsKeyword(name) {
		l.errorf(pos, "@%s: %q is not a keyword (only keywords may be escaped)", name, name)
	}
	return Token{Type: Ident, Literal: name, Pos: pos}
}

// scanIdentOrKeyword scans an identifier or keyword, including
// composite keyword detection for "is not", "is named", "is not named",
// "is type", "is not type", and "or else".
func (l *Lexer) scanIdentOrKeyword(pos Pos) Token {
	start := l.pos - l.chSize
	for isIdentContinue(l.ch) {
		l.advance()
	}
	lit := string(l.src[start:l.litEnd()])

	// Detect composite keywords.
	if lit == "is" {
		return l.resolveIsComposite(pos, lit)
	}
	if lit == "or" {
		return l.resolveOrComposite(pos, lit)
	}
	if tt, ok := Keywords[lit]; ok {
		return Token{Type: tt, Literal: lit, Pos: pos}
	}
	return Token{Type: Ident, Literal: lit, Pos: pos}
}

// resolveIsComposite checks whether "is" is followed by "not", "named",
// or "not named" to form a composite operator token.
func (l *Lexer) resolveIsComposite(pos Pos, lit string) Token {
	saved := l.saveState()
	l.skipWhitespaceAndComments() // crosses newlines per §9

	if !isIdentStart(l.ch) {
		l.restoreState(saved)
		return Token{Type: Is, Literal: lit, Pos: pos}
	}

	word := l.peekWord()
	if word == "not" {
		l.advanceN(len(word))
		saved2 := l.saveState()
		l.skipWhitespaceAndComments() // crosses newlines per §9
		word2 := l.peekWord()
		if word2 == "named" {
			l.advanceN(len(word2))
			return Token{Type: IsNotNamed, Literal: "is not named", Pos: pos}
		}
		if word2 == "type" {
			l.advanceN(len(word2))
			return Token{Type: IsNotType, Literal: "is not type", Pos: pos}
		}
		l.restoreState(saved2)
		return Token{Type: IsNot, Literal: "is not", Pos: pos}
	}
	if word == "named" {
		l.advanceN(len(word))
		return Token{Type: IsNamed, Literal: "is named", Pos: pos}
	}
	if word == "type" {
		l.advanceN(len(word))
		return Token{Type: IsType, Literal: "is type", Pos: pos}
	}

	l.restoreState(saved)
	return Token{Type: Is, Literal: lit, Pos: pos}
}

// resolveOrComposite checks whether "or" is followed by "else" to
// form the "or else" composite operator.
func (l *Lexer) resolveOrComposite(pos Pos, lit string) Token {
	saved := l.saveState()
	l.skipWhitespaceAndComments() // crosses newlines per §9

	word := l.peekWord()
	if word == "else" {
		l.advanceN(len(word))
		return Token{Type: OrElse, Literal: "or else", Pos: pos}
	}

	l.restoreState(saved)
	return Token{Type: Or, Literal: lit, Pos: pos}
}

// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package token

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Lexer tokenizes UZON source text into a stream of tokens.
// It handles UTF-8 BOM stripping (§2.1), line comments (§2.2),
// composite operator detection (e.g. "is not" → IsNot), and
// context-sensitive minus sign disambiguation.
type Lexer struct {
	src  []byte
	file string

	pos    int  // current byte offset
	line   int  // 1-based line number
	col    int  // 1-based column (Unicode scalar count)
	ch     rune // current rune, -1 at EOF
	chSize int  // byte size of current rune

	// prevTokenIsValue tracks whether the previous non-comment token was a
	// value-producing token. Used to disambiguate unary minus from binary
	// subtraction: after a value token, "-" is always subtraction.
	prevTokenIsValue bool

	// peeked holds a lookahead buffer for composite operator detection
	// and the Peek method.
	peeked []Token
}

// NewLexer creates a new Lexer for the given source.
// A UTF-8 BOM at the start of src is silently stripped per §2.1.
func NewLexer(src []byte, file string) *Lexer {
	// Skip UTF-8 BOM (U+FEFF) per §2.1.
	if len(src) >= 3 && src[0] == 0xEF && src[1] == 0xBB && src[2] == 0xBF {
		src = src[3:]
	}
	l := &Lexer{
		src:  src,
		file: file,
		line: 1,
		col:  1,
	}
	l.advance()
	return l
}

// Next returns the next token from the source.
func (l *Lexer) Next() Token {
	if len(l.peeked) > 0 {
		tok := l.peeked[0]
		l.peeked = l.peeked[1:]
		l.prevTokenIsValue = isValueToken(tok.Type)
		return tok
	}
	tok := l.scan()
	l.prevTokenIsValue = isValueToken(tok.Type)
	return tok
}

// Peek returns the next token without consuming it.
func (l *Lexer) Peek() Token {
	if len(l.peeked) == 0 {
		l.peeked = append(l.peeked, l.scan())
	}
	return l.peeked[0]
}

// scan produces the next raw token from the source.
func (l *Lexer) scan() Token {
	l.skipWhitespaceAndComments()

	if l.ch < 0 {
		return Token{Type: EOF, Pos: l.curPos()}
	}

	pos := l.curPos()

	switch {
	case l.ch == '"':
		return l.scanString(pos)
	case l.ch == '\'':
		return l.scanQuotedIdent(pos)
	case l.ch == '@':
		return l.scanKeywordEscape(pos)
	case l.ch == '{':
		l.advance()
		return Token{Type: LBrace, Literal: "{", Pos: pos}
	case l.ch == '}':
		l.advance()
		return Token{Type: RBrace, Literal: "}", Pos: pos}
	case l.ch == '(':
		l.advance()
		return Token{Type: LParen, Literal: "(", Pos: pos}
	case l.ch == ')':
		l.advance()
		return Token{Type: RParen, Literal: ")", Pos: pos}
	case l.ch == '[':
		l.advance()
		return Token{Type: LBrack, Literal: "[", Pos: pos}
	case l.ch == ']':
		l.advance()
		return Token{Type: RBrack, Literal: "]", Pos: pos}
	case l.ch == ',':
		l.advance()
		return Token{Type: Comma, Literal: ",", Pos: pos}
	case l.ch == '.':
		l.advance()
		return Token{Type: Dot, Literal: ".", Pos: pos}
	case l.ch == '+':
		l.advance()
		if l.ch == '+' {
			l.advance()
			return Token{Type: PlusPlus, Literal: "++", Pos: pos}
		}
		return Token{Type: Plus, Literal: "+", Pos: pos}
	case l.ch == '*':
		l.advance()
		if l.ch == '*' {
			l.advance()
			return Token{Type: StarStar, Literal: "**", Pos: pos}
		}
		return Token{Type: Star, Literal: "*", Pos: pos}
	case l.ch == '/':
		l.advance()
		return Token{Type: Slash, Literal: "/", Pos: pos}
	case l.ch == '%':
		l.advance()
		return Token{Type: Percent, Literal: "%", Pos: pos}
	case l.ch == '^':
		l.advance()
		return Token{Type: Caret, Literal: "^", Pos: pos}
	case l.ch == '<':
		l.advance()
		if l.ch == '=' {
			l.advance()
			return Token{Type: LtEq, Literal: "<=", Pos: pos}
		}
		return Token{Type: Lt, Literal: "<", Pos: pos}
	case l.ch == '>':
		l.advance()
		if l.ch == '=' {
			l.advance()
			return Token{Type: GtEq, Literal: ">=", Pos: pos}
		}
		return Token{Type: Gt, Literal: ">", Pos: pos}
	case l.ch == '-':
		return l.scanMinusOrNegative(pos)
	default:
		if isDigit(l.ch) {
			return l.scanNumber(pos)
		}
		if isIdentStart(l.ch) {
			return l.scanIdentOrKeyword(pos)
		}
		// Unknown character → Illegal token.
		ch := l.ch
		l.advance()
		return Token{Type: Illegal, Literal: string(ch), Pos: pos}
	}
}

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
// separators, decimal points, and exponents.
func (l *Lexer) scanNumber(pos Pos) Token {
	start := l.pos - l.chSize

	if l.ch == '0' {
		l.advance()
		switch l.ch {
		case 'x', 'X':
			l.advance()
			l.scanHexDigits()
			return Token{Type: IntLit, Literal: string(l.src[start:l.litEnd()]), Pos: pos}
		case 'o', 'O':
			l.advance()
			l.scanOctDigits()
			return Token{Type: IntLit, Literal: string(l.src[start:l.litEnd()]), Pos: pos}
		case 'b', 'B':
			l.advance()
			l.scanBinDigits()
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

	lit := string(l.src[start:l.litEnd()])
	if isFloat {
		return Token{Type: FloatLit, Literal: lit, Pos: pos}
	}
	return Token{Type: IntLit, Literal: lit, Pos: pos}
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

// scanString scans a double-quoted string literal with escape processing.
// Supports standard escapes (\n, \t, \r, \\, \", \0, \{), hex escapes
// (\xHH restricted to 0x00–0x7F per §4.4), Unicode escapes (\u{HHHHHH}
// for valid Unicode scalar values, 1–6 hex digits per §4.4), and string
// interpolation ({expr}) with proper brace depth tracking for nested
// strings (§4.4.1).
func (l *Lexer) scanString(pos Pos) Token {
	var sb strings.Builder
	l.advance() // consume opening "

	for l.ch >= 0 && l.ch != '"' {
		if l.ch == '\\' {
			l.advance()
			switch l.ch {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case '0':
				sb.WriteByte(0)
			case '{':
				sb.WriteByte('{')
			case 'x':
				// \xHH — restricted to 0x00–0x7F per §4.4.
				l.advance()
				h1, ok1 := safeHexVal(l.ch)
				l.advance()
				h2, ok2 := safeHexVal(l.ch)
				if !ok1 || !ok2 {
					sb.WriteRune(unicode.ReplacementChar)
				} else {
					val := h1<<4 | h2
					if val > 0x7F {
						sb.WriteRune(unicode.ReplacementChar)
					} else {
						sb.WriteByte(byte(val))
					}
				}
			case 'u':
				// \u{HHHHHH} — 1–6 hex digits, valid Unicode scalar value.
				l.advance() // consume '{'
				var code rune
				digits := 0
				l.advance()
				for l.ch != '}' && l.ch >= 0 {
					hv, ok := safeHexVal(l.ch)
					if !ok {
						code = unicode.ReplacementChar
					} else {
						code = code*16 + rune(hv)
					}
					digits++
					l.advance()
				}
				if digits < 1 || digits > 6 || code > 0x10FFFF || (code >= 0xD800 && code <= 0xDFFF) {
					sb.WriteRune(unicode.ReplacementChar)
				} else {
					sb.WriteRune(code)
				}
			default:
				// Unrecognized escape — preserve literally.
				sb.WriteByte('\\')
				sb.WriteRune(l.ch)
			}
		} else if l.ch == '{' {
			// String interpolation: track brace depth and consume the full
			// expression including any nested "..." strings (§4.4.1).
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
		} else {
			sb.WriteRune(l.ch)
		}
		l.advance()
	}

	if l.ch == '"' {
		l.advance() // consume closing "
	}

	return Token{Type: StringLit, Literal: sb.String(), Pos: pos}
}

// scanQuotedIdent scans a single-quoted identifier ('Content-Type').
// Per §2.3, if the unquoted content is a keyword, it remains a keyword.
func (l *Lexer) scanQuotedIdent(pos Pos) Token {
	l.advance() // consume opening '
	var sb strings.Builder
	for l.ch >= 0 && l.ch != '\'' && l.ch != '\n' {
		sb.WriteRune(l.ch)
		l.advance()
	}
	if l.ch == '\'' {
		l.advance() // consume closing '
	}
	name := sb.String()
	if tt, ok := Keywords[name]; ok {
		return Token{Type: tt, Literal: name, Pos: pos}
	}
	return Token{Type: Ident, Literal: name, Pos: pos}
}

// scanKeywordEscape scans an @-prefixed keyword escape (@is → Ident "is").
// Per §2.4, @keyword forces the keyword to be treated as an identifier.
func (l *Lexer) scanKeywordEscape(pos Pos) Token {
	l.advance() // consume @
	if !isIdentStart(l.ch) {
		return Token{Type: At, Literal: "@", Pos: pos}
	}
	start := l.pos - l.chSize
	for isIdentContinue(l.ch) {
		l.advance()
	}
	name := string(l.src[start:l.litEnd()])
	return Token{Type: Ident, Literal: name, Pos: pos}
}

// scanIdentOrKeyword scans an identifier or keyword, including
// composite keyword detection for "is not", "is named", "is not named",
// and "or else".
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
	l.skipSpacesAndTabs()

	if !isIdentStart(l.ch) {
		l.restoreState(saved)
		return Token{Type: Is, Literal: lit, Pos: pos}
	}

	word := l.peekWord()
	if word == "not" {
		l.advanceN(len(word))
		saved2 := l.saveState()
		l.skipSpacesAndTabs()
		word2 := l.peekWord()
		if word2 == "named" {
			l.advanceN(len(word2))
			return Token{Type: IsNotNamed, Literal: "is not named", Pos: pos}
		}
		l.restoreState(saved2)
		return Token{Type: IsNot, Literal: "is not", Pos: pos}
	}
	if word == "named" {
		l.advanceN(len(word))
		return Token{Type: IsNamed, Literal: "is named", Pos: pos}
	}

	l.restoreState(saved)
	return Token{Type: Is, Literal: lit, Pos: pos}
}

// resolveOrComposite checks whether "or" is followed by "else" to
// form the "or else" composite operator.
func (l *Lexer) resolveOrComposite(pos Pos, lit string) Token {
	saved := l.saveState()
	l.skipSpacesAndTabs()

	word := l.peekWord()
	if word == "else" {
		l.advanceN(len(word))
		return Token{Type: OrElse, Literal: "or else", Pos: pos}
	}

	l.restoreState(saved)
	return Token{Type: Or, Literal: lit, Pos: pos}
}

// advance reads the next rune from the source, updating position tracking.
func (l *Lexer) advance() {
	if l.pos >= len(l.src) {
		l.ch = -1
		l.chSize = 0
		return
	}
	r, size := utf8.DecodeRune(l.src[l.pos:])
	l.ch = r
	l.chSize = size
	l.pos += size

	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
}

func (l *Lexer) advanceN(n int) {
	for i := 0; i < n; i++ {
		l.advance()
	}
}

// litEnd returns the byte offset just past the last consumed character.
// Used to slice the source for literal text extraction.
func (l *Lexer) litEnd() int {
	if l.ch >= 0 {
		return l.pos - l.chSize
	}
	return l.pos
}

func (l *Lexer) curPos() Pos {
	return Pos{
		File:   l.file,
		Line:   l.line,
		Column: l.col - 1, // col was already advanced past current char
		Offset: l.pos - l.chSize,
	}
}

// skipWhitespaceAndComments skips whitespace (space, tab, CR, LF) and
// line comments (// to end of line). Comments are treated as whitespace
// per §2.2.
func (l *Lexer) skipWhitespaceAndComments() {
	for {
		for l.ch == ' ' || l.ch == '\t' || l.ch == '\r' || l.ch == '\n' {
			l.advance()
		}
		if l.ch == '/' && l.pos < len(l.src) && l.src[l.pos] == '/' {
			for l.ch >= 0 && l.ch != '\n' {
				l.advance()
			}
			continue
		}
		break
	}
}

func (l *Lexer) skipSpacesAndTabs() {
	for l.ch == ' ' || l.ch == '\t' {
		l.advance()
	}
}

// peekWord reads an identifier-like word starting at the current position
// without advancing the lexer state.
func (l *Lexer) peekWord() string {
	start := l.pos - l.chSize
	i := start
	for i < len(l.src) {
		r, size := utf8.DecodeRune(l.src[i:])
		if !isIdentContinue(r) {
			break
		}
		i += size
	}
	return string(l.src[start:i])
}

// lexerState captures lexer state for backtracking during composite
// operator detection.
type lexerState struct {
	pos              int
	line             int
	col              int
	ch               rune
	chSize           int
	prevTokenIsValue bool
}

func (l *Lexer) saveState() lexerState {
	return lexerState{
		pos:              l.pos,
		line:             l.line,
		col:              l.col,
		ch:               l.ch,
		chSize:           l.chSize,
		prevTokenIsValue: l.prevTokenIsValue,
	}
}

func (l *Lexer) restoreState(s lexerState) {
	l.pos = s.pos
	l.line = s.line
	l.col = s.col
	l.ch = s.ch
	l.chSize = s.chSize
	l.prevTokenIsValue = s.prevTokenIsValue
}

// tokenBoundary lists ASCII characters that cannot appear inside identifiers
// per §2.3.
var tokenBoundary = [128]bool{
	'{': true, '}': true, '[': true, ']': true, '(': true, ')': true,
	',': true, '.': true, '"': true, '\'': true, '@': true,
	'+': true, '-': true, '*': true, '/': true, '%': true, '^': true,
	'<': true, '>': true, '=': true, '!': true, '?': true,
	':': true, ';': true, '|': true, '&': true, '$': true,
	'~': true, '#': true, '\\': true,
}

// isIdentStart reports whether ch can start an identifier.
// Per §2.3, identifiers can start with any non-whitespace, non-boundary,
// non-digit character (including Unicode letters, emoji, etc.).
func isIdentStart(ch rune) bool {
	if ch < 0 {
		return false
	}
	if ch < 128 {
		return !tokenBoundary[ch] && ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' && !isDigit(ch)
	}
	return !unicode.IsSpace(ch)
}

// isIdentContinue reports whether ch can continue an identifier.
// Digits are allowed after the first character.
func isIdentContinue(ch rune) bool {
	if ch < 0 {
		return false
	}
	if ch < 128 {
		return !tokenBoundary[ch] && ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r'
	}
	return !unicode.IsSpace(ch)
}

func isDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}

func isHexDigit(ch rune) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

// safeHexVal returns the numeric value of a hex digit and whether ch is valid.
func safeHexVal(ch rune) (int, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		return int(ch - '0'), true
	case ch >= 'a' && ch <= 'f':
		return int(ch-'a') + 10, true
	case ch >= 'A' && ch <= 'F':
		return int(ch-'A') + 10, true
	default:
		return 0, false
	}
}

// isValueToken reports whether t is a value-producing token.
// Used by the lexer to distinguish unary minus from binary subtraction.
func isValueToken(t Type) bool {
	switch t {
	case IntLit, FloatLit, StringLit, Ident,
		True, False, Null, Inf, NaN, Undefined,
		Self, Env,
		RParen, RBrack, RBrace:
		return true
	}
	return false
}

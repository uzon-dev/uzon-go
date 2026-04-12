// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package token

import (
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
		Env,
		RParen, RBrack, RBrace:
		return true
	}
	return false
}

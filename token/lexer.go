// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package token

import (
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

	// errors collects lexical errors (unterminated strings, invalid escapes).
	errors []LexError
}

// LexError records a lexical error with its source position.
type LexError struct {
	Pos Pos
	Msg string
}

func (e LexError) Error() string {
	return e.Pos.String() + ": " + e.Msg
}

// Errors returns all lexical errors accumulated so far.
func (l *Lexer) Errors() []LexError {
	return l.errors
}

// errorf records a lexical error at the given position.
func (l *Lexer) errorf(pos Pos, format string, args ...interface{}) {
	l.errors = append(l.errors, LexError{Pos: pos, Msg: sprintf(format, args...)})
}

// NewLexer creates a new Lexer for the given source.
// A UTF-8 BOM at the start of src is silently stripped per §2.1.
// Invalid UTF-8 sequences anywhere in src are recorded as lexical errors
// per §2.1 (parser MUST reject the document).
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
	if !utf8.Valid(src) {
		l.errorf(firstInvalidUTF8Pos(src, file), "invalid UTF-8 encoding")
	} else {
		l.validateSourceChars()
	}
	l.advance()
	return l
}

// validateSourceChars enforces §2.3 character restrictions on the raw source:
//   - Control characters U+0000–U+001F and U+007F (except LF, CR, HT) MUST
//     be rejected anywhere in source — inside strings, comments, and
//     identifiers alike.
//   - RTL and bidi marks (U+200E, U+200F, U+202A–U+202E, U+2066–U+2069)
//     MUST be rejected outside string literals.
//
// A minimal state machine tracks line comments and string literals so that
// bidi marks inside string content are accepted while marks in surrounding
// code (including comments) are rejected. Escaped `\"` inside strings does
// not close the string.
func (l *Lexer) validateSourceChars() {
	line, col := 1, 1
	inString := false
	inLineComment := false
	i := 0
	for i < len(l.src) {
		r, size := utf8.DecodeRune(l.src[i:])
		pos := Pos{File: l.file, Line: line, Column: col, Offset: i}

		if isForbiddenControl(r) {
			l.errorf(pos, "control character U+%04X is not allowed in source", r)
		}
		if !inString && isBidiMark(r) {
			l.errorf(pos, "bidi/directional mark U+%04X is not allowed outside string literals", r)
		}

		if inLineComment {
			if r == '\n' {
				inLineComment = false
			}
		} else if inString {
			if r == '\\' && i+size < len(l.src) {
				// Skip the escaped character so \" does not close the string.
				nr, nsize := utf8.DecodeRune(l.src[i+size:])
				if isForbiddenControl(nr) {
					npos := Pos{File: l.file, Line: line, Column: col + 1, Offset: i + size}
					l.errorf(npos, "control character U+%04X is not allowed in source", nr)
				}
				if nr == '\n' {
					line++
					col = 1
				} else {
					col += 2
				}
				i += size + nsize
				continue
			}
			if r == '"' {
				inString = false
			}
		} else {
			if r == '"' {
				inString = true
			} else if r == '/' && i+1 < len(l.src) && l.src[i+1] == '/' {
				inLineComment = true
			}
		}

		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		i += size
	}
}

// isForbiddenControl reports whether r is a control character that §2.3
// forbids in source. LF, CR, and HT are permitted.
func isForbiddenControl(r rune) bool {
	if r == '\n' || r == '\r' || r == '\t' {
		return false
	}
	return r <= 0x1F || r == 0x7F
}

// isBidiMark reports whether r is an RTL or bidi directional mark that
// §2.3 forbids outside string literals.
func isBidiMark(r rune) bool {
	switch r {
	case 0x200E, 0x200F:
		return true
	}
	if r >= 0x202A && r <= 0x202E {
		return true
	}
	if r >= 0x2066 && r <= 0x2069 {
		return true
	}
	return false
}

// firstInvalidUTF8Pos locates the position of the first invalid UTF-8
// byte in src, tracking 1-based line/column counts.
func firstInvalidUTF8Pos(src []byte, file string) Pos {
	line, col := 1, 1
	for i := 0; i < len(src); {
		r, size := utf8.DecodeRune(src[i:])
		if r == utf8.RuneError && size == 1 {
			return Pos{File: file, Line: line, Column: col, Offset: i}
		}
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		i += size
	}
	return Pos{File: file, Line: line, Column: col}
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
// Per §2.1 and §2.3, only ASCII space/tab/LF/CR and the listed ASCII
// punctuation terminate identifiers — any non-ASCII rune (including
// U+00A0 non-breaking space and U+2000–U+200B typographic spaces) is a
// valid identifier character.
func isIdentStart(ch rune) bool {
	if ch < 0 {
		return false
	}
	if ch < 128 {
		return !tokenBoundary[ch] && ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' && !isDigit(ch)
	}
	return true
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
	return true
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

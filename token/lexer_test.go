// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package token

import (
	"testing"
)

// collectTokens scans all tokens from the lexer until EOF.
func collectTokens(lex *Lexer) []Token {
	var tokens []Token
	for {
		tok := lex.Next()
		tokens = append(tokens, tok)
		if tok.Type == EOF {
			break
		}
	}
	return tokens
}

func TestLexerBasicBinding(t *testing.T) {
	lex := NewLexer([]byte(`x is 42`), "test.uzon")
	expect := []struct {
		typ Type
		lit string
	}{
		{Ident, "x"},
		{Is, "is"},
		{IntLit, "42"},
		{EOF, ""},
	}
	for _, e := range expect {
		tok := lex.Next()
		if tok.Type != e.typ {
			t.Errorf("expected type %v, got %v (literal=%q)", e.typ, tok.Type, tok.Literal)
		}
		if e.lit != "" && tok.Literal != e.lit {
			t.Errorf("expected literal %q, got %q", e.lit, tok.Literal)
		}
	}
}

func TestLexerNumericLiterals(t *testing.T) {
	tests := []struct {
		src string
		typ Type
		lit string
	}{
		{"42", IntLit, "42"},
		{"0xff", IntLit, "0xff"},
		{"0xFF", IntLit, "0xFF"},
		{"0o77", IntLit, "0o77"},
		{"0b1010", IntLit, "0b1010"},
		{"1_000_000", IntLit, "1_000_000"},
		{"3.14", FloatLit, "3.14"},
		{"1e10", FloatLit, "1e10"},
		{"2.5E-3", FloatLit, "2.5E-3"},
		{"0", IntLit, "0"},
		{"100", IntLit, "100"},
	}
	for _, tt := range tests {
		lex := NewLexer([]byte(tt.src), "")
		tok := lex.Next()
		if tok.Type != tt.typ {
			t.Errorf("input %q: expected type %v, got %v", tt.src, tt.typ, tok.Type)
		}
		if tok.Literal != tt.lit {
			t.Errorf("input %q: expected literal %q, got %q", tt.src, tt.lit, tok.Literal)
		}
	}
}

func TestLexerNegativeNumbers(t *testing.T) {
	// After "is", "-7" is a single negative literal.
	lex := NewLexer([]byte(`x is -7`), "")
	tokens := collectTokens(lex)
	if tokens[2].Type != IntLit || tokens[2].Literal != "-7" {
		t.Errorf("expected IntLit -7, got %v %q", tokens[2].Type, tokens[2].Literal)
	}

	// After a value token, "-" is binary subtraction.
	lex = NewLexer([]byte(`3 - 5`), "")
	tokens = collectTokens(lex)
	if tokens[1].Type != Minus {
		t.Errorf("expected Minus, got %v", tokens[1].Type)
	}
	if tokens[2].Type != IntLit || tokens[2].Literal != "5" {
		t.Errorf("expected IntLit 5, got %v %q", tokens[2].Type, tokens[2].Literal)
	}
}

func TestLexerNegativeInfNan(t *testing.T) {
	lex := NewLexer([]byte(`x is -inf`), "")
	tokens := collectTokens(lex)
	if tokens[2].Type != FloatLit || tokens[2].Literal != "-inf" {
		t.Errorf("expected FloatLit -inf, got %v %q", tokens[2].Type, tokens[2].Literal)
	}

	lex = NewLexer([]byte(`x is -nan`), "")
	tokens = collectTokens(lex)
	if tokens[2].Type != FloatLit || tokens[2].Literal != "-nan" {
		t.Errorf("expected FloatLit -nan, got %v %q", tokens[2].Type, tokens[2].Literal)
	}
}

func TestLexerKeywords(t *testing.T) {
	keywords := map[string]Type{
		"true": True, "false": False, "null": Null,
		"inf": Inf, "nan": NaN, "undefined": Undefined,
		"is": Is, "are": Are, "from": From, "called": Called,
		"as": As, "named": Named, "with": With, "union": Union,
		"extends": Extends, "to": To, "of": Of,
		"and": And, "or": Or, "not": Not,
		"if": If, "then": Then, "else": Else,
		"case": Case, "when": When,
		"self": Self, "env": Env, "struct": Struct, "in": In,
		"function": Function, "returns": Returns, "default": Default,
		"lazy": Lazy, "type": TypeKw,
	}
	for kw, expected := range keywords {
		lex := NewLexer([]byte(kw), "")
		tok := lex.Next()
		if tok.Type != expected {
			t.Errorf("keyword %q: expected %v, got %v", kw, expected, tok.Type)
		}
	}
}

func TestLexerStringBasic(t *testing.T) {
	lex := NewLexer([]byte(`"hello, world"`), "")
	tok := lex.Next()
	if tok.Type != StringLit || tok.Literal != "hello, world" {
		t.Errorf("expected StringLit %q, got %v %q", "hello, world", tok.Type, tok.Literal)
	}
}

func TestLexerStringEscapes(t *testing.T) {
	tests := []struct {
		src      string
		expected string
	}{
		{`"tab:\there\n"`, "tab:\there\n"},
		{`"quote:\" backslash:\\"`, "quote:\" backslash:\\\\"},
		{`"\r\t\0"`, "\r\t\x00"},
		{`"\{curly}"`, "\\{curly}"},
	}
	for _, tt := range tests {
		lex := NewLexer([]byte(tt.src), "")
		tok := lex.Next()
		if tok.Literal != tt.expected {
			t.Errorf("input %s: expected %q, got %q", tt.src, tt.expected, tok.Literal)
		}
	}
}

func TestLexerStringEscapeHexRange(t *testing.T) {
	// \x41 = 'A' (valid, in 0x00-0x7F range).
	lex := NewLexer([]byte(`"\x41"`), "")
	tok := lex.Next()
	if tok.Literal != "A" {
		t.Errorf("\\x41: expected 'A', got %q", tok.Literal)
	}

	// \x7F is valid (max ASCII).
	lex = NewLexer([]byte(`"\x7F"`), "")
	tok = lex.Next()
	if tok.Literal != "\x7f" {
		t.Errorf("\\x7F: expected 0x7F, got %q", tok.Literal)
	}

	// \xFF is invalid (>0x7F per §4.4) → replacement char.
	lex = NewLexer([]byte(`"\xFF"`), "")
	tok = lex.Next()
	if tok.Literal != "\uFFFD" {
		t.Errorf("\\xFF: expected replacement char, got %q", tok.Literal)
	}
}

func TestLexerStringEscapeUnicode(t *testing.T) {
	// Valid Unicode scalar value.
	lex := NewLexer([]byte(`"\u{0041}"`), "")
	tok := lex.Next()
	if tok.Literal != "A" {
		t.Errorf("\\u{0041}: expected 'A', got %q", tok.Literal)
	}

	// Surrogate range is invalid (§4.4).
	lex = NewLexer([]byte(`"\u{D800}"`), "")
	tok = lex.Next()
	if tok.Literal != "\uFFFD" {
		t.Errorf("\\u{D800}: expected replacement char, got %q", tok.Literal)
	}

	// Above U+10FFFF is invalid.
	lex = NewLexer([]byte(`"\u{110000}"`), "")
	tok = lex.Next()
	if tok.Literal != "\uFFFD" {
		t.Errorf("\\u{110000}: expected replacement char, got %q", tok.Literal)
	}

	// Emoji via Unicode escape.
	lex = NewLexer([]byte(`"\u{1F600}"`), "")
	tok = lex.Next()
	if tok.Literal != "\U0001F600" {
		t.Errorf("\\u{1F600}: expected grinning face emoji, got %q", tok.Literal)
	}
}

func TestLexerCompositeOperators(t *testing.T) {
	tests := []struct {
		src string
		typ Type
		lit string
	}{
		{"x is not 0", IsNot, "is not"},
		{"x is named ok", IsNamed, "is named"},
		{"x is not named ok", IsNotNamed, "is not named"},
		{"x or else 1", OrElse, "or else"},
	}
	for _, tt := range tests {
		lex := NewLexer([]byte(tt.src), "")
		lex.Next() // skip 'x'
		tok := lex.Next()
		if tok.Type != tt.typ {
			t.Errorf("input %q: expected %v, got %v (%q)", tt.src, tt.typ, tok.Type, tok.Literal)
		}
		if tok.Literal != tt.lit {
			t.Errorf("input %q: expected literal %q, got %q", tt.src, tt.lit, tok.Literal)
		}
	}
}

func TestLexerKeywordEscape(t *testing.T) {
	lex := NewLexer([]byte(`@is is 3`), "")
	tok := lex.Next()
	if tok.Type != Ident {
		t.Errorf("expected Ident, got %v", tok.Type)
	}
	if tok.Literal != "is" {
		t.Errorf("expected literal %q, got %q", "is", tok.Literal)
	}
	// Next token should be the real "is" keyword.
	tok = lex.Next()
	if tok.Type != Is {
		t.Errorf("expected Is keyword, got %v", tok.Type)
	}
}

func TestLexerQuotedIdent(t *testing.T) {
	lex := NewLexer([]byte(`'Content-Type' is "json"`), "")
	tok := lex.Next()
	if tok.Type != Ident || tok.Literal != "Content-Type" {
		t.Errorf("expected Ident Content-Type, got %v %q", tok.Type, tok.Literal)
	}
}

func TestLexerQuotedKeyword(t *testing.T) {
	// 'is' in quotes remains the keyword per §2.3.
	lex := NewLexer([]byte(`'is'`), "")
	tok := lex.Next()
	if tok.Type != Is {
		t.Errorf("expected Is keyword, got %v %q", tok.Type, tok.Literal)
	}
}

func TestLexerComments(t *testing.T) {
	src := "// comment\nx is 42 // inline"
	lex := NewLexer([]byte(src), "")
	tok := lex.Next()
	if tok.Type != Ident || tok.Literal != "x" {
		t.Errorf("expected Ident x, got %v %q", tok.Type, tok.Literal)
	}
}

func TestLexerOperators(t *testing.T) {
	src := `++ ** + - * / % ^ < <= > >=`
	lex := NewLexer([]byte(src), "")
	expected := []Type{PlusPlus, StarStar, Plus, Minus, Star, Slash, Percent, Caret, Lt, LtEq, Gt, GtEq}
	for _, e := range expected {
		tok := lex.Next()
		if tok.Type != e {
			t.Errorf("expected %v, got %v (%q)", e, tok.Type, tok.Literal)
		}
	}
}

func TestLexerPunctuation(t *testing.T) {
	src := `{ } ( ) [ ] , .`
	lex := NewLexer([]byte(src), "")
	expected := []Type{LBrace, RBrace, LParen, RParen, LBrack, RBrack, Comma, Dot}
	for _, e := range expected {
		tok := lex.Next()
		if tok.Type != e {
			t.Errorf("expected %v, got %v (%q)", e, tok.Type, tok.Literal)
		}
	}
}

func TestLexerUnicodeIdent(t *testing.T) {
	src := `안녕 is "인사말"`
	lex := NewLexer([]byte(src), "")
	tok := lex.Next()
	if tok.Type != Ident || tok.Literal != "안녕" {
		t.Errorf("expected Ident 안녕, got %v %q", tok.Type, tok.Literal)
	}
}

func TestLexerBOM(t *testing.T) {
	// UTF-8 BOM should be silently skipped per §2.1.
	src := []byte{0xEF, 0xBB, 0xBF, 'x', ' ', 'i', 's', ' ', '4', '2'}
	lex := NewLexer(src, "")
	tok := lex.Next()
	if tok.Type != Ident || tok.Literal != "x" {
		t.Errorf("expected Ident x after BOM, got %v %q", tok.Type, tok.Literal)
	}
}

func TestLexerPeek(t *testing.T) {
	lex := NewLexer([]byte(`a is 1`), "")
	peeked := lex.Peek()
	if peeked.Type != Ident || peeked.Literal != "a" {
		t.Errorf("Peek: expected Ident a, got %v %q", peeked.Type, peeked.Literal)
	}
	tok := lex.Next()
	if tok.Type != peeked.Type || tok.Literal != peeked.Literal {
		t.Errorf("Next after Peek: expected same token, got %v %q", tok.Type, tok.Literal)
	}
}

func TestLexerPositionTracking(t *testing.T) {
	src := "x is\n  42"
	lex := NewLexer([]byte(src), "test.uzon")
	tok := lex.Next() // x
	if tok.Pos.Line != 1 || tok.Pos.Column != 1 {
		t.Errorf("x: expected 1:1, got %d:%d", tok.Pos.Line, tok.Pos.Column)
	}
	lex.Next() // is
	tok = lex.Next() // 42
	if tok.Pos.Line != 2 || tok.Pos.Column != 3 {
		t.Errorf("42: expected 2:3, got %d:%d", tok.Pos.Line, tok.Pos.Column)
	}
}

func TestLexerEmptyInput(t *testing.T) {
	lex := NewLexer([]byte(""), "")
	tok := lex.Next()
	if tok.Type != EOF {
		t.Errorf("expected EOF for empty input, got %v", tok.Type)
	}
}

func TestLexerMultilineBinding(t *testing.T) {
	src := "a is 1\nb is 2\nc is 3"
	lex := NewLexer([]byte(src), "")
	tokens := collectTokens(lex)
	// Should be: a is 1 b is 2 c is 3 EOF = 10 tokens.
	if len(tokens) != 10 {
		t.Errorf("expected 10 tokens, got %d", len(tokens))
	}
}

func TestLexerStringInterpolation(t *testing.T) {
	// Basic interpolation: {config.x} is preserved as literal text.
	lex := NewLexer([]byte(`"hello {config.x}"`), "")
	tok := lex.Next()
	if tok.Type != StringLit {
		t.Errorf("expected StringLit, got %v", tok.Type)
	}
	if tok.Literal != "hello {config.x}" {
		t.Errorf("expected %q, got %q", "hello {config.x}", tok.Literal)
	}

	// Nested string inside interpolation.
	lex = NewLexer([]byte(`"value: {std.join([\"a\", \"b\"], \", \")}"`), "")
	tok = lex.Next()
	if tok.Type != StringLit {
		t.Errorf("nested string interpolation: expected StringLit, got %v", tok.Type)
	}
}

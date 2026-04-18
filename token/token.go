// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

// Package token defines the lexical token types and source position
// tracking for the UZON language.
//
// Token types cover all UZON keywords (§2.5), operators (§2.6),
// punctuation, and literal forms. The lexer emits composite operators
// (e.g. "is not", "or else") as single tokens for simpler parsing.
package token

// Type represents a UZON token type.
type Type int

const (
	// Special tokens.
	Illegal Type = iota // unrecognized input
	EOF                 // end of file
	Comment             // line comment starting with "//"

	// Literal tokens.
	IntLit    // integer literal: 42, 0xff, 0o77, 0b1010
	FloatLit  // float literal: 3.14, 1e10
	StringLit // string literal: "hello"

	// Identifier.
	Ident // any non-keyword identifier

	// Keywords — value literals (§4).
	True      // true
	False     // false
	Null      // null
	Inf       // inf  (IEEE 754 infinity)
	NaN       // nan  (IEEE 754 not-a-number)
	Undefined // undefined (missing value state, §3.1)

	// Keywords — binding (§1, §3.3).
	Is  // is  (associates name with value)
	Are // are (list sugar: elements without brackets)

	// Keywords — type system (§3, §6).
	From    // from    (enum/union variant source)
	Called  // called  (names a type)
	As      // as      (type annotation/assertion)
	Named   // named   (tagged union variant label)
	With    // with    (struct override)
	Union   // union   (union type marker)
	PlusKw  // plus    (struct extension)
	Enum    // enum    (enum type declaration, §3.5)
	Tagged  // tagged  (tagged union prefix, §3.7)

	// Keywords — conversion/extraction (§5.5, §5.8).
	To // to (type conversion)
	Of // of (field extraction)

	// Keywords — logic (§5.3).
	And // and (logical conjunction)
	Or  // or  (logical disjunction)
	Not // not (logical negation)

	// Keywords — control flow (§5.9, §5.10).
	If   // if
	Then // then
	Else // else
	Case // case
	When // when

	// Keywords — references (§5.13, §7).
	Env    // env    (environment variable access)
	Struct // struct (file import)
	In     // in     (membership test)

	// Keywords — function (§3.7).
	Function // function
	Returns  // returns
	Default  // default

	// Keywords — type check (§5.2).
	TypeKw // type (runtime type check)

	// Keywords — reserved for future use.
	Lazy // lazy

	// Composite operators — the lexer emits these as single tokens
	// to simplify parsing of multi-word keyword sequences.
	IsNot      // is not
	IsNamed    // is named
	IsNotNamed // is not named
	IsType    // is type
	IsNotType // is not type
	OrElse    // or else

	// Arithmetic operators (§5.1).
	Plus     // +
	Minus    // -
	Star     // *
	Slash    // /
	Percent  // %
	Caret    // ^  (exponentiation)
	PlusPlus // ++ (string/list concatenation)
	StarStar // ** (string/list repetition)

	// Comparison operators (§5.3).
	Lt   // <
	LtEq // <=
	Gt   // >
	GtEq // >=

	// Punctuation and delimiters.
	LBrace // {
	RBrace // }
	LParen // (
	RParen // )
	LBrack // [
	RBrack // ]
	Comma  // ,
	Dot    // .
	At     // @ (keyword escape prefix)
)

// typeNames maps token types to their display strings.
var typeNames = map[Type]string{
	Illegal: "ILLEGAL", EOF: "EOF", Comment: "COMMENT",
	IntLit: "INT", FloatLit: "FLOAT", StringLit: "STRING",
	Ident: "IDENT",
	True: "true", False: "false", Null: "null", Inf: "inf", NaN: "nan", Undefined: "undefined",
	Is: "is", Are: "are",
	From: "from", Called: "called", As: "as", Named: "named", With: "with", Union: "union", PlusKw: "plus",
	Enum: "enum", Tagged: "tagged",
	To: "to", Of: "of",
	And: "and", Or: "or", Not: "not",
	If: "if", Then: "then", Else: "else", Case: "case", When: "when",
	Env: "env", Struct: "struct", In: "in",
	Function: "function", Returns: "returns", Default: "default",
	TypeKw: "type", Lazy: "lazy",
	IsNot: "is not", IsNamed: "is named", IsNotNamed: "is not named",
	IsType: "is type", IsNotType: "is not type",
	OrElse: "or else",
	Plus: "+", Minus: "-", Star: "*", Slash: "/", Percent: "%", Caret: "^",
	PlusPlus: "++", StarStar: "**",
	Lt: "<", LtEq: "<=", Gt: ">", GtEq: ">=",
	LBrace: "{", RBrace: "}", LParen: "(", RParen: ")",
	LBrack: "[", RBrack: "]", Comma: ",", Dot: ".", At: "@",
}

// String returns the display name of the token type.
func (t Type) String() string {
	if s, ok := typeNames[t]; ok {
		return s
	}
	return "UNKNOWN"
}

// Keywords maps keyword strings to their token types.
// Used by the lexer to distinguish keywords from identifiers.
var Keywords = map[string]Type{
	"true": True, "false": False, "null": Null, "inf": Inf, "nan": NaN, "undefined": Undefined,
	"is": Is, "are": Are,
	"from": From, "called": Called, "as": As, "named": Named, "with": With, "union": Union, "plus": PlusKw,
	"enum": Enum, "tagged": Tagged,
	"to": To, "of": Of,
	"and": And, "or": Or, "not": Not,
	"if": If, "then": Then, "else": Else, "case": Case, "when": When,
	"env": Env, "struct": Struct, "in": In,
	"function": Function, "returns": Returns, "default": Default,
	"type": TypeKw, "lazy": Lazy,
}

// IsKeyword reports whether s is a reserved UZON keyword.
func IsKeyword(s string) bool {
	_, ok := Keywords[s]
	return ok
}

// Pos represents a source position in a UZON document.
type Pos struct {
	File   string // file name (empty for in-memory parsing)
	Line   int    // 1-based line number
	Column int    // 1-based column (Unicode scalar count)
	Offset int    // 0-based byte offset into the source
}

// String returns a human-readable "file:line:col" or "line:col" position string.
func (p Pos) String() string {
	if p.File != "" {
		return p.File + ":" + itoa(p.Line) + ":" + itoa(p.Column)
	}
	return itoa(p.Line) + ":" + itoa(p.Column)
}

// sprintf is a minimal fmt.Sprintf-style helper supporting %d, %s, %q, %v, %x.
// Used by lexer error messages without importing fmt.
func sprintf(format string, args ...interface{}) string {
	var sb []byte
	ai := 0
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' || i+1 >= len(format) {
			sb = append(sb, c)
			continue
		}
		i++
		if ai >= len(args) {
			sb = append(sb, '%', format[i])
			continue
		}
		switch format[i] {
		case 'd':
			switch v := args[ai].(type) {
			case int:
				sb = append(sb, itoa(v)...)
			case int64:
				sb = append(sb, itoa(int(v))...)
			case rune:
				sb = append(sb, itoa(int(v))...)
			default:
				sb = append(sb, "?"...)
			}
		case 's':
			if s, ok := args[ai].(string); ok {
				sb = append(sb, s...)
			}
		case 'q':
			if s, ok := args[ai].(string); ok {
				sb = append(sb, '"')
				sb = append(sb, s...)
				sb = append(sb, '"')
			}
		case 'c':
			if r, ok := args[ai].(rune); ok {
				sb = append(sb, string(r)...)
			} else if c, ok := args[ai].(byte); ok {
				sb = append(sb, c)
			}
		case 'x':
			switch v := args[ai].(type) {
			case int:
				sb = append(sb, hexstr(uint64(v))...)
			case int64:
				sb = append(sb, hexstr(uint64(v))...)
			case rune:
				sb = append(sb, hexstr(uint64(v))...)
			case uint:
				sb = append(sb, hexstr(uint64(v))...)
			default:
				sb = append(sb, "?"...)
			}
		case 'v':
			switch v := args[ai].(type) {
			case string:
				sb = append(sb, v...)
			case int:
				sb = append(sb, itoa(v)...)
			case rune:
				sb = append(sb, string(v)...)
			default:
				sb = append(sb, "?"...)
			}
		default:
			sb = append(sb, '%', format[i])
		}
		ai++
	}
	return string(sb)
}

func hexstr(u uint64) string {
	if u == 0 {
		return "0"
	}
	const digits = "0123456789ABCDEF"
	var buf [20]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = digits[u&0xF]
		u >>= 4
	}
	return string(buf[i:])
}

// itoa converts an integer to its decimal string representation
// without importing strconv.
func itoa(i int) string {
	if i < 0 {
		return "-" + uitoa(uint(-i))
	}
	return uitoa(uint(i))
}

func uitoa(u uint) string {
	if u == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	return string(buf[i:])
}

// Token represents a single lexical token produced by the lexer.
type Token struct {
	Type    Type   // the token's type
	Literal string // the raw text of the token
	Pos     Pos    // source position where the token begins
}

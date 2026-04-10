// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"errors"
	"strings"
	"testing"

	"github.com/uzon-dev/uzon-go/ast"
)

// evalSrc parses and evaluates UZON source, returning the top-level struct.
func evalSrc(t *testing.T, src string) *Value {
	t.Helper()
	p := ast.NewParser([]byte(src), "test.uzon")
	doc, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ev := NewEvaluator()
	val, err := ev.EvalDocument(doc)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	return val
}

// getField extracts a named field from a struct value.
func getField(t *testing.T, v *Value, name string) *Value {
	t.Helper()
	if v.Kind != KindStruct {
		t.Fatalf("expected struct, got %s", v.Kind)
	}
	f := v.Struct.Get(name)
	if f == nil {
		t.Fatalf("field %q not found", name)
	}
	return f
}

// --- Basic bindings and literals ---

func TestEvalSimpleBindings(t *testing.T) {
	v := evalSrc(t, `x is 42
y is "hello"
z is true`)
	x := getField(t, v, "x")
	if x.Kind != KindInt || x.Int.Int64() != 42 {
		t.Errorf("x: want 42, got %v", x)
	}
	y := getField(t, v, "y")
	if y.Kind != KindString || y.Str != "hello" {
		t.Errorf("y: want \"hello\", got %v", y)
	}
	z := getField(t, v, "z")
	if z.Kind != KindBool || !z.Bool {
		t.Errorf("z: want true, got %v", z)
	}
}

func TestEvalNullAndUndefined(t *testing.T) {
	v := evalSrc(t, `
a is null
b is self.a is null
c is self.nonexistent or else "fallback"`)
	a := getField(t, v, "a")
	if a.Kind != KindNull {
		t.Errorf("a: want null, got %s", a.Kind)
	}
	b := getField(t, v, "b")
	if !b.Bool {
		t.Errorf("b: want true")
	}
	c := getField(t, v, "c")
	if c.Str != "fallback" {
		t.Errorf("c: want \"fallback\", got %q", c.Str)
	}
}

// --- Arithmetic ---

func TestEvalArithmetic(t *testing.T) {
	v := evalSrc(t, `
a is 3 + 4
b is 10 - 3
c is 2 * 5
d is 10 / 3
e is 10 % 3
f is 2 ^ 10`)
	tests := []struct {
		name string
		want int64
	}{
		{"a", 7}, {"b", 7}, {"c", 10}, {"d", 3}, {"e", 1}, {"f", 1024},
	}
	for _, tt := range tests {
		f := getField(t, v, tt.name)
		if f.Int.Int64() != tt.want {
			t.Errorf("%s: want %d, got %d", tt.name, tt.want, f.Int.Int64())
		}
	}
}

func TestEvalFloatArithmetic(t *testing.T) {
	v := evalSrc(t, `
a is 1.5 + 2.5
b is 10.0 - 3.0
c is 2.0 * 3.0`)
	a := getField(t, v, "a")
	if a.Kind != KindFloat {
		t.Fatalf("a: want float, got %s", a.Kind)
	}
	af, _ := a.Float.Float64()
	if af != 4.0 {
		t.Errorf("a: want 4.0, got %f", af)
	}
	b := getField(t, v, "b")
	bf, _ := b.Float.Float64()
	if bf != 7.0 {
		t.Errorf("b: want 7.0, got %f", bf)
	}
}

// --- Self-reference and scope ---

func TestEvalSelfReference(t *testing.T) {
	v := evalSrc(t, `
x is 10
y is self.x + 5`)
	y := getField(t, v, "y")
	if y.Int.Int64() != 15 {
		t.Errorf("y: want 15, got %d", y.Int.Int64())
	}
}

func TestEvalSelfExclusion(t *testing.T) {
	v := evalSrc(t, `x is self.x or else 1`)
	x := getField(t, v, "x")
	if x.Int.Int64() != 1 {
		t.Errorf("x: want 1, got %d", x.Int.Int64())
	}
}

func TestEvalScopeChain(t *testing.T) {
	v := evalSrc(t, `
base_port is 8080
server is {
    port is self.base_port
    admin is self.port + 1
}`)
	server := getField(t, v, "server")
	port := server.Struct.Get("port")
	if port.Int.Int64() != 8080 {
		t.Errorf("port: want 8080, got %d", port.Int.Int64())
	}
	admin := server.Struct.Get("admin")
	if admin.Int.Int64() != 8081 {
		t.Errorf("admin: want 8081, got %d", admin.Int.Int64())
	}
}

// --- Nested struct and member access ---

func TestEvalNestedStruct(t *testing.T) {
	v := evalSrc(t, `
server is {
    host is "localhost"
    port is 8080
}
url is self.server.host`)
	url := getField(t, v, "url")
	if url.Str != "localhost" {
		t.Errorf("want \"localhost\", got %q", url.Str)
	}
}

func TestEvalMemberAccessOnUndefined(t *testing.T) {
	v := evalSrc(t, `
config is { }
port is self.config.missing.nested or else 8080`)
	port := getField(t, v, "port")
	if port.Int.Int64() != 8080 {
		t.Errorf("want 8080, got %d", port.Int.Int64())
	}
}

// --- Control flow ---

func TestEvalIfExpr(t *testing.T) {
	v := evalSrc(t, `
x is 5
y is if self.x > 3 then "big" else "small"`)
	y := getField(t, v, "y")
	if y.Str != "big" {
		t.Errorf("want \"big\", got %q", y.Str)
	}
}

func TestEvalCaseExpr(t *testing.T) {
	v := evalSrc(t, `
x is case 5 % 3
    when 0 then "zero"
    when 2 then "two"
    else "other"`)
	x := getField(t, v, "x")
	if x.Str != "two" {
		t.Errorf("want \"two\", got %q", x.Str)
	}
}

// --- Compound types ---

func TestEvalList(t *testing.T) {
	v := evalSrc(t, `primes is [ 2, 3, 5, 7 ]`)
	p := getField(t, v, "primes")
	if p.Kind != KindList || len(p.List.Elements) != 4 {
		t.Errorf("want list of 4, got %s len=%d", p.Kind, len(p.List.Elements))
	}
}

func TestEvalTuple(t *testing.T) {
	v := evalSrc(t, `pair is (42, "hello")`)
	p := getField(t, v, "pair")
	if p.Kind != KindTuple || len(p.Tuple.Elements) != 2 {
		t.Errorf("want tuple of 2, got %s", p.Kind)
	}
}

func TestEvalAre(t *testing.T) {
	v := evalSrc(t, `names are "a", "b", "c"`)
	n := getField(t, v, "names")
	if n.Kind != KindList || len(n.List.Elements) != 3 {
		t.Errorf("want list of 3, got %s len=%d", n.Kind, len(n.List.Elements))
	}
}

func TestEvalListAccess(t *testing.T) {
	v := evalSrc(t, `
scores is [ 97, 85, 92 ]
a is self.scores.0
b is self.scores.first
c is self.scores.second`)
	a := getField(t, v, "a")
	if a.Int.Int64() != 97 {
		t.Errorf("a: want 97, got %d", a.Int.Int64())
	}
	b := getField(t, v, "b")
	if b.Int.Int64() != 97 {
		t.Errorf("b (first): want 97, got %d", b.Int.Int64())
	}
	c := getField(t, v, "c")
	if c.Int.Int64() != 85 {
		t.Errorf("c (second): want 85, got %d", c.Int.Int64())
	}
}

// --- Struct operations ---

func TestEvalWith(t *testing.T) {
	v := evalSrc(t, `
base is { host is "localhost", port is 8080 }
dev is self.base with { port is 9090 }`)
	dev := getField(t, v, "dev")
	port := dev.Struct.Get("port")
	if port.Int.Int64() != 9090 {
		t.Errorf("port: want 9090, got %d", port.Int.Int64())
	}
	host := dev.Struct.Get("host")
	if host.Str != "localhost" {
		t.Errorf("host: want \"localhost\", got %q", host.Str)
	}
}

func TestEvalExtends(t *testing.T) {
	v := evalSrc(t, `
base is { host is "localhost", port is 8080 }
secure is self.base extends { tls is true }`)
	secure := getField(t, v, "secure")
	tls := secure.Struct.Get("tls")
	if tls.Kind != KindBool || !tls.Bool {
		t.Errorf("tls: want true, got %v", tls)
	}
	if len(secure.Struct.Fields) != 3 {
		t.Errorf("fields: want 3, got %d", len(secure.Struct.Fields))
	}
}

func TestEvalWithTypeCompat(t *testing.T) {
	p := ast.NewParser([]byte(`
base is { x is 10, y is 20 }
bad is self.base with { x is "string" }`), "test.uzon")
	doc, err := p.Parse()
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator()
	_, err = ev.EvalDocument(doc)
	if err == nil {
		t.Error("expected type error for string override of int field")
	}
}

func TestEvalWithNullCompat(t *testing.T) {
	v := evalSrc(t, `
base is { x is 10, y is 20 }
result is self.base with { x is null }`)
	result := getField(t, v, "result")
	x := result.Struct.Get("x")
	if x.Kind != KindNull {
		t.Errorf("x: want null, got %s", x.Kind)
	}
}

// --- String operations ---

func TestEvalConcat(t *testing.T) {
	v := evalSrc(t, `
a is "hello" ++ " " ++ "world"
b is [ 1, 2 ] ++ [ 3, 4 ]`)
	a := getField(t, v, "a")
	if a.Str != "hello world" {
		t.Errorf("a: want \"hello world\", got %q", a.Str)
	}
	b := getField(t, v, "b")
	if len(b.List.Elements) != 4 {
		t.Errorf("b: want 4 elements, got %d", len(b.List.Elements))
	}
}

func TestEvalRepeat(t *testing.T) {
	v := evalSrc(t, `x is "*" ** 3`)
	x := getField(t, v, "x")
	if x.Str != "***" {
		t.Errorf("want \"***\", got %q", x.Str)
	}
}

func TestEvalInterpolation(t *testing.T) {
	v := evalSrc(t, `
name is "UZON"
greeting is "Hello, {self.name}!"`)
	g := getField(t, v, "greeting")
	if g.Str != "Hello, UZON!" {
		t.Errorf("want \"Hello, UZON!\", got %q", g.Str)
	}
}

// --- Or else ---

func TestEvalOrElse(t *testing.T) {
	v := evalSrc(t, `x is self.missing or else 42`)
	x := getField(t, v, "x")
	if x.Int.Int64() != 42 {
		t.Errorf("want 42, got %d", x.Int.Int64())
	}
}

// --- Enums ---

func TestEvalEnum(t *testing.T) {
	v := evalSrc(t, `color is green from red, green, blue called RGB`)
	c := getField(t, v, "color")
	if c.Kind != KindEnum {
		t.Fatalf("want enum, got %s", c.Kind)
	}
	if c.Enum.Variant != "green" {
		t.Errorf("variant: want \"green\", got %q", c.Enum.Variant)
	}
	if len(c.Enum.Variants) != 3 {
		t.Errorf("variants: want 3, got %d", len(c.Enum.Variants))
	}
}

// --- Tagged unions ---

func TestEvalTaggedUnion(t *testing.T) {
	v := evalSrc(t, `
status is "all good" named ok from ok as string, err as string
is_ok is self.status is named ok`)
	isOk := getField(t, v, "is_ok")
	if !isOk.Bool {
		t.Error("want true, got false")
	}
}

func TestEvalTaggedUnionTransparency(t *testing.T) {
	v := evalSrc(t, `
val is 10 named myVal from myVal as i64, other as string
doubled is self.val + self.val
compared is self.val > 5`)
	doubled := getField(t, v, "doubled")
	if doubled.Kind != KindInt || doubled.Int.Int64() != 20 {
		t.Errorf("doubled: want 20, got %v", doubled)
	}
	compared := getField(t, v, "compared")
	if !compared.Bool {
		t.Error("compared: want true")
	}
}

// --- Type conversion ---

func TestEvalConversion(t *testing.T) {
	v := evalSrc(t, `x is 3.14 to i32`)
	x := getField(t, v, "x")
	if x.Kind != KindInt || x.Int.Int64() != 3 {
		t.Errorf("want int 3, got %v %v", x.Kind, x.Int)
	}
}

// --- Functions ---

func TestEvalFunction(t *testing.T) {
	v := evalSrc(t, `
add is function a as i32, b as i32 returns i32 { a + b }
result is self.add(3, 4)`)
	r := getField(t, v, "result")
	if r.Int.Int64() != 7 {
		t.Errorf("want 7, got %d", r.Int.Int64())
	}
}

func TestEvalFunctionDefault(t *testing.T) {
	v := evalSrc(t, `
greet is function name as string default "world" returns string { "Hello, " ++ name }
a is self.greet()
b is self.greet("UZON")`)
	a := getField(t, v, "a")
	if a.Str != "Hello, world" {
		t.Errorf("a: want \"Hello, world\", got %q", a.Str)
	}
	b := getField(t, v, "b")
	if b.Str != "Hello, UZON" {
		t.Errorf("b: want \"Hello, UZON\", got %q", b.Str)
	}
}

// --- Standard library ---

func TestEvalStdLen(t *testing.T) {
	v := evalSrc(t, `
items is [ 1, 2, 3 ]
n is std.len(self.items)`)
	n := getField(t, v, "n")
	if n.Int.Int64() != 3 {
		t.Errorf("want 3, got %d", n.Int.Int64())
	}
}

func TestEvalStdMap(t *testing.T) {
	v := evalSrc(t, `
nums are 1, 2, 3
doubled is std.map(self.nums, function n as i64 returns i64 { n * 2 })`)
	d := getField(t, v, "doubled")
	if len(d.List.Elements) != 3 {
		t.Fatalf("want 3 elements, got %d", len(d.List.Elements))
	}
	if d.List.Elements[0].Int.Int64() != 2 {
		t.Errorf("first: want 2, got %d", d.List.Elements[0].Int.Int64())
	}
}

func TestEvalStdFilter(t *testing.T) {
	v := evalSrc(t, `
nums are 1, 2, 3, 4, 5
evens is std.filter(self.nums, function n as i64 returns bool { n % 2 is 0 })`)
	e := getField(t, v, "evens")
	if len(e.List.Elements) != 2 {
		t.Errorf("want 2 elements, got %d", len(e.List.Elements))
	}
}

func TestEvalStdReduce(t *testing.T) {
	v := evalSrc(t, `
nums are 1, 2, 3, 4, 5
total is std.reduce(self.nums, 0, function acc as i64, n as i64 returns i64 { acc + n })`)
	total := getField(t, v, "total")
	if total.Int.Int64() != 15 {
		t.Errorf("want 15, got %d", total.Int.Int64())
	}
}

func TestEvalStdValues(t *testing.T) {
	v := evalSrc(t, `
data is { a is 1, b is "hi" }
result is std.values(self.data)`)
	result := getField(t, v, "result")
	if result.Kind != KindTuple {
		t.Fatalf("want tuple, got %s", result.Kind)
	}
	if len(result.Tuple.Elements) != 2 {
		t.Fatalf("want 2 elements, got %d", len(result.Tuple.Elements))
	}
	if result.Tuple.Elements[0].Int.Int64() != 1 {
		t.Errorf("first: want 1, got %v", result.Tuple.Elements[0])
	}
	if result.Tuple.Elements[1].Str != "hi" {
		t.Errorf("second: want \"hi\", got %v", result.Tuple.Elements[1])
	}
}

func TestEvalStdJoin(t *testing.T) {
	v := evalSrc(t, `result is std.join([ "a", "b", "c" ], ":")`)
	result := getField(t, v, "result")
	if result.Str != "a:b:c" {
		t.Errorf("want \"a:b:c\", got %q", result.Str)
	}
}

func TestEvalStdReplace(t *testing.T) {
	v := evalSrc(t, `result is std.replace("a:b:c", ":", "-")`)
	result := getField(t, v, "result")
	if result.Str != "a-b-c" {
		t.Errorf("want \"a-b-c\", got %q", result.Str)
	}
}

func TestEvalStdSplit(t *testing.T) {
	v := evalSrc(t, `result is std.split("a:b:c", ":")`)
	result := getField(t, v, "result")
	if result.Kind != KindList {
		t.Fatalf("want list, got %s", result.Kind)
	}
	if len(result.List.Elements) != 3 {
		t.Fatalf("want 3 elements, got %d", len(result.List.Elements))
	}
	if result.List.Elements[0].Str != "a" {
		t.Errorf("first: want \"a\", got %q", result.List.Elements[0].Str)
	}
}

func TestEvalStdTrim(t *testing.T) {
	v := evalSrc(t, `result is std.trim("  hello  ")`)
	result := getField(t, v, "result")
	if result.Str != "hello" {
		t.Errorf("want \"hello\", got %q", result.Str)
	}
}

func TestEvalStdSort(t *testing.T) {
	v := evalSrc(t, `
nums are 3, 1, 4, 1, 5
sorted is std.sort(self.nums, function a as i64, b as i64 returns bool { a < b })`)
	s := getField(t, v, "sorted")
	if s.Kind != KindList || len(s.List.Elements) != 5 {
		t.Fatalf("want list of 5, got %s len=%d", s.Kind, len(s.List.Elements))
	}
	vals := make([]int64, len(s.List.Elements))
	for i, e := range s.List.Elements {
		vals[i] = e.Int.Int64()
	}
	for i := 1; i < len(vals); i++ {
		if vals[i] < vals[i-1] {
			t.Errorf("not sorted: %v", vals)
			break
		}
	}
}

// --- Equality and comparison ---

func TestEvalEquality(t *testing.T) {
	v := evalSrc(t, `
a is [ 1, 2, 3 ]
b is [ 1, 2, 3 ]
eq is self.a is self.b`)
	eq := getField(t, v, "eq")
	if !eq.Bool {
		t.Error("want true for deep equality")
	}
}

func TestEvalIn(t *testing.T) {
	v := evalSrc(t, `
x is "bravo" in [ "alfa", "bravo", "charlie" ]
y is 4 in [ 1, 2, 3 ]`)
	x := getField(t, v, "x")
	if !x.Bool {
		t.Error("x: want true")
	}
	y := getField(t, v, "y")
	if y.Bool {
		t.Error("y: want false")
	}
}

// --- Logical operators ---

func TestEvalLogical(t *testing.T) {
	v := evalSrc(t, `
a is true and false
b is true or false
c is not true`)
	a := getField(t, v, "a")
	if a.Bool {
		t.Error("a: want false")
	}
	b := getField(t, v, "b")
	if !b.Bool {
		t.Error("b: want true")
	}
	c := getField(t, v, "c")
	if c.Bool {
		t.Error("c: want false")
	}
}

// --- Type annotation and adoption ---

func TestEvalNoIntFloatCoercion(t *testing.T) {
	p := ast.NewParser([]byte(`x is 1 + 1.5`), "test.uzon")
	doc, err := p.Parse()
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator()
	_, err = ev.EvalDocument(doc)
	if err == nil {
		t.Error("expected error for int + float coercion")
	}
}

func TestEvalAsRangeCheck(t *testing.T) {
	p := ast.NewParser([]byte(`x is 300 as u8`), "test.uzon")
	doc, err := p.Parse()
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator()
	_, err = ev.EvalDocument(doc)
	if err == nil {
		t.Error("expected range error for 300 as u8")
	}
}

func TestEvalNumericTypeMismatch(t *testing.T) {
	p := ast.NewParser([]byte(`
a is 5 as i32
b is 10 as u8
result is self.a >= self.b`), "test.uzon")
	doc, err := p.Parse()
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator()
	_, err = ev.EvalDocument(doc)
	if err == nil {
		t.Error("expected type error for i32 >= u8 comparison")
	}
}

func TestEvalUntypedAdoptsThroughBinding(t *testing.T) {
	v := evalSrc(t, `
count is 5
max is 10 as u8
ok is self.count >= self.max`)
	ok := getField(t, v, "ok")
	if ok.Kind != KindBool || ok.Bool {
		t.Errorf("want false (5 >= 10), got %v", ok)
	}
}

func TestEvalUntypedLiteralAdoption(t *testing.T) {
	v := evalSrc(t, `
x is 3 as u8 + 2
y is 10 as i32 - 1
z is 5 as u8 >= 3`)
	x := getField(t, v, "x")
	if x.Int.Int64() != 5 {
		t.Errorf("x: want 5, got %d", x.Int.Int64())
	}
	if x.Type == nil || x.Type.BaseType != "u8" {
		t.Errorf("x type: want u8, got %v", x.Type)
	}
	y := getField(t, v, "y")
	if y.Int.Int64() != 9 {
		t.Errorf("y: want 9, got %d", y.Int.Int64())
	}
	if y.Type == nil || y.Type.BaseType != "i32" {
		t.Errorf("y type: want i32, got %v", y.Type)
	}
	z := getField(t, v, "z")
	if z.Kind != KindBool || !z.Bool {
		t.Errorf("z: want true, got %v", z)
	}
}

func TestEvalAdoptionRangeCheck(t *testing.T) {
	p := ast.NewParser([]byte(`x is 10 as u8 + 300`), "test.uzon")
	doc, err := p.Parse()
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator()
	_, err = ev.EvalDocument(doc)
	if err == nil {
		t.Error("expected range error for 300 adopting u8")
	}
}

// --- Binding decomposition ---

func TestEvalBindingDecomposition(t *testing.T) {
	v := evalSrc(t, `x is not true`)
	x := getField(t, v, "x")
	if x.Kind != KindBool || x.Bool {
		t.Errorf("want false, got %v", x)
	}
}

// --- Error location ---

func TestEvalErrorLocationString(t *testing.T) {
	p := ast.NewParser([]byte(`x is 10 as u8 + 300`), "")
	doc, err := p.Parse()
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator()
	_, err = ev.EvalDocument(doc)
	if err == nil {
		t.Fatal("expected error")
	}
	var pe *PosError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PosError, got %T: %v", err, err)
	}
	if pe.Pos.File != "" {
		t.Errorf("expected no file for string input, got %q", pe.Pos.File)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "1:") {
		t.Errorf("expected error to start with line:col, got %q", msg)
	}
}

func TestEvalErrorLocationFile(t *testing.T) {
	p := ast.NewParser([]byte(`x is 10 as u8 + 300`), "myfile.uzon")
	doc, err := p.Parse()
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator()
	_, err = ev.EvalDocument(doc)
	if err == nil {
		t.Fatal("expected error")
	}
	var pe *PosError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PosError, got %T: %v", err, err)
	}
	if pe.Pos.File != "myfile.uzon" {
		t.Errorf("expected file=myfile.uzon, got %q", pe.Pos.File)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "myfile.uzon:") {
		t.Errorf("expected error to start with file:line:col, got %q", msg)
	}
}

func TestEvalStdLower(t *testing.T) {
	v := evalSrc(t, `x is std.lower("Hello World")`)
	if v.Struct.Get("x").Str != "hello world" {
		t.Errorf("want \"hello world\", got %q", v.Struct.Get("x").Str)
	}
}

func TestEvalStdUpper(t *testing.T) {
	v := evalSrc(t, `x is std.upper("Hello World")`)
	if v.Struct.Get("x").Str != "HELLO WORLD" {
		t.Errorf("want \"HELLO WORLD\", got %q", v.Struct.Get("x").Str)
	}
}

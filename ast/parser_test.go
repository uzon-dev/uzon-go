// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package ast

import (
	"testing"

	"github.com/uzon-dev/uzon-go/token"
)

func mustParse(t *testing.T, src string) *Document {
	t.Helper()
	p := NewParser([]byte(src), "test.uzon")
	doc, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return doc
}

func TestParseSimpleBinding(t *testing.T) {
	doc := mustParse(t, `x is 42`)
	if len(doc.Bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(doc.Bindings))
	}
	b := doc.Bindings[0]
	if b.Name != "x" {
		t.Errorf("expected name 'x', got %q", b.Name)
	}
	lit, ok := b.Value.(*LiteralExpr)
	if !ok {
		t.Fatalf("expected LiteralExpr, got %T", b.Value)
	}
	if lit.Token.Type != token.IntLit || lit.Token.Literal != "42" {
		t.Errorf("expected IntLit 42, got %v %q", lit.Token.Type, lit.Token.Literal)
	}
}

func TestParseMultipleBindings(t *testing.T) {
	doc := mustParse(t, "x is 1\ny is 2\nz is 3")
	if len(doc.Bindings) != 3 {
		t.Fatalf("expected 3 bindings, got %d", len(doc.Bindings))
	}
	for i, name := range []string{"x", "y", "z"} {
		if doc.Bindings[i].Name != name {
			t.Errorf("binding %d: expected %q, got %q", i, name, doc.Bindings[i].Name)
		}
	}
}

func TestParseStruct(t *testing.T) {
	doc := mustParse(t, `server is { host is "localhost", port is 8080 }`)
	se, ok := doc.Bindings[0].Value.(*StructExpr)
	if !ok {
		t.Fatalf("expected StructExpr, got %T", doc.Bindings[0].Value)
	}
	if len(se.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(se.Fields))
	}
	if se.Fields[0].Name != "host" || se.Fields[1].Name != "port" {
		t.Errorf("unexpected field names: %q, %q", se.Fields[0].Name, se.Fields[1].Name)
	}
}

func TestParseList(t *testing.T) {
	doc := mustParse(t, `primes is [2, 3, 5, 7]`)
	le, ok := doc.Bindings[0].Value.(*ListExpr)
	if !ok {
		t.Fatalf("expected ListExpr, got %T", doc.Bindings[0].Value)
	}
	if len(le.Elements) != 4 {
		t.Errorf("expected 4 elements, got %d", len(le.Elements))
	}
}

func TestParseTuple(t *testing.T) {
	doc := mustParse(t, `pair is (1, "hello")`)
	te, ok := doc.Bindings[0].Value.(*TupleExpr)
	if !ok {
		t.Fatalf("expected TupleExpr, got %T", doc.Bindings[0].Value)
	}
	if len(te.Elements) != 2 {
		t.Errorf("expected 2 elements, got %d", len(te.Elements))
	}
}

func TestParseEmptyTuple(t *testing.T) {
	doc := mustParse(t, `empty is ()`)
	te, ok := doc.Bindings[0].Value.(*TupleExpr)
	if !ok {
		t.Fatalf("expected TupleExpr, got %T", doc.Bindings[0].Value)
	}
	if len(te.Elements) != 0 {
		t.Errorf("expected 0 elements, got %d", len(te.Elements))
	}
}

func TestParseSingleElementTuple(t *testing.T) {
	doc := mustParse(t, `single is (42,)`)
	te, ok := doc.Bindings[0].Value.(*TupleExpr)
	if !ok {
		t.Fatalf("expected TupleExpr for (42,), got %T", doc.Bindings[0].Value)
	}
	if len(te.Elements) != 1 {
		t.Errorf("expected 1 element, got %d", len(te.Elements))
	}
}

func TestParseGrouping(t *testing.T) {
	doc := mustParse(t, `x is (1 + 2)`)
	be, ok := doc.Bindings[0].Value.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr from grouping, got %T", doc.Bindings[0].Value)
	}
	if be.Op != token.Plus {
		t.Errorf("expected Plus, got %v", be.Op)
	}
}

func TestParseIfExpr(t *testing.T) {
	doc := mustParse(t, `x is if true then 1 else 0`)
	ie, ok := doc.Bindings[0].Value.(*IfExpr)
	if !ok {
		t.Fatalf("expected IfExpr, got %T", doc.Bindings[0].Value)
	}
	if ie.Cond == nil || ie.Then == nil || ie.Else == nil {
		t.Error("if expr has nil branches")
	}
}

func TestParseCaseExpr(t *testing.T) {
	doc := mustParse(t, "x is case 5 % 3\n    when 0 then \"a\"\n    when 1 then \"b\"\n    else \"c\"")
	ce, ok := doc.Bindings[0].Value.(*CaseExpr)
	if !ok {
		t.Fatalf("expected CaseExpr, got %T", doc.Bindings[0].Value)
	}
	if len(ce.Whens) != 2 {
		t.Errorf("expected 2 when clauses, got %d", len(ce.Whens))
	}
}

func TestParseCaseRequiresWhen(t *testing.T) {
	p := NewParser([]byte(`x is case 1 else 0`), "test.uzon")
	_, err := p.Parse()
	if err == nil {
		t.Error("expected error for case without when clause")
	}
}

func TestParseEnum(t *testing.T) {
	doc := mustParse(t, `color is red from red, green, blue called RGB`)
	b := doc.Bindings[0]
	fe, ok := b.Value.(*FromExpr)
	if !ok {
		t.Fatalf("expected FromExpr, got %T", b.Value)
	}
	if len(fe.Variants) != 3 {
		t.Errorf("expected 3 variants, got %d", len(fe.Variants))
	}
	if b.CalledName != "RGB" {
		t.Errorf("expected called name 'RGB', got %q", b.CalledName)
	}
}

func TestParseAsExpr(t *testing.T) {
	doc := mustParse(t, `x is 42 as i32`)
	ae, ok := doc.Bindings[0].Value.(*AsExpr)
	if !ok {
		t.Fatalf("expected AsExpr, got %T", doc.Bindings[0].Value)
	}
	if len(ae.TypeExpr.Path) != 1 || ae.TypeExpr.Path[0] != "i32" {
		t.Errorf("expected type path [i32], got %v", ae.TypeExpr.Path)
	}
}

func TestParseToExpr(t *testing.T) {
	doc := mustParse(t, `x is 3.14 to i32`)
	_, ok := doc.Bindings[0].Value.(*ToExpr)
	if !ok {
		t.Fatalf("expected ToExpr, got %T", doc.Bindings[0].Value)
	}
}

func TestParseAsToChain(t *testing.T) {
	doc := mustParse(t, `x is "42" as string to i32`)
	te, ok := doc.Bindings[0].Value.(*ToExpr)
	if !ok {
		t.Fatalf("expected ToExpr (outer), got %T", doc.Bindings[0].Value)
	}
	_, ok = te.Value.(*AsExpr)
	if !ok {
		t.Fatalf("expected AsExpr (inner), got %T", te.Value)
	}
}

func TestParseSelfRef(t *testing.T) {
	doc := mustParse(t, `y is self.x + 1`)
	be, ok := doc.Bindings[0].Value.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", doc.Bindings[0].Value)
	}
	me, ok := be.Left.(*MemberExpr)
	if !ok {
		t.Fatalf("expected MemberExpr, got %T", be.Left)
	}
	if me.Member != "x" {
		t.Errorf("expected member 'x', got %q", me.Member)
	}
}

func TestParseAreBinding(t *testing.T) {
	doc := mustParse(t, `names are "a", "b", "c"`)
	ae, ok := doc.Bindings[0].Value.(*AreExpr)
	if !ok {
		t.Fatalf("expected AreExpr, got %T", doc.Bindings[0].Value)
	}
	if len(ae.Elements) != 3 {
		t.Errorf("expected 3 elements, got %d", len(ae.Elements))
	}
}

func TestParseWithExpr(t *testing.T) {
	doc := mustParse(t, `dev is self.base with { debug is true }`)
	we, ok := doc.Bindings[0].Value.(*WithExpr)
	if !ok {
		t.Fatalf("expected WithExpr, got %T", doc.Bindings[0].Value)
	}
	if len(we.Override.Fields) != 1 {
		t.Errorf("expected 1 override field, got %d", len(we.Override.Fields))
	}
}

func TestParseExtendsExpr(t *testing.T) {
	doc := mustParse(t, `secure is self.base extends { tls is true }`)
	ee, ok := doc.Bindings[0].Value.(*ExtendsExpr)
	if !ok {
		t.Fatalf("expected ExtendsExpr, got %T", doc.Bindings[0].Value)
	}
	if len(ee.Extension.Fields) != 1 {
		t.Errorf("expected 1 extension field, got %d", len(ee.Extension.Fields))
	}
}

func TestParseTaggedUnion(t *testing.T) {
	doc := mustParse(t, `r is "ok" named ok from ok as string, err as string`)
	ne, ok := doc.Bindings[0].Value.(*NamedExpr)
	if !ok {
		t.Fatalf("expected NamedExpr, got %T", doc.Bindings[0].Value)
	}
	if ne.Tag != "ok" {
		t.Errorf("expected tag 'ok', got %q", ne.Tag)
	}
	if len(ne.Variants) != 2 {
		t.Errorf("expected 2 variants, got %d", len(ne.Variants))
	}
}

func TestParseFunction(t *testing.T) {
	doc := mustParse(t, `add is function a as i32, b as i32 returns i32 { a + b }`)
	fe, ok := doc.Bindings[0].Value.(*FunctionExpr)
	if !ok {
		t.Fatalf("expected FunctionExpr, got %T", doc.Bindings[0].Value)
	}
	if len(fe.Params) != 2 {
		t.Errorf("expected 2 params, got %d", len(fe.Params))
	}
}

func TestParseFunctionWithBindings(t *testing.T) {
	doc := mustParse(t, "f is function n as i32 returns i32 {\n\tx is n + 1\n\tx * 2\n}")
	fe, ok := doc.Bindings[0].Value.(*FunctionExpr)
	if !ok {
		t.Fatalf("expected FunctionExpr, got %T", doc.Bindings[0].Value)
	}
	if len(fe.Bindings) != 1 {
		t.Errorf("expected 1 intermediate binding, got %d", len(fe.Bindings))
	}
	if fe.Body == nil {
		t.Error("expected body expression, got nil")
	}
}

func TestParseFunctionParamOrder(t *testing.T) {
	p := NewParser([]byte(`f is function x as i64 default 0, y as i64 returns i64 { y }`), "test.uzon")
	_, err := p.Parse()
	if err == nil {
		t.Error("expected error for required param after defaulted param")
	}
}

func TestParseStructImport(t *testing.T) {
	doc := mustParse(t, `shared is struct "./shared"`)
	si, ok := doc.Bindings[0].Value.(*StructImportExpr)
	if !ok {
		t.Fatalf("expected StructImportExpr, got %T", doc.Bindings[0].Value)
	}
	if si.Path != "./shared" {
		t.Errorf("expected path './shared', got %q", si.Path)
	}
}

func TestParseOrElse(t *testing.T) {
	doc := mustParse(t, `x is self.y or else 0`)
	be, ok := doc.Bindings[0].Value.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", doc.Bindings[0].Value)
	}
	if be.Op != token.OrElse {
		t.Errorf("expected OrElse, got %v", be.Op)
	}
}

func TestParseOfExpr(t *testing.T) {
	doc := mustParse(t, `port is of self.config`)
	_, ok := doc.Bindings[0].Value.(*OfExpr)
	if !ok {
		t.Fatalf("expected OfExpr, got %T", doc.Bindings[0].Value)
	}
}

func TestParsePrecedence(t *testing.T) {
	doc := mustParse(t, `x is 1 + 2 * 3`)
	be, ok := doc.Bindings[0].Value.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", doc.Bindings[0].Value)
	}
	if be.Op != token.Plus {
		t.Errorf("expected outer op Plus, got %v", be.Op)
	}
	inner, ok := be.Right.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected inner BinaryExpr, got %T", be.Right)
	}
	if inner.Op != token.Star {
		t.Errorf("expected inner op Star, got %v", inner.Op)
	}
}

func TestParseBindingDecomposition(t *testing.T) {
	doc := mustParse(t, `x is not true`)
	unary, ok := doc.Bindings[0].Value.(*UnaryExpr)
	if !ok {
		t.Fatalf("expected UnaryExpr, got %T", doc.Bindings[0].Value)
	}
	if unary.Op != token.Not {
		t.Errorf("expected Not operator, got %v", unary.Op)
	}
}

func TestParseComplexExpr(t *testing.T) {
	src := `port is if env.PORT is undefined then 8080 else env.PORT to u16`
	doc := mustParse(t, src)
	if len(doc.Bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(doc.Bindings))
	}
}

func TestParseUnionExpr(t *testing.T) {
	doc := mustParse(t, `val is 42 from union i32, string`)
	ue, ok := doc.Bindings[0].Value.(*UnionExpr)
	if !ok {
		t.Fatalf("expected UnionExpr, got %T", doc.Bindings[0].Value)
	}
	if len(ue.MemberTypes) != 2 {
		t.Errorf("expected 2 member types, got %d", len(ue.MemberTypes))
	}
}

func TestParseLogicalOperators(t *testing.T) {
	doc := mustParse(t, `x is true and false or true`)
	be, ok := doc.Bindings[0].Value.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", doc.Bindings[0].Value)
	}
	if be.Op != token.Or {
		t.Errorf("expected outer op Or, got %v", be.Op)
	}
}

func TestParseMembershipIn(t *testing.T) {
	doc := mustParse(t, `found is 3 in [1, 2, 3]`)
	be, ok := doc.Bindings[0].Value.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", doc.Bindings[0].Value)
	}
	if be.Op != token.In {
		t.Errorf("expected In, got %v", be.Op)
	}
}

func TestParsePowerRightAssociative(t *testing.T) {
	doc := mustParse(t, `x is 2 ^ 3 ^ 4`)
	be, ok := doc.Bindings[0].Value.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", doc.Bindings[0].Value)
	}
	if be.Op != token.Caret {
		t.Errorf("expected outer Caret, got %v", be.Op)
	}
	inner, ok := be.Right.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected inner BinaryExpr, got %T", be.Right)
	}
	if inner.Op != token.Caret {
		t.Errorf("expected inner Caret, got %v", inner.Op)
	}
}

func TestParseEnvAsBindingName(t *testing.T) {
	doc := mustParse(t, `env is "production"`)
	if len(doc.Bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(doc.Bindings))
	}
	if doc.Bindings[0].Name != "env" {
		t.Errorf("expected name 'env', got %q", doc.Bindings[0].Name)
	}
}

func TestParseFunctionBodyMustReturnExpr(t *testing.T) {
	p := NewParser([]byte(`f is function returns i32 { }`), "test.uzon")
	_, err := p.Parse()
	if err == nil {
		t.Error("expected error for function without return expression")
	}
}

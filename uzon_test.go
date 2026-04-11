// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	v, err := Parse([]byte(`x is 42, y is "hello"`))
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != KindStruct {
		t.Fatalf("want struct, got %s", v.Kind)
	}
	x := v.Struct.Get("x")
	if x.Int.Int64() != 42 {
		t.Errorf("x: want 42, got %d", x.Int.Int64())
	}
	y := v.Struct.Get("y")
	if y.Str != "hello" {
		t.Errorf("y: want \"hello\", got %q", y.Str)
	}
}

func TestValueMarshal(t *testing.T) {
	v, err := Parse([]byte(`
name is "UZON"
port is 8080
debug is true`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := v.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `name is "UZON"`) {
		t.Errorf("missing name: %s", s)
	}
	if !strings.Contains(s, "port is 8080") {
		t.Errorf("missing port: %s", s)
	}
	if !strings.Contains(s, "debug is true") {
		t.Errorf("missing debug: %s", s)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	type Config struct {
		Host  string `uzon:"host"`
		Port  int    `uzon:"port"`
		Debug bool   `uzon:"debug"`
	}

	original := Config{Host: "localhost", Port: 8080, Debug: true}
	data, err := Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Config
	err = Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("unmarshal error: %v\ndata: %s", err, data)
	}

	if decoded.Host != original.Host || decoded.Port != original.Port || decoded.Debug != original.Debug {
		t.Errorf("round trip failed: %+v != %+v", decoded, original)
	}
}

func TestUnmarshalStruct(t *testing.T) {
	type Server struct {
		Host string `uzon:"host"`
		Port int    `uzon:"port"`
	}

	var s Server
	err := Unmarshal([]byte(`host is "localhost", port is 8080`), &s)
	if err != nil {
		t.Fatal(err)
	}
	if s.Host != "localhost" {
		t.Errorf("host: want \"localhost\", got %q", s.Host)
	}
	if s.Port != 8080 {
		t.Errorf("port: want 8080, got %d", s.Port)
	}
}

func TestUnmarshalNested(t *testing.T) {
	type DB struct {
		Host string `uzon:"host"`
		Port int    `uzon:"port"`
	}
	type Config struct {
		Name string `uzon:"name"`
		DB   DB     `uzon:"db"`
	}

	var c Config
	err := Unmarshal([]byte(`
name is "myapp"
db is { host is "db.local", port is 5432 }`), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "myapp" {
		t.Errorf("name: want \"myapp\", got %q", c.Name)
	}
	if c.DB.Host != "db.local" {
		t.Errorf("db.host: want \"db.local\", got %q", c.DB.Host)
	}
	if c.DB.Port != 5432 {
		t.Errorf("db.port: want 5432, got %d", c.DB.Port)
	}
}

func TestUnmarshalList(t *testing.T) {
	type Config struct {
		Tags []string `uzon:"tags"`
	}

	var c Config
	err := Unmarshal([]byte(`tags is [ "a", "b", "c" ]`), &c)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Tags) != 3 || c.Tags[0] != "a" {
		t.Errorf("tags: want [a b c], got %v", c.Tags)
	}
}

func TestUnmarshalMap(t *testing.T) {
	var m map[string]any
	err := Unmarshal([]byte(`x is 42, y is "hello"`), &m)
	if err != nil {
		t.Fatal(err)
	}
	if m["x"] != int64(42) {
		t.Errorf("x: want 42, got %v (%T)", m["x"], m["x"])
	}
	if m["y"] != "hello" {
		t.Errorf("y: want \"hello\", got %v", m["y"])
	}
}

func TestUnmarshalWithComputation(t *testing.T) {
	type Config struct {
		Base   int `uzon:"base"`
		Double int `uzon:"double"`
	}
	var c Config
	err := Unmarshal([]byte(`
base is 21
double is base * 2`), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Double != 42 {
		t.Errorf("double: want 42, got %d", c.Double)
	}
}

func TestUnmarshalOrElse(t *testing.T) {
	type Config struct {
		Port int `uzon:"port"`
	}
	var c Config
	err := Unmarshal([]byte(`port is missing_port or else 8080`), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 8080 {
		t.Errorf("port: want 8080, got %d", c.Port)
	}
}

func TestValueOf(t *testing.T) {
	type Item struct {
		Name  string `uzon:"name"`
		Price int    `uzon:"price"`
	}

	item := Item{Name: "widget", Price: 100}
	v, err := ValueOf(item)
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != KindStruct {
		t.Fatalf("want struct, got %s", v.Kind)
	}
	name := v.Struct.Get("name")
	if name.Str != "widget" {
		t.Errorf("name: want \"widget\", got %q", name.Str)
	}
}

func TestValueDecode(t *testing.T) {
	v := NewStruct(
		Field{Name: "host", Value: String("localhost")},
		Field{Name: "port", Value: Int(8080)},
	)

	type Server struct {
		Host string `uzon:"host"`
		Port int    `uzon:"port"`
	}

	var s Server
	if err := v.Decode(&s); err != nil {
		t.Fatal(err)
	}
	if s.Host != "localhost" || s.Port != 8080 {
		t.Errorf("decode failed: %+v", s)
	}
}

func TestMarshalGoMap(t *testing.T) {
	m := map[string]any{
		"name": "test",
		"port": 8080,
	}
	data, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"test"`) || !strings.Contains(s, "8080") {
		t.Errorf("marshal map: %s", s)
	}
}

func TestMarshalSlice(t *testing.T) {
	v, err := ValueOf([]int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != KindList || len(v.List.Elements) != 3 {
		t.Errorf("want list of 3, got %s", v.Kind)
	}
}

func TestUnmarshalEnum(t *testing.T) {
	var m map[string]any
	err := Unmarshal([]byte(`color is green from red, green, blue`), &m)
	if err != nil {
		t.Fatal(err)
	}
	if m["color"] != "green" {
		t.Errorf("color: want \"green\", got %v", m["color"])
	}
}

func TestUnmarshalJsonTag(t *testing.T) {
	type Config struct {
		Host string `json:"host_name"`
		Port int    `json:"port_number"`
	}
	var c Config
	err := Unmarshal([]byte(`host_name is "localhost", port_number is 3000`), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Host != "localhost" {
		t.Errorf("host: want \"localhost\", got %q", c.Host)
	}
}

func TestUnmarshalOmitempty(t *testing.T) {
	type Config struct {
		Name  string `uzon:"name"`
		Debug bool   `uzon:"debug,omitempty"`
	}
	c := Config{Name: "test"}
	data, err := Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "debug") {
		t.Errorf("omitempty field should not be in output: %s", s)
	}
}

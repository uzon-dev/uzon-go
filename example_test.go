// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon_test

import (
	"encoding/json"
	"fmt"

	"github.com/uzon-dev/uzon-go"
)

func Example() {
	data := []byte(`
		name is "my-service"
		port is 8080
		debug is false
	`)

	v, err := uzon.Parse(data)
	if err != nil {
		panic(err)
	}

	name, _ := v.GetPath("name").AsString()
	port, _ := v.GetPath("port").AsInt()
	fmt.Println(name, port)
	// Output: my-service 8080
}

func ExampleParse() {
	v, err := uzon.Parse([]byte(`x is 42`))
	if err != nil {
		panic(err)
	}
	n, _ := v.GetPath("x").AsInt()
	fmt.Println(n)
	// Output: 42
}

func ExampleNewStruct() {
	v := uzon.NewStruct(
		uzon.Bind("host", "localhost"),
		uzon.Bind("port", 8080),
		uzon.Bind("debug", false),
	)
	fmt.Println(v)
	// Output: { host is "localhost", port is 8080, debug is false }
}

func ExampleValue_GetPath() {
	v := uzon.NewStruct(
		uzon.Bind("server", uzon.NewStruct(
			uzon.Bind("host", "localhost"),
			uzon.Bind("port", 8080),
		)),
	)
	host, _ := v.GetPath("server.host").AsString()
	fmt.Println(host)
	// Output: localhost
}

func ExampleValue_SetPath() {
	v := uzon.NewStruct(
		uzon.Bind("server", uzon.NewStruct(
			uzon.Bind("host", "localhost"),
			uzon.Bind("port", 8080),
		)),
	)
	v.SetPath("server.port", 9090)
	port, _ := v.GetPath("server.port").AsInt()
	fmt.Println(port)
	// Output: 9090
}

func ExampleAdd() {
	r, _ := uzon.Add(uzon.Int(3), 7)
	fmt.Println(r)
	// Output: 10
}

func ExampleEqual() {
	fmt.Println(uzon.Equal(uzon.Int(42), 42))
	fmt.Println(uzon.Equal(uzon.String("hello"), "world"))
	// Output:
	// true
	// false
}

func ExampleValue_EqualTo() {
	v := uzon.Int(42)
	fmt.Println(v.EqualTo(42))
	fmt.Println(v.EqualTo("hello"))
	// Output:
	// true
	// false
}

func ExampleMerge() {
	base := uzon.NewStruct(
		uzon.Bind("host", "localhost"),
		uzon.Bind("port", 8080),
	)
	override := uzon.NewStruct(
		uzon.Bind("port", 9090),
		uzon.Bind("debug", true),
	)
	merged, _ := uzon.Merge(base, override)
	fmt.Println(merged)
	// Output: { host is "localhost", port is 9090, debug is true }
}

func ExampleDeepMerge() {
	base := uzon.NewStruct(
		uzon.Bind("server", uzon.NewStruct(
			uzon.Bind("host", "localhost"),
			uzon.Bind("port", 8080),
		)),
	)
	override := uzon.NewStruct(
		uzon.Bind("server", uzon.NewStruct(
			uzon.Bind("port", 9090),
		)),
	)
	merged, _ := uzon.DeepMerge(base, override)
	host, _ := merged.GetPath("server.host").AsString()
	port, _ := merged.GetPath("server.port").AsInt()
	fmt.Println(host, port)
	// Output: localhost 9090
}

func ExampleClone() {
	original := uzon.NewStruct(uzon.Bind("x", 42))
	cloned := uzon.Clone(original)
	original.SetPath("x", 99)
	n, _ := cloned.GetPath("x").AsInt()
	fmt.Println(n)
	// Output: 42
}

func ExampleWalk() {
	v := uzon.NewStruct(
		uzon.Bind("name", "alice"),
		uzon.Bind("nested", uzon.NewStruct(
			uzon.Bind("role", "engineer"),
		)),
	)
	uzon.Walk(v, func(path string, val *uzon.Value) error {
		if s, ok := val.AsString(); ok {
			fmt.Printf("%s = %s\n", path, s)
		}
		return nil
	})
	// Output:
	// name = alice
	// nested.role = engineer
}

func ExampleValue_MarshalJSON() {
	v := uzon.NewStruct(
		uzon.Bind("name", "test"),
		uzon.Bind("values", uzon.ListOf(1, 2, 3)),
	)
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
	// Output: {"name":"test","values":[1,2,3]}
}

func ExampleFromJSON() {
	v, _ := uzon.FromJSON([]byte(`{"name":"test","count":42}`))
	name, _ := v.GetPath("name").AsString()
	count, _ := v.GetPath("count").AsInt()
	fmt.Println(name, count)
	// Output: test 42
}

func ExampleUnmarshal() {
	var config struct {
		Host string `uzon:"host"`
		Port int    `uzon:"port"`
	}
	uzon.Unmarshal([]byte(`host is "localhost", port is 8080`), &config)
	fmt.Println(config.Host, config.Port)
	// Output: localhost 8080
}

func ExampleListOf() {
	v := uzon.ListOf(1, 2, 3)
	fmt.Println(v)
	// Output: [ 1, 2, 3 ]
}

func ExampleContains() {
	list := uzon.ListOf(1, 2, 3)
	found, _ := uzon.Contains(list, 2)
	fmt.Println(found)
	// Output: true
}

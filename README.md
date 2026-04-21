# uzon-go

Go implementation of [UZON](https://uzon.dev), a typed configuration language.

Implements [UZON specification v0.11](https://uzon.dev). Requires Go 1.25 or later.

```
go get github.com/uzon-dev/uzon-go
```

## Quick Start

**Parse a UZON file:**

```go
v, err := uzon.ParseFile("config.uzon")
if err != nil {
    log.Fatal(err)
}

host, _ := v.GetPath("server.host").AsString()
port, _ := v.GetPath("server.port").AsInt()
```

**Unmarshal into a Go struct (like `encoding/json`):**

```go
var config struct {
    Host string `uzon:"host"`
    Port int    `uzon:"port"`
}
err := uzon.UnmarshalFile("config.uzon", &config)
```

**Build values programmatically:**

```go
v := uzon.NewStruct(
    uzon.Bind("host", "localhost"),
    uzon.Bind("port", 8080),
    uzon.Bind("tags", uzon.ListOf("web", "api")),
)
```

**Convert to/from JSON:**

```go
// UZON -> JSON
jsonBytes, _ := json.Marshal(v)

// JSON -> UZON Value
v, err := uzon.FromJSON(jsonBytes)
```

## UZON Syntax

```
name is "my-service"
version is 3
debug is false

server is {
    host is "0.0.0.0"
    port is 8080 as u16
}

tags are "web", "api", "v3"

limits is {
    max_conn is 1000
    timeout is 30.0 as f32
}
```

UZON supports structs, lists, tuples, enums, tagged unions, functions, conditionals (`if`/`case`/`case type`/`case named`), arithmetic, type checking (`is type`/`is not type`), environment variables (`env.VAR`), file imports (`from "path"`), struct overrides (`with`), extensions (`plus`), and a standard library (`std.*`). See the full [UZON specification](https://uzon.dev) for details.

---

## API Reference

All operation functions (`Add`, `Equal`, `Merge`, etc.) accept `any` — both `*Value` and Go primitives (`int`, `string`, `bool`, `float64`, etc.) are supported. Go primitives are auto-wrapped internally.

### Parsing

```go
func Parse(data []byte) (*Value, error)
```
Parses UZON source text and evaluates it, returning a `*Value`.

```go
func ParseFile(path string) (*Value, error)
```
Reads a `.uzon` file, parses, and evaluates it. File imports (`from`) are resolved relative to the file's directory.

### Go Reflection Encoding

`encoding/json`-style marshal/unmarshal using struct tags (`uzon:"fieldname"`).

```go
func Marshal(v any) ([]byte, error)
```
Converts a Go value to UZON document text via reflection.

```go
func Unmarshal(data []byte, v any) error
```
Parses UZON text and decodes it into a Go value.

```go
func UnmarshalFile(path string, v any) error
```
Reads a `.uzon` file, parses, and decodes it into a Go value.

```go
func ValueOf(v any) (*Value, error)
```
Converts a Go value to `*Value` using reflection. Supported types: `bool`, integers, floats, `string`, slices, maps, structs.

```go
func (v *Value) Decode(target any) error
```
Decodes a `*Value` into a Go value (the inverse of `ValueOf`).

### JSON Interop

`*Value` implements `json.Marshaler` and `json.Unmarshaler`. Struct field order is preserved. NaN and Infinity are encoded as `null`. Tagged unions are encoded as `{"_tag": "name", "_value": inner}`.

```go
func FromJSON(data []byte) (*Value, error)
```
Converts JSON bytes to `*Value`. Integers that fit `int64` are stored as `KindInt`, others as `KindFloat`. JSON object key order is preserved.

```go
func (v *Value) MarshalJSON() ([]byte, error)   // json.Marshaler
func (v *Value) UnmarshalJSON(data []byte) error // json.Unmarshaler
```

### Value Constructors

```go
func Null() *Value               // null
func Undefined() *Value          // undefined (missing value)
func Bool(b bool) *Value         // boolean
func Int(n int64) *Value         // signed integer
func Uint(n uint64) *Value       // unsigned integer
func BigInt(n *big.Int) *Value   // arbitrary-precision integer
func Float64(f float64) *Value   // IEEE 754 float
func BigFloat(f *big.Float) *Value // arbitrary-precision float
func String(s string) *Value     // UTF-8 string
func NewStruct(fields ...Field) *Value   // struct from fields
func NewTuple(elems ...*Value) *Value    // tuple from elements
func NewList(elems []*Value, elemType *TypeInfo) *Value // list
```

### Convenience Builders

Auto-wrap Go primitives into `*Value`. Panics on unsupported types.

```go
func Bind(name string, v any) Field
```
Creates a `Field` with automatic conversion. `v` may be `*Value`, `int`, `string`, `bool`, `float64`, etc.

```go
func ListOf(elems ...any) *Value
```
Creates a list with auto-wrapped elements.

```go
func TupleOf(elems ...any) *Value
```
Creates a tuple with auto-wrapped elements.

### Type Predicates

```go
func (v *Value) IsNull() bool
```
Reports whether `v` is a null value.

```go
func (v *Value) IsUndefined() bool
```
Reports whether `v` is an undefined value.

### Unwrap Accessors

Each returns `(zero, false)` if `v` is not the expected kind.

```go
func (v *Value) AsInt() (int64, bool)
```
Returns the integer as `int64`. Returns `false` on overflow or wrong kind.

```go
func (v *Value) AsFloat() (float64, bool)
```
Returns the float as `float64`. Returns `(NaN, true)` for NaN values.

```go
func (v *Value) AsString() (string, bool)
```
Returns the string value.

```go
func (v *Value) AsBool() (bool, bool)
```
Returns the boolean value.

### Undefined Coalescing

```go
func (v *Value) OrElse(fallback *Value) *Value
```
Returns `v` if it is not undefined, otherwise returns `fallback`. Null is **not** replaced.

### Collection Accessors

```go
func (v *Value) Len() int
```
Returns field count (struct), element count (list/tuple), or codepoint count (string). Returns 0 for other kinds.

```go
func (v *Value) Keys() []string
```
Returns struct field names in source order. Returns `nil` for non-struct kinds.

### Path-Based Access

Dot-separated paths support struct field names and numeric indices for tuples/lists. Tagged unions and unions are transparently unwrapped during traversal.

```go
func (v *Value) GetPath(path string) *Value
```
Returns the nested value at `path`. Returns `nil` if any segment is not found.

```go
v.GetPath("server.host")   // struct field
v.GetPath("pair.0")        // tuple index
v.GetPath("items.2")       // list index
```

```go
func (v *Value) SetPath(path string, val any) error
```
Sets the nested value at `path`. `val` is auto-wrapped if not `*Value`. Intermediate path segments must already exist; leaf struct fields are created if absent.

### Struct Operations

```go
func (s *StructValue) Get(name string) *Value
```
Returns the field value by name, or `nil`.

```go
func (s *StructValue) Set(name string, v *Value)
```
Sets or adds a field. If the field exists, its value is replaced; otherwise it is appended.

```go
func (s *StructValue) Delete(name string) bool
```
Removes a field by name. Returns `true` if found and removed.

### List Operations

```go
func (l *ListValue) Push(elems ...*Value)
```
Appends elements to the end.

```go
func (l *ListValue) Pop() (*Value, bool)
```
Removes and returns the last element. Returns `(nil, false)` if empty.

### Merge

```go
func Merge(a, b any) (*Value, error)
```
Returns a new struct with all fields from `a`, overridden by fields from `b`. Fields from `a` appear first in order; new fields from `b` are appended. Field values are cloned. Both operands must be structs.

```go
func DeepMerge(a, b any) (*Value, error)
```
Like `Merge`, but when both `a` and `b` have a field with the same name and both values are structs, the values are recursively deep-merged. Tagged unions and unions wrapping structs are transparently unwrapped. Returns an independent clone.

### Clone

```go
func Clone(v *Value) *Value
```
Returns a deep copy of the value tree. The cloned value is completely independent of the original. `TypeInfo` pointers are shared (read-only). Returns `nil` for `nil` input.

### Walk

```go
type WalkFunc func(path string, v *Value) error
```

```go
func Walk(v *Value, fn WalkFunc) error
```
Traverses the value tree depth-first, calling `fn` for each value. `path` is the dot-separated path to the current value (empty string for root). Compound values are visited before their children. Return a non-nil error to stop the walk.

### Arithmetic Operations

All operations require operands of the same numeric kind (`KindInt` or `KindFloat`). Integer operations include overflow checking for typed values (e.g., `i8`, `u32`).

```go
func Add(a, b any) (*Value, error)      // a + b
func Sub(a, b any) (*Value, error)      // a - b
func Mul(a, b any) (*Value, error)      // a * b
func Div(a, b any) (*Value, error)      // a / b (integer: error on zero; float: IEEE 754)
func Mod(a, b any) (*Value, error)      // a % b (integer: error on zero)
func Pow(a, b any) (*Value, error)      // a ^ b (integer: exponent must be non-negative)
func Negate(v any) (*Value, error)      // -v
```

### Logical Operations

```go
func Not(v any) (*Value, error)
```
Boolean negation. Operand must be `KindBool`.

### Comparison

```go
func Equal(a, b any) bool
```
Deep equality. Values of different kinds are never equal. NaN != NaN (IEEE 754).

```go
func (v *Value) EqualTo(other any) bool
```
Method form of `Equal`.

```go
func Compare(a, b any) (int, error)
```
Ordered comparison: `-1` if a < b, `0` if a == b, `+1` if a > b. Both operands must be the same comparable kind (`KindInt`, `KindFloat`, or `KindString`).

### String & List Operations

```go
func Concat(a, b any) (*Value, error)
```
Concatenates two strings or two lists.

```go
func Repeat(v any, n int) (*Value, error)
```
Repeats a string or list `n` times.

```go
func Contains(list, elem any) (bool, error)
```
Reports whether `elem` is in `list` (deep equality).

### Type Conversions

```go
func ToString(v any) (*Value, error)
```
Converts to string. Supported: string, bool, int, float, null, enum, tagged union, union.

```go
func ToInt(v any) (*Value, error)
```
Converts to integer. Supported: int (identity), float (truncated), string (parsed). String parsing supports decimal, `0x` hex, `0o` octal, `0b` binary.

```go
func ToFloat(v any) (*Value, error)
```
Converts to float. Supported: float (identity), int, string (parsed). String parsing supports decimal, `"inf"`, `"-inf"`, `"nan"`.

### Serialization

```go
func (v *Value) Marshal() ([]byte, error)
```
Serializes to UZON text.

```go
func (v *Value) String() string
```
Returns the UZON text representation. Implements `fmt.Stringer`.

```go
func (v *Value) MarshalText() ([]byte, error)
```
Returns the UZON text as bytes. Implements `encoding.TextMarshaler`.

### Types

#### ValueKind

```go
type ValueKind int
```

| Constant          | Description                           |
| ----------------- | ------------------------------------- |
| `KindNull`        | Intentionally empty value             |
| `KindUndefined`   | Missing value; resolved with `OrElse` |
| `KindBool`        | `true` or `false`                     |
| `KindInt`         | Arbitrary-precision integer           |
| `KindFloat`       | IEEE 754 float                        |
| `KindString`      | UTF-8 string                          |
| `KindStruct`      | Named field collection                |
| `KindTuple`       | Fixed-length heterogeneous sequence   |
| `KindList`        | Variable-length homogeneous sequence  |
| `KindEnum`        | Named variant from a fixed set        |
| `KindUnion`       | Untagged union                        |
| `KindTaggedUnion` | Union with variant labels             |
| `KindFunction`    | First-class callable                  |

```go
func (k ValueKind) String() string // "null", "integer", "float", "string", ...
```

#### Value

The central type. Exactly one typed field is meaningful, determined by `Kind`.

```go
type Value struct {
    Kind        ValueKind
    Type        *TypeInfo    // optional type annotation or named type
    Adoptable   bool         // untyped literal that adopts type from context (internal)
    Bool        bool
    Int         *big.Int
    Float       *big.Float
    FloatIsNaN  bool         // true when value is NaN (big.Float cannot represent NaN)
    Str         string
    Struct      *StructValue
    Tuple       *TupleValue
    List        *ListValue
    Enum        *EnumValue
    Union       *UnionValue
    TaggedUnion *TaggedUnionValue
    Function    *FunctionValue
}
```

#### TypeInfo

```go
type TypeInfo struct {
    Name     string   // type name from "called", empty if anonymous
    BaseType string   // "i32", "f64", "bool", "string", etc.
    BitSize  int      // bit width for numeric types
    Signed   bool     // true for signed integers (iN)
    Path     []string // qualified type path segments
}
```

#### StructValue

Ordered field collection with O(1) lookup by name.

```go
type StructValue struct {
    Fields []Field
}
```

#### Field

```go
type Field struct {
    Name  string
    Value *Value
}
```

#### ListValue

```go
type ListValue struct {
    Elements    []*Value
    ElementType *TypeInfo // nil if untyped
}
```

#### TupleValue

```go
type TupleValue struct {
    Elements []*Value
}
```

#### EnumValue

```go
type EnumValue struct {
    Variant  string   // selected variant
    Variants []string // all valid variants
}
```

#### TaggedUnionValue

```go
type TaggedUnionValue struct {
    Tag      string          // active variant label
    Inner    *Value          // variant value
    Variants []TaggedVariant // all variant definitions
}
```

#### TaggedVariant

```go
type TaggedVariant struct {
    Name string
    Type *TypeInfo
}
```

#### UnionValue

```go
type UnionValue struct {
    Inner       *Value
    MemberTypes []*TypeInfo
}
```

#### FunctionValue

```go
type FunctionValue struct {
    Params     []FuncParam
    ReturnType *TypeInfo
    Body       any // *ast.FunctionExpr, set during evaluation
    Scope      any // *Scope, captured lexical scope
}
```

#### FuncParam

```go
type FuncParam struct {
    Name    string
    Type    *TypeInfo
    Default *Value // nil if no default
}
```

#### PosError

Error annotated with source position. Implements `error` and `Unwrap()`.

```go
type PosError struct {
    Pos   token.Pos
    Msg   string
    Cause error
}
```

#### Evaluator

```go
func NewEvaluator() *Evaluator
func (ev *Evaluator) EvalDocument(doc *ast.Document) (*Value, error)
```

Low-level access to the evaluation engine. Most users should use `Parse` or `ParseFile` instead. `NewEvaluator` captures the process environment for `env.*` expressions.

## License

MIT

// SPDX-FileCopyrightText: © 2026 Suho Kang
// SPDX-License-Identifier: MIT

package uzon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
)

// MarshalJSON implements encoding/json.Marshaler.
// Struct field order is preserved. NaN and Infinity are encoded as null
// since JSON has no representation for them. Tagged unions are encoded as
// {"_tag": "name", "_value": inner}.
func (v *Value) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	writeJSON(&buf, v)
	return buf.Bytes(), nil
}

func writeJSON(buf *bytes.Buffer, v *Value) {
	switch v.Kind {
	case KindNull, KindUndefined, KindFunction:
		buf.WriteString("null")

	case KindBool:
		if v.Bool {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}

	case KindInt:
		buf.WriteString(v.Int.String())

	case KindFloat:
		if v.FloatIsNaN || v.Float.IsInf() {
			buf.WriteString("null")
			return
		}
		f, _ := v.Float.Float64()
		if math.IsInf(f, 0) {
			buf.WriteString("null") // exceeds float64 range
		} else {
			buf.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
		}

	case KindString:
		b, _ := json.Marshal(v.Str)
		buf.Write(b)

	case KindStruct:
		buf.WriteByte('{')
		for i, f := range v.Struct.Fields {
			if i > 0 {
				buf.WriteByte(',')
			}
			key, _ := json.Marshal(f.Name)
			buf.Write(key)
			buf.WriteByte(':')
			writeJSON(buf, f.Value)
		}
		buf.WriteByte('}')

	case KindList:
		buf.WriteByte('[')
		for i, e := range v.List.Elements {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSON(buf, e)
		}
		buf.WriteByte(']')

	case KindTuple:
		buf.WriteByte('[')
		for i, e := range v.Tuple.Elements {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSON(buf, e)
		}
		buf.WriteByte(']')

	case KindEnum:
		b, _ := json.Marshal(v.Enum.Variant)
		buf.Write(b)

	case KindTaggedUnion:
		buf.WriteString(`{"_tag":`)
		tag, _ := json.Marshal(v.TaggedUnion.Tag)
		buf.Write(tag)
		buf.WriteString(`,"_value":`)
		writeJSON(buf, v.TaggedUnion.Inner)
		buf.WriteByte('}')

	case KindUnion:
		writeJSON(buf, v.Union.Inner)
	}
}

// UnmarshalJSON implements encoding/json.Unmarshaler.
// JSON numbers are preserved as integers when possible, otherwise as floats.
// JSON objects become KindStruct (field order preserved), arrays become KindList.
func (v *Value) UnmarshalJSON(data []byte) error {
	parsed, err := FromJSON(data)
	if err != nil {
		return err
	}
	*v = *parsed
	return nil
}

// FromJSON converts JSON bytes to a *Value.
// Uses json.Number for precise numeric handling: integers that fit int64
// are stored as KindInt, others as KindFloat. JSON object key order is preserved.
func FromJSON(data []byte) (*Value, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return decodeJSONValue(dec)
}

func decodeJSONValue(dec *json.Decoder) (*Value, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}

	switch t := tok.(type) {
	case nil:
		return Null(), nil

	case bool:
		return Bool(t), nil

	case json.Number:
		if n, err := t.Int64(); err == nil {
			return Int(n), nil
		}
		if f, err := t.Float64(); err == nil {
			return Float64(f), nil
		}
		n := new(big.Int)
		if _, ok := n.SetString(string(t), 10); ok {
			return BigInt(n), nil
		}
		return nil, fmt.Errorf("invalid JSON number: %s", t)

	case string:
		return String(t), nil

	case json.Delim:
		switch t {
		case '{':
			var fields []Field
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("expected string key, got %T", keyTok)
				}
				val, err := decodeJSONValue(dec)
				if err != nil {
					return nil, fmt.Errorf("key %q: %w", key, err)
				}
				fields = append(fields, Field{Name: key, Value: val})
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return &Value{Kind: KindStruct, Struct: &StructValue{Fields: fields}}, nil

		case '[':
			var elems []*Value
			for dec.More() {
				val, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				elems = append(elems, val)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return NewList(elems, nil), nil
		}
	}
	return nil, fmt.Errorf("unexpected JSON token: %v", tok)
}

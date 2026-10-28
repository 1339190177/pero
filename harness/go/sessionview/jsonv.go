package sessionview

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// Values are JSON as JavaScript sees it: nil, bool, float64, string, []any and
// *Obj. Objects keep insertion order because the projection re-stringifies
// some subtrees (tool arguments, header signatures) and must match the order a
// JS engine would produce; plain Go maps would sort the keys.

// Obj is an insertion-ordered JSON object.
type Obj struct {
	keys []string
	vals map[string]any
}

func NewObj() *Obj { return &Obj{vals: map[string]any{}} }

func (o *Obj) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

func (o *Obj) Keys() []string {
	if o == nil {
		return nil
	}
	return o.keys
}

func (o *Obj) Has(key string) bool {
	if o == nil {
		return false
	}
	_, ok := o.vals[key]
	return ok
}

func (o *Obj) Get(key string) any {
	if o == nil {
		return nil
	}
	return o.vals[key]
}

// Set keeps the key's first position, like assigning to a JS object.
func (o *Obj) Set(key string, value any) *Obj {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = value
	return o
}

func (o *Obj) Delete(key string) {
	if _, ok := o.vals[key]; !ok {
		return
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i:i], o.keys[i+1:]...)
			break
		}
	}
}

// Clone deep-copies a JSON value.
func Clone(v any) any {
	switch t := v.(type) {
	case *Obj:
		if t == nil {
			return (*Obj)(nil)
		}
		c := &Obj{keys: append([]string(nil), t.keys...), vals: make(map[string]any, len(t.vals))}
		for k, x := range t.vals {
			c.vals[k] = Clone(x)
		}
		return c
	case []any:
		c := make([]any, len(t))
		for i, x := range t {
			c[i] = Clone(x)
		}
		return c
	}
	return v
}

// ErrUnrepresentableNumber is JavaScript's UNREPRESENTABLE_JSON_NUMBER: -0 would
// not survive a JSON round trip.
var ErrUnrepresentableNumber = errors.New("UNREPRESENTABLE_JSON_NUMBER: invalid content number")

// Parse reads one JSON document. Numbers become float64 as in JSON.parse.
func Parse(data []byte) (any, error) {
	p := parser{data: data}
	p.skip()
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.i != len(p.data) {
		return nil, fmt.Errorf("sessionview: trailing JSON at offset %d", p.i)
	}
	return v, nil
}

type parser struct {
	data []byte
	i    int
}

func (p *parser) skip() {
	for p.i < len(p.data) {
		switch p.data[p.i] {
		case ' ', '\t', '\r', '\n':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) fail(what string) error {
	return fmt.Errorf("sessionview: invalid JSON (%s) at offset %d", what, p.i)
}

func (p *parser) value() (any, error) {
	if p.i >= len(p.data) {
		return nil, p.fail("unexpected end")
	}
	switch c := p.data[p.i]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		return p.str()
	case c == 't':
		return p.word("true", true)
	case c == 'f':
		return p.word("false", false)
	case c == 'n':
		return p.word("null", nil)
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	}
	return nil, p.fail("unexpected character")
}

func (p *parser) word(text string, value any) (any, error) {
	if p.i+len(text) > len(p.data) || string(p.data[p.i:p.i+len(text)]) != text {
		return nil, p.fail("bad literal")
	}
	p.i += len(text)
	return value, nil
}

func (p *parser) number() (any, error) {
	start := p.i
	for p.i < len(p.data) {
		c := p.data[p.i]
		if c >= '0' && c <= '9' || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' {
			p.i++
			continue
		}
		break
	}
	f, err := strconv.ParseFloat(string(p.data[start:p.i]), 64)
	if err != nil || math.IsInf(f, 0) {
		return nil, p.fail("bad number")
	}
	if f == 0 && math.Signbit(f) {
		return nil, ErrUnrepresentableNumber
	}
	return f, nil
}

func (p *parser) object() (any, error) {
	p.i++
	obj := NewObj()
	p.skip()
	if p.i < len(p.data) && p.data[p.i] == '}' {
		p.i++
		return obj, nil
	}
	for {
		p.skip()
		if p.i >= len(p.data) || p.data[p.i] != '"' {
			return nil, p.fail("object key")
		}
		key, err := p.str()
		if err != nil {
			return nil, err
		}
		p.skip()
		if p.i >= len(p.data) || p.data[p.i] != ':' {
			return nil, p.fail("colon")
		}
		p.i++
		p.skip()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		obj.Set(key.(string), v)
		p.skip()
		if p.i >= len(p.data) {
			return nil, p.fail("unterminated object")
		}
		if p.data[p.i] == ',' {
			p.i++
			continue
		}
		if p.data[p.i] == '}' {
			p.i++
			return obj, nil
		}
		return nil, p.fail("object separator")
	}
}

func (p *parser) array() (any, error) {
	p.i++
	out := []any{}
	p.skip()
	if p.i < len(p.data) && p.data[p.i] == ']' {
		p.i++
		return out, nil
	}
	for {
		p.skip()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.skip()
		if p.i >= len(p.data) {
			return nil, p.fail("unterminated array")
		}
		if p.data[p.i] == ',' {
			p.i++
			continue
		}
		if p.data[p.i] == ']' {
			p.i++
			return out, nil
		}
		return nil, p.fail("array separator")
	}
}

func (p *parser) str() (any, error) {
	p.i++
	start := p.i
	for p.i < len(p.data) {
		c := p.data[p.i]
		if c == '"' {
			s := string(p.data[start:p.i])
			p.i++
			if !utf8.ValidString(s) {
				return nil, p.fail("invalid UTF-8")
			}
			return s, nil
		}
		if c == '\\' {
			break
		}
		if c < 0x20 {
			return nil, p.fail("control character in string")
		}
		p.i++
	}
	if p.i >= len(p.data) {
		return nil, p.fail("unterminated string")
	}
	// Escapes present: decode into a buffer.
	buf := append([]byte(nil), p.data[start:p.i]...)
	for p.i < len(p.data) {
		c := p.data[p.i]
		switch {
		case c == '"':
			p.i++
			if !utf8.Valid(buf) {
				return nil, p.fail("invalid UTF-8")
			}
			return string(buf), nil
		case c == '\\':
			p.i++
			if p.i >= len(p.data) {
				return nil, p.fail("bad escape")
			}
			e := p.data[p.i]
			p.i++
			switch e {
			case '"', '\\', '/':
				buf = append(buf, e)
			case 'b':
				buf = append(buf, '\b')
			case 'f':
				buf = append(buf, '\f')
			case 'n':
				buf = append(buf, '\n')
			case 'r':
				buf = append(buf, '\r')
			case 't':
				buf = append(buf, '\t')
			case 'u':
				r, err := p.hex4()
				if err != nil {
					return nil, err
				}
				if utf16.IsSurrogate(r) {
					// A pair joins; a lone surrogate cannot be a Go string and the
					// server never ships one (UNREPRESENTABLE_JSON_STRING).
					if p.i+1 < len(p.data) && p.data[p.i] == '\\' && p.data[p.i+1] == 'u' {
						save := p.i
						p.i += 2
						low, err := p.hex4()
						if err == nil {
							if joined := utf16.DecodeRune(r, low); joined != utf8.RuneError {
								buf = utf8.AppendRune(buf, joined)
								continue
							}
						}
						p.i = save
					}
					return nil, p.fail("lone surrogate")
				}
				buf = utf8.AppendRune(buf, r)
			default:
				return nil, p.fail("bad escape")
			}
		case c < 0x20:
			return nil, p.fail("control character in string")
		default:
			buf = append(buf, c)
			p.i++
		}
	}
	return nil, p.fail("unterminated string")
}

func (p *parser) hex4() (rune, error) {
	if p.i+4 > len(p.data) {
		return 0, p.fail("bad unicode escape")
	}
	n, err := strconv.ParseUint(string(p.data[p.i:p.i+4]), 16, 32)
	if err != nil {
		return 0, p.fail("bad unicode escape")
	}
	p.i += 4
	return rune(n), nil
}

// AppendJSON writes v exactly as JavaScript's JSON.stringify would.
func AppendJSON(b []byte, v any) []byte {
	switch t := v.(type) {
	case nil:
		return append(b, "null"...)
	case bool:
		if t {
			return append(b, "true"...)
		}
		return append(b, "false"...)
	case float64:
		return appendNumber(b, t)
	case int:
		return strconv.AppendInt(b, int64(t), 10)
	case int64:
		return strconv.AppendInt(b, t, 10)
	case string:
		return appendString(b, t)
	case []any:
		b = append(b, '[')
		for i, x := range t {
			if i > 0 {
				b = append(b, ',')
			}
			b = AppendJSON(b, x)
		}
		return append(b, ']')
	case *Obj:
		if t == nil {
			return append(b, "null"...)
		}
		b = append(b, '{')
		for i, k := range t.keys {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendString(b, k)
			b = append(b, ':')
			b = AppendJSON(b, t.vals[k])
		}
		return append(b, '}')
	}
	panic(fmt.Sprintf("sessionview: unsupported JSON value %T", v))
}

// Stringify is JSON.stringify for these values.
func Stringify(v any) string { return string(AppendJSON(nil, v)) }

func appendNumber(b []byte, f float64) []byte {
	if f == 0 {
		return append(b, '0')
	}
	abs := math.Abs(f)
	format := byte('f')
	if abs < 1e-6 || abs >= 1e21 {
		format = 'e'
	}
	b = strconv.AppendFloat(b, f, format, -1, 64)
	if format == 'e' {
		// e-09 -> e-9, as ES Number::toString.
		if n := len(b); n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b
}

const hexDigits = "0123456789abcdef"

func appendString(b []byte, s string) []byte {
	b = append(b, '"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '"':
				b = append(b, '\\', '"')
			case c == '\\':
				b = append(b, '\\', '\\')
			case c == '\b':
				b = append(b, '\\', 'b')
			case c == '\f':
				b = append(b, '\\', 'f')
			case c == '\n':
				b = append(b, '\\', 'n')
			case c == '\r':
				b = append(b, '\\', 'r')
			case c == '\t':
				b = append(b, '\\', 't')
			case c < 0x20:
				b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			default:
				b = append(b, c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b = append(b, `�`...)
		} else {
			b = append(b, s[i:i+size]...)
		}
		i += size
	}
	return append(b, '"')
}

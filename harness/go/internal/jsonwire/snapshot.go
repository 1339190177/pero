package jsonwire

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrNonLosslessJSON identifies JSON that a DSH snapshot would change. This
// opt-in helper never changes native storage or generic transport admission.
var ErrNonLosslessJSON = errors.New("JSON cannot round-trip through a DSH snapshot")

// ValidateDSHJSONSnapshot checks UTF-8 and negative zero without decoding raw
// values to float64. Callers must select their trusted content or Session subtree first.
func ValidateDSHJSONSnapshot(data []byte) error {
	if err := ValidateJSONUTF8(data); err != nil {
		return err
	}
	if !json.Valid(data) {
		return ErrNonLosslessJSON
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return ErrNonLosslessJSON
		}
		number, ok := token.(json.Number)
		if !ok {
			continue
		}
		literal := string(number)
		if !strings.HasPrefix(literal, "-") {
			continue
		}
		mantissa := literal[1:]
		if index := strings.IndexAny(mantissa, "eE"); index >= 0 {
			mantissa = mantissa[:index]
		}
		zero := true
		for _, digit := range mantissa {
			if digit != '0' && digit != '.' {
				zero = false
				break
			}
		}
		if zero {
			return ErrNonLosslessJSON
		}
	}
}

// Freeze encodes caller data once, validates exactly that encoding and returns
// only those bytes. A validator must select its authorized subtrees itself.
func Freeze(value any, validate func([]byte) error) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if err = validate(data); err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

const UnrepresentableJSONStringCode = "UNREPRESENTABLE_JSON_STRING"

// ErrUnrepresentableJSONString is bounded and contains no caller data.
var ErrUnrepresentableJSONString = errors.New("JSON string cannot round-trip without changing its value")

// ValidateJSONUTF8 checks encoded bytes before a JSON decoder can replace invalid
// UTF-8. Escaped UTF-16 surrogates are ASCII here and remain opaque raw JSON.
func ValidateJSONUTF8(data []byte) error {
	if !utf8.Valid(data) {
		return ErrUnrepresentableJSONString
	}
	return nil
}

var jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
var textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
var jsonZeroType = reflect.TypeFor[interface{ IsZero() bool }]()

// ValidateNativeJSONStrings checks only a caller-selected native JSON domain,
// before encoding/json can replace invalid Go strings or map keys. It never
// calls marshaling, text conversion or IsZero methods. JSON marshalers are
// opaque: callers must validate their single actual encoded output afterward.
// Text-only marshalers are unsupported in these selected domains, since their
// returned text would be normalized inside encoding/json before postflight.
// Ordinary byte slices retain their base64 contract. Struct traversal follows
// exported fields, JSON tags, embedding dominance and omitted empty/zero values.
func ValidateNativeJSONStrings(value any) error {
	type visit struct {
		typeOf reflect.Type
		ptr    uintptr
		length int
	}
	seen := map[visit]bool{}
	implements := func(v reflect.Value, t reflect.Type) bool {
		return v.Type().Implements(t) || v.Kind() != reflect.Pointer && v.CanAddr() && v.Addr().Type().Implements(t)
	}
	var walk func(reflect.Value) error
	walk = func(v reflect.Value) error {
		if !v.IsValid() {
			return nil
		}
		if (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) && v.IsNil() {
			return nil
		}
		if implements(v, jsonMarshalerType) {
			return nil
		}
		if implements(v, textMarshalerType) {
			return ErrUnrepresentableJSONString
		}
		// A method-bearing static interface chooses its encoder before any
		// dynamic concrete type; plain interfaces delegate to their value.
		if v.Kind() == reflect.Interface {
			return walk(v.Elem())
		}
		switch v.Kind() {
		case reflect.String:
			if !utf8.ValidString(v.String()) {
				return ErrUnrepresentableJSONString
			}
		case reflect.Pointer, reflect.Map, reflect.Slice:
			if v.IsNil() {
				return nil
			}
			if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 &&
				!reflect.PointerTo(v.Type().Elem()).Implements(jsonMarshalerType) &&
				!reflect.PointerTo(v.Type().Elem()).Implements(textMarshalerType) {
				return nil
			}
			key := visit{typeOf: v.Type(), ptr: uintptr(v.UnsafePointer())}
			if v.Kind() == reflect.Slice {
				key.length = v.Len()
			}
			if seen[key] {
				return nil
			} // json.Marshal retains its own cycle error.
			seen[key] = true
			defer delete(seen, key)
			if v.Kind() == reflect.Pointer {
				return walk(v.Elem())
			}
			if v.Kind() == reflect.Map {
				iter := v.MapRange()
				for iter.Next() {
					key := iter.Key()
					// encoding/json gives string keys precedence over MarshalText,
					// but invokes MarshalText before formatting integer keys.
					if key.Kind() == reflect.String {
						if !utf8.ValidString(key.String()) {
							return ErrUnrepresentableJSONString
						}
					} else if key.Type().Implements(textMarshalerType) && !(key.Kind() == reflect.Pointer && key.IsNil()) {
						return ErrUnrepresentableJSONString
					}
					if err := walk(iter.Value()); err != nil {
						return err
					}
				}
				return nil
			}
			fallthrough
		case reflect.Array:
			for i := 0; i < v.Len(); i++ {
				if err := walk(v.Index(i)); err != nil {
					return err
				}
			}
		case reflect.Struct:
			for _, field := range nativeJSONFields(v.Type()) {
				fv := v
				for _, index := range field.index {
					if fv.Kind() == reflect.Pointer {
						if fv.IsNil() {
							fv = reflect.Value{}
							break
						}
						fv = fv.Elem()
					}
					fv = fv.Field(index)
				}
				if !fv.IsValid() || field.omitEmpty && nativeJSONEmpty(fv) {
					continue
				}
				if field.omitZero {
					// encoding/json omits nil pointers/interfaces without calling
					// even a custom IsZero method on their static type.
					if (fv.Kind() == reflect.Pointer || fv.Kind() == reflect.Interface) && fv.IsNil() {
						continue
					}
					if fv.Kind() == reflect.Interface && fv.Type().Implements(jsonZeroType) && fv.Elem().Kind() == reflect.Pointer && fv.Elem().IsNil() {
						continue
					}
					// Unlike MarshalJSON, omitzero boxes an unaddressable value
					// to call a pointer-receiver IsZero. Check its type directly.
					if fv.Type().Implements(jsonZeroType) || fv.Kind() != reflect.Pointer && reflect.PointerTo(fv.Type()).Implements(jsonZeroType) {
						return ErrUnrepresentableJSONString
					}
					if fv.IsZero() {
						continue
					}
				}
				if err := walk(fv); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(reflect.ValueOf(value))
}

type nativeJSONField struct {
	name                        string
	index                       []int
	tagged, omitEmpty, omitZero bool
}

func nativeJSONFields(root reflect.Type) []nativeJSONField {
	byName := map[string][]nativeJSONField{}
	path := map[reflect.Type]bool{}
	var collect func(reflect.Type, []int)
	collect = func(t reflect.Type, prefix []int) {
		if path[t] {
			return
		}
		path[t] = true
		defer delete(path, t)
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			ft := field.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if !field.IsExported() && (!field.Anonymous || ft.Kind() != reflect.Struct) {
				continue
			}
			rawTag := field.Tag.Get("json")
			if rawTag == "-" {
				continue
			}
			tag := strings.Split(rawTag, ",")
			name := tag[0]
			if !nativeJSONTag(name) {
				name = ""
			}
			index := append(append([]int(nil), prefix...), i)
			if name == "" && field.Anonymous && ft.Kind() == reflect.Struct {
				collect(ft, index)
				continue
			}
			selected := nativeJSONField{name: name, index: index, tagged: name != ""}
			if selected.name == "" {
				selected.name = field.Name
			}
			for _, option := range tag[1:] {
				selected.omitEmpty = selected.omitEmpty || option == "omitempty"
				selected.omitZero = selected.omitZero || option == "omitzero"
			}
			byName[selected.name] = append(byName[selected.name], selected)
		}
	}
	collect(root, nil)
	var fields []nativeJSONField
	for _, candidates := range byName {
		depth := len(candidates[0].index)
		for _, field := range candidates {
			if len(field.index) < depth {
				depth = len(field.index)
			}
		}
		var winner nativeJSONField
		count, tagged := 0, false
		for _, field := range candidates {
			if len(field.index) != depth {
				continue
			}
			if field.tagged && !tagged {
				winner, count, tagged = field, 1, true
				continue
			}
			if field.tagged == tagged {
				winner = field
				count++
			}
		}
		if count == 1 {
			fields = append(fields, winner)
		}
	}
	return fields
}

func nativeJSONTag(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", r) && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func nativeJSONEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Interface, reflect.Pointer:
		return v.IsZero()
	}
	return false
}

package api

import (
	"fmt"
	"reflect"

	"github.com/godbus/dbus/v5"
)

func splitSig(sig string) (string, string, error) {
	if sig == "" {
		return "", "", fmt.Errorf("empty signature")
	}
	switch sig[0] {
	case 'a':
		head, rest, err := splitSig(sig[1:])
		return "a" + head, rest, err
	case '(', '{':
		closer := byte(')')
		if sig[0] == '{' {
			closer = '}'
		}
		i := 1
		for i < len(sig) && sig[i] != closer {
			_, rest, err := splitSig(sig[i:])
			if err != nil {
				return "", "", err
			}
			i = len(sig) - len(rest)
		}
		if i >= len(sig) {
			return "", "", fmt.Errorf("unterminated signature %q", sig)
		}
		return sig[:i+1], sig[i+1:], nil
	}
	return sig[:1], sig[1:], nil
}

func members(sig string) ([]string, error) {
	var out []string
	for sig != "" {
		head, rest, err := splitSig(sig)
		if err != nil {
			return nil, err
		}
		out = append(out, head)
		sig = rest
	}
	return out, nil
}

var basicTypes = map[byte]reflect.Type{
	'y': reflect.TypeOf(byte(0)), 'b': reflect.TypeOf(false), 'n': reflect.TypeOf(int16(0)), 'q': reflect.TypeOf(uint16(0)),
	'i': reflect.TypeOf(int32(0)), 'u': reflect.TypeOf(uint32(0)), 'x': reflect.TypeOf(int64(0)), 't': reflect.TypeOf(uint64(0)),
	'd': reflect.TypeOf(float64(0)), 's': reflect.TypeOf(""), 'o': reflect.TypeOf(dbus.ObjectPath("")),
	'g': reflect.TypeOf(dbus.Signature{}), 'v': reflect.TypeOf(dbus.Variant{}), 'h': reflect.TypeOf(dbus.UnixFDIndex(0)),
}

func typeFor(sig string) (reflect.Type, error) {
	switch sig[0] {
	case 'a':
		if sig[1] == '{' {
			parts, err := members(sig[2 : len(sig)-1])
			if err != nil || len(parts) != 2 {
				return nil, fmt.Errorf("bad dictionary signature %q", sig)
			}
			k, err := typeFor(parts[0])
			if err != nil {
				return nil, err
			}
			v, err := typeFor(parts[1])
			if err != nil {
				return nil, err
			}
			return reflect.MapOf(k, v), nil
		}
		e, err := typeFor(sig[1:])
		if err != nil {
			return nil, err
		}
		return reflect.SliceOf(e), nil
	case '(':
		parts, err := members(sig[1 : len(sig)-1])
		if err != nil {
			return nil, err
		}
		fields := make([]reflect.StructField, len(parts))
		for i, p := range parts {
			t, err := typeFor(p)
			if err != nil {
				return nil, err
			}
			fields[i] = reflect.StructField{Name: fmt.Sprintf("F%d", i), Type: t}
		}
		return reflect.StructOf(fields), nil
	}
	if t, ok := basicTypes[sig[0]]; ok && len(sig) == 1 {
		return t, nil
	}
	return nil, fmt.Errorf("unsupported signature %q", sig)
}

func convert(sig string, in interface{}) (reflect.Value, error) {
	t, err := typeFor(sig)
	if err != nil {
		return reflect.Value{}, err
	}
	switch sig[0] {
	case 'v':
		v, ok := in.(dbus.Variant)
		if !ok {
			return reflect.Value{}, fmt.Errorf("expected a variant")
		}
		inner, err := normalize(v)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(inner), nil
	case '(':
		items, ok := in.([]interface{})
		parts, _ := members(sig[1 : len(sig)-1])
		out := reflect.New(t).Elem()
		if !ok {
			src := reflect.ValueOf(in)
			if src.Kind() != reflect.Struct || src.NumField() != len(parts) {
				return reflect.Value{}, fmt.Errorf("expected a struct for %q", sig)
			}
			items = make([]interface{}, len(parts))
			for i := range parts {
				items[i] = src.Field(i).Interface()
			}
		}
		if len(items) != len(parts) {
			return reflect.Value{}, fmt.Errorf("wrong struct size for %q", sig)
		}
		for i, p := range parts {
			f, err := convert(p, items[i])
			if err != nil {
				return reflect.Value{}, err
			}
			out.Field(i).Set(f)
		}
		return out, nil
	case 'a':
		src := reflect.ValueOf(in)
		if sig[1] == '{' {
			parts, _ := members(sig[2 : len(sig)-1])
			if src.Kind() != reflect.Map {
				return reflect.Value{}, fmt.Errorf("expected a map for %q", sig)
			}
			out := reflect.MakeMapWithSize(t, src.Len())
			iter := src.MapRange()
			for iter.Next() {
				k, err := convert(parts[0], iter.Key().Interface())
				if err != nil {
					return reflect.Value{}, err
				}
				v, err := convert(parts[1], iter.Value().Interface())
				if err != nil {
					return reflect.Value{}, err
				}
				out.SetMapIndex(k, v)
			}
			return out, nil
		}
		if src.Kind() != reflect.Slice {
			return reflect.Value{}, fmt.Errorf("expected an array for %q", sig)
		}
		out := reflect.MakeSlice(t, src.Len(), src.Len())
		for i := 0; i < src.Len(); i++ {
			e, err := convert(sig[1:], src.Index(i).Interface())
			if err != nil {
				return reflect.Value{}, err
			}
			out.Index(i).Set(e)
		}
		return out, nil
	}
	v := reflect.ValueOf(in)
	if !v.IsValid() || !v.Type().ConvertibleTo(t) {
		return reflect.Value{}, fmt.Errorf("expected %s for %q", t, sig)
	}
	return v.Convert(t), nil
}

func normalize(v dbus.Variant) (dbus.Variant, error) {
	sig := v.Signature().String()
	if sig == "" {
		return v, fmt.Errorf("empty variant")
	}
	out, err := convert(sig, v.Value())
	if err != nil {
		return dbus.Variant{}, err
	}
	return dbus.MakeVariantWithSignature(out.Interface(), v.Signature()), nil
}

package luadata

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Decode copies a Value into a Go value, the way encoding/json does for JSON.
// Struct fields are matched by the `lua:"name"` tag; fields without a tag and
// table keys without a field are ignored. Tag options:
//
//	lua:",list"     the field takes the table's positional entries
//	lua:"name,keys" a { a = true, b = true } set becomes []string{"a", "b"}
//
// Strings holding whole numbers decode into integer fields (tree node links
// are stored as "35362").
func Decode(v Value, out any) error {
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("luadata: Decode needs a non-nil pointer")
	}
	return decode(v, rv.Elem(), "")
}

func pathErr(path, format string, a ...any) error {
	if path == "" {
		path = "(root)"
	}
	return fmt.Errorf("luadata: %s: %s", path, fmt.Sprintf(format, a...))
}

func join(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}

func decode(v Value, dst reflect.Value, path string) error {
	if v == nil {
		return nil // leave the zero value
	}
	switch dst.Kind() {
	case reflect.Pointer:
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		return decode(v, dst.Elem(), path)
	case reflect.Interface:
		dst.Set(reflect.ValueOf(v))
		return nil
	case reflect.String:
		switch x := v.(type) {
		case String:
			dst.SetString(string(x))
		case Number:
			dst.SetString(strconv.FormatFloat(float64(x), 'f', -1, 64))
		default:
			return pathErr(path, "want string, have %s", typeName(v))
		}
		return nil
	case reflect.Bool:
		b, ok := v.(Bool)
		if !ok {
			return pathErr(path, "want boolean, have %s", typeName(v))
		}
		dst.SetBool(bool(b))
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := wholeNumber(v, path)
		if err != nil {
			return err
		}
		if dst.CanInt() {
			dst.SetInt(n)
		} else {
			if n < 0 {
				return pathErr(path, "negative number %d for unsigned field", n)
			}
			dst.SetUint(uint64(n))
		}
		return nil
	case reflect.Float32, reflect.Float64:
		n, ok := v.(Number)
		if !ok {
			return pathErr(path, "want number, have %s", typeName(v))
		}
		dst.SetFloat(float64(n))
		return nil
	}

	t, ok := v.(*Table)
	if !ok {
		return pathErr(path, "want table, have %s", typeName(v))
	}
	switch dst.Kind() {
	case reflect.Slice:
		// Lua lists may be written with explicit keys: { [1] = a, [2] = b }.
		list := t.List
		for n := len(list) + 1; t.Has(Number(n)); n++ {
			list = append(list[:len(list):len(list)], t.Get(Number(n)))
		}
		s := reflect.MakeSlice(dst.Type(), len(list), len(list))
		for i, item := range list {
			if err := decode(item, s.Index(i), fmt.Sprintf("%s[%d]", path, i+1)); err != nil {
				return err
			}
		}
		dst.Set(s)
		return nil
	case reflect.Map:
		m := reflect.MakeMapWithSize(dst.Type(), len(t.Keys))
		for _, k := range t.Keys {
			kv := reflect.New(dst.Type().Key()).Elem()
			if err := decode(k, kv, path+"[key]"); err != nil {
				return err
			}
			ev := reflect.New(dst.Type().Elem()).Elem()
			if err := decode(t.Get(k), ev, join(path, keyString(k))); err != nil {
				return err
			}
			m.SetMapIndex(kv, ev)
		}
		dst.Set(m)
		return nil
	case reflect.Struct:
		return decodeStruct(t, dst, path)
	}
	return pathErr(path, "cannot decode into %s", dst.Type())
}

func keyString(k Value) string {
	if s, ok := toStr(k); ok {
		return s
	}
	return fmt.Sprint(k)
}

func wholeNumber(v Value, path string) (int64, error) {
	var f float64
	switch x := v.(type) {
	case Number:
		f = float64(x)
	case String:
		n, err := strconv.ParseInt(strings.TrimSpace(string(x)), 10, 64)
		if err != nil {
			return 0, pathErr(path, "want integer, have string %q", string(x))
		}
		return n, nil
	default:
		return 0, pathErr(path, "want integer, have %s", typeName(v))
	}
	if f != float64(int64(f)) {
		return 0, pathErr(path, "want integer, have %v", f)
	}
	return int64(f), nil
}

func decodeStruct(t *Table, dst reflect.Value, path string) error {
	typ := dst.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag, ok := f.Tag.Lookup("lua")
		if !ok || !f.IsExported() {
			continue
		}
		name, opt, _ := strings.Cut(tag, ",")
		field := dst.Field(i)
		switch opt {
		case "list":
			if err := decode(&Table{List: t.List}, field, join(path, f.Name)); err != nil {
				return err
			}
		case "keys":
			set, ok := t.Get(name).(*Table)
			if !ok {
				if t.Get(name) != nil {
					return pathErr(join(path, name), "want table, have %s", typeName(t.Get(name)))
				}
				continue
			}
			var keys []string
			for _, k := range set.Keys {
				if b, _ := set.Get(k).(Bool); b {
					keys = append(keys, keyString(k))
				}
			}
			sort.Strings(keys)
			field.Set(reflect.ValueOf(keys))
		default:
			if err := decode(t.Get(name), field, join(path, name)); err != nil {
				return err
			}
		}
	}
	return nil
}

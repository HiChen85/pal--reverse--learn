package palgame

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Saves deliberately use a closed schema. encoding/json alone accepts duplicate
// keys, case-insensitive field aliases, null scalars and short fixed arrays.
func readSaveJSON(data []byte, target any) (map[string]any, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("save JSON must be valid UTF-8")
	}
	if len(data) == 0 || len(data) > MaxSaveBytes {
		return nil, fmt.Errorf("save JSON exceeds the 1..%d byte limit", MaxSaveBytes)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	nodes := 0
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		nodes++
		if depth > 64 || nodes > 1_000_000 {
			return nil, fmt.Errorf("save JSON nesting or node limit exceeded")
		}
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		if t == nil {
			return nil, fmt.Errorf("null is not allowed in a session save")
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				out := map[string]any{}
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return nil, err
					}
					name, ok := key.(string)
					if !ok {
						return nil, fmt.Errorf("invalid object key")
					}
					if _, exists := out[name]; exists {
						return nil, fmt.Errorf("duplicate save field %q", name)
					}
					value, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					out[name] = value
				}
				if end, err := d.Token(); err != nil || end != json.Delim('}') {
					return nil, fmt.Errorf("incomplete save object")
				}
				return out, nil
			case '[':
				out := []any{}
				for d.More() {
					value, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					out = append(out, value)
				}
				if end, err := d.Token(); err != nil || end != json.Delim(']') {
					return nil, fmt.Errorf("incomplete save array")
				}
				return out, nil
			}
			return nil, fmt.Errorf("unexpected JSON delimiter")
		}
		return t, nil
	}
	v, err := read(0)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("save must contain exactly one JSON object")
	}
	if err := saveJSONShape(v, reflect.TypeOf(target).Elem(), "save"); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(canonical, target); err != nil {
		return nil, err
	}
	return v.(map[string]any), nil
}

func saveField(f reflect.StructField) (string, bool) {
	parts := strings.Split(f.Tag.Get("json"), ",")
	name := parts[0]
	if name == "" {
		name = f.Name
	}
	return name, len(parts) > 1 && parts[1] == "omitempty"
}

// Combat PlayerState embeds Player. Its exported fields are promoted in JSON;
// use the same flattened field paths for shape validation and nil metadata.
func saveStructFields(typ reflect.Type) []reflect.StructField {
	fields := []reflect.StructField{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous && field.Tag.Get("json") == "" && field.Type.Kind() == reflect.Struct {
			for _, child := range saveStructFields(field.Type) {
				child.Index = append([]int{i}, child.Index...)
				fields = append(fields, child)
			}
		} else {
			fields = append(fields, field)
		}
	}
	return fields
}

func saveJSONShape(v any, typ reflect.Type, path string) error {
	if typ.Kind() == reflect.Pointer {
		return saveJSONShape(v, typ.Elem(), path)
	}
	switch typ.Kind() {
	case reflect.Struct:
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		fields := map[string]reflect.StructField{}
		for _, f := range saveStructFields(typ) {
			name, optional := saveField(f)
			fields[name] = f
			if _, ok := obj[name]; !ok && !optional {
				return fmt.Errorf("%s.%s is required", path, name)
			}
		}
		for name, value := range obj {
			f, ok := fields[name]
			if !ok {
				return fmt.Errorf("unknown save field %s.%s", path, name)
			}
			if err := saveJSONShape(value, f.Type, path+"."+name); err != nil {
				return err
			}
		}
	case reflect.Map:
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		for key, value := range obj {
			if typ.Key().Kind() != reflect.String {
				n, err := strconv.ParseUint(key, 10, typ.Key().Bits())
				if err != nil || strconv.FormatUint(n, 10) != key {
					return fmt.Errorf("%s has a noncanonical map key %q", path, key)
				}
			}
			if err := saveJSONShape(value, typ.Elem(), path+"/"+key); err != nil {
				return err
			}
		}
	case reflect.Array, reflect.Slice:
		list, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if typ.Kind() == reflect.Array && len(list) != typ.Len() {
			return fmt.Errorf("%s requires exactly %d entries", path, typ.Len())
		}
		for i, value := range list {
			if err := saveJSONShape(value, typ.Elem(), fmt.Sprintf("%s/%d", path, i)); err != nil {
				return err
			}
		}
	case reflect.Bool:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s must be boolean", path)
		}
	case reflect.String:
		if _, ok := v.(string); !ok {
			return fmt.Errorf("%s must be string", path)
		}
	default:
		if _, ok := v.(json.Number); !ok {
			return fmt.Errorf("%s must be numeric", path)
		}
	}
	return nil
}

// Nil collections have explicit paths instead of JSON null. This preserves the
// detached Go snapshot exactly, including nil versus empty slices in traces.
func saveJSONValue(v reflect.Value, path string, nils *[]string) any {
	if v.Kind() == reflect.Pointer {
		return saveJSONValue(v.Elem(), path, nils)
	}
	switch v.Kind() {
	case reflect.Struct:
		out := map[string]any{}
		for _, f := range saveStructFields(v.Type()) {
			name, optional := saveField(f)
			field := v.FieldByIndex(f.Index)
			if optional && field.IsZero() {
				continue
			}
			out[name] = saveJSONValue(field, path+"/"+name, nils)
		}
		return out
	case reflect.Map:
		out := map[string]any{}
		if v.IsNil() {
			*nils = append(*nils, path)
		}
		iter := v.MapRange()
		for iter.Next() {
			key := fmt.Sprint(iter.Key().Interface())
			out[key] = saveJSONValue(iter.Value(), path+"/"+key, nils)
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, v.Len())
		if v.Kind() == reflect.Slice && v.IsNil() {
			*nils = append(*nils, path)
		}
		for i := 0; i < v.Len(); i++ {
			out[i] = saveJSONValue(v.Index(i), fmt.Sprintf("%s/%d", path, i), nils)
		}
		return out
	default:
		return v.Interface()
	}
}

func restoreSaveNils(v reflect.Value, path string, wanted map[string]bool) error {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return restoreSaveNils(v.Elem(), path, wanted)
	}
	if wanted[path] {
		if (v.Kind() != reflect.Map && v.Kind() != reflect.Slice) || v.Len() != 0 || !v.CanSet() {
			return fmt.Errorf("invalid nil collection path %q", path)
		}
		v.SetZero()
		delete(wanted, path)
		return nil
	}
	switch v.Kind() {
	case reflect.Struct:
		for _, f := range saveStructFields(v.Type()) {
			name, _ := saveField(f)
			if err := restoreSaveNils(v.FieldByIndex(f.Index), path+"/"+name, wanted); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := restoreSaveNils(v.Index(i), fmt.Sprintf("%s/%d", path, i), wanted); err != nil {
				return err
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			value := reflect.New(v.Type().Elem()).Elem()
			value.Set(iter.Value())
			if err := restoreSaveNils(value, path+"/"+fmt.Sprint(iter.Key().Interface()), wanted); err != nil {
				return err
			}
			v.SetMapIndex(iter.Key(), value)
		}
	}
	return nil
}

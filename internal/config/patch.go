package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"slices"
)

// Patch applies a JSON merge patch (RFC 7396) to c. Every key must name an
// existing setting, null resets a setting to its default, and schema and
// revision cannot be patched. The result is not validated.
func Patch(c Config, patch json.RawMessage) (Config, error) {
	var p map[string]any
	if err := json.Unmarshal(patch, &p); err != nil || p == nil {
		return Config{}, &ValidationError{Fields: []FieldError{{Message: "the patch must be a JSON object"}}}
	}
	v := &validator{}
	for _, key := range []string{"schema", "revision"} {
		if _, ok := p[key]; ok {
			v.add(key, "cannot be changed")
			delete(p, key)
		}
	}
	cur, def := toMap(c), toMap(Defaults())
	merge(cur, def, p, "", v)
	if err := v.err(); err != nil {
		return Config{}, err
	}
	raw, err := json.Marshal(cur)
	if err != nil {
		return Config{}, err
	}
	var out Config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) {
			return Config{}, &ValidationError{Fields: []FieldError{{Field: te.Field, Message: "must be " + typeName(te.Type)}}}
		}
		return Config{}, err
	}
	return out, nil
}

// toMap converts c to its generic JSON form, which shares nothing with c.
func toMap(c Config) map[string]any {
	raw, _ := json.Marshal(c)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

func merge(dst, def, patch map[string]any, prefix string, v *validator) {
	for _, key := range slices.Sorted(maps.Keys(patch)) {
		field := key
		if prefix != "" {
			field = prefix + "." + key
		}
		cur, ok := dst[key]
		if !ok {
			v.add(field, "is not a setting")
			continue
		}
		val := patch[key]
		curObj, curIsObj := cur.(map[string]any)
		valObj, valIsObj := val.(map[string]any)
		switch {
		case val == nil:
			dst[key] = def[key]
		case curIsObj && valIsObj:
			merge(curObj, def[key].(map[string]any), valObj, field, v)
		case curIsObj:
			v.add(field, "must be an object")
		default:
			dst[key] = val
		}
	}
}

func typeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "true or false"
	case reflect.Int:
		return "a whole number"
	case reflect.String:
		return "a string"
	case reflect.Slice:
		return "a list"
	default:
		return "a " + t.Kind().String()
	}
}

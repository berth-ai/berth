package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
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

// userMaps are settings whose keys the user chooses. A patch may add keys
// there and null removes one. A new entry starts from a copy of the
// template; nil means entries are plain values (lists, strings), replaced
// whole. A * segment matches any key.
var userMaps = map[string]map[string]any{
	"mcp.servers":                            {"url": "", "auth": AuthNone, "enabled": true},
	"mcp.available":                          nil,
	"mcp.workspaces":                         nil,
	"skills.sources":                         {"repo": "", "ref": "", "path": "", "commit": "", "found": map[string]any{}, "attached": map[string]any{}},
	"skills.sources.*.found":                 {"path": "", "description": "", "error": ""},
	"skills.sources.*.attached":              {"enabled": true, "agents": nil, "workspaces": map[string]any{}},
	"skills.sources.*.attached.*.workspaces": nil,
	"workspaces":                             {"path": "", "repo": "", "env": map[string]any{}, "instructions": map[string]any{"shared": false}},
	"workspaces.*.env":                       {"value": "", "secret": false},
}

// userMapAt returns the user map template at field. Patterns never
// overlap, so at most one matches.
func userMapAt(field string) (map[string]any, bool) {
	segs := strings.Split(field, ".")
	for pattern, tmpl := range userMaps {
		if slices.EqualFunc(strings.Split(pattern, "."), segs, func(p, s string) bool { return p == "*" || p == s }) {
			return tmpl, true
		}
	}
	return nil, false
}

// deepCopy copies JSON objects and lists, so a config never shares them
// with a template or the defaults.
func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = deepCopy(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = deepCopy(x)
		}
		return out
	}
	return v
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
			dst[key] = deepCopy(def[key])
		case curIsObj && valIsObj:
			if tmpl, ok := userMapAt(field); ok {
				mergeUserMap(curObj, tmpl, valObj, field, v)
			} else {
				merge(curObj, def[key].(map[string]any), valObj, field, v)
			}
		case curIsObj:
			v.add(field, "must be an object")
		default:
			dst[key] = val
		}
	}
}

func mergeUserMap(dst, tmpl, patch map[string]any, prefix string, v *validator) {
	for _, key := range slices.Sorted(maps.Keys(patch)) {
		field, val := prefix+"."+key, patch[key]
		switch obj, isObj := val.(map[string]any); {
		case val == nil:
			delete(dst, key)
		case tmpl == nil:
			dst[key] = val // decoding checks the type
		case !isObj:
			v.add(field, "must be an object")
		default:
			cur, ok := dst[key].(map[string]any)
			if !ok {
				cur = deepCopy(tmpl).(map[string]any)
				dst[key] = cur
			}
			merge(cur, tmpl, obj, field, v)
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

package mcpsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// errNotPlainJSON marks a config file Berth will not rewrite: comments,
// trailing commas or anything else that is not one plain JSON object.
var errNotPlainJSON = errors.New("not plain JSON")

// entry is one server entry to write under the edited key.
type entry struct {
	id  string
	val json.RawMessage
}

type member struct {
	key string
	val json.RawMessage
}

// parseObject splits a JSON object into its members, in file order. Empty
// input is an empty object.
func parseObject(data []byte) ([]member, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	if !json.Valid(data) {
		return nil, errNotPlainJSON
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errNotPlainJSON
	}
	var out []member
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, errNotPlainJSON
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, errNotPlainJSON
		}
		out = append(out, member{key: tok.(string), val: val})
	}
	return out, nil
}

// marshal encodes v without HTML escaping, so "<" stays "<".
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// formatObject writes members as a two-space indented object and a newline.
// Member values are copied byte for byte, then re-indented.
func formatObject(members []member) ([]byte, error) {
	var raw bytes.Buffer
	raw.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			raw.WriteByte(',')
		}
		k, err := marshal(m.key)
		if err != nil {
			return nil, err
		}
		raw.Write(k)
		raw.WriteByte(':')
		raw.Write(m.val)
	}
	raw.WriteByte('}')
	var out bytes.Buffer
	if err := json.Indent(&out, raw.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func sameJSON(a, b []byte) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return false
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

// inner returns the members of the object at key, its index in top (-1 when
// absent), and an error when the value is not an object.
func inner(top []member, key string) ([]member, int, error) {
	i := slices.IndexFunc(top, func(m member) bool { return m.key == key })
	if i < 0 {
		return nil, -1, nil
	}
	members, err := parseObject(top[i].val)
	if err != nil {
		return nil, i, fmt.Errorf("%s is not an object", key)
	}
	return members, i, nil
}

// objectKeys lists the member names of the object at key; nil when absent.
func objectKeys(data []byte, key string) ([]string, error) {
	top, err := parseObject(data)
	if err != nil {
		return nil, err
	}
	members, _, err := inner(top, key)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, m := range members {
		names = append(names, m.key)
	}
	return names, nil
}

// editJSON sets entries in the object at key and deletes the ids in del,
// keeping the order and content of every other member. It returns data
// itself when nothing changes.
func editJSON(data []byte, key string, set []entry, del []string) ([]byte, error) {
	top, err := parseObject(data)
	if err != nil {
		return nil, err
	}
	members, at, err := inner(top, key)
	if err != nil {
		return nil, err
	}
	changed := false
	for _, id := range del {
		if j := slices.IndexFunc(members, func(m member) bool { return m.key == id }); j >= 0 {
			members = slices.Delete(members, j, j+1)
			changed = true
		}
	}
	for _, e := range set {
		j := slices.IndexFunc(members, func(m member) bool { return m.key == e.id })
		switch {
		case j < 0:
			members = append(members, member{key: e.id, val: e.val})
			changed = true
		case !sameJSON(members[j].val, e.val):
			members[j].val = e.val
			changed = true
		}
	}
	if !changed {
		return data, nil
	}
	val, err := formatObject(members)
	if err != nil {
		return nil, err
	}
	if at >= 0 {
		top[at].val = val
	} else {
		top = append(top, member{key: key, val: val})
	}
	return formatObject(top)
}

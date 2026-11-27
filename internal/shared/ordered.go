package shared

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type OrderedMap struct {
	keys []string
	vals map[string]any
}

func NewOrderedMap() *OrderedMap { return &OrderedMap{vals: map[string]any{}} }

func (m *OrderedMap) Keys() []string { return append([]string{}, m.keys...) }

func (m *OrderedMap) Get(k string) (any, bool) {
	v, ok := m.vals[k]
	return v, ok
}

func (m *OrderedMap) Set(k string, v any) {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

func (m *OrderedMap) Delete(k string) {
	if _, ok := m.vals[k]; !ok {
		return
	}
	delete(m.vals, k)
	for i, x := range m.keys {
		if x == k {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}

func (m *OrderedMap) Len() int { return len(m.keys) }

func (m *OrderedMap) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeOrdered(dec)
	if err != nil {
		return err
	}
	om, ok := v.(*OrderedMap)
	if !ok {
		return fmt.Errorf("not a JSON object")
	}
	*m = *om
	return nil
}

func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			om := NewOrderedMap()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := kt.(string)
				val, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				om.Set(key, val)
			}
			_, err := dec.Token()
			return om, err
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			_, err := dec.Token()
			return arr, err
		}
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f, nil
		}
		return t.String(), nil
	}
	return tok, nil
}

func (m *OrderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := MarshalNoEscape(k)
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := MarshalNoEscape(m.vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func MarshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func (m *OrderedMap) ToPlain() map[string]any {
	out := make(map[string]any, len(m.keys))
	for _, k := range m.keys {
		out[k] = toPlain(m.vals[k])
	}
	return out
}

func toPlain(v any) any {
	switch t := v.(type) {
	case *OrderedMap:
		return t.ToPlain()
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = toPlain(x)
		}
		return out
	}
	return v
}

func MarshalPretty(v any, indent string) []byte {
	raw, err := MarshalNoEscape(v)
	if err != nil {
		return []byte("{}\n")
	}
	var out bytes.Buffer
	if json.Indent(&out, raw, "", indent) != nil {
		return append(raw, '\n')
	}
	s := out.String()
	return []byte(strings.TrimRight(s, "\n") + "\n")
}

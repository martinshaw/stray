package jsonc

import (
	"bytes"
	"encoding/json"
)

// KV is one object entry in source order.
type KV struct {
	Key string
	Val json.RawMessage
}

// Object decodes a JSON object without sorting its keys.
// The boolean is false when raw is not an object.
func Object(raw json.RawMessage) ([]KV, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil, false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, true, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, false, nil
	}
	var out []KV
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, true, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, true, errUnexpected(keyTok)
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, true, err
		}
		out = append(out, KV{Key: key, Val: val})
	}
	return out, true, nil
}

type unexpected string

func errUnexpected(v any) error {
	return unexpected("unexpected json token")
}
func (e unexpected) Error() string { return string(e) }

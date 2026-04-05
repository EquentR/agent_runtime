package jsonutil

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func MarshalRawMessage(value any, objectDefault bool) (json.RawMessage, error) {
	switch v := value.(type) {
	case nil:
		return defaultRawMessage(objectDefault), nil
	case json.RawMessage:
		return NormalizeRawMessage(v, objectDefault)
	case []byte:
		return NormalizeRawMessage(v, objectDefault)
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(raw), nil
	}
}

func NormalizeRawMessage(raw []byte, objectDefault bool) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return defaultRawMessage(objectDefault), nil
	}
	if !json.Valid(trimmed) {
		return nil, fmt.Errorf("invalid json")
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, trimmed); err != nil {
		return nil, err
	}
	return json.RawMessage(compacted.Bytes()), nil
}

func defaultRawMessage(objectDefault bool) json.RawMessage {
	if objectDefault {
		return json.RawMessage("{}")
	}
	return json.RawMessage("null")
}

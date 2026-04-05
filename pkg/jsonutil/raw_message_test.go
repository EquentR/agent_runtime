package jsonutil

import (
	"encoding/json"
	"testing"
)

func TestNormalizeRawMessageHandlesNilRawAndCompactJSON(t *testing.T) {
	t.Run("defaults nil raw to object when requested", func(t *testing.T) {
		normalized, err := NormalizeRawMessage(nil, true)
		if err != nil {
			t.Fatalf("NormalizeRawMessage(nil, true) error = %v", err)
		}
		if string(normalized) != "{}" {
			t.Fatalf("NormalizeRawMessage(nil, true) = %s, want {}", string(normalized))
		}
	})

	t.Run("defaults nil raw to null when object default disabled", func(t *testing.T) {
		normalized, err := NormalizeRawMessage(nil, false)
		if err != nil {
			t.Fatalf("NormalizeRawMessage(nil, false) error = %v", err)
		}
		if string(normalized) != "null" {
			t.Fatalf("NormalizeRawMessage(nil, false) = %s, want null", string(normalized))
		}
	})

	t.Run("compacts valid raw json", func(t *testing.T) {
		normalized, err := NormalizeRawMessage([]byte("{\n  \"status\" : \"running\"\n}"), true)
		if err != nil {
			t.Fatalf("NormalizeRawMessage(compact) error = %v", err)
		}
		if string(normalized) != `{"status":"running"}` {
			t.Fatalf("NormalizeRawMessage(compact) = %s, want compact JSON", string(normalized))
		}
	})

	t.Run("keeps explicit null raw bytes", func(t *testing.T) {
		normalized, err := NormalizeRawMessage([]byte(" null \n"), true)
		if err != nil {
			t.Fatalf("NormalizeRawMessage(null) error = %v", err)
		}
		if string(normalized) != "null" {
			t.Fatalf("NormalizeRawMessage(null) = %s, want null", string(normalized))
		}
	})
}

func TestMarshalRawMessageRejectsInvalidJSONBytes(t *testing.T) {
	t.Run("rejects invalid byte slice", func(t *testing.T) {
		_, err := MarshalRawMessage([]byte(`{"status":`), true)
		if err == nil {
			t.Fatal("MarshalRawMessage([]byte) error = nil, want invalid json")
		}
	})

	t.Run("rejects invalid raw message", func(t *testing.T) {
		_, err := MarshalRawMessage(json.RawMessage(`{"status":`), false)
		if err == nil {
			t.Fatal("MarshalRawMessage(json.RawMessage) error = nil, want invalid json")
		}
	})
}

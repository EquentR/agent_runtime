package openai_responses

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
)

func TestResponseStreamSkipsSSEKeepAliveBlocks(t *testing.T) {
	sse := strings.Join([]string{
		`: OPENROUTER PROCESSING`,
		``,
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		``,
		`: OPENROUTER PROCESSING`,
		``,
		`event: response.output_item.added`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup_weather","arguments":""}}`,
		``,
		`: OPENROUTER PROCESSING`,
		``,
		`event: response.function_call_arguments.delta`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{\"city\":"}`,
		``,
		`event: response.function_call_arguments.delta`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"\"Beijing\"}"}`,
		``,
		`event: response.function_call_arguments.done`,
		`data: {"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":0,"name":"lookup_weather","arguments":"{\"city\":\"Beijing\"}"}`,
		``,
	}, "\n") + "\n"

	raw := &http.Response{
		Header: http.Header{},
		Body:   io.NopCloser(strings.NewReader(sse)),
	}
	stream := ssestream.NewStream[responses.ResponseStreamEventUnion](ssestream.NewDecoder(raw), nil)

	var sawArgumentsDone bool
	for stream.Next() {
		event := stream.Current()
		if event.Type == "response.function_call_arguments.done" {
			sawArgumentsDone = true
			if event.Arguments != `{"city":"Beijing"}` {
				t.Fatalf("arguments = %q, want %q", event.Arguments, `{"city":"Beijing"}`)
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v, want nil", err)
	}
	if !sawArgumentsDone {
		t.Fatal("response.function_call_arguments.done was not received")
	}
}

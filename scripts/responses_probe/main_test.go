package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSummarizeResponseRedactsSecretsAndCapturesFunctionCalls(t *testing.T) {
	raw := []byte(`{
		"id":"resp_123",
		"status":"completed",
		"output":[
			{"type":"reasoning","id":"rs_1"},
			{"type":"function_call","id":"fc_1","call_id":"call_1","name":"echo_payload","arguments":"{\"value\":\"alpha\",\"step\":1}"},
			{"type":"message","id":"msg_1","content":[{"type":"output_text","text":"done"}]}
		],
		"usage":{"input_tokens":7,"output_tokens":5,"total_tokens":12}
	}`)

	summary := summarizeResponse(probeCase{
		ID:      "P4-A",
		Request: map[string]any{"authorization": "Bearer sk-secret", "model": "gpt-5.4"},
	}, 200, map[string][]string{"X-Request-Id": {"req_1"}}, raw)

	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("json.Marshal(summary) error = %v", err)
	}
	text := string(encoded)
	if strings.Contains(text, "sk-secret") {
		t.Fatalf("summary leaked API key: %s", text)
	}
	if summary.ResponseID != "resp_123" {
		t.Fatalf("ResponseID = %q, want resp_123", summary.ResponseID)
	}
	if summary.HTTPStatus != 200 {
		t.Fatalf("HTTPStatus = %d, want 200", summary.HTTPStatus)
	}
	if got := strings.Join(summary.OutputTypes, ","); got != "reasoning,function_call,message" {
		t.Fatalf("OutputTypes = %q, want reasoning,function_call,message", got)
	}
	if len(summary.FunctionCalls) != 1 {
		t.Fatalf("FunctionCalls length = %d, want 1", len(summary.FunctionCalls))
	}
	call := summary.FunctionCalls[0]
	if call.CallID != "call_1" || call.Name != "echo_payload" || !call.ArgumentsJSONValid {
		t.Fatalf("FunctionCall = %#v, want call_1 echo_payload valid JSON", call)
	}
	if summary.Usage.TotalTokens != 12 {
		t.Fatalf("Usage.TotalTokens = %d, want 12", summary.Usage.TotalTokens)
	}
}

func TestSummarizeResponseCapturesErrorPayload(t *testing.T) {
	raw := []byte(`{"error":{"type":"invalid_request_error","message":"Unknown parameter: temperature"}}`)

	summary := summarizeResponse(probeCase{ID: "P3-E"}, 400, nil, raw)

	if summary.Error == nil {
		t.Fatal("Error = nil, want captured error")
	}
	if summary.Error.Type != "invalid_request_error" {
		t.Fatalf("Error.Type = %q, want invalid_request_error", summary.Error.Type)
	}
	if !strings.Contains(summary.Error.Message, "temperature") {
		t.Fatalf("Error.Message = %q, want temperature mention", summary.Error.Message)
	}
}

func TestSummarizeResponseCapturesStreamingEvents(t *testing.T) {
	raw := []byte(strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"O"}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"K"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"id":"resp_stream","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
		``,
	}, "\n"))

	summary := summarizeResponse(probeCase{ID: "P1-C"}, 200, nil, raw)

	if !summary.Stream {
		t.Fatal("Stream = false, want true for SSE payload")
	}
	if got := strings.Join(summary.StreamEventTypes, ","); got != "response.output_text.delta,response.output_text.delta,response.completed" {
		t.Fatalf("StreamEventTypes = %q, want output deltas and completed", got)
	}
	if summary.ResponseID != "resp_stream" {
		t.Fatalf("ResponseID = %q, want resp_stream", summary.ResponseID)
	}
	if summary.Usage.TotalTokens != 3 {
		t.Fatalf("Usage.TotalTokens = %d, want 3", summary.Usage.TotalTokens)
	}
}

func TestBuildOutputPathUsesProbeIDAndTimestamp(t *testing.T) {
	got := buildOutputPath("tmp/responses-api-probes", "P4-A", "20260524T010203Z")
	wantSuffix := "tmp/responses-api-probes/P4-A-20260524T010203Z.json"
	if strings.ReplaceAll(got, "\\", "/") != wantSuffix {
		t.Fatalf("buildOutputPath() = %q, want %q", got, wantSuffix)
	}
}

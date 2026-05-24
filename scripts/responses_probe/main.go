package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type probeCase struct {
	ID      string         `json:"id"`
	Request map[string]any `json:"request"`
}

type probeSummary struct {
	ProbeID          string              `json:"probe_id"`
	HTTPStatus       int                 `json:"http_status"`
	RequestID        string              `json:"request_id,omitempty"`
	ResponseID       string              `json:"response_id,omitempty"`
	Status           string              `json:"status,omitempty"`
	OutputTypes      []string            `json:"output_types,omitempty"`
	FunctionCalls    []functionCallBrief `json:"function_calls,omitempty"`
	Usage            usageBrief          `json:"usage,omitempty"`
	Error            *errorBrief         `json:"error,omitempty"`
	RawJSONValid     bool                `json:"raw_json_valid"`
	Stream           bool                `json:"stream,omitempty"`
	StreamEventTypes []string            `json:"stream_event_types,omitempty"`
}

type functionCallBrief struct {
	ID                 string `json:"id,omitempty"`
	CallID             string `json:"call_id,omitempty"`
	Name               string `json:"name,omitempty"`
	ArgumentsJSONValid bool   `json:"arguments_json_valid"`
}

type usageBrief struct {
	InputTokens  int64 `json:"input_tokens,omitempty"`
	OutputTokens int64 `json:"output_tokens,omitempty"`
	TotalTokens  int64 `json:"total_tokens,omitempty"`
}

type errorBrief struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

func main() {
	var (
		casePath = flag.String("case", "", "path to a probe case JSON file")
		outDir   = flag.String("out", "tmp/responses-api-probes", "directory for redacted summaries")
		timeout  = flag.Duration("timeout", 90*time.Second, "HTTP request timeout")
	)
	flag.Parse()

	if err := run(context.Background(), *casePath, *outDir, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, casePath, outDir string, timeout time.Duration) error {
	if strings.TrimSpace(casePath) == "" {
		return errors.New("-case is required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RESPONSES_TEST_BASE_URL")), "/")
	apiKey := strings.TrimSpace(os.Getenv("RESPONSES_TEST_API_KEY"))
	if baseURL == "" {
		return errors.New("RESPONSES_TEST_BASE_URL is required")
	}
	if apiKey == "" {
		return errors.New("RESPONSES_TEST_API_KEY is required")
	}

	probe, err := readProbeCase(casePath)
	if err != nil {
		return err
	}
	rawRequest, err := json.Marshal(probe.Request)
	if err != nil {
		return fmt.Errorf("marshal probe request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/responses", bytes.NewReader(rawRequest))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send probe %s: %w", probe.ID, err)
	}
	defer resp.Body.Close()

	rawResponse, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read probe response: %w", err)
	}
	summary := summarizeResponse(probe, resp.StatusCode, resp.Header, rawResponse)
	if err := writeSummary(outDir, probe.ID, summary); err != nil {
		return err
	}
	encoded, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(encoded))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("probe %s returned HTTP %d", probe.ID, resp.StatusCode)
	}
	return nil
}

func readProbeCase(path string) (probeCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return probeCase{}, fmt.Errorf("read probe case: %w", err)
	}
	var probe probeCase
	if err := json.Unmarshal(raw, &probe); err != nil {
		return probeCase{}, fmt.Errorf("decode probe case: %w", err)
	}
	probe.ID = strings.TrimSpace(probe.ID)
	if probe.ID == "" {
		return probeCase{}, errors.New("probe id is required")
	}
	if probe.Request == nil {
		return probeCase{}, errors.New("probe request is required")
	}
	return probe, nil
}

func summarizeResponse(probe probeCase, httpStatus int, headers map[string][]string, raw []byte) probeSummary {
	summary := probeSummary{
		ProbeID:      strings.TrimSpace(probe.ID),
		HTTPStatus:   httpStatus,
		RequestID:    firstHeader(headers, "x-request-id", "openai-request-id", "cf-ray"),
		RawJSONValid: json.Valid(raw),
	}
	if !summary.RawJSONValid && looksLikeSSE(raw) {
		applySSESummary(raw, &summary)
		return summary
	}
	if !summary.RawJSONValid {
		summary.Error = &errorBrief{Message: "response body is not valid JSON"}
		return summary
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		summary.Error = &errorBrief{Message: err.Error()}
		return summary
	}
	summary.ResponseID = stringValue(body["id"])
	summary.Status = stringValue(body["status"])
	summary.Usage = usageFromAny(body["usage"])
	if errObj, ok := body["error"].(map[string]any); ok {
		summary.Error = &errorBrief{
			Type:    stringValue(errObj["type"]),
			Message: stringValue(errObj["message"]),
		}
	}
	if output, ok := body["output"].([]any); ok {
		for _, itemRaw := range output {
			item, ok := itemRaw.(map[string]any)
			if !ok {
				continue
			}
			itemType := stringValue(item["type"])
			if itemType != "" {
				summary.OutputTypes = append(summary.OutputTypes, itemType)
			}
			if itemType == "function_call" {
				args := stringValue(item["arguments"])
				summary.FunctionCalls = append(summary.FunctionCalls, functionCallBrief{
					ID:                 stringValue(item["id"]),
					CallID:             stringValue(item["call_id"]),
					Name:               stringValue(item["name"]),
					ArgumentsJSONValid: json.Valid([]byte(args)),
				})
			}
		}
	}
	return summary
}

func looksLikeSSE(raw []byte) bool {
	text := strings.TrimSpace(string(raw))
	return strings.HasPrefix(text, "event:") || strings.Contains(text, "\ndata:")
}

func applySSESummary(raw []byte, summary *probeSummary) {
	summary.Stream = true
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	var dataLines []string
	flush := func() {
		if len(dataLines) == 0 {
			return
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		if data == "[DONE]" || strings.TrimSpace(data) == "" {
			return
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return
		}
		eventType := stringValue(event["type"])
		if eventType != "" {
			summary.StreamEventTypes = append(summary.StreamEventTypes, eventType)
		}
		if resp, ok := event["response"].(map[string]any); ok {
			if summary.ResponseID == "" {
				summary.ResponseID = stringValue(resp["id"])
			}
			if summary.Status == "" {
				summary.Status = stringValue(resp["status"])
			}
			if usage := usageFromAny(resp["usage"]); usage.TotalTokens != 0 {
				summary.Usage = usage
			}
		}
		if errObj, ok := event["error"].(map[string]any); ok {
			summary.Error = &errorBrief{
				Type:    stringValue(errObj["type"]),
				Message: stringValue(errObj["message"]),
			}
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
}

func usageFromAny(value any) usageBrief {
	obj, ok := value.(map[string]any)
	if !ok {
		return usageBrief{}
	}
	return usageBrief{
		InputTokens:  int64Value(obj["input_tokens"]),
		OutputTokens: int64Value(obj["output_tokens"]),
		TotalTokens:  int64Value(obj["total_tokens"]),
	}
}

func writeSummary(outDir, probeID string, summary probeSummary) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	path := buildOutputPath(outDir, probeID, stamp)
	raw, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}

func buildOutputPath(outDir, probeID, timestamp string) string {
	name := sanitizeFilename(probeID) + "-" + sanitizeFilename(timestamp) + ".json"
	return filepath.ToSlash(filepath.Join(outDir, name))
}

func sanitizeFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "probe"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func firstHeader(headers map[string][]string, names ...string) string {
	for key, values := range headers {
		for _, name := range names {
			if strings.EqualFold(key, name) && len(values) > 0 {
				return strings.TrimSpace(values[0])
			}
		}
	}
	return ""
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func int64Value(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

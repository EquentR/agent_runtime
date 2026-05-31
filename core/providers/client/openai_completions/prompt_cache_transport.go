package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type promptCacheTransport struct {
	base http.RoundTripper
}

func newPromptCacheTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &promptCacheTransport{base: base}
}

func (t *promptCacheTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if !shouldInjectPromptCacheKey(req) {
		return base.RoundTrip(req)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))

	rewritten, changed := injectPromptCacheKey(body)
	if !changed {
		return base.RoundTrip(req)
	}

	cloned := req.Clone(req.Context())
	cloned.Body = io.NopCloser(bytes.NewReader(rewritten))
	cloned.ContentLength = int64(len(rewritten))
	cloned.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(rewritten)), nil
	}
	return base.RoundTrip(cloned)
}

func shouldInjectPromptCacheKey(req *http.Request) bool {
	if req == nil || req.Body == nil || req.Method != http.MethodPost || req.URL == nil {
		return false
	}
	return strings.HasSuffix(req.URL.Path, "/chat/completions")
}

func injectPromptCacheKey(body []byte) ([]byte, bool) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, false
	}
	if strings.TrimSpace(rawString(payload["prompt_cache_key"])) != "" {
		return body, false
	}
	cacheKey := strings.TrimSpace(rawString(payload["user"]))
	if cacheKey == "" {
		return body, false
	}
	cacheKeyJSON, err := json.Marshal(cacheKey)
	if err != nil {
		return body, false
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return body, false
	}
	prefix := trimmed[:len(trimmed)-1]
	rewritten := make([]byte, 0, len(trimmed)+len(cacheKeyJSON)+22)
	rewritten = append(rewritten, prefix...)
	if len(bytes.TrimSpace(prefix)) > 1 {
		rewritten = append(rewritten, ',')
	}
	rewritten = append(rewritten, `"prompt_cache_key":`...)
	rewritten = append(rewritten, cacheKeyJSON...)
	rewritten = append(rewritten, '}')
	return rewritten, true
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

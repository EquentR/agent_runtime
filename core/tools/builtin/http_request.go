package builtin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	corelog "github.com/EquentR/agent_runtime/core/log"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newHTTPRequestTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "http_request",
		Description: "Send an HTTP request and return the response",
		Source:      "builtin",
		Parameters: objectSchema([]string{"url"}, map[string]types.SchemaProperty{
			"url":             {Type: "string", Description: "Request URL"},
			"method":          {Type: "string", Description: "HTTP method"},
			"headers":         {Type: "object", Description: "Request headers"},
			"body":            {Type: "string", Description: "Request body"},
			"timeout_seconds": {Type: "integer", Description: "Request timeout in seconds"},
		}),
		Handler: func(ctx context.Context, arguments map[string]any) (string, error) {
			urlValue, err := requiredStringArg(arguments, "url")
			if err != nil {
				return "", err
			}
			method, ok, err := optionalStringArg(arguments, "method")
			if err != nil {
				return "", err
			}
			if !ok || method == "" {
				method = http.MethodGet
			}
			headers, err := stringMapArg(arguments, "headers")
			if err != nil {
				return "", err
			}
			body := ""
			if value, ok := arguments["body"]; ok && value != nil {
				text, ok := value.(string)
				if !ok {
					return "", fmt.Errorf("body must be a string")
				}
				body = text
			}
			timeoutSeconds, err := intArg(arguments, "timeout_seconds", int(defaultHTTPTimeout/time.Second))
			if err != nil {
				return "", err
			}

			startedAt := time.Now()
			logToolStart(ctx, "http_request", corelog.String("method", strings.ToUpper(method)), corelog.String("url", urlValue), corelog.Int("headers_count", len(headers)), corelog.Int("timeout_seconds", timeoutSeconds), corelog.Int("body_length", len(body)))
			requestCtx, cancel := context.WithTimeout(ctx, clampDuration(time.Duration(timeoutSeconds)*time.Second, minCommandTimeout, maxCommandTimeout))
			defer cancel()
			request, err := http.NewRequestWithContext(requestCtx, strings.ToUpper(method), urlValue, strings.NewReader(body))
			if err != nil {
				logToolFailure(ctx, "http_request", err, corelog.String("method", strings.ToUpper(method)), corelog.String("url", urlValue))
				return "", err
			}
			for key, value := range headers {
				request.Header.Set(key, value)
			}

			response, err := env.httpClientWithTimeout(time.Duration(timeoutSeconds) * time.Second).Do(request)
			if err != nil {
				logToolFailure(ctx, "http_request", err, corelog.String("method", strings.ToUpper(method)), corelog.String("url", urlValue), corelog.Duration("duration", time.Since(startedAt)))
				return "", err
			}
			defer response.Body.Close()

			responseBody, err := io.ReadAll(response.Body)
			if err != nil {
				logToolFailure(ctx, "http_request", err, corelog.String("method", strings.ToUpper(method)), corelog.String("url", urlValue), corelog.Int("status_code", response.StatusCode), corelog.Duration("duration", time.Since(startedAt)))
				return "", err
			}
			logToolFinish(ctx, "http_request", corelog.String("method", strings.ToUpper(method)), corelog.String("url", urlValue), corelog.Int("status_code", response.StatusCode), corelog.Int("response_length", len(responseBody)), corelog.Duration("duration", time.Since(startedAt)))

			return jsonResult(struct {
				StatusCode  int    `json:"status_code"`
				Body        string `json:"body"`
				URL         string `json:"url,omitempty"`
				ContentType string `json:"content_type,omitempty"`
			}{
				StatusCode:  response.StatusCode,
				Body:        string(responseBody),
				URL:         response.Request.URL.String(),
				ContentType: response.Header.Get("Content-Type"),
			})
		},
	}
}

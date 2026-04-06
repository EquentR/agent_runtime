package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	corelog "github.com/EquentR/agent_runtime/core/log"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

type webSearchProvider interface {
	Search(ctx context.Context, query string, maxResults int) ([]webSearchResult, error)
}

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func newWebSearchTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "web_search",
		Description: "Search the web using a configured provider",
		Source:      "builtin",
		Parameters: objectSchema([]string{"query"}, map[string]types.SchemaProperty{
			"query":       {Type: "string", Description: "Search query"},
			"provider":    {Type: "string", Description: "Preferred search provider", Enum: []string{"tavily", "serpapi", "bing"}},
			"max_results": {Type: "integer", Description: "Maximum number of results to return"},
		}),
		Handler: func(ctx context.Context, arguments map[string]any) (string, error) {
			query, err := requiredStringArg(arguments, "query")
			if err != nil {
				return "", err
			}
			providerName, _, err := optionalStringArg(arguments, "provider")
			if err != nil {
				return "", err
			}
			maxResults, err := intArg(arguments, "max_results", 5)
			if err != nil {
				return "", err
			}
			if maxResults <= 0 || maxResults > env.outputBudget.webSearchMaxResults {
				maxResults = env.outputBudget.webSearchMaxResults
			}
			startedAt := time.Now()
			logToolStart(ctx, "web_search", corelog.String("provider", providerName), corelog.Int("query_length", len(query)), corelog.Int("max_results", maxResults))

			resolvedName, provider, err := env.resolveWebSearchProvider(providerName)
			if err != nil {
				logToolFailure(ctx, "web_search", err, corelog.String("provider", providerName), corelog.Int("query_length", len(query)))
				return "", err
			}
			results, err := provider.Search(ctx, query, maxResults)
			if err != nil {
				logToolFailure(ctx, "web_search", err, corelog.String("provider", resolvedName), corelog.Int("query_length", len(query)), corelog.Duration("duration", time.Since(startedAt)))
				return "", err
			}
			trimmed, truncated := trimSlice(results, env.outputBudget.webSearchMaxResults)
			for i := range trimmed {
				trimmed[i].Snippet = truncateMatchText(trimmed[i].Snippet, env.outputBudget.matchTextMaxBytes)
			}
			logToolFinish(ctx, "web_search", corelog.String("provider", resolvedName), corelog.Int("query_length", len(query)), corelog.Int("result_count", len(trimmed)), corelog.Duration("duration", time.Since(startedAt)))
			return jsonResult(struct {
				Provider        string            `json:"provider"`
				Results         []webSearchResult `json:"results"`
				ReturnedResults int               `json:"returned_results"`
				Truncated       bool              `json:"truncated"`
			}{Provider: resolvedName, Results: trimmed, ReturnedResults: len(trimmed), Truncated: truncated})
		},
	}
}

func (e runtimeEnv) resolveWebSearchProvider(name string) (string, webSearchProvider, error) {
	resolved := strings.ToLower(strings.TrimSpace(name))
	if resolved == "" {
		resolved = strings.ToLower(strings.TrimSpace(e.webSearch.DefaultProvider))
	}
	if resolved == "" {
		providers := configuredProviders(e.webSearch)
		if len(providers) == 1 {
			resolved = providers[0]
		}
	}
	if resolved == "" {
		return "", nil, fmt.Errorf("web_search provider is not configured")
	}

	switch resolved {
	case "tavily":
		if e.webSearch.Tavily == nil || strings.TrimSpace(e.webSearch.Tavily.APIKey) == "" {
			return "", nil, fmt.Errorf("web_search provider %q is not configured", resolved)
		}
		return resolved, tavilyProvider{client: e.httpClient, config: *e.webSearch.Tavily}, nil
	case "serpapi":
		if e.webSearch.SerpAPI == nil || strings.TrimSpace(e.webSearch.SerpAPI.APIKey) == "" {
			return "", nil, fmt.Errorf("web_search provider %q is not configured", resolved)
		}
		return resolved, serpAPIProvider{client: e.httpClient, config: *e.webSearch.SerpAPI}, nil
	case "bing":
		if e.webSearch.Bing == nil || strings.TrimSpace(e.webSearch.Bing.APIKey) == "" {
			return "", nil, fmt.Errorf("web_search provider %q is not configured", resolved)
		}
		return resolved, bingProvider{client: e.httpClient, config: *e.webSearch.Bing}, nil
	default:
		return "", nil, fmt.Errorf("unsupported web_search provider: %s", resolved)
	}
}

func configuredProviders(options WebSearchOptions) []string {
	result := make([]string, 0, 3)
	if options.Tavily != nil && strings.TrimSpace(options.Tavily.APIKey) != "" {
		result = append(result, "tavily")
	}
	if options.SerpAPI != nil && strings.TrimSpace(options.SerpAPI.APIKey) != "" {
		result = append(result, "serpapi")
	}
	if options.Bing != nil && strings.TrimSpace(options.Bing.APIKey) != "" {
		result = append(result, "bing")
	}
	return result
}

type tavilyProvider struct {
	client *http.Client
	config TavilyConfig
}

func (p tavilyProvider) Search(ctx context.Context, query string, maxResults int) ([]webSearchResult, error) {
	endpoint := strings.TrimRight(defaultIfEmpty(p.config.BaseURL, "https://api.tavily.com"), "/") + "/search"
	payload := map[string]any{"api_key": p.config.APIKey, "query": query, "max_results": maxResults}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("tavily search failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var parsed struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	results := make([]webSearchResult, 0, len(parsed.Results))
	for _, item := range parsed.Results {
		results = append(results, webSearchResult{Title: item.Title, URL: item.URL, Snippet: item.Content})
	}
	return results, nil
}

type serpAPIProvider struct {
	client *http.Client
	config SerpAPIConfig
}

func (p serpAPIProvider) Search(ctx context.Context, query string, maxResults int) ([]webSearchResult, error) {
	baseURL := strings.TrimRight(defaultIfEmpty(p.config.BaseURL, "https://serpapi.com"), "/")
	values := url.Values{}
	values.Set("engine", "google")
	values.Set("q", query)
	values.Set("api_key", p.config.APIKey)
	if maxResults > 0 {
		values.Set("num", fmt.Sprintf("%d", maxResults))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/search.json?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("serpapi search failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var parsed struct {
		OrganicResults []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic_results"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	results := make([]webSearchResult, 0, len(parsed.OrganicResults))
	for _, item := range parsed.OrganicResults {
		results = append(results, webSearchResult{Title: item.Title, URL: item.Link, Snippet: item.Snippet})
	}
	return results, nil
}

type bingProvider struct {
	client *http.Client
	config BingConfig
}

func (p bingProvider) Search(ctx context.Context, query string, maxResults int) ([]webSearchResult, error) {
	baseURL := strings.TrimRight(defaultIfEmpty(p.config.BaseURL, "https://api.bing.microsoft.com"), "/")
	values := url.Values{}
	values.Set("q", query)
	if maxResults > 0 {
		values.Set("count", fmt.Sprintf("%d", maxResults))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v7.0/search?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", p.config.APIKey)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("bing search failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var parsed struct {
		WebPages struct {
			Value []struct {
				Name    string `json:"name"`
				URL     string `json:"url"`
				Snippet string `json:"snippet"`
			} `json:"value"`
		} `json:"webPages"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	results := make([]webSearchResult, 0, len(parsed.WebPages.Value))
	for _, item := range parsed.WebPages.Value {
		results = append(results, webSearchResult{Title: item.Name, URL: item.URL, Snippet: item.Snippet})
	}
	return results, nil
}

func defaultIfEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

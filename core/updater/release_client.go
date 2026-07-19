package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	OfficialOwner      = "EquentR"
	OfficialRepository = "agent_runtime"
)

type ReleaseClientOptions struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
}

type ReleaseClient struct {
	baseURL    *url.URL
	httpClient *http.Client
	token      string
}

type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	Assets      []Asset   `json:"assets"`
}

type Asset struct {
	Name          string `json:"name"`
	BrowserURL    string `json:"browser_download_url"`
	Size          int64  `json:"size"`
	DownloadCount int64  `json:"download_count"`
}

type ReleaseResult struct {
	Release           Release
	ETag              string
	NotModified       bool
	AuthorizationSent bool
}

func NewReleaseClient(options ReleaseClientOptions) (*ReleaseClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return nil, fmt.Errorf("invalid GitHub API base URL %q", baseURL)
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	token := strings.TrimSpace(options.Token)
	if token != "" && strings.EqualFold(parsed.Hostname(), "api.github.com") && parsed.Scheme != "https" {
		return nil, fmt.Errorf("GitHub API token requires HTTPS")
	}
	clientCopy := *httpClient
	transport := clientCopy.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clientCopy.Transport = tokenTransport{base: transport, token: token}
	return &ReleaseClient{baseURL: parsed, httpClient: &clientCopy, token: token}, nil
}

func (c *ReleaseClient) Latest(ctx context.Context, etag string) (ReleaseResult, error) {
	if c == nil || c.baseURL == nil || c.httpClient == nil {
		return ReleaseResult{}, fmt.Errorf("release client is not configured")
	}
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/repos/" + OfficialOwner + "/" + OfficialRepository + "/releases/latest"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return ReleaseResult{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "ice-art-updater")
	authorizationSent := false
	if c.token != "" && strings.EqualFold(c.baseURL.Hostname(), "api.github.com") && c.baseURL.Scheme == "https" {
		authorizationSent = true
	}
	if strings.TrimSpace(etag) != "" {
		request.Header.Set("If-None-Match", etag)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return ReleaseResult{}, err
	}
	defer response.Body.Close()
	result := ReleaseResult{ETag: response.Header.Get("ETag"), AuthorizationSent: authorizationSent}
	if response.StatusCode == http.StatusNotModified {
		result.NotModified = true
		return result, nil
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return ReleaseResult{}, fmt.Errorf("GitHub latest release returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&result.Release); err != nil {
		return ReleaseResult{}, fmt.Errorf("decode latest release: %w", err)
	}
	if result.Release.Draft || result.Release.Prerelease {
		return ReleaseResult{}, fmt.Errorf("latest release is draft or prerelease")
	}
	if _, err := parseReleaseVersion(result.Release.TagName); err != nil {
		return ReleaseResult{}, fmt.Errorf("invalid latest release tag: %w", err)
	}
	result.Release.HTMLURL = "https://github.com/" + OfficialOwner + "/" + OfficialRepository + "/releases/tag/" + url.PathEscape(result.Release.TagName)
	return result, nil
}

type tokenTransport struct {
	base  http.RoundTripper
	token string
}

func (t tokenTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header.Del("Authorization")
	if t.token != "" && strings.EqualFold(cloned.URL.Hostname(), "api.github.com") && cloned.URL.Scheme == "https" {
		cloned.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(cloned)
}

func (r Release) AssetFor(goos, goarch string) (Asset, error) {
	wantName, err := officialAssetName(goos, goarch)
	if err != nil {
		return Asset{}, err
	}
	var match Asset
	count := 0
	for _, asset := range r.Assets {
		if asset.Name == wantName {
			match = asset
			count++
		}
	}
	if count != 1 {
		return Asset{}, fmt.Errorf("expected one asset named %q, found %d", wantName, count)
	}
	return match, nil
}

func officialAssetName(goos, goarch string) (string, error) {
	goos = strings.TrimSpace(goos)
	goarch = strings.TrimSpace(goarch)
	extension := ".tar.gz"
	switch goos {
	case "windows":
		extension = ".zip"
	case "linux", "darwin":
	default:
		return "", fmt.Errorf("unsupported release platform %q", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return "", fmt.Errorf("unsupported release architecture %q", goarch)
	}
	return fmt.Sprintf("ice_art_%s_%s%s", goos, goarch, extension), nil
}

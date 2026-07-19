package updater

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReleaseClientReturnsStableLatestReleaseAndETag(t *testing.T) {
	var receivedETag string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedETag = r.Header.Get("If-None-Match")
		if receivedETag != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"release-etag"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"tag_name":"v1.2.3","name":"Ice Art 1.2.3","body":"notes",
			"html_url":"https://github.com/EquentR/agent_runtime/releases/tag/v1.2.3",
			"published_at":"2026-07-19T01:02:03Z","draft":false,"prerelease":false,
			"assets":[
				{"name":"ice_art_windows_amd64.zip","browser_download_url":"https://example.test/windows.zip","size":42},
				{"name":"SHA256SUMS","browser_download_url":"https://example.test/SHA256SUMS","size":64},
				{"name":"SHA256SUMS.sig","browser_download_url":"https://example.test/SHA256SUMS.sig","size":64}
			]
		}`)
	}))
	defer server.Close()

	client, err := NewReleaseClient(ReleaseClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewReleaseClient() error = %v", err)
	}
	result, err := client.Latest(context.Background(), "")
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if result.NotModified || result.ETag != `"release-etag"` || result.Release.TagName != "v1.2.3" {
		t.Fatalf("Latest() = %#v, want release v1.2.3 with ETag", result)
	}
	asset, err := result.Release.AssetFor("windows", "amd64")
	if err != nil {
		t.Fatalf("AssetFor() error = %v", err)
	}
	if asset.Name != "ice_art_windows_amd64.zip" {
		t.Fatalf("AssetFor().Name = %q", asset.Name)
	}

	result, err = client.Latest(context.Background(), result.ETag)
	if err != nil {
		t.Fatalf("Latest(ETag) error = %v", err)
	}
	if !result.NotModified || receivedETag != `"release-etag"` {
		t.Fatalf("Latest(ETag) = %#v, received If-None-Match %q", result, receivedETag)
	}
}

func TestReleaseClientRejectsDraftPrereleaseAndInvalidTag(t *testing.T) {
	for name, body := range map[string]string{
		"draft":             `{"tag_name":"v1.2.3","draft":true,"prerelease":false}`,
		"prerelease":        `{"tag_name":"v1.2.3-rc.1","draft":false,"prerelease":true}`,
		"hidden prerelease": `{"tag_name":"v1.2.3-rc.1","draft":false,"prerelease":false}`,
		"invalid tag":       `{"tag_name":"latest","draft":false,"prerelease":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			client, err := NewReleaseClient(ReleaseClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
			if err != nil {
				t.Fatalf("NewReleaseClient() error = %v", err)
			}
			if _, err := client.Latest(context.Background(), ""); err == nil {
				t.Fatal("Latest() error = nil, want rejection")
			}
		})
	}
}

func TestReleaseClientPreservesMirrorBasePath(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"tag_name":"v1.2.3","draft":false,"prerelease":false}`)
	}))
	defer server.Close()
	client, err := NewReleaseClient(ReleaseClientOptions{BaseURL: server.URL + "/github-api", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewReleaseClient() error = %v", err)
	}
	if _, err := client.Latest(context.Background(), ""); err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	want := "/github-api/repos/EquentR/agent_runtime/releases/latest"
	if gotPath != want {
		t.Fatalf("request path = %q, want %q", gotPath, want)
	}
}

func TestReleaseAssetForRequiresExactOfficialAssetName(t *testing.T) {
	release := Release{Assets: []Asset{{Name: "ice_art_windows_amd64.zip.sig"}}}
	if _, err := release.AssetFor("windows", "amd64"); err == nil {
		t.Fatal("AssetFor() error = nil, want unofficial suffix rejection")
	}
}

func TestReleaseClientSendsTokenOnlyToOfficialGitHubAPI(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := io.NopCloser(strings.NewReader(`{"tag_name":"v1.2.3","draft":false,"prerelease":false}`))
		response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: r}
		response.Header.Set("X-Observed-Authorization", r.Header.Get("Authorization"))
		return response, nil
	})
	for _, test := range []struct {
		name       string
		baseURL    string
		wantBearer bool
	}{
		{name: "official", baseURL: "https://api.github.com", wantBearer: true},
		{name: "mirror", baseURL: "https://github-mirror.example.test", wantBearer: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewReleaseClient(ReleaseClientOptions{BaseURL: test.baseURL, HTTPClient: &http.Client{Transport: transport}, Token: "secret-token"})
			if err != nil {
				t.Fatalf("NewReleaseClient() error = %v", err)
			}
			result, err := client.Latest(context.Background(), "")
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			got := result.AuthorizationSent
			if got != test.wantBearer {
				t.Fatalf("AuthorizationSent = %v, want %v", got, test.wantBearer)
			}
		})
	}
}

func TestReleaseClientRejectsTokenOverHTTPOfficialAPI(t *testing.T) {
	if _, err := NewReleaseClient(ReleaseClientOptions{BaseURL: "http://api.github.com", Token: "secret-token"}); err == nil {
		t.Fatal("NewReleaseClient() error = nil, want HTTPS requirement")
	}
}

func TestReleaseClientStripsTokenAcrossRedirect(t *testing.T) {
	var requests []struct {
		host string
		auth string
	}
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, struct {
			host string
			auth string
		}{host: request.URL.Hostname(), auth: request.Header.Get("Authorization")})
		if len(requests) == 1 {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://uploads.github.com/repos/EquentR/agent_runtime/releases/latest"}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v1.2.3","draft":false,"prerelease":false}`)), Header: make(http.Header), Request: request}, nil
	})
	client, err := NewReleaseClient(ReleaseClientOptions{BaseURL: "https://api.github.com", HTTPClient: &http.Client{Transport: transport}, Token: "secret-token"})
	if err != nil {
		t.Fatalf("NewReleaseClient() error = %v", err)
	}
	if _, err := client.Latest(context.Background(), ""); err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if len(requests) != 2 || requests[0].auth == "" || requests[1].auth != "" {
		t.Fatalf("redirect request auth = %#v, want only official request authorized", requests)
	}
}

func TestIsNewerReleaseUsesSemanticVersioning(t *testing.T) {
	newer, err := IsNewerRelease("v1.9.0", "v1.10.0")
	if err != nil || !newer {
		t.Fatalf("IsNewerRelease() = %v, %v, want true, nil", newer, err)
	}
	newer, err = IsNewerRelease("v1.10.0", "v1.10.0")
	if err != nil || newer {
		t.Fatalf("IsNewerRelease(equal) = %v, %v, want false, nil", newer, err)
	}
}

func TestIsNewerReleaseRejectsNonCanonicalStableTags(t *testing.T) {
	for _, value := range []string{"v1.2.3-rc.1", "v1.2.3+build.1", "v1.2.3.4"} {
		if _, err := IsNewerRelease("v1.2.2", value); err == nil {
			t.Fatalf("IsNewerRelease(%q) error = nil, want canonical stable tag rejection", value)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

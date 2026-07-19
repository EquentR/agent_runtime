package updater

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestReleaseCheckerCachesSuccessfulReleaseAndUsesETagNotModified(t *testing.T) {
	cache, err := NewReleaseCache(filepath.Join(t.TempDir(), "release-cache.json"))
	if err != nil {
		t.Fatalf("NewReleaseCache() error = %v", err)
	}
	source := &fakeReleaseSource{results: []ReleaseResult{
		{ETag: `"etag-1"`, Release: Release{TagName: "v1.2.3"}},
		{ETag: `"etag-1"`, NotModified: true},
	}}
	now := time.Date(2026, 7, 19, 1, 2, 3, 0, time.UTC)
	checker := NewReleaseChecker(ReleaseCheckerOptions{Source: source, Cache: cache, CurrentVersion: "v1.2.2", Now: func() time.Time { return now }})
	first, err := checker.Check(context.Background())
	if err != nil || !first.UpdateAvailable || first.Release.TagName != "v1.2.3" || first.FromCache {
		t.Fatalf("first Check() = %#v, %v", first, err)
	}
	second, err := checker.Check(context.Background())
	if err != nil || !second.UpdateAvailable || !second.FromCache || second.Release.TagName != "v1.2.3" {
		t.Fatalf("second Check() = %#v, %v", second, err)
	}
	if source.etags[1] != `"etag-1"` {
		t.Fatalf("second ETag = %q, want cached ETag", source.etags[1])
	}
}

func TestReleaseCheckerRetainsCacheWhenNetworkFails(t *testing.T) {
	cache, err := NewReleaseCache(filepath.Join(t.TempDir(), "release-cache.json"))
	if err != nil {
		t.Fatalf("NewReleaseCache() error = %v", err)
	}
	source := &fakeReleaseSource{results: []ReleaseResult{{ETag: `"etag-1"`, Release: Release{TagName: "v1.2.3"}}, {}}, errors: []error{nil, context.DeadlineExceeded}}
	checker := NewReleaseChecker(ReleaseCheckerOptions{Source: source, Cache: cache, CurrentVersion: "v1.2.2", Now: func() time.Time { return time.Date(2026, 7, 19, 1, 2, 3, 0, time.UTC) }})
	_, _ = checker.Check(context.Background())
	result, err := checker.Check(context.Background())
	if err == nil || result.Release.TagName != "v1.2.3" || !result.FromCache {
		t.Fatalf("failed Check() = %#v, %v, want cached release and error", result, err)
	}
}

type fakeReleaseSource struct {
	results []ReleaseResult
	errors  []error
	etags   []string
}

func (f *fakeReleaseSource) Latest(_ context.Context, etag string) (ReleaseResult, error) {
	f.etags = append(f.etags, etag)
	result := f.results[0]
	f.results = f.results[1:]
	var err error
	if len(f.errors) > 0 {
		err = f.errors[0]
		f.errors = f.errors[1:]
	}
	return result, err
}

package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type CachedRelease struct {
	ETag      string    `json:"etag,omitempty"`
	Release   Release   `json:"release"`
	CheckedAt time.Time `json:"checked_at"`
}

type ReleaseCache struct {
	path string
	mu   sync.Mutex
}

func NewReleaseCache(path string) (*ReleaseCache, error) {
	if path == "" {
		return nil, fmt.Errorf("release cache path is required")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, err
	}
	return &ReleaseCache{path: abs}, nil
}

func (c *ReleaseCache) Load() (CachedRelease, error) {
	if c == nil {
		return CachedRelease{}, fmt.Errorf("release cache is not configured")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loadUnlocked()
}

func (c *ReleaseCache) Save(value CachedRelease) error {
	if c == nil {
		return fmt.Errorf("release cache is not configured")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	value.CheckedAt = value.CheckedAt.UTC()
	return writeJSONAtomic(c.path, value, 0o600)
}

func (c *ReleaseCache) loadUnlocked() (CachedRelease, error) {
	raw, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		return CachedRelease{}, nil
	}
	if err != nil {
		return CachedRelease{}, err
	}
	var cached CachedRelease
	if err := json.Unmarshal(raw, &cached); err != nil {
		return CachedRelease{}, fmt.Errorf("decode release cache: %w", err)
	}
	return cached, nil
}

type ReleaseSource interface {
	Latest(ctx context.Context, etag string) (ReleaseResult, error)
}

type ReleaseCheckerOptions struct {
	Source         ReleaseSource
	Cache          *ReleaseCache
	CurrentVersion string
	Now            func() time.Time
}

type ReleaseChecker struct {
	source         ReleaseSource
	cache          *ReleaseCache
	currentVersion string
	now            func() time.Time
	mu             sync.Mutex
}

type CheckResult struct {
	Release         Release   `json:"release"`
	CheckedAt       time.Time `json:"checked_at"`
	UpdateAvailable bool      `json:"update_available"`
	FromCache       bool      `json:"from_cache"`
}

func NewReleaseChecker(options ReleaseCheckerOptions) *ReleaseChecker {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &ReleaseChecker{source: options.Source, cache: options.Cache, currentVersion: options.CurrentVersion, now: now}
}

func (c *ReleaseChecker) Check(ctx context.Context) (CheckResult, error) {
	if c == nil || c.source == nil || c.cache == nil {
		return CheckResult{}, fmt.Errorf("release checker is not configured")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cached, err := c.cache.Load()
	if err != nil {
		return CheckResult{}, err
	}
	latest, err := c.source.Latest(ctx, cached.ETag)
	if err != nil {
		if cached.Release.TagName != "" {
			result, resultErr := c.resultFor(cached.Release, cached.CheckedAt, true)
			if resultErr != nil {
				return CheckResult{}, resultErr
			}
			return result, err
		}
		return CheckResult{}, err
	}
	now := c.now().UTC()
	if latest.NotModified {
		if cached.Release.TagName == "" {
			return CheckResult{}, fmt.Errorf("GitHub returned not modified without a cached release")
		}
		cached.CheckedAt = now
		if latest.ETag != "" {
			cached.ETag = latest.ETag
		}
		if err := c.cache.Save(cached); err != nil {
			return CheckResult{}, err
		}
		return c.resultFor(cached.Release, now, true)
	}
	cached = CachedRelease{ETag: latest.ETag, Release: latest.Release, CheckedAt: now}
	if err := c.cache.Save(cached); err != nil {
		return CheckResult{}, err
	}
	return c.resultFor(latest.Release, now, false)
}

func (c *ReleaseChecker) resultFor(release Release, checkedAt time.Time, fromCache bool) (CheckResult, error) {
	available, err := IsNewerRelease(c.currentVersion, release.TagName)
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{Release: release, CheckedAt: checkedAt, UpdateAvailable: available, FromCache: fromCache}, nil
}

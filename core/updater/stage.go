package updater

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxReleaseDownloadSize int64 = 2 << 30

type StageOptions struct {
	Root                string
	Release             Release
	GOOS                string
	GOARCH              string
	HTTPClient          *http.Client
	TrustedKeys         map[string]ed25519.PublicKey
	DownloadURLTemplate string
}

type StagedRelease struct {
	Root             string    `json:"root"`
	ExtractedRoot    string    `json:"extracted_root"`
	ArchivePath      string    `json:"archive_path"`
	ChecksumsPath    string    `json:"checksums_path"`
	SignaturePath    string    `json:"signature_path"`
	ExecutablePath   string    `json:"executable_path"`
	ArchiveSHA256    string    `json:"archive_sha256"`
	ExecutableSHA256 string    `json:"executable_sha256"`
	BuildInfo        BuildInfo `json:"build_info"`
	Release          Release   `json:"release"`
}

func StageRelease(ctx context.Context, options StageOptions) (StagedRelease, error) {
	root, err := canonicalRoot(options.Root)
	if err != nil {
		return StagedRelease{}, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return StagedRelease{}, err
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Minute}
	}
	archiveAsset, err := options.Release.AssetFor(options.GOOS, options.GOARCH)
	if err != nil {
		return StagedRelease{}, err
	}
	checksumsAsset, err := options.Release.AssetNamed("SHA256SUMS")
	if err != nil {
		return StagedRelease{}, err
	}
	signatureAsset, err := options.Release.AssetNamed("SHA256SUMS.sig")
	if err != nil {
		return StagedRelease{}, err
	}
	stageDir, err := os.MkdirTemp(root, ".stage-*")
	if err != nil {
		return StagedRelease{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stageDir)
		}
	}()
	downloadsDir := filepath.Join(stageDir, "downloads")
	if err := os.MkdirAll(downloadsDir, 0o700); err != nil {
		return StagedRelease{}, err
	}
	download := func(asset Asset) (string, error) {
		downloadURL, err := resolveDownloadURL(asset, options.DownloadURLTemplate)
		if err != nil {
			return "", err
		}
		path := filepath.Join(downloadsDir, asset.Name)
		return path, downloadFile(ctx, httpClient, downloadURL, asset.Name, path, maxReleaseDownloadSize)
	}
	checksumsPath, err := download(checksumsAsset)
	if err != nil {
		return StagedRelease{}, err
	}
	signaturePath, err := download(signatureAsset)
	if err != nil {
		return StagedRelease{}, err
	}
	checksums, err := os.ReadFile(checksumsPath)
	if err != nil {
		return StagedRelease{}, err
	}
	signature, err := os.ReadFile(signaturePath)
	if err != nil {
		return StagedRelease{}, err
	}
	if err := VerifySignedChecksums(checksums, signature, options.TrustedKeys); err != nil {
		return StagedRelease{}, err
	}
	parsedChecksums, err := ParseChecksums(checksums)
	if err != nil {
		return StagedRelease{}, err
	}
	expected, ok := parsedChecksums[archiveAsset.Name]
	if !ok {
		return StagedRelease{}, fmt.Errorf("signed checksums do not contain %s", archiveAsset.Name)
	}
	archivePath, err := download(archiveAsset)
	if err != nil {
		return StagedRelease{}, err
	}
	if err := VerifyFileChecksum(archivePath, expected); err != nil {
		return StagedRelease{}, err
	}
	extractRoot := filepath.Join(stageDir, "extracted")
	if err := ExtractArchive(archivePath, extractRoot, DefaultExtractLimits()); err != nil {
		return StagedRelease{}, err
	}
	buildInfoPath, err := findUniqueBuildInfo(extractRoot)
	if err != nil {
		return StagedRelease{}, err
	}
	rawBuildInfo, err := os.ReadFile(buildInfoPath)
	if err != nil {
		return StagedRelease{}, err
	}
	var buildInfo BuildInfo
	if err := json.Unmarshal(rawBuildInfo, &buildInfo); err != nil {
		return StagedRelease{}, fmt.Errorf("decode staged build info: %w", err)
	}
	buildInfo = buildInfo.Normalized()
	if buildInfo.Version != options.Release.TagName || buildInfo.GOOS != options.GOOS || buildInfo.GOARCH != options.GOARCH || buildInfo.Distribution != DistributionRelease {
		return StagedRelease{}, fmt.Errorf("staged build info does not match requested release")
	}
	if supported, reason := buildInfo.InstallationCapability(); !supported {
		return StagedRelease{}, fmt.Errorf("staged build is not installable: %s", reason)
	}
	binaryName := "ice_art"
	if options.GOOS == "windows" {
		binaryName = "ice_art.exe"
	}
	executablePath := filepath.Join(filepath.Dir(buildInfoPath), binaryName)
	executableSHA, err := fileSHA256(executablePath)
	if err != nil {
		return StagedRelease{}, fmt.Errorf("hash staged executable: %w", err)
	}
	cleanup = false
	return StagedRelease{
		Root:             stageDir,
		ExtractedRoot:    extractRoot,
		ArchivePath:      archivePath,
		ChecksumsPath:    checksumsPath,
		SignaturePath:    signaturePath,
		ExecutablePath:   executablePath,
		ArchiveSHA256:    expected,
		ExecutableSHA256: executableSHA,
		BuildInfo:        buildInfo,
		Release:          options.Release,
	}, nil
}

func (r Release) AssetNamed(name string) (Asset, error) {
	var match Asset
	count := 0
	for _, asset := range r.Assets {
		if asset.Name == name {
			match = asset
			count++
		}
	}
	if count != 1 {
		return Asset{}, fmt.Errorf("expected one asset named %q, found %d", name, count)
	}
	return match, nil
}

func resolveDownloadURL(asset Asset, template string) (string, error) {
	resolved := strings.TrimSpace(asset.BrowserURL)
	if strings.TrimSpace(template) != "" {
		resolved = strings.ReplaceAll(template, "{url}", url.QueryEscape(asset.BrowserURL))
		resolved = strings.ReplaceAll(resolved, "{name}", url.PathEscape(asset.Name))
	}
	parsed, err := url.Parse(resolved)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("invalid asset download URL for %s", asset.Name)
	}
	return parsed.String(), nil
}

func downloadFile(ctx context.Context, client *http.Client, sourceURL, assetName, destination string, maxBytes int64) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("download asset %q: %w", assetName, ctx.Err())
		}
		return fmt.Errorf("download asset %q request failed", assetName)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download asset %q returned %s", assetName, response.Status)
	}
	if response.ContentLength > maxBytes {
		return fmt.Errorf("download exceeds size limit")
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".download-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	written, err := io.Copy(temp, io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		_ = temp.Close()
		return err
	}
	if written > maxBytes {
		_ = temp.Close()
		return fmt.Errorf("download exceeds size limit")
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, destination)
}

func findUniqueBuildInfo(root string) (string, error) {
	var match string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("staged tree contains symlink: %s", path)
		}
		if !entry.IsDir() && entry.Name() == "build-info.json" {
			if match != "" {
				return fmt.Errorf("staged archive contains multiple build-info.json files")
			}
			match = path
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if match == "" {
		return "", fmt.Errorf("staged archive does not contain build-info.json")
	}
	return match, nil
}

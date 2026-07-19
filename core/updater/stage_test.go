package updater

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageReleaseDownloadsVerifiesAndReadsBuildInfo(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), "asset.zip")
	archiveFile, _ := os.Create(archivePath)
	archiveWriter := zip.NewWriter(archiveFile)
	entry, _ := archiveWriter.Create("ice_art_windows_amd64/ice_art.exe")
	_, _ = entry.Write([]byte("binary"))
	buildInfo, _ := archiveWriter.Create("ice_art_windows_amd64/build-info.json")
	_, _ = buildInfo.Write([]byte(`{"version":"v1.2.3","commit":"abc","distribution":"release","goos":"windows","goarch":"amd64"}`))
	_ = archiveWriter.Close()
	_ = archiveFile.Close()
	archiveData, _ := os.ReadFile(archivePath)
	digest := sha256.Sum256(archiveData)
	checksums := []byte(hex.EncodeToString(digest[:]) + "  ice_art_windows_amd64.zip\n")
	signature := SignatureEnvelope{KeyID: "release-2026", Algorithm: "ed25519", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, checksums))}
	signatureData, _ := json.Marshal(signature)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/asset.zip":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(archiveData)
		case "/SHA256SUMS":
			_, _ = w.Write(checksums)
		case "/SHA256SUMS.sig":
			_, _ = w.Write(signatureData)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	release := Release{TagName: "v1.2.3", Assets: []Asset{
		{Name: "ice_art_windows_amd64.zip", BrowserURL: server.URL + "/asset.zip"},
		{Name: "SHA256SUMS", BrowserURL: server.URL + "/SHA256SUMS"},
		{Name: "SHA256SUMS.sig", BrowserURL: server.URL + "/SHA256SUMS.sig"},
	}}
	staged, err := StageRelease(context.Background(), StageOptions{Root: t.TempDir(), Release: release, GOOS: "windows", GOARCH: "amd64", HTTPClient: server.Client(), TrustedKeys: map[string]ed25519.PublicKey{"release-2026": publicKey}})
	if err != nil {
		t.Fatalf("StageRelease() error = %v", err)
	}
	if staged.BuildInfo.Version != "v1.2.3" || staged.BuildInfo.GOOS != "windows" || staged.BuildInfo.Distribution != DistributionRelease {
		t.Fatalf("BuildInfo = %#v", staged.BuildInfo)
	}
	if _, err := os.Stat(filepath.Join(staged.ExtractedRoot, "ice_art_windows_amd64", "ice_art.exe")); err != nil {
		t.Fatalf("staged binary missing: %v", err)
	}
}

func TestStageReleaseRejectsUnsignedArchive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/SHA256SUMS.sig" {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		_, _ = io.WriteString(w, "00  ice_art_windows_amd64.zip\n")
	}))
	defer server.Close()
	_, err := StageRelease(context.Background(), StageOptions{Root: t.TempDir(), Release: Release{TagName: "v1.2.3", Assets: []Asset{{Name: "ice_art_windows_amd64.zip", BrowserURL: server.URL + "/asset.zip"}, {Name: "SHA256SUMS", BrowserURL: server.URL + "/SHA256SUMS"}, {Name: "SHA256SUMS.sig", BrowserURL: server.URL + "/SHA256SUMS.sig"}}}, GOOS: "windows", GOARCH: "amd64", HTTPClient: server.Client(), TrustedKeys: map[string]ed25519.PublicKey{}})
	if err == nil {
		t.Fatal("StageRelease() error = nil, want signature rejection")
	}
}

func TestDownloadFileErrorDoesNotExposeURLCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	err := downloadFile(context.Background(), server.Client(), server.URL+"/asset?token=top-secret", "asset.zip", filepath.Join(t.TempDir(), "asset.zip"), 1024)
	if err == nil || strings.Contains(err.Error(), "top-secret") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("downloadFile() error = %v, want redacted asset-only error", err)
	}
}

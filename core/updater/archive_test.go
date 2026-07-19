package updater

import (
	"archive/tar"
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractArchiveExtractsValidZipWithinDestination(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "release.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("ice_art_windows_amd64/ice_art.exe")
	if err != nil {
		t.Fatalf("Create entry error = %v", err)
	}
	_, _ = entry.Write([]byte("binary"))
	if err := writer.Close(); err != nil {
		t.Fatalf("zip Close() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("file Close() error = %v", err)
	}

	destination := t.TempDir()
	if err := ExtractArchive(archivePath, destination, DefaultExtractLimits()); err != nil {
		t.Fatalf("ExtractArchive() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "ice_art_windows_amd64", "ice_art.exe"))
	if err != nil || string(data) != "binary" {
		t.Fatalf("extracted file = %q, %v", data, err)
	}
}

func TestExtractArchiveRejectsTraversalAndLinks(t *testing.T) {
	t.Run("zip traversal", func(t *testing.T) {
		archivePath := filepath.Join(t.TempDir(), "release.zip")
		file, _ := os.Create(archivePath)
		writer := zip.NewWriter(file)
		entry, _ := writer.Create("../escape.exe")
		_, _ = entry.Write([]byte("bad"))
		_ = writer.Close()
		_ = file.Close()
		if err := ExtractArchive(archivePath, t.TempDir(), DefaultExtractLimits()); err == nil {
			t.Fatal("ExtractArchive() error = nil, want traversal rejection")
		}
	})

	t.Run("tar symlink", func(t *testing.T) {
		archivePath := filepath.Join(t.TempDir(), "release.tar.gz")
		file, _ := os.Create(archivePath)
		gzipWriter := newGzipWriter(t, file)
		writer := tar.NewWriter(gzipWriter)
		_ = writer.WriteHeader(&tar.Header{Name: "ice_art/link", Typeflag: tar.TypeSymlink, Linkname: "../../outside", Mode: 0o777})
		_ = writer.Close()
		_ = gzipWriter.Close()
		_ = file.Close()
		if err := ExtractArchive(archivePath, t.TempDir(), DefaultExtractLimits()); err == nil {
			t.Fatal("ExtractArchive() error = nil, want symlink rejection")
		}
	})
}

func TestExtractArchiveEnforcesTotalSizeLimit(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "release.zip")
	file, _ := os.Create(archivePath)
	writer := zip.NewWriter(file)
	entry, _ := writer.Create("large.bin")
	_, _ = entry.Write([]byte("0123456789"))
	_ = writer.Close()
	_ = file.Close()
	limits := DefaultExtractLimits()
	limits.MaxTotalSize = 5
	if err := ExtractArchive(archivePath, t.TempDir(), limits); err == nil {
		t.Fatal("ExtractArchive() error = nil, want size limit rejection")
	}
}

func TestExtractArchiveCountsDirectoryEntriesAgainstFileLimit(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "release.zip")
	file, _ := os.Create(archivePath)
	writer := zip.NewWriter(file)
	_, _ = writer.Create("one/")
	_, _ = writer.Create("two/")
	_ = writer.Close()
	_ = file.Close()
	limits := DefaultExtractLimits()
	limits.MaxFiles = 1
	if err := ExtractArchive(archivePath, t.TempDir(), limits); err == nil {
		t.Fatal("ExtractArchive() error = nil, want directory count rejection")
	}
}

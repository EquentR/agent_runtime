package attachments

import "testing"

func TestClassifyFileIdentifiesRasterImagesAndWorkspaceOnlyTypes(t *testing.T) {
	tests := []struct {
		name        string
		fileName    string
		mimeType    string
		wantKind    string
		wantRaster  bool
		wantAllowed bool
	}{
		{
			name:        "png raster image",
			fileName:    "photo.png",
			mimeType:    "image/png",
			wantKind:    KindImage,
			wantRaster:  true,
			wantAllowed: true,
		},
		{
			name:        "svg is text not raster image",
			fileName:    "vector.svg",
			mimeType:    "image/svg+xml",
			wantKind:    KindText,
			wantRaster:  false,
			wantAllowed: true,
		},
		{
			name:        "markdown text",
			fileName:    "notes.md",
			mimeType:    "text/markdown",
			wantKind:    KindText,
			wantRaster:  false,
			wantAllowed: true,
		},
		{
			name:        "json text",
			fileName:    "data.json",
			mimeType:    "application/json",
			wantKind:    KindText,
			wantRaster:  false,
			wantAllowed: true,
		},
		{
			name:        "pdf document",
			fileName:    "report.pdf",
			mimeType:    "application/pdf",
			wantKind:    KindDocument,
			wantRaster:  false,
			wantAllowed: true,
		},
		{
			name:        "office document",
			fileName:    "deck.pptx",
			mimeType:    "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			wantKind:    KindDocument,
			wantRaster:  false,
			wantAllowed: true,
		},
		{
			name:        "archive",
			fileName:    "archive.zip",
			mimeType:    "application/zip",
			wantKind:    KindArchive,
			wantRaster:  false,
			wantAllowed: true,
		},
		{
			name:        "unknown binary",
			fileName:    "blob.bin",
			mimeType:    "application/octet-stream",
			wantKind:    KindBinary,
			wantRaster:  false,
			wantAllowed: true,
		},
		{
			name:        "dangerous executable",
			fileName:    "malware.exe",
			mimeType:    "application/octet-stream",
			wantKind:    KindBinary,
			wantRaster:  false,
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyFile(tt.fileName, tt.mimeType)
			if got.Kind != tt.wantKind {
				t.Fatalf("Kind = %q, want %q", got.Kind, tt.wantKind)
			}
			if got.RasterImage != tt.wantRaster {
				t.Fatalf("RasterImage = %v, want %v", got.RasterImage, tt.wantRaster)
			}
			if got.UploadAllowed != tt.wantAllowed {
				t.Fatalf("UploadAllowed = %v, want %v", got.UploadAllowed, tt.wantAllowed)
			}
		})
	}
}

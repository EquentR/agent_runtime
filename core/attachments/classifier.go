package attachments

import (
	"path/filepath"
	"strings"
)

type FileClassification struct {
	Kind          string
	MimeType      string
	RasterImage   bool
	UploadAllowed bool
	RejectReason  string
}

func ClassifyFile(fileName string, mimeType string) FileClassification {
	normalizedMime := strings.ToLower(strings.TrimSpace(mimeType))
	extension := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	if extension == "" {
		extension = strings.ToLower(filepath.Ext(filepath.Base(strings.TrimSpace(fileName))))
	}

	classification := FileClassification{
		Kind:          KindBinary,
		MimeType:      normalizedMime,
		UploadAllowed: true,
	}
	if isDangerousBinaryExtension(extension) {
		classification.UploadAllowed = false
		classification.RejectReason = "file type is not allowed"
		return classification
	}

	switch {
	case isRasterImageMimeType(normalizedMime) || isRasterImageExtension(extension):
		classification.Kind = KindImage
		classification.RasterImage = true
	case isSVGMimeType(normalizedMime) || extension == ".svg":
		classification.Kind = KindText
	case isTextLikeMimeType(normalizedMime) || isTextLikeExtension(extension):
		classification.Kind = KindText
	case isDocumentMimeType(normalizedMime) || isDocumentExtension(extension):
		classification.Kind = KindDocument
	case isArchiveMimeType(normalizedMime) || isArchiveExtension(extension):
		classification.Kind = KindArchive
	default:
		classification.Kind = KindBinary
	}
	return classification
}

func IsRasterImageMimeType(mimeType string) bool {
	return isRasterImageMimeType(strings.ToLower(strings.TrimSpace(mimeType)))
}

func isRasterImageMimeType(mimeType string) bool {
	switch mimeType {
	case "image/png", "image/jpeg", "image/jpg", "image/webp", "image/gif", "image/bmp", "image/tiff":
		return true
	default:
		return false
	}
}

func isRasterImageExtension(extension string) bool {
	switch extension {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

func isSVGMimeType(mimeType string) bool {
	return mimeType == "image/svg+xml" || mimeType == "application/svg+xml"
}

func isTextLikeMimeType(mimeType string) bool {
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	switch mimeType {
	case "application/json", "application/xml", "application/javascript", "application/x-javascript", "application/yaml", "application/x-yaml", "application/toml", "application/csv":
		return true
	default:
		return strings.HasSuffix(mimeType, "+json") || strings.HasSuffix(mimeType, "+xml") || strings.HasSuffix(mimeType, "+yaml")
	}
}

func isTextLikeExtension(extension string) bool {
	switch extension {
	case ".txt", ".md", ".markdown", ".json", ".jsonl", ".xml", ".yaml", ".yml", ".toml", ".csv", ".tsv", ".go", ".js", ".jsx", ".ts", ".tsx", ".vue", ".css", ".scss", ".sass", ".html", ".htm", ".py", ".rb", ".rs", ".java", ".c", ".h", ".cpp", ".hpp", ".cs", ".php", ".sh", ".ps1", ".sql", ".ini", ".cfg", ".conf", ".log":
		return true
	default:
		return false
	}
}

func isDocumentMimeType(mimeType string) bool {
	switch mimeType {
	case "application/pdf",
		"application/msword",
		"application/vnd.ms-excel",
		"application/vnd.ms-powerpoint",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/rtf":
		return true
	default:
		return false
	}
}

func isDocumentExtension(extension string) bool {
	switch extension {
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".rtf":
		return true
	default:
		return false
	}
}

func isArchiveMimeType(mimeType string) bool {
	switch mimeType {
	case "application/zip", "application/x-zip-compressed", "application/x-tar", "application/gzip", "application/x-gzip", "application/x-7z-compressed", "application/vnd.rar", "application/x-rar-compressed":
		return true
	default:
		return false
	}
}

func isArchiveExtension(extension string) bool {
	switch extension {
	case ".zip", ".tar", ".gz", ".tgz", ".7z", ".rar":
		return true
	default:
		return false
	}
}

func isDangerousBinaryExtension(extension string) bool {
	switch extension {
	case ".exe", ".dll", ".msi", ".com", ".scr":
		return true
	default:
		return false
	}
}

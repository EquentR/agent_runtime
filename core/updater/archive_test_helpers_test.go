package updater

import (
	"compress/gzip"
	"io"
	"testing"
)

func newGzipWriter(t *testing.T, writer io.Writer) *gzip.Writer {
	t.Helper()
	return gzip.NewWriter(writer)
}

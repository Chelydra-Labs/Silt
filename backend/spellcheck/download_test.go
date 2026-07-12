package spellcheck

import (
	"bytes"
	"compress/gzip"
	"testing"
)

func TestGunzip_RejectsOversized(t *testing.T) {
	// Build a gzip that expands past maxDomainFile.
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	chunk := bytes.Repeat([]byte("a"), 1024)
	written := 0
	for written < maxDomainFile+10 {
		n, _ := zw.Write(chunk)
		written += n
	}
	_ = zw.Close()
	_, err := gunzip(buf.Bytes())
	if err == nil {
		t.Fatal("expected oversized decompress error")
	}
}

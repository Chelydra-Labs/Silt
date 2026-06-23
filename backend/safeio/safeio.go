// Package safeio provides size-bounded readers for user-supplied files so a
// hostile synced or shared file cannot drive unbounded allocation before
// validation runs (audit F12). Every JSON/YAML decode of a user-controllable
// file routes its read through ReadFileMax.
package safeio

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ReadFileMax reads path into memory, failing if the file exceeds max bytes.
// A file of exactly max bytes is accepted; any byte over is a hard error.
// This caps the allocation that precedes a json/yaml Unmarshal of a user file
// (audit F12).
func ReadFileMax(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Pre-allocate exactly max+1 bytes and read in one shot. io.ReadAll would
	// grow a buffer from 512 B through successive doublings, temporarily
	// allocating ~2x the cap before returning — at odds with the F12 goal of
	// bounding memory. Reading max+1 bytes lets a file of exactly max pass
	// while any byte over proves the file exceeds the cap.
	buf := make([]byte, max+1)
	n, err := io.ReadFull(f, buf)
	// io.EOF (file is empty) and io.ErrUnexpectedEOF (file shorter than max+1)
	// both mean the file is within the cap. An empty file yields an empty
	// slice, matching os.ReadFile semantics so callers' empty-file handling
	// (e.g. an empty config.yaml decoding to defaults) is unchanged.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return buf[:n], nil
	}
	if err == nil {
		// All max+1 bytes were read: the file is at least one byte over.
		return nil, fmt.Errorf("%s exceeds the %d-byte cap; refusing to parse", path, max)
	}
	return nil, fmt.Errorf("read %s: %w", path, err)
}


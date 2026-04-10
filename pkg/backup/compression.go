// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package backup

import (
	"compress/gzip"
	"io"

	"github.com/klauspost/compress/zstd"
)

// Compressor provides compression functionality.
type Compressor interface {
	// Compress wraps a writer with compression.
	Compress(w io.Writer) (io.WriteCloser, error)

	// Decompress wraps a reader with decompression.
	Decompress(r io.Reader) (io.ReadCloser, error)
}

// NewCompressor creates a compressor for the given type.
func NewCompressor(ctype CompressionType) (Compressor, error) {
	switch ctype {
	case CompressionNone:
		return &noneCompressor{}, nil
	case CompressionGzip:
		return &gzipCompressor{}, nil
	case CompressionZstd:
		return &zstdCompressor{}, nil
	default:
		return nil, &UnsupportedCompressionError{Type: string(ctype)}
	}
}

// noneCompressor is a no-op compressor.
type noneCompressor struct{}

func (c *noneCompressor) Compress(w io.Writer) (io.WriteCloser, error) {
	return &nopWriteCloser{w}, nil
}

func (c *noneCompressor) Decompress(r io.Reader) (io.ReadCloser, error) {
	if rc, ok := r.(io.ReadCloser); ok {
		return rc, nil
	}
	return io.NopCloser(r), nil
}

// gzipCompressor implements gzip compression.
type gzipCompressor struct{}

func (c *gzipCompressor) Compress(w io.Writer) (io.WriteCloser, error) {
	return gzip.NewWriter(w), nil
}

func (c *gzipCompressor) Decompress(r io.Reader) (io.ReadCloser, error) {
	return gzip.NewReader(r)
}

// zstdCompressor implements Zstandard compression.
type zstdCompressor struct{}

func (c *zstdCompressor) Compress(w io.Writer) (io.WriteCloser, error) {
	return zstd.NewWriter(w)
}

func (c *zstdCompressor) Decompress(r io.Reader) (io.ReadCloser, error) {
	decoder, err := zstd.NewReader(r)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(decoder), nil
}

// nopWriteCloser wraps a writer to add a no-op Close method.
type nopWriteCloser struct {
	io.Writer
}

func (w *nopWriteCloser) Close() error {
	return nil
}

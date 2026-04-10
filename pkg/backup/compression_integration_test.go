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

//go:build integration
// +build integration

package backup

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompression_EmptyData tests actual compression/decompression with empty data
func TestCompression_EmptyData(t *testing.T) {
	tests := []struct {
		name  string
		ctype CompressionType
	}{
		{"none", CompressionNone},
		{"gzip", CompressionGzip},
		{"zstd", CompressionZstd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressor, err := NewCompressor(tt.ctype)
			require.NoError(t, err)

			// Compress empty data
			var compressed bytes.Buffer
			writer, err := compressor.Compress(&compressed)
			require.NoError(t, err)

			err = writer.Close()
			require.NoError(t, err)

			// Decompress
			reader, err := compressor.Decompress(&compressed)
			require.NoError(t, err)

			decompressed, err := io.ReadAll(reader)
			require.NoError(t, err)

			assert.Empty(t, decompressed)
		})
	}
}

// TestCompression_LargeData tests compression with 10MB of data
func TestCompression_LargeData(t *testing.T) {
	tests := []struct {
		name  string
		ctype CompressionType
	}{
		{"gzip", CompressionGzip},
		{"zstd", CompressionZstd},
	}

	// Create 10MB of test data
	largeData := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 400000) // ~10MB

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressor, err := NewCompressor(tt.ctype)
			require.NoError(t, err)

			// Compress
			var compressed bytes.Buffer
			writer, err := compressor.Compress(&compressed)
			require.NoError(t, err)

			_, err = writer.Write([]byte(largeData))
			require.NoError(t, err)

			err = writer.Close()
			require.NoError(t, err)

			// Verify compression ratio
			compressionRatio := float64(compressed.Len()) / float64(len(largeData))
			assert.Less(t, compressionRatio, 0.1, "Expected good compression ratio for repetitive data")

			// Decompress
			reader, err := compressor.Decompress(&compressed)
			require.NoError(t, err)

			decompressed, err := io.ReadAll(reader)
			require.NoError(t, err)

			assert.Equal(t, largeData, string(decompressed))
		})
	}
}

// TestCompression_BinaryData tests compression with binary data
func TestCompression_BinaryData(t *testing.T) {
	tests := []struct {
		name  string
		ctype CompressionType
	}{
		{"gzip", CompressionGzip},
		{"zstd", CompressionZstd},
	}

	// Create binary data with various byte values
	binaryData := make([]byte, 10000)
	for i := range binaryData {
		binaryData[i] = byte(i % 256)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressor, err := NewCompressor(tt.ctype)
			require.NoError(t, err)

			// Compress
			var compressed bytes.Buffer
			writer, err := compressor.Compress(&compressed)
			require.NoError(t, err)

			_, err = writer.Write(binaryData)
			require.NoError(t, err)

			err = writer.Close()
			require.NoError(t, err)

			// Decompress
			reader, err := compressor.Decompress(&compressed)
			require.NoError(t, err)

			decompressed, err := io.ReadAll(reader)
			require.NoError(t, err)

			assert.Equal(t, binaryData, decompressed)
		})
	}
}

// TestCompression_RandomData tests compression with pseudo-random data
func TestCompression_RandomData(t *testing.T) {
	tests := []struct {
		name  string
		ctype CompressionType
	}{
		{"gzip", CompressionGzip},
		{"zstd", CompressionZstd},
	}

	// Create pseudo-random data (not truly random for reproducibility)
	randomData := make([]byte, 10000)
	seed := uint32(12345)
	for i := range randomData {
		seed = seed*1103515245 + 12345
		randomData[i] = byte(seed >> 16)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressor, err := NewCompressor(tt.ctype)
			require.NoError(t, err)

			// Compress
			var compressed bytes.Buffer
			writer, err := compressor.Compress(&compressed)
			require.NoError(t, err)

			_, err = writer.Write(randomData)
			require.NoError(t, err)

			err = writer.Close()
			require.NoError(t, err)

			// Random data should have poor compression ratio
			compressionRatio := float64(compressed.Len()) / float64(len(randomData))
			assert.Greater(t, compressionRatio, 0.8, "Expected poor compression ratio for random data")

			// Decompress
			reader, err := compressor.Decompress(&compressed)
			require.NoError(t, err)

			decompressed, err := io.ReadAll(reader)
			require.NoError(t, err)

			assert.Equal(t, randomData, decompressed)
		})
	}
}

// TestCompression_MultipleWrites tests compression with multiple writes
func TestCompression_MultipleWrites(t *testing.T) {
	tests := []struct {
		name  string
		ctype CompressionType
	}{
		{"gzip", CompressionGzip},
		{"zstd", CompressionZstd},
	}

	chunks := []string{
		"First chunk of data ",
		"Second chunk of data ",
		"Third chunk of data",
	}
	expected := strings.Join(chunks, "")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressor, err := NewCompressor(tt.ctype)
			require.NoError(t, err)

			// Compress with multiple writes
			var compressed bytes.Buffer
			writer, err := compressor.Compress(&compressed)
			require.NoError(t, err)

			for _, chunk := range chunks {
				_, err = writer.Write([]byte(chunk))
				require.NoError(t, err)
			}

			err = writer.Close()
			require.NoError(t, err)

			// Decompress
			reader, err := compressor.Decompress(&compressed)
			require.NoError(t, err)

			decompressed, err := io.ReadAll(reader)
			require.NoError(t, err)

			assert.Equal(t, expected, string(decompressed))
		})
	}
}

// TestCompression_CorruptedData tests decompression with corrupted data
func TestCompression_CorruptedData(t *testing.T) {
	tests := []struct {
		name  string
		ctype CompressionType
	}{
		{"gzip", CompressionGzip},
		{"zstd", CompressionZstd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressor, err := NewCompressor(tt.ctype)
			require.NoError(t, err)

			// Create corrupted data
			corruptedData := bytes.NewReader([]byte("this is not valid compressed data"))

			// Attempt to decompress
			reader, err := compressor.Decompress(corruptedData)
			if err != nil {
				// Some compressors fail immediately
				assert.Error(t, err)
				return
			}

			// Others fail on read
			_, err = io.ReadAll(reader)
			assert.Error(t, err)
		})
	}
}

// TestCompression_ZeroLengthWrites tests compression with zero-length writes
func TestCompression_ZeroLengthWrites(t *testing.T) {
	tests := []struct {
		name  string
		ctype CompressionType
	}{
		{"gzip", CompressionGzip},
		{"zstd", CompressionZstd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressor, err := NewCompressor(tt.ctype)
			require.NoError(t, err)

			var compressed bytes.Buffer
			writer, err := compressor.Compress(&compressed)
			require.NoError(t, err)

			// Write zero-length data
			n, err := writer.Write([]byte{})
			require.NoError(t, err)
			assert.Equal(t, 0, n)

			// Write actual data
			data := []byte("test data")
			_, err = writer.Write(data)
			require.NoError(t, err)

			err = writer.Close()
			require.NoError(t, err)

			// Decompress
			reader, err := compressor.Decompress(&compressed)
			require.NoError(t, err)

			decompressed, err := io.ReadAll(reader)
			require.NoError(t, err)

			assert.Equal(t, data, decompressed)
		})
	}
}

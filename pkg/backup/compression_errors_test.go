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
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZstdCompressor_DecompressError(t *testing.T) {
	compressor := &zstdCompressor{}

	// Create corrupted data that's not valid zstd
	corruptedData := []byte("this is not valid zstd compressed data")

	reader, err := compressor.Decompress(bytes.NewReader(corruptedData))
	if err != nil {
		// Error returned immediately
		assert.Error(t, err)
		return
	}

	// Error might be returned on read
	_, err = io.ReadAll(reader)
	assert.Error(t, err)
}

func TestGzipCompressor_DecompressError(t *testing.T) {
	compressor := &gzipCompressor{}

	// Create corrupted data that's not valid gzip
	corruptedData := []byte("this is not valid gzip compressed data")

	reader, err := compressor.Decompress(bytes.NewReader(corruptedData))
	if err != nil {
		// Error returned immediately
		assert.Error(t, err)
		return
	}

	// Error might be returned on read
	_, err = io.ReadAll(reader)
	assert.Error(t, err)
}

func TestZstdCompressor_RoundTrip(t *testing.T) {
	compressor := &zstdCompressor{}

	testData := []byte("test data for zstd compression round trip")

	// Compress
	var compressed bytes.Buffer
	writer, err := compressor.Compress(&compressed)
	require.NoError(t, err)

	_, err = writer.Write(testData)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Decompress
	reader, err := compressor.Decompress(&compressed)
	require.NoError(t, err)

	decompressed, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, testData, decompressed)

	err = reader.Close()
	require.NoError(t, err)
}

func TestGzipCompressor_RoundTrip(t *testing.T) {
	compressor := &gzipCompressor{}

	testData := []byte("test data for gzip compression round trip")

	// Compress
	var compressed bytes.Buffer
	writer, err := compressor.Compress(&compressed)
	require.NoError(t, err)

	_, err = writer.Write(testData)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Decompress
	reader, err := compressor.Decompress(&compressed)
	require.NoError(t, err)

	decompressed, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, testData, decompressed)

	err = reader.Close()
	require.NoError(t, err)
}

func TestNopWriteCloser(t *testing.T) {
	var buf bytes.Buffer
	writer := &nopWriteCloser{&buf}

	data := []byte("test")
	n, err := writer.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)

	err = writer.Close()
	require.NoError(t, err)

	assert.Equal(t, data, buf.Bytes())
}

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

package grpc

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// VectorExportEntry represents a single vector for export/import.
type VectorExportEntry struct {
	ID        string            `json:"id"`
	Vector    []float32         `json:"vector"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Version   uint64            `json:"version,omitempty"`
	CreatedAt int64             `json:"created_at,omitempty"`
	UpdatedAt int64             `json:"updated_at,omitempty"`
}

// VectorExportBatch represents a batch of vectors for streaming export.
type VectorExportBatch struct {
	Vectors   []VectorExportEntry `json:"vectors"`
	Namespace string              `json:"namespace"`
	BatchNum  int32               `json:"batch_num"`
	Total     int64               `json:"total"`
}

// serializeExportBatch serializes a batch of vectors to the specified format.
func serializeExportBatch(batch *VectorExportBatch, format, compression string) ([]byte, error) {
	var data []byte
	var err error

	switch format {
	case "json", "":
		data, err = json.Marshal(batch)
	default:
		// Default to JSON for unsupported formats
		data, err = json.Marshal(batch)
	}

	if err != nil {
		return nil, err
	}

	// Apply compression
	switch compression {
	case "gzip":
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if _, err := gz.Write(data); err != nil {
			return nil, err
		}
		if err := gz.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "none", "":
		return data, nil
	default:
		return data, nil
	}
}

// deserializeExportBatch deserializes a batch of vectors from the specified format.
func deserializeExportBatch(data []byte, format, compression string) (*VectorExportBatch, error) {
	// Apply decompression
	switch compression {
	case "gzip":
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		data, err = io.ReadAll(gz)
		if err != nil {
			return nil, err
		}
	case "none", "":
		// No decompression needed
	}

	var batch VectorExportBatch

	switch format {
	case "json", "":
		if err := json.Unmarshal(data, &batch); err != nil {
			return nil, err
		}
	default:
		// Default to JSON
		if err := json.Unmarshal(data, &batch); err != nil {
			return nil, err
		}
	}

	return &batch, nil
}

// calculateChecksum calculates CRC32 checksum for data integrity.
func calculateChecksum(data []byte) uint32 {
	return crc32.ChecksumIEEE(data)
}

// vectorDataToExportEntries converts VectorData results to export entries.
func vectorDataToExportEntries(results []*service.VectorData) []VectorExportEntry {
	entries := make([]VectorExportEntry, 0, len(results))
	for _, r := range results {
		// Convert metadata to string map
		metadata := make(map[string]string)
		for k, v := range r.Metadata {
			if s, ok := v.(string); ok {
				metadata[k] = s
			} else {
				// Convert non-string values to string representation
				metadata[k] = fmt.Sprintf("%v", v)
			}
		}

		entry := VectorExportEntry{
			ID:        fmt.Sprintf("%d", r.ID),
			Vector:    r.Vector,
			Metadata:  metadata,
			Version:   r.Version,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		}
		entries = append(entries, entry)
	}
	return entries
}

// exportEntriesToVectorData converts export entries to VectorData for batch upsert.
func exportEntriesToVectorData(entries []VectorExportEntry) []*service.VectorData {
	vectors := make([]*service.VectorData, 0, len(entries))
	for _, e := range entries {
		// Parse ID back to uint64
		id := parseVectorID(e.ID)

		// Convert string metadata to interface map
		metadata := make(map[string]interface{})
		for k, v := range e.Metadata {
			metadata[k] = v
		}

		vectors = append(vectors, &service.VectorData{
			ID:       id,
			Vector:   e.Vector,
			Metadata: metadata,
		})
	}
	return vectors
}

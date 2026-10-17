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

package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"strconv"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Vector export wire format: one JSON-encoded VectorExportBatch per batch,
// optionally gzip-compressed, with a CRC32 (IEEE) checksum over the bytes on
// the wire. Every transport produces and consumes exactly this format.
const (
	// VectorExportFormatJSON is the only supported export format.
	VectorExportFormatJSON = "json"
	// VectorExportCompressionNone leaves batch data uncompressed.
	VectorExportCompressionNone = "none"
	// VectorExportCompressionGzip gzip-compresses batch data.
	VectorExportCompressionGzip = "gzip"
	// DefaultVectorExportBatchSize is the batch size used when none is given.
	DefaultVectorExportBatchSize = 1000
)

// VectorExportEntry represents a single vector in export format.
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

// ExportVectorsRequest selects the namespace and encoding of an export.
type ExportVectorsRequest struct {
	Namespace   string
	Format      string // "json" or "" (json)
	Compression string // "none", "gzip" or "" (none)
	BatchSize   int    // <= 0 selects DefaultVectorExportBatchSize
	ShardID     uint64
}

// ExportVectorsChunk is one encoded batch produced by ExportVectors.
type ExportVectorsChunk struct {
	Data            []byte
	VectorsExported int64
	BatchNumber     int32
	IsFinal         bool
	Checksum        uint32
}

// ImportVectorsRequest carries one encoded batch to import.
type ImportVectorsRequest struct {
	Namespace         string
	Format            string // "json" or "" (json)
	Compression       string // "none", "gzip" or "" (none)
	Data              []byte
	OverwriteExisting bool
	// VerifyChecksum makes ImportVectors refuse Data whose CRC32 differs
	// from Checksum.
	VerifyChecksum bool
	Checksum       uint32
	ShardID        uint64
}

// ImportVectorsResult counts the outcome of importing one batch.
type ImportVectorsResult struct {
	Imported int64
	Skipped  int64
	Failed   int64
}

// UnsupportedExportFormatError reports an export format other than json.
type UnsupportedExportFormatError struct {
	Format string
}

// Error implements error.
func (e *UnsupportedExportFormatError) Error() string {
	return "unsupported vector export format: " + e.Format
}

// UnsupportedExportCompressionError reports an unknown export compression.
type UnsupportedExportCompressionError struct {
	Compression string
}

// Error implements error.
func (e *UnsupportedExportCompressionError) Error() string {
	return "unsupported vector export compression: " + e.Compression
}

// ExportChecksumMismatchError reports batch data whose CRC32 does not match.
type ExportChecksumMismatchError struct {
	Expected uint32
	Actual   uint32
}

// Error implements error.
func (e *ExportChecksumMismatchError) Error() string {
	return "vector export checksum mismatch: expected " +
		strconv.FormatUint(uint64(e.Expected), 10) + ", got " +
		strconv.FormatUint(uint64(e.Actual), 10)
}

func invalidExportInput(op string, err error) error {
	return &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: op, Err: err}
}

// ExportVectors reads every vector of a namespace in batches and passes each
// encoded batch to send. An empty namespace yields one empty final chunk.
func ExportVectors(ctx context.Context, vs VectorService, req *ExportVectorsRequest, send func(*ExportVectorsChunk) error) error {
	const op = "vector.Export"
	if req == nil || req.Namespace == "" {
		return WrapError(ErrInvalidInput, dberrors.ErrInvalidInput, op, "namespace is required")
	}
	if err := validateExportEncoding(op, req.Format, req.Compression); err != nil {
		return err
	}
	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultVectorExportBatchSize
	}

	var totalExported int64
	var batchNumber int32
	offset := 0
	for {
		resp, err := vs.SearchByMetadata(ctx, &SearchByMetadataRequest{
			Namespace:       req.Namespace,
			Filters:         make(map[string]interface{}),
			FilterOperator:  "AND",
			Limit:           batchSize,
			Offset:          offset,
			IncludeVectors:  true,
			IncludeMetadata: true,
			ShardID:         req.ShardID,
			Consistency:     ConsistencyLevelLinearizable,
		})
		if err != nil {
			return err
		}
		if len(resp.Results) == 0 {
			if batchNumber == 0 {
				return send(&ExportVectorsChunk{Data: []byte{}, BatchNumber: 1, IsFinal: true})
			}
			return nil
		}

		batchNumber++
		entries := vectorDataToExportEntries(resp.Results)
		data, err := encodeExportBatch(&VectorExportBatch{
			Vectors:   entries,
			Namespace: req.Namespace,
			BatchNum:  batchNumber,
			Total:     totalExported + int64(len(entries)),
		}, req.Compression)
		if err != nil {
			return WrapError(err, dberrors.ErrInternal, op, "encode export batch")
		}

		isFinal := len(resp.Results) < batchSize
		if err := send(&ExportVectorsChunk{
			Data:            data,
			VectorsExported: int64(len(entries)),
			BatchNumber:     batchNumber,
			IsFinal:         isFinal,
			Checksum:        crc32.ChecksumIEEE(data),
		}); err != nil {
			return err
		}
		if isFinal {
			return nil
		}
		totalExported += int64(len(entries))
		offset += len(resp.Results)
	}
}

// ImportVectors decodes one batch and upserts its vectors. Unless
// OverwriteExisting is set, vectors that already exist are skipped. The
// result is never nil: an undecodable batch counts as one failure and a
// failed upsert counts every vector it carried.
func ImportVectors(ctx context.Context, vs VectorService, req *ImportVectorsRequest) (*ImportVectorsResult, error) {
	const op = "vector.Import"
	result := &ImportVectorsResult{}
	if req == nil || req.Namespace == "" {
		return result, WrapError(ErrInvalidInput, dberrors.ErrInvalidInput, op, "namespace is required")
	}
	if len(req.Data) == 0 {
		return result, nil
	}
	batch, err := decodeImportBatch(op, req)
	if err != nil {
		result.Failed++
		return result, err
	}

	vectors := exportEntriesToVectorData(batch.Vectors)
	if !req.OverwriteExisting {
		fresh := make([]*VectorData, 0, len(vectors))
		for _, vec := range vectors {
			existing, err := vs.GetVector(ctx, &GetVectorRequest{Namespace: req.Namespace, ID: vec.ID, ShardID: req.ShardID})
			if err == nil && existing.Found {
				result.Skipped++
				continue
			}
			fresh = append(fresh, vec)
		}
		vectors = fresh
	}
	if len(vectors) == 0 {
		return result, nil
	}

	resp, err := vs.BatchUpsert(ctx, &BatchUpsertRequest{Namespace: req.Namespace, Vectors: vectors, ShardID: req.ShardID})
	if err != nil {
		result.Failed += int64(len(vectors))
		return result, err
	}
	result.Imported = int64(resp.InsertedCount)
	return result, nil
}

// ParseVectorID converts a wire vector ID to its numeric form: decimal IDs
// parse directly, any other string is hashed.
func ParseVectorID(id string) uint64 {
	if parsed, err := strconv.ParseUint(id, 10, 64); err == nil {
		return parsed
	}
	hash := uint64(0)
	for i := 0; i < len(id); i++ {
		hash = hash*31 + uint64(id[i])
	}
	return hash
}

func validateExportEncoding(op, format, compression string) error {
	if format != "" && format != VectorExportFormatJSON {
		return invalidExportInput(op, &UnsupportedExportFormatError{Format: format})
	}
	switch compression {
	case "", VectorExportCompressionNone, VectorExportCompressionGzip:
		return nil
	default:
		return invalidExportInput(op, &UnsupportedExportCompressionError{Compression: compression})
	}
}

func decodeImportBatch(op string, req *ImportVectorsRequest) (*VectorExportBatch, error) {
	if err := validateExportEncoding(op, req.Format, req.Compression); err != nil {
		return nil, err
	}
	if req.VerifyChecksum {
		if actual := crc32.ChecksumIEEE(req.Data); actual != req.Checksum {
			return nil, invalidExportInput(op, &ExportChecksumMismatchError{Expected: req.Checksum, Actual: actual})
		}
	}
	batch, err := decodeExportBatch(req.Data, req.Compression)
	if err != nil {
		return nil, WrapError(err, dberrors.ErrInvalidInput, op, "decode export batch")
	}
	return batch, nil
}

// encodeExportBatch serializes a batch to JSON and applies compression.
func encodeExportBatch(batch *VectorExportBatch, compression string) ([]byte, error) {
	data, err := json.Marshal(batch)
	if err != nil {
		return nil, err
	}
	if compression != VectorExportCompressionGzip {
		return data, nil
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeExportBatch reverses encodeExportBatch.
func decodeExportBatch(data []byte, compression string) (*VectorExportBatch, error) {
	if compression == VectorExportCompressionGzip {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		data, err = io.ReadAll(gz)
		if err != nil {
			return nil, err
		}
	}
	var batch VectorExportBatch
	if err := json.Unmarshal(data, &batch); err != nil {
		return nil, err
	}
	return &batch, nil
}

// vectorDataToExportEntries converts stored vectors to export entries.
func vectorDataToExportEntries(results []*VectorData) []VectorExportEntry {
	entries := make([]VectorExportEntry, 0, len(results))
	for _, r := range results {
		metadata := make(map[string]string, len(r.Metadata))
		for k, v := range r.Metadata {
			if s, ok := v.(string); ok {
				metadata[k] = s
			} else {
				metadata[k] = fmt.Sprint(v)
			}
		}
		entries = append(entries, VectorExportEntry{
			ID:        strconv.FormatUint(r.ID, 10),
			Vector:    r.Vector,
			Metadata:  metadata,
			Version:   r.Version,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		})
	}
	return entries
}

// exportEntriesToVectorData converts export entries to vectors for upsert.
func exportEntriesToVectorData(entries []VectorExportEntry) []*VectorData {
	vectors := make([]*VectorData, 0, len(entries))
	for _, e := range entries {
		metadata := make(map[string]interface{}, len(e.Metadata))
		for k, v := range e.Metadata {
			metadata[k] = v
		}
		vectors = append(vectors, &VectorData{
			ID:       ParseVectorID(e.ID),
			Vector:   e.Vector,
			Metadata: metadata,
		})
	}
	return vectors
}

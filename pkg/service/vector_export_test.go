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
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryVectorStore is an in-memory double serving the three VectorService
// verbs export and import use; the embedded interface panics on any other.
type memoryVectorStore struct {
	VectorService
	vectors   map[string]map[uint64]*VectorData
	upsertErr error
	// missingIsError makes GetVector report a missing vector as ErrNotFound
	// instead of a response with Found false; both mean "absent".
	missingIsError bool
}

func newMemoryVectorStore() *memoryVectorStore {
	return &memoryVectorStore{vectors: make(map[string]map[uint64]*VectorData)}
}

func (m *memoryVectorStore) SearchByMetadata(_ context.Context, req *SearchByMetadataRequest) (*SearchByMetadataResponse, error) {
	ids := make([]uint64, 0, len(m.vectors[req.Namespace]))
	for id := range m.vectors[req.Namespace] {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	resp := &SearchByMetadataResponse{}
	for i := req.Offset; i < len(ids) && len(resp.Results) < req.Limit; i++ {
		resp.Results = append(resp.Results, m.vectors[req.Namespace][ids[i]])
	}
	return resp, nil
}

func (m *memoryVectorStore) GetVector(_ context.Context, req *GetVectorRequest) (*GetVectorResponse, error) {
	if v, ok := m.vectors[req.Namespace][req.ID]; ok {
		return &GetVectorResponse{ID: v.ID, Vector: v.Vector, Found: true}, nil
	}
	if m.missingIsError {
		return nil, ErrNotFound
	}
	return &GetVectorResponse{ID: req.ID}, nil
}

func (m *memoryVectorStore) BatchUpsert(_ context.Context, req *BatchUpsertRequest) (*BatchUpsertResponse, error) {
	if m.upsertErr != nil {
		return nil, m.upsertErr
	}
	if m.vectors[req.Namespace] == nil {
		m.vectors[req.Namespace] = make(map[uint64]*VectorData)
	}
	for _, v := range req.Vectors {
		m.vectors[req.Namespace][v.ID] = v
	}
	return &BatchUpsertResponse{InsertedCount: len(req.Vectors)}, nil
}

func seededStore(n int) *memoryVectorStore {
	store := newMemoryVectorStore()
	store.vectors["src"] = make(map[uint64]*VectorData)
	for i := 1; i <= n; i++ {
		store.vectors["src"][uint64(i)] = &VectorData{
			ID:       uint64(i),
			Vector:   []float32{float32(i), float32(i) * 2},
			Metadata: map[string]interface{}{"label": "v", "rank": i},
		}
	}
	return store
}

func exportAll(t *testing.T, store *memoryVectorStore, req *ExportVectorsRequest) []*ExportVectorsChunk {
	t.Helper()
	var chunks []*ExportVectorsChunk
	require.NoError(t, ExportVectors(context.Background(), store, req, func(c *ExportVectorsChunk) error {
		chunks = append(chunks, c)
		return nil
	}))
	return chunks
}

func TestExportImportVectors_RoundTrip(t *testing.T) {
	for _, compression := range []string{"", VectorExportCompressionNone, VectorExportCompressionGzip} {
		t.Run("compression="+compression, func(t *testing.T) {
			store := seededStore(5)
			chunks := exportAll(t, store, &ExportVectorsRequest{Namespace: "src", Compression: compression, BatchSize: 2})
			require.Len(t, chunks, 3)
			assert.True(t, chunks[2].IsFinal)

			for _, c := range chunks {
				res, err := ImportVectors(context.Background(), store, &ImportVectorsRequest{
					Namespace: "dst", Compression: compression, Data: c.Data,
					VerifyChecksum: true, Checksum: c.Checksum,
				})
				require.NoError(t, err)
				assert.Equal(t, c.VectorsExported, res.Imported)
			}

			require.Len(t, store.vectors["dst"], 5)
			got := store.vectors["dst"][3]
			assert.Equal(t, []float32{3, 6}, got.Vector)
			assert.Equal(t, map[string]interface{}{"label": "v", "rank": "3"}, got.Metadata)
		})
	}
}

func TestExportVectors_EmptyNamespaceSendsOneFinalChunk(t *testing.T) {
	chunks := exportAll(t, newMemoryVectorStore(), &ExportVectorsRequest{Namespace: "none"})
	require.Len(t, chunks, 1)
	assert.True(t, chunks[0].IsFinal)
	assert.Empty(t, chunks[0].Data)
	assert.Equal(t, int32(1), chunks[0].BatchNumber)
}

func TestExportVectors_RefusesBadRequests(t *testing.T) {
	send := func(*ExportVectorsChunk) error { return nil }
	ctx := context.Background()
	store := seededStore(1)

	assert.True(t, IsInvalidInput(ExportVectors(ctx, store, nil, send)))
	assert.True(t, IsInvalidInput(ExportVectors(ctx, store, &ExportVectorsRequest{}, send)))

	err := ExportVectors(ctx, store, &ExportVectorsRequest{Namespace: "src", Format: "parquet"}, send)
	var formatErr *UnsupportedExportFormatError
	require.ErrorAs(t, err, &formatErr)
	assert.True(t, IsInvalidInput(err))

	err = ExportVectors(ctx, store, &ExportVectorsRequest{Namespace: "src", Compression: "zstd"}, send)
	var compressionErr *UnsupportedExportCompressionError
	require.ErrorAs(t, err, &compressionErr)
	assert.True(t, IsInvalidInput(err))
}

func TestImportVectors_RefusesCorruptedChecksum(t *testing.T) {
	store := seededStore(2)
	chunks := exportAll(t, store, &ExportVectorsRequest{Namespace: "src"})

	res, err := ImportVectors(context.Background(), store, &ImportVectorsRequest{
		Namespace: "dst", Data: chunks[0].Data, VerifyChecksum: true, Checksum: chunks[0].Checksum + 1,
	})
	var mismatch *ExportChecksumMismatchError
	require.ErrorAs(t, err, &mismatch)
	assert.True(t, IsInvalidInput(err))
	assert.Equal(t, int64(1), res.Failed)
	assert.Empty(t, store.vectors["dst"])
}

func TestImportVectors_RefusesUnknownEncodingAndBadData(t *testing.T) {
	ctx := context.Background()
	store := newMemoryVectorStore()
	tests := []struct {
		name string
		req  *ImportVectorsRequest
	}{
		{"unknown format", &ImportVectorsRequest{Namespace: "dst", Format: "parquet", Data: []byte("{}")}},
		{"unknown compression", &ImportVectorsRequest{Namespace: "dst", Compression: "zstd", Data: []byte("{}")}},
		{"invalid json", &ImportVectorsRequest{Namespace: "dst", Data: []byte("{invalid")}},
		{"invalid gzip", &ImportVectorsRequest{Namespace: "dst", Compression: "gzip", Data: []byte("not gzip")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := ImportVectors(ctx, store, tt.req)
			assert.True(t, IsInvalidInput(err))
			assert.Equal(t, int64(1), res.Failed)
		})
	}

	_, err := ImportVectors(ctx, store, &ImportVectorsRequest{Data: []byte("{}")})
	assert.True(t, IsInvalidInput(err))
}

func TestImportVectors_SkipsExistingUnlessOverwrite(t *testing.T) {
	store := seededStore(3)
	chunks := exportAll(t, store, &ExportVectorsRequest{Namespace: "src"})

	res, err := ImportVectors(context.Background(), store, &ImportVectorsRequest{Namespace: "src", Data: chunks[0].Data})
	require.NoError(t, err)
	assert.Equal(t, ImportVectorsResult{Skipped: 3}, *res)

	res, err = ImportVectors(context.Background(), store, &ImportVectorsRequest{Namespace: "src", Data: chunks[0].Data, OverwriteExisting: true})
	require.NoError(t, err)
	assert.Equal(t, ImportVectorsResult{Imported: 3}, *res)
}

func TestImportVectors_ImportsAbsentVectors(t *testing.T) {
	for _, missingIsError := range []bool{false, true} {
		store := seededStore(3)
		store.missingIsError = missingIsError
		chunks := exportAll(t, store, &ExportVectorsRequest{Namespace: "src"})

		res, err := ImportVectors(context.Background(), store, &ImportVectorsRequest{Namespace: "dst", Data: chunks[0].Data})
		require.NoError(t, err)
		assert.Equal(t, ImportVectorsResult{Imported: 3}, *res, "missingIsError=%v", missingIsError)
	}
}

func TestImportVectors_CountsFailedUpsert(t *testing.T) {
	store := seededStore(2)
	chunks := exportAll(t, store, &ExportVectorsRequest{Namespace: "src"})
	store.upsertErr = ErrUnavailable

	res, err := ImportVectors(context.Background(), store, &ImportVectorsRequest{Namespace: "dst", Data: chunks[0].Data})
	assert.Equal(t, ErrUnavailable, err)
	assert.Equal(t, int64(2), res.Failed)
}

func TestParseVectorID(t *testing.T) {
	assert.Equal(t, uint64(42), ParseVectorID("42"))
	assert.Equal(t, ParseVectorID("doc-a"), ParseVectorID("doc-a"))
	assert.NotEqual(t, ParseVectorID("doc-a"), ParseVectorID("doc-b"))
}

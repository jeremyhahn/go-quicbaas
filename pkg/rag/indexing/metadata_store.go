//go:build rag

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

package indexing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

const keyPrefix = "idx:"

// FileRecord tracks the indexing state of a single file.
type FileRecord struct {
	// Namespace scopes the record to a project or tenant.
	Namespace string `json:"namespace"`

	// FilePath is the absolute or project-relative path to the file.
	FilePath string `json:"file_path"`

	// Checksum is the SHA-256 hex digest of the file content.
	Checksum string `json:"checksum"`

	// ChunkIDs lists the vector IDs produced from this file.
	ChunkIDs []string `json:"chunk_ids"`

	// IndexedAt is the Unix timestamp (seconds) when the file was last indexed.
	IndexedAt int64 `json:"indexed_at"`

	// Size is the file size in bytes at the time of indexing.
	Size int64 `json:"size"`

	// Language is the detected programming language name.
	Language string `json:"language"`
}

// MetadataStore provides CRUD operations for file indexing records.
type MetadataStore interface {
	// GetFileRecord retrieves a file record by namespace and file path.
	GetFileRecord(ctx context.Context, namespace, filePath string) (*FileRecord, error)

	// PutFileRecord stores or updates a file record.
	PutFileRecord(ctx context.Context, record *FileRecord) error

	// DeleteFileRecord removes a file record by namespace and file path.
	DeleteFileRecord(ctx context.Context, namespace, filePath string) error

	// ListFileRecords returns all file records for a namespace.
	ListFileRecords(ctx context.Context, namespace string) ([]*FileRecord, error)
}

// kvMetadataStore implements MetadataStore backed by a KVStore.
type kvMetadataStore struct {
	kv interfaces.KVStore
}

// NewMetadataStore creates a MetadataStore backed by the provided KVStore.
func NewMetadataStore(kv interfaces.KVStore) MetadataStore {
	return &kvMetadataStore{kv: kv}
}

// buildKey constructs the KV key: idx:{namespace}:{sha256(filePath)}.
func buildKey(namespace, filePath string) string {
	hash := sha256.Sum256([]byte(filePath))
	return keyPrefix + namespace + ":" + hex.EncodeToString(hash[:])
}

// GetFileRecord retrieves a file record from the KV store.
func (s *kvMetadataStore) GetFileRecord(ctx context.Context, namespace, filePath string) (*FileRecord, error) {
	key := buildKey(namespace, filePath)
	data, err := s.kv.Get(ctx, key)
	if err != nil {
		return nil, &MetadataStoreError{
			Operation: "get",
			Key:       key,
			Cause:     err,
		}
	}
	if data == nil {
		return nil, nil
	}

	var record FileRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, &MetadataStoreError{
			Operation: "get",
			Key:       key,
			Cause:     err,
		}
	}
	return &record, nil
}

// PutFileRecord stores a file record in the KV store.
func (s *kvMetadataStore) PutFileRecord(ctx context.Context, record *FileRecord) error {
	key := buildKey(record.Namespace, record.FilePath)
	data, err := json.Marshal(record)
	if err != nil {
		return &MetadataStoreError{
			Operation: "put",
			Key:       key,
			Cause:     err,
		}
	}
	if err := s.kv.Put(ctx, key, data); err != nil {
		return &MetadataStoreError{
			Operation: "put",
			Key:       key,
			Cause:     err,
		}
	}
	return nil
}

// DeleteFileRecord removes a file record from the KV store.
func (s *kvMetadataStore) DeleteFileRecord(ctx context.Context, namespace, filePath string) error {
	key := buildKey(namespace, filePath)
	if err := s.kv.Delete(ctx, key); err != nil {
		return &MetadataStoreError{
			Operation: "delete",
			Key:       key,
			Cause:     err,
		}
	}
	return nil
}

// ListFileRecords returns all file records for a given namespace by scanning
// the KV store with the namespace prefix.
func (s *kvMetadataStore) ListFileRecords(ctx context.Context, namespace string) ([]*FileRecord, error) {
	prefix := keyPrefix + namespace + ":"
	var records []*FileRecord

	err := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var record FileRecord
		if err := json.Unmarshal(value, &record); err != nil {
			return &MetadataStoreError{
				Operation: "list",
				Key:       key,
				Cause:     err,
			}
		}
		records = append(records, &record)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return records, nil
}

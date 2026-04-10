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

//go:build vector

package server

import (
	"context"
	"fmt"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// vectorStorageAdapter wraps a qrdb PebbleVectorStorage to satisfy the
// quicbaas interfaces.VectorStorage interface. It converts between the
// qrdb vector types and the quicbaas vector types.
type vectorStorageAdapter struct {
	storage *xkmssdk.QRDBPebbleVectorStorage
}

// newVectorStorageAdapter creates a new adapter around the qrdb storage.
func newVectorStorageAdapter(storage *xkmssdk.QRDBPebbleVectorStorage) interfaces.VectorStorage {
	return &vectorStorageAdapter{storage: storage}
}

// CreateNamespace creates a new vector namespace in the underlying storage.
func (a *vectorStorageAdapter) CreateNamespace(ctx context.Context, info *interfaces.VectorNamespaceInfo) error {
	now := time.Now().UnixNano()
	qrdbInfo := &xkmssdk.QRDBVectorNamespaceInfo{
		Name:           info.Name,
		Dimension:      info.Dimension,
		DistanceMetric: info.DistanceMetric,
		IndexType:      info.IndexType,
		IndexConfig:    info.IndexConfig,
		Metadata:       info.Metadata,
		TenantID:       info.TenantID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return a.storage.CreateNamespace(ctx, qrdbInfo)
}

// DeleteNamespace removes a namespace and all its vectors from the underlying storage.
func (a *vectorStorageAdapter) DeleteNamespace(ctx context.Context, namespace string) error {
	return a.storage.DeleteNamespace(ctx, namespace)
}

// ListNamespaces returns all available vector namespaces.
func (a *vectorStorageAdapter) ListNamespaces(ctx context.Context) ([]interfaces.VectorNamespaceInfo, error) {
	qrdbInfos, err := a.storage.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]interfaces.VectorNamespaceInfo, len(qrdbInfos))
	for i, info := range qrdbInfos {
		result[i] = convertNamespaceInfo(&info)
	}
	return result, nil
}

// GetNamespaceInfo retrieves metadata about a specific namespace.
func (a *vectorStorageAdapter) GetNamespaceInfo(ctx context.Context, namespace string) (*interfaces.VectorNamespaceInfo, error) {
	info, err := a.storage.GetNamespaceInfo(ctx, namespace)
	if err != nil {
		return nil, err
	}
	result := convertNamespaceInfo(info)
	return &result, nil
}

// UpsertVector inserts or updates a single vector in the underlying storage.
func (a *vectorStorageAdapter) UpsertVector(ctx context.Context, namespace string, vec *interfaces.VectorValue) error {
	qrdbVec := &xkmssdk.QRDBVectorValue{
		ID:        vec.ID,
		Embedding: vec.Embedding,
		Metadata:  vec.Metadata,
		Version:   vec.Version,
		CreatedAt: vec.CreatedAt,
		UpdatedAt: vec.UpdatedAt,
	}
	return a.storage.UpsertVector(ctx, namespace, qrdbVec)
}

// GetVector retrieves a vector by namespace and ID.
func (a *vectorStorageAdapter) GetVector(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error) {
	vec, err := a.storage.GetVector(ctx, namespace, id)
	if err != nil {
		return nil, err
	}
	return convertVectorValue(vec), nil
}

// DeleteVector removes a single vector by namespace and ID.
func (a *vectorStorageAdapter) DeleteVector(ctx context.Context, namespace string, id uint64) error {
	return a.storage.DeleteVector(ctx, namespace, id)
}

// BatchUpsertVectors inserts or updates multiple vectors atomically.
func (a *vectorStorageAdapter) BatchUpsertVectors(ctx context.Context, namespace string, vecs []*interfaces.VectorValue) error {
	qrdbVecs := make([]*xkmssdk.QRDBVectorValue, len(vecs))
	for i, vec := range vecs {
		qrdbVecs[i] = &xkmssdk.QRDBVectorValue{
			ID:        vec.ID,
			Embedding: vec.Embedding,
			Metadata:  vec.Metadata,
			Version:   vec.Version,
			CreatedAt: vec.CreatedAt,
			UpdatedAt: vec.UpdatedAt,
		}
	}
	return a.storage.BatchUpsertVectors(ctx, namespace, qrdbVecs)
}

// ScanNamespace lists vectors in a namespace with optional pagination.
func (a *vectorStorageAdapter) ScanNamespace(ctx context.Context, namespace string, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
	qrdbOpts := convertScanOptions(opts)
	vecs, err := a.storage.ScanNamespace(ctx, namespace, qrdbOpts)
	if err != nil {
		return nil, err
	}
	return convertVectorValues(vecs), nil
}

// ScanByMultipleFilters scans vectors matching multiple metadata filters.
func (a *vectorStorageAdapter) ScanByMultipleFilters(ctx context.Context, namespace string, filters []interfaces.VectorMetadataFilter, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
	qrdbFilters := make([]xkmssdk.QRDBVectorMetadataFilter, len(filters))
	for i, f := range filters {
		qrdbFilters[i] = xkmssdk.QRDBVectorMetadataFilter{
			Field:    f.Field,
			Value:    fmt.Sprintf("%v", f.Value),
			Operator: xkmssdk.QRDBVectorFilterOperator(f.Operator),
		}
	}
	qrdbOpts := convertScanOptions(opts)
	vecs, err := a.storage.ScanByMultipleFilters(ctx, namespace, qrdbFilters, qrdbOpts)
	if err != nil {
		return nil, err
	}
	return convertVectorValues(vecs), nil
}

// BatchDeleteVectors deletes multiple vectors by IDs.
func (a *vectorStorageAdapter) BatchDeleteVectors(ctx context.Context, namespace string, ids []uint64) error {
	return a.storage.BatchDeleteVectors(ctx, namespace, ids)
}

// GetStats returns storage statistics for a namespace.
func (a *vectorStorageAdapter) GetStats(ctx context.Context, namespace string) (*interfaces.VectorStorageStats, error) {
	stats, err := a.storage.GetStats(ctx, namespace)
	if err != nil {
		return nil, err
	}
	return &interfaces.VectorStorageStats{
		VectorCount: stats.Count,
		SizeBytes:   stats.SizeBytes,
	}, nil
}

// Compact triggers compaction for a namespace.
func (a *vectorStorageAdapter) Compact(ctx context.Context, namespace string) error {
	return a.storage.Compact(ctx, namespace)
}

// Close releases storage resources.
func (a *vectorStorageAdapter) Close() error {
	return a.storage.Close()
}

// convertNamespaceInfo converts a qrdb NamespaceInfo to a quicbaas VectorNamespaceInfo.
func convertNamespaceInfo(info *xkmssdk.QRDBVectorNamespaceInfo) interfaces.VectorNamespaceInfo {
	return interfaces.VectorNamespaceInfo{
		Name:           info.Name,
		Dimension:      info.Dimension,
		VectorCount:    info.VectorCount,
		DistanceMetric: info.DistanceMetric,
		IndexType:      info.IndexType,
		IndexConfig:    info.IndexConfig,
		Metadata:       info.Metadata,
		TenantID:       info.TenantID,
		StorageBytes:   uint64(info.StorageBytes),
		CreatedAt:      info.CreatedAt,
		UpdatedAt:      info.UpdatedAt,
	}
}

// convertVectorValue converts a qrdb VectorValue to a quicbaas VectorValue.
func convertVectorValue(vec *xkmssdk.QRDBVectorValue) *interfaces.VectorValue {
	if vec == nil {
		return nil
	}
	return &interfaces.VectorValue{
		ID:        vec.ID,
		Embedding: vec.Embedding,
		Metadata:  vec.Metadata,
		Version:   vec.Version,
		CreatedAt: vec.CreatedAt,
		UpdatedAt: vec.UpdatedAt,
	}
}

// convertVectorValues converts a slice of qrdb VectorValues to quicbaas VectorValues.
func convertVectorValues(vecs []*xkmssdk.QRDBVectorValue) []*interfaces.VectorValue {
	result := make([]*interfaces.VectorValue, len(vecs))
	for i, vec := range vecs {
		result[i] = convertVectorValue(vec)
	}
	return result
}

// convertScanOptions converts quicbaas VectorScanOptions to qrdb ScanOptions.
func convertScanOptions(opts *interfaces.VectorScanOptions) *xkmssdk.QRDBVectorScanOptions {
	if opts == nil {
		return nil
	}
	return &xkmssdk.QRDBVectorScanOptions{
		Limit:  opts.Limit,
		Offset: opts.Offset,
	}
}

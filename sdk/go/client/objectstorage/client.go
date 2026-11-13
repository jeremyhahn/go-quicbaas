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

package objectstorage

import (
	"context"
	stderrors "errors"
	"io"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.ObjectStorage"

	// defaultChunkSize is the default size for streaming chunks (64KB).
	defaultChunkSize = 64 * 1024
)

// Client provides object storage operations.
type Client interface {
	// Object CRUD Operations

	// PutObject stores an object with specified metadata and policies.
	PutObject(ctx context.Context, req *PutObjectRequest) (*PutObjectResponse, error)

	// GetObject retrieves an object by key.
	GetObject(ctx context.Context, req *GetObjectRequest) (*GetObjectResponse, error)

	// DeleteObject removes an object from storage.
	DeleteObject(ctx context.Context, req *DeleteObjectRequest) (*DeleteObjectResponse, error)

	// HeadObject retrieves object metadata without the data.
	HeadObject(ctx context.Context, req *HeadObjectRequest) (*HeadObjectResponse, error)

	// Listing Operations

	// ListObjects lists objects matching the specified criteria.
	ListObjects(ctx context.Context, req *ListObjectsRequest) (*ListObjectsResponse, error)

	// ListByTag lists objects with a specific tag.
	ListByTag(ctx context.Context, req *ListByTagRequest) (*ListByTagResponse, error)

	// ListByTenant lists objects owned by a specific tenant.
	ListByTenant(ctx context.Context, req *ListByTenantRequest) (*ListByTenantResponse, error)

	// Copy Operations

	// CopyObject copies an object to a new location.
	CopyObject(ctx context.Context, req *CopyObjectRequest) (*CopyObjectResponse, error)

	// Replication & Migration Operations

	// ReplicateObject replicates an object to target backends.
	ReplicateObject(ctx context.Context, req *ReplicateObjectRequest) (*ReplicateObjectResponse, error)

	// MigrateObject migrates an object to a new backend.
	MigrateObject(ctx context.Context, req *MigrateObjectRequest) (*MigrateObjectResponse, error)

	// Archive & Restore Operations

	// ArchiveObject moves an object to archive storage.
	ArchiveObject(ctx context.Context, req *ArchiveObjectRequest) (*ArchiveObjectResponse, error)

	// RestoreObject restores an archived object to active storage.
	RestoreObject(ctx context.Context, req *RestoreObjectRequest) (*RestoreObjectResponse, error)

	// Verification Operations

	// VerifyObject verifies object integrity across storage backends.
	VerifyObject(ctx context.Context, req *VerifyObjectRequest) (*VerifyObjectResponse, error)

	// Policy Management Operations

	// SetObjectPolicy sets policies for an object.
	SetObjectPolicy(ctx context.Context, req *SetObjectPolicyRequest) (*SetObjectPolicyResponse, error)

	// GetObjectPolicy retrieves policies for an object.
	GetObjectPolicy(ctx context.Context, req *GetObjectPolicyRequest) (*GetObjectPolicyResponse, error)

	// Backend Management Operations

	// ListBackends returns available storage backends.
	ListBackends(ctx context.Context, req *ListBackendsRequest) (*ListBackendsResponse, error)

	// GetBackendHealth returns health status for a backend.
	GetBackendHealth(ctx context.Context, req *GetBackendHealthRequest) (*GetBackendHealthResponse, error)

	// Tenant Management Operations

	// GetTenantUsage returns storage usage for a tenant.
	GetTenantUsage(ctx context.Context, req *GetTenantUsageRequest) (*GetTenantUsageResponse, error)

	// GetTenantQuotas returns quota settings for a tenant.
	GetTenantQuotas(ctx context.Context, req *GetTenantQuotasRequest) (*GetTenantQuotasResponse, error)

	// SetTenantQuotas sets quota settings for a tenant.
	SetTenantQuotas(ctx context.Context, req *SetTenantQuotasRequest) (*SetTenantQuotasResponse, error)

	// Batch Operations

	// BatchPutObjects stores multiple objects in a single request.
	BatchPutObjects(ctx context.Context, req *BatchPutObjectsRequest) (*BatchPutObjectsResponse, error)

	// BatchGetObjects retrieves multiple objects in a single request.
	BatchGetObjects(ctx context.Context, req *BatchGetObjectsRequest) (*BatchGetObjectsResponse, error)

	// BatchDeleteObjects deletes multiple objects in a single request.
	BatchDeleteObjects(ctx context.Context, req *BatchDeleteObjectsRequest) (*BatchDeleteObjectsResponse, error)

	// Streaming Operations

	// StreamPutObject creates a streaming writer for uploading large objects.
	StreamPutObject(ctx context.Context, opts StreamPutOptions) (StreamPutWriter, error)

	// StreamGetObject creates a streaming reader for downloading large objects.
	StreamGetObject(ctx context.Context, req *GetObjectRequest) (StreamGetReader, error)
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new Object Storage client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// =============================================================================
// Object CRUD Operations
// =============================================================================

// PutObject stores an object with specified metadata and policies.
func (c *client) PutObject(ctx context.Context, req *PutObjectRequest) (*PutObjectResponse, error) {
	if err := validatePutObjectRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "PutObject", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorage.PutObject", "nil response from server")
	}

	putResp, ok := resp.(*PutObjectResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.PutObject", "invalid response type")
	}

	return putResp, nil
}

// GetObject retrieves an object by key.
func (c *client) GetObject(ctx context.Context, req *GetObjectRequest) (*GetObjectResponse, error) {
	if err := validateGetObjectRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetObject", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("objectstorage.GetObject", "object not found")
	}

	getResp, ok := resp.(*GetObjectResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.GetObject", "invalid response type")
	}

	return getResp, nil
}

// DeleteObject removes an object from storage.
func (c *client) DeleteObject(ctx context.Context, req *DeleteObjectRequest) (*DeleteObjectResponse, error) {
	if err := validateDeleteObjectRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteObject", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorage.DeleteObject", "nil response from server")
	}

	deleteResp, ok := resp.(*DeleteObjectResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.DeleteObject", "invalid response type")
	}

	return deleteResp, nil
}

// HeadObject retrieves object metadata without the data.
func (c *client) HeadObject(ctx context.Context, req *HeadObjectRequest) (*HeadObjectResponse, error) {
	if err := validateHeadObjectRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "HeadObject", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &HeadObjectResponse{Key: req.Key, Exists: false}, nil
	}

	headResp, ok := resp.(*HeadObjectResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.HeadObject", "invalid response type")
	}

	return headResp, nil
}

// =============================================================================
// Listing Operations
// =============================================================================

// ListObjects lists objects matching the specified criteria.
func (c *client) ListObjects(ctx context.Context, req *ListObjectsRequest) (*ListObjectsResponse, error) {
	if err := validateListObjectsRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListObjects", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListObjectsResponse{Objects: []*ObjectSummary{}}, nil
	}

	listResp, ok := resp.(*ListObjectsResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.ListObjects", "invalid response type")
	}

	return listResp, nil
}

// ListByTag lists objects with a specific tag.
func (c *client) ListByTag(ctx context.Context, req *ListByTagRequest) (*ListByTagResponse, error) {
	if err := validateListByTagRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListByTag", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListByTagResponse{Objects: []*ObjectSummary{}}, nil
	}

	listResp, ok := resp.(*ListByTagResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.ListByTag", "invalid response type")
	}

	return listResp, nil
}

// ListByTenant lists objects owned by a specific tenant.
func (c *client) ListByTenant(ctx context.Context, req *ListByTenantRequest) (*ListByTenantResponse, error) {
	if err := validateListByTenantRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListByTenant", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListByTenantResponse{Objects: []*ObjectSummary{}}, nil
	}

	listResp, ok := resp.(*ListByTenantResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.ListByTenant", "invalid response type")
	}

	return listResp, nil
}

// =============================================================================
// Copy Operations
// =============================================================================

// CopyObject copies an object to a new location.
func (c *client) CopyObject(ctx context.Context, req *CopyObjectRequest) (*CopyObjectResponse, error) {
	if err := validateCopyObjectRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "CopyObject", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorage.CopyObject", "nil response from server")
	}

	copyResp, ok := resp.(*CopyObjectResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.CopyObject", "invalid response type")
	}

	return copyResp, nil
}

// =============================================================================
// Replication & Migration Operations
// =============================================================================

// ReplicateObject replicates an object to target backends.
func (c *client) ReplicateObject(ctx context.Context, req *ReplicateObjectRequest) (*ReplicateObjectResponse, error) {
	if err := validateReplicateObjectRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "ReplicateObject", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorage.ReplicateObject", "nil response from server")
	}

	replicateResp, ok := resp.(*ReplicateObjectResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.ReplicateObject", "invalid response type")
	}

	return replicateResp, nil
}

// MigrateObject migrates an object to a new backend.
func (c *client) MigrateObject(ctx context.Context, req *MigrateObjectRequest) (*MigrateObjectResponse, error) {
	if err := validateMigrateObjectRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "MigrateObject", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorage.MigrateObject", "nil response from server")
	}

	migrateResp, ok := resp.(*MigrateObjectResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.MigrateObject", "invalid response type")
	}

	return migrateResp, nil
}

// =============================================================================
// Archive & Restore Operations
// =============================================================================

// ArchiveObject moves an object to archive storage.
func (c *client) ArchiveObject(ctx context.Context, req *ArchiveObjectRequest) (*ArchiveObjectResponse, error) {
	if err := validateArchiveObjectRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "ArchiveObject", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorage.ArchiveObject", "nil response from server")
	}

	archiveResp, ok := resp.(*ArchiveObjectResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.ArchiveObject", "invalid response type")
	}

	return archiveResp, nil
}

// RestoreObject restores an archived object to active storage.
func (c *client) RestoreObject(ctx context.Context, req *RestoreObjectRequest) (*RestoreObjectResponse, error) {
	if err := validateRestoreObjectRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "RestoreObject", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorage.RestoreObject", "nil response from server")
	}

	restoreResp, ok := resp.(*RestoreObjectResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.RestoreObject", "invalid response type")
	}

	return restoreResp, nil
}

// =============================================================================
// Verification Operations
// =============================================================================

// VerifyObject verifies object integrity across storage backends.
func (c *client) VerifyObject(ctx context.Context, req *VerifyObjectRequest) (*VerifyObjectResponse, error) {
	if err := validateVerifyObjectRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "VerifyObject", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorage.VerifyObject", "nil response from server")
	}

	verifyResp, ok := resp.(*VerifyObjectResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.VerifyObject", "invalid response type")
	}

	return verifyResp, nil
}

// =============================================================================
// Policy Management Operations
// =============================================================================

// SetObjectPolicy sets policies for an object.
func (c *client) SetObjectPolicy(ctx context.Context, req *SetObjectPolicyRequest) (*SetObjectPolicyResponse, error) {
	if err := validateSetObjectPolicyRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "SetObjectPolicy", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorage.SetObjectPolicy", "nil response from server")
	}

	policyResp, ok := resp.(*SetObjectPolicyResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.SetObjectPolicy", "invalid response type")
	}

	return policyResp, nil
}

// GetObjectPolicy retrieves policies for an object.
func (c *client) GetObjectPolicy(ctx context.Context, req *GetObjectPolicyRequest) (*GetObjectPolicyResponse, error) {
	if err := validateGetObjectPolicyRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetObjectPolicy", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("objectstorage.GetObjectPolicy", "object policy not found")
	}

	policyResp, ok := resp.(*GetObjectPolicyResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.GetObjectPolicy", "invalid response type")
	}

	return policyResp, nil
}

// =============================================================================
// Backend Management Operations
// =============================================================================

// ListBackends returns available storage backends.
func (c *client) ListBackends(ctx context.Context, req *ListBackendsRequest) (*ListBackendsResponse, error) {
	if err := validateListBackendsRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListBackends", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListBackendsResponse{Backends: []*BackendInfo{}}, nil
	}

	listResp, ok := resp.(*ListBackendsResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.ListBackends", "invalid response type")
	}

	return listResp, nil
}

// GetBackendHealth returns health status for a backend.
func (c *client) GetBackendHealth(ctx context.Context, req *GetBackendHealthRequest) (*GetBackendHealthResponse, error) {
	if err := validateGetBackendHealthRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetBackendHealth", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("objectstorage.GetBackendHealth", "backend not found")
	}

	healthResp, ok := resp.(*GetBackendHealthResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.GetBackendHealth", "invalid response type")
	}

	return healthResp, nil
}

// =============================================================================
// Tenant Management Operations
// =============================================================================

// GetTenantUsage returns storage usage for a tenant.
func (c *client) GetTenantUsage(ctx context.Context, req *GetTenantUsageRequest) (*GetTenantUsageResponse, error) {
	if err := validateGetTenantUsageRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetTenantUsage", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("objectstorage.GetTenantUsage", "tenant not found")
	}

	usageResp, ok := resp.(*GetTenantUsageResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.GetTenantUsage", "invalid response type")
	}

	return usageResp, nil
}

// GetTenantQuotas returns quota settings for a tenant.
func (c *client) GetTenantQuotas(ctx context.Context, req *GetTenantQuotasRequest) (*GetTenantQuotasResponse, error) {
	if err := validateGetTenantQuotasRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetTenantQuotas", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("objectstorage.GetTenantQuotas", "tenant not found")
	}

	quotasResp, ok := resp.(*GetTenantQuotasResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.GetTenantQuotas", "invalid response type")
	}

	return quotasResp, nil
}

// SetTenantQuotas sets quota settings for a tenant.
func (c *client) SetTenantQuotas(ctx context.Context, req *SetTenantQuotasRequest) (*SetTenantQuotasResponse, error) {
	if err := validateSetTenantQuotasRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "SetTenantQuotas", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorage.SetTenantQuotas", "nil response from server")
	}

	quotasResp, ok := resp.(*SetTenantQuotasResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.SetTenantQuotas", "invalid response type")
	}

	return quotasResp, nil
}

// =============================================================================
// Batch Operations
// =============================================================================

// BatchPutObjects stores multiple objects in a single request.
func (c *client) BatchPutObjects(ctx context.Context, req *BatchPutObjectsRequest) (*BatchPutObjectsResponse, error) {
	if err := validateBatchPutObjectsRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "BatchPutObjects", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorage.BatchPutObjects", "nil response from server")
	}

	batchResp, ok := resp.(*BatchPutObjectsResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.BatchPutObjects", "invalid response type")
	}

	return batchResp, nil
}

// BatchGetObjects retrieves multiple objects in a single request.
func (c *client) BatchGetObjects(ctx context.Context, req *BatchGetObjectsRequest) (*BatchGetObjectsResponse, error) {
	if err := validateBatchGetObjectsRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "BatchGetObjects", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &BatchGetObjectsResponse{Objects: []*GetObjectResponse{}, NotFound: []string{}}, nil
	}

	batchResp, ok := resp.(*BatchGetObjectsResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.BatchGetObjects", "invalid response type")
	}

	return batchResp, nil
}

// BatchDeleteObjects deletes multiple objects in a single request.
func (c *client) BatchDeleteObjects(ctx context.Context, req *BatchDeleteObjectsRequest) (*BatchDeleteObjectsResponse, error) {
	if err := validateBatchDeleteObjectsRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "BatchDeleteObjects", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorage.BatchDeleteObjects", "nil response from server")
	}

	batchResp, ok := resp.(*BatchDeleteObjectsResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorage.BatchDeleteObjects", "invalid response type")
	}

	return batchResp, nil
}

// =============================================================================
// Streaming Operations
// =============================================================================

// StreamPutObject creates a streaming writer for uploading large objects.
func (c *client) StreamPutObject(ctx context.Context, opts StreamPutOptions) (StreamPutWriter, error) {
	if err := validateStreamPutOptions(&opts); err != nil {
		return nil, err
	}

	// Set default chunk size if not specified.
	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}

	// Create the streaming request.
	stream, err := c.transport.RequestStream(ctx, serviceName, "StreamPutObject", nil)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeConnection, "objectstorage.StreamPutObject", "failed to create stream")
	}

	return &streamPutWriter{
		stream:    stream,
		opts:      opts,
		chunkSize: chunkSize,
	}, nil
}

// StreamGetObject creates a streaming reader for downloading large objects.
func (c *client) StreamGetObject(ctx context.Context, req *GetObjectRequest) (StreamGetReader, error) {
	if err := validateGetObjectRequest(req); err != nil {
		return nil, err
	}

	// Create the streaming request.
	stream, err := c.transport.RequestStream(ctx, serviceName, "StreamGetObject", req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeConnection, "objectstorage.StreamGetObject", "failed to create stream")
	}

	return &streamGetReader{
		stream: stream,
		req:    req,
	}, nil
}

// =============================================================================
// Stream Writer Implementation
// =============================================================================

// streamPutWriter implements StreamPutWriter.
type streamPutWriter struct {
	stream       transport.Stream
	opts         StreamPutOptions
	chunkSize    int
	bytesWritten int64
	chunkIndex   int32
	metadataSent bool
	closed       bool
	aborted      bool
}

// Write writes data to the stream.
func (w *streamPutWriter) Write(p []byte) (n int, err error) {
	if w.closed {
		return 0, errors.NewInvalidInput("streamPutWriter.Write", "writer is closed")
	}
	if w.aborted {
		return 0, errors.NewCanceled("streamPutWriter.Write", "upload was aborted")
	}

	// Send metadata as the first chunk if not already sent.
	if !w.metadataSent {
		metadataReq := &PutObjectRequest{
			Key:               w.opts.Key,
			ContentType:       w.opts.ContentType,
			TenantID:          w.opts.TenantID,
			Tags:              w.opts.Tags,
			ReplicationPolicy: w.opts.ReplicationPolicy,
			ArchivePolicy:     w.opts.ArchivePolicy,
			RetentionPolicy:   w.opts.RetentionPolicy,
			TargetBackend:     w.opts.TargetBackend,
		}

		chunk := &streamPutChunk{
			Metadata:   metadataReq,
			ChunkIndex: 0,
			IsLast:     false,
		}

		if err := w.stream.Send(chunk); err != nil {
			return 0, errors.Wrap(err, errors.CodeConnection, "streamPutWriter.Write", "failed to send metadata")
		}
		w.metadataSent = true
		w.chunkIndex = 1
	}

	// Write data in chunks.
	totalWritten := 0
	for len(p) > 0 {
		chunkData := p
		if len(chunkData) > w.chunkSize {
			chunkData = p[:w.chunkSize]
		}
		p = p[len(chunkData):]

		chunk := &streamPutChunk{
			Data:       chunkData,
			ChunkIndex: w.chunkIndex,
			IsLast:     false,
		}

		if err := w.stream.Send(chunk); err != nil {
			return totalWritten, errors.Wrap(err, errors.CodeConnection, "streamPutWriter.Write", "failed to send chunk")
		}

		w.chunkIndex++
		totalWritten += len(chunkData)
		atomic.AddInt64(&w.bytesWritten, int64(len(chunkData)))
	}

	return totalWritten, nil
}

// Close completes the upload and returns the response.
func (w *streamPutWriter) Close() (*PutObjectResponse, error) {
	if w.closed {
		return nil, errors.NewInvalidInput("streamPutWriter.Close", "writer is already closed")
	}
	if w.aborted {
		return nil, errors.NewCanceled("streamPutWriter.Close", "upload was aborted")
	}

	w.closed = true

	// Send final chunk to signal completion.
	finalChunk := &streamPutChunk{
		ChunkIndex: w.chunkIndex,
		IsLast:     true,
	}

	if err := w.stream.Send(finalChunk); err != nil {
		return nil, errors.Wrap(err, errors.CodeConnection, "streamPutWriter.Close", "failed to send final chunk")
	}

	// Close send side.
	if err := w.stream.CloseSend(); err != nil {
		return nil, errors.Wrap(err, errors.CodeConnection, "streamPutWriter.Close", "failed to close send")
	}

	// Receive response.
	resp, err := w.stream.Recv()
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeConnection, "streamPutWriter.Close", "failed to receive response")
	}

	if resp == nil {
		return nil, errors.NewInternal("streamPutWriter.Close", "nil response from server")
	}

	putResp, ok := resp.(*PutObjectResponse)
	if !ok {
		return nil, errors.NewInternal("streamPutWriter.Close", "invalid response type")
	}

	return putResp, nil
}

// Abort cancels the upload without committing.
func (w *streamPutWriter) Abort() error {
	if w.closed {
		return errors.NewInvalidInput("streamPutWriter.Abort", "writer is already closed")
	}
	if w.aborted {
		return nil // Already aborted
	}

	w.aborted = true
	return w.stream.Close()
}

// BytesWritten returns the total bytes written so far.
func (w *streamPutWriter) BytesWritten() int64 {
	return atomic.LoadInt64(&w.bytesWritten)
}

// streamPutChunk is the internal chunk representation for streaming puts.
type streamPutChunk struct {
	Metadata   *PutObjectRequest
	Data       []byte
	ChunkIndex int32
	IsLast     bool
	Checksum   []byte
}

// =============================================================================
// Stream Reader Implementation
// =============================================================================

// streamGetReader implements StreamGetReader.
type streamGetReader struct {
	stream    transport.Stream
	req       *GetObjectRequest
	metadata  *ObjectMetadata
	buffer    []byte
	bytesRead int64
	totalSize int64
	closed    bool
	eof       bool // set once the stream has ended; later Reads return io.EOF
}

// Read reads data from the stream.
func (r *streamGetReader) Read(p []byte) (n int, err error) {
	if r.closed {
		return 0, errors.NewInvalidInput("streamGetReader.Read", "reader is closed")
	}

	// If we have buffered data, return it first.
	if len(r.buffer) > 0 {
		n = copy(p, r.buffer)
		r.buffer = r.buffer[n:]
		atomic.AddInt64(&r.bytesRead, int64(n))
		return n, nil
	}

	// The stream has ended; never call Recv on a finished stream.
	if r.eof {
		return 0, io.EOF
	}

	// Receive next chunk from stream.
	chunk, err := r.recvChunk("streamGetReader.Read")
	if err != nil {
		if stderrors.Is(err, io.EOF) {
			r.eof = true
			return 0, io.EOF
		}
		return 0, err
	}

	// Update metadata from first chunk if not set.
	if r.metadata == nil && chunk.TotalSize > 0 {
		r.totalSize = chunk.TotalSize
	}

	if chunk.IsLast {
		r.eof = true
		if len(chunk.Data) == 0 {
			return 0, io.EOF
		}
	}

	// Copy chunk data to output buffer.
	n = copy(p, chunk.Data)
	if n < len(chunk.Data) {
		// Buffer remaining data.
		r.buffer = make([]byte, len(chunk.Data)-n)
		copy(r.buffer, chunk.Data[n:])
	}

	atomic.AddInt64(&r.bytesRead, int64(n))
	return n, nil
}

// Close closes the stream.
func (r *streamGetReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.stream.Close()
}

// Metadata returns the object metadata.
func (r *streamGetReader) Metadata() *ObjectMetadata {
	return r.metadata
}

// ReadMetadata reads and returns metadata without consuming data.
func (r *streamGetReader) ReadMetadata() (*ObjectMetadata, error) {
	if r.metadata != nil {
		return r.metadata, nil
	}

	// Receive first chunk to get metadata.
	chunk, err := r.recvChunk("streamGetReader.ReadMetadata")
	if err != nil {
		return nil, err
	}

	// Store metadata.
	r.metadata = &ObjectMetadata{
		Key:  r.req.Key,
		Size: chunk.TotalSize,
	}
	r.totalSize = chunk.TotalSize

	// Buffer the data from first chunk.
	if len(chunk.Data) > 0 {
		r.buffer = make([]byte, len(chunk.Data))
		copy(r.buffer, chunk.Data)
	}

	return r.metadata, nil
}

// recvChunk receives the next chunk. A stream that ends in an error delivers
// that error as its final message; it is returned as the stream's failure.
func (r *streamGetReader) recvChunk(op string) (*ObjectChunk, error) {
	resp, err := r.stream.Recv()
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeConnection, op, "failed to receive chunk")
	}

	switch m := resp.(type) {
	case *ObjectChunk:
		if m == nil {
			return nil, errors.NewInternal(op, "nil chunk from server")
		}
		return m, nil
	case error:
		return nil, m
	case nil:
		return nil, errors.NewInternal(op, "nil chunk from server")
	default:
		return nil, errors.NewInternal(op, "invalid chunk type")
	}
}

// TotalSize returns the total object size in bytes.
func (r *streamGetReader) TotalSize() int64 {
	return r.totalSize
}

// BytesRead returns the total bytes read so far.
func (r *streamGetReader) BytesRead() int64 {
	return atomic.LoadInt64(&r.bytesRead)
}

// =============================================================================
// Validation Functions
// =============================================================================

func validatePutObjectRequest(req *PutObjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.PutObject", "request is required")
	}
	if req.Key == "" {
		return errors.NewInvalidInput("objectstorage.PutObject", "key is required")
	}
	if len(req.Value) == 0 {
		return errors.NewInvalidInput("objectstorage.PutObject", "value is required")
	}
	return nil
}

func validateGetObjectRequest(req *GetObjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.GetObject", "request is required")
	}
	if req.Key == "" {
		return errors.NewInvalidInput("objectstorage.GetObject", "key is required")
	}
	return nil
}

func validateDeleteObjectRequest(req *DeleteObjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.DeleteObject", "request is required")
	}
	if req.Key == "" {
		return errors.NewInvalidInput("objectstorage.DeleteObject", "key is required")
	}
	return nil
}

func validateHeadObjectRequest(req *HeadObjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.HeadObject", "request is required")
	}
	if req.Key == "" {
		return errors.NewInvalidInput("objectstorage.HeadObject", "key is required")
	}
	return nil
}

func validateListObjectsRequest(req *ListObjectsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.ListObjects", "request is required")
	}
	return nil
}

func validateListByTagRequest(req *ListByTagRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.ListByTag", "request is required")
	}
	if req.TagName == "" {
		return errors.NewInvalidInput("objectstorage.ListByTag", "tag name is required")
	}
	return nil
}

func validateListByTenantRequest(req *ListByTenantRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.ListByTenant", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("objectstorage.ListByTenant", "tenant ID is required")
	}
	return nil
}

func validateCopyObjectRequest(req *CopyObjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.CopyObject", "request is required")
	}
	if req.SourceKey == "" {
		return errors.NewInvalidInput("objectstorage.CopyObject", "source key is required")
	}
	if req.DestinationKey == "" {
		return errors.NewInvalidInput("objectstorage.CopyObject", "destination key is required")
	}
	return nil
}

func validateReplicateObjectRequest(req *ReplicateObjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.ReplicateObject", "request is required")
	}
	if req.Key == "" {
		return errors.NewInvalidInput("objectstorage.ReplicateObject", "key is required")
	}
	if len(req.Targets) == 0 {
		return errors.NewInvalidInput("objectstorage.ReplicateObject", "at least one target is required")
	}
	return nil
}

func validateMigrateObjectRequest(req *MigrateObjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.MigrateObject", "request is required")
	}
	if req.Key == "" {
		return errors.NewInvalidInput("objectstorage.MigrateObject", "key is required")
	}
	if req.TargetBackend == "" {
		return errors.NewInvalidInput("objectstorage.MigrateObject", "target backend is required")
	}
	return nil
}

func validateArchiveObjectRequest(req *ArchiveObjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.ArchiveObject", "request is required")
	}
	if req.Key == "" {
		return errors.NewInvalidInput("objectstorage.ArchiveObject", "key is required")
	}
	return nil
}

func validateRestoreObjectRequest(req *RestoreObjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.RestoreObject", "request is required")
	}
	if req.Key == "" {
		return errors.NewInvalidInput("objectstorage.RestoreObject", "key is required")
	}
	return nil
}

func validateVerifyObjectRequest(req *VerifyObjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.VerifyObject", "request is required")
	}
	if req.Key == "" {
		return errors.NewInvalidInput("objectstorage.VerifyObject", "key is required")
	}
	return nil
}

func validateSetObjectPolicyRequest(req *SetObjectPolicyRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.SetObjectPolicy", "request is required")
	}
	if req.Key == "" {
		return errors.NewInvalidInput("objectstorage.SetObjectPolicy", "key is required")
	}
	return nil
}

func validateGetObjectPolicyRequest(req *GetObjectPolicyRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.GetObjectPolicy", "request is required")
	}
	if req.Key == "" {
		return errors.NewInvalidInput("objectstorage.GetObjectPolicy", "key is required")
	}
	return nil
}

func validateListBackendsRequest(req *ListBackendsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.ListBackends", "request is required")
	}
	return nil
}

func validateGetBackendHealthRequest(req *GetBackendHealthRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.GetBackendHealth", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("objectstorage.GetBackendHealth", "backend name is required")
	}
	return nil
}

func validateGetTenantUsageRequest(req *GetTenantUsageRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.GetTenantUsage", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("objectstorage.GetTenantUsage", "tenant ID is required")
	}
	return nil
}

func validateGetTenantQuotasRequest(req *GetTenantQuotasRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.GetTenantQuotas", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("objectstorage.GetTenantQuotas", "tenant ID is required")
	}
	return nil
}

func validateSetTenantQuotasRequest(req *SetTenantQuotasRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.SetTenantQuotas", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("objectstorage.SetTenantQuotas", "tenant ID is required")
	}
	if req.MaxStorageBytes < 0 {
		return errors.NewInvalidInput("objectstorage.SetTenantQuotas", "max storage bytes cannot be negative")
	}
	if req.MaxObjectSize < 0 {
		return errors.NewInvalidInput("objectstorage.SetTenantQuotas", "max object size cannot be negative")
	}
	if req.MaxObjectCount < 0 {
		return errors.NewInvalidInput("objectstorage.SetTenantQuotas", "max object count cannot be negative")
	}
	return nil
}

func validateBatchPutObjectsRequest(req *BatchPutObjectsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.BatchPutObjects", "request is required")
	}
	if len(req.Objects) == 0 {
		return errors.NewInvalidInput("objectstorage.BatchPutObjects", "at least one object is required")
	}
	if len(req.Objects) > 100 {
		return errors.NewInvalidInput("objectstorage.BatchPutObjects", "maximum 100 objects per batch")
	}
	for i, obj := range req.Objects {
		if obj == nil {
			return errors.NewInvalidInput("objectstorage.BatchPutObjects", "object at index "+string(rune('0'+i))+" is nil")
		}
		if obj.Key == "" {
			return errors.NewInvalidInput("objectstorage.BatchPutObjects", "key is required for all objects")
		}
		if len(obj.Value) == 0 {
			return errors.NewInvalidInput("objectstorage.BatchPutObjects", "value is required for all objects")
		}
	}
	return nil
}

func validateBatchGetObjectsRequest(req *BatchGetObjectsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.BatchGetObjects", "request is required")
	}
	if len(req.Keys) == 0 {
		return errors.NewInvalidInput("objectstorage.BatchGetObjects", "at least one key is required")
	}
	if len(req.Keys) > 100 {
		return errors.NewInvalidInput("objectstorage.BatchGetObjects", "maximum 100 keys per batch")
	}
	for _, key := range req.Keys {
		if key == "" {
			return errors.NewInvalidInput("objectstorage.BatchGetObjects", "empty key not allowed")
		}
	}
	return nil
}

func validateBatchDeleteObjectsRequest(req *BatchDeleteObjectsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorage.BatchDeleteObjects", "request is required")
	}
	if len(req.Keys) == 0 {
		return errors.NewInvalidInput("objectstorage.BatchDeleteObjects", "at least one key is required")
	}
	if len(req.Keys) > 1000 {
		return errors.NewInvalidInput("objectstorage.BatchDeleteObjects", "maximum 1000 keys per batch")
	}
	for _, key := range req.Keys {
		if key == "" {
			return errors.NewInvalidInput("objectstorage.BatchDeleteObjects", "empty key not allowed")
		}
	}
	return nil
}

func validateStreamPutOptions(opts *StreamPutOptions) error {
	if opts == nil {
		return errors.NewInvalidInput("objectstorage.StreamPutObject", "options are required")
	}
	if opts.Key == "" {
		return errors.NewInvalidInput("objectstorage.StreamPutObject", "key is required")
	}
	return nil
}

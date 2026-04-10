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
	"context"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// ========================================
// Object Storage CRUD Operations
// ========================================

// ObjectStoragePutObject stores an object in the distributed storage system.
func (c *Client) ObjectStoragePutObject(ctx context.Context, req *pb.PutObjectRequest) (*pb.PutObjectResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.PutObjectResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.PutObject(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "PutObject")
	}

	return resp, nil
}

// ObjectStorageGetObject retrieves an object by key.
func (c *Client) ObjectStorageGetObject(ctx context.Context, req *pb.GetObjectRequest) (*pb.GetObjectResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.GetObjectResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.GetObject(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "GetObject")
	}

	return resp, nil
}

// ObjectStorageDeleteObject removes an object from storage.
func (c *Client) ObjectStorageDeleteObject(ctx context.Context, req *pb.DeleteObjectRequest) (*pb.DeleteObjectResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.DeleteObjectResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.DeleteObject(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "DeleteObject")
	}

	return resp, nil
}

// ObjectStorageHeadObject retrieves object metadata without the data.
func (c *Client) ObjectStorageHeadObject(ctx context.Context, req *pb.HeadObjectRequest) (*pb.HeadObjectResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.HeadObjectResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.HeadObject(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "HeadObject")
	}

	return resp, nil
}

// ========================================
// Object Storage Listing Operations
// ========================================

// ObjectStorageListObjects lists objects in a namespace with pagination.
func (c *Client) ObjectStorageListObjects(ctx context.Context, req *pb.ListObjectsRequest) (*pb.ListObjectsResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.ListObjectsResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.ListObjects(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "ListObjects")
	}

	return resp, nil
}

// ObjectStorageListByTag lists objects matching a specific tag.
func (c *Client) ObjectStorageListByTag(ctx context.Context, req *pb.ListByTagRequest) (*pb.ListByTagResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.ListByTagResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.ListByTag(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "ListByTag")
	}

	return resp, nil
}

// ObjectStorageListByTenant lists objects belonging to a tenant.
func (c *Client) ObjectStorageListByTenant(ctx context.Context, req *pb.ListByTenantRequest) (*pb.ListByTenantResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.ListByTenantResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.ListByTenant(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "ListByTenant")
	}

	return resp, nil
}

// ========================================
// Object Storage Operations
// ========================================

// ObjectStorageCopyObject copies an object to a new location.
func (c *Client) ObjectStorageCopyObject(ctx context.Context, req *pb.CopyObjectRequest) (*pb.CopyObjectResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.CopyObjectResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.CopyObject(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "CopyObject")
	}

	return resp, nil
}

// ObjectStorageReplicateObject replicates an object to specified backends.
func (c *Client) ObjectStorageReplicateObject(ctx context.Context, req *pb.ReplicateObjectRequest) (*pb.ReplicateObjectResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.ReplicateObjectResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.ReplicateObject(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "ReplicateObject")
	}

	return resp, nil
}

// ObjectStorageMigrateObject migrates an object to another backend.
func (c *Client) ObjectStorageMigrateObject(ctx context.Context, req *pb.MigrateObjectRequest) (*pb.MigrateObjectResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.MigrateObjectResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.MigrateObject(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "MigrateObject")
	}

	return resp, nil
}

// ========================================
// Object Storage Archive & Restore
// ========================================

// ObjectStorageArchiveObject moves an object to archive storage.
func (c *Client) ObjectStorageArchiveObject(ctx context.Context, req *pb.ArchiveObjectRequest) (*pb.ArchiveObjectResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.ArchiveObjectResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.ArchiveObject(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "ArchiveObject")
	}

	return resp, nil
}

// ObjectStorageRestoreObject restores an archived object.
func (c *Client) ObjectStorageRestoreObject(ctx context.Context, req *pb.RestoreObjectRequest) (*pb.RestoreObjectResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.RestoreObjectResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.RestoreObject(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "RestoreObject")
	}

	return resp, nil
}

// ========================================
// Object Storage Verification
// ========================================

// ObjectStorageVerifyObject validates object integrity.
func (c *Client) ObjectStorageVerifyObject(ctx context.Context, req *pb.VerifyObjectRequest) (*pb.VerifyObjectResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.VerifyObjectResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.VerifyObject(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "VerifyObject")
	}

	return resp, nil
}

// ========================================
// Object Storage Policy Operations
// ========================================

// ObjectStorageSetObjectPolicy sets policies for an object.
func (c *Client) ObjectStorageSetObjectPolicy(ctx context.Context, req *pb.SetObjectPolicyRequest) (*pb.SetObjectPolicyResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.SetObjectPolicyResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.SetObjectPolicy(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "SetObjectPolicy")
	}

	return resp, nil
}

// ObjectStorageGetObjectPolicy retrieves policies for an object.
func (c *Client) ObjectStorageGetObjectPolicy(ctx context.Context, req *pb.GetObjectPolicyRequest) (*pb.GetObjectPolicyResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.GetObjectPolicyResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.GetObjectPolicy(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "GetObjectPolicy")
	}

	return resp, nil
}

// ========================================
// Object Storage Backend Operations
// ========================================

// ObjectStorageListBackends returns available storage backends.
func (c *Client) ObjectStorageListBackends(ctx context.Context, req *pb.ListBackendsRequest) (*pb.ListBackendsResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.ListBackendsResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.ListBackends(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "ListBackends")
	}

	return resp, nil
}

// ObjectStorageGetBackendHealth returns health status for a backend.
func (c *Client) ObjectStorageGetBackendHealth(ctx context.Context, req *pb.GetBackendHealthRequest) (*pb.GetBackendHealthResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.GetBackendHealthResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.GetBackendHealth(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "GetBackendHealth")
	}

	return resp, nil
}

// ========================================
// Object Storage Tenant Operations
// ========================================

// ObjectStorageGetTenantUsage returns storage usage for a tenant.
func (c *Client) ObjectStorageGetTenantUsage(ctx context.Context, req *pb.GetObjectStorageTenantUsageRequest) (*pb.GetObjectStorageTenantUsageResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.GetObjectStorageTenantUsageResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.GetObjectStorageTenantUsage(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "GetObjectStorageTenantUsage")
	}

	return resp, nil
}

// ObjectStorageGetTenantQuotas returns quota settings for a tenant.
func (c *Client) ObjectStorageGetTenantQuotas(ctx context.Context, req *pb.GetObjectStorageTenantQuotasRequest) (*pb.GetObjectStorageTenantQuotasResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata.
	ctx = c.addMetadata(ctx)

	// Get object storage client.
	client, err := c.getObjectStorageClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry.
	var resp *pb.GetObjectStorageTenantQuotasResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.GetObjectStorageTenantQuotas(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "GetObjectStorageTenantQuotas")
	}

	return resp, nil
}

// ========================================
// Helper Functions
// ========================================

// getObjectStorageClient returns the object storage gRPC client.
func (c *Client) getObjectStorageClient() (pb.ObjectStorageServiceClient, error) {
	// If using connection pool, get a connection from pool.
	if c.pool != nil {
		conn, err := c.pool.Get()
		if err != nil {
			return nil, err
		}
		return pb.NewObjectStorageServiceClient(conn.conn), nil
	}

	// Otherwise use the single connection.
	if c.conn == nil {
		return nil, api.NewClientClosedError("grpc.getObjectStorageClient")
	}

	return pb.NewObjectStorageServiceClient(c.conn), nil
}

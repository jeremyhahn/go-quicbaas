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
	"time"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// CreateBackup creates a new backup of the database.
func (c *Client) CreateBackup(ctx context.Context, req *proto.CreateBackupRequest) (*proto.CreateBackupResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	ctx = c.addMetadata(ctx)

	client, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "CreateBackup")
	}

	var resp *proto.CreateBackupResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.CreateBackup(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "CreateBackup")
	}

	return resp, nil
}

// ListBackups returns a list of available backups.
func (c *Client) ListBackups(ctx context.Context, req *proto.ListBackupsRequest) (*proto.ListBackupsResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	ctx = c.addMetadata(ctx)

	client, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "ListBackups")
	}

	var resp *proto.ListBackupsResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.ListBackups(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "ListBackups")
	}

	return resp, nil
}

// RestoreBackup restores the database from a backup.
func (c *Client) RestoreBackup(ctx context.Context, req *proto.RestoreBackupRequest) (*proto.RestoreBackupResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	ctx = c.addMetadata(ctx)

	client, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "RestoreBackup")
	}

	var resp *proto.RestoreBackupResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.RestoreBackup(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "RestoreBackup")
	}

	return resp, nil
}

// DeleteBackup deletes a backup.
func (c *Client) DeleteBackup(ctx context.Context, req *proto.DeleteBackupRequest) (*proto.DeleteBackupResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	ctx = c.addMetadata(ctx)

	client, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "DeleteBackup")
	}

	var resp *proto.DeleteBackupResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.DeleteBackup(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "DeleteBackup")
	}

	return resp, nil
}

// VerifyBackup verifies the integrity of a backup.
func (c *Client) VerifyBackup(ctx context.Context, req *proto.VerifyBackupRequest) (*proto.VerifyBackupResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	ctx = c.addMetadata(ctx)

	client, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "VerifyBackup")
	}

	var resp *proto.VerifyBackupResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.VerifyBackup(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "VerifyBackup")
	}

	return resp, nil
}

// BackupCreateRequest defines the interface for backup create requests.
// This allows the adapter to work with any struct that has these fields.
type BackupCreateRequest interface {
	GetName() string
	GetBackupType() string
	GetCompression() string
	GetEncryption() string
	GetStoragePath() string
	GetStorageType() string
	GetStorageConfig() map[string]interface{}
	GetTags() map[string]string
}

// BackupListRequest defines the interface for backup list requests.
type BackupListRequest interface {
	GetLimit() int
	GetOffset() int
	GetStoragePath() string
	GetStorageType() string
	GetStorageConfig() map[string]interface{}
}

// BackupRestoreRequest defines the interface for backup restore requests.
type BackupRestoreRequest interface {
	GetBackupID() string
	GetStoragePath() string
	GetVerifyFirst() bool
	GetVerifyChecksum() bool
	GetSkipExisting() bool
	GetPointInTime() time.Time
	GetStorageType() string
	GetStorageConfig() map[string]interface{}
	GetEncryptionKey() string
}

// BackupDeleteRequest defines the interface for backup delete requests.
type BackupDeleteRequest interface {
	GetBackupID() string
	GetStoragePath() string
	GetForce() bool
}

// BackupVerifyRequest defines the interface for backup verify requests.
type BackupVerifyRequest interface {
	GetBackupID() string
	GetStoragePath() string
	GetStorageType() string
	GetStorageConfig() map[string]interface{}
	GetEncryptionKey() string
}

// BackupClientAdapter wraps the gRPC client to implement the CLI BackupClient interface.
// This provides the translation layer between the CLI request/response types and gRPC proto types.
type BackupClientAdapter struct {
	client *Client
}

// NewBackupClientAdapter creates a new backup client adapter.
func NewBackupClientAdapter(client *Client) *BackupClientAdapter {
	return &BackupClientAdapter{client: client}
}

// CreateBackupFromRequest implements the BackupClient interface for CLI.
// It accepts a BackupCreateRequest interface to allow flexibility in request types.
func (a *BackupClientAdapter) CreateBackupFromRequest(ctx context.Context, req BackupCreateRequest) (*cli.BackupCreateResponse, error) {
	// Convert backup type string to proto enum
	backupType := proto.BackupType_BACKUP_TYPE_FULL
	switch req.GetBackupType() {
	case "full":
		backupType = proto.BackupType_BACKUP_TYPE_FULL
	case "incremental":
		backupType = proto.BackupType_BACKUP_TYPE_INCREMENTAL
	case "snapshot":
		backupType = proto.BackupType_BACKUP_TYPE_SNAPSHOT
	}

	// Convert compression string to proto enum
	compression := proto.CompressionType_COMPRESSION_TYPE_UNSPECIFIED
	switch req.GetCompression() {
	case "none":
		compression = proto.CompressionType_COMPRESSION_TYPE_NONE
	case "gzip":
		compression = proto.CompressionType_COMPRESSION_TYPE_GZIP
	case "snappy":
		compression = proto.CompressionType_COMPRESSION_TYPE_SNAPPY
	case "lz4":
		compression = proto.CompressionType_COMPRESSION_TYPE_LZ4
	case "zstd":
		compression = proto.CompressionType_COMPRESSION_TYPE_GZIP // Map zstd to gzip as fallback
	}

	// Convert encryption string to proto enum
	encryption := proto.EncryptionType_ENCRYPTION_TYPE_UNSPECIFIED
	switch req.GetEncryption() {
	case "none":
		encryption = proto.EncryptionType_ENCRYPTION_TYPE_NONE
	case "aes256", "aes256gcm":
		encryption = proto.EncryptionType_ENCRYPTION_TYPE_AES256
	}

	protoReq := &proto.CreateBackupRequest{
		BackupType:  backupType,
		Compression: compression,
		Encryption:  encryption,
		Tags:        req.GetTags(),
	}

	resp, err := a.client.CreateBackup(ctx, protoReq)
	if err != nil {
		return nil, err
	}

	return &cli.BackupCreateResponse{
		BackupID:  resp.BackupId,
		Timestamp: time.Unix(0, resp.Timestamp),
		Size:      resp.Size,
		Success:   true,
	}, nil
}

// ListBackupsFromRequest implements the BackupClient interface for CLI.
func (a *BackupClientAdapter) ListBackupsFromRequest(ctx context.Context, req BackupListRequest) (*cli.BackupListResponse, error) {
	limit, err := clientInt32("limit", req.GetLimit())
	if err != nil {
		return nil, err
	}
	protoReq := &proto.ListBackupsRequest{
		Limit: limit,
	}

	resp, err := a.client.ListBackups(ctx, protoReq)
	if err != nil {
		return nil, err
	}

	backups := make([]cli.BackupInfo, 0, len(resp.Backups))
	for _, b := range resp.Backups {
		// Convert proto backup type to string
		backupType := "unknown"
		switch b.BackupType {
		case proto.BackupType_BACKUP_TYPE_FULL:
			backupType = "full"
		case proto.BackupType_BACKUP_TYPE_INCREMENTAL:
			backupType = "incremental"
		case proto.BackupType_BACKUP_TYPE_SNAPSHOT:
			backupType = "snapshot"
		}

		backups = append(backups, cli.BackupInfo{
			ID:        b.BackupId,
			Type:      backupType,
			Timestamp: time.Unix(0, b.Timestamp),
			Size:      b.Size,
			NumShards: int(b.NumShards),
		})
	}

	return &cli.BackupListResponse{
		Backups:    backups,
		TotalCount: len(backups),
	}, nil
}

// RestoreBackupFromRequest implements the BackupClient interface for CLI.
func (a *BackupClientAdapter) RestoreBackupFromRequest(ctx context.Context, req BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
	protoReq := &proto.RestoreBackupRequest{
		BackupId:       req.GetBackupID(),
		VerifyChecksum: req.GetVerifyChecksum(),
		SkipExisting:   req.GetSkipExisting(),
	}

	resp, err := a.client.RestoreBackup(ctx, protoReq)
	if err != nil {
		return nil, err
	}

	return &cli.BackupRestoreResponse{
		BackupID:     resp.BackupId,
		KeysRestored: resp.KeysRestored,
		Duration:     time.Duration(resp.Duration),
		Timestamp:    time.Now(),
		Success:      true,
	}, nil
}

// DeleteBackupFromRequest implements the BackupClient interface for CLI.
func (a *BackupClientAdapter) DeleteBackupFromRequest(ctx context.Context, req BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
	protoReq := &proto.DeleteBackupRequest{
		BackupId: req.GetBackupID(),
	}

	resp, err := a.client.DeleteBackup(ctx, protoReq)
	if err != nil {
		return nil, err
	}

	return &cli.BackupDeleteResponse{
		BackupID:  req.GetBackupID(),
		Deleted:   resp.Success,
		Timestamp: time.Now(),
	}, nil
}

// VerifyBackupFromRequest implements the BackupClient interface for CLI.
func (a *BackupClientAdapter) VerifyBackupFromRequest(ctx context.Context, req BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
	protoReq := &proto.VerifyBackupRequest{
		BackupId: req.GetBackupID(),
	}

	resp, err := a.client.VerifyBackup(ctx, protoReq)
	if err != nil {
		return nil, err
	}

	// Convert message to issues list if present
	var issues []string
	if resp.Message != "" {
		issues = []string{resp.Message}
	}

	return &cli.BackupVerifyResponse{
		BackupID:      req.GetBackupID(),
		Valid:         resp.Valid,
		ChecksumValid: resp.ChecksumValid,
		Issues:        issues,
		Timestamp:     time.Now(),
	}, nil
}

// Close closes the underlying gRPC client.
func (a *BackupClientAdapter) Close() error {
	return a.client.Close()
}

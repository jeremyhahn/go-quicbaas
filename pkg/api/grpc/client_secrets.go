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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// StoreSecret stores a new secret.
func (c *Client) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.StoreSecret",
			Err:  ErrRequestNil,
		}
	}

	secretsClient := pb.NewSecretsManagementClient(c.conn)

	var expiresAt int64
	if req.ExpiresAt != nil {
		expiresAt = req.ExpiresAt.Unix()
	}

	pbReq := &pb.StoreSecretRequest{
		SecretId:    req.SecretID,
		Name:        req.Name,
		Description: req.Description,
		Data:        req.Data,
		Metadata:    req.Metadata,
		Tags:        req.Tags,
		ExpiresAt:   expiresAt,
	}

	pbResp, err := secretsClient.StoreSecret(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.StoreSecretResponse{
		SecretID:  pbResp.SecretId,
		Version:   pbResp.Version,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// GetSecret retrieves a secret by ID.
func (c *Client) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.GetSecret",
			Err:  ErrRequestNil,
		}
	}

	secretsClient := pb.NewSecretsManagementClient(c.conn)

	pbReq := &pb.GetSecretRequest{
		SecretId: req.SecretID,
		Version:  req.Version,
	}

	pbResp, err := secretsClient.GetSecret(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.GetSecretResponse{
		SecretID:  pbResp.SecretId,
		Version:   pbResp.Version,
		Data:      pbResp.Data,
		Metadata:  pbResp.Metadata,
		CreatedAt: time.Unix(pbResp.CreatedAt, 0),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// ListSecrets returns all accessible secrets.
func (c *Client) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.ListSecrets",
			Err:  ErrRequestNil,
		}
	}

	limit, err := clientInt32("limit", req.Limit)
	if err != nil {
		return nil, err
	}

	secretsClient := pb.NewSecretsManagementClient(c.conn)

	pbReq := &pb.ListSecretsRequest{
		Limit:  limit,
		Cursor: req.Cursor,
		Filter: req.Filter,
	}

	pbResp, err := secretsClient.ListSecrets(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	secrets := make([]*service.Secret, len(pbResp.Secrets))
	for i, sec := range pbResp.Secrets {
		secrets[i] = convertProtoSecret(sec)
	}

	return &service.ListSecretsResponse{
		Secrets:    secrets,
		NextCursor: pbResp.NextCursor,
		HasMore:    pbResp.HasMore,
		TotalCount: int(pbResp.TotalCount),
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// DeleteSecret deletes a secret.
func (c *Client) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.DeleteSecret",
			Err:  ErrRequestNil,
		}
	}

	secretsClient := pb.NewSecretsManagementClient(c.conn)

	pbReq := &pb.DeleteSecretRequest{
		SecretId:          req.SecretID,
		PermanentlyDelete: req.PermanentlyDelete,
	}

	pbResp, err := secretsClient.DeleteSecret(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.DeleteSecretResponse{
		SecretID:  pbResp.SecretId,
		Deleted:   pbResp.Deleted,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// ListSecretVersions returns all versions of a secret.
func (c *Client) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.ListSecretVersions",
			Err:  ErrRequestNil,
		}
	}

	limit, err := clientInt32("limit", req.Limit)
	if err != nil {
		return nil, err
	}

	secretsClient := pb.NewSecretsManagementClient(c.conn)

	pbReq := &pb.ListSecretVersionsRequest{
		SecretId: req.SecretID,
		Limit:    limit,
		Cursor:   req.Cursor,
	}

	pbResp, err := secretsClient.ListSecretVersions(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	versions := make([]*service.SecretVersion, len(pbResp.Versions))
	for i, ver := range pbResp.Versions {
		versions[i] = convertProtoSecretVersion(ver)
	}

	return &service.ListSecretVersionsResponse{
		Versions:   versions,
		NextCursor: pbResp.NextCursor,
		HasMore:    pbResp.HasMore,
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// RollbackSecret rolls back to a previous version.
func (c *Client) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.RollbackSecret",
			Err:  ErrRequestNil,
		}
	}

	secretsClient := pb.NewSecretsManagementClient(c.conn)

	pbReq := &pb.RollbackSecretRequest{
		SecretId:      req.SecretID,
		TargetVersion: req.TargetVersion,
	}

	pbResp, err := secretsClient.RollbackSecret(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.RollbackSecretResponse{
		SecretID:   pbResp.SecretId,
		NewVersion: pbResp.NewVersion,
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// RotateSecret rotates a secret to a new value.
func (c *Client) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.RotateSecret",
			Err:  ErrRequestNil,
		}
	}

	secretsClient := pb.NewSecretsManagementClient(c.conn)

	pbReq := &pb.RotateSecretRequest{
		SecretId: req.SecretID,
		NewData:  req.NewData,
		Metadata: req.Metadata,
	}

	pbResp, err := secretsClient.RotateSecret(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.RotateSecretResponse{
		SecretID:   pbResp.SecretId,
		OldVersion: pbResp.OldVersion,
		NewVersion: pbResp.NewVersion,
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// UpdateSecretStatus updates the status of a secret (enable/disable).
func (c *Client) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.UpdateSecretStatus",
			Err:  ErrRequestNil,
		}
	}

	secretsClient := pb.NewSecretsManagementClient(c.conn)

	pbReq := &pb.UpdateSecretStatusRequest{
		SecretId: req.SecretID,
		Status:   convertServiceSecretStatus(req.Status),
	}

	pbResp, err := secretsClient.UpdateSecretStatus(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.UpdateSecretStatusResponse{
		SecretID:  pbResp.SecretId,
		OldStatus: service.SecretStatus(""), // Proto doesn't return old status
		NewStatus: convertProtoSecretStatusToService(pbResp.Status),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// GetSecretsByPrefix retrieves all secrets matching a path prefix.
func (c *Client) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.GetSecretsByPrefix",
			Err:  ErrRequestNil,
		}
	}

	limit, err := clientInt32("limit", req.Limit)
	if err != nil {
		return nil, err
	}

	secretsClient := pb.NewSecretsManagementClient(c.conn)

	pbReq := &pb.GetSecretsByPrefixRequest{
		Prefix:      req.Prefix,
		IncludeData: req.IncludeData,
		Limit:       limit,
		Cursor:      req.Cursor,
	}

	pbResp, err := secretsClient.GetSecretsByPrefix(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	// Convert proto secrets to service format
	secrets := make(map[string]map[string][]byte)
	metadata := make(map[string]map[string]string)
	var version int64

	for path, secretData := range pbResp.Secrets {
		secrets[path] = secretData.Data
		metadata[path] = secretData.Metadata
		version = secretData.Version
	}

	return &service.GetSecretsByPrefixResponse{
		Secrets:    secrets,
		Metadata:   metadata,
		Version:    version,
		NextCursor: pbResp.NextCursor,
		HasMore:    pbResp.HasMore,
		TotalCount: int(pbResp.TotalCount),
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// WatchSecretsByPrefix streams secret changes for a prefix.
func (c *Client) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.WatchSecretsByPrefix",
			Err:  ErrRequestNil,
		}
	}

	secretsClient := pb.NewSecretsManagementClient(c.conn)

	pbReq := &pb.WatchSecretsRequest{
		Prefix:       req.Prefix,
		StartVersion: req.StartVersion,
	}

	stream, err := secretsClient.WatchSecretsByPrefix(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	changes := make(chan *service.SecretChange, 100)

	go func() {
		defer close(changes)

		for {
			pbChange, err := stream.Recv()
			if err != nil {
				// Stream closed or error
				return
			}

			// Convert proto change type to service
			var changeType service.SecretChangeType
			switch pbChange.ChangeType {
			case pb.SecretChangeType_SECRET_CHANGE_TYPE_CREATED:
				changeType = service.SecretChangeCreated
			case pb.SecretChangeType_SECRET_CHANGE_TYPE_UPDATED:
				changeType = service.SecretChangeUpdated
			case pb.SecretChangeType_SECRET_CHANGE_TYPE_DELETED:
				changeType = service.SecretChangeDeleted
			default:
				changeType = service.SecretChangeCreated
			}

			change := &service.SecretChange{
				Path:       pbChange.Path,
				ChangeType: changeType,
				Data:       pbChange.Data,
				Metadata:   pbChange.Metadata,
				Version:    pbChange.Version,
				Timestamp:  time.Unix(pbChange.Timestamp, 0),
			}

			select {
			case changes <- change:
			case <-ctx.Done():
				return
			}
		}
	}()

	return changes, nil
}

// =============================================================================
// Conversion Helpers
// =============================================================================

func convertProtoSecret(sec *pb.Secret) *service.Secret {
	if sec == nil {
		return nil
	}

	return &service.Secret{
		SecretID:       sec.SecretId,
		Name:           sec.Name,
		Description:    sec.Description,
		CurrentVersion: sec.CurrentVersion,
		CreatedAt:      time.Unix(sec.CreatedAt, 0),
		UpdatedAt:      time.Unix(sec.UpdatedAt, 0),
		RotationPolicy: convertProtoSecretRotationPolicy(sec.RotationPolicy),
		Tags:           sec.Tags,
		Owner:          sec.Owner,
	}
}

func convertProtoSecretVersion(ver *pb.SecretVersion) *service.SecretVersion {
	if ver == nil {
		return nil
	}

	sv := &service.SecretVersion{
		SecretID:  ver.SecretId,
		Version:   ver.Version,
		Metadata:  ver.Metadata,
		CreatedAt: time.Unix(ver.CreatedAt, 0),
		CreatedBy: ver.CreatedBy,
	}

	if ver.DeletedAt > 0 {
		t := time.Unix(ver.DeletedAt, 0)
		sv.DeletedAt = &t
	}

	if ver.ExpiresAt > 0 {
		t := time.Unix(ver.ExpiresAt, 0)
		sv.ExpiresAt = &t
	}

	return sv
}

// Note: convertProtoSecretRotationPolicy is defined in server_secrets.go

func convertServiceSecretStatus(status service.SecretStatus) pb.SecretStatus {
	switch status {
	case service.SecretStatusEnabled:
		return pb.SecretStatus_SECRET_STATUS_ENABLED
	case service.SecretStatusDisabled:
		return pb.SecretStatus_SECRET_STATUS_DISABLED
	default:
		return pb.SecretStatus_SECRET_STATUS_UNSPECIFIED
	}
}

func convertProtoSecretStatusToService(status pb.SecretStatus) service.SecretStatus {
	switch status {
	case pb.SecretStatus_SECRET_STATUS_ENABLED:
		return service.SecretStatusEnabled
	case pb.SecretStatus_SECRET_STATUS_DISABLED:
		return service.SecretStatusDisabled
	default:
		return service.SecretStatusEnabled
	}
}

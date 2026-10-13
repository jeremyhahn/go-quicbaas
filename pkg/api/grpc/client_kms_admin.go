// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// client_kms_admin.go provides the Client wrappers for the KMS key
// administration verbs (public key retrieval, revocation, metadata,
// access policy and rotation policy). Each wrapper converts between the
// canonical service.* request/response types and the generated
// KeyManagement proto types, mirroring the client_kms.go conventions.

package grpc

import (
	"context"
	"time"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// KMSGetPublicKey retrieves the DER-encoded public key component of an
// asymmetric key.
func (c *Client) KMSGetPublicKey(ctx context.Context, req *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGetPublicKey", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbResp, err := kmsClient.GetPublicKey(ctx, &pb.GetPublicKeyRequest{
		KeyId:   req.KeyID,
		Version: req.Version,
	})
	if err != nil {
		return nil, convertError(err)
	}

	return &service.GetPublicKeyResponse{
		KeyID:     pbResp.KeyId,
		PublicKey: pbResp.PublicKey,
		Algorithm: convertProtoToServiceKeyAlgorithm(pbResp.Algorithm),
		Version:   pbResp.Version,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSRevokeKey marks a key as revoked and unusable.
func (c *Client) KMSRevokeKey(ctx context.Context, req *service.RevokeKeyRequest) (*service.RevokeKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSRevokeKey", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbResp, err := kmsClient.RevokeKey(ctx, &pb.RevokeKeyRequest{
		KeyId:  req.KeyID,
		Reason: req.Reason,
	})
	if err != nil {
		return nil, convertError(err)
	}

	return &service.RevokeKeyResponse{
		KeyID:     pbResp.KeyId,
		Status:    convertProtoToServiceKeyStatus(pbResp.Status),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSGetKeyMetadata retrieves metadata for a key without exposing key material.
func (c *Client) KMSGetKeyMetadata(ctx context.Context, req *service.GetKeyMetadataRequest) (*service.GetKeyMetadataResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGetKeyMetadata", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbResp, err := kmsClient.GetKeyMetadata(ctx, &pb.GetKeyMetadataRequest{KeyId: req.KeyID})
	if err != nil {
		return nil, convertError(err)
	}

	return &service.GetKeyMetadataResponse{
		Metadata:  convertProtoKeyMetadata(pbResp.Metadata),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSUpdateKeyMetadata updates mutable metadata fields for a key.
func (c *Client) KMSUpdateKeyMetadata(ctx context.Context, req *service.UpdateKeyMetadataRequest) (*service.UpdateKeyMetadataResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSUpdateKeyMetadata", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.UpdateKeyMetadataRequest{
		KeyId:      req.KeyID,
		Tags:       req.Tags,
		RemoveTags: req.RemoveTags,
	}
	if req.Name != nil {
		pbReq.Name = *req.Name
	}
	if req.Description != nil {
		pbReq.Description = *req.Description
	}

	pbResp, err := kmsClient.UpdateKeyMetadata(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.UpdateKeyMetadataResponse{
		Metadata:  convertProtoKeyMetadata(pbResp.Metadata),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSSetKeyPolicy sets the access control policy for a key.
func (c *Client) KMSSetKeyPolicy(ctx context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSSetKeyPolicy", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbResp, err := kmsClient.SetKeyPolicy(ctx, &pb.SetKeyPolicyRequest{
		KeyId:  req.KeyID,
		Policy: serviceKeyPolicyToProto(req.Policy),
	})
	if err != nil {
		return nil, convertError(err)
	}

	return &service.SetKeyPolicyResponse{
		KeyID:         pbResp.KeyId,
		PolicyVersion: pbResp.PolicyVersion,
		Timestamp:     time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSGetKeyPolicy retrieves the access control policy for a key.
func (c *Client) KMSGetKeyPolicy(ctx context.Context, req *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGetKeyPolicy", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbResp, err := kmsClient.GetKeyPolicy(ctx, &pb.GetKeyPolicyRequest{KeyId: req.KeyID})
	if err != nil {
		return nil, convertError(err)
	}

	return &service.GetKeyPolicyResponse{
		Policy:    protoKeyPolicyToService(pbResp.Policy),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSSetKeyRotationPolicy sets or replaces the automatic rotation policy for a key.
func (c *Client) KMSSetKeyRotationPolicy(ctx context.Context, req *service.SetKeyRotationPolicyRequest) (*service.SetKeyRotationPolicyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSSetKeyRotationPolicy", "request")
	}

	policy, err := serviceToProtoRotationPolicy(req.Policy)
	if err != nil {
		return nil, err
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbResp, err := kmsClient.SetKeyRotationPolicy(ctx, &pb.SetKeyRotationPolicyRequest{
		KeyId:  req.KeyID,
		Policy: policy,
	})
	if err != nil {
		return nil, convertError(err)
	}

	return &service.SetKeyRotationPolicyResponse{
		KeyID:     pbResp.KeyId,
		Policy:    protoToServiceRotationPolicy(pbResp.Policy),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSGetKeyRotationPolicy retrieves the current automatic rotation policy for a key.
func (c *Client) KMSGetKeyRotationPolicy(ctx context.Context, req *service.GetKeyRotationPolicyRequest) (*service.GetKeyRotationPolicyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGetKeyRotationPolicy", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbResp, err := kmsClient.GetKeyRotationPolicy(ctx, &pb.GetKeyRotationPolicyRequest{KeyId: req.KeyID})
	if err != nil {
		return nil, convertError(err)
	}

	return &service.GetKeyRotationPolicyResponse{
		KeyID:     pbResp.KeyId,
		Policy:    protoToServiceRotationPolicy(pbResp.Policy),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSDeleteKeyRotationPolicy removes the rotation policy for a key, disabling
// automatic rotation.
func (c *Client) KMSDeleteKeyRotationPolicy(ctx context.Context, req *service.DeleteKeyRotationPolicyRequest) (*service.DeleteKeyRotationPolicyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSDeleteKeyRotationPolicy", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbResp, err := kmsClient.DeleteKeyRotationPolicy(ctx, &pb.DeleteKeyRotationPolicyRequest{KeyId: req.KeyID})
	if err != nil {
		return nil, convertError(err)
	}

	return &service.DeleteKeyRotationPolicyResponse{
		KeyID:     pbResp.KeyId,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

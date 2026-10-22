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
	stderrors "errors"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// KMSGenerateKey creates a new cryptographic key with the specified parameters.
func (c *Client) KMSGenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGenerateKey", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Convert KeyAttributes to protobuf KeySpec
	alg, keySize, usage, exportable, err := convertKeyAttributesToProto(req.KeyAttributes)
	if err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.GenerateKeyRequest{
		Name:        req.Name,
		Description: req.Description,
		Tags:        req.Tags,
		Owner:       req.Owner,
		KeySpec: &pb.KeySpec{
			Algorithm:  alg,
			KeySize:    keySize,
			Usage:      usage,
			Exportable: exportable,
		},
	}

	pbResp, err := kmsClient.GenerateKey(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.GenerateKeyResponse{
		Key:       convertProtoKeyHandle(pbResp.Key),
		Metadata:  convertProtoKeyMetadata(pbResp.Metadata),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSImportKey imports an external key into the KMS.
func (c *Client) KMSImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSImportKey", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Convert KeyAttributes to protobuf KeySpec
	alg, keySize, usage, exportable, err := convertKeyAttributesToProto(req.KeyAttributes)
	if err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.ImportKeyRequest{
		KeyMaterial:   req.KeyMaterial,
		WrappingKeyId: req.WrappingKeyID,
		Name:          req.Name,
		Description:   req.Description,
		Tags:          req.Tags,
		KeySpec: &pb.KeySpec{
			Algorithm:  alg,
			KeySize:    keySize,
			Usage:      usage,
			Exportable: exportable,
		},
	}

	pbResp, err := kmsClient.ImportKey(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.ImportKeyResponse{
		Key:       convertProtoKeyHandle(pbResp.Key),
		Metadata:  convertProtoKeyMetadata(pbResp.Metadata),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSGetKey retrieves key metadata by ID.
func (c *Client) KMSGetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGetKey", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.GetKeyRequest{
		KeyId:   req.KeyID,
		Version: req.Version,
	}

	pbResp, err := kmsClient.GetKey(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.GetKeyResponse{
		Key:       convertProtoKeyHandle(pbResp.Key),
		Metadata:  convertProtoKeyMetadata(pbResp.Metadata),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSListKeys lists all keys matching optional filters.
func (c *Client) KMSListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	if req == nil {
		req = &service.ListKeysRequest{}
	}

	limit, err := clientInt32("limit", req.Limit)
	if err != nil {
		return nil, err
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.ListKeysRequest{
		Limit:  limit,
		Cursor: req.Cursor,
		Filter: req.Filter,
	}

	pbResp, err := kmsClient.ListKeys(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	keys := make([]*service.KeyHandle, len(pbResp.Keys))
	for i, key := range pbResp.Keys {
		keys[i] = convertProtoKeyHandle(key)
	}

	return &service.ListKeysResponse{
		Keys:       keys,
		NextCursor: pbResp.NextCursor,
		HasMore:    pbResp.HasMore,
		TotalCount: int(pbResp.TotalCount),
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSRotateKey creates a new version of an existing key.
func (c *Client) KMSRotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSRotateKey", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.RotateKeyRequest{
		KeyId:              req.KeyID,
		RetainOldVersion:   req.RetainOldVersion,
		GracePeriodSeconds: req.GracePeriodSeconds,
	}

	pbResp, err := kmsClient.RotateKey(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.RotateKeyResponse{
		KeyID:                pbResp.KeyId,
		OldVersion:           pbResp.OldVersion,
		NewVersion:           pbResp.NewVersion,
		Timestamp:            time.Unix(pbResp.Timestamp, 0),
		GracePeriodDisableAt: pbResp.GracePeriodDisableAt,
		GracePeriodJobID:     pbResp.GracePeriodJobId,
	}, nil
}

// KMSDeleteKey schedules a key for deletion.
func (c *Client) KMSDeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSDeleteKey", "request")
	}

	pendingWindowDays, err := clientInt32("pending_window_days", req.PendingWindowDays)
	if err != nil {
		return nil, err
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.DeleteKeyRequest{
		KeyId:             req.KeyID,
		PendingWindowDays: pendingWindowDays,
		Force:             req.Force,
	}

	pbResp, err := kmsClient.DeleteKey(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.DeleteKeyResponse{
		KeyID:        pbResp.KeyId,
		Status:       convertProtoToServiceKeyStatus(pbResp.Status),
		DeletionDate: time.Unix(pbResp.DeletionDate, 0),
		Timestamp:    time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSEncrypt encrypts plaintext using the specified key.
func (c *Client) KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSEncrypt", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	cryptoClient := pb.NewCryptoClient(c.conn)

	pbReq := &pb.CryptoEncryptRequest{
		KeyId:     req.KeyID,
		Plaintext: req.Plaintext,
		Context:   req.Context,
		Nonce:     req.Nonce,
	}

	pbResp, err := cryptoClient.Encrypt(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.EncryptResponse{
		KeyID:      pbResp.KeyId,
		KeyVersion: pbResp.KeyVersion,
		Ciphertext: pbResp.Ciphertext,
		Nonce:      pbResp.Nonce,
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSDecrypt decrypts ciphertext using the specified key.
func (c *Client) KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSDecrypt", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	cryptoClient := pb.NewCryptoClient(c.conn)

	pbReq := &pb.CryptoDecryptRequest{
		KeyId:      req.KeyID,
		Ciphertext: req.Ciphertext,
		Context:    req.Context,
		Nonce:      req.Nonce,
		KeyVersion: req.KeyVersion,
	}

	pbResp, err := cryptoClient.Decrypt(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.DecryptResponse{
		KeyID:      pbResp.KeyId,
		KeyVersion: pbResp.KeyVersion,
		Plaintext:  pbResp.Plaintext,
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSGenerateDataKey generates a new data encryption key.
func (c *Client) KMSGenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGenerateDataKey", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Convert KeyAttributes to protobuf KeySpec
	alg, keySize, usage, exportable, err := convertKeyAttributesToProto(req.KeyAttributes)
	if err != nil {
		return nil, err
	}

	cryptoClient := pb.NewCryptoClient(c.conn)

	pbReq := &pb.GenerateDataKeyRequest{
		KeyId:   req.KeyID,
		Context: req.Context,
		KeySpec: &pb.KeySpec{
			Algorithm:  alg,
			KeySize:    keySize,
			Usage:      usage,
			Exportable: exportable,
		},
	}

	pbResp, err := cryptoClient.GenerateDataKey(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.GenerateDataKeyResponse{
		KeyID:          pbResp.KeyId,
		Plaintext:      pbResp.Plaintext,
		CiphertextBlob: pbResp.CiphertextBlob,
		Timestamp:      time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSUpdateKeyStatus enables or disables a key or key version.
func (c *Client) KMSUpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSUpdateKeyStatus", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.UpdateKeyStatusRequest{
		KeyId:   req.KeyID,
		Version: req.Version,
		Status:  convertServiceToProtoKeyStatus(req.Status),
	}

	pbResp, err := kmsClient.UpdateKeyStatus(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	newStatus := convertProtoToServiceKeyStatus(pbResp.Status)
	return &service.UpdateKeyStatusResponse{
		KeyID:     pbResp.KeyId,
		Version:   pbResp.Version,
		NewStatus: newStatus,
		Status:    newStatus, // Alias for NewStatus
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSListKeyVersions lists all versions of a key.
func (c *Client) KMSListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSListKeyVersions", "request")
	}

	limit, err := clientInt32("limit", req.Limit)
	if err != nil {
		return nil, err
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.ListKeyVersionsRequest{
		KeyId:  req.KeyID,
		Limit:  limit,
		Cursor: req.Cursor,
	}

	pbResp, err := kmsClient.ListKeyVersions(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	versions := make([]*service.KeyVersionInfo, len(pbResp.Versions))
	for i, v := range pbResp.Versions {
		versions[i] = convertProtoKeyVersionInfo(pbResp.KeyId, v)
	}

	return &service.ListKeyVersionsResponse{
		KeyID:      pbResp.KeyId,
		Versions:   versions,
		NextCursor: pbResp.NextCursor,
		HasMore:    pbResp.HasMore,
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSExportKey exports a key wrapped with a wrapping key.
func (c *Client) KMSExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSExportKey", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.ExportKeyRequest{
		KeyId:             req.KeyID,
		WrappingKeyId:     req.WrappingKeyID,
		WrappingAlgorithm: convertServiceToProtoWrappingAlgorithm(req.WrappingAlgorithm),
		Format:            convertServiceToProtoExportFormat(req.Format),
		Version:           req.Version,
	}

	pbResp, err := kmsClient.ExportKey(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.ExportKeyResponse{
		KeyID:             pbResp.KeyId,
		Version:           pbResp.Version,
		WrappingKeyID:     pbResp.WrappingKeyId,
		WrappingAlgorithm: convertProtoToServiceWrappingAlgorithm(pbResp.WrappingAlgorithm),
		Format:            convertProtoToServiceExportFormat(pbResp.Format),
		WrappedKey:        pbResp.WrappedMaterial,
		Timestamp:         time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSUnwrapKey unwraps a wrapped key and imports it into the KMS.
func (c *Client) KMSUnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSUnwrapKey", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.UnwrapKeyRequest{
		KeyId:             req.KeyID,
		WrappingKeyId:     req.WrappingKeyID,
		WrappingAlgorithm: convertServiceToProtoWrappingAlgorithm(req.WrappingAlgorithm),
		WrappedMaterial:   req.WrappedKey,
		Algorithm:         convertServiceToProtoKeyAlgorithm(req.Algorithm),
		Name:              req.Name,
		SourceKeyId:       req.SourceKeyID,
		SourceVersion:     req.SourceVersion,
		Exportable:        req.KeyAttributes != nil && req.KeyAttributes.Exportable,
	}

	pbResp, err := kmsClient.UnwrapKey(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.UnwrapKeyResponse{
		KeyID:     pbResp.Key.GetKeyId(),
		Version:   pbResp.Key.GetVersion(),
		Algorithm: convertProtoToServiceKeyAlgorithm(pbResp.Key.GetAlgorithm()),
		Status:    convertProtoToServiceKeyStatus(pbResp.Key.GetStatus()),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSSetKeySafety sets key safety configuration.
func (c *Client) KMSSetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSSetKeySafety", "request")
	}

	warningPercent, err := clientInt32("warning_percent", req.WarningPercent)
	if err != nil {
		return nil, err
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.SetKeySafetyRequest{
		KeyId:          req.KeyID,
		MaxOperations:  req.MaxOperations,
		MaxBytes:       req.MaxBytes,
		WarningPercent: warningPercent,
	}

	pbResp, err := kmsClient.SetKeySafety(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.SetKeySafetyResponse{
		KeyID: pbResp.KeyId,
		Config: service.AEADSafetyConfigDTO{
			MaxOperations:  req.MaxOperations,
			MaxBytes:       req.MaxBytes,
			WarningPercent: req.WarningPercent,
		},
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSClearKeySafety clears key safety configuration.
func (c *Client) KMSClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSClearKeySafety", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.ClearKeySafetyRequest{
		KeyId: req.KeyID,
	}

	pbResp, err := kmsClient.ClearKeySafety(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.ClearKeySafetyResponse{
		KeyID:     pbResp.KeyId,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSGetSupportedAlgorithms returns the list of cryptographic algorithms supported by the current backend.
// Use this to check if specific algorithms like CHACHA20-POLY1305 are available before attempting operations.
func (c *Client) KMSGetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbResp, err := kmsClient.GetSupportedAlgorithms(ctx, &pb.GetSupportedAlgorithmsRequest{})
	if err != nil {
		return nil, convertError(err)
	}

	return pbResp.GetAlgorithms(), nil
}

// Helper functions to convert from proto to service types

func convertProtoKeyHandle(ph *pb.KeyHandle) *service.KeyHandle {
	if ph == nil {
		return nil
	}
	return &service.KeyHandle{
		KeyID:     ph.KeyId,
		Version:   ph.Version,
		Algorithm: convertProtoToServiceKeyAlgorithm(ph.Algorithm),
		Status:    convertProtoToServiceKeyStatus(ph.Status),
		CreatedAt: time.Unix(ph.CreatedAt, 0),
	}
}

func convertProtoKeyMetadata(pm *pb.KeyMetadata) *service.KeyMetadata {
	if pm == nil {
		return nil
	}

	var lastUsedAt *time.Time
	if pm.LastUsedAt > 0 {
		t := time.Unix(pm.LastUsedAt, 0)
		lastUsedAt = &t
	}

	return &service.KeyMetadata{
		KeyID:              pm.KeyId,
		Name:               pm.Name,
		Description:        pm.Description,
		Algorithm:          convertProtoToServiceKeyAlgorithm(pm.Algorithm),
		Status:             convertProtoToServiceKeyStatus(pm.Status),
		Usage:              convertProtoToServiceKeyUsageList(pm.Usage),
		Version:            pm.Version,
		Exportable:         pm.Exportable,
		DeletionProtection: pm.DeletionProtection,
		CreatedAt:          time.Unix(pm.CreatedAt, 0),
		UpdatedAt:          time.Unix(pm.UpdatedAt, 0),
		LastUsedAt:         lastUsedAt,
		Tags:               pm.Tags,
		Owner:              pm.Owner,
	}
}

func convertProtoToServiceKeyAlgorithm(alg pb.KeyAlgorithm) service.KeyAlgorithm {
	switch alg {
	case pb.KeyAlgorithm_KEY_ALG_AES_128_GCM:
		return service.KeyAlgAES128GCM
	case pb.KeyAlgorithm_KEY_ALG_AES_192_GCM:
		return service.KeyAlgAES192GCM
	case pb.KeyAlgorithm_KEY_ALG_AES_256_GCM:
		return service.KeyAlgAES256GCM
	case pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305:
		return service.KeyAlgChaCha20Poly1305
	case pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305:
		return service.KeyAlgXChaCha20Poly1305
	case pb.KeyAlgorithm_KEY_ALG_RSA_2048:
		return service.KeyAlgRSA2048
	case pb.KeyAlgorithm_KEY_ALG_RSA_3072:
		return service.KeyAlgRSA3072
	case pb.KeyAlgorithm_KEY_ALG_RSA_4096:
		return service.KeyAlgRSA4096
	case pb.KeyAlgorithm_KEY_ALG_ECDSA_P256:
		return service.KeyAlgECDSAP256
	case pb.KeyAlgorithm_KEY_ALG_ECDSA_P384:
		return service.KeyAlgECDSAP384
	case pb.KeyAlgorithm_KEY_ALG_ECDSA_P521:
		return service.KeyAlgECDSAP521
	case pb.KeyAlgorithm_KEY_ALG_ED25519:
		return service.KeyAlgEd25519
	case pb.KeyAlgorithm_KEY_ALG_ED448:
		return service.KeyAlgEd448
	case pb.KeyAlgorithm_KEY_ALG_X25519:
		return service.KeyAlgX25519
	case pb.KeyAlgorithm_KEY_ALG_X448:
		return service.KeyAlgX448
	case pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256:
		return service.KeyAlgHMACSHA256
	case pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384:
		return service.KeyAlgHMACSHA384
	case pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512:
		return service.KeyAlgHMACSHA512
	default:
		return ""
	}
}

func convertProtoToServiceKeyStatus(status pb.KeyStatus) service.KeyStatus {
	switch status {
	case pb.KeyStatus_KEY_STATUS_ENABLED:
		return service.KeyStatusEnabled
	case pb.KeyStatus_KEY_STATUS_DISABLED:
		return service.KeyStatusDisabled
	case pb.KeyStatus_KEY_STATUS_PENDING_DELETION:
		return service.KeyStatusPendingDeletion
	case pb.KeyStatus_KEY_STATUS_DELETED:
		return service.KeyStatusDeleted
	case pb.KeyStatus_KEY_STATUS_ROTATING:
		return service.KeyStatusRotating
	case pb.KeyStatus_KEY_STATUS_COMPROMISED:
		return service.KeyStatusCompromised
	default:
		return ""
	}
}

func convertProtoToServiceKeyUsageList(usages []pb.KeyUsage) []service.KeyUsage {
	result := make([]service.KeyUsage, len(usages))
	for i, usage := range usages {
		result[i] = convertProtoToServiceKeyUsage(usage)
	}
	return result
}

func convertProtoToServiceKeyUsage(usage pb.KeyUsage) service.KeyUsage {
	switch usage {
	case pb.KeyUsage_KEY_USAGE_ENCRYPT:
		return service.KeyUsageEncrypt
	case pb.KeyUsage_KEY_USAGE_DECRYPT:
		return service.KeyUsageDecrypt
	case pb.KeyUsage_KEY_USAGE_SIGN:
		return service.KeyUsageSign
	case pb.KeyUsage_KEY_USAGE_VERIFY:
		return service.KeyUsageVerify
	case pb.KeyUsage_KEY_USAGE_WRAP_KEY:
		return service.KeyUsageWrapKey
	case pb.KeyUsage_KEY_USAGE_DERIVE_KEY:
		return service.KeyUsageDeriveKey
	case pb.KeyUsage_KEY_USAGE_GENERATE_MAC:
		return service.KeyUsageGenerateMAC
	case pb.KeyUsage_KEY_USAGE_VERIFY_MAC:
		return service.KeyUsageVerifyMAC
	default:
		return ""
	}
}

func convertServiceToProtoKeyStatus(status service.KeyStatus) pb.KeyStatus {
	switch status {
	case service.KeyStatusEnabled:
		return pb.KeyStatus_KEY_STATUS_ENABLED
	case service.KeyStatusDisabled:
		return pb.KeyStatus_KEY_STATUS_DISABLED
	case service.KeyStatusPendingDeletion:
		return pb.KeyStatus_KEY_STATUS_PENDING_DELETION
	case service.KeyStatusDeleted:
		return pb.KeyStatus_KEY_STATUS_DELETED
	case service.KeyStatusRotating:
		return pb.KeyStatus_KEY_STATUS_ROTATING
	case service.KeyStatusCompromised:
		return pb.KeyStatus_KEY_STATUS_COMPROMISED
	default:
		return pb.KeyStatus_KEY_STATUS_UNSPECIFIED
	}
}

func convertProtoKeyVersionInfo(keyID string, v *pb.KeyVersionInfo) *service.KeyVersionInfo {
	if v == nil {
		return nil
	}

	info := &service.KeyVersionInfo{
		KeyID:     keyID,
		Version:   v.Version,
		Status:    convertProtoToServiceKeyStatus(v.Status),
		Algorithm: convertProtoToServiceKeyAlgorithm(v.Algorithm),
		CreatedAt: time.Unix(v.CreatedAt, 0),
	}

	if v.DisabledAt > 0 {
		disabledAt := time.Unix(v.DisabledAt, 0)
		info.DisabledAt = &disabledAt
	}

	return info
}

// convertKeyAttributesToProto converts go-xkms KeyAttributes to protobuf KeySpec values.
func convertKeyAttributesToProto(attrs *kctypes.KeyAttributes) (pb.KeyAlgorithm, int32, []pb.KeyUsage, bool, error) {
	if attrs == nil {
		return pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, 0, nil, false, api.NewNilInputError("convertKeyAttributesToProto", "key attributes")
	}

	var alg pb.KeyAlgorithm
	var keySize int32

	// Handle symmetric algorithms
	switch {
	case attrs.SymmetricAlgorithm != "":
		switch attrs.SymmetricAlgorithm {
		case kctypes.SymmetricAES128GCM:
			alg = pb.KeyAlgorithm_KEY_ALG_AES_128_GCM
			keySize = 16
		case kctypes.SymmetricAES192GCM:
			alg = pb.KeyAlgorithm_KEY_ALG_AES_192_GCM
			keySize = 24
		case kctypes.SymmetricAES256GCM:
			alg = pb.KeyAlgorithm_KEY_ALG_AES_256_GCM
			keySize = 32
		case kctypes.SymmetricChaCha20Poly1305:
			alg = pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305
			keySize = 32
		case kctypes.SymmetricXChaCha20Poly1305:
			alg = pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305
			keySize = 32
		default:
			return pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, 0, nil, false, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "convertKeyAttributesToProto",
				Err:  &api.InvalidInputError{Message: "unsupported symmetric algorithm: " + string(attrs.SymmetricAlgorithm)},
			}
		}
	case attrs.KeyAlgorithm != 0:
		// Handle asymmetric algorithms using KeyAlgorithm (x509.PublicKeyAlgorithm)
		switch attrs.KeyAlgorithm {
		case 1: // x509.RSA
			// Determine RSA key size from RSAAttributes or default to 4096
			rsaKeySize := 4096
			if attrs.RSAAttributes != nil && attrs.RSAAttributes.KeySize > 0 {
				rsaKeySize = attrs.RSAAttributes.KeySize
			}
			switch rsaKeySize {
			case 2048:
				alg = pb.KeyAlgorithm_KEY_ALG_RSA_2048
				keySize = 2048
			case 3072:
				alg = pb.KeyAlgorithm_KEY_ALG_RSA_3072
				keySize = 3072
			default:
				alg = pb.KeyAlgorithm_KEY_ALG_RSA_4096
				keySize = 4096
			}
		case 3: // x509.ECDSA
			// Determine ECDSA curve from ECCAttributes
			if attrs.ECCAttributes != nil && attrs.ECCAttributes.Curve != nil {
				curveName := attrs.ECCAttributes.Curve.Params().Name
				switch curveName {
				case "P-256":
					alg = pb.KeyAlgorithm_KEY_ALG_ECDSA_P256
					keySize = 256
				case "P-384":
					alg = pb.KeyAlgorithm_KEY_ALG_ECDSA_P384
					keySize = 384
				case "P-521":
					alg = pb.KeyAlgorithm_KEY_ALG_ECDSA_P521
					keySize = 521
				default:
					alg = pb.KeyAlgorithm_KEY_ALG_ECDSA_P256
					keySize = 256
				}
			} else {
				alg = pb.KeyAlgorithm_KEY_ALG_ECDSA_P256
				keySize = 256
			}
		case 4: // x509.Ed25519
			alg = pb.KeyAlgorithm_KEY_ALG_ED25519
			keySize = 256
		default:
			return pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, 0, nil, false, api.NewInvalidFormatError("key algorithm", &unsupportedKeyAlgorithmError{Algorithm: attrs.KeyAlgorithm})
		}
	default:
		return pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, 0, nil, false, api.NewInvalidInputError("either SymmetricAlgorithm or KeyAlgorithm must be set")
	}

	// Determine usage based on key type
	var usage []pb.KeyUsage
	if attrs.SymmetricAlgorithm != "" {
		usage = []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_ENCRYPT, pb.KeyUsage_KEY_USAGE_DECRYPT}
	} else {
		// Asymmetric: default to sign/verify unless specified otherwise
		usage = []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_SIGN, pb.KeyUsage_KEY_USAGE_VERIFY}
	}

	return alg, keySize, usage, attrs.Exportable, nil
}

// convertServiceToProtoWrappingAlgorithm converts service wrapping algorithm string to proto enum.
func convertServiceToProtoWrappingAlgorithm(alg string) pb.WrappingAlgorithm {
	switch alg {
	case "AES-KW":
		return pb.WrappingAlgorithm_WRAP_ALG_AES_KW
	case "AES-KWP":
		return pb.WrappingAlgorithm_WRAP_ALG_AES_KWP
	case "AES-GCM":
		return pb.WrappingAlgorithm_WRAP_ALG_AES_GCM
	case "RSA-OAEP-SHA256":
		return pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA256
	case "RSA-OAEP-SHA384":
		return pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA384
	case "RSA-OAEP-SHA512":
		return pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA512
	case "ECDH-ES+A256KW":
		return pb.WrappingAlgorithm_WRAP_ALG_ECDH_ES_A256KW
	default:
		return pb.WrappingAlgorithm_WRAP_ALG_UNSPECIFIED
	}
}

// convertProtoToServiceWrappingAlgorithm converts proto enum to service wrapping algorithm string.
func convertProtoToServiceWrappingAlgorithm(alg pb.WrappingAlgorithm) string {
	switch alg {
	case pb.WrappingAlgorithm_WRAP_ALG_AES_KW:
		return "AES-KW"
	case pb.WrappingAlgorithm_WRAP_ALG_AES_KWP:
		return "AES-KWP"
	case pb.WrappingAlgorithm_WRAP_ALG_AES_GCM:
		return "AES-GCM"
	case pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA256:
		return "RSA-OAEP-SHA256"
	case pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA384:
		return "RSA-OAEP-SHA384"
	case pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA512:
		return "RSA-OAEP-SHA512"
	case pb.WrappingAlgorithm_WRAP_ALG_ECDH_ES_A256KW:
		return "ECDH-ES+A256KW"
	default:
		return ""
	}
}

// convertServiceToProtoExportFormat converts service export format string to proto enum.
func convertServiceToProtoExportFormat(format string) pb.ExportFormat {
	switch format {
	case "raw":
		return pb.ExportFormat_EXPORT_FORMAT_RAW
	case "jwk":
		return pb.ExportFormat_EXPORT_FORMAT_JWK
	case "pem":
		return pb.ExportFormat_EXPORT_FORMAT_PEM
	default:
		return pb.ExportFormat_EXPORT_FORMAT_UNSPECIFIED
	}
}

// convertProtoToServiceExportFormat converts proto enum to service export format string.
func convertProtoToServiceExportFormat(format pb.ExportFormat) string {
	switch format {
	case pb.ExportFormat_EXPORT_FORMAT_RAW:
		return "raw"
	case pb.ExportFormat_EXPORT_FORMAT_JWK:
		return "jwk"
	case pb.ExportFormat_EXPORT_FORMAT_PEM:
		return "pem"
	default:
		return ""
	}
}

// KMSCreateDEK creates a new data encryption key.
func (c *Client) KMSCreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSCreateDEK", "request")
	}

	warningPercent, err := clientInt32("warning_percent", req.WarningPercent)
	if err != nil {
		return nil, err
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	dekClient := pb.NewDEKManagementClient(c.conn)

	pbReq := &pb.CreateDEKRequest{
		Path:           req.Path,
		Algorithm:      convertServiceKeyAlgorithmToProto(req.Algorithm),
		Description:    req.Description,
		MaxOperations:  req.MaxOperations,
		MaxBytes:       req.MaxBytes,
		WarningPercent: warningPercent,
	}

	pbResp, err := dekClient.CreateDEK(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.CreateDEKResponse{
		DEK:       convertProtoDEKMetadata(pbResp.Dek),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSGetDEK retrieves a DEK by path.
func (c *Client) KMSGetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGetDEK", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	dekClient := pb.NewDEKManagementClient(c.conn)

	pbReq := &pb.GetDEKRequest{
		Path: req.Path,
	}

	pbResp, err := dekClient.GetDEK(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.GetDEKResponse{
		DEK:       convertProtoDEKMetadata(pbResp.Dek),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSListDEKs lists DEKs matching optional filters.
func (c *Client) KMSListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	if req == nil {
		req = &service.ListDEKsRequest{}
	}

	limit, err := clientInt32("limit", req.Limit)
	if err != nil {
		return nil, err
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	dekClient := pb.NewDEKManagementClient(c.conn)

	pbReq := &pb.ListDEKsRequest{
		Prefix:    req.Prefix,
		Recursive: req.Recursive,
		Limit:     limit,
		Cursor:    req.Cursor,
	}

	pbResp, err := dekClient.ListDEKs(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	deks := make([]*service.DEKMetadata, len(pbResp.Deks))
	for i, dek := range pbResp.Deks {
		deks[i] = convertProtoDEKMetadata(dek)
	}

	return &service.ListDEKsResponse{
		DEKs:       deks,
		NextCursor: pbResp.NextCursor,
		HasMore:    pbResp.HasMore,
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSRotateDEK rotates a DEK.
func (c *Client) KMSRotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSRotateDEK", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	dekClient := pb.NewDEKManagementClient(c.conn)

	pbReq := &pb.RotateDEKRequest{
		Path:       req.Path,
		Background: req.Background,
	}

	pbResp, err := dekClient.RotateDEK(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.RotateDEKResponse{
		Path:       pbResp.Path,
		OldVersion: pbResp.OldVersion,
		NewVersion: pbResp.NewVersion,
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSDeleteDEK deletes a DEK.
func (c *Client) KMSDeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSDeleteDEK", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	dekClient := pb.NewDEKManagementClient(c.conn)

	pbReq := &pb.DeleteDEKRequest{
		Path:      req.Path,
		Force:     req.Force,
		MigrateTo: req.MigrateTo,
	}

	pbResp, err := dekClient.DeleteDEK(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.DeleteDEKResponse{
		Path:      pbResp.Path,
		Deleted:   pbResp.Deleted,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSGetDEKStatus returns status of a DEK including re-encryption progress.
func (c *Client) KMSGetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGetDEKStatus", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	dekClient := pb.NewDEKManagementClient(c.conn)

	pbReq := &pb.GetDEKStatusRequest{
		Path: req.Path,
	}

	pbResp, err := dekClient.GetDEKStatus(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	resp := &service.GetDEKStatusResponse{
		DEK:       convertProtoDEKMetadata(pbResp.Dek),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}

	progress, err := convertProtoReencryptionProgress(pbResp.ReencryptionProgress)
	if err != nil {
		return nil, err
	}
	resp.ReencryptionProgress = progress

	return resp, nil
}

// KMSSetDefaultDEK sets a DEK as the default for a path.
func (c *Client) KMSSetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSSetDefaultDEK", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	dekClient := pb.NewDEKManagementClient(c.conn)

	pbReq := &pb.SetDefaultDEKRequest{
		Path: req.Path,
	}

	pbResp, err := dekClient.SetDefaultDEK(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.SetDefaultDEKResponse{
		Path:            pbResp.Path,
		PreviousDefault: pbResp.PreviousDefault,
		Timestamp:       time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSSetDEKSafety sets AEAD safety limits for a DEK.
func (c *Client) KMSSetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSSetDEKSafety", "request")
	}

	warningPercent, err := clientInt32("warning_percent", req.WarningPercent)
	if err != nil {
		return nil, err
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	dekClient := pb.NewDEKManagementClient(c.conn)

	pbReq := &pb.SetDEKSafetyRequest{
		Path:           req.Path,
		MaxOperations:  req.MaxOperations,
		MaxBytes:       req.MaxBytes,
		WarningPercent: warningPercent,
	}

	pbResp, err := dekClient.SetDEKSafety(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.SetDEKSafetyResponse{
		Path: pbResp.Path,
		Config: &service.DEKSafetyConfig{
			MaxOperations:  req.MaxOperations,
			MaxBytes:       req.MaxBytes,
			WarningPercent: req.WarningPercent,
		},
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSClearDEKSafety clears AEAD safety limits for a DEK.
func (c *Client) KMSClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSClearDEKSafety", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	dekClient := pb.NewDEKManagementClient(c.conn)

	pbReq := &pb.ClearDEKSafetyRequest{
		Path: req.Path,
	}

	pbResp, err := dekClient.ClearDEKSafety(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.ClearDEKSafetyResponse{
		Path:      pbResp.Path,
		Cleared:   true,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSSafetyCheck performs a safety check on all keys and DEKs.
func (c *Client) KMSSafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	if req == nil {
		req = &service.SafetyCheckRequest{}
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbReq := &pb.SafetyCheckRequest{}

	pbResp, err := kmsClient.SafetyCheck(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	// Convert warnings to items
	items := make([]service.SafetyCheckItem, len(pbResp.Warnings))
	for i, warning := range pbResp.Warnings {
		items[i] = service.SafetyCheckItem{
			ID:             warning.Path,
			Type:           warning.Type,
			OperationCount: warning.OperationsUsed,
			BytesProcessed: warning.BytesUsed,
			UsagePercent:   warning.UsagePercent,
			MaxOperations:  warning.MaxOperations,
			MaxBytes:       warning.MaxBytes,
		}
	}

	return &service.SafetyCheckResponse{
		Items:            items,
		ThresholdPercent: req.ThresholdPercent, // Pass through from request
		TotalKeysChecked: 0,                    // Not available in proto
		TotalDEKsChecked: 0,                    // Not available in proto
		Timestamp:        time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSSafetyReport generates a safety report for all keys and DEKs.
func (c *Client) KMSSafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	if req == nil {
		req = &service.SafetyReportRequest{}
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	kmsClient := pb.NewKeyManagementClient(c.conn)

	// Proto uses format field, convert includeDetails to format
	format := "json"
	if req.IncludeDetails {
		format = "table"
	}
	pbReq := &pb.SafetyReportRequest{
		Format: format,
	}

	pbResp, err := kmsClient.SafetyReport(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	// Convert all_items to items
	items := make([]service.SafetyCheckItem, len(pbResp.AllItems))
	for i, item := range pbResp.AllItems {
		items[i] = service.SafetyCheckItem{
			ID:             item.Path,
			Type:           item.Type,
			OperationCount: item.OperationsUsed,
			BytesProcessed: item.BytesUsed,
			UsagePercent:   item.UsagePercent,
			MaxOperations:  item.MaxOperations,
			MaxBytes:       item.MaxBytes,
		}
	}

	// Calculate totals from response
	var totalOps, totalBytes uint64
	for _, item := range pbResp.AllItems {
		totalOps += item.OperationsUsed
		totalBytes += item.BytesUsed
	}

	return &service.SafetyReportResponse{
		Items:               items,
		TotalOperations:     totalOps,
		TotalBytesProcessed: totalBytes,
		ServerConfig:        service.AEADServerConfig{}, // Not available in proto response
		Timestamp:           time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSStartReencryption starts background re-encryption of data with a new DEK version.
func (c *Client) KMSStartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSStartReencryption", "request")
	}

	batchSize, err := clientInt32("batch_size", req.BatchSize)
	if err != nil {
		return nil, err
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	dekClient := pb.NewDEKManagementClient(c.conn)

	pbReq := &pb.StartReencryptionRequest{
		Path:        req.Path,
		FromVersion: req.FromVersion,
		ToVersion:   req.ToVersion,
		BatchSize:   batchSize,
		Force:       req.Force,
	}

	pbResp, err := dekClient.StartReencryption(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.StartReencryptionResponse{
		OperationID:    pbResp.OperationId,
		Started:        pbResp.Started,
		Path:           pbResp.Path,
		EstimatedItems: int(pbResp.EstimatedItems),
		Timestamp:      time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSGetReencryptionProgress returns the current progress of re-encryption operation.
func (c *Client) KMSGetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGetReencryptionProgress", "request")
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	dekClient := pb.NewDEKManagementClient(c.conn)

	pbReq := &pb.GetReencryptionProgressRequest{
		Path:        req.Path,
		OperationId: req.OperationID,
	}

	pbResp, err := dekClient.GetReencryptionProgress(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	progress, err := convertProtoReencryptionProgress(pbResp.Progress)
	if err != nil {
		return nil, err
	}

	return &service.GetReencryptionProgressResponse{
		OperationID: pbResp.OperationId,
		Path:        pbResp.Path,
		Status:      pbResp.Status,
		Progress:    progress,
		Error:       pbResp.Error,
		Timestamp:   time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// convertProtoDEKMetadata converts proto DEKMetadata to service DEKMetadata.
func convertProtoDEKMetadata(pm *pb.DEKMetadata) *service.DEKMetadata {
	if pm == nil {
		return nil
	}

	dek := &service.DEKMetadata{
		Path:           pm.Path,
		Algorithm:      convertProtoToServiceKeyAlgorithm(pm.Algorithm),
		Version:        pm.Version,
		Status:         pm.Status,
		IsDefault:      pm.IsDefault,
		CreatedAt:      time.Unix(pm.CreatedAt, 0),
		UsageCount:     pm.UsageCount,
		BytesUsed:      pm.BytesUsed,
		MaxUsageCount:  pm.MaxUsageCount,
		MaxBytesUsed:   pm.MaxBytesUsed,
		WarningPercent: int(pm.WarningPercent),
		Description:    pm.Description,
		BackendType:    pm.BackendType,
	}

	if pm.RotatedAt > 0 {
		rotatedAt := time.Unix(pm.RotatedAt, 0)
		dek.RotatedAt = &rotatedAt
	}

	return dek
}

// convertProtoReencryptionProgress converts proto ReencryptionProgress to
// service ReencryptionProgress. The proto carries the key counts as int32; a
// negative count is a malformed response and is returned as a decode error.
func convertProtoReencryptionProgress(pm *pb.ReencryptionProgress) (*service.ReencryptionProgress, error) {
	if pm == nil {
		return nil, nil
	}

	totalKeys, totalErr := conv.SafeInt64ToUint64(int64(pm.TotalKeys))
	processedKeys, processedErr := conv.SafeInt64ToUint64(int64(pm.ProcessedKeys))
	failedKeys, failedErr := conv.SafeInt64ToUint64(int64(pm.FailedKeys))
	if err := stderrors.Join(totalErr, processedErr, failedErr); err != nil {
		return nil, api.NewDecodeError("Client.convertProtoReencryptionProgress", "reencryption progress", err)
	}

	result := &service.ReencryptionProgress{
		InProgress:    pm.InProgress,
		TotalKeys:     totalKeys,
		ProcessedKeys: processedKeys,
		FailedKeys:    failedKeys,
	}

	if pm.StartedAt > 0 {
		startedAt := time.Unix(pm.StartedAt, 0)
		result.StartedAt = &startedAt
	}

	if pm.EstimatedCompletion > 0 {
		estimatedCompletion := time.Unix(pm.EstimatedCompletion, 0)
		result.EstimatedCompletion = &estimatedCompletion
	}

	// Calculate percent complete
	if pm.TotalKeys > 0 {
		result.PercentComplete = float64(pm.ProcessedKeys) / float64(pm.TotalKeys) * 100
	}

	return result, nil
}

// convertServiceKeyAlgorithmToProto converts service.KeyAlgorithm to pb.KeyAlgorithm.
func convertServiceKeyAlgorithmToProto(alg service.KeyAlgorithm) pb.KeyAlgorithm {
	switch alg {
	case service.KeyAlgAES128GCM:
		return pb.KeyAlgorithm_KEY_ALG_AES_128_GCM
	case service.KeyAlgAES192GCM:
		return pb.KeyAlgorithm_KEY_ALG_AES_192_GCM
	case service.KeyAlgAES256GCM:
		return pb.KeyAlgorithm_KEY_ALG_AES_256_GCM
	case service.KeyAlgChaCha20Poly1305:
		return pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305
	case service.KeyAlgXChaCha20Poly1305:
		return pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305
	case service.KeyAlgRSA2048:
		return pb.KeyAlgorithm_KEY_ALG_RSA_2048
	case service.KeyAlgRSA3072:
		return pb.KeyAlgorithm_KEY_ALG_RSA_3072
	case service.KeyAlgRSA4096:
		return pb.KeyAlgorithm_KEY_ALG_RSA_4096
	case service.KeyAlgECDSAP256:
		return pb.KeyAlgorithm_KEY_ALG_ECDSA_P256
	case service.KeyAlgECDSAP384:
		return pb.KeyAlgorithm_KEY_ALG_ECDSA_P384
	case service.KeyAlgECDSAP521:
		return pb.KeyAlgorithm_KEY_ALG_ECDSA_P521
	case service.KeyAlgEd25519:
		return pb.KeyAlgorithm_KEY_ALG_ED25519
	case service.KeyAlgEd448:
		return pb.KeyAlgorithm_KEY_ALG_ED448
	case service.KeyAlgX25519:
		return pb.KeyAlgorithm_KEY_ALG_X25519
	case service.KeyAlgX448:
		return pb.KeyAlgorithm_KEY_ALG_X448
	case service.KeyAlgHMACSHA256:
		return pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256
	case service.KeyAlgHMACSHA384:
		return pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384
	case service.KeyAlgHMACSHA512:
		return pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512
	default:
		return pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED
	}
}

// convertServiceToProtoKeyAlgorithm converts service key algorithm string to proto enum.
func convertServiceToProtoKeyAlgorithm(alg string) pb.KeyAlgorithm {
	switch alg {
	case "AES-128-GCM":
		return pb.KeyAlgorithm_KEY_ALG_AES_128_GCM
	case "AES-192-GCM":
		return pb.KeyAlgorithm_KEY_ALG_AES_192_GCM
	case "AES-256-GCM":
		return pb.KeyAlgorithm_KEY_ALG_AES_256_GCM
	case "CHACHA20-POLY1305":
		return pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305
	case "XCHACHA20-POLY1305":
		return pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305
	case "RSA-2048":
		return pb.KeyAlgorithm_KEY_ALG_RSA_2048
	case "RSA-3072":
		return pb.KeyAlgorithm_KEY_ALG_RSA_3072
	case "RSA-4096":
		return pb.KeyAlgorithm_KEY_ALG_RSA_4096
	case "ECDSA-P256":
		return pb.KeyAlgorithm_KEY_ALG_ECDSA_P256
	case "ECDSA-P384":
		return pb.KeyAlgorithm_KEY_ALG_ECDSA_P384
	case "ECDSA-P521":
		return pb.KeyAlgorithm_KEY_ALG_ECDSA_P521
	case "ED25519":
		return pb.KeyAlgorithm_KEY_ALG_ED25519
	case "ED448":
		return pb.KeyAlgorithm_KEY_ALG_ED448
	case "X25519":
		return pb.KeyAlgorithm_KEY_ALG_X25519
	case "X448":
		return pb.KeyAlgorithm_KEY_ALG_X448
	case "HMAC-SHA256":
		return pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256
	case "HMAC-SHA384":
		return pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384
	case "HMAC-SHA512":
		return pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512
	default:
		return pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED
	}
}

// unsupportedKeyAlgorithmError indicates an unsupported public key algorithm was used.
type unsupportedKeyAlgorithmError struct {
	Algorithm interface{}
}

// Error implements the error interface.
func (e *unsupportedKeyAlgorithmError) Error() string {
	return "unsupported key algorithm"
}

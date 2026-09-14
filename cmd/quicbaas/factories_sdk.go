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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	operationsSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/operations"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// =============================================================================
// SDK Crypto Client
// =============================================================================

// sdkCryptoClient implements the CryptoClient interface using SDK transport.
type sdkCryptoClient struct {
	sdk quicbaas.Client
}

// createSDKCryptoClient creates an SDK client for crypto operations.
func createSDKCryptoClient() (CryptoClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &sdkCryptoClient{sdk: sdk}, nil
}

// KMSEncrypt encrypts plaintext using the specified key.
func (c *sdkCryptoClient) KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Crypto", "Encrypt", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.EncryptResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkCryptoClient.KMSEncrypt", "unexpected response type")
	}
	return result, nil
}

// KMSDecrypt decrypts ciphertext using the specified key.
func (c *sdkCryptoClient) KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Crypto", "Decrypt", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.DecryptResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkCryptoClient.KMSDecrypt", "unexpected response type")
	}
	return result, nil
}

// KMSSign signs data using the specified key.
func (c *sdkCryptoClient) KMSSign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Crypto", "Sign", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.SignResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkCryptoClient.KMSSign", "unexpected response type")
	}
	return result, nil
}

// KMSVerify verifies a signature using the specified key.
func (c *sdkCryptoClient) KMSVerify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Crypto", "Verify", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.VerifyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkCryptoClient.KMSVerify", "unexpected response type")
	}
	return result, nil
}

// KMSHash computes a cryptographic hash.
func (c *sdkCryptoClient) KMSHash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Crypto", "Hash", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.HashResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkCryptoClient.KMSHash", "unexpected response type")
	}
	return result, nil
}

// KMSGenerateHMAC generates an HMAC using the specified key.
func (c *sdkCryptoClient) KMSGenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Crypto", "GenerateHMAC", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.HMACResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkCryptoClient.KMSGenerateHMAC", "unexpected response type")
	}
	return result, nil
}

// KMSVerifyHMAC verifies an HMAC using the specified key.
func (c *sdkCryptoClient) KMSVerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Crypto", "VerifyHMAC", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.VerifyHMACResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkCryptoClient.KMSVerifyHMAC", "unexpected response type")
	}
	return result, nil
}

// KMSRewrap re-encrypts data with the latest key version.
func (c *sdkCryptoClient) KMSRewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Crypto", "Rewrap", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.RewrapResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkCryptoClient.KMSRewrap", "unexpected response type")
	}
	return result, nil
}

// KMSGenerateDataKey generates a new data encryption key.
func (c *sdkCryptoClient) KMSGenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Crypto", "GenerateDataKey", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.GenerateDataKeyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkCryptoClient.KMSGenerateDataKey", "unexpected response type")
	}
	return result, nil
}

// KMSGenerateRandom generates cryptographically secure random bytes.
func (c *sdkCryptoClient) KMSGenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Crypto", "GenerateRandom", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.GenerateRandomResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkCryptoClient.KMSGenerateRandom", "unexpected response type")
	}
	return result, nil
}

// Close closes the underlying SDK client.
func (c *sdkCryptoClient) Close() error {
	return c.sdk.Close()
}

// =============================================================================
// SDK KMS Client
// =============================================================================

// sdkKMSClient implements the KMSClient interface using SDK transport.
type sdkKMSClient struct {
	sdk quicbaas.Client
}

// createSDKKMSClient creates an SDK client for KMS operations.
func createSDKKMSClient() (KMSClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &sdkKMSClient{sdk: sdk}, nil
}

// KMSGenerateKey generates a new encryption key.
func (c *sdkKMSClient) KMSGenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "GenerateKey", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.GenerateKeyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSGenerateKey", "unexpected response type")
	}
	return result, nil
}

// KMSImportKey imports an existing key.
func (c *sdkKMSClient) KMSImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "ImportKey", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.ImportKeyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSImportKey", "unexpected response type")
	}
	return result, nil
}

// KMSGetKey retrieves key metadata.
func (c *sdkKMSClient) KMSGetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "GetKey", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.GetKeyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSGetKey", "unexpected response type")
	}
	return result, nil
}

// KMSListKeys lists available keys.
func (c *sdkKMSClient) KMSListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "ListKeys", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.ListKeysResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSListKeys", "unexpected response type")
	}
	return result, nil
}

// KMSRotateKey rotates a key to a new version.
func (c *sdkKMSClient) KMSRotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "RotateKey", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.RotateKeyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSRotateKey", "unexpected response type")
	}
	return result, nil
}

// KMSDeleteKey deletes a key.
func (c *sdkKMSClient) KMSDeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "DeleteKey", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.DeleteKeyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSDeleteKey", "unexpected response type")
	}
	return result, nil
}

// KMSUpdateKeyStatus updates the status of a key.
func (c *sdkKMSClient) KMSUpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "UpdateKeyStatus", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.UpdateKeyStatusResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSUpdateKeyStatus", "unexpected response type")
	}
	return result, nil
}

// KMSListKeyVersions lists all versions of a key.
func (c *sdkKMSClient) KMSListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "ListKeyVersions", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.ListKeyVersionsResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSListKeyVersions", "unexpected response type")
	}
	return result, nil
}

// KMSExportKey exports a key in wrapped form.
func (c *sdkKMSClient) KMSExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "ExportKey", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.ExportKeyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSExportKey", "unexpected response type")
	}
	return result, nil
}

// KMSUnwrapKey unwraps a wrapped key.
func (c *sdkKMSClient) KMSUnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "UnwrapKey", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.UnwrapKeyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSUnwrapKey", "unexpected response type")
	}
	return result, nil
}

// KMSSetKeySafety sets safety limits on a key.
func (c *sdkKMSClient) KMSSetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "SetKeySafety", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.SetKeySafetyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSSetKeySafety", "unexpected response type")
	}
	return result, nil
}

// KMSClearKeySafety clears safety limits on a key.
func (c *sdkKMSClient) KMSClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "ClearKeySafety", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.ClearKeySafetyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSClearKeySafety", "unexpected response type")
	}
	return result, nil
}

// KMSEncrypt encrypts plaintext using the specified key.
func (c *sdkKMSClient) KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "Encrypt", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.EncryptResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSEncrypt", "unexpected response type")
	}
	return result, nil
}

// KMSDecrypt decrypts ciphertext using the specified key.
func (c *sdkKMSClient) KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.KMS", "Decrypt", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.DecryptResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkKMSClient.KMSDecrypt", "unexpected response type")
	}
	return result, nil
}

// Close closes the underlying SDK client.
func (c *sdkKMSClient) Close() error {
	return c.sdk.Close()
}

// =============================================================================
// SDK Seal Client
// =============================================================================

// sdkSealClient implements the SealClient interface using SDK transport.
type sdkSealClient struct {
	sdk quicbaas.Client
}

// createSDKSealClient creates an SDK client for seal operations.
func createSDKSealClient() (SealClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &sdkSealClient{sdk: sdk}, nil
}

// SealInit initializes the seal and generates key shares.
func (c *sdkSealClient) SealInit(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Seal", "Init", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.SealInitResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkSealClient.SealInit", "unexpected response type")
	}
	return result, nil
}

// Unseal provides a key share to unseal the database.
func (c *sdkSealClient) Unseal(ctx context.Context, req *service.UnsealRequest) (*service.UnsealResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Seal", "Unseal", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.UnsealResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkSealClient.Unseal", "unexpected response type")
	}
	return result, nil
}

// Seal re-seals the database.
func (c *sdkSealClient) Seal(ctx context.Context) error {
	_, err := c.sdk.Transport().Request(ctx, "quicbaas.Seal", "Seal", nil)
	return err
}

// SealStatus returns the current seal status.
func (c *sdkSealClient) SealStatus(ctx context.Context) (*service.SealStatusResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Seal", "Status", nil)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.SealStatusResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkSealClient.SealStatus", "unexpected response type")
	}
	return result, nil
}

// Rekey generates new key shares with a new threshold configuration.
func (c *sdkSealClient) Rekey(ctx context.Context, req *service.RekeyRequest) (*service.RekeyResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Seal", "Rekey", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.RekeyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkSealClient.Rekey", "unexpected response type")
	}
	return result, nil
}

// Close closes the underlying SDK client.
func (c *sdkSealClient) Close() error {
	return c.sdk.Close()
}

// =============================================================================
// SDK Backend Client
// =============================================================================

// sdkBackendClient implements the BackendClient interface using SDK transport.
type sdkBackendClient struct {
	sdk quicbaas.Client
}

// createSDKBackendClient creates an SDK client for backend operations.
func createSDKBackendClient() (BackendClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &sdkBackendClient{sdk: sdk}, nil
}

// KMSBackendStatus returns the status of the KMS backend.
func (c *sdkBackendClient) KMSBackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Backend", "Status", nil)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.BackendStatusResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkBackendClient.KMSBackendStatus", "unexpected response type")
	}
	return result, nil
}

// KMSBackendTest runs a test against the KMS backend.
func (c *sdkBackendClient) KMSBackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Backend", "Test", nil)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.BackendTestResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkBackendClient.KMSBackendTest", "unexpected response type")
	}
	return result, nil
}

// Close closes the underlying SDK client.
func (c *sdkBackendClient) Close() error {
	return c.sdk.Close()
}

// =============================================================================
// SDK Safety Client
// =============================================================================

// sdkSafetyClient implements the SafetyClient interface using SDK transport.
type sdkSafetyClient struct {
	sdk quicbaas.Client
}

// createSDKSafetyClient creates an SDK client for safety operations.
func createSDKSafetyClient() (SafetyClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &sdkSafetyClient{sdk: sdk}, nil
}

// KMSSafetyCheck performs a safety check on keys and DEKs.
func (c *sdkSafetyClient) KMSSafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Safety", "Check", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.SafetyCheckResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkSafetyClient.KMSSafetyCheck", "unexpected response type")
	}
	return result, nil
}

// KMSSafetyReport generates a safety report.
func (c *sdkSafetyClient) KMSSafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Safety", "Report", req)
	if err != nil {
		return nil, err
	}
	result, ok := resp.(*service.SafetyReportResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkSafetyClient.KMSSafetyReport", "unexpected response type")
	}
	return result, nil
}

// Close closes the underlying SDK client.
func (c *sdkSafetyClient) Close() error {
	return c.sdk.Close()
}

// =============================================================================
// SDK Backup Client
// =============================================================================

// sdkBackupClient implements the BackupClient interface using SDK transport.
type sdkBackupClient struct {
	sdk quicbaas.Client
}

// createSDKBackupClient creates an SDK client for backup operations.
func createSDKBackupClient() (BackupClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &sdkBackupClient{sdk: sdk}, nil
}

// CreateBackup creates a new backup.
func (c *sdkBackupClient) CreateBackup(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error) {
	svcReq := &service.CreateBackupRequest{
		Name:        req.Name,
		Compression: req.Compression,
		Encryption:  req.Encryption,
	}

	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Backup", "Create", svcReq)
	if err != nil {
		return nil, err
	}

	result, ok := resp.(*service.CreateBackupResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkBackupClient.CreateBackup", "unexpected response type")
	}

	return &cli.BackupCreateResponse{
		BackupID:    result.BackupID,
		Type:        req.BackupType,
		Size:        int64(result.Size),
		Compression: req.Compression,
		Encryption:  req.Encryption,
		Timestamp:   time.Now(),
		Success:     result.Status == "completed" || result.Status == "success",
		Message:     result.Status,
	}, nil
}

// ListBackups lists available backups.
func (c *sdkBackupClient) ListBackups(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
	svcReq := &service.ListBackupsRequest{
		Limit:  uint64(req.Limit),
		Offset: uint64(req.Offset),
	}

	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Backup", "List", svcReq)
	if err != nil {
		return nil, err
	}

	result, ok := resp.(*service.ListBackupsResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkBackupClient.ListBackups", "unexpected response type")
	}

	backups := make([]cli.BackupInfo, 0, len(result.Backups))
	for _, b := range result.Backups {
		backups = append(backups, cli.BackupInfo{
			ID:        b.BackupID,
			Size:      int64(b.Size),
			Timestamp: parseTimestamp(b.Timestamp),
		})
	}

	return &cli.BackupListResponse{
		Backups:     backups,
		TotalCount:  int(result.TotalCount),
		StoragePath: req.StoragePath,
	}, nil
}

// RestoreBackup restores a backup.
func (c *sdkBackupClient) RestoreBackup(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
	svcReq := &service.RestoreBackupRequest{
		BackupID: req.BackupID,
	}

	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Backup", "Restore", svcReq)
	if err != nil {
		return nil, err
	}

	result, ok := resp.(*service.RestoreBackupResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkBackupClient.RestoreBackup", "unexpected response type")
	}

	return &cli.BackupRestoreResponse{
		BackupID:  req.BackupID,
		Timestamp: time.Now(),
		Success:   result.Status == "completed" || result.Status == "success",
		Message:   result.Status,
	}, nil
}

// DeleteBackup deletes a backup.
func (c *sdkBackupClient) DeleteBackup(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
	svcReq := &service.DeleteBackupRequest{
		BackupID: req.BackupID,
	}

	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Backup", "Delete", svcReq)
	if err != nil {
		return nil, err
	}

	result, ok := resp.(*service.DeleteBackupResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkBackupClient.DeleteBackup", "unexpected response type")
	}

	return &cli.BackupDeleteResponse{
		BackupID:  req.BackupID,
		Deleted:   result.Success,
		Message:   result.Message,
		Timestamp: time.Now(),
	}, nil
}

// VerifyBackup verifies a backup.
func (c *sdkBackupClient) VerifyBackup(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
	svcReq := &service.VerifyBackupRequest{
		BackupID: req.BackupID,
	}

	resp, err := c.sdk.Transport().Request(ctx, "quicbaas.Backup", "Verify", svcReq)
	if err != nil {
		return nil, err
	}

	result, ok := resp.(*service.VerifyBackupResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("sdkBackupClient.VerifyBackup", "unexpected response type")
	}

	return &cli.BackupVerifyResponse{
		BackupID:      req.BackupID,
		Valid:         result.Valid,
		ChecksumValid: result.ChecksumMatch,
		Issues:        result.Issues,
		Timestamp:     time.Now(),
	}, nil
}

// Close closes the underlying SDK client.
func (c *sdkBackupClient) Close() error {
	return c.sdk.Close()
}

// =============================================================================
// =============================================================================
// SDK Orchestrator Client
// =============================================================================

// sdkOrchestratorClient implements the OrchestratorClient interface using the SDK.
type sdkOrchestratorClient struct {
	sdk quicbaas.Client
}

// createOrchestratorSDKClient creates an SDK client for orchestrator operations.
func createOrchestratorSDKClient() (OrchestratorClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &sdkOrchestratorClient{sdk: sdk}, nil
}

// Health performs an orchestrator health check.
func (c *sdkOrchestratorClient) Health(ctx context.Context) (*orchestratorSDK.HealthCheckReport, error) {
	return c.sdk.Orchestrator().Health(ctx)
}

// ListBackends lists all registered agent backends.
func (c *sdkOrchestratorClient) ListBackends(ctx context.Context) (*orchestratorSDK.ListBackendsResponse, error) {
	return c.sdk.Orchestrator().ListBackends(ctx)
}

// ListBackendModels lists the model names available on the named backend
// as seen from the specified agent. AgentID is used as a host affinity hint
// for the remote backend resolver; pass an empty string to skip host affinity.
func (c *sdkOrchestratorClient) ListBackendModels(ctx context.Context, req *orchestratorSDK.ListBackendModelsRequest) (*orchestratorSDK.ListBackendModelsResponse, error) {
	return c.sdk.Orchestrator().ListBackendModels(ctx, req)
}

// SpawnSession creates a new agent session.
func (c *sdkOrchestratorClient) SpawnSession(ctx context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
	return c.sdk.Orchestrator().SpawnSession(ctx, req)
}

// PromptSession sends a prompt to an existing session.
func (c *sdkOrchestratorClient) PromptSession(ctx context.Context, req *orchestratorSDK.PromptSessionRequest) (*orchestratorSDK.PromptSessionResponse, error) {
	return c.sdk.Orchestrator().PromptSession(ctx, req)
}

// SessionStatus retrieves the status of a session.
func (c *sdkOrchestratorClient) SessionStatus(ctx context.Context, req *orchestratorSDK.SessionStatusRequest) (*orchestratorSDK.SessionStatusResponse, error) {
	return c.sdk.Orchestrator().SessionStatus(ctx, req)
}

// CloseSession terminates a session.
func (c *sdkOrchestratorClient) CloseSession(ctx context.Context, req *orchestratorSDK.CloseSessionRequest) (*orchestratorSDK.CloseSessionResponse, error) {
	return c.sdk.Orchestrator().CloseSession(ctx, req)
}

// RotateSessionAccount moves a session onto a different pooled provider account.
func (c *sdkOrchestratorClient) RotateSessionAccount(ctx context.Context, req *orchestratorSDK.RotateSessionAccountRequest) (*orchestratorSDK.RotateSessionAccountResponse, error) {
	return c.sdk.Orchestrator().RotateSessionAccount(ctx, req)
}

// ListSessions returns all active sessions.
func (c *sdkOrchestratorClient) ListSessions(ctx context.Context, req *orchestratorSDK.ListSessionsRequest) (*orchestratorSDK.ListSessionsResponse, error) {
	return c.sdk.Orchestrator().ListSessions(ctx, req)
}

// CreateTask creates a new task.
func (c *sdkOrchestratorClient) CreateTask(ctx context.Context, req *orchestratorSDK.CreateTaskRequest) (*orchestratorSDK.Task, error) {
	return c.sdk.Orchestrator().CreateTask(ctx, req)
}

// GetTask returns a task by ID.
func (c *sdkOrchestratorClient) GetTask(ctx context.Context, req *orchestratorSDK.GetTaskRequest) (*orchestratorSDK.Task, error) {
	return c.sdk.Orchestrator().GetTask(ctx, req)
}

// ListTasks returns tasks matching the filter criteria.
func (c *sdkOrchestratorClient) ListTasks(ctx context.Context, req *orchestratorSDK.ListTasksRequest) (*orchestratorSDK.ListTasksResponse, error) {
	return c.sdk.Orchestrator().ListTasks(ctx, req)
}

// ClaimTask claims a pending task for execution.
func (c *sdkOrchestratorClient) ClaimTask(ctx context.Context, req *orchestratorSDK.ClaimTaskRequest) (*orchestratorSDK.ClaimTaskResponse, error) {
	return c.sdk.Orchestrator().ClaimTask(ctx, req)
}

// BlockTask parks a task until a person readmits it.
func (c *sdkOrchestratorClient) BlockTask(ctx context.Context, req *orchestratorSDK.BlockTaskRequest) (*orchestratorSDK.BlockTaskResponse, error) {
	return c.sdk.Orchestrator().BlockTask(ctx, req)
}

// ReadmitTask returns a blocked task to the pending queue.
func (c *sdkOrchestratorClient) ReadmitTask(ctx context.Context, req *orchestratorSDK.ReadmitTaskRequest) (*orchestratorSDK.ReadmitTaskResponse, error) {
	return c.sdk.Orchestrator().ReadmitTask(ctx, req)
}

// DeleteTask removes a task and releases its source reservation.
func (c *sdkOrchestratorClient) DeleteTask(ctx context.Context, req *orchestratorSDK.DeleteTaskRequest) (*orchestratorSDK.DeleteTaskResponse, error) {
	return c.sdk.Orchestrator().DeleteTask(ctx, req)
}

// CompleteTask marks a task as completed.
func (c *sdkOrchestratorClient) CompleteTask(ctx context.Context, req *orchestratorSDK.CompleteTaskRequest) (*orchestratorSDK.CompleteTaskResponse, error) {
	return c.sdk.Orchestrator().CompleteTask(ctx, req)
}

// RegisterAgent registers a new agent with the fleet.
func (c *sdkOrchestratorClient) RegisterAgent(ctx context.Context, req *orchestratorSDK.RegisterAgentRequest) (*orchestratorSDK.RegisterAgentResponse, error) {
	return c.sdk.Orchestrator().RegisterAgent(ctx, req)
}

// GetAgent returns an agent by ID.
func (c *sdkOrchestratorClient) GetAgent(ctx context.Context, req *orchestratorSDK.GetAgentRequest) (*orchestratorSDK.AgentRegistration, error) {
	return c.sdk.Orchestrator().GetAgent(ctx, req)
}

// ListAgents returns agents matching the filter criteria.
func (c *sdkOrchestratorClient) ListAgents(ctx context.Context, req *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error) {
	return c.sdk.Orchestrator().ListAgents(ctx, req)
}

// RemoveAgent removes an agent from the fleet.
func (c *sdkOrchestratorClient) RemoveAgent(ctx context.Context, req *orchestratorSDK.RemoveAgentRequest) (*orchestratorSDK.RemoveAgentResponse, error) {
	return c.sdk.Orchestrator().RemoveAgent(ctx, req)
}

// AgentHeartbeat sends a heartbeat for an agent.
func (c *sdkOrchestratorClient) AgentHeartbeat(ctx context.Context, req *orchestratorSDK.AgentHeartbeatRequest) (*orchestratorSDK.AgentHeartbeatResponse, error) {
	return c.sdk.Orchestrator().AgentHeartbeat(ctx, req)
}

// DecomposeTask decomposes a task into structured work items.
func (c *sdkOrchestratorClient) DecomposeTask(ctx context.Context, req *orchestratorSDK.DecomposeTaskRequest) (*orchestratorSDK.DecomposeTaskResponse, error) {
	return c.sdk.Orchestrator().DecomposeTask(ctx, req)
}

// StartJob starts an autonomous orchestration job.
func (c *sdkOrchestratorClient) StartJob(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
	return c.sdk.Orchestrator().StartJob(ctx, req)
}

// GetJobStatus returns the current status of an orchestration job.
func (c *sdkOrchestratorClient) GetJobStatus(ctx context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
	return c.sdk.Orchestrator().GetJobStatus(ctx, req)
}

// ListJobs returns all jobs for a tenant.
func (c *sdkOrchestratorClient) ListJobs(ctx context.Context, req *orchestratorSDK.ListJobsRequest) (*orchestratorSDK.ListJobsResponse, error) {
	return c.sdk.Orchestrator().ListJobs(ctx, req)
}

// CancelJob cancels a running orchestration job.
func (c *sdkOrchestratorClient) CancelJob(ctx context.Context, req *orchestratorSDK.CancelJobRequest) (*orchestratorSDK.CancelJobResponse, error) {
	return c.sdk.Orchestrator().CancelJob(ctx, req)
}

// SubscribeJobEvents returns a channel of real-time events for a running job.
func (c *sdkOrchestratorClient) SubscribeJobEvents(ctx context.Context, req *orchestratorSDK.SubscribeJobEventsRequest) (<-chan *orchestratorSDK.JobEvent, error) {
	return c.sdk.Orchestrator().SubscribeJobEvents(ctx, req)
}

func (c *sdkOrchestratorClient) GetSessionHistory(ctx context.Context, req *orchestratorSDK.GetSessionHistoryRequest) (*orchestratorSDK.GetSessionHistoryResponse, error) {
	return c.sdk.Orchestrator().GetSessionHistory(ctx, req)
}

func (c *sdkOrchestratorClient) SubscribeSessionEvents(ctx context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
	return c.sdk.Orchestrator().SubscribeSessionEvents(ctx, req)
}

// Inference runs a stateless fleet-routed inference call and returns the
// aggregated response.
func (c *sdkOrchestratorClient) Inference(ctx context.Context, req *orchestratorSDK.InferenceRequest) (*orchestratorSDK.InferenceResponse, error) {
	return c.sdk.Orchestrator().Inference(ctx, req)
}

// InferenceStream runs a stateless fleet-routed inference call and returns a
// channel of InferenceChunk values streamed from the backend.
func (c *sdkOrchestratorClient) InferenceStream(ctx context.Context, req *orchestratorSDK.InferenceRequest) (<-chan *orchestratorSDK.InferenceChunk, error) {
	return c.sdk.Orchestrator().InferenceStream(ctx, req)
}

// Close closes the underlying SDK client.
func (c *sdkOrchestratorClient) Close() error {
	return c.sdk.Close()
}

// =============================================================================
// SDK Operations Client
// =============================================================================

// sdkOperationsClient implements the OperationsClient interface using the
// SDK transport. Every method delegates to sdk.Operations(), which routes
// the request through the active transport (rest / grpc / quic / mcp /
// unix / embedded) so the migrate / node / upgrade verbs are no longer
// pinned to a hardcoded gRPC connection.
type sdkOperationsClient struct {
	sdk quicbaas.Client
}

// createSDKOperationsClient resolves the active SDK client and wraps its
// Operations() accessor.
func createSDKOperationsClient() (OperationsClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	ops := sdk.Operations()
	if ops == nil {
		return nil, sdkerrors.NewInternal("createSDKOperationsClient", "operations client not available")
	}
	return &sdkOperationsClient{sdk: sdk}, nil
}

// MigrateData migrates data between shards via the active transport.
func (c *sdkOperationsClient) MigrateData(ctx context.Context, req *operationsSDK.MigrateDataRequest) (*operationsSDK.MigrateDataResponse, error) {
	return c.sdk.Operations().MigrateData(ctx, req)
}

// GetMigrationProgress reports migration progress via the active transport.
func (c *sdkOperationsClient) GetMigrationProgress(ctx context.Context, req *operationsSDK.GetMigrationProgressRequest) (*operationsSDK.GetMigrationProgressResponse, error) {
	return c.sdk.Operations().GetMigrationProgress(ctx, req)
}

// ReplaceNode replaces a cluster node via the active transport.
func (c *sdkOperationsClient) ReplaceNode(ctx context.Context, req *operationsSDK.ReplaceNodeRequest) (*operationsSDK.ReplaceNodeResponse, error) {
	return c.sdk.Operations().ReplaceNode(ctx, req)
}

// UpgradeNode starts a rolling node upgrade via the active transport.
func (c *sdkOperationsClient) UpgradeNode(ctx context.Context, req *operationsSDK.UpgradeNodeRequest) (*operationsSDK.UpgradeNodeResponse, error) {
	return c.sdk.Operations().UpgradeNode(ctx, req)
}

// GetUpgradeStatus reports rolling-upgrade status via the active transport.
func (c *sdkOperationsClient) GetUpgradeStatus(ctx context.Context, req *operationsSDK.GetUpgradeStatusRequest) (*operationsSDK.GetUpgradeStatusResponse, error) {
	return c.sdk.Operations().GetUpgradeStatus(ctx, req)
}

// =============================================================================
// Compile-time Interface Checks
// =============================================================================

var _ CryptoClient = (*sdkCryptoClient)(nil)
var _ KMSClient = (*sdkKMSClient)(nil)
var _ SealClient = (*sdkSealClient)(nil)
var _ BackendClient = (*sdkBackendClient)(nil)
var _ SafetyClient = (*sdkSafetyClient)(nil)
var _ BackupClient = (*sdkBackupClient)(nil)
var _ OrchestratorClient = (*sdkOrchestratorClient)(nil)
var _ OperationsClient = (*sdkOperationsClient)(nil)

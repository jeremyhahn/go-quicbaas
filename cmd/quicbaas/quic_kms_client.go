//go:build quic

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
	"crypto/tls"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// QUICKMSClient wraps the QUIC client to implement the KMSClient interface.
type QUICKMSClient struct {
	client *quic.Client
}

// KMSGenerateKey creates a new cryptographic key.
func (c *QUICKMSClient) KMSGenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	return c.client.KMSGenerateKey(ctx, req)
}

// KMSImportKey imports an external key into the KMS.
func (c *QUICKMSClient) KMSImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	return c.client.KMSImportKey(ctx, req)
}

// KMSGetKey retrieves key metadata by ID.
func (c *QUICKMSClient) KMSGetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	return c.client.KMSGetKey(ctx, req)
}

// KMSListKeys lists all keys matching optional filters.
func (c *QUICKMSClient) KMSListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	return c.client.KMSListKeys(ctx, req)
}

// KMSRotateKey creates a new version of an existing key.
func (c *QUICKMSClient) KMSRotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	return c.client.KMSRotateKey(ctx, req)
}

// KMSDeleteKey schedules a key for deletion.
func (c *QUICKMSClient) KMSDeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	return c.client.KMSDeleteKey(ctx, req)
}

// KMSEncrypt encrypts plaintext using the specified key.
func (c *QUICKMSClient) KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	return c.client.KMSEncrypt(ctx, req)
}

// KMSDecrypt decrypts ciphertext using the specified key.
func (c *QUICKMSClient) KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	return c.client.KMSDecrypt(ctx, req)
}

// KMSUpdateKeyStatus updates the status of a key or key version.
func (c *QUICKMSClient) KMSUpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	return c.client.KMSUpdateKeyStatus(ctx, req)
}

// KMSListKeyVersions lists all versions of a cryptographic key.
func (c *QUICKMSClient) KMSListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	return c.client.KMSListKeyVersions(ctx, req)
}

// KMSExportKey exports key material wrapped with a wrapping key.
func (c *QUICKMSClient) KMSExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	return c.client.KMSExportKey(ctx, req)
}

// KMSUnwrapKey imports key material that was wrapped with a wrapping key.
func (c *QUICKMSClient) KMSUnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	return c.client.KMSUnwrapKey(ctx, req)
}

// KMSSetKeySafety sets AEAD safety limits for a key.
func (c *QUICKMSClient) KMSSetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	return c.client.KMSSetKeySafety(ctx, req)
}

// KMSClearKeySafety clears AEAD safety limits for a key.
func (c *QUICKMSClient) KMSClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	return c.client.KMSClearKeySafety(ctx, req)
}

// Close closes the QUIC client connection.
func (c *QUICKMSClient) Close() error {
	return c.client.Close()
}

// QUICSecretsClient wraps the QUIC client to implement the SecretsClient interface.
type QUICSecretsClient struct {
	client *quic.Client
}

// StoreSecret stores a new secret.
func (c *QUICSecretsClient) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	return c.client.StoreSecret(ctx, req)
}

// GetSecret retrieves a secret by ID.
func (c *QUICSecretsClient) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return c.client.GetSecret(ctx, req)
}

// ListSecrets returns all accessible secrets.
func (c *QUICSecretsClient) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	return c.client.ListSecrets(ctx, req)
}

// DeleteSecret deletes a secret.
func (c *QUICSecretsClient) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	return c.client.DeleteSecret(ctx, req)
}

// ListSecretVersions returns all versions of a secret.
func (c *QUICSecretsClient) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	return c.client.ListSecretVersions(ctx, req)
}

// RollbackSecret rolls back to a previous version.
func (c *QUICSecretsClient) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	return c.client.RollbackSecret(ctx, req)
}

// RotateSecret rotates a secret to a new value.
func (c *QUICSecretsClient) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	return c.client.RotateSecret(ctx, req)
}

// UpdateSecretStatus updates the status of a secret (enable/disable).
func (c *QUICSecretsClient) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	return c.client.UpdateSecretStatus(ctx, req)
}

// GetSecretsByPrefix retrieves all secrets matching a path prefix.
func (c *QUICSecretsClient) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	return c.client.GetSecretsByPrefix(ctx, req)
}

// WatchSecretsByPrefix watches for secret changes under a prefix.
func (c *QUICSecretsClient) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	return c.client.WatchSecretsByPrefix(ctx, req)
}

// Close closes the QUIC client connection.
func (c *QUICSecretsClient) Close() error {
	return c.client.Close()
}

// QUICSealClient wraps the QUIC client to implement the SealClient interface.
type QUICSealClient struct {
	client *quic.Client
}

// QUICSealRemovedError signals that the QUIC seal-tree path was deleted in
// Phase 8c. Callers should migrate to the barrier surface (QUICBarrierClient
// over the new OpBarrier dispatcher).
type QUICSealRemovedError struct {
	Op string
}

// Error implements the error interface.
func (e *QUICSealRemovedError) Error() string {
	return "quic seal." + e.Op + " removed in Phase 8c; use the barrier surface"
}

// SealStatus is no longer supported on the QUIC transport. Use the barrier
// surface (BarrierStatus) instead.
func (c *QUICSealClient) SealStatus(ctx context.Context) (*service.SealStatusResponse, error) {
	return nil, &QUICSealRemovedError{Op: "SealStatus"}
}

// SealInit is no longer supported on the QUIC transport. Use the barrier
// surface (BarrierInitialize / BarrierShamirInitialize) instead.
func (c *QUICSealClient) SealInit(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error) {
	return nil, &QUICSealRemovedError{Op: "SealInit"}
}

// Unseal is no longer supported on the QUIC transport. Use the barrier
// surface (BarrierUnseal / BarrierShamirUnsealShare) instead.
func (c *QUICSealClient) Unseal(ctx context.Context, req *service.UnsealRequest) (*service.UnsealResponse, error) {
	return nil, &QUICSealRemovedError{Op: "Unseal"}
}

// Seal is no longer supported on the QUIC transport. Use the barrier
// surface (BarrierSeal) instead.
func (c *QUICSealClient) Seal(ctx context.Context) error {
	return &QUICSealRemovedError{Op: "Seal"}
}

// Rekey is no longer supported on the QUIC transport. Use the barrier
// surface (BarrierRekey) instead.
func (c *QUICSealClient) Rekey(ctx context.Context, req *service.RekeyRequest) (*service.RekeyResponse, error) {
	return nil, &QUICSealRemovedError{Op: "Rekey"}
}

// Close closes the QUIC client connection.
func (c *QUICSealClient) Close() error {
	return c.client.Close()
}

// QUICBackendClient wraps the QUIC client to implement the BackendClient interface.
type QUICBackendClient struct {
	client *quic.Client
}

// KMSBackendStatus returns the KMS backend status.
func (c *QUICBackendClient) KMSBackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	return c.client.KMSBackendStatus(ctx)
}

// KMSBackendTest tests the KMS backend.
func (c *QUICBackendClient) KMSBackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	return c.client.KMSBackendTest(ctx)
}

// Close closes the QUIC client connection.
func (c *QUICBackendClient) Close() error {
	return c.client.Close()
}

// QUICSafetyClient wraps the QUIC client to implement the SafetyClient interface.
type QUICSafetyClient struct {
	client *quic.Client
}

// KMSSafetyCheck checks AEAD safety status for all keys approaching limits.
func (c *QUICSafetyClient) KMSSafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	return c.client.KMSSafetyCheck(ctx, req)
}

// KMSSafetyReport generates a detailed AEAD safety report for all keys.
func (c *QUICSafetyClient) KMSSafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	return c.client.KMSSafetyReport(ctx, req)
}

// Close closes the QUIC client connection.
func (c *QUICSafetyClient) Close() error {
	return c.client.Close()
}

// QUICCryptoClient wraps the QUIC client to implement the CryptoClient interface.
type QUICCryptoClient struct {
	client *quic.Client
}

// KMSEncrypt encrypts plaintext using the specified key.
func (c *QUICCryptoClient) KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	return c.client.KMSEncrypt(ctx, req)
}

// KMSDecrypt decrypts ciphertext using the specified key.
func (c *QUICCryptoClient) KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	return c.client.KMSDecrypt(ctx, req)
}

// KMSGenerateDataKey generates a new data encryption key.
func (c *QUICCryptoClient) KMSGenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	return c.client.KMSGenerateDataKey(ctx, req)
}

// KMSSign signs data using the specified key.
// Note: Not yet implemented in QUIC protocol.
func (c *QUICCryptoClient) KMSSign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	return nil, errors.NotImplemented("QUICCryptoClient.KMSSign", "QUIC protocol does not yet support KMSSign")
}

// KMSVerify verifies a signature using the specified key.
// Note: Not yet implemented in QUIC protocol.
func (c *QUICCryptoClient) KMSVerify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	return nil, errors.NotImplemented("QUICCryptoClient.KMSVerify", "QUIC protocol does not yet support KMSVerify")
}

// KMSHash computes a cryptographic hash.
// Note: Not yet implemented in QUIC protocol.
func (c *QUICCryptoClient) KMSHash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	return nil, errors.NotImplemented("QUICCryptoClient.KMSHash", "QUIC protocol does not yet support KMSHash")
}

// KMSGenerateHMAC generates an HMAC using the specified key.
// Note: Not yet implemented in QUIC protocol.
func (c *QUICCryptoClient) KMSGenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	return nil, errors.NotImplemented("QUICCryptoClient.KMSGenerateHMAC", "QUIC protocol does not yet support KMSGenerateHMAC")
}

// KMSVerifyHMAC verifies an HMAC using the specified key.
// Note: Not yet implemented in QUIC protocol.
func (c *QUICCryptoClient) KMSVerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	return nil, errors.NotImplemented("QUICCryptoClient.KMSVerifyHMAC", "QUIC protocol does not yet support KMSVerifyHMAC")
}

// KMSRewrap re-encrypts data with a new key.
// Note: Not yet implemented in QUIC protocol.
func (c *QUICCryptoClient) KMSRewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	return nil, errors.NotImplemented("QUICCryptoClient.KMSRewrap", "QUIC protocol does not yet support KMSRewrap")
}

// KMSGenerateRandom generates cryptographically secure random bytes.
// Note: Not yet implemented in QUIC protocol.
func (c *QUICCryptoClient) KMSGenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	return nil, errors.NotImplemented("QUICCryptoClient.KMSGenerateRandom", "QUIC protocol does not yet support KMSGenerateRandom")
}

// Close closes the QUIC client connection.
func (c *QUICCryptoClient) Close() error {
	return c.client.Close()
}

// createQUICClientLogger creates a logger for QUIC CLI clients.
func createQUICClientLogger() (*logger.Logger, error) {
	// Use the global logLevel from CLI flags, default format to json
	level := logLevel
	if level == "" {
		level = "info"
	}
	logCfg := logger.LogConfig{
		Level:     level,
		Format:    "json",
		Output:    "stderr",
		Component: "quic-cli-client",
	}
	return logger.New(logCfg)
}

// createQUICClientInternal creates the underlying QUIC client.
func createQUICClientInternal() (*quic.Client, error) {
	log, err := createQUICClientLogger()
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrConfiguration,
			"createQUICClientInternal",
			"failed to create logger",
		)
	}

	opts := []quic.ClientOption{
		quic.WithLogger(log),
		quic.WithTimeout(timeout),
		quic.WithMaxIdleTimeout(5 * time.Minute),
	}

	// QUIC always uses TLS — QuicBaaS bans insecure TLS so the operator must
	// supply a CA via --tls-ca (or set --tls-cert/--tls-key for mTLS).
	tlsCfg, err := buildTLSConfig()
	if err != nil {
		return nil, err
	}
	if tlsCfg == nil {
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createQUICClientInternal",
			"TLS configuration is required for QUIC connections (use --tls-ca)",
		)
	}
	tlsCfg.MinVersion = tls.VersionTLS13
	opts = append(opts, quic.WithTLSConfig(tlsCfg))

	// Create QUIC client
	client, err := quic.NewClient(address, opts...)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrUnavailable,
			"createQUICClientInternal",
			"failed to create QUIC client for server %s", address,
		)
	}

	return client, nil
}

// createQUICSecretsClient creates a QUIC client for secrets operations.
func createQUICSecretsClient() (SecretsClient, error) {
	client, err := createQUICClientInternal()
	if err != nil {
		return nil, err
	}
	return &QUICSecretsClient{client: client}, nil
}

// Compile-time interface checks.
var (
	_ KMSClient     = (*QUICKMSClient)(nil)
	_ SecretsClient = (*QUICSecretsClient)(nil)
	_ SealClient    = (*QUICSealClient)(nil)
	_ BackendClient = (*QUICBackendClient)(nil)
	_ SafetyClient  = (*QUICSafetyClient)(nil)
	_ CryptoClient  = (*QUICCryptoClient)(nil)
)

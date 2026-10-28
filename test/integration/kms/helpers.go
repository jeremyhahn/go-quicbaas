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

//go:build integration && quic

package kms

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// KeyAttributes Helper Functions
// =============================================================================

// AES128GCMAttributes returns KeyAttributes for AES-128-GCM symmetric encryption.
func AES128GCMAttributes() *kctypes.KeyAttributes {
	return &kctypes.KeyAttributes{
		SymmetricAlgorithm: kctypes.SymmetricAES128GCM,
	}
}

// AES192GCMAttributes returns KeyAttributes for AES-192-GCM symmetric encryption.
func AES192GCMAttributes() *kctypes.KeyAttributes {
	return &kctypes.KeyAttributes{
		SymmetricAlgorithm: kctypes.SymmetricAES192GCM,
	}
}

// AES256GCMAttributes returns KeyAttributes for AES-256-GCM symmetric encryption.
func AES256GCMAttributes() *kctypes.KeyAttributes {
	return &kctypes.KeyAttributes{
		SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
	}
}

// ChaCha20Poly1305Attributes returns KeyAttributes for ChaCha20-Poly1305 symmetric encryption.
func ChaCha20Poly1305Attributes() *kctypes.KeyAttributes {
	return &kctypes.KeyAttributes{
		SymmetricAlgorithm: kctypes.SymmetricChaCha20Poly1305,
	}
}

// XChaCha20Poly1305Attributes returns KeyAttributes for XChaCha20-Poly1305 symmetric encryption.
func XChaCha20Poly1305Attributes() *kctypes.KeyAttributes {
	return &kctypes.KeyAttributes{
		SymmetricAlgorithm: kctypes.SymmetricXChaCha20Poly1305,
	}
}

// RSA4096Attributes returns KeyAttributes for RSA-4096 asymmetric keys.
func RSA4096Attributes() *kctypes.KeyAttributes {
	return &kctypes.KeyAttributes{
		KeyAlgorithm: x509.RSA,
		RSAAttributes: &kctypes.RSAAttributes{
			KeySize: 4096,
		},
	}
}

// RSA2048Attributes returns KeyAttributes for RSA-2048 asymmetric keys.
func RSA2048Attributes() *kctypes.KeyAttributes {
	return &kctypes.KeyAttributes{
		KeyAlgorithm: x509.RSA,
		RSAAttributes: &kctypes.RSAAttributes{
			KeySize: 2048,
		},
	}
}

// ECDSAP256Attributes returns KeyAttributes for ECDSA P-256 asymmetric keys.
func ECDSAP256Attributes() *kctypes.KeyAttributes {
	return &kctypes.KeyAttributes{
		KeyAlgorithm: x509.ECDSA,
		ECCAttributes: &kctypes.ECCAttributes{
			Curve: elliptic.P256(),
		},
	}
}

// ECDSAP384Attributes returns KeyAttributes for ECDSA P-384 asymmetric keys.
func ECDSAP384Attributes() *kctypes.KeyAttributes {
	return &kctypes.KeyAttributes{
		KeyAlgorithm: x509.ECDSA,
		ECCAttributes: &kctypes.ECCAttributes{
			Curve: elliptic.P384(),
		},
	}
}

// ECDSAP521Attributes returns KeyAttributes for ECDSA P-521 asymmetric keys.
func ECDSAP521Attributes() *kctypes.KeyAttributes {
	return &kctypes.KeyAttributes{
		KeyAlgorithm: x509.ECDSA,
		ECCAttributes: &kctypes.ECCAttributes{
			Curve: elliptic.P521(),
		},
	}
}

// EncryptDecryptUsage returns the standard usage for encryption/decryption keys.
func EncryptDecryptUsage() []service.KeyUsage {
	return []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt}
}

// SignVerifyUsage returns the standard usage for signing/verification keys.
func SignVerifyUsage() []service.KeyUsage {
	return []service.KeyUsage{service.KeyUsageSign, service.KeyUsageVerify}
}

// =============================================================================
// Retry and Replication Helpers
// =============================================================================

// RetryConfig configures retry behavior for cluster operations.
type RetryConfig struct {
	MaxAttempts int
	InitialWait time.Duration
	MaxWait     time.Duration
	Multiplier  float64
}

// DefaultRetryConfig returns sensible defaults for cluster operations.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 10,
		InitialWait: 500 * time.Millisecond,
		MaxWait:     5 * time.Second,
		Multiplier:  1.5,
	}
}

// RetryOperation executes an operation with exponential backoff retry.
// Returns nil on success, or the last error after all retries are exhausted.
func RetryOperation(ctx context.Context, cfg RetryConfig, operation func() error) error {
	var lastErr error
	wait := cfg.InitialWait

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		lastErr = operation()
		if lastErr == nil {
			return nil
		}

		// Check context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Don't wait after last attempt
		if attempt < cfg.MaxAttempts {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}

			// Exponential backoff
			wait = time.Duration(float64(wait) * cfg.Multiplier)
			if wait > cfg.MaxWait {
				wait = cfg.MaxWait
			}
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", cfg.MaxAttempts, lastErr)
}

// WaitForReplication waits for a key to be replicated across the cluster.
// This is useful for cluster consistency tests where Raft needs time to replicate.
func WaitForReplication(ctx context.Context, client *RESTClient, keyID string, cfg RetryConfig) error {
	return RetryOperation(ctx, cfg, func() error {
		req := &service.GetKeyRequest{KeyID: keyID}
		_, err := client.GetKey(ctx, req)
		return err
	})
}

// =============================================================================
// Endpoint Helpers
// =============================================================================

// getGRPCEndpoints returns all gRPC endpoints for testing.
func getGRPCEndpoints() []string {
	node1 := os.Getenv("QUICBAAS_NODE1_GRPC")
	if node1 == "" {
		node1 = os.Getenv("QUICBAAS_NODE1")
	}
	if node1 == "" {
		node1 = "localhost:50051"
	}

	node2 := os.Getenv("QUICBAAS_NODE2_GRPC")
	if node2 == "" {
		node2 = os.Getenv("QUICBAAS_NODE2")
	}
	if node2 == "" {
		node2 = "localhost:50052"
	}

	node3 := os.Getenv("QUICBAAS_NODE3_GRPC")
	if node3 == "" {
		node3 = os.Getenv("QUICBAAS_NODE3")
	}
	if node3 == "" {
		node3 = "localhost:50053"
	}

	return []string{node1, node2, node3}
}

// getRESTEndpoints returns all REST API endpoints for testing.
func getRESTEndpoints() []string {
	node1 := os.Getenv("QUICBAAS_NODE1_URL")
	if node1 == "" {
		node1 = "https://test-node1:8080"
	}

	node2 := os.Getenv("QUICBAAS_NODE2_URL")
	if node2 == "" {
		node2 = "https://test-node2:8080"
	}

	node3 := os.Getenv("QUICBAAS_NODE3_URL")
	if node3 == "" {
		node3 = "https://test-node3:8080"
	}

	return []string{node1, node2, node3}
}

// getMCPEndpoint returns the MCP endpoint for testing.
func getMCPEndpoint() string {
	return helpers.GetMCPEndpoints()[0]
}

// =============================================================================
// Skip Helpers (deprecated - use require helpers instead)
// =============================================================================

// skipIfKMSUnavailable checks if the KMS service is available and skips the test if not.
// Deprecated: Use requireKMSAvailable instead for proper test failures.
func skipIfKMSUnavailable(t *testing.T, client *grpcclient.Client, ctx context.Context) {
	t.Helper()
	req := &service.ListKeysRequest{
		Limit: 1,
	}
	_, err := client.KMSListKeys(ctx, req)
	if err != nil && (strings.Contains(err.Error(), "Unimplemented") ||
		strings.Contains(err.Error(), "unknown service")) {
		require.Fail(t, "KMS service must be available", "KMS service is not enabled on the server")
	}
}

// skipIfCryptoUnavailable checks if the Crypto service is available and skips the test if not.
// Deprecated: Use requireCryptoAvailable instead for proper test failures.
func skipIfCryptoUnavailable(t *testing.T, client *grpcclient.Client, ctx context.Context, keyID string) {
	t.Helper()
	req := &service.EncryptRequest{
		KeyID:     keyID,
		Plaintext: []byte("test"),
	}
	_, err := client.KMSEncrypt(ctx, req)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "Unimplemented") ||
			strings.Contains(errStr, "unknown service") ||
			strings.Contains(errStr, "crypto service not available") ||
			strings.Contains(errStr, "not available") ||
			strings.Contains(errStr, "not initialized") {
			require.Fail(t, "Crypto service must be available",
				"Crypto service is not enabled on the server: %v", err)
		}
	}
}

// =============================================================================
// Require Helpers (preferred - fail tests properly instead of skipping)
// =============================================================================

// requireKMSAvailable checks if the KMS service is available and fails the test if not.
// Use this instead of skipIfKMSUnavailable to ensure integration tests properly fail
// when services are unavailable.
func requireKMSAvailable(t *testing.T, client *grpcclient.Client, ctx context.Context) {
	t.Helper()
	req := &service.ListKeysRequest{
		Limit: 1,
	}
	_, err := client.KMSListKeys(ctx, req)
	if err != nil && (strings.Contains(err.Error(), "Unimplemented") ||
		strings.Contains(err.Error(), "unknown service")) {
		require.Fail(t, "KMS service is not enabled on the server", "KMS service must be available for integration tests")
	}
	// For other errors, we don't fail - they might be legitimate (e.g., empty list)
}

// requireCryptoAvailable checks if the Crypto service is available and fails the test if not.
// Use this instead of skipIfCryptoUnavailable to ensure integration tests properly fail
// when services are unavailable.
func requireCryptoAvailable(t *testing.T, client *grpcclient.Client, ctx context.Context, keyID string) {
	t.Helper()
	req := &service.EncryptRequest{
		KeyID:     keyID,
		Plaintext: []byte("test"),
	}
	_, err := client.KMSEncrypt(ctx, req)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "Unimplemented") ||
			strings.Contains(errStr, "unknown service") ||
			strings.Contains(errStr, "crypto service not available") ||
			strings.Contains(errStr, "not available") ||
			strings.Contains(errStr, "not initialized") {
			require.Fail(t, "Crypto service is not enabled on the server",
				"Crypto service must be available for integration tests: %v", err)
		}
		// For other errors (like invalid key), don't fail the availability check
	}
}

// requireAlgorithmSupported checks if a specific algorithm is supported and fails the test if not.
func requireAlgorithmSupported(t *testing.T, client *grpcclient.Client, ctx context.Context, algorithm string) {
	t.Helper()
	require.True(t, isAlgorithmSupported(client, ctx, algorithm),
		"Algorithm %s is not supported by the current backend", algorithm)
}

// isAlgorithmSupported checks if a specific algorithm is supported by the backend.
// Returns true if the algorithm is supported, false otherwise.
func isAlgorithmSupported(client *grpcclient.Client, ctx context.Context, algorithm string) bool {
	algorithms, err := client.KMSGetSupportedAlgorithms(ctx)
	if err != nil {
		// If we can't get supported algorithms, assume it's not supported
		return false
	}

	for _, alg := range algorithms {
		if strings.EqualFold(alg, algorithm) {
			return true
		}
	}
	return false
}

// skipIfAlgorithmNotSupported checks if a specific algorithm is supported and skips the test if not.
// Deprecated: Use requireAlgorithmSupported instead for proper test failures.
func skipIfAlgorithmNotSupported(t *testing.T, client *grpcclient.Client, ctx context.Context, algorithm string) {
	t.Helper()
	if !isAlgorithmSupported(client, ctx, algorithm) {
		require.Fail(t, "Algorithm must be supported",
			"Algorithm %s is not supported by the current backend", algorithm)
	}
}

// skipIfProtocolUnavailable checks if a protocol endpoint is available.
// Deprecated: Use requireProtocolAvailable instead for proper test failures.
func skipIfProtocolUnavailable(t *testing.T, protocol string, endpoint string) {
	t.Helper()

	switch protocol {
	case "grpc":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		tlsCfg, err := helpers.IntegrationTLSConfig()
		require.NoError(t, err, "load integration TLS config")
		client, err := grpcclient.NewClient(endpoint, grpcclient.WithTLSConfig(tlsCfg), helpers.SystemAdminGRPCClientOption(t))
		if err != nil {
			require.NoError(t, err, "%s service must be available at %s", protocol, endpoint)
		}
		defer client.Close()

		skipIfKMSUnavailable(t, client, ctx)

	case "rest":
		client := helpers.NewHTTPClient(5 * time.Second)
		resp, err := client.Get(fmt.Sprintf("%s/health", endpoint))
		if err != nil {
			require.NoError(t, err, "%s service must be available at %s", protocol, endpoint)
		}
		defer resp.Body.Close()

	case "quic":
		// QUIC availability check would go here if needed
		// For now we just assume it's available

	case "mcp":
		// MCP availability check would go here if needed
		// For now we just assume it's available
	}
}

// =============================================================================
// Test Data Helpers
// =============================================================================

// generateUniqueKeyName generates a unique key name with a prefix.
func generateUniqueKeyName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// generateTestPlaintext generates test plaintext of a given size.
func generateTestPlaintext(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return data
}

// =============================================================================
// REST Client
// =============================================================================

// RESTClient wraps HTTP client for REST API testing.
type RESTClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewRESTClient creates a new REST client that acts as the system admin. The
// KMS routes need keys:create and keys:delete, which go-qrdb grants to the
// built-in Admin role and not to Operator.
func NewRESTClient(t *testing.T, baseURL string) *RESTClient {
	t.Helper()
	return &RESTClient{
		baseURL:    baseURL,
		httpClient: helpers.NewSystemAdminHTTPClient(t, 30*time.Second),
	}
}

// RESTKeyAttributesRequest matches the REST API expected format.
type RESTKeyAttributesRequest struct {
	Algorithm      string   `json:"algorithm"`
	KeySize        int      `json:"key_size,omitempty"`
	KeyUsage       []string `json:"key_usage"`
	Exportable     bool     `json:"exportable,omitempty"`
	AutoRotate     bool     `json:"auto_rotate,omitempty"`
	RotationPeriod string   `json:"rotation_period,omitempty"`
}

// RESTGenerateKeyRequest matches the REST API expected format.
type RESTGenerateKeyRequest struct {
	Name          string                    `json:"name,omitempty"`
	Description   string                    `json:"description,omitempty"`
	KeyAttributes *RESTKeyAttributesRequest `json:"key_attributes"`
	Tags          map[string]string         `json:"tags,omitempty"`
	Owner         string                    `json:"owner,omitempty"`
}

// RESTKeyHandleResponse matches the REST API key handle format.
type RESTKeyHandleResponse struct {
	KeyID     string `json:"key_id"`
	Version   int    `json:"version"`
	Algorithm string `json:"algorithm"`
	Status    string `json:"status"`
}

// RESTKeyMetadataResponse matches the REST API key metadata format.
type RESTKeyMetadataResponse struct {
	KeyID       string            `json:"key_id"`
	Algorithm   string            `json:"algorithm"`
	Status      string            `json:"status"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	CreatedAt   int64             `json:"created_at"`
	UpdatedAt   int64             `json:"updated_at,omitempty"`
	ExpiresAt   int64             `json:"expires_at,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Owner       string            `json:"owner,omitempty"`
	Version     int               `json:"version"`
	KeySize     int               `json:"key_size"`
	Usage       []string          `json:"usage,omitempty"`
}

// RESTGenerateKeyResponse matches the REST API response format.
type RESTGenerateKeyResponse struct {
	Key       *RESTKeyHandleResponse   `json:"key"`
	Metadata  *RESTKeyMetadataResponse `json:"metadata,omitempty"`
	Timestamp int64                    `json:"timestamp"`
}

// convertRESTToServiceResponse converts REST response to service response.
func convertRESTToServiceResponse(restResp *RESTGenerateKeyResponse) *service.GenerateKeyResponse {
	if restResp == nil {
		return nil
	}

	resp := &service.GenerateKeyResponse{
		Timestamp: time.Unix(restResp.Timestamp, 0),
	}

	if restResp.Key != nil {
		resp.Key = &service.KeyHandle{
			KeyID:     restResp.Key.KeyID,
			Version:   uint64(restResp.Key.Version),
			Algorithm: service.KeyAlgorithm(restResp.Key.Algorithm),
			Status:    service.KeyStatus(restResp.Key.Status),
		}
	}

	if restResp.Metadata != nil {
		resp.Metadata = &service.KeyMetadata{
			KeyID:       restResp.Metadata.KeyID,
			Algorithm:   service.KeyAlgorithm(restResp.Metadata.Algorithm),
			Status:      service.KeyStatus(restResp.Metadata.Status),
			Name:        restResp.Metadata.Name,
			Description: restResp.Metadata.Description,
			CreatedAt:   time.Unix(restResp.Metadata.CreatedAt, 0),
			UpdatedAt:   time.Unix(restResp.Metadata.UpdatedAt, 0),
			Tags:        restResp.Metadata.Tags,
			Owner:       restResp.Metadata.Owner,
			Version:     uint64(restResp.Metadata.Version),
		}
		// Convert usage strings to service.KeyUsage
		for _, u := range restResp.Metadata.Usage {
			resp.Metadata.Usage = append(resp.Metadata.Usage, service.KeyUsage(u))
		}
	}

	return resp
}

// RESTGetKeyResponse matches the REST API get key response format.
type RESTGetKeyResponse struct {
	Key       *RESTKeyHandleResponse   `json:"key"`
	Metadata  *RESTKeyMetadataResponse `json:"metadata,omitempty"`
	Timestamp int64                    `json:"timestamp"`
}

// convertRESTGetKeyResponse converts REST GetKey response to service response.
func convertRESTGetKeyResponse(restResp *RESTGetKeyResponse) *service.GetKeyResponse {
	if restResp == nil {
		return nil
	}

	resp := &service.GetKeyResponse{
		Timestamp: time.Unix(restResp.Timestamp, 0),
	}

	if restResp.Key != nil {
		resp.Key = &service.KeyHandle{
			KeyID:     restResp.Key.KeyID,
			Version:   uint64(restResp.Key.Version),
			Algorithm: service.KeyAlgorithm(restResp.Key.Algorithm),
			Status:    service.KeyStatus(restResp.Key.Status),
		}
	}

	if restResp.Metadata != nil {
		resp.Metadata = &service.KeyMetadata{
			KeyID:       restResp.Metadata.KeyID,
			Algorithm:   service.KeyAlgorithm(restResp.Metadata.Algorithm),
			Status:      service.KeyStatus(restResp.Metadata.Status),
			Name:        restResp.Metadata.Name,
			Description: restResp.Metadata.Description,
			CreatedAt:   time.Unix(restResp.Metadata.CreatedAt, 0),
			UpdatedAt:   time.Unix(restResp.Metadata.UpdatedAt, 0),
			Tags:        restResp.Metadata.Tags,
			Owner:       restResp.Metadata.Owner,
			Version:     uint64(restResp.Metadata.Version),
		}
		for _, u := range restResp.Metadata.Usage {
			resp.Metadata.Usage = append(resp.Metadata.Usage, service.KeyUsage(u))
		}
	}

	return resp
}

// RESTListKeysResponse matches the REST API list keys response format.
type RESTListKeysResponse struct {
	Keys       []*RESTKeyHandleResponse `json:"keys"`
	NextCursor string                   `json:"next_cursor,omitempty"`
	HasMore    bool                     `json:"has_more"`
	TotalCount int                      `json:"total_count,omitempty"`
	Timestamp  int64                    `json:"timestamp"`
}

// convertRESTListKeysResponse converts REST ListKeys response to service response.
func convertRESTListKeysResponse(restResp *RESTListKeysResponse) *service.ListKeysResponse {
	if restResp == nil {
		return nil
	}

	resp := &service.ListKeysResponse{
		Keys:       make([]*service.KeyHandle, 0, len(restResp.Keys)),
		NextCursor: restResp.NextCursor,
		HasMore:    restResp.HasMore,
		TotalCount: restResp.TotalCount,
		Timestamp:  time.Unix(restResp.Timestamp, 0),
	}

	for _, k := range restResp.Keys {
		if k != nil {
			resp.Keys = append(resp.Keys, &service.KeyHandle{
				KeyID:     k.KeyID,
				Version:   uint64(k.Version),
				Algorithm: service.KeyAlgorithm(k.Algorithm),
				Status:    service.KeyStatus(k.Status),
			})
		}
	}

	return resp
}

// RESTRotateKeyResponse matches the REST API rotate key response format.
type RESTRotateKeyResponse struct {
	KeyID      string `json:"key_id"`
	OldVersion int    `json:"old_version"`
	NewVersion int    `json:"new_version"`
	Timestamp  int64  `json:"timestamp"`
}

// convertRESTRotateKeyResponse converts REST RotateKey response to service response.
func convertRESTRotateKeyResponse(restResp *RESTRotateKeyResponse) *service.RotateKeyResponse {
	if restResp == nil {
		return nil
	}

	return &service.RotateKeyResponse{
		KeyID:      restResp.KeyID,
		OldVersion: uint64(restResp.OldVersion),
		NewVersion: uint64(restResp.NewVersion),
		Timestamp:  time.Unix(restResp.Timestamp, 0),
	}
}

// RESTDeleteKeyResponse matches the REST API delete key response format.
type RESTDeleteKeyResponse struct {
	KeyID        string `json:"key_id"`
	Status       string `json:"status"`
	DeletionDate int64  `json:"deletion_date"`
	Timestamp    int64  `json:"timestamp"`
}

// convertRESTDeleteKeyResponse converts REST DeleteKey response to service response.
func convertRESTDeleteKeyResponse(restResp *RESTDeleteKeyResponse) *service.DeleteKeyResponse {
	if restResp == nil {
		return nil
	}

	return &service.DeleteKeyResponse{
		KeyID:        restResp.KeyID,
		Status:       service.KeyStatus(restResp.Status),
		DeletionDate: time.Unix(restResp.DeletionDate, 0),
		Timestamp:    time.Unix(restResp.Timestamp, 0),
	}
}

// RESTUpdateKeyStatusResponse matches the REST API update key status response format.
type RESTUpdateKeyStatusResponse struct {
	KeyID     string `json:"key_id"`
	Version   int    `json:"version"`
	OldStatus string `json:"old_status"`
	NewStatus string `json:"new_status"`
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
}

// convertRESTUpdateKeyStatusResponse converts REST UpdateKeyStatus response to service response.
func convertRESTUpdateKeyStatusResponse(restResp *RESTUpdateKeyStatusResponse) *service.UpdateKeyStatusResponse {
	if restResp == nil {
		return nil
	}

	return &service.UpdateKeyStatusResponse{
		KeyID:     restResp.KeyID,
		Version:   uint64(restResp.Version),
		OldStatus: service.KeyStatus(restResp.OldStatus),
		NewStatus: service.KeyStatus(restResp.NewStatus),
		Status:    service.KeyStatus(restResp.Status),
		Timestamp: time.Unix(restResp.Timestamp, 0),
	}
}

// convertKeyAttributesToREST converts KeyAttributes to REST format.
func convertKeyAttributesToREST(attrs *kctypes.KeyAttributes, usage []service.KeyUsage) *RESTKeyAttributesRequest {
	if attrs == nil {
		return nil
	}

	restAttrs := &RESTKeyAttributesRequest{
		Exportable: attrs.Exportable,
	}

	// Determine algorithm string based on KeyAttributes
	switch {
	case attrs.SymmetricAlgorithm == kctypes.SymmetricAES128GCM:
		restAttrs.Algorithm = "AES-128-GCM"
		restAttrs.KeySize = 128
	case attrs.SymmetricAlgorithm == kctypes.SymmetricAES192GCM:
		restAttrs.Algorithm = "AES-192-GCM"
		restAttrs.KeySize = 192
	case attrs.SymmetricAlgorithm == kctypes.SymmetricAES256GCM:
		restAttrs.Algorithm = "AES-256-GCM"
		restAttrs.KeySize = 256
	case attrs.SymmetricAlgorithm == kctypes.SymmetricChaCha20Poly1305:
		restAttrs.Algorithm = "CHACHA20-POLY1305"
		restAttrs.KeySize = 256
	case attrs.SymmetricAlgorithm == kctypes.SymmetricXChaCha20Poly1305:
		restAttrs.Algorithm = "XCHACHA20-POLY1305"
		restAttrs.KeySize = 256
	case attrs.KeyAlgorithm == x509.RSA && attrs.RSAAttributes != nil:
		restAttrs.KeySize = attrs.RSAAttributes.KeySize
		switch attrs.RSAAttributes.KeySize {
		case 2048:
			restAttrs.Algorithm = "RSA-2048"
		case 3072:
			restAttrs.Algorithm = "RSA-3072"
		case 4096:
			restAttrs.Algorithm = "RSA-4096"
		default:
			restAttrs.Algorithm = fmt.Sprintf("RSA-%d", attrs.RSAAttributes.KeySize)
		}
	case attrs.KeyAlgorithm == x509.ECDSA && attrs.ECCAttributes != nil:
		curveSize := attrs.ECCAttributes.Curve.Params().BitSize
		restAttrs.KeySize = curveSize
		switch curveSize {
		case 256:
			restAttrs.Algorithm = "ECDSA-P256"
		case 384:
			restAttrs.Algorithm = "ECDSA-P384"
		case 521:
			restAttrs.Algorithm = "ECDSA-P521"
		default:
			restAttrs.Algorithm = fmt.Sprintf("ECDSA-P%d", curveSize)
		}
	case attrs.KeyAlgorithm == x509.Ed25519:
		restAttrs.Algorithm = "ED25519"
		restAttrs.KeySize = 256
	default:
		restAttrs.Algorithm = "AES-256-GCM"
		restAttrs.KeySize = 256
	}

	// Convert usage
	for _, u := range usage {
		switch u {
		case service.KeyUsageEncrypt:
			restAttrs.KeyUsage = append(restAttrs.KeyUsage, "encrypt")
		case service.KeyUsageDecrypt:
			restAttrs.KeyUsage = append(restAttrs.KeyUsage, "decrypt")
		case service.KeyUsageSign:
			restAttrs.KeyUsage = append(restAttrs.KeyUsage, "sign")
		case service.KeyUsageVerify:
			restAttrs.KeyUsage = append(restAttrs.KeyUsage, "verify")
		case service.KeyUsageWrapKey:
			restAttrs.KeyUsage = append(restAttrs.KeyUsage, "wrap_key")
		case service.KeyUsageDeriveKey:
			restAttrs.KeyUsage = append(restAttrs.KeyUsage, "derive_key")
		case service.KeyUsageGenerateMAC:
			restAttrs.KeyUsage = append(restAttrs.KeyUsage, "generate_mac")
		case service.KeyUsageVerifyMAC:
			restAttrs.KeyUsage = append(restAttrs.KeyUsage, "verify_mac")
		}
	}

	return restAttrs
}

// GenerateKey generates a key via REST API.
func (c *RESTClient) GenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	// Convert to REST format
	restReq := &RESTGenerateKeyRequest{
		Name:          req.Name,
		Description:   req.Description,
		KeyAttributes: convertKeyAttributesToREST(req.KeyAttributes, req.Usage),
		Tags:          req.Tags,
		Owner:         req.Owner,
	}

	bodyBytes, err := json.Marshal(restReq)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/keys", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var restResult RESTGenerateKeyResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	return convertRESTToServiceResponse(&restResult), nil
}

// GetKey retrieves a key via REST API.
func (c *RESTClient) GetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/keys/%s", c.baseURL, req.KeyID)
	if req.Version > 0 {
		url += fmt.Sprintf("?version=%d", req.Version)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var restResult RESTGetKeyResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	return convertRESTGetKeyResponse(&restResult), nil
}

// ListKeys lists keys via REST API.
func (c *RESTClient) ListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/keys", c.baseURL)

	// Add query parameters
	if req.Limit > 0 || req.Cursor != "" || len(req.Filter) > 0 {
		url += "?"
		params := []string{}
		if req.Limit > 0 {
			params = append(params, fmt.Sprintf("limit=%d", req.Limit))
		}
		if req.Cursor != "" {
			params = append(params, fmt.Sprintf("cursor=%s", req.Cursor))
		}
		if prefix, ok := req.Filter["prefix"]; ok {
			params = append(params, fmt.Sprintf("prefix=%s", prefix))
		}
		url += strings.Join(params, "&")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var restResult RESTListKeysResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	return convertRESTListKeysResponse(&restResult), nil
}

// RotateKey rotates a key via REST API.
func (c *RESTClient) RotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/keys/%s/rotate", c.baseURL, req.KeyID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var restResult RESTRotateKeyResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	return convertRESTRotateKeyResponse(&restResult), nil
}

// DeleteKey deletes a key via REST API.
func (c *RESTClient) DeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/keys/%s", c.baseURL, req.KeyID)

	// Add query parameters for force and purge
	params := []string{}
	if req.Force {
		params = append(params, "force=true")
	}
	if req.PendingWindowDays > 0 {
		params = append(params, fmt.Sprintf("pending_window_days=%d", req.PendingWindowDays))
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var restResult RESTDeleteKeyResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	return convertRESTDeleteKeyResponse(&restResult), nil
}

// UpdateKeyStatus updates a key's status via REST API.
func (c *RESTClient) UpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/keys/%s/status", c.baseURL, req.KeyID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var restResult RESTUpdateKeyStatusResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	return convertRESTUpdateKeyStatusResponse(&restResult), nil
}

// restEncryptResponse matches the REST API response format for encrypt.
type restEncryptResponse struct {
	KeyID      string `json:"key_id"`
	KeyVersion uint64 `json:"key_version"`
	Ciphertext []byte `json:"ciphertext"`
	Nonce      []byte `json:"nonce,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

// Encrypt encrypts data via REST API.
func (c *RESTClient) Encrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	bodyBytes, err := json.Marshal(map[string]interface{}{
		"plaintext": req.Plaintext,
		"context":   req.Context,
	})
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/encrypt/%s", c.baseURL, req.KeyID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var restResult restEncryptResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	return &service.EncryptResponse{
		KeyID:      restResult.KeyID,
		KeyVersion: restResult.KeyVersion,
		Ciphertext: restResult.Ciphertext,
		Nonce:      restResult.Nonce,
		Timestamp:  time.Unix(restResult.Timestamp, 0),
	}, nil
}

// restDecryptResponse matches the REST API response format for decrypt.
type restDecryptResponse struct {
	KeyID      string `json:"key_id"`
	KeyVersion uint64 `json:"key_version"`
	Plaintext  []byte `json:"plaintext"`
	Timestamp  int64  `json:"timestamp"`
}

// Decrypt decrypts data via REST API.
func (c *RESTClient) Decrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	bodyBytes, err := json.Marshal(map[string]interface{}{
		"ciphertext": req.Ciphertext,
		"context":    req.Context,
	})
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/decrypt/%s", c.baseURL, req.KeyID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var restResult restDecryptResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	return &service.DecryptResponse{
		KeyID:      restResult.KeyID,
		KeyVersion: restResult.KeyVersion,
		Plaintext:  restResult.Plaintext,
		Timestamp:  time.Unix(restResult.Timestamp, 0),
	}, nil
}

// ListKeyVersions lists key versions via REST API.
// RESTKeyVersionInfo is the REST API version info response format.
type RESTKeyVersionInfo struct {
	Version   int    `json:"version"`
	Status    string `json:"status"`
	Algorithm string `json:"algorithm"`
	CreatedAt int64  `json:"created_at"`
}

// RESTListKeyVersionsResponse is the REST API response format for listing key versions.
type RESTListKeyVersionsResponse struct {
	KeyID      string                `json:"key_id"`
	Versions   []*RESTKeyVersionInfo `json:"versions"`
	NextCursor string                `json:"next_cursor,omitempty"`
	HasMore    bool                  `json:"has_more"`
	Timestamp  int64                 `json:"timestamp"`
}

func (c *RESTClient) ListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/keys/%s/versions", c.baseURL, req.KeyID)

	if req.Limit > 0 || req.Cursor != "" {
		url += "?"
		params := []string{}
		if req.Limit > 0 {
			params = append(params, fmt.Sprintf("limit=%d", req.Limit))
		}
		if req.Cursor != "" {
			params = append(params, fmt.Sprintf("cursor=%s", req.Cursor))
		}
		url += strings.Join(params, "&")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse into REST-specific format first
	var restResult RESTListKeyVersionsResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	// Convert to service type
	versions := make([]*service.KeyVersionInfo, len(restResult.Versions))
	for i, v := range restResult.Versions {
		versions[i] = &service.KeyVersionInfo{
			KeyID:     restResult.KeyID,
			Version:   uint64(v.Version),
			Status:    service.KeyStatus(v.Status),
			Algorithm: service.KeyAlgorithm(v.Algorithm),
			CreatedAt: time.Unix(v.CreatedAt, 0),
		}
	}

	return &service.ListKeyVersionsResponse{
		KeyID:      restResult.KeyID,
		Versions:   versions,
		NextCursor: restResult.NextCursor,
		HasMore:    restResult.HasMore,
		Timestamp:  time.Unix(restResult.Timestamp, 0),
	}, nil
}

// Cleanup and Assertion Helpers
// =============================================================================

// cleanupKey attempts to delete a test key.
func cleanupKey(t *testing.T, client *grpcclient.Client, ctx context.Context, keyID string) {
	t.Helper()

	req := &service.DeleteKeyRequest{
		KeyID: keyID,
		Force: true,
	}

	_, _ = client.KMSDeleteKey(ctx, req)
}

// assertKeyExists verifies a key exists.
func assertKeyExists(t *testing.T, client *grpcclient.Client, ctx context.Context, keyID string) {
	t.Helper()

	req := &service.GetKeyRequest{
		KeyID: keyID,
	}

	resp, err := client.KMSGetKey(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, keyID, resp.Metadata.KeyID)
}

// assertKeyNotFound verifies a key does not exist.
func assertKeyNotFound(t *testing.T, client *grpcclient.Client, ctx context.Context, keyID string) {
	t.Helper()

	req := &service.GetKeyRequest{
		KeyID: keyID,
	}

	_, err := client.KMSGetKey(ctx, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

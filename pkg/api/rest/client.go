//go:build rest

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

package rest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// RESTClient implements the QuicBaaS SDK client interfaces using REST API.
type RESTClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *logger.Logger
}

// ClientOption configures the REST client.
type ClientOption func(*RESTClient) error

// NewRESTClient creates a new REST API client with optional configuration.
func NewRESTClient(baseURL string, log *logger.Logger, opts ...ClientOption) (*RESTClient, error) {
	if baseURL == "" {
		return nil, api.NewRequiredFieldError("baseURL")
	}

	if log == nil {
		return nil, api.NewNilInputError("rest.NewRESTClient", "logger")
	}

	client := &RESTClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: log,
	}

	// Apply options
	for _, opt := range opts {
		if err := opt(client); err != nil {
			return nil, api.NewConfigurationError("rest.NewRESTClient", "apply client option", err)
		}
	}

	return client, nil
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *RESTClient) error {
		if timeout <= 0 {
			return api.NewInvalidInputError("timeout must be positive")
		}
		c.httpClient.Timeout = timeout
		return nil
	}
}

// WithTLSConfig sets custom TLS configuration.
func WithTLSConfig(tlsConfig *tls.Config) ClientOption {
	return func(c *RESTClient) error {
		if tlsConfig == nil {
			return api.NewNilInputError("rest.WithTLSConfig", "tlsConfig")
		}
		c.httpClient.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
		return nil
	}
}

// WithHTTPClient sets a caller-supplied HTTP client, overriding the default
// http.Client the constructor otherwise builds. This is the seam the QUIC
// (HTTP/3) CLI clients use to reuse this REST-dialect client unchanged: they
// supply an *http.Client whose Transport is quic-go's http3.Transport, so
// requests are framed as HTTP/3 while every endpoint mapping, JSON
// marshaling, and error decoding in this file stays identical to plain
// REST.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *RESTClient) error {
		if client == nil {
			return api.NewNilInputError("rest.WithHTTPClient", "client")
		}
		c.httpClient = client
		return nil
	}
}

// WithTLS configures TLS with optional client certificates.
func WithTLS(certFile, keyFile, caFile string) ClientOption {
	return func(c *RESTClient) error {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS13,
		}

		// Load client certificates if provided
		if certFile != "" && keyFile != "" {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				return api.NewOperationFailedError("load client certificates", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		// Load CA certificate if provided
		if caFile != "" {
			caCert, err := os.ReadFile(caFile)
			if err != nil {
				return api.NewOperationFailedError("read CA certificate", err)
			}

			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return api.NewInvalidFormatError("CA certificate", nil)
			}
			tlsConfig.RootCAs = caCertPool
		}

		c.httpClient.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
		return nil
	}
}

// Close closes the REST client.
func (c *RESTClient) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// =============================================================================
// CryptoClient Interface Implementation
// =============================================================================

// KMSEncrypt encrypts plaintext using the specified key.
func (c *RESTClient) KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSEncrypt", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/encrypt/%s", req.KeyID)

	reqBody := map[string]interface{}{
		"plaintext": req.Plaintext,
	}
	if len(req.Context) > 0 {
		reqBody["context"] = req.Context
	}

	var resp service.EncryptResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, reqBody, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSDecrypt decrypts ciphertext using the specified key.
func (c *RESTClient) KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSDecrypt", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/decrypt/%s", req.KeyID)

	reqBody := map[string]interface{}{
		"ciphertext": req.Ciphertext,
	}
	if len(req.Context) > 0 {
		reqBody["context"] = req.Context
	}

	var resp service.DecryptResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, reqBody, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSGenerateDataKey generates a new data encryption key.
// Uses the REST-specific DataKeyRESTRequest type.
func (c *RESTClient) KMSGenerateDataKey(ctx context.Context, req *DataKeyRESTRequest) (*DataKeyRESTResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSGenerateDataKey", "request")
	}

	// For data key generation, we need a key name from context or use default
	// The endpoint requires a key name, so we'll use "default" if not specified
	keyName := "default"
	endpoint := fmt.Sprintf("/api/v1/crypto/datakey/%s", keyName)

	var resp DataKeyRESTResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSGenerateDataKeyWithKEK generates a new data encryption key wrapped with specified KEK.
func (c *RESTClient) KMSGenerateDataKeyWithKEK(ctx context.Context, kekName string, req *DataKeyRESTRequest) (*DataKeyRESTResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSGenerateDataKeyWithKEK", "request")
	}

	if kekName == "" {
		return nil, api.NewRequiredFieldError("KEK name")
	}

	endpoint := fmt.Sprintf("/api/v1/crypto/datakey/%s", kekName)

	var resp DataKeyRESTResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// =============================================================================
// KMSClient Interface Implementation
// =============================================================================

// KMSGenerateKey creates a new cryptographic key.
// Uses REST-specific types that don't depend on service.KeySpec.
func (c *RESTClient) KMSGenerateKey(ctx context.Context, req *GenerateKeyRESTRequest) (*GenerateKeyRESTResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSGenerateKey", "request")
	}

	endpoint := "/api/v1/kms/keys"

	var resp GenerateKeyRESTResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSImportKey imports an external key.
// Uses REST-specific types that don't depend on service.KeySpec.
func (c *RESTClient) KMSImportKey(ctx context.Context, req *ImportKeyRESTRequest) (*GenerateKeyRESTResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSImportKey", "request")
	}

	endpoint := "/api/v1/kms/keys/import"

	var resp GenerateKeyRESTResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSGetKey retrieves key metadata by ID.
func (c *RESTClient) KMSGetKey(ctx context.Context, keyID string) (*GetKeyRESTResponse, error) {
	if keyID == "" {
		return nil, api.NewRequiredFieldError("keyID")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/keys/%s", keyID)

	var resp GetKeyRESTResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSListKeys lists all keys matching optional filters.
func (c *RESTClient) KMSListKeys(ctx context.Context, limit int, cursor string) (*ListKeysRESTResponse, error) {
	if limit <= 0 {
		limit = 100
	}

	endpoint := fmt.Sprintf("/api/v1/kms/keys?limit=%d", limit)
	if cursor != "" {
		endpoint += "&cursor=" + url.QueryEscape(cursor)
	}

	var resp ListKeysRESTResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSRotateKey creates a new version of an existing key.
func (c *RESTClient) KMSRotateKey(ctx context.Context, keyID string) (*RotateKeyRESTResponse, error) {
	if keyID == "" {
		return nil, api.NewRequiredFieldError("keyID")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/keys/%s/rotate", keyID)

	var resp RotateKeyRESTResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSDeleteKey schedules a key for deletion.
func (c *RESTClient) KMSDeleteKey(ctx context.Context, keyID string) (*DeleteKeyRESTResponse, error) {
	if keyID == "" {
		return nil, api.NewRequiredFieldError("keyID")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/keys/%s", keyID)

	var resp DeleteKeyRESTResponse
	if err := c.doRequest(ctx, http.MethodDelete, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSListKeyVersions lists all versions of a key.
func (c *RESTClient) KMSListKeyVersions(ctx context.Context, keyID string) (*ListKeyVersionsRESTResponse, error) {
	if keyID == "" {
		return nil, api.NewRequiredFieldError("keyID")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/keys/%s/versions", keyID)

	var resp ListKeyVersionsRESTResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSUpdateKeyStatus updates the status of a key.
func (c *RESTClient) KMSUpdateKeyStatus(ctx context.Context, keyID string, req *UpdateKeyStatusRESTRequest) (*UpdateKeyStatusRESTResponse, error) {
	if keyID == "" {
		return nil, api.NewRequiredFieldError("keyID")
	}

	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSUpdateKeyStatus", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/keys/%s/status", keyID)

	var resp UpdateKeyStatusRESTResponse
	if err := c.doRequest(ctx, http.MethodPut, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSCreateDEK creates a new Data Encryption Key.
func (c *RESTClient) KMSCreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSCreateDEK", "request")
	}

	endpoint := "/api/v1/kms/deks"

	var resp service.CreateDEKResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSGetDEK retrieves a DEK by path.
func (c *RESTClient) KMSGetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSGetDEK", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/deks/%s", url.PathEscape(req.Path))

	var resp service.GetDEKResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSListDEKs lists all DEKs matching optional filters.
func (c *RESTClient) KMSListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	if req == nil {
		req = &service.ListDEKsRequest{}
	}

	endpoint := "/api/v1/kms/deks"
	params := url.Values{}
	if req.Prefix != "" {
		params.Add("prefix", req.Prefix)
	}
	if req.Recursive {
		params.Add("recursive", "true")
	}
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	var resp service.ListDEKsResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSRotateDEK rotates a DEK to a new version.
func (c *RESTClient) KMSRotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSRotateDEK", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/deks/%s/rotate", url.PathEscape(req.Path))

	var resp service.RotateDEKResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSDeleteDEK deletes a DEK.
func (c *RESTClient) KMSDeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSDeleteDEK", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/deks/%s", url.PathEscape(req.Path))
	params := url.Values{}
	if req.Force {
		params.Add("force", "true")
	}
	if req.MigrateTo != "" {
		params.Add("migrate_to", req.MigrateTo)
	}
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	var resp service.DeleteDEKResponse
	if err := c.doRequest(ctx, http.MethodDelete, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSGetDEKStatus retrieves detailed DEK status.
func (c *RESTClient) KMSGetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSGetDEKStatus", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/deks/%s/status", url.PathEscape(req.Path))

	var resp service.GetDEKStatusResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSSetDefaultDEK sets a DEK as the default for its path.
func (c *RESTClient) KMSSetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSSetDefaultDEK", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/deks/%s/set-default", url.PathEscape(req.Path))

	var resp service.SetDefaultDEKResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSSetDEKSafety sets AEAD safety limits for a DEK.
func (c *RESTClient) KMSSetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSSetDEKSafety", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/deks/%s/safety", url.PathEscape(req.Path))

	var resp service.SetDEKSafetyResponse
	if err := c.doRequest(ctx, http.MethodPut, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSClearDEKSafety clears AEAD safety limits for a DEK.
func (c *RESTClient) KMSClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSClearDEKSafety", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/deks/%s/safety", url.PathEscape(req.Path))

	var resp service.ClearDEKSafetyResponse
	if err := c.doRequest(ctx, http.MethodDelete, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSExportKey exports key material wrapped with a wrapping key.
func (c *RESTClient) KMSExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSExportKey", "request")
	}

	if req.KeyID == "" {
		return nil, api.NewRequiredFieldError("keyID")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/keys/%s/export", url.PathEscape(req.KeyID))

	var resp service.ExportKeyResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSUnwrapKey imports key material that was wrapped with a wrapping key.
func (c *RESTClient) KMSUnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSUnwrapKey", "request")
	}

	endpoint := "/api/v1/kms/keys/unwrap"

	var resp service.UnwrapKeyResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSSetKeySafety sets AEAD safety limits for a key.
func (c *RESTClient) KMSSetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.KMSSetKeySafety", "request")
	}

	if req.KeyID == "" {
		return nil, api.NewRequiredFieldError("keyID")
	}

	endpoint := fmt.Sprintf("/api/v1/kms/keys/%s/safety", url.PathEscape(req.KeyID))

	var resp service.SetKeySafetyResponse
	if err := c.doRequest(ctx, http.MethodPut, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSSafetyCheck checks AEAD safety status for all keys approaching limits.
func (c *RESTClient) KMSSafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	if req == nil {
		req = &service.SafetyCheckRequest{}
	}

	endpoint := "/api/v1/kms/safety/check"
	if req.ThresholdPercent > 0 {
		endpoint += fmt.Sprintf("?threshold_percent=%s",
			url.QueryEscape(strconv.FormatFloat(req.ThresholdPercent, 'f', -1, 64)))
	}

	var resp service.SafetyCheckResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// KMSSafetyReport generates a detailed AEAD safety report for all keys.
func (c *RESTClient) KMSSafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	if req == nil {
		req = &service.SafetyReportRequest{}
	}

	endpoint := "/api/v1/kms/safety/report"
	if req.IncludeDetails {
		endpoint += "?include_details=true"
	}

	var resp service.SafetyReportResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// =============================================================================
// SealClient Interface Implementation
// =============================================================================

// SealStatus retrieves the current seal status.
func (c *RESTClient) SealStatus(ctx context.Context) (*service.SealStatusResponse, error) {
	endpoint := "/api/v1/seal/status"

	var resp service.SealStatusResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// SealInit initializes the seal with Shamir's Secret Sharing.
func (c *RESTClient) SealInit(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.SealInit", "request")
	}

	endpoint := "/api/v1/seal/init"

	var resp service.SealInitResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// Unseal unseals the database with a key share.
func (c *RESTClient) Unseal(ctx context.Context, req *service.UnsealRequest) (*service.UnsealResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.Unseal", "request")
	}

	endpoint := "/api/v1/seal/unseal"

	var resp service.UnsealResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// Seal seals the database.
func (c *RESTClient) Seal(ctx context.Context) error {
	endpoint := "/api/v1/seal/seal"

	if err := c.doRequest(ctx, http.MethodPost, endpoint, nil, nil); err != nil {
		return err
	}

	return nil
}

// Rekey rekeys the master encryption key.
func (c *RESTClient) Rekey(ctx context.Context, req *service.RekeyRequest) (*service.RekeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.Rekey", "request")
	}

	endpoint := "/api/v1/seal/rekey"

	var resp service.RekeyResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// =============================================================================
// SecretsClient Interface Implementation
// =============================================================================

// StoreSecret stores a new secret or creates a new version.
func (c *RESTClient) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.StoreSecret", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/secrets/%s", req.Name)

	var resp service.StoreSecretResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// GetSecret retrieves a secret by name and optional version.
func (c *RESTClient) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.GetSecret", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/secrets/%s", req.SecretID)
	if req.Version > 0 {
		endpoint += "?version=" + strconv.FormatUint(req.Version, 10)
	}

	var resp service.GetSecretResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// ListSecrets lists all secrets.
func (c *RESTClient) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	if req == nil {
		req = &service.ListSecretsRequest{}
	}

	endpoint := fmt.Sprintf("/api/v1/secrets?limit=%d", req.Limit)
	if req.Cursor != "" {
		endpoint += "&cursor=" + url.QueryEscape(req.Cursor)
	}
	if req.Prefix != "" {
		endpoint += "&prefix=" + url.QueryEscape(req.Prefix)
	}

	var resp service.ListSecretsResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// DeleteSecret deletes a secret.
func (c *RESTClient) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.DeleteSecret", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/secrets/%s", req.SecretID)
	if req.PermanentlyDelete {
		endpoint += "?destroy=true"
	}

	var resp service.DeleteSecretResponse
	if err := c.doRequest(ctx, http.MethodDelete, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// ListSecretVersions lists all versions of a secret.
func (c *RESTClient) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.ListSecretVersions", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/secrets/%s/versions", req.SecretID)

	var resp service.ListSecretVersionsResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// RollbackSecret rolls back to a previous version.
func (c *RESTClient) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.RollbackSecret", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/secrets/%s/rollback", req.SecretID)

	reqBody := map[string]interface{}{
		"version": req.TargetVersion,
	}

	var resp service.RollbackSecretResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, reqBody, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// RotateSecret rotates a secret to a new value.
func (c *RESTClient) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.RotateSecret", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/secrets/%s/rotate", req.SecretID)

	var resp service.RotateSecretResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// UpdateSecretStatus updates the status of a secret version.
func (c *RESTClient) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.UpdateSecretStatus", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/secrets/%s/status", req.SecretID)

	var resp service.UpdateSecretStatusResponse
	if err := c.doRequest(ctx, http.MethodPut, endpoint, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// GetSecretsByPrefix retrieves all secrets matching a path prefix.
func (c *RESTClient) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.GetSecretsByPrefix", "request")
	}

	if req.Prefix == "" {
		return nil, api.NewRequiredFieldError("prefix")
	}

	endpoint := fmt.Sprintf("/api/v1/secrets/prefix/%s", url.PathEscape(req.Prefix))
	params := url.Values{}
	if !req.IncludeData {
		params.Add("include_data", "false")
	}
	if req.Limit > 0 {
		params.Add("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		params.Add("cursor", req.Cursor)
	}
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	var resp service.GetSecretsByPrefixResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// WatchSecretsByPrefix watches for secret changes matching a prefix.
// Returns a channel that receives secret changes via Server-Sent Events (SSE).
func (c *RESTClient) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.WatchSecretsByPrefix", "request")
	}

	if req.Prefix == "" {
		return nil, api.NewRequiredFieldError("prefix")
	}

	endpoint := fmt.Sprintf("/api/v1/secrets/watch/%s", url.PathEscape(req.Prefix))
	if req.StartVersion > 0 {
		endpoint += "?start_version=" + strconv.FormatInt(req.StartVersion, 10)
	}

	fullURL := c.baseURL + endpoint

	// Create request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, api.NewOperationFailedError("create request", err)
	}

	// Set SSE headers
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Cache-Control", "no-cache")
	httpReq.Header.Set("Connection", "keep-alive")

	// Execute request
	// The body is closed on the error path below and by the streaming
	// goroutine that owns it once the stream is established.
	resp, err := c.httpClient.Do(httpReq) //nolint:bodyclose // closed by the SSE reader goroutine started below
	if err != nil {
		return nil, api.NewOperationFailedError("HTTP request", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respData, _ := io.ReadAll(resp.Body)
		return nil, api.NewHTTPStatusError("RESTClient.WatchSecretsByPrefix", errors.ErrInternal, resp.StatusCode, string(respData))
	}

	changes := make(chan *service.SecretChange, 100)

	// Start goroutine to read SSE events
	go func() {
		defer close(changes)
		defer resp.Body.Close()

		reader := bufio.NewReader(resp.Body)
		var eventType string
		var dataBuffer bytes.Buffer

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line, err := reader.ReadString('\n')
			if err != nil {
				// Connection closed or error
				return
			}

			line = strings.TrimSpace(line)

			if line == "" {
				// Empty line marks end of event
				if eventType == "change" && dataBuffer.Len() > 0 {
					var change service.SecretChange
					if err := json.Unmarshal(dataBuffer.Bytes(), &change); err == nil {
						select {
						case changes <- &change:
						case <-ctx.Done():
							return
						}
					}
				}
				eventType = ""
				dataBuffer.Reset()
				continue
			}

			if strings.HasPrefix(line, "event:") {
				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				dataBuffer.WriteString(data)
			}
		}
	}()

	return changes, nil
}

// =============================================================================
// SchedulerClient Interface Implementation
// =============================================================================

// SchedulerCreateJob creates a new scheduled job.
func (c *RESTClient) SchedulerCreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.SchedulerCreateJob", "request")
	}

	endpoint := "/api/v1/scheduler/jobs"

	// Build REST request body
	restReq := &CreateSchedulerJobRESTRequest{
		Name:           req.Name,
		Description:    req.Description,
		OwnerType:      string(req.OwnerType),
		OwnerID:        req.OwnerID,
		TargetTenantID: req.TargetTenantID,
		Type:           string(req.Type),
		Schedule:       req.Schedule,
		Delay:          req.Delay,
		Handler:        req.Handler,
		Payload:        req.Payload,
		Priority:       req.Priority,
		Timeout:        req.Timeout,
		MaxRetries:     req.MaxRetries,
		Labels:         req.Labels,
	}

	// Format ExecuteAt as RFC3339 if set
	if !req.ExecuteAt.IsZero() {
		restReq.ExecuteAt = req.ExecuteAt.Format(time.RFC3339)
	}

	var restResp CreateSchedulerJobRESTResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, restReq, &restResp); err != nil {
		return nil, err
	}

	// Convert REST response to service response
	return &service.CreateSchedulerJobResponse{
		Job:       schedulerJobFromREST(restResp.Job),
		Timestamp: restResp.Timestamp,
	}, nil
}

// SchedulerGetJob retrieves a job by ID.
func (c *RESTClient) SchedulerGetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.SchedulerGetJob", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/scheduler/jobs/%d", req.JobID)
	if req.TenantID != "" {
		endpoint += "?tenant_id=" + url.QueryEscape(req.TenantID)
	}

	var restResp GetSchedulerJobRESTResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &restResp); err != nil {
		return nil, err
	}

	return &service.GetSchedulerJobResponse{
		Job:       schedulerJobFromREST(restResp.Job),
		Timestamp: restResp.Timestamp,
	}, nil
}

// SchedulerUpdateJob updates an existing job.
func (c *RESTClient) SchedulerUpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.SchedulerUpdateJob", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/scheduler/jobs/%d", req.JobID)
	if req.TenantID != "" {
		endpoint += "?tenant_id=" + url.QueryEscape(req.TenantID)
	}

	// Build REST request body
	restReq := &UpdateSchedulerJobRESTRequest{
		Name:           req.Name,
		Description:    req.Description,
		Schedule:       req.Schedule,
		Handler:        req.Handler,
		Payload:        req.Payload,
		Priority:       req.Priority,
		TimeoutSeconds: int64(req.Timeout.Seconds()),
		MaxRetries:     req.MaxRetries,
		Labels:         req.Labels,
	}

	// Format ExecuteAt as RFC3339 if set
	if !req.ExecuteAt.IsZero() {
		restReq.ExecuteAt = req.ExecuteAt.Format(time.RFC3339)
	}

	var restResp UpdateSchedulerJobRESTResponse
	if err := c.doRequest(ctx, http.MethodPut, endpoint, restReq, &restResp); err != nil {
		return nil, err
	}

	return &service.UpdateSchedulerJobResponse{
		Job:       schedulerJobFromREST(restResp.Job),
		Timestamp: restResp.Timestamp,
	}, nil
}

// SchedulerDeleteJob removes a scheduled job.
func (c *RESTClient) SchedulerDeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.SchedulerDeleteJob", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/scheduler/jobs/%d", req.JobID)
	params := url.Values{}
	if req.TenantID != "" {
		params.Add("tenant_id", req.TenantID)
	}
	if req.Force {
		params.Add("force", "true")
	}
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	var restResp DeleteSchedulerJobRESTResponse
	if err := c.doRequest(ctx, http.MethodDelete, endpoint, nil, &restResp); err != nil {
		return nil, err
	}

	return &service.DeleteSchedulerJobResponse{
		JobID:     restResp.JobID,
		Deleted:   restResp.Deleted,
		Timestamp: restResp.Timestamp,
	}, nil
}

// SchedulerListJobs returns jobs matching the query criteria.
func (c *RESTClient) SchedulerListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if req == nil {
		req = &service.ListSchedulerJobsRequest{}
	}

	endpoint := "/api/v1/scheduler/jobs"
	params := url.Values{}

	if req.OwnerType != "" {
		params.Add("owner_type", string(req.OwnerType))
	}
	if req.OwnerID != "" {
		params.Add("owner_id", req.OwnerID)
	}
	if req.TargetTenantID != "" {
		params.Add("target_tenant_id", req.TargetTenantID)
	}
	if req.Status != "" {
		params.Add("status", string(req.Status))
	}
	if req.Handler != "" {
		params.Add("handler", req.Handler)
	}
	if req.Limit > 0 {
		params.Add("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		params.Add("cursor", req.Cursor)
	}

	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	var restResp ListSchedulerJobsRESTResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &restResp); err != nil {
		return nil, err
	}

	// Convert REST jobs to service jobs
	jobs := make([]*service.SchedulerJob, 0, len(restResp.Jobs))
	for _, restJob := range restResp.Jobs {
		jobs = append(jobs, schedulerJobFromREST(restJob))
	}

	return &service.ListSchedulerJobsResponse{
		Jobs:       jobs,
		NextCursor: restResp.NextCursor,
		HasMore:    restResp.HasMore,
		TotalCount: restResp.TotalCount,
		Timestamp:  restResp.Timestamp,
	}, nil
}

// SchedulerTriggerJob immediately executes a job.
func (c *RESTClient) SchedulerTriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.SchedulerTriggerJob", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/scheduler/jobs/%d/trigger", req.JobID)
	if req.TenantID != "" {
		endpoint += "?tenant_id=" + url.QueryEscape(req.TenantID)
	}

	var restResp TriggerSchedulerJobRESTResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, nil, &restResp); err != nil {
		return nil, err
	}

	return &service.TriggerSchedulerJobResponse{
		JobID:     restResp.JobID,
		Status:    service.SchedulerJobStatus(restResp.Status),
		Timestamp: restResp.Timestamp,
	}, nil
}

// SchedulerPauseJob pauses a scheduled job.
func (c *RESTClient) SchedulerPauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.SchedulerPauseJob", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/scheduler/jobs/%d/pause", req.JobID)
	if req.TenantID != "" {
		endpoint += "?tenant_id=" + url.QueryEscape(req.TenantID)
	}

	var restResp PauseSchedulerJobRESTResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, nil, &restResp); err != nil {
		return nil, err
	}

	return &service.PauseSchedulerJobResponse{
		JobID:     restResp.JobID,
		OldStatus: service.SchedulerJobStatus(restResp.OldStatus),
		NewStatus: service.SchedulerJobStatus(restResp.NewStatus),
		Timestamp: restResp.Timestamp,
	}, nil
}

// SchedulerResumeJob resumes a paused job.
func (c *RESTClient) SchedulerResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.SchedulerResumeJob", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/scheduler/jobs/%d/resume", req.JobID)
	if req.TenantID != "" {
		endpoint += "?tenant_id=" + url.QueryEscape(req.TenantID)
	}

	var restResp ResumeSchedulerJobRESTResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, nil, &restResp); err != nil {
		return nil, err
	}

	return &service.ResumeSchedulerJobResponse{
		JobID:     restResp.JobID,
		OldStatus: service.SchedulerJobStatus(restResp.OldStatus),
		NewStatus: service.SchedulerJobStatus(restResp.NewStatus),
		Timestamp: restResp.Timestamp,
	}, nil
}

// SchedulerCancelJob cancels a scheduled job.
func (c *RESTClient) SchedulerCancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.SchedulerCancelJob", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/scheduler/jobs/%d/cancel", req.JobID)
	if req.TenantID != "" {
		endpoint += "?tenant_id=" + url.QueryEscape(req.TenantID)
	}

	// Build request body if reason is provided
	var reqBody interface{}
	if req.Reason != "" {
		reqBody = &CancelSchedulerJobRESTRequest{
			Reason: req.Reason,
		}
	}

	var restResp CancelSchedulerJobRESTResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, reqBody, &restResp); err != nil {
		return nil, err
	}

	return &service.CancelSchedulerJobResponse{
		JobID:     restResp.JobID,
		OldStatus: service.SchedulerJobStatus(restResp.OldStatus),
		NewStatus: service.SchedulerJobStatus(restResp.NewStatus),
		Timestamp: restResp.Timestamp,
	}, nil
}

// SchedulerGetJobHistory returns execution history for a job.
func (c *RESTClient) SchedulerGetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.SchedulerGetJobHistory", "request")
	}

	endpoint := fmt.Sprintf("/api/v1/scheduler/jobs/%d/history", req.JobID)
	params := url.Values{}

	if req.TenantID != "" {
		params.Add("tenant_id", req.TenantID)
	}
	if req.Limit > 0 {
		params.Add("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		params.Add("cursor", req.Cursor)
	}

	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	var restResp GetSchedulerJobHistoryRESTResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &restResp); err != nil {
		return nil, err
	}

	// Convert REST executions to service executions
	executions := make([]*service.SchedulerJobExecution, 0, len(restResp.Executions))
	for _, restExec := range restResp.Executions {
		executions = append(executions, schedulerExecutionFromREST(restExec))
	}

	return &service.GetSchedulerJobHistoryResponse{
		Executions: executions,
		NextCursor: restResp.NextCursor,
		HasMore:    restResp.HasMore,
		Timestamp:  restResp.Timestamp,
	}, nil
}

// SchedulerGetStatus returns scheduler status and statistics.
func (c *RESTClient) SchedulerGetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	endpoint := "/api/v1/scheduler/status"

	var restResp GetSchedulerStatusRESTResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &restResp); err != nil {
		return nil, err
	}

	var stats *service.SchedulerStats
	if restResp.Stats != nil {
		stats = &service.SchedulerStats{
			JobCount:     restResp.Stats.JobCount,
			RunningJobs:  restResp.Stats.RunningJobs,
			SuccessCount: restResp.Stats.SuccessCount,
			FailureCount: restResp.Stats.FailureCount,
			LastTickTime: restResp.Stats.LastTickTime,
			StartedAt:    restResp.Stats.StartedAt,
		}
	}

	return &service.GetSchedulerStatusResponse{
		Running:   restResp.Running,
		Stats:     stats,
		Timestamp: restResp.Timestamp,
	}, nil
}

// =============================================================================
// Scheduler Conversion Helpers
// =============================================================================

// schedulerJobFromREST converts a REST SchedulerJobRESTResponse to service.SchedulerJob.
func schedulerJobFromREST(restJob *SchedulerJobRESTResponse) *service.SchedulerJob {
	if restJob == nil {
		return nil
	}

	return &service.SchedulerJob{
		ID:             restJob.ID,
		Name:           restJob.Name,
		Description:    restJob.Description,
		OwnerType:      service.SchedulerOwnerType(restJob.OwnerType),
		OwnerID:        restJob.OwnerID,
		TargetTenantID: restJob.TargetTenantID,
		CreatedBy:      restJob.CreatedBy,
		Type:           service.SchedulerJobType(restJob.Type),
		Schedule:       restJob.Schedule,
		ExecuteAt:      restJob.ExecuteAt,
		Delay:          restJob.Delay,
		Handler:        restJob.Handler,
		Payload:        restJob.Payload,
		Priority:       restJob.Priority,
		Timeout:        restJob.Timeout,
		MaxRetries:     restJob.MaxRetries,
		Status:         service.SchedulerJobStatus(restJob.Status),
		LastRun:        restJob.LastRun,
		NextRun:        restJob.NextRun,
		RunCount:       restJob.RunCount,
		FailCount:      restJob.FailCount,
		LastError:      restJob.LastError,
		Labels:         restJob.Labels,
		CreatedAt:      restJob.CreatedAt,
		UpdatedAt:      restJob.UpdatedAt,
	}
}

// schedulerExecutionFromREST converts a REST SchedulerJobExecutionRESTResponse to service.SchedulerJobExecution.
func schedulerExecutionFromREST(restExec *SchedulerJobExecutionRESTResponse) *service.SchedulerJobExecution {
	if restExec == nil {
		return nil
	}

	return &service.SchedulerJobExecution{
		ID:        restExec.ID,
		JobID:     restExec.JobID,
		Status:    service.SchedulerJobStatus(restExec.Status),
		StartedAt: restExec.StartedAt,
		EndedAt:   restExec.EndedAt,
		Duration:  restExec.Duration,
		Error:     restExec.Error,
		Output:    restExec.Output,
	}
}

// =============================================================================
// HTTP Request Helper
// =============================================================================

// doRequest performs an HTTP request and handles JSON marshaling/unmarshaling.
func (c *RESTClient) doRequest(ctx context.Context, method, path string, reqBody, respBody interface{}) error {
	// Build URL
	fullURL := c.baseURL + path

	// Marshal request body if provided
	var bodyReader io.Reader
	if reqBody != nil {
		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return api.NewEncodeError("rest", "request", err)
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return api.NewOperationFailedError("create request", err)
	}

	// Set headers
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return api.NewOperationFailedError("HTTP request", err)
	}
	defer resp.Body.Close()

	// Read response body
	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return api.NewReadError("rest", "response", err)
	}

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to parse error response
		var errResp ErrorResponse
		if err := json.Unmarshal(respData, &errResp); err == nil {
			return c.convertErrorResponse(&errResp, resp.StatusCode)
		}

		// Fallback to generic error
		return api.NewHTTPStatusError("RESTClient.doRequest", errors.ErrInternal, resp.StatusCode, string(respData))
	}

	// Unmarshal response body if output is provided
	if respBody != nil && len(respData) > 0 {
		if err := json.Unmarshal(respData, respBody); err != nil {
			return api.NewDecodeError("rest", "response", err)
		}
	}

	return nil
}

// convertErrorResponse converts REST error to QBError.
func (c *RESTClient) convertErrorResponse(errResp *ErrorResponse, statusCode int) error {
	var code errors.ErrorCode

	switch statusCode {
	case http.StatusBadRequest:
		code = errors.ErrInvalidInput
	case http.StatusUnauthorized:
		code = errors.ErrUnauthorized
	case http.StatusForbidden:
		code = errors.ErrForbidden
	case http.StatusNotFound:
		code = errors.ErrNotFound
	case http.StatusConflict:
		code = errors.ErrAlreadyExists
	case http.StatusRequestTimeout:
		code = errors.ErrTimeout
	case http.StatusTooManyRequests:
		code = errors.ErrResourceExhausted
	case http.StatusServiceUnavailable:
		code = errors.ErrLeaderNotAvailable
	case http.StatusNotImplemented:
		code = errors.ErrNotImplemented
	default:
		code = errors.ErrInternal
	}

	return api.NewRemoteError("RESTClient", code, errResp.Error)
}

// TSWritePoint writes a single time series data point.
func (c *RESTClient) TSWritePoint(ctx context.Context, req *cli.WritePointRequest) (*cli.WritePointResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.TSWritePoint", "request")
	}

	var resp cli.WritePointResponse
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/timeseries/write", req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// TSQuery queries time series data.
func (c *RESTClient) TSQuery(ctx context.Context, req *cli.TSQueryRequest) (*cli.TSQueryResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.TSQuery", "request")
	}

	var resp cli.TSQueryResponse
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/timeseries/query", req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// ClusterStatus returns the current cluster status.
func (c *RESTClient) ClusterStatus(ctx context.Context) (*cli.ClusterStatusResponse, error) {
	var resp ClusterStatusResponse
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/cluster/status", nil, &resp); err != nil {
		return nil, err
	}

	// Convert LeaderID from string to uint64
	var leaderID uint64
	if resp.LeaderID != "" {
		leaderID, _ = strconv.ParseUint(resp.LeaderID, 10, 64)
	}

	// Convert to CLI response
	cliResp := &cli.ClusterStatusResponse{
		Healthy:    resp.Healthy,
		LeaderID:   leaderID,
		NodeCount:  resp.TotalNodes,
		ReadyNodes: resp.TotalNodes, // Assume all nodes are ready
	}

	return cliResp, nil
}

// Ping checks server connectivity and health.
func (c *RESTClient) Ping(ctx context.Context) error {
	var resp HealthResponse
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/health", nil, &resp); err != nil {
		return err
	}

	if resp.Status != "healthy" {
		return api.NewServiceUnavailableError("server (unhealthy: " + resp.Status + ")")
	}

	return nil
}

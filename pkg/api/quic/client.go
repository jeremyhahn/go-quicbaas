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

// Package quic provides a high-performance QUIC transport layer for QuicBaaS.
package quic

import (
	"context"
	"crypto/tls"
	"sync"
	"time"

	quicgo "github.com/quic-go/quic-go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Client is a QUIC client for QuicBaaS that implements KMS, Crypto, Secrets, and Seal interfaces.
// It provides a high-performance binary protocol over QUIC with connection migration support.
type Client struct {
	addr       string
	conn       *quicgo.Conn
	tlsConfig  *tls.Config
	logger     *logger.Logger
	ctx        context.Context
	cancelFunc context.CancelFunc

	// Connection management
	mu        sync.RWMutex
	connected bool
	closed    bool
}

// ClientOption configures the QUIC client.
type ClientOption func(*clientConfig)

type clientConfig struct {
	tlsConfig      *tls.Config
	timeout        time.Duration
	logger         *logger.Logger
	maxIdleTimeout time.Duration
}

// WithTLSConfig sets custom TLS configuration.
func WithTLSConfig(cfg *tls.Config) ClientOption {
	return func(c *clientConfig) {
		c.tlsConfig = cfg
	}
}

// WithTimeout sets the connection timeout.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.timeout = timeout
	}
}

// WithLogger sets a custom logger.
func WithLogger(log *logger.Logger) ClientOption {
	return func(c *clientConfig) {
		c.logger = log
	}
}

// WithMaxIdleTimeout sets the maximum idle timeout for the connection.
func WithMaxIdleTimeout(timeout time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.maxIdleTimeout = timeout
	}
}

// NewClient creates a new QUIC client connected to the specified address.
func NewClient(addr string, opts ...ClientOption) (*Client, error) {
	cfg := &clientConfig{
		timeout:        10 * time.Second,
		maxIdleTimeout: 30 * time.Second,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	// Create logger if not provided
	if cfg.logger == nil {
		logCfg := logger.LogConfig{
			Level:     "warn",
			Format:    "json",
			Output:    "stderr",
			Component: "quic-client",
		}
		log, err := logger.New(logCfg)
		if err != nil {
			return nil, api.NewConfigurationError("NewClient", "create logger", err)
		}
		cfg.logger = log
	}

	// Setup TLS configuration
	tlsConf := cfg.tlsConfig
	if tlsConf == nil {
		tlsConf = &tls.Config{
			NextProtos: []string{"quicbaas-quic"},
			MinVersion: tls.VersionTLS13,
		}
	} else {
		// Ensure NextProtos is set
		if len(tlsConf.NextProtos) == 0 {
			tlsConf.NextProtos = []string{"quicbaas-quic"}
		}
	}

	// Create context for connection lifecycle
	ctx, cancel := context.WithCancel(context.Background())

	client := &Client{
		addr:       addr,
		tlsConfig:  tlsConf,
		logger:     cfg.logger,
		ctx:        ctx,
		cancelFunc: cancel,
	}

	// Establish connection
	connCtx, connCancel := context.WithTimeout(ctx, cfg.timeout)
	defer connCancel()

	quicConf := &quicgo.Config{
		MaxIdleTimeout:  cfg.maxIdleTimeout,
		KeepAlivePeriod: 15 * time.Second,
	}

	conn, err := quicgo.DialAddr(connCtx, addr, tlsConf, quicConf)
	if err != nil {
		cancel()
		return nil, api.NewConnectionError("NewClient", "dial QUIC server at "+addr, "", err)
	}

	client.conn = conn
	client.connected = true

	cfg.logger.Debug("QUIC client connected")

	return client, nil
}

// Close closes the QUIC connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	c.connected = false
	c.cancelFunc()

	if c.conn != nil {
		err := c.conn.CloseWithError(0, "client closing")
		c.logger.Debug("QUIC client closed")
		return err
	}

	return nil
}

// sendRequest sends a QUIC request and returns the response.
func (c *Client) sendRequest(ctx context.Context, opcode byte, reqPayload []byte) (*Response, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, api.NewClientClosedError("sendRequest")
	}
	conn := c.conn
	c.mu.RUnlock()

	// Open a stream
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, api.NewConnectionError("sendRequest", "open stream", "", err)
	}
	defer stream.Close()

	// Write request
	req := &Request{
		Opcode:  opcode,
		Payload: reqPayload,
	}

	if err := WriteRequest(stream, req); err != nil {
		return nil, api.NewWriteError("sendRequest", "request", err)
	}

	// Read response
	resp, err := ReadResponse(stream)
	if err != nil {
		return nil, api.NewReadError("sendRequest", "response", err)
	}

	// Check status
	if resp.Status != StatusOK {
		errMsg := string(resp.Payload)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, StatusToError(resp.Status, errMsg)
	}

	return resp, nil
}

// =============================================================================
// Data Operations
// =============================================================================

// Put stores a key-value pair.
func (c *Client) Put(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.Put", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.Put", "request", err)
	}

	resp, err := c.sendRequest(ctx, OpPut, payload)
	if err != nil {
		return nil, err
	}

	var result service.PutResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.Put", "response", err)
	}

	return &result, nil
}

// Get retrieves a value by key.
func (c *Client) Get(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.Get", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.Get", "request", err)
	}

	resp, err := c.sendRequest(ctx, OpGet, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.Get", "response", err)
	}

	return &result, nil
}

// Delete removes a key-value pair.
func (c *Client) Delete(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.Delete", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.Delete", "request", err)
	}

	resp, err := c.sendRequest(ctx, OpDelete, payload)
	if err != nil {
		return nil, err
	}

	var result service.DeleteResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.Delete", "response", err)
	}

	return &result, nil
}

// List returns entities matching the filter.
func (c *Client) List(ctx context.Context, req *service.ListRequest) (*service.ListResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.List", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.List", "request", err)
	}

	resp, err := c.sendRequest(ctx, OpList, payload)
	if err != nil {
		return nil, err
	}

	var result service.ListResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.List", "response", err)
	}

	return &result, nil
}

// =============================================================================
// KMS Operations
// =============================================================================

// KMSGenerateKey creates a new cryptographic key.
func (c *Client) KMSGenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGenerateKey", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSGenerateKey, payload)
	if err != nil {
		return nil, err
	}

	var result service.GenerateKeyResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSImportKey imports an external key into the KMS.
func (c *Client) KMSImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSImportKey", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSImportKey, payload)
	if err != nil {
		return nil, err
	}

	var result service.ImportKeyResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSGetKey retrieves key metadata by ID.
func (c *Client) KMSGetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGetKey", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSGetKey, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetKeyResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSListKeys lists all keys matching optional filters.
func (c *Client) KMSListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	if req == nil {
		req = &service.ListKeysRequest{}
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSListKeys, payload)
	if err != nil {
		return nil, err
	}

	var result service.ListKeysResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSRotateKey creates a new version of an existing key.
func (c *Client) KMSRotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSRotateKey", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSRotateKey, payload)
	if err != nil {
		return nil, err
	}

	var result service.RotateKeyResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSDeleteKey schedules a key for deletion.
func (c *Client) KMSDeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSDeleteKey", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSDeleteKey, payload)
	if err != nil {
		return nil, err
	}

	var result service.DeleteKeyResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSEncrypt encrypts plaintext using the specified key.
func (c *Client) KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSEncrypt", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSEncrypt, payload)
	if err != nil {
		return nil, err
	}

	var result service.EncryptResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSDecrypt decrypts ciphertext using the specified key.
func (c *Client) KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSDecrypt", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSDecrypt, payload)
	if err != nil {
		return nil, err
	}

	var result service.DecryptResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSListKeyVersions lists all versions of a cryptographic key.
func (c *Client) KMSListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSListKeyVersions", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSKeyVersions, payload)
	if err != nil {
		return nil, err
	}

	var result service.ListKeyVersionsResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSUpdateKeyStatus updates the status of a key or key version.
func (c *Client) KMSUpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSUpdateKeyStatus", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSKeyStatus, payload)
	if err != nil {
		return nil, err
	}

	var result service.UpdateKeyStatusResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSGenerateDataKey generates a new data encryption key.
func (c *Client) KMSGenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGenerateDataKey", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpCryptoDataKey, payload)
	if err != nil {
		return nil, err
	}

	var result service.GenerateDataKeyResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// =============================================================================
// DEK Operations
// =============================================================================

// KMSCreateDEK creates a new Data Encryption Key.
func (c *Client) KMSCreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSCreateDEK", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSDEKCreate, payload)
	if err != nil {
		return nil, err
	}

	var result service.CreateDEKResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSGetDEK retrieves a DEK by path.
func (c *Client) KMSGetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGetDEK", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSDEKGet, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetDEKResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSListDEKs lists all DEKs matching optional filters.
func (c *Client) KMSListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	if req == nil {
		req = &service.ListDEKsRequest{}
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSDEKList, payload)
	if err != nil {
		return nil, err
	}

	var result service.ListDEKsResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSRotateDEK rotates a DEK to a new version.
func (c *Client) KMSRotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSRotateDEK", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSDEKRotate, payload)
	if err != nil {
		return nil, err
	}

	var result service.RotateDEKResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSDeleteDEK deletes a DEK.
func (c *Client) KMSDeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSDeleteDEK", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSDEKDelete, payload)
	if err != nil {
		return nil, err
	}

	var result service.DeleteDEKResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSGetDEKStatus retrieves detailed DEK status.
func (c *Client) KMSGetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSGetDEKStatus", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSDEKStatus, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetDEKStatusResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSSetDefaultDEK sets a DEK as the default for its path.
func (c *Client) KMSSetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSSetDefaultDEK", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSDEKSetDefault, payload)
	if err != nil {
		return nil, err
	}

	var result service.SetDefaultDEKResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSSetDEKSafety sets AEAD safety limits for a DEK.
func (c *Client) KMSSetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSSetDEKSafety", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSDEKSetSafety, payload)
	if err != nil {
		return nil, err
	}

	var result service.SetDEKSafetyResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSClearDEKSafety clears AEAD safety limits for a DEK.
func (c *Client) KMSClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSClearDEKSafety", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSDEKClearSafety, payload)
	if err != nil {
		return nil, err
	}

	var result service.ClearDEKSafetyResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// =============================================================================
// Key Export/Unwrap/Safety Operations
// =============================================================================

// KMSExportKey exports key material wrapped with a wrapping key.
func (c *Client) KMSExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSExportKey", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSExportKey, payload)
	if err != nil {
		return nil, err
	}

	var result service.ExportKeyResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSUnwrapKey imports key material that was wrapped with a wrapping key.
func (c *Client) KMSUnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSUnwrapKey", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSUnwrapKey, payload)
	if err != nil {
		return nil, err
	}

	var result service.UnwrapKeyResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSSetKeySafety sets AEAD safety limits for a key.
func (c *Client) KMSSetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSSetKeySafety", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSSetKeySafety, payload)
	if err != nil {
		return nil, err
	}

	var result service.SetKeySafetyResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSClearKeySafety clears AEAD safety limits for a key.
func (c *Client) KMSClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.KMSClearKeySafety", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSClearKeySafety, payload)
	if err != nil {
		return nil, err
	}

	var result service.ClearKeySafetyResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSSafetyCheck checks AEAD safety status for all keys approaching limits.
func (c *Client) KMSSafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	if req == nil {
		req = &service.SafetyCheckRequest{}
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSSafetyCheck, payload)
	if err != nil {
		return nil, err
	}

	var result service.SafetyCheckResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSSafetyReport generates a detailed AEAD safety report for all keys.
func (c *Client) KMSSafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	if req == nil {
		req = &service.SafetyReportRequest{}
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSSafetyReport, payload)
	if err != nil {
		return nil, err
	}

	var result service.SafetyReportResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// =============================================================================
// Backend Operations
// =============================================================================

// KMSBackendStatus returns the KMS backend status.
func (c *Client) KMSBackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	payload, err := EncodePayload(nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSBackendStatus, payload)
	if err != nil {
		return nil, err
	}

	var result service.BackendStatusResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// KMSBackendTest tests the KMS backend.
func (c *Client) KMSBackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	payload, err := EncodePayload(nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpKMSBackendTest, payload)
	if err != nil {
		return nil, err
	}

	var result service.BackendTestResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// =============================================================================
// Barrier Operations
//
// These methods replace the deprecated seal-tree client methods (SealStatus,
// SealInit, Unseal, Seal, Rekey) removed in Phase 8c. All calls multiplex
// over the single OpBarrier wire opcode with the sub-operation byte
// prepended to the JSON payload.
// =============================================================================

// callBarrier executes a barrier sub-operation. The encoded request body is
// prefixed with the sub-opcode byte so the server's handleBarrierDispatch
// can route to the correct handler.
func (c *Client) callBarrier(ctx context.Context, subOp byte, body interface{}) (*Response, error) {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = EncodePayload(body)
		if err != nil {
			return nil, err
		}
	}
	payload := make([]byte, 1+len(bodyBytes))
	payload[0] = subOp
	copy(payload[1:], bodyBytes)
	return c.sendRequest(ctx, OpBarrier, payload)
}

// BarrierStatus returns the current barrier status (mirrors
// /api/v1/barrier/status). The response is decoded against the JSON shape
// emitted by handler_barrier.go.
func (c *Client) BarrierStatus(ctx context.Context) (*BarrierStatusResponse, error) {
	resp, err := c.callBarrier(ctx, SubOpBarrierStatus, nil)
	if err != nil {
		return nil, err
	}
	var out BarrierStatusResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BarrierInitialize initialises the barrier with a single passphrase
// (mirrors /api/v1/barrier/initialize).
func (c *Client) BarrierInitialize(ctx context.Context, req *BarrierInitializeRequest) error {
	if req == nil {
		return api.NewNilInputError("Client.BarrierInitialize", "request")
	}
	_, err := c.callBarrier(ctx, SubOpBarrierInitialize, req)
	return err
}

// BarrierUnseal unseals the barrier with a single passphrase
// (mirrors /api/v1/barrier/unseal).
func (c *Client) BarrierUnseal(ctx context.Context, req *BarrierUnsealRequest) error {
	if req == nil {
		return api.NewNilInputError("Client.BarrierUnseal", "request")
	}
	_, err := c.callBarrier(ctx, SubOpBarrierUnseal, req)
	return err
}

// BarrierSeal transitions the barrier into the sealed state (mirrors
// /api/v1/barrier/seal).
func (c *Client) BarrierSeal(ctx context.Context) error {
	_, err := c.callBarrier(ctx, SubOpBarrierSeal, nil)
	return err
}

// BarrierShamirInitialize initialises the barrier with Shamir secret sharing
// (mirrors /api/v1/barrier/shamir/initialize).
func (c *Client) BarrierShamirInitialize(ctx context.Context, req *BarrierShamirInitializeRequest) (*ShamirInitResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.BarrierShamirInitialize", "request")
	}
	resp, err := c.callBarrier(ctx, SubOpBarrierShamirInitialize, req)
	if err != nil {
		return nil, err
	}
	var out ShamirInitResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BarrierShamirUnsealShare submits a single Shamir share (mirrors
// /api/v1/barrier/shamir/unseal-share).
func (c *Client) BarrierShamirUnsealShare(ctx context.Context, req *BarrierShamirUnsealShareRequest) (*UnsealShareProgressResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.BarrierShamirUnsealShare", "request")
	}
	resp, err := c.callBarrier(ctx, SubOpBarrierShamirUnsealShare, req)
	if err != nil {
		return nil, err
	}
	var out UnsealShareProgressResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BarrierShamirUnsealShares submits the complete set of Shamir shares
// (mirrors /api/v1/barrier/shamir/unseal-shares).
func (c *Client) BarrierShamirUnsealShares(ctx context.Context, req *BarrierShamirUnsealSharesRequest) error {
	if req == nil {
		return api.NewNilInputError("Client.BarrierShamirUnsealShares", "request")
	}
	_, err := c.callBarrier(ctx, SubOpBarrierShamirUnsealShares, req)
	return err
}

// BarrierRekey generates new Shamir shares for the existing root key
// (mirrors POST /api/v1/barrier/rekey).
func (c *Client) BarrierRekey(ctx context.Context, req *BarrierRekeyRequest) (*ShamirInitResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.BarrierRekey", "request")
	}
	resp, err := c.callBarrier(ctx, SubOpBarrierRekey, req)
	if err != nil {
		return nil, err
	}
	var out ShamirInitResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BarrierGenerateRecoveryKeys generates recovery shares (mirrors POST
// /api/v1/barrier/recovery/generate).
func (c *Client) BarrierGenerateRecoveryKeys(ctx context.Context, req *BarrierRecoveryGenerateRequest) (*RecoveryKeysResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.BarrierGenerateRecoveryKeys", "request")
	}
	resp, err := c.callBarrier(ctx, SubOpBarrierRecoveryGenerate, req)
	if err != nil {
		return nil, err
	}
	var out RecoveryKeysResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BarrierRecoverWithKeys unseals the barrier using recovery shares
// (mirrors POST /api/v1/barrier/recovery/recover).
func (c *Client) BarrierRecoverWithKeys(ctx context.Context, req *BarrierRecoveryRecoverRequest) error {
	if req == nil {
		return api.NewNilInputError("Client.BarrierRecoverWithKeys", "request")
	}
	_, err := c.callBarrier(ctx, SubOpBarrierRecoveryRecover, req)
	return err
}

// BarrierDeleteRecoveryKeys removes recovery key metadata (mirrors DELETE
// /api/v1/barrier/recovery).
func (c *Client) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	_, err := c.callBarrier(ctx, SubOpBarrierRecoveryDelete, nil)
	return err
}

// BarrierHasRecoveryKeys reports whether recovery key metadata exists
// (mirrors GET /api/v1/barrier/recovery/status).
func (c *Client) BarrierHasRecoveryKeys(ctx context.Context) (*RecoveryStatusResponse, error) {
	resp, err := c.callBarrier(ctx, SubOpBarrierRecoveryStatus, nil)
	if err != nil {
		return nil, err
	}
	var out RecoveryStatusResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BarrierGenerateRootToken creates a new root token by proving knowledge of
// the master key through Shamir shares (mirrors POST
// /api/v1/barrier/root-token).
func (c *Client) BarrierGenerateRootToken(ctx context.Context, req *BarrierRootTokenRequest) (*RootTokenResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.BarrierGenerateRootToken", "request")
	}
	resp, err := c.callBarrier(ctx, SubOpBarrierRootToken, req)
	if err != nil {
		return nil, err
	}
	var out RootTokenResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// =============================================================================
// Ceremony Operations
//
// These methods multiplex over the single OpCeremony wire opcode with the
// ceremony sub-operation byte prepended to the JSON payload.
// =============================================================================

// callCeremony executes a ceremony sub-operation, prefixing the JSON body
// with the ceremony sub-opcode byte.
func (c *Client) callCeremony(ctx context.Context, subOp byte, body interface{}) (*Response, error) {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = EncodePayload(body)
		if err != nil {
			return nil, err
		}
	}
	payload := make([]byte, 1+len(bodyBytes))
	payload[0] = subOp
	copy(payload[1:], bodyBytes)
	return c.sendRequest(ctx, OpCeremony, payload)
}

// CeremonyInitialize starts the initialization ceremony (mirrors POST
// /api/v1/init/initialize).
func (c *Client) CeremonyInitialize(ctx context.Context, req *CeremonyInitializeRequest) (*CeremonyInitResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.CeremonyInitialize", "request")
	}
	resp, err := c.callCeremony(ctx, SubOpCeremonyInitialize, req)
	if err != nil {
		return nil, err
	}
	var out CeremonyInitResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CeremonyStatus returns the current ceremony state (mirrors GET
// /api/v1/init/status).
func (c *Client) CeremonyStatus(ctx context.Context) (*CeremonyStatusResponse, error) {
	resp, err := c.callCeremony(ctx, SubOpCeremonyStatus, nil)
	if err != nil {
		return nil, err
	}
	var out CeremonyStatusResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CeremonyClaimCertBegin starts a certificate claim challenge (mirrors POST
// /api/v1/init/claim-cert/begin).
func (c *Client) CeremonyClaimCertBegin(ctx context.Context, req *CeremonyClaimCertBeginRequest) (*CeremonyChallengeResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.CeremonyClaimCertBegin", "request")
	}
	resp, err := c.callCeremony(ctx, SubOpCeremonyClaimCertBegin, req)
	if err != nil {
		return nil, err
	}
	var out CeremonyChallengeResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CeremonyClaimCertComplete completes a certificate claim by submitting a
// signed nonce (mirrors POST /api/v1/init/claim-cert/complete).
func (c *Client) CeremonyClaimCertComplete(ctx context.Context, req *CeremonyClaimCertCompleteRequest) (*CeremonyClaimCertResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.CeremonyClaimCertComplete", "request")
	}
	resp, err := c.callCeremony(ctx, SubOpCeremonyClaimCertComplete, req)
	if err != nil {
		return nil, err
	}
	var out CeremonyClaimCertResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CeremonyClaimShare retrieves the Shamir share for an officer (mirrors POST
// /api/v1/init/claim-share).
func (c *Client) CeremonyClaimShare(ctx context.Context, req *CeremonyClaimShareRequest) (*CeremonyClaimShareResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.CeremonyClaimShare", "request")
	}
	resp, err := c.callCeremony(ctx, SubOpCeremonyClaimShare, req)
	if err != nil {
		return nil, err
	}
	var out CeremonyClaimShareResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CeremonySignCSR signs a CSR during the ceremony (mirrors POST
// /api/v1/init/sign-csr).
func (c *Client) CeremonySignCSR(ctx context.Context, req *CeremonySignCSRRequest) (*CeremonySignCSRResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.CeremonySignCSR", "request")
	}
	resp, err := c.callCeremony(ctx, SubOpCeremonySignCSR, req)
	if err != nil {
		return nil, err
	}
	var out CeremonySignCSRResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CeremonyListOfficers returns the persistent ceremony officer registry
// (mirrors GET /api/v1/init/officers).
func (c *Client) CeremonyListOfficers(ctx context.Context) (*CeremonyListOfficersResponse, error) {
	resp, err := c.callCeremony(ctx, SubOpCeremonyListOfficers, nil)
	if err != nil {
		return nil, err
	}
	var out CeremonyListOfficersResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CeremonyEnrollExternalOfficer enrols an officer whose key material lives
// outside the daemon (mirrors POST /api/v1/init/enroll-external-officer).
func (c *Client) CeremonyEnrollExternalOfficer(ctx context.Context, req *CeremonyEnrollExternalOfficerRequest) (*CeremonyEnrollExternalOfficerResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.CeremonyEnrollExternalOfficer", "request")
	}
	resp, err := c.callCeremony(ctx, SubOpCeremonyEnrollExternalOfficer, req)
	if err != nil {
		return nil, err
	}
	var out CeremonyEnrollExternalOfficerResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// =============================================================================
// Secrets Operations
// =============================================================================

// StoreSecret stores a new secret.
func (c *Client) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.StoreSecret", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpSecretsStore, payload)
	if err != nil {
		return nil, err
	}

	var result service.StoreSecretResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetSecret retrieves a secret by ID.
func (c *Client) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.GetSecret", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpSecretsGet, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetSecretResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListSecrets returns all accessible secrets.
func (c *Client) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.ListSecrets", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpSecretsList, payload)
	if err != nil {
		return nil, err
	}

	var result service.ListSecretsResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// DeleteSecret deletes a secret.
func (c *Client) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.DeleteSecret", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpSecretsDelete, payload)
	if err != nil {
		return nil, err
	}

	var result service.DeleteSecretResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListSecretVersions returns all versions of a secret.
func (c *Client) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.ListSecretVersions", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpSecretsListVersions, payload)
	if err != nil {
		return nil, err
	}

	var result service.ListSecretVersionsResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// RollbackSecret rolls back to a previous version.
func (c *Client) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.RollbackSecret", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpSecretsRollback, payload)
	if err != nil {
		return nil, err
	}

	var result service.RollbackSecretResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// RotateSecret rotates a secret to a new value.
func (c *Client) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.RotateSecret", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpSecretsRotate, payload)
	if err != nil {
		return nil, err
	}

	var result service.RotateSecretResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// UpdateSecretStatus updates the status of a secret (enable/disable).
func (c *Client) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.UpdateSecretStatus", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpSecretsUpdateStatus, payload)
	if err != nil {
		return nil, err
	}

	var result service.UpdateSecretStatusResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetSecretsByPrefix retrieves all secrets matching a path prefix.
func (c *Client) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.GetSecretsByPrefix", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, OpSecretsGetByPrefix, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetSecretsByPrefixResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// WatchSecretsByPrefix returns an error because QUIC uses a request/response model
// that cannot deliver a streaming channel. The server-side handler supports a
// single-shot batch response, but the channel-based return type requires true
// streaming which is only available via gRPC. Use the gRPC client for watch operations.
func (c *Client) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.WatchSecretsByPrefix", "request")
	}

	return nil, api.NewFeatureUnsupportedError("Client.WatchSecretsByPrefix", "streaming watch over QUIC transport")
}

// =============================================================================
// Cluster Operations
// =============================================================================

// ClusterStatus retrieves the current cluster status.
func (c *Client) ClusterStatus(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
	if req == nil {
		req = &service.ClusterStatusRequest{}
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.ClusterStatus", "request", err)
	}

	resp, err := c.sendRequest(ctx, OpClusterStatus, payload)
	if err != nil {
		return nil, err
	}

	var result service.ClusterStatusResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.ClusterStatus", "response", err)
	}

	return &result, nil
}

// GetLeader retrieves the leader node for a shard.
func (c *Client) GetLeader(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.GetLeader", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.GetLeader", "request", err)
	}

	resp, err := c.sendRequest(ctx, OpGetLeader, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetLeaderResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.GetLeader", "response", err)
	}

	return &result, nil
}

// AddNode adds a node to a shard.
func (c *Client) AddNode(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.AddNode", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.AddNode", "request", err)
	}

	resp, err := c.sendRequest(ctx, OpAddNode, payload)
	if err != nil {
		return nil, err
	}

	var result service.AddNodeResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.AddNode", "response", err)
	}

	return &result, nil
}

// RemoveNode removes a node from a shard.
func (c *Client) RemoveNode(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.RemoveNode", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.RemoveNode", "request", err)
	}

	resp, err := c.sendRequest(ctx, OpRemoveNode, payload)
	if err != nil {
		return nil, err
	}

	var result service.RemoveNodeResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.RemoveNode", "response", err)
	}

	return &result, nil
}

// CreateShard creates a new shard.
func (c *Client) CreateShard(ctx context.Context, req *service.CreateShardRequest) (*service.CreateShardResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.CreateShard", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.CreateShard", "request", err)
	}

	resp, err := c.sendRequest(ctx, OpCreateShard, payload)
	if err != nil {
		return nil, err
	}

	var result service.CreateShardResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.CreateShard", "response", err)
	}

	return &result, nil
}

// RebalanceCluster initiates a cluster rebalancing operation.
func (c *Client) RebalanceCluster(ctx context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
	if req == nil {
		req = &service.RebalanceClusterRequest{}
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.RebalanceCluster", "request", err)
	}

	resp, err := c.sendRequest(ctx, OpRebalanceCluster, payload)
	if err != nil {
		return nil, err
	}

	var result service.RebalanceClusterResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.RebalanceCluster", "response", err)
	}

	return &result, nil
}

// =============================================================================
// Scheduler Operations
// =============================================================================

// sendSchedulerRequest sends a scheduler request with sub-opcode dispatch.
// The scheduler uses OpScheduler (0x00) as the main opcode with the sub-operation
// code prepended to the payload.
func (c *Client) sendSchedulerRequest(ctx context.Context, subOpcode byte, reqPayload []byte) (*Response, error) {
	// Prepend sub-opcode to payload
	payload := make([]byte, 1+len(reqPayload))
	payload[0] = subOpcode
	copy(payload[1:], reqPayload)

	return c.sendRequest(ctx, OpScheduler, payload)
}

// SchedulerCreateJob creates a new scheduled job.
func (c *Client) SchedulerCreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.SchedulerCreateJob", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.SchedulerCreateJob", "request", err)
	}

	resp, err := c.sendSchedulerRequest(ctx, SubOpSchedulerCreateJob, payload)
	if err != nil {
		return nil, err
	}

	var result service.CreateSchedulerJobResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.SchedulerCreateJob", "response", err)
	}

	return &result, nil
}

// SchedulerGetJob retrieves a job by ID.
func (c *Client) SchedulerGetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.SchedulerGetJob", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.SchedulerGetJob", "request", err)
	}

	resp, err := c.sendSchedulerRequest(ctx, SubOpSchedulerGetJob, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetSchedulerJobResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.SchedulerGetJob", "response", err)
	}

	return &result, nil
}

// SchedulerUpdateJob updates an existing job.
func (c *Client) SchedulerUpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.SchedulerUpdateJob", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.SchedulerUpdateJob", "request", err)
	}

	resp, err := c.sendSchedulerRequest(ctx, SubOpSchedulerUpdateJob, payload)
	if err != nil {
		return nil, err
	}

	var result service.UpdateSchedulerJobResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.SchedulerUpdateJob", "response", err)
	}

	return &result, nil
}

// SchedulerDeleteJob removes a scheduled job.
func (c *Client) SchedulerDeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.SchedulerDeleteJob", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.SchedulerDeleteJob", "request", err)
	}

	resp, err := c.sendSchedulerRequest(ctx, SubOpSchedulerDeleteJob, payload)
	if err != nil {
		return nil, err
	}

	var result service.DeleteSchedulerJobResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.SchedulerDeleteJob", "response", err)
	}

	return &result, nil
}

// SchedulerListJobs returns jobs matching the query criteria.
func (c *Client) SchedulerListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if req == nil {
		req = &service.ListSchedulerJobsRequest{}
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.SchedulerListJobs", "request", err)
	}

	resp, err := c.sendSchedulerRequest(ctx, SubOpSchedulerListJobs, payload)
	if err != nil {
		return nil, err
	}

	var result service.ListSchedulerJobsResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.SchedulerListJobs", "response", err)
	}

	return &result, nil
}

// SchedulerTriggerJob immediately executes a job.
func (c *Client) SchedulerTriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.SchedulerTriggerJob", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.SchedulerTriggerJob", "request", err)
	}

	resp, err := c.sendSchedulerRequest(ctx, SubOpSchedulerTriggerJob, payload)
	if err != nil {
		return nil, err
	}

	var result service.TriggerSchedulerJobResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.SchedulerTriggerJob", "response", err)
	}

	return &result, nil
}

// SchedulerPauseJob pauses a scheduled job.
func (c *Client) SchedulerPauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.SchedulerPauseJob", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.SchedulerPauseJob", "request", err)
	}

	resp, err := c.sendSchedulerRequest(ctx, SubOpSchedulerPauseJob, payload)
	if err != nil {
		return nil, err
	}

	var result service.PauseSchedulerJobResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.SchedulerPauseJob", "response", err)
	}

	return &result, nil
}

// SchedulerResumeJob resumes a paused job.
func (c *Client) SchedulerResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.SchedulerResumeJob", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.SchedulerResumeJob", "request", err)
	}

	resp, err := c.sendSchedulerRequest(ctx, SubOpSchedulerResumeJob, payload)
	if err != nil {
		return nil, err
	}

	var result service.ResumeSchedulerJobResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.SchedulerResumeJob", "response", err)
	}

	return &result, nil
}

// SchedulerCancelJob cancels a scheduled job.
func (c *Client) SchedulerCancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.SchedulerCancelJob", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.SchedulerCancelJob", "request", err)
	}

	resp, err := c.sendSchedulerRequest(ctx, SubOpSchedulerCancelJob, payload)
	if err != nil {
		return nil, err
	}

	var result service.CancelSchedulerJobResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.SchedulerCancelJob", "response", err)
	}

	return &result, nil
}

// SchedulerGetJobHistory returns execution history for a job.
func (c *Client) SchedulerGetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.SchedulerGetJobHistory", "request")
	}

	payload, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.SchedulerGetJobHistory", "request", err)
	}

	resp, err := c.sendSchedulerRequest(ctx, SubOpSchedulerGetJobHistory, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetSchedulerJobHistoryResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.SchedulerGetJobHistory", "response", err)
	}

	return &result, nil
}

// SchedulerGetStatus returns scheduler status and statistics.
func (c *Client) SchedulerGetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	// GetStatus takes no request parameters, but we still need an empty payload
	resp, err := c.sendSchedulerRequest(ctx, SubOpSchedulerGetStatus, nil)
	if err != nil {
		return nil, err
	}

	var result service.GetSchedulerStatusResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.SchedulerGetStatus", "response", err)
	}

	return &result, nil
}

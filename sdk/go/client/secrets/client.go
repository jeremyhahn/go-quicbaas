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

package secrets

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Secrets"
)

// Client provides secrets management operations.
type Client interface {
	// Store stores a secret at the given path.
	Store(ctx context.Context, req *StoreSecretRequest) (*StoreSecretResponse, error)

	// Get retrieves a secret from the given path.
	Get(ctx context.Context, path string, opts ...GetOption) (*Secret, error)

	// List lists secrets under a path prefix.
	List(ctx context.Context, prefix string) (*ListSecretsResponse, error)

	// Delete deletes a secret at the given path.
	Delete(ctx context.Context, path string) error

	// Ensure returns the bytes stored at path, generating and
	// persisting a fresh value on the FIRST call if no secret
	// exists there. Subsequent calls return the same stable bytes.
	// Concurrent callers racing on the same path are serialized via
	// a distributed lock so only one call actually generates.
	//
	// Spec mismatch between this call and a previously stored value
	// returns an error — Ensure never silently rotates. To change a
	// spec, the operator must rotate the secret explicitly via
	// Store (or Delete + Ensure).
	//
	// This is the Vault-style generate-on-first-access primitive
	// that replaces the deprecated autogen:// resolver pattern.
	Ensure(ctx context.Context, req *EnsureSecretRequest) (*EnsureSecretResponse, error)
}

// StoreSecretRequest is a request to store a secret.
type StoreSecretRequest struct {
	Path     string
	Data     map[string]interface{}
	Metadata map[string]string
}

// StoreSecretResponse is the response from storing a secret.
type StoreSecretResponse struct {
	Version int
}

// EnsureSecretRequest is a request to generate-or-read a secret at
// a stable path. See Client.Ensure for the contract.
type EnsureSecretRequest struct {
	// Path is the stable identity of the secret.
	Path string

	// Length is the number of random bytes (for base64/hex/raw) or
	// digits (for numeric) to generate on first access. Required.
	Length int

	// Encoding is the output encoding. One of "base64", "hex",
	// "numeric", "raw". Required.
	Encoding string

	// TenantID optionally overrides the context tenant for this
	// call. Empty means "use context tenant".
	TenantID string
}

// EnsureSecretResponse carries either the existing stable bytes or
// bytes generated on this call's first-ever access.
type EnsureSecretResponse struct {
	// Path echoes the request path.
	Path string

	// Data is the secret value as the caller should consume it.
	// For base64/hex/numeric encodings this is printable ASCII;
	// for raw it is opaque bytes.
	Data []byte

	// Generated is true when this call produced a fresh value.
	// Subsequent reads return Generated=false.
	Generated bool

	// Version is the current version of the underlying secret.
	Version int
}

// GetOption configures secret retrieval.
type GetOption func(*getOptions)

type getOptions struct {
	version int
}

// WithVersion retrieves a specific version of the secret.
func WithVersion(version int) GetOption {
	return func(o *getOptions) {
		o.version = version
	}
}

// Secret represents a stored secret.
type Secret struct {
	Path      string
	Data      map[string]interface{}
	Metadata  map[string]string
	Version   int
	CreatedAt time.Time
}

// ListSecretsResponse is the response from listing secrets.
type ListSecretsResponse struct {
	Paths []string
}

// getSecretRequest is the internal get secret request.
type getSecretRequest struct {
	Path    string
	Version int
}

// listSecretsRequest is the internal list secrets request.
type listSecretsRequest struct {
	Prefix string
}

// deleteSecretRequest is the internal delete secret request.
type deleteSecretRequest struct {
	Path string
}

// deleteSecretResponse is the internal delete secret response.
type deleteSecretResponse struct{}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new secrets client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Store stores a secret at the given path.
func (c *client) Store(ctx context.Context, req *StoreSecretRequest) (*StoreSecretResponse, error) {
	if err := validateStoreSecretRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("secrets.Store", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Store", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("secrets.Store", "nil response from server")
	}

	storeResp, ok := resp.(*StoreSecretResponse)
	if !ok {
		return nil, errors.NewInternal("secrets.Store", "invalid response type")
	}

	return storeResp, nil
}

// Ensure is the Vault-style generate-on-first-access primitive.
// See Client.Ensure for the full contract.
func (c *client) Ensure(ctx context.Context, req *EnsureSecretRequest) (*EnsureSecretResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("secrets.Ensure", "request is required")
	}
	if req.Path == "" {
		return nil, errors.NewInvalidInput("secrets.Ensure", "path is required")
	}
	if req.Length <= 0 {
		return nil, errors.NewInvalidInput("secrets.Ensure", "length must be positive")
	}
	if req.Encoding == "" {
		return nil, errors.NewInvalidInput("secrets.Ensure", "encoding is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("secrets.Ensure", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Ensure", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("secrets.Ensure", "nil response from server")
	}
	ensureResp, ok := resp.(*EnsureSecretResponse)
	if !ok {
		return nil, errors.NewInternal("secrets.Ensure", "invalid response type")
	}
	return ensureResp, nil
}

// Get retrieves a secret from the given path.
func (c *client) Get(ctx context.Context, path string, opts ...GetOption) (*Secret, error) {
	if path == "" {
		return nil, errors.NewInvalidInput("secrets.Get", "path is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("secrets.Get", "context is required")
	}

	options := &getOptions{}
	for _, opt := range opts {
		opt(options)
	}

	resp, err := c.transport.Request(ctx, serviceName, "Get", &getSecretRequest{
		Path:    path,
		Version: options.version,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("secrets.Get", "secret not found")
	}

	secret, ok := resp.(*Secret)
	if !ok {
		return nil, errors.NewInternal("secrets.Get", "invalid response type")
	}

	return secret, nil
}

// List lists secrets under a path prefix.
func (c *client) List(ctx context.Context, prefix string) (*ListSecretsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("secrets.List", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "List", &listSecretsRequest{Prefix: prefix})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListSecretsResponse{Paths: []string{}}, nil
	}

	listResp, ok := resp.(*ListSecretsResponse)
	if !ok {
		return nil, errors.NewInternal("secrets.List", "invalid response type")
	}

	return listResp, nil
}

// Delete deletes a secret at the given path.
func (c *client) Delete(ctx context.Context, path string) error {
	if path == "" {
		return errors.NewInvalidInput("secrets.Delete", "path is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("secrets.Delete", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Delete", &deleteSecretRequest{Path: path})
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*deleteSecretResponse); !ok {
		return errors.NewInternal("secrets.Delete", "invalid response type")
	}

	return nil
}

// Validation functions

func validateStoreSecretRequest(req *StoreSecretRequest) error {
	if req == nil {
		return errors.NewInvalidInput("secrets.Store", "request is required")
	}
	if req.Path == "" {
		return errors.NewInvalidInput("secrets.Store", "path is required")
	}
	if len(req.Data) == 0 {
		return errors.NewInvalidInput("secrets.Store", "data is required")
	}
	return nil
}

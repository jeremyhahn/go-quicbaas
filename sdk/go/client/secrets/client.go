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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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

	// GetVersion retrieves a specific version of a secret.
	GetVersion(ctx context.Context, req *GetSecretVersionRequest) (*SecretVersionInfo, error)

	// ListVersions returns all versions of a secret.
	ListVersions(ctx context.Context, secretID string) (*ListSecretVersionsResponse, error)

	// Rotate manually rotates a secret to a new value.
	Rotate(ctx context.Context, req *RotateSecretRequest) (*RotateSecretResponse, error)

	// Rollback rolls back to a previous version of a secret.
	Rollback(ctx context.Context, req *RollbackSecretRequest) (*RollbackSecretResponse, error)

	// UpdateStatus enables or disables a secret.
	UpdateStatus(ctx context.Context, req *UpdateSecretStatusRequest) error

	// SetRotationPolicy sets or updates the automatic rotation policy.
	SetRotationPolicy(ctx context.Context, req *SetRotationPolicyRequest) error

	// GetRotationPolicy retrieves the current rotation policy for a secret.
	GetRotationPolicy(ctx context.Context, secretID string) (*RotationPolicy, error)
}

// =============================================================================
// Request/Response Types
// =============================================================================

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

	// TenantID names the tenant that owns the secret. It must be the
	// caller's own tenant — the server refuses a request that names
	// any other, including the system tenant naming a tenant — and it
	// selects both the storage namespace and the tenant-keyed DEK.
	// Empty means "my own tenant" and is the usual value.
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

// GetSecretVersionRequest is a request to get a specific secret version.
type GetSecretVersionRequest struct {
	// SecretID is the secret identifier.
	SecretID string

	// Version is the version number to retrieve.
	Version int
}

// SecretVersionInfo contains metadata for a specific secret version.
type SecretVersionInfo struct {
	SecretID  string
	Version   int
	Data      []byte
	Metadata  map[string]string
	CreatedAt time.Time
	CreatedBy string
}

// ListSecretVersionsResponse is the response from listing secret versions.
type ListSecretVersionsResponse struct {
	Versions []SecretVersionInfo
}

// RotateSecretRequest is a request to rotate a secret.
type RotateSecretRequest struct {
	// SecretID is the secret to rotate.
	SecretID string

	// NewData is the new secret data.
	NewData []byte

	// Metadata contains version-specific metadata.
	Metadata map[string]string
}

// RotateSecretResponse is the response from rotating a secret.
type RotateSecretResponse struct {
	SecretID   string
	OldVersion int
	NewVersion int
}

// RollbackSecretRequest is a request to rollback a secret.
type RollbackSecretRequest struct {
	// SecretID is the secret to rollback.
	SecretID string

	// TargetVersion is the version to roll back to.
	TargetVersion int
}

// RollbackSecretResponse is the response from rolling back a secret.
type RollbackSecretResponse struct {
	SecretID   string
	NewVersion int
}

// SecretStatus represents the lifecycle state of a secret.
type SecretStatus string

const (
	// SecretStatusEnabled indicates the secret is active.
	SecretStatusEnabled SecretStatus = "enabled"
	// SecretStatusDisabled indicates the secret is disabled.
	SecretStatusDisabled SecretStatus = "disabled"
	// SecretStatusDeleted indicates the secret is deleted.
	SecretStatusDeleted SecretStatus = "deleted"
)

// UpdateSecretStatusRequest is a request to update a secret's status.
type UpdateSecretStatusRequest struct {
	// SecretID is the secret to update.
	SecretID string

	// Status is the new status.
	Status SecretStatus
}

// RotationPolicy defines automatic secret rotation behavior.
type RotationPolicy struct {
	Enabled        bool
	Interval       time.Duration
	NextRotation   time.Time
	RetainVersions int
	RotationLambda string
}

// SetRotationPolicyRequest is a request to set rotation policy.
type SetRotationPolicyRequest struct {
	// SecretID is the secret to configure.
	SecretID string

	// Policy is the rotation policy.
	Policy *RotationPolicy
}

// =============================================================================
// Internal request types
// =============================================================================

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

// listSecretVersionsRequest is the internal list secret versions request.
type listSecretVersionsRequest struct {
	SecretID string
}

// updateSecretStatusResponse is the internal update status response.
type updateSecretStatusResponse struct{}

// setRotationPolicyResponse is the internal set rotation policy response.
type setRotationPolicyResponse struct{}

// getRotationPolicyRequest is the internal get rotation policy request.
type getRotationPolicyRequest struct {
	SecretID string
}

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

// GetVersion retrieves a specific version of a secret.
func (c *client) GetVersion(ctx context.Context, req *GetSecretVersionRequest) (*SecretVersionInfo, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("secrets.GetVersion", "request is required")
	}
	if req.SecretID == "" {
		return nil, errors.NewInvalidInput("secrets.GetVersion", "secret ID is required")
	}
	if req.Version <= 0 {
		return nil, errors.NewInvalidInput("secrets.GetVersion", "version must be positive")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("secrets.GetVersion", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetVersion", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("secrets.GetVersion", "secret version not found")
	}

	switch r := resp.(type) {
	case *SecretVersionInfo:
		return r, nil
	case *service.GetSecretVersionResponse:
		if r.Version == nil {
			return nil, errors.NewNotFound("secrets.GetVersion", "secret version not found")
		}
		return &SecretVersionInfo{
			SecretID:  r.Version.SecretID,
			Version:   int(r.Version.Version),
			Data:      r.Version.Data,
			Metadata:  r.Version.Metadata,
			CreatedAt: r.Version.CreatedAt,
			CreatedBy: r.Version.CreatedBy,
		}, nil
	default:
		return nil, errors.NewInternal("secrets.GetVersion", "invalid response type")
	}
}

// ListVersions returns all versions of a secret.
func (c *client) ListVersions(ctx context.Context, secretID string) (*ListSecretVersionsResponse, error) {
	if secretID == "" {
		return nil, errors.NewInvalidInput("secrets.ListVersions", "secret ID is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("secrets.ListVersions", "context is required")
	}

	req := &listSecretVersionsRequest{
		SecretID: secretID,
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListVersions", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListSecretVersionsResponse{Versions: []SecretVersionInfo{}}, nil
	}

	switch r := resp.(type) {
	case *ListSecretVersionsResponse:
		return r, nil
	case *service.ListSecretVersionsResponse:
		versions := make([]SecretVersionInfo, len(r.Versions))
		for i, v := range r.Versions {
			versions[i] = SecretVersionInfo{
				SecretID:  v.SecretID,
				Version:   int(v.Version),
				Data:      v.Data,
				Metadata:  v.Metadata,
				CreatedAt: v.CreatedAt,
				CreatedBy: v.CreatedBy,
			}
		}
		return &ListSecretVersionsResponse{Versions: versions}, nil
	default:
		return nil, errors.NewInternal("secrets.ListVersions", "invalid response type")
	}
}

// Rotate manually rotates a secret to a new value.
func (c *client) Rotate(ctx context.Context, req *RotateSecretRequest) (*RotateSecretResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("secrets.Rotate", "request is required")
	}
	if req.SecretID == "" {
		return nil, errors.NewInvalidInput("secrets.Rotate", "secret ID is required")
	}
	if len(req.NewData) == 0 {
		return nil, errors.NewInvalidInput("secrets.Rotate", "new data is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("secrets.Rotate", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Rotate", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("secrets.Rotate", "nil response from server")
	}

	switch r := resp.(type) {
	case *RotateSecretResponse:
		return r, nil
	case *service.RotateSecretResponse:
		return &RotateSecretResponse{
			SecretID:   r.SecretID,
			OldVersion: int(r.OldVersion),
			NewVersion: int(r.NewVersion),
		}, nil
	default:
		return nil, errors.NewInternal("secrets.Rotate", "invalid response type")
	}
}

// Rollback rolls back to a previous version of a secret.
func (c *client) Rollback(ctx context.Context, req *RollbackSecretRequest) (*RollbackSecretResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("secrets.Rollback", "request is required")
	}
	if req.SecretID == "" {
		return nil, errors.NewInvalidInput("secrets.Rollback", "secret ID is required")
	}
	if req.TargetVersion <= 0 {
		return nil, errors.NewInvalidInput("secrets.Rollback", "target version must be positive")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("secrets.Rollback", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Rollback", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("secrets.Rollback", "nil response from server")
	}

	switch r := resp.(type) {
	case *RollbackSecretResponse:
		return r, nil
	case *service.RollbackSecretResponse:
		return &RollbackSecretResponse{
			SecretID:   r.SecretID,
			NewVersion: int(r.NewVersion),
		}, nil
	default:
		return nil, errors.NewInternal("secrets.Rollback", "invalid response type")
	}
}

// UpdateStatus enables or disables a secret.
func (c *client) UpdateStatus(ctx context.Context, req *UpdateSecretStatusRequest) error {
	if req == nil {
		return errors.NewInvalidInput("secrets.UpdateStatus", "request is required")
	}
	if req.SecretID == "" {
		return errors.NewInvalidInput("secrets.UpdateStatus", "secret ID is required")
	}
	if ctx == nil {
		return errors.NewInvalidInput("secrets.UpdateStatus", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "UpdateStatus", req)
	if err != nil {
		return err
	}

	if resp == nil {
		return nil
	}

	switch resp.(type) {
	case *updateSecretStatusResponse:
		return nil
	case *service.UpdateSecretStatusResponse:
		return nil
	default:
		return errors.NewInternal("secrets.UpdateStatus", "invalid response type")
	}
}

// SetRotationPolicy sets or updates the automatic rotation policy.
func (c *client) SetRotationPolicy(ctx context.Context, req *SetRotationPolicyRequest) error {
	if req == nil {
		return errors.NewInvalidInput("secrets.SetRotationPolicy", "request is required")
	}
	if req.SecretID == "" {
		return errors.NewInvalidInput("secrets.SetRotationPolicy", "secret ID is required")
	}
	if req.Policy == nil {
		return errors.NewInvalidInput("secrets.SetRotationPolicy", "policy is required")
	}
	if ctx == nil {
		return errors.NewInvalidInput("secrets.SetRotationPolicy", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SetRotationPolicy", req)
	if err != nil {
		return err
	}

	if resp == nil {
		return nil
	}

	switch resp.(type) {
	case *setRotationPolicyResponse:
		return nil
	case *service.SetRotationPolicyResponse:
		return nil
	default:
		return errors.NewInternal("secrets.SetRotationPolicy", "invalid response type")
	}
}

// GetRotationPolicy retrieves the current rotation policy for a secret.
func (c *client) GetRotationPolicy(ctx context.Context, secretID string) (*RotationPolicy, error) {
	if secretID == "" {
		return nil, errors.NewInvalidInput("secrets.GetRotationPolicy", "secret ID is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("secrets.GetRotationPolicy", "context is required")
	}

	req := &getRotationPolicyRequest{
		SecretID: secretID,
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetRotationPolicy", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("secrets.GetRotationPolicy", "rotation policy not found")
	}

	switch r := resp.(type) {
	case *RotationPolicy:
		return r, nil
	case *service.GetRotationPolicyResponse:
		if r.Policy == nil {
			return nil, errors.NewNotFound("secrets.GetRotationPolicy", "rotation policy not found")
		}
		return &RotationPolicy{
			Enabled:        r.Policy.Enabled,
			Interval:       r.Policy.Interval,
			NextRotation:   r.Policy.NextRotation,
			RetainVersions: r.Policy.RetainVersions,
			RotationLambda: r.Policy.RotationLambda,
		}, nil
	default:
		return nil, errors.NewInternal("secrets.GetRotationPolicy", "invalid response type")
	}
}

// =============================================================================
// Validation functions
// =============================================================================

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

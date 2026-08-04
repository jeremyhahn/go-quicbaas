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

package server

import (
	"context"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Lazy qrdb User / UserAuthFactor servicer proxies
//
// The embedded qrdb node exposes its UserServicer and UserAuthFactorServicer
// only after Unseal completes the boot (Node.UserService /
// Node.UserAuthFactorService return nil while sealed). The gRPC registration
// (registerQRDBService) and the REST wire mounts run at server start — BEFORE
// an operator can unseal via the API on the seal-enabled boot path — so
// capturing the servicer instance eagerly would leave the user surface
// permanently unavailable on that path.
//
// These proxies resolve the node servicer PER CALL through the same
// GetQRDBUserServicer / GetQRDBUserAuthFactorServicer accessors the SDK
// embedded transport is wired from, so every transport (gRPC, Unix-over-gRPC,
// REST, embedded) operates on the SAME user store the unified-login ceremony
// reads. While the node is absent or sealed, calls fail closed with
// QRDBUserServiceUnavailableError.
// =============================================================================

// QRDBUserServiceUnavailableError indicates the embedded qrdb node's user (or
// user auth-factor) service is not available — the node is absent or still
// sealed. Callers should retry after Unseal completes the boot.
type QRDBUserServiceUnavailableError struct {
	// Surface identifies which service was unavailable ("user" or
	// "user_authfactor").
	Surface string
}

// Error implements the error interface.
func (e *QRDBUserServiceUnavailableError) Error() string {
	return "qrdb " + e.Surface + " service unavailable (embedded node absent or sealed)"
}

// qrdbUserServicerProxy is a lazy qrdbsdk.UserServicer that resolves the
// embedded node's user service on every call.
type qrdbUserServicerProxy struct {
	resolve func() qrdbsdk.UserServicer
}

// newQRDBUserServicerProxy builds the lazy user servicer proxy bound to the
// server's embedded-node accessor.
func newQRDBUserServicerProxy(s *Server) *qrdbUserServicerProxy {
	return &qrdbUserServicerProxy{resolve: s.GetQRDBUserServicer}
}

// servicer resolves the live node user service or fails closed.
func (p *qrdbUserServicerProxy) servicer() (qrdbsdk.UserServicer, error) {
	if p.resolve != nil {
		if svc := p.resolve(); svc != nil {
			return svc, nil
		}
	}
	return nil, &QRDBUserServiceUnavailableError{Surface: "user"}
}

// CreateUser delegates to the node user service.
func (p *qrdbUserServicerProxy) CreateUser(ctx context.Context, req *qrdbsdk.CreateUserRequest) (*qrdbsdk.User, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.CreateUser(ctx, req)
}

// GetUser delegates to the node user service.
func (p *qrdbUserServicerProxy) GetUser(ctx context.Context, id uint64) (*qrdbsdk.User, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.GetUser(ctx, id)
}

// GetUserByUsername delegates to the node user service.
func (p *qrdbUserServicerProxy) GetUserByUsername(ctx context.Context, tenantID, username string) (*qrdbsdk.User, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.GetUserByUsername(ctx, tenantID, username)
}

// UserByCertThumbprint delegates to the node user service.
func (p *qrdbUserServicerProxy) UserByCertThumbprint(ctx context.Context, thumbprint string) (*qrdbsdk.User, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.UserByCertThumbprint(ctx, thumbprint)
}

// AddCertBinding delegates to the node user service.
func (p *qrdbUserServicerProxy) AddCertBinding(ctx context.Context, userID uint64, thumbprint string, notAfterUnix int64) (*qrdbsdk.User, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.AddCertBinding(ctx, userID, thumbprint, notAfterUnix)
}

// UpdateUser delegates to the node user service.
func (p *qrdbUserServicerProxy) UpdateUser(ctx context.Context, req *qrdbsdk.UpdateUserRequest) (*qrdbsdk.User, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.UpdateUser(ctx, req)
}

// DeleteUser delegates to the node user service.
func (p *qrdbUserServicerProxy) DeleteUser(ctx context.Context, id uint64) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.DeleteUser(ctx, id)
}

// ListUsers delegates to the node user service.
func (p *qrdbUserServicerProxy) ListUsers(ctx context.Context, req *qrdbsdk.ListUsersRequest) (*qrdbsdk.ListUsersResponse, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.ListUsers(ctx, req)
}

// EnableUser delegates to the node user service.
func (p *qrdbUserServicerProxy) EnableUser(ctx context.Context, id uint64) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.EnableUser(ctx, id)
}

// DisableUser delegates to the node user service.
func (p *qrdbUserServicerProxy) DisableUser(ctx context.Context, id uint64) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.DisableUser(ctx, id)
}

// Compile-time interface check.
var _ qrdbsdk.UserServicer = (*qrdbUserServicerProxy)(nil)

// qrdbUserAuthFactorServicerProxy is a lazy qrdbsdk.UserAuthFactorServicer
// that resolves the embedded node's UserAuthFactor service INSTANCE — the
// exact store the login ceremony resolves against — on every call.
type qrdbUserAuthFactorServicerProxy struct {
	resolve func() qrdbsdk.UserAuthFactorServicer
}

// newQRDBUserAuthFactorServicerProxy builds the lazy user auth-factor
// servicer proxy bound to the server's embedded-node accessor.
func newQRDBUserAuthFactorServicerProxy(s *Server) *qrdbUserAuthFactorServicerProxy {
	return &qrdbUserAuthFactorServicerProxy{resolve: s.GetQRDBUserAuthFactorServicer}
}

// servicer resolves the live node auth-factor service or fails closed.
func (p *qrdbUserAuthFactorServicerProxy) servicer() (qrdbsdk.UserAuthFactorServicer, error) {
	if p.resolve != nil {
		if svc := p.resolve(); svc != nil {
			return svc, nil
		}
	}
	return nil, &QRDBUserServiceUnavailableError{Surface: "user_authfactor"}
}

// Add delegates to the node auth-factor service.
func (p *qrdbUserAuthFactorServicerProxy) Add(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.Add(ctx, userID, factor)
}

// List delegates to the node auth-factor service.
func (p *qrdbUserAuthFactorServicerProxy) List(ctx context.Context, userID uint64) ([]*qrdbsdk.AuthFactor, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.List(ctx, userID)
}

// Get delegates to the node auth-factor service.
func (p *qrdbUserAuthFactorServicerProxy) Get(ctx context.Context, factorID string) (*qrdbsdk.AuthFactor, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.Get(ctx, factorID)
}

// LookupByCredentialKey delegates to the node auth-factor service.
func (p *qrdbUserAuthFactorServicerProxy) LookupByCredentialKey(ctx context.Context, t qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.LookupByCredentialKey(ctx, t, key)
}

// Remove delegates to the node auth-factor service.
func (p *qrdbUserAuthFactorServicerProxy) Remove(ctx context.Context, factorID string, force bool) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.Remove(ctx, factorID, force)
}

// Rotate delegates to the node auth-factor service.
func (p *qrdbUserAuthFactorServicerProxy) Rotate(ctx context.Context, oldID string, newFactor *qrdbsdk.AuthFactor) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.Rotate(ctx, oldID, newFactor)
}

// Relabel delegates to the node auth-factor service.
func (p *qrdbUserAuthFactorServicerProxy) Relabel(ctx context.Context, factorID string, label string) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.Relabel(ctx, factorID, label)
}

// MarkUsed delegates to the node auth-factor service.
func (p *qrdbUserAuthFactorServicerProxy) MarkUsed(ctx context.Context, factorID string, ts time.Time) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.MarkUsed(ctx, factorID, ts)
}

// Compile-time interface check.
var _ qrdbsdk.UserAuthFactorServicer = (*qrdbUserAuthFactorServicerProxy)(nil)

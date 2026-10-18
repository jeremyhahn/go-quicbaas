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

//go:build integration

package helpers

import (
	"context"
	"strings"
	"sync"
	"time"
)

// tenantAdminLogin logs a tenant admin in with its certificate and returns
// the bearer and the time to stop using it.
type tenantAdminLogin func(ctx context.Context, baseURL string, certPEM, keyPEM []byte) (string, time.Time, error)

// TenantAdminSession is a provisioned tenant's admin credential and the
// bearer it last obtained with it. Access tokens live about 15 minutes, so
// the session logs the admin in again with its certificate once the bearer
// nears expiry: a test or soak iteration that runs longer keeps its access,
// the way a production caller holding the certificate does.
type TenantAdminSession struct {
	baseURL string
	tenant  *ProvisionedTenant
	login   tenantAdminLogin
	now     func() time.Time

	mu    sync.Mutex
	token pivCachedToken
}

// NewTenantAdminSession returns a session for tenant's admin against
// baseURL. It logs in on the first Bearer call.
func NewTenantAdminSession(baseURL string, tenant *ProvisionedTenant) *TenantAdminSession {
	return newTenantAdminSession(baseURL, tenant, LoginWithCertSession, time.Now)
}

func newTenantAdminSession(baseURL string, tenant *ProvisionedTenant, login tenantAdminLogin, now func() time.Time) *TenantAdminSession {
	return &TenantAdminSession{
		baseURL: strings.TrimRight(baseURL, "/"),
		tenant:  tenant,
		login:   login,
		now:     now,
	}
}

// Tenant is the provisioned tenant and its admin credential.
func (s *TenantAdminSession) Tenant() *ProvisionedTenant { return s.tenant }

// Bearer returns a bearer for the tenant admin that is not near expiry,
// logging in again with the admin certificate when the last one is.
func (s *TenantAdminSession) Bearer(ctx context.Context) (string, error) {
	bearer, _, err := s.BearerSession(ctx)
	return bearer, err
}

// BearerSession is Bearer that also reports when the bearer must no longer
// be used, for callers that hand the bearer to a token store.
func (s *TenantAdminSession) BearerSession(ctx context.Context) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token.Bearer != "" && s.now().Before(s.token.ExpiresAt) {
		return s.token.Bearer, s.token.ExpiresAt, nil
	}

	bearer, expiresAt, err := s.login(ctx, s.baseURL, s.tenant.AdminCertPEM, s.tenant.AdminKeyPEM)
	if err != nil {
		s.token = pivCachedToken{}
		return "", time.Time{}, &TenantProvisionError{Step: "admin PIV login", TenantID: s.tenant.TenantID, Cause: err}
	}
	s.token = pivCachedToken{Bearer: bearer, ExpiresAt: expiresAt}
	return bearer, expiresAt, nil
}

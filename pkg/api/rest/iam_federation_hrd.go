//go:build rest && iam

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
	"context"
	"net/http"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// AmbiguousProviderError indicates a tenant has multiple enabled federation
// providers and the caller did not name one for an auto-selected login.
type AmbiguousProviderError struct {
	TenantID string
	Count    int
}

func (e *AmbiguousProviderError) Error() string {
	return "tenant has multiple federation providers; specify a provider name"
}

// NoProviderError indicates a tenant has no enabled federation provider to
// auto-select for a federated login.
type NoProviderError struct {
	TenantID string
}

func (e *NoProviderError) Error() string {
	return "tenant has no enabled federation provider"
}

// isAutoSelector reports whether a path segment is a placeholder requesting
// automatic resolution (home-realm discovery for tenant, single-provider
// selection for provider).
func isAutoSelector(v string) bool {
	switch strings.TrimSpace(v) {
	case "", "_", "auto":
		return true
	default:
		return false
	}
}

// resolveFederationTenant resolves the login's tenant from non-secret routing
// hints (X-Tenant header, TLS SNI/Host, login_hint email domain) via the
// attached home-realm directory. It fails closed when no resolver is configured
// or no routing entry matches.
func (h *IAMHandler) resolveFederationTenant(ctx context.Context, r *http.Request) (string, error) {
	if h.server.config == nil || h.server.config.HomeRealmResolver == nil {
		return "", ErrLoginTenantUnresolved
	}
	hints := RealmHints{
		TenantHint: strings.TrimSpace(r.Header.Get("X-Tenant")),
		SNIHost:    requestSNIHost(r),
		Email:      strings.TrimSpace(r.URL.Query().Get("login_hint")),
	}
	tenantID, err := h.server.config.HomeRealmResolver.Resolve(ctx, hints)
	if err != nil || tenantID == "" {
		return "", ErrLoginTenantUnresolved
	}
	return tenantID, nil
}

// selectTenantProvider auto-selects a tenant's federation provider when the
// caller did not name one. It returns the single enabled provider's name, or a
// typed error when none or more than one exist (the caller must then name one).
func (h *IAMHandler) selectTenantProvider(ctx context.Context, mgr iam.FederationManager, tenantID string) (string, error) {
	providers, err := mgr.ListProviders(ctx, tenantID, iam.ListOptions{})
	if err != nil {
		return "", err
	}

	var enabled []*iam.ExternalProvider
	for _, p := range providers {
		if p.Enabled {
			enabled = append(enabled, p)
		}
	}

	switch len(enabled) {
	case 0:
		return "", &NoProviderError{TenantID: tenantID}
	case 1:
		return enabled[0].Name, nil
	default:
		return "", &AmbiguousProviderError{TenantID: tenantID, Count: len(enabled)}
	}
}

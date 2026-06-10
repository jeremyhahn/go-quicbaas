//go:build iam

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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// fakeTenantAuthzModeResolver is a controllable tenantAuthzModeResolver for the
// per-tenant mode-resolution tests.
type fakeTenantAuthzModeResolver struct {
	mode string
	err  error
}

func (f *fakeTenantAuthzModeResolver) TenantAuthzMode(_ context.Context, _ string) (string, error) {
	return f.mode, f.err
}

func TestNewTenantModeResolver_PerTenantOverride(t *testing.T) {
	resolver := newTenantModeResolver(
		&fakeTenantAuthzModeResolver{mode: "online"},
		platformtoken.AuthzModeHybrid, // deployment default differs from override
	)
	assert.Equal(t, platformtoken.AuthzModeOnline, resolver("tenant-x"),
		"per-tenant override must win over the deployment default")
}

func TestNewTenantModeResolver_FallsBackWhenUnset(t *testing.T) {
	resolver := newTenantModeResolver(
		&fakeTenantAuthzModeResolver{mode: ""}, // no override
		platformtoken.AuthzModeOffline,
	)
	assert.Equal(t, platformtoken.AuthzModeOffline, resolver("tenant-x"),
		"empty override must fall back to the deployment default")
}

func TestNewTenantModeResolver_FailsSafeOnLookupError(t *testing.T) {
	resolver := newTenantModeResolver(
		&fakeTenantAuthzModeResolver{err: errors.New("tenant store down")},
		platformtoken.AuthzModeOnline,
	)
	assert.Equal(t, platformtoken.AuthzModeOnline, resolver("tenant-x"),
		"a lookup error must fail SAFE to the deployment default, never panic")
}

func TestNewTenantModeResolver_UnknownOverrideFallsBack(t *testing.T) {
	resolver := newTenantModeResolver(
		&fakeTenantAuthzModeResolver{mode: "garbage-mode"},
		platformtoken.AuthzModeHybrid,
	)
	assert.Equal(t, platformtoken.AuthzModeHybrid, resolver("tenant-x"),
		"an unparseable per-tenant override must fall back to the deployment default")
}

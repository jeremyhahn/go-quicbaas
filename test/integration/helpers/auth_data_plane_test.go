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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// errDeleteRefused is the failure fakeTenantDeletes returns when told to.
var errDeleteRefused = errors.New("delete refused")

// fakeTenantDeletes stands in for the server's tenant delete and records
// each tenant it was asked to delete.
type fakeTenantDeletes struct {
	deleted []string
	fail    bool
}

func (f *fakeTenantDeletes) delete(_ context.Context, _, tenantID string) error {
	if f.fail {
		return errDeleteRefused
	}
	f.deleted = append(f.deleted, tenantID)
	return nil
}

func TestDataPlaneRelease_DeletesProvisionedTenantOnce(t *testing.T) {
	registry := newDataPlaneRegistry()
	registry.principals["https://node:8443"] = &dataPlanePrincipal{tenantID: "dataplane-1"}
	fake := &fakeTenantDeletes{}

	require.NoError(t, registry.release(context.Background(), fake.delete))
	require.NoError(t, registry.release(context.Background(), fake.delete))

	require.Equal(t, []string{"dataplane-1"}, fake.deleted, "a released tenant is deleted exactly once")
	require.Empty(t, registry.principals)
}

func TestDataPlaneRelease_NothingProvisionedIsNoOp(t *testing.T) {
	registry := newDataPlaneRegistry()
	fake := &fakeTenantDeletes{}

	require.NoError(t, registry.release(context.Background(), fake.delete))
	require.Empty(t, fake.deleted)
}

func TestDataPlaneRelease_DeleteFailureIsTyped(t *testing.T) {
	registry := newDataPlaneRegistry()
	registry.principals["https://node:8443"] = &dataPlanePrincipal{tenantID: "dataplane-1"}
	fake := &fakeTenantDeletes{fail: true}

	err := registry.release(context.Background(), fake.delete)

	var authErr *DataPlaneAuthError
	require.ErrorAs(t, err, &authErr)
	require.Equal(t, "dataplane-1", authErr.TenantID)
	require.ErrorIs(t, err, errDeleteRefused)
	require.Len(t, registry.principals, 1, "a tenant whose delete failed is kept")
}

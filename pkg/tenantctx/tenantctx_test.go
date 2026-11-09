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

package tenantctx

import (
	"context"
	stderrors "errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// requireForbidden asserts err is the ErrForbidden QBError every transport
// maps to 403 / PermissionDenied, wrapping a cause of type T.
func requireForbidden[T error](t *testing.T, err error) T {
	t.Helper()
	require.Error(t, err)
	var qb *errors.QBError
	require.True(t, stderrors.As(err, &qb), "want a QBError, got %T", err)
	assert.Equal(t, errors.ErrForbidden, qb.Code)
	var cause T
	require.True(t, stderrors.As(err, &cause), "want a %T cause, got %v", cause, err)
	return cause
}

// TestFromContext_ReadsGoQRDBCentralKey pins that the tenant is read from
// go-qrdb's key, the one its AuthMiddleware binds on every transport — so a
// binding made by go-qrdb alone is what go-quicbaas reads.
func TestFromContext_ReadsGoQRDBCentralKey(t *testing.T) {
	tenantID, err := FromContext(qrdbsdk.ContextWithTenant(context.Background(), "acme"), "op")
	require.NoError(t, err)
	assert.Equal(t, "acme", tenantID)

	got, ok := qrdbsdk.TenantFromContext(WithTenant(context.Background(), "acme"))
	require.True(t, ok)
	assert.Equal(t, "acme", got, "WithTenant must bind go-qrdb's key")
}

// TestFromContext_FallsBackToTheAuthenticatedIdentity proves a context that
// carries go-qrdb's identity but no central binding is confined to the
// identity's tenant, and that the central binding wins when both are present.
func TestFromContext_FallsBackToTheAuthenticatedIdentity(t *testing.T) {
	ctx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "s", TenantID: "acme"})
	tenantID, err := FromContext(ctx, "op")
	require.NoError(t, err)
	assert.Equal(t, "acme", tenantID)

	tenantID, err = FromContext(WithTenant(ctx, "globex"), "op")
	require.NoError(t, err)
	assert.Equal(t, "globex", tenantID)
}

// TestFromContext_RefusesAnAbsentTenant is the fail-closed contract: neither
// an unbound context nor an empty binding is the system tenant.
func TestFromContext_RefusesAnAbsentTenant(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"unbound":             context.Background(),
		"empty binding":       WithTenant(context.Background(), ""),
		"tenantless identity": qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "s"}),
	} {
		_, err := FromContext(ctx, "op")
		missing := requireForbidden[*MissingTenantError](t, err)
		assert.Equal(t, "op", missing.Operation, name)
	}
}

// TestWithSystem_BindsTheSystemTenantExplicitly proves the system tenant is a
// binding: WithSystem produces it and IsSystem recognises only it.
func TestWithSystem_BindsTheSystemTenantExplicitly(t *testing.T) {
	tenantID, err := FromContext(WithSystem(context.Background()), "op")
	require.NoError(t, err)
	assert.Equal(t, SystemTenantID, tenantID)
	assert.True(t, IsSystem(tenantID))
	assert.False(t, IsSystem(""), "the empty string is not a system binding")
	assert.False(t, IsSystem("acme"))
}

// TestLookup_ReportsWhetherATenantIsBound covers the login-plane read.
func TestLookup_ReportsWhetherATenantIsBound(t *testing.T) {
	_, ok := Lookup(context.Background())
	assert.False(t, ok)
	tenantID, ok := Lookup(WithTenant(context.Background(), "acme"))
	assert.True(t, ok)
	assert.Equal(t, "acme", tenantID)
}

// TestTarget_TenantCallerNamesOnlyItself proves a tenant caller resolves to
// its own tenant and is refused for any other, the system tenant included.
func TestTarget_TenantCallerNamesOnlyItself(t *testing.T) {
	ctx := WithTenant(context.Background(), "acme")
	for _, requested := range []string{"", "acme"} {
		tenantID, err := Target(ctx, "op", requested)
		require.NoError(t, err)
		assert.Equal(t, "acme", tenantID)
	}
	for _, requested := range []string{"globex", SystemTenantID} {
		_, err := Target(ctx, "op", requested)
		cross := requireForbidden[*CrossTenantError](t, err)
		assert.Equal(t, "acme", cross.CallerTenant)
		assert.Equal(t, requested, cross.TargetTenant)
	}
}

// TestTarget_SystemCallerNamesOnlyItself proves the system tenant gets no
// cross-tenant lift through a request field: it acts for itself, and naming
// another tenant is refused like any other caller's.
func TestTarget_SystemCallerNamesOnlyItself(t *testing.T) {
	ctx := WithSystem(context.Background())
	for _, requested := range []string{"", SystemTenantID} {
		tenantID, err := Target(ctx, "op", requested)
		require.NoError(t, err)
		assert.Equal(t, SystemTenantID, tenantID)
	}
	_, err := Target(ctx, "op", "globex")
	cross := requireForbidden[*CrossTenantError](t, err)
	assert.Equal(t, SystemTenantID, cross.CallerTenant)
}

// TestTarget_RefusesAnUnboundCallerWhateverItNames proves a request naming a
// tenant is not trusted to name it when no caller tenant is bound.
func TestTarget_RefusesAnUnboundCallerWhateverItNames(t *testing.T) {
	for _, requested := range []string{"", "acme", SystemTenantID} {
		_, err := Target(context.Background(), "op", requested)
		requireForbidden[*MissingTenantError](t, err)
	}
}

// TestContextForTarget_BindsTheResolvedTenant proves the resolved tenant is
// what the downstream services read, and that a refusal returns the
// original context unchanged.
func TestContextForTarget_BindsTheResolvedTenant(t *testing.T) {
	ctx, tenantID, err := ContextForTarget(WithTenant(context.Background(), "globex"), "op", "")
	require.NoError(t, err)
	assert.Equal(t, "globex", tenantID)
	bound, err := FromContext(ctx, "op")
	require.NoError(t, err)
	assert.Equal(t, "globex", bound)

	caller := WithTenant(context.Background(), "acme")
	ctx, tenantID, err = ContextForTarget(caller, "op", "globex")
	requireForbidden[*CrossTenantError](t, err)
	assert.Empty(t, tenantID)
	assert.Equal(t, caller, ctx)
}

func TestAdminTarget_SystemCallerActsInTheNamedTenant(t *testing.T) {
	ctx, tenantID, err := AdminTarget(WithSystem(context.Background()), "op", "acme")
	require.NoError(t, err)
	assert.Equal(t, "acme", tenantID)
	bound, err := FromContext(ctx, "op")
	require.NoError(t, err)
	assert.Equal(t, "acme", bound)
}

func TestAdminTarget_SystemCallerNamingNothingStaysSystem(t *testing.T) {
	ctx, tenantID, err := AdminTarget(WithSystem(context.Background()), "op", "")
	require.NoError(t, err)
	assert.Equal(t, SystemTenantID, tenantID)
	bound, err := FromContext(ctx, "op")
	require.NoError(t, err)
	assert.Equal(t, SystemTenantID, bound)
}

func TestAdminTarget_TenantCallerNamesOnlyItself(t *testing.T) {
	caller := WithTenant(context.Background(), "acme")
	ctx, tenantID, err := AdminTarget(caller, "op", "other")
	cross := requireForbidden[*CrossTenantError](t, err)
	assert.Equal(t, "acme", cross.CallerTenant)
	assert.Equal(t, "other", cross.TargetTenant)
	assert.Empty(t, tenantID)
	assert.Equal(t, caller, ctx)
}

func TestAdminTarget_RefusesAnUnboundCallerWhateverItNames(t *testing.T) {
	for _, requested := range []string{"", "acme", SystemTenantID} {
		_, tenantID, err := AdminTarget(context.Background(), "op", requested)
		requireForbidden[*MissingTenantError](t, err)
		assert.Empty(t, tenantID)
	}
}

// TestErrorMessages_NameTheOperationAndTenants keeps the refusals diagnosable.
func TestErrorMessages_NameTheOperationAndTenants(t *testing.T) {
	assert.Contains(t, (&MissingTenantError{Operation: "kms"}).Error(), "kms")
	msg := (&CrossTenantError{Operation: "kms", CallerTenant: "acme", TargetTenant: "globex"}).Error()
	assert.Contains(t, msg, "acme")
	assert.Contains(t, msg, "globex")
}

func TestAuthRecordRead_NamesOneTenant(t *testing.T) {
	ctx := WithAuthRecordRead(context.Background(), "acme")
	if !InAuthRecordRead(ctx, "acme") {
		t.Fatal("a context marked for acme must report acme")
	}
	if InAuthRecordRead(ctx, "beta") {
		t.Fatal("a context marked for acme must not report beta")
	}
	if InAuthRecordRead(ctx, "") {
		t.Fatal("an empty tenant must never report marked")
	}
	if InAuthRecordRead(context.Background(), "acme") {
		t.Fatal("an unmarked context must not report marked")
	}
	if InAuthRecordRead(WithAuthRecordRead(context.Background(), ""), "") {
		t.Fatal("an empty tenant must leave the context unmarked")
	}
}

func TestPolicySyncRead_NamesOneTenant(t *testing.T) {
	ctx := WithPolicySyncRead(context.Background(), "acme")
	if !InPolicySyncRead(ctx, "acme") {
		t.Fatal("a context marked for acme must report acme")
	}
	if InPolicySyncRead(ctx, "beta") {
		t.Fatal("a context marked for acme must not report beta")
	}
	if InPolicySyncRead(ctx, "") {
		t.Fatal("an empty tenant must never report marked")
	}
	if InPolicySyncRead(context.Background(), "acme") {
		t.Fatal("an unmarked context must not report marked")
	}
	if InPolicySyncRead(WithPolicySyncRead(context.Background(), ""), "") {
		t.Fatal("an empty tenant must leave the context unmarked")
	}
	if InPolicySyncRead(WithAuthRecordRead(context.Background(), "acme"), "acme") {
		t.Fatal("an authentication-record read must not report a policy sync read")
	}
	if InAuthRecordRead(ctx, "acme") {
		t.Fatal("a policy sync read must not report an authentication-record read")
	}
}

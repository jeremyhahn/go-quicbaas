//go:build !integration

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

// server_coverage_boost3_test.go covers IAM-init paths. Specifically the
// BindAdminToSOCert fallback branch that
// looks up the admin user by listing the system tenant's users when the
// in-process bootstrapAdminUserID has not been populated.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// BindAdminToSOCert fallback branch — covers iam_init.go:824-851
//
// The fallback branch runs when bootstrapAdminUserID/TenantID are empty
// (e.g., on process restart before firstBootBootstrap re-runs). It calls
// ListUsers on the system tenant and scans for the "admin" user.
//
// Two scenarios:
//  1. Admin user exists in system tenant → fallback path completes and
//     UpdateUser persists the SPKI thumbprint.
//  2. No admin user in system tenant → returns FirstBootBootstrapError
//     wrapping ErrBootstrapAdminMissing.
// =============================================================================

// TestBoost3_BindAdminToSOCert_FallbackFindsSystemTenantAdmin covers the
// fallback discovery path: when bootstrapAdminUserID is empty but an
// "admin" user exists in the reserved system tenant, BindAdminToSOCert
// recovers the user, applies the SPKI thumbprint, and activates the account.
// The fallback scans qrdb's SystemTenantID (where the bootstrap admin lives),
// not "default".
func TestBoost3_BindAdminToSOCert_FallbackFindsSystemTenantAdmin(t *testing.T) {
	srv := withTempServer(t)
	c := buildTestIAMComponents(t)
	srv.iamComponents = c

	// Intentionally leave bootstrapAdminUserID/TenantID empty so the
	// fallback branch executes. Seed the system tenant with an admin user
	// matching the name the fallback scan looks for.
	ctx := context.Background()
	_, err := c.identityStore.CreateUser(ctx, "system", iam.CreateUserOptions{
		ID:       "admin",
		Email:    "admin@example.test",
		Username: "admin",
		Name:     "admin",
		Enabled:  true,
	})
	require.NoError(t, err)

	// Use a real self-signed cert so SOCertSPKIThumbprint succeeds.
	soCertPEM, _ := newTestSOCertWithSPKI(t, "fallback-admin")
	require.NoError(t, srv.BindAdminToSOCert(ctx, soCertPEM))

	// Verify the discovered admin user now carries the thumbprint and is
	// in active state.
	user, err := c.identityStore.GetUser(ctx, "system", "admin")
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.NotEmpty(t, user.PIVThumbprint(),
		"fallback path must persist the login thumbprint as a PIV-attested binding")
	assert.Equal(t, iam.UserStateActive, user.State,
		"fallback path must activate the admin user")
}

// TestBoost3_BindAdminToSOCert_FallbackNoAdminUser covers the
// "no admin user found" branch within the fallback path: when no user
// matches the "admin" name/ID, the function returns ErrBootstrapAdminMissing
// wrapped in FirstBootBootstrapError.
func TestBoost3_BindAdminToSOCert_FallbackNoAdminUser(t *testing.T) {
	srv := withTempServer(t)
	c := buildTestIAMComponents(t)
	srv.iamComponents = c

	// Default tenant exists but contains no admin user. The fallback
	// ListUsers returns an empty slice; the scan finds nothing and the
	// function returns ErrBootstrapAdminMissing.
	soCertPEM, _ := newTestSOCertWithSPKI(t, "missing-admin")
	err := srv.BindAdminToSOCert(context.Background(), soCertPEM)
	require.Error(t, err)

	var typedErr *FirstBootBootstrapError
	require.True(t, errors.As(err, &typedErr),
		"fallback path must return FirstBootBootstrapError when no admin exists; got %T: %v", err, err)
	assert.True(t, errors.Is(typedErr.Cause, ErrBootstrapAdminMissing),
		"fallback no-admin error must wrap ErrBootstrapAdminMissing; got %v", typedErr.Cause)
}

// TestBoost3_BindAdminToSOCert_FallbackMatchesUserByID verifies the
// fallback scan's "match by ID" branch — the loop accepts either
// u.Name == "admin" OR u.ID == "admin". A user with ID "admin" but no
// Name should still be discovered.
func TestBoost3_BindAdminToSOCert_FallbackMatchesUserByID(t *testing.T) {
	srv := withTempServer(t)
	c := buildTestIAMComponents(t)
	srv.iamComponents = c

	ctx := context.Background()
	// Create a user whose Name is intentionally NOT "admin" but whose ID is.
	// The fallback scan's `u.Name == "admin" || u.ID == "admin"` branch
	// must still match this user. Seeded in the system tenant (where the
	// fallback scans).
	_, err := c.identityStore.CreateUser(ctx, "system", iam.CreateUserOptions{
		ID:       "admin",
		Email:    "id-admin@example.test",
		Username: "rootuser",
		Name:     "Root User", // Deliberately not "admin"
		Enabled:  true,
	})
	require.NoError(t, err)

	soCertPEM, _ := newTestSOCertWithSPKI(t, "id-admin")
	require.NoError(t, srv.BindAdminToSOCert(ctx, soCertPEM))

	user, err := c.identityStore.GetUser(ctx, "system", "admin")
	require.NoError(t, err)
	assert.NotEmpty(t, user.PIVThumbprint())
}

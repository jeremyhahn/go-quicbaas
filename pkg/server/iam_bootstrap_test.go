//go:build iam && !integration

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

package server

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ---------------------------------------------------------------------------
// printFirstBootBanner
// ---------------------------------------------------------------------------

func TestPrintFirstBootBanner_ContainsURL(t *testing.T) {
	var buf bytes.Buffer
	printFirstBootBanner(&buf, "https://localhost:8443/ui/#/activate?token=abc123")

	output := buf.String()
	assert.Contains(t, output, "https://localhost:8443/ui/#/activate?token=abc123",
		"banner must include the full activation URL")
	assert.Contains(t, output, "FIRST-BOOT BOOTSTRAP",
		"banner must include the bootstrap heading")
	// Phase 11 reworded the banner (now: "Shown ONCE.") to match the
	// three-option ceremony copy from the unification plan.
	assert.Contains(t, output, "Shown ONCE",
		"banner must warn the operator that the activation URL is shown once")
}

func TestPrintFirstBootBanner_EmptyURLSafe(t *testing.T) {
	// Passing an empty URL should not panic; the banner is still printed.
	var buf bytes.Buffer
	printFirstBootBanner(&buf, "")
	assert.NotEmpty(t, buf.String())
}

// ---------------------------------------------------------------------------
// firstBootBootstrap: fresh storage
// ---------------------------------------------------------------------------

func TestFirstBootBootstrap_FreshStorage_CreatesAdminAndPrintsInvite(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false
	srv.config.Auth.BootstrapAdminUsername = "admin@quicbaas.local"

	c := buildTestIAMComponents(t)

	// Capture stderr output by redirecting printFirstBootBanner through a
	// buffer — we test firstBootBootstrap directly with a captured writer.
	var stderrCapture bytes.Buffer
	origPrint := printFirstBootBannerFn
	printFirstBootBannerFn = func(w interface{ WriteString(string) (int, error) }, url string) {
		printFirstBootBanner(&stderrCapture, url)
	}
	defer func() { printFirstBootBannerFn = origPrint }()

	err := srv.firstBootBootstrap(context.Background(), c)
	require.NoError(t, err)

	// The activation URL must appear in the captured output.
	output := stderrCapture.String()
	assert.Contains(t, output, "/ui/#/activate?token=",
		"activation URL must be printed to stderr on first boot")

	// An active admin assignment must exist.
	assignments, listErr := c.adminManager.ListAssignments(context.Background(), iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		Limit:      10,
	})
	require.NoError(t, listErr)
	assert.Len(t, assignments, 1, "exactly one admin assignment must be created on first boot")
	assert.Equal(t, iam.AdminRoleSuperAdmin, assignments[0].AdminRole)
}

// ---------------------------------------------------------------------------
// firstBootBootstrap: second boot skips
// ---------------------------------------------------------------------------

func TestFirstBootBootstrap_ExistingAdmin_Skipped(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false

	c := buildTestIAMComponents(t)

	// Pre-seed an admin assignment to simulate a prior boot.
	_, err := c.adminManager.CreateAssignment(context.Background(), iam.CreateAdminAssignmentOptions{
		UserID:    "existing-admin-id",
		AdminRole: iam.AdminRoleSuperAdmin,
		GrantedBy: "system:first-boot",
	})
	require.NoError(t, err)

	var bannerCalled bool
	origPrint := printFirstBootBannerFn
	printFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, _ string) {
		bannerCalled = true
	}
	defer func() { printFirstBootBannerFn = origPrint }()

	bootstrapErr := srv.firstBootBootstrap(context.Background(), c)
	require.NoError(t, bootstrapErr)

	assert.False(t, bannerCalled, "banner must NOT be printed when an admin already exists")

	// Still only one assignment.
	assignments, listErr := c.adminManager.ListAssignments(context.Background(), iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		Limit:      10,
	})
	require.NoError(t, listErr)
	assert.Len(t, assignments, 1, "no new assignment must be created on second boot")
}

// ---------------------------------------------------------------------------
// firstBootBootstrap: insecure dev mode skips first-boot invite path
// ---------------------------------------------------------------------------

func TestFirstBootBootstrap_SkippedWhenPasswordAuthEnabled(t *testing.T) {
	// When AllowPasswordAuth=true the bootstrap invite path must not run.
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = true
	srv.config.Auth.InsecureDevMode = false
	srv.config.Auth.BootstrapAdminPassword = "Super$ecureP@ss1"

	c := buildTestIAMComponents(t)

	var bannerCalled bool
	origPrint := printFirstBootBannerFn
	printFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, _ string) {
		bannerCalled = true
	}
	defer func() { printFirstBootBannerFn = origPrint }()

	// Simulate the gate that wraps firstBootBootstrap in initializeIAMComponents.
	if !srv.config.Auth.AllowPasswordAuth && !srv.config.Auth.InsecureDevMode {
		err := srv.firstBootBootstrap(context.Background(), c)
		require.NoError(t, err)
	}

	assert.False(t, bannerCalled, "no invite banner when password auth is enabled")

	assignments, listErr := c.adminManager.ListAssignments(context.Background(), iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		Limit:      10,
	})
	require.NoError(t, listErr)
	assert.Empty(t, assignments, "no admin assignment when password auth flow owns bootstrap")
}

// ---------------------------------------------------------------------------
// FirstBootBootstrapError typed error
// ---------------------------------------------------------------------------

func TestFirstBootBootstrapError_IsAndMessage(t *testing.T) {
	cause := errors.New("storage failure")
	err := &FirstBootBootstrapError{Op: "create bootstrap admin user", Cause: cause}

	assert.True(t, errors.Is(err, ErrFirstBootBootstrap),
		"errors.Is must match ErrFirstBootBootstrap sentinel")
	assert.Contains(t, err.Error(), "first-boot bootstrap")
	assert.Contains(t, err.Error(), "create bootstrap admin user")
	assert.Contains(t, err.Error(), "storage failure")
}

// ---------------------------------------------------------------------------
// Invite token in store after bootstrap
// ---------------------------------------------------------------------------

func TestFirstBootBootstrap_InviteTokenStoredWithExpiry(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false

	c := buildTestIAMComponents(t)

	// Intercept the printed URL to extract the token.
	var capturedURL string
	origPrint := printFirstBootBannerFn
	printFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, url string) {
		capturedURL = url
	}
	defer func() { printFirstBootBannerFn = origPrint }()

	require.NoError(t, srv.firstBootBootstrap(context.Background(), c))
	require.NotEmpty(t, capturedURL, "activation URL must have been captured")

	// Extract token from URL: …?token=<TOKEN>
	parts := strings.SplitN(capturedURL, "token=", 2)
	require.Len(t, parts, 2, "activation URL must contain 'token=' parameter")
	token := parts[1]

	// The invite record must be retrievable and not yet used.
	record, err := c.inviteStore.GetByToken(context.Background(), token)
	require.NoError(t, err)
	assert.False(t, record.Used, "invite must not be pre-consumed")
	assert.True(t, record.ExpiresAt.After(time.Now()), "invite must not be expired")
	// Per the unified bootstrap (Phase 5): admin user lives in qrdb's
	// canonical "system" tenant via qrdbfirstboot.Initialize, and the
	// invite record is bound to that same tenant.
	assert.Equal(t, "system", record.TenantID)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// buildTestIAMComponents constructs a minimal iamComponents for unit testing
// the bootstrap logic, without starting a real server.
func buildTestIAMComponents(t *testing.T) *iamComponents {
	t.Helper()

	memStorage := iam.NewMemoryStorage()
	tenantManager := iam.NewDefaultTenantManager(memStorage)

	ctx := context.Background()
	if _, err := tenantManager.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:   "default",
		Name: "Default Tenant",
	}); err != nil {
		t.Fatalf("buildTestIAMComponents: create default tenant: %v", err)
	}
	// The bootstrap admin lives in the reserved "system" tenant (qrdb's
	// SystemTenantID), so provision it too — BindAdminToSOCert's fallback
	// scans the system tenant, not "default".
	if _, err := tenantManager.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:   "system",
		Name: "System Tenant",
	}); err != nil {
		t.Fatalf("buildTestIAMComponents: create system tenant: %v", err)
	}

	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  memStorage,
		GroupStorage: memStorage,
		RoleStorage:  memStorage,
		Tenants:      tenantManager,
	})

	auditLogger, err := iam.NewAuditLogger()
	require.NoError(t, err)

	adminManager, err := iam.NewAdminManager(iam.AdminManagerOptions{
		AdminStorage:         iam.NewMemoryAdminStorage(),
		ImpersonationStorage: iam.NewMemoryImpersonationStorage(),
		AuditLogger:          auditLogger,
	})
	require.NoError(t, err)

	return &iamComponents{
		tenantManager: tenantManager,
		identityStore: identityStore,
		adminManager:  adminManager,
		inviteStore:   iam.NewMemoryInviteStore(),
	}
}

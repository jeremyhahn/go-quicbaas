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

package embedded

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	gdprTestTenant = "tenant-a"
	gdprTestUserID = uint64(71)
	gdprTestUser   = "71"
)

var gdprVerbs = []string{
	"GrantConsent", "WithdrawConsent", "GetConsent", "ListConsents",
	"RequestExport", "GetExportStatus", "GenerateExport",
	"RequestDeletion", "CancelDeletion", "GetDeletionStatus",
}

// gdprMock serves a GDPR manager and counts how often a handler asks for it.
type gdprMock struct {
	*mockQuicBaaS
	mgr   *iam.GDPRManager
	calls atomic.Int32
}

func (m *gdprMock) GetGDPRManager(_ context.Context) (*iam.GDPRManager, error) {
	m.calls.Add(1)
	return m.mgr, nil
}

// gdprFixture is a GDPR manager over in-memory storage with its stores
// exposed, so a test can seed records the manager would otherwise reach only
// through background processing.
type gdprFixture struct {
	exports   *iam.MemoryDataExportStorage
	deletions *iam.MemoryDeletionStorage
	svc       *gdprMock
	client    iamsdk.GDPRClient
	tr        *Transport
}

func newGDPRFixture(t *testing.T) *gdprFixture {
	t.Helper()
	users := iam.NewMemoryStorage()
	require.NoError(t, users.CreateUser(context.Background(), &iam.User{
		ID: gdprTestUserID, TenantID: gdprTestTenant, Email: "gdpr@example.com",
	}))
	f := &gdprFixture{
		exports:   iam.NewMemoryDataExportStorage(),
		deletions: iam.NewMemoryDeletionStorage(),
	}
	mgr, err := iam.NewGDPRManager(iam.GDPRManagerDependencies{
		ConsentStorage:    iam.NewMemoryConsentStorage(),
		DataExportStorage: f.exports,
		DeletionStorage:   f.deletions,
		UserStorage:       users,
	})
	require.NoError(t, err)
	f.svc = &gdprMock{mockQuicBaaS: &mockQuicBaaS{}, mgr: mgr}
	f.tr = mustNewTransport(t, f.svc)
	f.client = iamsdk.NewClient(f.tr).GDPR()
	return f
}

// seedExport stores an export of the given owner in the given status.
func (f *gdprFixture) seedExport(t *testing.T, tenantID string, userID uint64, status iam.DataExportStatus) string {
	t.Helper()
	ctx := context.Background()
	rec, err := f.exports.CreateExportRequest(ctx, iam.CreateDataExportOptions{
		UserID: userID, TenantID: tenantID, RequestedBy: userID, Format: iam.DataExportFormatJSON,
	})
	require.NoError(t, err)
	expires := time.Now().Add(time.Hour)
	rec.Status = status
	rec.ExpiresAt = &expires
	require.NoError(t, f.exports.UpdateExportRequest(ctx, rec))
	return rec.ID
}

// seedDeletion stores a pending erasure of the given owner.
func (f *gdprFixture) seedDeletion(t *testing.T, tenantID string, userID uint64) string {
	t.Helper()
	rec, err := f.deletions.CreateDeletionRequest(context.Background(), iam.CreateDeletionRequestOptions{
		UserID: userID, TenantID: tenantID, RequestedBy: userID, Reason: "seeded",
	})
	require.NoError(t, err)
	return rec.ID
}

func TestEmbeddedGDPRDispatchRegistered(t *testing.T) {
	for _, method := range gdprVerbs {
		_, ok := unaryHandlers["quicbaas.IAM.GDPR/"+method]
		assert.True(t, ok, "quicbaas.IAM.GDPR/%s is not registered", method)
	}
	assert.Len(t, iamGDPRUnaryHandlers, len(gdprVerbs))
}

func TestEmbeddedGDPRConsentLifecycle(t *testing.T) {
	f := newGDPRFixture(t)
	ctx := tenantA()
	expires := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)

	require.NoError(t, f.client.GrantConsent(ctx, gdprTestTenant, gdprTestUser, &iamsdk.GrantConsentRequest{
		Purpose: "marketing", Version: "3", ExpiresAt: expires,
	}))

	consent, err := f.client.GetConsent(ctx, gdprTestTenant, gdprTestUser, "marketing")
	require.NoError(t, err)
	assert.Equal(t, gdprTestTenant, consent.TenantID)
	assert.Equal(t, gdprTestUser, consent.UserID)
	assert.Equal(t, "marketing", consent.Purpose)
	assert.Equal(t, iamsdk.ConsentStatusGranted, consent.Status)
	assert.Equal(t, iamsdk.FlexInt(3), consent.Version)
	assert.True(t, expires.Equal(consent.ExpiresAt))
	assert.False(t, consent.GrantedAt.IsZero())

	list, err := f.client.ListConsents(ctx, gdprTestTenant, gdprTestUser)
	require.NoError(t, err)
	require.Equal(t, 1, list.TotalCount)
	assert.Equal(t, consent.ID, list.Consents[0].ID)

	require.NoError(t, f.client.WithdrawConsent(ctx, gdprTestTenant, gdprTestUser, "marketing"))
	consent, err = f.client.GetConsent(ctx, gdprTestTenant, gdprTestUser, "marketing")
	require.NoError(t, err)
	assert.Equal(t, iamsdk.ConsentStatusWithdrawn, consent.Status)
	assert.False(t, consent.WithdrawnAt.IsZero())
}

func TestEmbeddedGDPRListConsentsEmpty(t *testing.T) {
	f := newGDPRFixture(t)
	list, err := f.client.ListConsents(tenantA(), gdprTestTenant, gdprTestUser)
	require.NoError(t, err)
	assert.Zero(t, list.TotalCount)
	assert.Empty(t, list.Consents)
}

func TestEmbeddedGDPRConsentErrors(t *testing.T) {
	f := newGDPRFixture(t)
	ctx := tenantA()

	t.Run("GrantConsent", func(t *testing.T) {
		err := f.client.GrantConsent(ctx, gdprTestTenant, "not-a-number", &iamsdk.GrantConsentRequest{Purpose: "marketing"})
		var invalid *InvalidUserIDError
		require.ErrorAs(t, err, &invalid)
	})
	t.Run("WithdrawConsent", func(t *testing.T) {
		require.Error(t, f.client.WithdrawConsent(ctx, gdprTestTenant, gdprTestUser, "never-granted"))
	})
	t.Run("GetConsent", func(t *testing.T) {
		_, err := f.client.GetConsent(ctx, gdprTestTenant, gdprTestUser, "never-granted")
		require.Error(t, err)
	})
	t.Run("ListConsents", func(t *testing.T) {
		_, err := f.client.ListConsents(ctx, gdprTestTenant, "-1")
		var invalid *InvalidUserIDError
		require.ErrorAs(t, err, &invalid)
	})
}

func TestEmbeddedGDPRRequestExport(t *testing.T) {
	f := newGDPRFixture(t)
	export, err := f.client.RequestExport(tenantA(), gdprTestTenant, gdprTestUser, &iamsdk.ExportRequest{
		Format: iamsdk.ExportFormatJSON, IncludeCategories: []string{"profile"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, export.ID)
	assert.Equal(t, gdprTestTenant, export.TenantID)
	assert.Equal(t, gdprTestUser, export.UserID)
	assert.Equal(t, iamsdk.ExportFormatJSON, export.Format)
	assert.Equal(t, []string{"profile"}, export.IncludeCategories)
	assert.False(t, export.RequestedAt.IsZero())
}

func TestEmbeddedGDPRRequestExportDefaultsToJSON(t *testing.T) {
	f := newGDPRFixture(t)
	export, err := f.client.RequestExport(tenantA(), gdprTestTenant, gdprTestUser, nil)
	require.NoError(t, err)
	assert.Equal(t, iamsdk.ExportFormatJSON, export.Format)
}

// The export generator writes JSON only, so the manager refuses CSV; the
// embedded path must surface that refusal, not record the export.
func TestEmbeddedGDPRRequestExportCSVRefused(t *testing.T) {
	f := newGDPRFixture(t)
	_, err := f.client.RequestExport(tenantA(), gdprTestTenant, gdprTestUser, &iamsdk.ExportRequest{Format: iamsdk.ExportFormatCSV})
	var unsupported *iam.UnsupportedDataExportFormatError
	require.ErrorAs(t, err, &unsupported)
}

func TestEmbeddedGDPRRequestExportUnknownFormat(t *testing.T) {
	f := newGDPRFixture(t)
	_, err := f.client.RequestExport(tenantA(), gdprTestTenant, gdprTestUser, &iamsdk.ExportRequest{Format: iamsdk.ExportFormat(9)})
	require.Error(t, err)
}

func TestEmbeddedGDPRGetExportStatus(t *testing.T) {
	f := newGDPRFixture(t)
	id := f.seedExport(t, gdprTestTenant, gdprTestUserID, iam.DataExportStatusCompleted)

	export, err := f.client.GetExportStatus(tenantA(), gdprTestTenant, gdprTestUser, id)
	require.NoError(t, err)
	assert.Equal(t, id, export.ID)
	assert.Equal(t, iamsdk.ExportStatusCompleted, export.Status)
	assert.False(t, export.ExpiresAt.IsZero())
}

func TestEmbeddedGDPRGetExportStatusErrors(t *testing.T) {
	f := newGDPRFixture(t)
	ctx := tenantA()

	t.Run("unknown export", func(t *testing.T) {
		_, err := f.client.GetExportStatus(ctx, gdprTestTenant, gdprTestUser, "missing")
		require.Error(t, err)
	})
	t.Run("another data subject's export", func(t *testing.T) {
		id := f.seedExport(t, gdprTestTenant, 72, iam.DataExportStatusCompleted)
		_, err := f.client.GetExportStatus(ctx, gdprTestTenant, gdprTestUser, id)
		assert.True(t, sdkerrors.IsNotFound(err), "got %v", err)
	})
	t.Run("another tenant's export", func(t *testing.T) {
		id := f.seedExport(t, "tenant-b", gdprTestUserID, iam.DataExportStatusCompleted)
		_, err := f.client.GetExportStatus(ctx, gdprTestTenant, gdprTestUser, id)
		assert.True(t, sdkerrors.IsNotFound(err), "got %v", err)
	})
}

func TestEmbeddedGDPRGenerateExport(t *testing.T) {
	f := newGDPRFixture(t)
	id := f.seedExport(t, gdprTestTenant, gdprTestUserID, iam.DataExportStatusCompleted)

	export, err := f.client.GenerateExport(tenantA(), gdprTestTenant, gdprTestUser, id)
	require.NoError(t, err)
	assert.Equal(t, id, export.RequestID)
	assert.Equal(t, gdprTestUser, export.UserID)
	assert.Equal(t, iamsdk.ExportFormatJSON, export.Format)
	assert.Contains(t, string(export.Data), "gdpr@example.com")
	assert.False(t, export.ExpiresAt.IsZero())
}

func TestEmbeddedGDPRGenerateExportErrors(t *testing.T) {
	f := newGDPRFixture(t)
	ctx := tenantA()

	t.Run("export not completed", func(t *testing.T) {
		id := f.seedExport(t, gdprTestTenant, gdprTestUserID, iam.DataExportStatusPending)
		_, err := f.client.GenerateExport(ctx, gdprTestTenant, gdprTestUser, id)
		assert.True(t, sdkerrors.IsConflict(err), "got %v", err)
	})
	t.Run("another data subject's export", func(t *testing.T) {
		id := f.seedExport(t, gdprTestTenant, 72, iam.DataExportStatusCompleted)
		_, err := f.client.GenerateExport(ctx, gdprTestTenant, gdprTestUser, id)
		assert.True(t, sdkerrors.IsNotFound(err), "got %v", err)
	})
}

func TestEmbeddedGDPRRequestDeletion(t *testing.T) {
	f := newGDPRFixture(t)
	deletion, err := f.client.RequestDeletion(tenantA(), gdprTestTenant, gdprTestUser, &iamsdk.DeletionRequest{
		Reason: "leaving", RetainCategories: []string{"audit_logs"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, deletion.ID)
	assert.Equal(t, gdprTestUser, deletion.UserID)
	assert.Equal(t, "leaving", deletion.Reason)
	assert.Equal(t, iamsdk.DeletionStatusPending, deletion.Status)
	assert.Equal(t, []string{"audit_logs"}, deletion.RetainCategories)
	// No date keeps the manager's default grace period rather than erasing now.
	assert.True(t, deletion.ScheduledFor.After(time.Now().Add(time.Duration(iam.DefaultGracePeriodDays-1)*24*time.Hour)))
}

func TestEmbeddedGDPRRequestDeletionScheduled(t *testing.T) {
	f := newGDPRFixture(t)
	deletion, err := f.client.RequestDeletion(tenantA(), gdprTestTenant, gdprTestUser, &iamsdk.DeletionRequest{
		Reason: "leaving", ScheduledFor: time.Now().Add(36 * time.Hour),
	})
	require.NoError(t, err)
	// 36 hours rounds up to a two-day grace period.
	assert.WithinDuration(t, time.Now().Add(48*time.Hour), deletion.ScheduledFor, time.Minute)
}

func TestEmbeddedGDPRRequestDeletionPastDateRefused(t *testing.T) {
	f := newGDPRFixture(t)
	_, err := f.client.RequestDeletion(tenantA(), gdprTestTenant, gdprTestUser, &iamsdk.DeletionRequest{
		Reason: "leaving", ScheduledFor: time.Now().Add(-time.Hour),
	})
	assert.True(t, sdkerrors.IsInvalidInput(err), "got %v", err)
}

func TestEmbeddedGDPRGracePeriodDays(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	days, err := gdprGracePeriodDays(time.Time{}, now)
	require.NoError(t, err)
	assert.Equal(t, iam.DefaultGracePeriodDays, days)

	days, err = gdprGracePeriodDays(now.Add(time.Minute), now)
	require.NoError(t, err)
	assert.Equal(t, 1, days)

	_, err = gdprGracePeriodDays(now, now)
	assert.True(t, sdkerrors.IsInvalidInput(err))
}

func TestEmbeddedGDPRCancelDeletion(t *testing.T) {
	f := newGDPRFixture(t)
	ctx := tenantA()
	id := f.seedDeletion(t, gdprTestTenant, gdprTestUserID)

	require.NoError(t, f.client.CancelDeletion(ctx, gdprTestTenant, gdprTestUser, id))
	deletion, err := f.client.GetDeletionStatus(ctx, gdprTestTenant, gdprTestUser, id)
	require.NoError(t, err)
	assert.Equal(t, iamsdk.DeletionStatusCancelled, deletion.Status)
	assert.False(t, deletion.CancelledAt.IsZero())
}

func TestEmbeddedGDPRCancelDeletionErrors(t *testing.T) {
	f := newGDPRFixture(t)
	ctx := tenantA()

	t.Run("unknown deletion", func(t *testing.T) {
		require.Error(t, f.client.CancelDeletion(ctx, gdprTestTenant, gdprTestUser, "missing"))
	})
	t.Run("another data subject's deletion", func(t *testing.T) {
		id := f.seedDeletion(t, gdprTestTenant, 72)
		err := f.client.CancelDeletion(ctx, gdprTestTenant, gdprTestUser, id)
		assert.True(t, sdkerrors.IsNotFound(err), "got %v", err)
		rec, getErr := f.deletions.GetDeletionRequest(context.Background(), id)
		require.NoError(t, getErr)
		assert.Equal(t, iam.DeletionStatusPending, rec.Status, "a refused cancel must leave the erasure pending")
	})
}

func TestEmbeddedGDPRGetDeletionStatus(t *testing.T) {
	f := newGDPRFixture(t)
	id := f.seedDeletion(t, gdprTestTenant, gdprTestUserID)

	deletion, err := f.client.GetDeletionStatus(tenantA(), gdprTestTenant, gdprTestUser, id)
	require.NoError(t, err)
	assert.Equal(t, id, deletion.ID)
	assert.Equal(t, "seeded", deletion.Reason)
	assert.Equal(t, iamsdk.DeletionStatusPending, deletion.Status)
}

func TestEmbeddedGDPRGetDeletionStatusErrors(t *testing.T) {
	f := newGDPRFixture(t)
	ctx := tenantA()

	t.Run("unknown deletion", func(t *testing.T) {
		_, err := f.client.GetDeletionStatus(ctx, gdprTestTenant, gdprTestUser, "missing")
		require.Error(t, err)
	})
	t.Run("another tenant's deletion", func(t *testing.T) {
		id := f.seedDeletion(t, "tenant-b", gdprTestUserID)
		_, err := f.client.GetDeletionStatus(ctx, gdprTestTenant, gdprTestUser, id)
		assert.True(t, sdkerrors.IsNotFound(err), "got %v", err)
	})
}

func TestEmbeddedGDPRCrossTenantRefused(t *testing.T) {
	f := newGDPRFixture(t)
	ctx := tenantA()
	const other = "tenant-b"

	calls := map[string]func() error{
		"GrantConsent": func() error {
			return f.client.GrantConsent(ctx, other, gdprTestUser, &iamsdk.GrantConsentRequest{Purpose: "marketing"})
		},
		"WithdrawConsent": func() error { return f.client.WithdrawConsent(ctx, other, gdprTestUser, "marketing") },
		"GetConsent": func() error {
			_, err := f.client.GetConsent(ctx, other, gdprTestUser, "marketing")
			return err
		},
		"ListConsents": func() error {
			_, err := f.client.ListConsents(ctx, other, gdprTestUser)
			return err
		},
		"RequestExport": func() error {
			_, err := f.client.RequestExport(ctx, other, gdprTestUser, nil)
			return err
		},
		"GetExportStatus": func() error {
			_, err := f.client.GetExportStatus(ctx, other, gdprTestUser, "e")
			return err
		},
		"GenerateExport": func() error {
			_, err := f.client.GenerateExport(ctx, other, gdprTestUser, "e")
			return err
		},
		"RequestDeletion": func() error {
			_, err := f.client.RequestDeletion(ctx, other, gdprTestUser, nil)
			return err
		},
		"CancelDeletion": func() error { return f.client.CancelDeletion(ctx, other, gdprTestUser, "d") },
		"GetDeletionStatus": func() error {
			_, err := f.client.GetDeletionStatus(ctx, other, gdprTestUser, "d")
			return err
		},
	}
	require.Len(t, calls, len(gdprVerbs))
	for method, call := range calls {
		t.Run(method, func(t *testing.T) {
			require.Error(t, call())
		})
	}
	assert.Zero(t, f.svc.calls.Load(), "a refused call must not reach the GDPR manager")
}

func TestEmbeddedGDPRNotConfigured(t *testing.T) {
	cases := map[string]*Transport{
		"service without GDPR": mustNewTransport(t, &mockQuicBaaS{}),
		"nil manager":          mustNewTransport(t, &gdprMock{mockQuicBaaS: &mockQuicBaaS{}}),
	}
	for name, tr := range cases {
		t.Run(name, func(t *testing.T) {
			client := iamsdk.NewClient(tr).GDPR()
			_, err := client.ListConsents(tenantA(), gdprTestTenant, gdprTestUser)
			var notConfigured *ServiceNotConfiguredError
			require.ErrorAs(t, err, &notConfigured)
		})
	}
}

func TestEmbeddedGDPRInvalidRequest(t *testing.T) {
	f := newGDPRFixture(t)
	type bareRequest struct {
		TenantID string
		UserID   string
	}
	bad := map[string]interface{}{
		"typed nil": (*bareRequest)(nil),
		"string":    "not a request",
		"int":       42,
		"map":       map[string]string{"TenantID": gdprTestTenant},
	}
	for _, method := range gdprVerbs {
		for name, req := range bad {
			t.Run(method+"/"+name, func(t *testing.T) {
				_, err := f.tr.Request(tenantA(), gdprServiceName, method, req)
				var invalid *InvalidRequestTypeError
				require.ErrorAs(t, err, &invalid)
			})
		}
	}
	t.Run("GrantConsent without a consent", func(t *testing.T) {
		_, err := f.tr.Request(tenantA(), gdprServiceName, "GrantConsent",
			&bareRequest{TenantID: gdprTestTenant, UserID: gdprTestUser})
		var invalid *InvalidRequestTypeError
		require.ErrorAs(t, err, &invalid)
	})
}

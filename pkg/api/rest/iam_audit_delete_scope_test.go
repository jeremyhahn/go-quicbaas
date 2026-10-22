//go:build rest

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
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// deleteRecordingAuditLogger counts the DeleteOlderThan calls that reach it.
type deleteRecordingAuditLogger struct {
	captureAuditLogger
	deletes int
}

func (l *deleteRecordingAuditLogger) DeleteOlderThan(_ context.Context, _ string, _ time.Time) (int, error) {
	l.deletes++
	return 3, nil
}

// newAuditDeleteScopeHandler returns a handler whose caller (user 70001)
// holds audit:delete on tenant1, so only the scope refusal can stop a delete.
func newAuditDeleteScopeHandler(t *testing.T) (*IAMHandler, *deleteRecordingAuditLogger) {
	t.Helper()
	admin := newTestAdminManager(t)
	_, err := admin.CreateAssignment(tenantctx.WithSystem(context.Background()), iam.CreateAdminAssignmentOptions{
		UserID:      70001,
		AdminRole:   iam.AdminRoleTenantAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []iam.AdminPermission{iam.PermAuditDelete},
		GrantedBy:   "super",
	})
	require.NoError(t, err)
	audit := &deleteRecordingAuditLogger{}
	svc := &adminAuditService{MockQuicBaaS: testutil.NewMockQuicBaaS(), admin: admin, audit: audit}
	return NewIAMHandler(IAMHandlerDependencies{Server: &Server{service: svc}}), audit
}

func auditDeleteScopeReq(extraQuery string) *http.Request {
	r := auditDeleteReq(70001)
	r.URL.RawQuery += "&" + extraQuery
	return r
}

// TestDeleteOldAuditEvents_UnsupportedScopeRefused proves a dry run or an
// event-type filter answers 400 with the shared SDK refusal and never reaches
// DeleteOlderThan, which would delete every event before the cutoff.
func TestDeleteOldAuditEvents_UnsupportedScopeRefused(t *testing.T) {
	dryRunMsg := (&iamsdk.AuditDeleteDryRunUnsupportedError{}).Error()
	eventTypesMsg := (&iamsdk.AuditDeleteEventTypesUnsupportedError{}).Error()
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"dry run", "dry_run=true", dryRunMsg},
		{"malformed dry run", "dry_run=maybe", dryRunMsg},
		{"event types", "event_types=authorization", eventTypesMsg},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, audit := newAuditDeleteScopeHandler(t)
			w := httptest.NewRecorder()
			handler.DeleteOldAuditEvents(w, auditDeleteScopeReq(tt.query))
			assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
			assert.Contains(t, w.Body.String(), tt.want)
			assert.Zero(t, audit.deletes, "a refused delete reached DeleteOlderThan")
		})
	}
}

// TestDeleteOldAuditEvents_RepeatedParamRefused pins that a repeated
// single-valued parameter is refused before any delete: url.Values.Get reads
// only the first value, so "dry_run=false&dry_run=true" would otherwise
// delete although the caller also asked for a preview.
func TestDeleteOldAuditEvents_RepeatedParamRefused(t *testing.T) {
	tests := []struct {
		name  string
		query string
		param string
	}{
		{"dry run false then true", "dry_run=false&dry_run=true", "dry_run"},
		{"dry run true then false", "dry_run=true&dry_run=false", "dry_run"},
		{"dry run false then malformed", "dry_run=false&dry_run=maybe", "dry_run"},
		{"tenant id", "tenant_id=tenant2", "tenant_id"},
		{"before", "before=2025-01-01T00:00:00Z", "before"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, audit := newAuditDeleteScopeHandler(t)
			w := httptest.NewRecorder()
			handler.DeleteOldAuditEvents(w, auditDeleteScopeReq(tt.query))
			assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
			want := (&AuditDeleteRepeatedParamError{Name: tt.param}).Error()
			assert.Contains(t, w.Body.String(), want)
			assert.Zero(t, audit.deletes, "a repeated parameter reached DeleteOlderThan")
		})
	}
}

// TestDeleteOldAuditEvents_MalformedQueryRefused pins Codex review 6480: a
// malformed pair, which r.URL.Query() drops silently, refuses the whole
// request with 400 before any delete, whichever key it is on.
func TestDeleteOldAuditEvents_MalformedQueryRefused(t *testing.T) {
	for name, query := range map[string]string{
		"semicolon after dry run": "dry_run=true;",
		"bad escape in dry run":   "dry_run=%ZZ",
		"bad escape on other key": "note=%ZZ",
	} {
		t.Run(name, func(t *testing.T) {
			handler, audit := newAuditDeleteScopeHandler(t)
			w := httptest.NewRecorder()
			handler.DeleteOldAuditEvents(w, auditDeleteScopeReq(query))
			assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
			assert.Contains(t, w.Body.String(), "malformed query string")
			assert.Zero(t, audit.deletes, "a malformed query reached DeleteOlderThan")
		})
	}
}

// TestDeleteOldAuditEvents_PlainDeleteDeletes proves an unscoped delete, and
// one with dry_run=false, still reach DeleteOlderThan.
func TestDeleteOldAuditEvents_PlainDeleteDeletes(t *testing.T) {
	for name, req := range map[string]*http.Request{
		"no scope":      auditDeleteReq(70001),
		"dry run false": auditDeleteScopeReq("dry_run=false"),
	} {
		t.Run(name, func(t *testing.T) {
			handler, audit := newAuditDeleteScopeHandler(t)
			w := httptest.NewRecorder()
			handler.DeleteOldAuditEvents(w, req)
			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
			assert.Equal(t, 1, audit.deletes)
		})
	}
}

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

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

const auditDeleteTool = "quicbaas_iam_audit_delete"

// TestAuditDelete_UnsupportedScopeRefused proves a dry run or an event-type
// filter is refused client-side with the shared typed error, and that the
// server's delete tool is never called.
func TestAuditDelete_UnsupportedScopeRefused(t *testing.T) {
	before := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		req  *iamsdk.AuditDeleteRequest
		want func(error) bool
	}{
		{"dry run", &iamsdk.AuditDeleteRequest{TenantID: "t1", Before: before, Confirm: true, DryRun: true},
			func(err error) bool {
				var refused *iamsdk.AuditDeleteDryRunUnsupportedError
				return errors.As(err, &refused)
			}},
		{"event types", &iamsdk.AuditDeleteRequest{TenantID: "t1", Before: before, Confirm: true,
			EventTypes: []iamsdk.AuditEventType{iamsdk.AuditEventTypeAuthorization}},
			func(err error) bool {
				var refused *iamsdk.AuditDeleteEventTypesUnsupportedError
				return errors.As(err, &refused)
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, called, cleanup := newMultiToolMCPServer(t, []string{auditDeleteTool},
				func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
					return map[string]interface{}{"deleted": 3}
				})
			defer cleanup()

			err := iamsdk.NewAuditClient(tr).Delete(context.Background(), tt.req)
			if !tt.want(err) {
				t.Fatalf("err = %v, want the %s refusal", err, tt.name)
			}
			if len(*called) != 0 {
				t.Fatalf("a refused Delete reached the server: %v", *called)
			}
		})
	}
}

// TestAuditDelete_PlainDeleteSent proves an unfiltered, confirmed delete
// still reaches the server's delete tool with its tenant and cutoff.
func TestAuditDelete_PlainDeleteSent(t *testing.T) {
	var gotArgs map[string]interface{}
	tr, called, cleanup := newMultiToolMCPServer(t, []string{auditDeleteTool},
		func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
			gotArgs = args
			return map[string]interface{}{"deleted": 3}
		})
	defer cleanup()

	before := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err := iamsdk.NewAuditClient(tr).Delete(context.Background(),
		&iamsdk.AuditDeleteRequest{TenantID: "t1", Before: before, Confirm: true})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(*called) != 1 || (*called)[0] != auditDeleteTool {
		t.Fatalf("called = %v, want one %s call", *called, auditDeleteTool)
	}
	if gotArgs["tenant_id"] != "t1" {
		t.Fatalf("tenant_id = %v, want t1", gotArgs["tenant_id"])
	}
	if _, ok := gotArgs["before"]; !ok {
		t.Fatalf("before missing from %v", gotArgs)
	}
}

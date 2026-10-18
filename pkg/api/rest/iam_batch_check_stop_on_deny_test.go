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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// stopOnDenyDeniedError is the denial the counting authorizer returns.
type stopOnDenyDeniedError struct{ resource string }

func (e *stopOnDenyDeniedError) Error() string { return "denied: " + e.resource }

// pepCountingAuthorizer denies the "deny" resource, allows every other one,
// and counts how many decisions it was asked for.
type pepCountingAuthorizer struct{ calls int }

func (a *pepCountingAuthorizer) Authorize(_ context.Context, _ *iam.Subject, resource, _ string) error {
	a.calls++
	if resource == "deny" {
		return &stopOnDenyDeniedError{resource: resource}
	}
	return nil
}

func TestBatchCheckAccess_StopOnDeny(t *testing.T) {
	tests := []struct {
		name        string
		stopOnDeny  bool
		wantAllowed []bool
	}{
		{name: "flag on stops after the first deny", stopOnDeny: true, wantAllowed: []bool{true, false}},
		{name: "flag off evaluates every check", stopOnDeny: false, wantAllowed: []bool{true, false, true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			authz := &pepCountingAuthorizer{}
			handler, cleanup := setupPolicyHandlerWithEngine(t, iam.NewPolicyEngine(iam.PolicyEngineOptions{
				Storage:        iam.NewMemoryStorage(),
				RBACAuthorizer: authz,
			}))
			defer cleanup()

			subject := iam.Subject{ID: "u1", TenantID: policyTenant, Authenticated: true}
			body := map[string]interface{}{
				"checks": []iam.CheckRequest{
					{Subject: subject, Action: "read", Resource: "allow-1"},
					{Subject: subject, Action: "read", Resource: "deny"},
					{Subject: subject, Action: "read", Resource: "allow-2"},
				},
				"stop_on_deny": tc.stopOnDeny,
			}
			w := httptest.NewRecorder()
			handler.BatchCheckAccess(w, policyRequest(t, http.MethodPost, "/api/v1/iam/check/batch", body, nil))
			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

			var resp struct {
				Results []iam.CheckResponse `json:"results"`
			}
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			allowed := make([]bool, 0, len(resp.Results))
			for _, r := range resp.Results {
				allowed = append(allowed, r.Allowed)
			}
			assert.Equal(t, tc.wantAllowed, allowed)
			assert.Equal(t, len(tc.wantAllowed), authz.calls)
		})
	}
}

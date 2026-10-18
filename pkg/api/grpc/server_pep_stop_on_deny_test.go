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

//go:build !integration

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// stopOnDenyDeniedError is the denial the counting authorizer returns.
type stopOnDenyDeniedError struct{ resource string }

func (e *stopOnDenyDeniedError) Error() string { return "denied: " + e.resource }

// pepCountingAuthorizer denies the "deny" resource, allows every other one, and
// counts how many decisions it was asked for.
type pepCountingAuthorizer struct{ calls int }

func (a *pepCountingAuthorizer) Authorize(_ context.Context, _ *iam.Subject, resource, _ string) error {
	a.calls++
	if resource == "deny" {
		return &stopOnDenyDeniedError{resource: resource}
	}
	return nil
}

func newStopOnDenyPEPServer(authz *pepCountingAuthorizer) *PEPServer {
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:        iam.NewMemoryStorage(),
		RBACAuthorizer: authz,
	})
	return NewPEPServer(NewPEPServiceAdapter(engine), newPEPTestLogger())
}

func TestPEPServer_CheckBatch_StopOnDeny(t *testing.T) {
	tests := []struct {
		name        string
		stopOnDeny  bool
		wantResults []bool
	}{
		{name: "flag on stops after the first deny", stopOnDeny: true, wantResults: []bool{true, false}},
		{name: "flag off evaluates every resource", stopOnDeny: false, wantResults: []bool{true, false, true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			authz := &pepCountingAuthorizer{}
			srv := newStopOnDenyPEPServer(authz)

			resp, err := srv.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
				Subject:    &iampb.Subject{Id: "user-1"},
				Action:     "read",
				Resources:  []string{"allow-1", "deny", "allow-2"},
				StopOnDeny: tc.stopOnDeny,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.wantResults, resp.Results)
			assert.Equal(t, len(tc.wantResults), authz.calls)
		})
	}
}

func TestPEPServiceAdapter_CheckBatch_StopOnDenyNoDeny(t *testing.T) {
	authz := &pepCountingAuthorizer{}
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:        iam.NewMemoryStorage(),
		RBACAuthorizer: authz,
	})
	adapter := NewPEPServiceAdapter(engine)

	results, _, err := adapter.CheckBatch(context.Background(), &iam.Subject{ID: "user-1"}, "read",
		[]string{"allow-1", "allow-2"}, nil, true)
	require.NoError(t, err)
	assert.Equal(t, []bool{true, true}, results)
	assert.Equal(t, 2, authz.calls)
}

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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/permission"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// getSpendReportMethod is the tenant spend report's gRPC method. The Unix
// transport serves the same service through the same interceptor.
const getSpendReportMethod = "/quicbaas.orchestrator.v1.OrchestratorService/GetSpendReport"

// TestGetSpendReport_RequiresSpendReportRead pins go-quicbaas#885: the spend
// report enumerates the tenant's whole attribution tree and account roster,
// so a principal holding only orchestrator:status (one session's status) is
// refused it, and a principal holding spend_report:read is served.
func TestGetSpendReport_RequiresSpendReportRead(t *testing.T) {
	cases := []struct {
		name    string
		granted string
		want    codes.Code
	}{
		{name: "session status alone is refused", granted: "orchestrator:status", want: codes.PermissionDenied},
		{name: "spend report read is served", granted: string(permission.SpendReportRead), want: codes.OK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authorizer := &fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: tc.granted,
				authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "t1"},
			}
			interceptor := UnaryAuthorizationInterceptor(authorizer)
			called := false
			handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }

			_, err := interceptor(ctxWithBearer("good"), nil,
				&grpc.UnaryServerInfo{FullMethod: getSpendReportMethod}, handler)

			assert.Equal(t, tc.want, status.Code(err))
			assert.Equal(t, tc.want == codes.OK, called)
			require.Equal(t, string(permission.SpendReportRead), authorizer.lastPerm,
				"the interceptor must ask for spend_report:read")
		})
	}
}

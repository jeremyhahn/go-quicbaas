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
	"errors"
	"fmt"
	"reflect"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

func TestRBACGRPCStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"rbac denial", &qrdbsdk.PermissionDeniedError{}, codes.PermissionDenied},
		{"wrapped rbac denial", fmt.Errorf("x: %w", &qrdbsdk.PermissionDeniedError{}), codes.PermissionDenied},
		{"abac denial", &qrdbsdk.ABACPolicyDeniedError{}, codes.PermissionDenied},
		{"wrapped abac denial", fmt.Errorf("x: %w", &qrdbsdk.ABACPolicyDeniedError{}), codes.PermissionDenied},
		{"node down", errors.New("node down"), codes.Unavailable},
		{"cancelled", context.Canceled, codes.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rbacGRPCStatus(tt.err)
			if got == nil {
				t.Fatal("rbacGRPCStatus returned nil: there is no allow path")
			}
			if code := status.Code(got); code != tt.want {
				t.Fatalf("code = %v, want %v", code, tt.want)
			}
		})
	}
}

func TestAuthorizedContextToQRDBIdentity(t *testing.T) {
	got := authorizedContextToQRDBIdentity(&platformtoken.AuthorizedContext{
		Subject:  "alice",
		TenantID: "t1",
		Roles:    []string{"r1"},
	})
	if got == nil {
		t.Fatal("identity is nil")
	}
	if got.Subject != "alice" || got.TenantID != "t1" || !reflect.DeepEqual(got.Roles, []string{"r1"}) {
		t.Fatalf("identity = %+v", got)
	}

	empty := authorizedContextToQRDBIdentity(nil)
	if empty == nil {
		t.Fatal("nil AuthorizedContext projected to a nil identity")
	}
	if empty.Subject != "" || empty.TenantID != "" || len(empty.Roles) != 0 {
		t.Fatalf("nil AuthorizedContext projected to %+v, want empty", empty)
	}
}

// recordingRBACAuthorizer is a GRPCRBACAuthorizer fake: it records every
// question the gate asks go-qrdb and answers with a configured error.
type recordingRBACAuthorizer struct {
	err      error
	denyPerm string // when set, only this "resource:action" is refused with err

	calls []rbacCall
}

type rbacCall struct {
	identity    *qrdbtransport.Identity
	resource    string
	action      string
	qrdbTenant  string
	tenantBound bool
}

func (r *recordingRBACAuthorizer) Authorize(ctx context.Context, identity *qrdbtransport.Identity, resource, action string) error {
	tenant, ok := qrdbsdk.TenantFromContext(ctx)
	r.calls = append(r.calls, rbacCall{identity: identity, resource: resource, action: action, qrdbTenant: tenant, tenantBound: ok})
	if r.denyPerm != "" && r.denyPerm != resource+":"+action {
		return nil
	}
	return r.err
}

const rbacTestMethod = "/quicbaas.v1.QuicBaaS/Put"

func rbacTestValidator() *fakeValidatorAuthorizer {
	return &fakeValidatorAuthorizer{
		token:       "tok-test",
		grantedPerm: "kv:put",
		authzCtx:    &platformtoken.AuthorizedContext{Subject: "alice", TenantID: "t1", Roles: []string{"r1"}, Scopes: []string{"kv:put"}},
	}
}

func runUnary(t *testing.T, interceptor grpc.UnaryServerInterceptor, ctx context.Context, method string, req interface{}) (bool, error) {
	t.Helper()
	reached := false
	_, err := interceptor(ctx, req, &grpc.UnaryServerInfo{FullMethod: method}, func(context.Context, interface{}) (interface{}, error) {
		reached = true
		return "ok", nil
	})
	return reached, err
}

func TestGRPCRBAC_AllowsOnlyWhenBothAllow(t *testing.T) {
	tests := []struct {
		name         string
		validatorOK  bool
		rbacErr      error
		wantReached  bool
		wantCode     codes.Code
		wantRBACCall bool
	}{
		{"validator allow, rbac allow", true, nil, true, codes.OK, true},
		{"validator allow, rbac denies", true, &qrdbsdk.PermissionDeniedError{}, false, codes.PermissionDenied, true},
		{"validator allow, rbac errors", true, errors.New("node down"), false, codes.Unavailable, true},
		{"validator deny, rbac allow", false, nil, false, codes.PermissionDenied, false},
		{"validator deny, rbac denies", false, &qrdbsdk.PermissionDeniedError{}, false, codes.PermissionDenied, false},
		{"validator deny, rbac errors", false, errors.New("node down"), false, codes.PermissionDenied, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := rbacTestValidator()
			if !tt.validatorOK {
				validator.grantedPerm = "kv:get"
			}
			rbac := &recordingRBACAuthorizer{err: tt.rbacErr}
			reached, err := runUnary(t, UnaryAuthorizationInterceptor(validator, WithRBACAuthorizer(rbac)), ctxWithBearer("tok-test"), rbacTestMethod, nil)
			if reached != tt.wantReached {
				t.Fatalf("handler reached = %v, want %v (err %v)", reached, tt.wantReached, err)
			}
			if code := status.Code(err); code != tt.wantCode {
				t.Fatalf("code = %v, want %v (err %v)", code, tt.wantCode, err)
			}
			if got := len(rbac.calls) > 0; got != tt.wantRBACCall {
				t.Fatalf("go-qrdb asked = %v, want %v", got, tt.wantRBACCall)
			}
		})
	}
}

func TestGRPCRBAC_AsksTheMappedPermission(t *testing.T) {
	rbac := &recordingRBACAuthorizer{}
	reached, err := runUnary(t, UnaryAuthorizationInterceptor(rbacTestValidator(), WithRBACAuthorizer(rbac)), ctxWithBearer("tok-test"), rbacTestMethod, nil)
	if err != nil || !reached {
		t.Fatalf("reached = %v, err = %v", reached, err)
	}
	if len(rbac.calls) != 1 {
		t.Fatalf("go-qrdb asked %d times, want 1", len(rbac.calls))
	}
	call := rbac.calls[0]
	if call.resource != "kv" || call.action != "put" {
		t.Fatalf("asked %s:%s, want kv:put", call.resource, call.action)
	}
	if call.identity.Subject != "alice" || call.identity.TenantID != "t1" || !reflect.DeepEqual(call.identity.Roles, []string{"r1"}) {
		t.Fatalf("identity = %+v", call.identity)
	}
	if !call.tenantBound || call.qrdbTenant != "t1" {
		t.Fatalf("go-qrdb tenant on context = %q (bound %v), want t1", call.qrdbTenant, call.tenantBound)
	}
}

func TestGRPCRBAC_PublicSealedAndIdentityGatedPathsDoNotAsk(t *testing.T) {
	rbac := &recordingRBACAuthorizer{err: &qrdbsdk.PermissionDeniedError{}}

	reached, err := runUnary(t, UnaryAuthorizationInterceptor(rbacTestValidator(), WithRBACAuthorizer(rbac)), context.Background(), "/quicbaas.v1.QuicBaaS/Health", nil)
	if err != nil || !reached {
		t.Fatalf("public method: reached = %v, err = %v", reached, err)
	}

	gated := &fakeAuthenticatingAuthorizer{fakeValidatorAuthorizer: *rbacTestValidator()}
	reached, err = runUnary(t, UnaryAuthorizationInterceptor(gated, WithRBACAuthorizer(rbac)), ctxWithBearer("tok-test"), "/qrdb.v1.QRDBService/BarrierShareClaimBegin", nil)
	if err != nil || !reached {
		t.Fatalf("identity-gated method: reached = %v, err = %v", reached, err)
	}

	if len(rbac.calls) != 0 {
		t.Fatalf("go-qrdb asked %d times on paths that carry no permission", len(rbac.calls))
	}
}

func TestGRPCRBAC_ArgumentPermissionsAsked(t *testing.T) {
	validator := &scopeSetAuthorizer{token: "tok-test", scp: map[string]bool{"users:create": true, "roles:assign": true}}
	req := &iampb.CreateUserRequest{Roles: []string{"admin"}}

	rbac := &recordingRBACAuthorizer{}
	reached, err := runUnary(t, UnaryAuthorizationInterceptor(validator, WithRBACAuthorizer(rbac)), ctxWithBearer("tok-test"), iampb.IAMService_CreateUser_FullMethodName, req)
	if err != nil || !reached {
		t.Fatalf("reached = %v, err = %v", reached, err)
	}
	var asked []string
	for _, c := range rbac.calls {
		asked = append(asked, c.resource+":"+c.action)
		if !c.tenantBound || c.qrdbTenant != "t1" {
			t.Fatalf("%s:%s asked without the caller's tenant (%q)", c.resource, c.action, c.qrdbTenant)
		}
	}
	if !reflect.DeepEqual(asked, []string{"users:create", "roles:assign"}) {
		t.Fatalf("go-qrdb asked %v, want [users:create roles:assign]", asked)
	}

	denying := &recordingRBACAuthorizer{err: &qrdbsdk.PermissionDeniedError{}, denyPerm: "roles:assign"}
	reached, err = runUnary(t, UnaryAuthorizationInterceptor(validator, WithRBACAuthorizer(denying)), ctxWithBearer("tok-test"), iampb.IAMService_CreateUser_FullMethodName, req)
	if reached {
		t.Fatal("handler reached although go-qrdb denied roles:assign")
	}
	if code := status.Code(err); code != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied", code)
	}
}

func TestGRPCRBAC_StreamAsks(t *testing.T) {
	for _, tt := range []struct {
		name    string
		rbacErr error
		want    codes.Code
	}{
		{"allow", nil, codes.OK},
		{"deny", &qrdbsdk.PermissionDeniedError{}, codes.PermissionDenied},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rbac := &recordingRBACAuthorizer{err: tt.rbacErr}
			interceptor := StreamAuthorizationInterceptor(rbacTestValidator(), WithRBACAuthorizer(rbac))
			reached := false
			err := interceptor(nil, &rbacTestStream{ctx: ctxWithBearer("tok-test")}, &grpc.StreamServerInfo{FullMethod: rbacTestMethod}, func(interface{}, grpc.ServerStream) error {
				reached = true
				return nil
			})
			if code := status.Code(err); code != tt.want {
				t.Fatalf("code = %v, want %v", code, tt.want)
			}
			if reached != (tt.want == codes.OK) {
				t.Fatalf("handler reached = %v", reached)
			}
			if len(rbac.calls) != 1 || rbac.calls[0].resource != "kv" || rbac.calls[0].action != "put" {
				t.Fatalf("go-qrdb calls = %+v, want one kv:put", rbac.calls)
			}
		})
	}
}

func TestGRPCRBAC_NoAuthorizerKeepsValidatorDecision(t *testing.T) {
	reached, err := runUnary(t, UnaryAuthorizationInterceptor(rbacTestValidator()), ctxWithBearer("tok-test"), rbacTestMethod, nil)
	if err != nil || !reached {
		t.Fatalf("validator allow: reached = %v, err = %v", reached, err)
	}

	denying := rbacTestValidator()
	denying.grantedPerm = "kv:get"
	reached, err = runUnary(t, UnaryAuthorizationInterceptor(denying), ctxWithBearer("tok-test"), rbacTestMethod, nil)
	if reached || status.Code(err) != codes.PermissionDenied {
		t.Fatalf("validator deny: reached = %v, err = %v", reached, err)
	}
}

// rbacTestStream is a minimal grpc.ServerStream carrying a context.
type rbacTestStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *rbacTestStream) Context() context.Context { return s.ctx }

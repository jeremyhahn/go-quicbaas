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

package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// fakeSessionServer implements SessionServiceServer.
type fakeSessionServer struct {
	iampb.UnimplementedSessionServiceServer
	fail bool
}

func (f *fakeSessionServer) err() error { return failErr(f.fail) }

func (f *fakeSessionServer) ListSessions(context.Context, *iampb.ListSessionsRequest) (*iampb.ListSessionsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.ListSessionsResponse{Sessions: []*iampb.Session{sampleSession()}, Total: 1}, nil
}
func (f *fakeSessionServer) GetSession(context.Context, *iampb.GetSessionRequest) (*iampb.Session, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleSession(), nil
}
func (f *fakeSessionServer) RevokeSession(context.Context, *iampb.RevokeSessionRequest) (*iampb.RevokeSessionResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.RevokeSessionResponse{}, nil
}
func (f *fakeSessionServer) RevokeAllSessions(context.Context, *iampb.RevokeAllSessionsRequest) (*iampb.RevokeAllSessionsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.RevokeAllSessionsResponse{}, nil
}
func (f *fakeSessionServer) RevokeOtherSessions(context.Context, *iampb.RevokeOtherSessionsRequest) (*iampb.RevokeOtherSessionsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.RevokeOtherSessionsResponse{}, nil
}

func sampleSession() *iampb.Session {
	return &iampb.Session{
		Id: "s1", UserId: "u1", TenantId: "t1", MfaVerified: true,
		IpAddress: "1.2.3.4", UserAgent: "agent", DeviceInfo: "dev",
		CreatedAt: iamTime(), LastActive: iamTime(), ExpiresAt: iamTime(),
	}
}

// fakeAuditServer implements AuditServiceServer.
type fakeAuditServer struct {
	iampb.UnimplementedAuditServiceServer
	fail bool
}

func (f *fakeAuditServer) err() error { return failErr(f.fail) }

func (f *fakeAuditServer) QueryAuditEvents(context.Context, *iampb.QueryAuditEventsRequest) (*iampb.QueryAuditEventsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.QueryAuditEventsResponse{Events: []*iampb.AuditEvent{sampleAuditEvent()}, Total: 1}, nil
}
func (f *fakeAuditServer) GetAuditEvent(context.Context, *iampb.GetAuditEventRequest) (*iampb.AuditEvent, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleAuditEvent(), nil
}

func sampleAuditEvent() *iampb.AuditEvent {
	return &iampb.AuditEvent{
		Id: "e1", TenantId: "t1", Action: "login", ActorId: "u1", ActorType: "user",
		IpAddress: "1.2.3.4", UserAgent: "agent", ResourceId: "r1",
		Details: map[string]string{"k": "v"}, Timestamp: iamTime(),
	}
}

// fakePEPServer implements PEPServiceServer.
type fakePEPServer struct {
	iampb.UnimplementedPEPServiceServer
	fail bool
}

func (f *fakePEPServer) err() error { return failErr(f.fail) }

func (f *fakePEPServer) Check(context.Context, *iampb.CheckRequest) (*iampb.CheckResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.CheckResponse{Allowed: true, MatchedPolicy: "p1", MatchedRule: "r1", EvaluationTimeNs: 10}, nil
}
func (f *fakePEPServer) CheckBatch(context.Context, *iampb.CheckBatchRequest) (*iampb.CheckBatchResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.CheckBatchResponse{Results: []bool{true, false}, EvaluationTimeNs: 20}, nil
}

// fakeMFAServer implements MFAServiceServer.
type fakeMFAServer struct {
	iampb.UnimplementedMFAServiceServer
	fail bool
}

func (f *fakeMFAServer) err() error { return failErr(f.fail) }

func (f *fakeMFAServer) BeginEnrollment(context.Context, *iampb.BeginMFAEnrollmentRequest) (*iampb.BeginMFAEnrollmentResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.BeginMFAEnrollmentResponse{EnrollmentId: "en1", Secret: "sec", ProvisioningUri: "uri", ExpiresAt: iamTime()}, nil
}
func (f *fakeMFAServer) CompleteEnrollment(context.Context, *iampb.CompleteMFAEnrollmentRequest) (*iampb.CompleteMFAEnrollmentResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.CompleteMFAEnrollmentResponse{Success: true, BackupCodes: []string{"a", "b"}}, nil
}
func (f *fakeMFAServer) CancelEnrollment(context.Context, *iampb.CancelMFAEnrollmentRequest) (*iampb.CancelMFAEnrollmentResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.CancelMFAEnrollmentResponse{Success: true}, nil
}
func (f *fakeMFAServer) BeginChallenge(context.Context, *iampb.BeginMFAChallengeRequest) (*iampb.BeginMFAChallengeResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.BeginMFAChallengeResponse{ChallengeId: "ch1", ExpiresAt: iamTime()}, nil
}
func (f *fakeMFAServer) VerifyChallenge(context.Context, *iampb.VerifyMFAChallengeRequest) (*iampb.VerifyMFAChallengeResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.VerifyMFAChallengeResponse{Success: true, SessionToken: "tok", VerifiedAt: iamTime()}, nil
}
func (f *fakeMFAServer) VerifyCode(context.Context, *iampb.VerifyMFACodeRequest) (*iampb.VerifyMFACodeResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.VerifyMFACodeResponse{Success: true}, nil
}
func (f *fakeMFAServer) GetStatus(context.Context, *iampb.GetMFAStatusRequest) (*iampb.GetMFAStatusResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.GetMFAStatusResponse{Enabled: true, BackupCodesRemaining: 5, EnrolledAt: iamTime(), LastVerified: iamTime()}, nil
}
func (f *fakeMFAServer) Disable(context.Context, *iampb.DisableMFARequest) (*iampb.DisableMFAResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.DisableMFAResponse{Success: true}, nil
}
func (f *fakeMFAServer) GenerateBackupCodes(context.Context, *iampb.GenerateBackupCodesRequest) (*iampb.GenerateBackupCodesResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.GenerateBackupCodesResponse{Codes: []string{"a", "b", "c"}}, nil
}
func (f *fakeMFAServer) VerifyBackupCode(context.Context, *iampb.VerifyBackupCodeRequest) (*iampb.VerifyBackupCodeResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.VerifyBackupCodeResponse{Success: true, RemainingCodes: 4}, nil
}
func (f *fakeMFAServer) GetBackupCodeStatus(context.Context, *iampb.GetBackupCodeStatusRequest) (*iampb.GetBackupCodeStatusResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.GetBackupCodeStatusResponse{TotalCodes: 10, RemainingCodes: 4, GeneratedAt: iamTime()}, nil
}

// newIAMMultiServiceHandler registers all IAM-related proto services on one
// in-memory server so the handler can route every IAM sub-service through a
// single connection.
func newIAMMultiServiceHandler(t *testing.T, fail bool) *iamHandler {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterIAMServiceServer(s, &fakeIAMServer{fail: fail})
		iampb.RegisterSessionServiceServer(s, &fakeSessionServer{fail: fail})
		iampb.RegisterAuditServiceServer(s, &fakeAuditServer{fail: fail})
		iampb.RegisterPEPServiceServer(s, &fakePEPServer{fail: fail})
		iampb.RegisterMFAServiceServer(s, &fakeMFAServer{fail: fail})
	})
	return newIAMHandler(srv.dial(t))
}

var iamExtraCases = []iamCase{
	// Session.
	{"ListSessions", "quicbaas.IAM.Session", "List", map[string]interface{}{"request": map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "limit": 10}}},
	{"GetSession", "quicbaas.IAM.Session", "Get", map[string]interface{}{"session_id": "s1"}},
	{"RevokeSession", "quicbaas.IAM.Session", "Revoke", map[string]interface{}{"session_id": "s1"}},
	{"RevokeAllSessions", "quicbaas.IAM.Session", "RevokeAll", map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}},
	{"RevokeOtherSessions", "quicbaas.IAM.Session", "RevokeOther", map[string]interface{}{"tenant_id": "t1", "keep_session_id": "s1"}},
	// Audit.
	{"QueryAudit", "quicbaas.IAM.Audit", "Query", map[string]interface{}{"filter": map[string]interface{}{"tenant_id": "t1", "limit": 10}}},
	{"GetAudit", "quicbaas.IAM.Audit", "Get", map[string]interface{}{"tenant_id": "t1", "event_id": "e1"}},
	// PEP.
	{"PEPCheck", "quicbaas.IAM.PEP", "Check", map[string]interface{}{"request": map[string]interface{}{"subject": map[string]interface{}{"id": "u1"}, "action": "read", "resource": "doc"}}},
	{"PEPCheckBatch", "quicbaas.IAM.PEP", "CheckBatch", map[string]interface{}{"request": map[string]interface{}{"requests": []interface{}{map[string]interface{}{"subject": map[string]interface{}{"id": "u1"}, "action": "read", "resource": "a"}, map[string]interface{}{"resource": "b"}}}}},
	// MFA.
	{"BeginMFAEnrollment", "quicbaas.IAM.MFA", "BeginEnrollment", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "method": 1}},
	{"CompleteMFAEnrollment", "quicbaas.IAM.MFA", "CompleteEnrollment", map[string]interface{}{"enrollment_id": "en1", "code": "123456"}},
	{"CancelMFAEnrollment", "quicbaas.IAM.MFA", "CancelEnrollment", map[string]interface{}{"enrollment_id": "en1"}},
	{"BeginMFAChallenge", "quicbaas.IAM.MFA", "BeginChallenge", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "session_token": "tok"}},
	{"VerifyMFAChallenge", "quicbaas.IAM.MFA", "VerifyChallenge", map[string]interface{}{"challenge_id": "ch1", "code": "123456"}},
	{"VerifyMFACode", "quicbaas.IAM.MFA", "VerifyCode", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "code": "123456"}},
	{"GetMFAStatus", "quicbaas.IAM.MFA", "GetStatus", map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}},
	{"DisableMFA", "quicbaas.IAM.MFA", "Disable", map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}},
	{"GenerateBackupCodes", "quicbaas.IAM.MFA", "GenerateBackupCodes", map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}},
	{"VerifyBackupCode", "quicbaas.IAM.MFA", "VerifyBackupCode", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "code": "abc"}},
	{"GetBackupCodeStatus", "quicbaas.IAM.MFA", "GetBackupCodeStatus", map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}},
}

func TestIAMExtraServices_Success(t *testing.T) {
	h := newIAMMultiServiceHandler(t, false)
	for _, tc := range iamExtraCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.handleRequest(context.Background(), tc.service, tc.method, tc.req)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
		})
	}
}

func TestIAMExtraServices_ServerError(t *testing.T) {
	h := newIAMMultiServiceHandler(t, true)
	for _, tc := range iamExtraCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.handleRequest(context.Background(), tc.service, tc.method, tc.req)
			if err == nil {
				t.Fatalf("%s: expected error from failing server", tc.name)
			}
		})
	}
}

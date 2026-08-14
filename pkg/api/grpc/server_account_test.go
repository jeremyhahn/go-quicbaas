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
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// Harness
// =============================================================================

func newAccountServer(svc service.AccountService) *AccountServer {
	return NewAccountServer(svc, createOrchestratorGRPCTestLogger())
}

func grpcSampleAccount() *orchestration.Account {
	return &orchestration.Account{
		ID:            "acct-1",
		TenantID:      "tenant-1",
		ProjectID:     "proj-1",
		Provider:      orchestration.AccountProviderClaude,
		DisplayName:   "alice-work",
		Default:       true,
		Status:        orchestration.AccountStatusActive,
		RefreshPolicy: orchestration.AccountRefreshOAuth2,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
}

func grpcSampleCredential() *pb.AccountCredential {
	return &pb.AccountCredential{
		AccessToken:       "SECRET-ACCESS-TOKEN",
		RefreshToken:      "SECRET-REFRESH-TOKEN",
		Env:               map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "SECRET-ENV-VALUE"},
		ExpiresAtUnix:     time.Now().Add(time.Hour).Unix(),
		AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
		Files:             map[string][]byte{".config/opencode/auth.json": []byte("SECRET-FILE-BYTES")},
	}
}

// =============================================================================
// Happy paths
// =============================================================================

func TestAccountServer_CreateAccount(t *testing.T) {
	var captured *service.CreateAccountRequest
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			captured = req
			return &service.CreateAccountResponse{Account: grpcSampleAccount(), Timestamp: time.Now().UTC()}, nil
		},
	}
	resp, err := newAccountServer(svc).CreateAccount(context.Background(), &pb.CreateAccountRequest{
		TenantId:      "tenant-1",
		ProjectId:     "proj-1",
		Provider:      "claude",
		DisplayName:   "alice-work",
		IsDefault:     true,
		RefreshPolicy: "oauth2",
		Credential:    grpcSampleCredential(),
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if resp.Account == nil || resp.Account.Id != "acct-1" {
		t.Fatalf("unexpected account: %+v", resp.Account)
	}
	if captured == nil || captured.Credential == nil {
		t.Fatal("credential did not reach the service layer")
	}
	if captured.Credential.AccessToken != "SECRET-ACCESS-TOKEN" ||
		captured.Credential.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "SECRET-ENV-VALUE" ||
		string(captured.Credential.Files[".config/opencode/auth.json"]) != "SECRET-FILE-BYTES" {
		t.Fatalf("credential fields lost in conversion: %+v", captured.Credential)
	}
	if captured.Credential.ExpiresAt.IsZero() {
		t.Error("expires_at lost in conversion")
	}
	if !captured.Default || captured.RefreshPolicy != orchestration.AccountRefreshOAuth2 {
		t.Errorf("metadata lost in conversion: %+v", captured)
	}
}

func TestAccountServer_ReadAndLifecycleVerbs(t *testing.T) {
	svc := &mocks.MockAccountService{
		GetAccountFunc: func(ctx context.Context, req *service.GetAccountRequest) (*service.GetAccountResponse, error) {
			return &service.GetAccountResponse{Account: grpcSampleAccount(), Timestamp: time.Now().UTC()}, nil
		},
		ListAccountsFunc: func(ctx context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			if req.Provider != orchestration.AccountProviderClaude || req.ProjectID != "proj-1" {
				t.Errorf("filters lost in conversion: %+v", req)
			}
			return &service.ListAccountsResponse{
				Accounts:  []*orchestration.Account{grpcSampleAccount()},
				Total:     1,
				Timestamp: time.Now().UTC(),
			}, nil
		},
		UpdateAccountFunc: func(ctx context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			if !req.ClearProjectID {
				t.Error("clear_project_id lost in conversion")
			}
			return &service.UpdateAccountResponse{Account: grpcSampleAccount(), Timestamp: time.Now().UTC()}, nil
		},
		DeleteAccountFunc: func(ctx context.Context, req *service.DeleteAccountRequest) (*service.DeleteAccountResponse, error) {
			return &service.DeleteAccountResponse{AccountID: req.AccountID, Deleted: true, Timestamp: time.Now().UTC()}, nil
		},
		RotateAccountCredentialFunc: func(ctx context.Context, req *service.RotateAccountCredentialRequest) (*service.RotateAccountCredentialResponse, error) {
			if req.Credential == nil || req.Credential.RefreshToken != "SECRET-REFRESH-TOKEN" {
				t.Errorf("rotate credential lost in conversion: %+v", req.Credential)
			}
			return &service.RotateAccountCredentialResponse{AccountID: req.AccountID, Timestamp: time.Now().UTC()}, nil
		},
		RevokeAccountFunc: func(ctx context.Context, req *service.RevokeAccountRequest) (*service.RevokeAccountResponse, error) {
			if req.Reason != "compromised" {
				t.Errorf("reason lost in conversion: %q", req.Reason)
			}
			revoked := grpcSampleAccount()
			revoked.Status = orchestration.AccountStatusRevoked
			return &service.RevokeAccountResponse{Account: revoked, Timestamp: time.Now().UTC()}, nil
		},
		SetDefaultAccountFunc: func(ctx context.Context, req *service.SetDefaultAccountRequest) (*service.SetDefaultAccountResponse, error) {
			return &service.SetDefaultAccountResponse{AccountID: req.AccountID, Timestamp: time.Now().UTC()}, nil
		},
		GetAccountUsageFunc: func(ctx context.Context, req *service.GetAccountUsageRequest) (*service.GetAccountUsageResponse, error) {
			return &service.GetAccountUsageResponse{
				Usage:     &orchestration.UsageReport{TotalTokens: 4242, ByAgent: map[string]int64{"a": 42}},
				Timestamp: time.Now().UTC(),
			}, nil
		},
	}
	server := newAccountServer(svc)
	ctx := context.Background()

	if _, err := server.GetAccount(ctx, &pb.GetAccountRequest{TenantId: "tenant-1", AccountId: "acct-1"}); err != nil {
		t.Errorf("GetAccount: %v", err)
	}
	listResp, err := server.ListAccounts(ctx, &pb.ListAccountsRequest{
		TenantId: "tenant-1", Provider: "claude", ProjectId: "proj-1",
	})
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if listResp.Total != 1 || len(listResp.Accounts) != 1 {
		t.Fatalf("unexpected list response: %+v", listResp)
	}
	if _, err := server.UpdateAccount(ctx, &pb.UpdateAccountRequest{
		TenantId: "tenant-1", AccountId: "acct-1", ClearProjectId: true,
	}); err != nil {
		t.Errorf("UpdateAccount: %v", err)
	}
	delResp, err := server.DeleteAccount(ctx, &pb.DeleteAccountRequest{TenantId: "tenant-1", AccountId: "acct-1"})
	if err != nil || !delResp.Deleted {
		t.Errorf("DeleteAccount: %v %+v", err, delResp)
	}
	if _, err := server.RotateAccountCredential(ctx, &pb.RotateAccountCredentialRequest{
		TenantId: "tenant-1", AccountId: "acct-1", Credential: grpcSampleCredential(),
	}); err != nil {
		t.Errorf("RotateAccountCredential: %v", err)
	}
	revokeResp, err := server.RevokeAccount(ctx, &pb.RevokeAccountRequest{
		TenantId: "tenant-1", AccountId: "acct-1", Reason: "compromised",
	})
	if err != nil {
		t.Fatalf("RevokeAccount: %v", err)
	}
	if revokeResp.Account.Status != string(orchestration.AccountStatusRevoked) {
		t.Errorf("revoked status lost: %+v", revokeResp.Account)
	}
	if _, err := server.SetDefaultAccount(ctx, &pb.SetDefaultAccountRequest{
		TenantId: "tenant-1", AccountId: "acct-1",
	}); err != nil {
		t.Errorf("SetDefaultAccount: %v", err)
	}
	usageResp, err := server.GetAccountUsage(ctx, &pb.GetAccountUsageRequest{TenantId: "tenant-1", AccountId: "acct-1"})
	if err != nil {
		t.Fatalf("GetAccountUsage: %v", err)
	}
	if usageResp.Usage == nil || usageResp.Usage.TotalTokens != 4242 || usageResp.Usage.ByAgent["a"] != 42 {
		t.Fatalf("usage lost in conversion: %+v", usageResp.Usage)
	}
}

// =============================================================================
// Invalid input
// =============================================================================

// TestAccountServer_InvalidInput proves every RPC validates before touching the
// service, and that a nil request is rejected rather than dereferenced.
func TestAccountServer_InvalidInput(t *testing.T) {
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			t.Fatal("service must not be called for an invalid request")
			return nil, nil
		},
	}
	server := newAccountServer(svc)
	ctx := context.Background()

	calls := map[string]func() error{
		"create nil": func() error { _, err := server.CreateAccount(ctx, nil); return err },
		"create no tenant": func() error {
			_, err := server.CreateAccount(ctx, &pb.CreateAccountRequest{Provider: "claude", Credential: grpcSampleCredential()})
			return err
		},
		"create no provider": func() error {
			_, err := server.CreateAccount(ctx, &pb.CreateAccountRequest{TenantId: "t", Credential: grpcSampleCredential()})
			return err
		},
		"create no credential": func() error {
			_, err := server.CreateAccount(ctx, &pb.CreateAccountRequest{TenantId: "t", Provider: "claude"})
			return err
		},
		"get nil":           func() error { _, err := server.GetAccount(ctx, nil); return err },
		"get no account":    func() error { _, err := server.GetAccount(ctx, &pb.GetAccountRequest{TenantId: "t"}); return err },
		"list nil":          func() error { _, err := server.ListAccounts(ctx, nil); return err },
		"list no tenant":    func() error { _, err := server.ListAccounts(ctx, &pb.ListAccountsRequest{}); return err },
		"update nil":        func() error { _, err := server.UpdateAccount(ctx, nil); return err },
		"update no account": func() error { _, err := server.UpdateAccount(ctx, &pb.UpdateAccountRequest{TenantId: "t"}); return err },
		"delete nil":        func() error { _, err := server.DeleteAccount(ctx, nil); return err },
		"delete no account": func() error { _, err := server.DeleteAccount(ctx, &pb.DeleteAccountRequest{TenantId: "t"}); return err },
		"rotate nil":        func() error { _, err := server.RotateAccountCredential(ctx, nil); return err },
		"rotate no credential": func() error {
			_, err := server.RotateAccountCredential(ctx, &pb.RotateAccountCredentialRequest{TenantId: "t", AccountId: "a"})
			return err
		},
		"revoke nil":        func() error { _, err := server.RevokeAccount(ctx, nil); return err },
		"revoke no account": func() error { _, err := server.RevokeAccount(ctx, &pb.RevokeAccountRequest{TenantId: "t"}); return err },
		"set-default nil":   func() error { _, err := server.SetDefaultAccount(ctx, nil); return err },
		"set-default no tenant": func() error {
			_, err := server.SetDefaultAccount(ctx, &pb.SetDefaultAccountRequest{AccountId: "a"})
			return err
		},
		"usage nil": func() error { _, err := server.GetAccountUsage(ctx, nil); return err },
		"usage no account": func() error {
			_, err := server.GetAccountUsage(ctx, &pb.GetAccountUsageRequest{TenantId: "t"})
			return err
		},
	}
	for name, call := range calls {
		err := call()
		if err == nil {
			t.Errorf("%s: expected an error", name)
			continue
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: code = %s, want InvalidArgument", name, status.Code(err))
		}
	}
}

// TestConvertAccountError maps each service-layer error class to its gRPC code.
func TestConvertAccountError(t *testing.T) {
	cases := []struct {
		err  error
		want codes.Code
	}{
		{&service.AccountNotFoundError{TenantID: "t", AccountID: "a"}, codes.NotFound},
		{&service.AccountInvalidError{Field: "f", Message: "m"}, codes.InvalidArgument},
		{&service.AccountUsageTrackerNotConfiguredError{}, codes.Unavailable},
		{status.Error(codes.Unknown, "other"), codes.Internal},
	}
	for _, tc := range cases {
		if got := status.Code(convertAccountError(tc.err)); got != tc.want {
			t.Errorf("convertAccountError(%v) = %s, want %s", tc.err, got, tc.want)
		}
	}
}

// =============================================================================
// Converters
// =============================================================================

// TestAccountToProto_ZeroTimestampsStayZero pins the unix-epoch trap: a
// never-set timestamp must serialize as 0, not as a huge negative number.
func TestAccountToProto_ZeroTimestampsStayZero(t *testing.T) {
	out := accountToProto(&orchestration.Account{ID: "a", Provider: orchestration.AccountProviderCodex})
	if out.CreatedAtUnix != 0 || out.LastUsedAtUnix != 0 || out.LastRefreshedAtUnix != 0 {
		t.Fatalf("zero times did not serialize as 0: %+v", out)
	}
	if accountToProto(nil) != nil {
		t.Error("accountToProto(nil) must be nil")
	}
	if accountCredentialFromProto(nil) != nil {
		t.Error("accountCredentialFromProto(nil) must be nil")
	}
	if accountUsageToProto(nil) != nil {
		t.Error("accountUsageToProto(nil) must be nil")
	}
}

// =============================================================================
// Credential leak guards
// =============================================================================

// TestAccountProtoResponsesCarryNoCredential is the structural guard: no
// AccountService response message may reach the AccountCredential message.
func TestAccountProtoResponsesCarryNoCredential(t *testing.T) {
	credentialType := reflect.TypeOf(pb.AccountCredential{})
	responses := []proto.Message{
		&pb.CreateAccountResponse{},
		&pb.GetAccountResponse{},
		&pb.ListAccountsResponse{},
		&pb.UpdateAccountResponse{},
		&pb.DeleteAccountResponse{},
		&pb.RotateAccountCredentialResponse{},
		&pb.RevokeAccountResponse{},
		&pb.SetDefaultAccountResponse{},
		&pb.GetAccountUsageResponse{},
	}
	for _, msg := range responses {
		rt := reflect.TypeOf(msg).Elem()
		if protoReachesType(rt, credentialType, make(map[reflect.Type]bool)) {
			t.Errorf("response message %s reaches %s", rt, credentialType)
		}
	}
}

// protoReachesType reports whether rt is, or transitively contains, target.
func protoReachesType(rt, target reflect.Type, seen map[reflect.Type]bool) bool {
	for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array || rt.Kind() == reflect.Map {
		rt = rt.Elem()
	}
	if rt == target {
		return true
	}
	if rt.Kind() != reflect.Struct || seen[rt] {
		return false
	}
	seen[rt] = true
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		// Skip protobuf-internal bookkeeping fields, which reference the
		// whole message registry and would make every type "reachable".
		if strings.HasPrefix(field.Name, "state") || strings.HasPrefix(field.Name, "sizeCache") ||
			strings.HasPrefix(field.Name, "unknownFields") {
			continue
		}
		if protoReachesType(field.Type, target, seen) {
			return true
		}
	}
	return false
}

// TestAccountGRPCResponsesSerializeNoCredential is the behavioral guard: the
// server is fed a fully populated credential and no response may serialize any
// of it.
func TestAccountGRPCResponsesSerializeNoCredential(t *testing.T) {
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			if req.Credential == nil || req.Credential.AccessToken == "" {
				t.Fatal("test setup: credential did not reach the service")
			}
			return &service.CreateAccountResponse{Account: grpcSampleAccount(), Timestamp: time.Now().UTC()}, nil
		},
		GetAccountFunc: func(ctx context.Context, req *service.GetAccountRequest) (*service.GetAccountResponse, error) {
			return &service.GetAccountResponse{Account: grpcSampleAccount(), Timestamp: time.Now().UTC()}, nil
		},
		ListAccountsFunc: func(ctx context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			return &service.ListAccountsResponse{Accounts: []*orchestration.Account{grpcSampleAccount()}, Total: 1}, nil
		},
		RotateAccountCredentialFunc: func(ctx context.Context, req *service.RotateAccountCredentialRequest) (*service.RotateAccountCredentialResponse, error) {
			return &service.RotateAccountCredentialResponse{AccountID: req.AccountID}, nil
		},
	}
	server := newAccountServer(svc)
	ctx := context.Background()

	createResp, err := server.CreateAccount(ctx, &pb.CreateAccountRequest{
		TenantId: "tenant-1", Provider: "claude", Credential: grpcSampleCredential(),
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	getResp, err := server.GetAccount(ctx, &pb.GetAccountRequest{TenantId: "tenant-1", AccountId: "acct-1"})
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	listResp, err := server.ListAccounts(ctx, &pb.ListAccountsRequest{TenantId: "tenant-1"})
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	rotateResp, err := server.RotateAccountCredential(ctx, &pb.RotateAccountCredentialRequest{
		TenantId: "tenant-1", AccountId: "acct-1", Credential: grpcSampleCredential(),
	})
	if err != nil {
		t.Fatalf("RotateAccountCredential: %v", err)
	}

	markers := []string{"SECRET-ACCESS-TOKEN", "SECRET-REFRESH-TOKEN", "SECRET-ENV-VALUE", "SECRET-FILE-BYTES"}
	for i, msg := range []proto.Message{createResp, getResp, listResp, rotateResp} {
		encoded, marshalErr := protojson.Marshal(msg)
		if marshalErr != nil {
			t.Fatalf("marshal response %d: %v", i, marshalErr)
		}
		for _, marker := range markers {
			if strings.Contains(string(encoded), marker) {
				t.Errorf("response %d leaked credential marker %q: %s", i, marker, encoded)
			}
		}
	}
}

// =============================================================================
// Authorization
// =============================================================================

// TestAccountGRPCMethodsAreMapped is the default-deny guard: an unmapped gRPC
// method is denied under RequireAuthz, so a missing mapping ships a dead RPC.
func TestAccountGRPCMethodsAreMapped(t *testing.T) {
	want := map[string][2]string{
		"CreateAccount":           {"credentials", "store"},
		"GetAccount":              {"credentials", "retrieve"},
		"ListAccounts":            {"credentials", "list"},
		"UpdateAccount":           {"credentials", "store"},
		"DeleteAccount":           {"credentials", "delete"},
		"RotateAccountCredential": {"credentials", "rotate"},
		"RevokeAccount":           {"credentials", "delete"},
		"SetDefaultAccount":       {"credentials", "store"},
		"GetAccountUsage":         {"credentials", "retrieve"},
	}
	for _, method := range pb.AccountService_ServiceDesc.Methods {
		full := "/" + pb.AccountService_ServiceDesc.ServiceName + "/" + method.MethodName
		perm, ok := grpcMethodPermissions[full]
		if !ok {
			t.Errorf("%s is unmapped and is therefore denied under RequireAuthz", full)
			continue
		}
		expected, declared := want[method.MethodName]
		if !declared {
			t.Errorf("%s has no expected permission in this test; add one", method.MethodName)
			continue
		}
		if perm.resource != expected[0] || perm.action != expected[1] {
			t.Errorf("%s -> %s:%s, want %s:%s", full, perm.resource, perm.action, expected[0], expected[1])
		}
	}
}

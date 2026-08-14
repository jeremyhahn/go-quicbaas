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
	"strings"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// accountServicePrefix is the SDK service name for pooled provider accounts.
const accountServicePrefix = "quicbaas.Account"

// isAccountService reports whether svc names the Account service.
func isAccountService(svc string) bool {
	return strings.HasPrefix(svc, accountServicePrefix)
}

// accountHandlerFunc converts an SDK request to proto, invokes gRPC, and
// converts the proto response back to the SDK type.
type accountHandlerFunc func(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error)

// accountMethodMap gives O(1) dispatch from SDK method name to handler.
var accountMethodMap = map[string]accountHandlerFunc{
	"Create":           handleAccountCreate,
	"Get":              handleAccountGet,
	"List":             handleAccountList,
	"Update":           handleAccountUpdate,
	"Delete":           handleAccountDelete,
	"RotateCredential": handleAccountRotateCredential,
	"Revoke":           handleAccountRevoke,
	"SetDefault":       handleAccountSetDefault,
	"Usage":            handleAccountUsage,
}

// accountHandler adapts the SDK account surface to the generated
// AccountService gRPC client.
type accountHandler struct {
	conn *grpc.ClientConn
}

// newAccountHandler creates an account gRPC handler bound to conn.
func newAccountHandler(conn *grpc.ClientConn) *accountHandler {
	return &accountHandler{conn: conn}
}

// handleRequest dispatches an account method.
func (h *accountHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	handler, ok := accountMethodMap[method]
	if !ok {
		return nil, errors.NewInvalidInput("grpc.account."+method, "unknown account method")
	}
	return handler(ctx, h.conn, req)
}

// =============================================================================
// Method handlers
// =============================================================================

func handleAccountCreate(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.account.Create"
	sdkReq, ok := req.(*account.CreateAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewAccountServiceClient(conn).CreateAccount(ctx, &pb.CreateAccountRequest{
		TenantId:      sdkReq.TenantID,
		ProjectId:     sdkReq.ProjectID,
		Provider:      string(sdkReq.Provider),
		DisplayName:   sdkReq.DisplayName,
		IsDefault:     sdkReq.Default,
		RefreshPolicy: string(sdkReq.RefreshPolicy),
		Credential:    accountCredentialToProto(sdkReq.Credential),
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return accountFromProto(resp.Account), nil
}

func handleAccountGet(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.account.Get"
	sdkReq, ok := req.(*account.GetAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewAccountServiceClient(conn).GetAccount(ctx, &pb.GetAccountRequest{
		TenantId:  sdkReq.TenantID,
		AccountId: sdkReq.AccountID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return accountFromProto(resp.Account), nil
}

func handleAccountList(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.account.List"
	sdkReq, ok := req.(*account.ListAccountsRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewAccountServiceClient(conn).ListAccounts(ctx, &pb.ListAccountsRequest{
		TenantId:  sdkReq.TenantID,
		Provider:  string(sdkReq.Provider),
		ProjectId: sdkReq.ProjectID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &account.ListAccountsResponse{
		Accounts: make([]*account.AccountInfo, 0, len(resp.Accounts)),
		Total:    int(resp.Total),
	}
	for _, a := range resp.Accounts {
		out.Accounts = append(out.Accounts, accountFromProto(a))
	}
	return out, nil
}

func handleAccountUpdate(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.account.Update"
	sdkReq, ok := req.(*account.UpdateAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewAccountServiceClient(conn).UpdateAccount(ctx, &pb.UpdateAccountRequest{
		TenantId:       sdkReq.TenantID,
		AccountId:      sdkReq.AccountID,
		DisplayName:    sdkReq.DisplayName,
		ProjectId:      sdkReq.ProjectID,
		ClearProjectId: sdkReq.ClearProjectID,
		RefreshPolicy:  string(sdkReq.RefreshPolicy),
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return accountFromProto(resp.Account), nil
}

func handleAccountDelete(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.account.Delete"
	sdkReq, ok := req.(*account.DeleteAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewAccountServiceClient(conn).DeleteAccount(ctx, &pb.DeleteAccountRequest{
		TenantId:  sdkReq.TenantID,
		AccountId: sdkReq.AccountID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &account.DeleteAccountResponse{AccountID: resp.AccountId, Deleted: resp.Deleted}, nil
}

func handleAccountRotateCredential(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.account.RotateCredential"
	sdkReq, ok := req.(*account.RotateCredentialRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewAccountServiceClient(conn).RotateAccountCredential(ctx, &pb.RotateAccountCredentialRequest{
		TenantId:   sdkReq.TenantID,
		AccountId:  sdkReq.AccountID,
		Credential: accountCredentialToProto(sdkReq.Credential),
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &account.AccountAckResponse{AccountID: resp.AccountId}, nil
}

func handleAccountRevoke(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.account.Revoke"
	sdkReq, ok := req.(*account.RevokeAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewAccountServiceClient(conn).RevokeAccount(ctx, &pb.RevokeAccountRequest{
		TenantId:  sdkReq.TenantID,
		AccountId: sdkReq.AccountID,
		Reason:    sdkReq.Reason,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return accountFromProto(resp.Account), nil
}

func handleAccountSetDefault(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.account.SetDefault"
	sdkReq, ok := req.(*account.SetDefaultAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewAccountServiceClient(conn).SetDefaultAccount(ctx, &pb.SetDefaultAccountRequest{
		TenantId:  sdkReq.TenantID,
		AccountId: sdkReq.AccountID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &account.AccountAckResponse{AccountID: resp.AccountId}, nil
}

func handleAccountUsage(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.account.Usage"
	sdkReq, ok := req.(*account.GetAccountUsageRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewAccountServiceClient(conn).GetAccountUsage(ctx, &pb.GetAccountUsageRequest{
		TenantId:  sdkReq.TenantID,
		AccountId: sdkReq.AccountID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	if resp.Usage == nil {
		return nil, errors.NewInternal(op, "server returned no usage report")
	}
	return &account.UsageReport{
		TotalTokens: resp.Usage.TotalTokens,
		BudgetLimit: resp.Usage.BudgetLimit,
		PercentUsed: resp.Usage.PercentUsed,
		ByAgent:     resp.Usage.ByAgent,
		LastUpdated: resp.Usage.LastUpdatedUnix,
	}, nil
}

// =============================================================================
// Converters
// =============================================================================

// accountCredentialToProto converts the outbound (request-only) credential
// envelope. There is deliberately no reverse converter: no response message
// carries a credential.
func accountCredentialToProto(c *account.Credential) *pb.AccountCredential {
	if c == nil {
		return nil
	}
	out := &pb.AccountCredential{
		Env:               c.Env,
		Files:             c.Files,
		AccessToken:       c.AccessToken,
		RefreshToken:      c.RefreshToken,
		TokenEndpoint:     c.TokenEndpoint,
		ClientId:          c.ClientID,
		Scopes:            c.Scopes,
		AccessTokenEnvKey: c.AccessTokenEnvKey,
	}
	if !c.ExpiresAt.IsZero() {
		out.ExpiresAtUnix = c.ExpiresAt.Unix()
	}
	return out
}

// accountFromProto converts the proto metadata record to the SDK type.
func accountFromProto(a *pb.Account) *account.AccountInfo {
	if a == nil {
		return nil
	}
	return &account.AccountInfo{
		ID:                  a.Id,
		TenantID:            a.TenantId,
		ProjectID:           a.ProjectId,
		Provider:            account.Provider(a.Provider),
		DisplayName:         a.DisplayName,
		Default:             a.IsDefault,
		Status:              account.Status(a.Status),
		RefreshPolicy:       account.RefreshPolicy(a.RefreshPolicy),
		AvailableAt:         timeFromUnix(a.AvailableAtUnix),
		CredentialExpiresAt: timeFromUnix(a.CredentialExpiresAtUnix),
		LastValidatedAt:     timeFromUnix(a.LastValidatedAtUnix),
		CredentialHealth:    account.CredentialHealth(a.CredentialHealth),
		CreatedAt:           timeFromUnix(a.CreatedAtUnix),
		UpdatedAt:           timeFromUnix(a.UpdatedAtUnix),
		LastUsedAt:          timeFromUnix(a.LastUsedAtUnix),
		LastRefreshedAt:     timeFromUnix(a.LastRefreshedAtUnix),
		LastError:           a.LastError,
	}
}

// timeFromUnix maps 0 back to the zero time so a never-set timestamp does not
// become 1970-01-01.
func timeFromUnix(seconds int64) time.Time {
	if seconds == 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

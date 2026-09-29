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
	stderrors "errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// AccountServer exposes the pooled provider account surface over gRPC (and,
// via the Unix domain socket transport, over Unix as well — the Unix
// transport is gRPC on a different listener).
//
// SECURITY INVARIANT: no method on this server ever populates a credential
// field on a response. accountToProto below constructs the proto Account from
// the metadata record only, and the proto Account message has no credential
// field to populate. Credential material travels inbound only, on
// CreateAccount and RotateAccountCredential.
type AccountServer struct {
	pb.UnimplementedAccountServiceServer
	accountSvc service.AccountService
	logger     *logger.Logger
}

// NewAccountServer creates a new gRPC server for pooled account operations.
func NewAccountServer(accountSvc service.AccountService, log *logger.Logger) *AccountServer {
	return &AccountServer{
		accountSvc: accountSvc,
		logger:     log.WithField("component", "account-grpc-server"),
	}
}

// CreateAccount handles the CreateAccount RPC.
func (s *AccountServer) CreateAccount(ctx context.Context, req *pb.CreateAccountRequest) (*pb.CreateAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	s.logger.WithField("provider", req.Provider).Debug("Processing CreateAccount request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.Provider == "" {
		return nil, status.Error(codes.InvalidArgument, "provider is required")
	}

	// Inline credential vs credential_secret_ref (exactly one) is the
	// service's rule, so every transport answers it identically.
	resp, err := s.accountSvc.CreateAccount(ctx, &service.CreateAccountRequest{
		TenantID:              req.TenantId,
		ProjectID:             req.ProjectId,
		Provider:              orchestration.AccountProvider(req.Provider),
		DisplayName:           req.DisplayName,
		Default:               req.IsDefault,
		RefreshPolicy:         orchestration.AccountRefreshPolicy(req.RefreshPolicy),
		Credential:            accountCredentialFromProto(req.Credential),
		CredentialSecretRef:   req.CredentialSecretRef,
		CredentialSecretFile:  req.CredentialSecretFile,
		MaxConcurrentSessions: int(req.MaxConcurrentSessions),
		Billing:               accountBillingFromProto(req.GetBilling()),
		Spend:                 accountSpendPolicyFromProto(req.GetSpend()),
	})
	if err != nil {
		return nil, convertAccountError(err)
	}

	return &pb.CreateAccountResponse{
		Account:   accountToProto(resp.Account),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GetAccount handles the GetAccount RPC.
func (s *AccountServer) GetAccount(ctx context.Context, req *pb.GetAccountRequest) (*pb.GetAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	s.logger.WithField("account_id", req.AccountId).Debug("Processing GetAccount request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}

	resp, err := s.accountSvc.GetAccount(ctx, &service.GetAccountRequest{
		TenantID:  req.TenantId,
		AccountID: req.AccountId,
	})
	if err != nil {
		return nil, convertAccountError(err)
	}

	return &pb.GetAccountResponse{
		Account:   accountToProto(resp.Account),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// ListAccounts handles the ListAccounts RPC.
func (s *AccountServer) ListAccounts(ctx context.Context, req *pb.ListAccountsRequest) (*pb.ListAccountsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	s.logger.WithField("tenant_id", req.TenantId).Debug("Processing ListAccounts request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	page, err := pageFromRPC(req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ())
	if err != nil {
		return nil, err
	}

	resp, err := s.accountSvc.ListAccounts(ctx, &service.ListAccountsRequest{
		TenantID:  req.TenantId,
		Provider:  orchestration.AccountProvider(req.Provider),
		ProjectID: req.ProjectId,
		Page:      page,
	})
	if err != nil {
		return nil, convertListError(err, convertAccountError)
	}

	accounts := make([]*pb.Account, 0, len(resp.Accounts))
	for _, a := range resp.Accounts {
		accounts = append(accounts, accountToProto(a))
	}

	return &pb.ListAccountsResponse{
		Accounts:   accounts,
		Total:      pageInt32(resp.Total),
		Timestamp:  resp.Timestamp.Unix(),
		Limit:      pageInt32(resp.Limit),
		Offset:     pageInt32(resp.Offset),
		NextOffset: pageInt32(resp.NextOffset),
	}, nil
}

// UpdateAccount handles the UpdateAccount RPC.
func (s *AccountServer) UpdateAccount(ctx context.Context, req *pb.UpdateAccountRequest) (*pb.UpdateAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	s.logger.WithField("account_id", req.AccountId).Debug("Processing UpdateAccount request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}

	resp, err := s.accountSvc.UpdateAccount(ctx, &service.UpdateAccountRequest{
		TenantID:       req.TenantId,
		AccountID:      req.AccountId,
		DisplayName:    req.DisplayName,
		ProjectID:      req.ProjectId,
		ClearProjectID: req.ClearProjectId,
		RefreshPolicy:  orchestration.AccountRefreshPolicy(req.RefreshPolicy),
		Billing:        optionalAccountBilling(req.GetBilling()),
		Spend:          optionalAccountSpendPolicy(req.GetSpend()),

		CredentialSecretRef:   req.CredentialSecretRef,
		CredentialSecretFile:  req.CredentialSecretFile,
		MaxConcurrentSessions: optionalInt(req.MaxConcurrentSessions),
	})
	if err != nil {
		return nil, convertAccountError(err)
	}

	return &pb.UpdateAccountResponse{
		Account:   accountToProto(resp.Account),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// DeleteAccount handles the DeleteAccount RPC.
func (s *AccountServer) DeleteAccount(ctx context.Context, req *pb.DeleteAccountRequest) (*pb.DeleteAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	s.logger.WithField("account_id", req.AccountId).Debug("Processing DeleteAccount request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}

	resp, err := s.accountSvc.DeleteAccount(ctx, &service.DeleteAccountRequest{
		TenantID:  req.TenantId,
		AccountID: req.AccountId,
	})
	if err != nil {
		return nil, convertAccountError(err)
	}

	return &pb.DeleteAccountResponse{
		AccountId: resp.AccountID,
		Deleted:   resp.Deleted,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// RotateAccountCredential handles the RotateAccountCredential RPC.
func (s *AccountServer) RotateAccountCredential(ctx context.Context, req *pb.RotateAccountCredentialRequest) (*pb.RotateAccountCredentialResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	s.logger.WithField("account_id", req.AccountId).Debug("Processing RotateAccountCredential request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}
	if req.Credential == nil {
		return nil, status.Error(codes.InvalidArgument, "credential is required")
	}

	resp, err := s.accountSvc.RotateAccountCredential(ctx, &service.RotateAccountCredentialRequest{
		TenantID:   req.TenantId,
		AccountID:  req.AccountId,
		Credential: accountCredentialFromProto(req.Credential),
	})
	if err != nil {
		return nil, convertAccountError(err)
	}

	return &pb.RotateAccountCredentialResponse{
		AccountId: resp.AccountID,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// RevokeAccount handles the RevokeAccount RPC.
func (s *AccountServer) RevokeAccount(ctx context.Context, req *pb.RevokeAccountRequest) (*pb.RevokeAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	s.logger.WithField("account_id", req.AccountId).Debug("Processing RevokeAccount request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}

	resp, err := s.accountSvc.RevokeAccount(ctx, &service.RevokeAccountRequest{
		TenantID:  req.TenantId,
		AccountID: req.AccountId,
		Reason:    req.Reason,
	})
	if err != nil {
		return nil, convertAccountError(err)
	}

	return &pb.RevokeAccountResponse{
		Account:   accountToProto(resp.Account),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// SetDefaultAccount handles the SetDefaultAccount RPC.
func (s *AccountServer) SetDefaultAccount(ctx context.Context, req *pb.SetDefaultAccountRequest) (*pb.SetDefaultAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	s.logger.WithField("account_id", req.AccountId).Debug("Processing SetDefaultAccount request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}

	resp, err := s.accountSvc.SetDefaultAccount(ctx, &service.SetDefaultAccountRequest{
		TenantID:  req.TenantId,
		AccountID: req.AccountId,
	})
	if err != nil {
		return nil, convertAccountError(err)
	}

	return &pb.SetDefaultAccountResponse{
		AccountId: resp.AccountID,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GetAccountUsage handles the GetAccountUsage RPC.
func (s *AccountServer) GetAccountUsage(ctx context.Context, req *pb.GetAccountUsageRequest) (*pb.GetAccountUsageResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	s.logger.WithField("account_id", req.AccountId).Debug("Processing GetAccountUsage request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}

	resp, err := s.accountSvc.GetAccountUsage(ctx, &service.GetAccountUsageRequest{
		TenantID:  req.TenantId,
		AccountID: req.AccountId,
	})
	if err != nil {
		return nil, convertAccountError(err)
	}

	return &pb.GetAccountUsageResponse{
		Usage:        accountUsageToProto(resp.Usage),
		Timestamp:    resp.Timestamp.Unix(),
		LiveSessions: int32(resp.LiveSessions),
		Spend:        spendHeadroomToProto(resp.Spend),
	}, nil
}

// spendHeadroomToProto renders the spend-gate verdict, leaving an absent one
// — no gate wired — unset.
func spendHeadroomToProto(h *orchestration.SpendHeadroom) *pb.SpendHeadroom {
	if h == nil {
		return nil
	}
	return &pb.SpendHeadroom{
		Admits:   h.Admits,
		Refusal:  h.Refusal,
		Window:   h.Window,
		Axis:     h.Axis,
		Used:     h.Used,
		Limit:    h.Limit,
		Failures: int32(h.Failures),
		FreesAt:  spendTimestamp(h.FreesAt),
	}
}

// optionalInt converts a proto3 optional int32 to the service's "nil means
// unchanged" pointer.
func optionalInt(v *int32) *int {
	if v == nil {
		return nil
	}
	out := int(*v)
	return &out
}

// =============================================================================
// Converters
// =============================================================================

// accountToProto converts an orchestration.Account metadata record to its
// proto representation. It reads only metadata fields; there is no path here
// through which credential material could reach the wire.
func accountToProto(a *orchestration.Account) *pb.Account {
	if a == nil {
		return nil
	}
	out := &pb.Account{
		Id:            a.ID,
		TenantId:      a.TenantID,
		ProjectId:     a.ProjectID,
		Provider:      string(a.Provider),
		DisplayName:   a.DisplayName,
		IsDefault:     a.Default,
		Status:        string(a.Status),
		RefreshPolicy: string(a.RefreshPolicy),
		LastError:     a.LastError,

		CredentialSecretRef:   a.CredentialSecretRef,
		CredentialSecretFile:  a.CredentialSecretFile,
		MaxConcurrentSessions: int32(a.MaxConcurrentSessions),
	}
	out.CreatedAtUnix = unixOrZero(a.CreatedAt)
	out.UpdatedAtUnix = unixOrZero(a.UpdatedAt)
	out.LastUsedAtUnix = unixOrZero(a.LastUsedAt)
	out.LastRefreshedAtUnix = unixOrZero(a.LastRefreshedAt)
	out.AvailableAtUnix = unixOrZero(a.AvailableAt)
	out.CredentialExpiresAtUnix = unixOrZero(a.CredentialExpiresAt)
	out.LastValidatedAtUnix = unixOrZero(a.LastValidatedAt)
	out.CredentialHealth = string(a.CredentialHealth())
	out.Spend = accountSpendPolicyToProto(a.Spend)
	if a.Billing != (orchestration.AccountBilling{}) {
		out.Billing = &pb.AccountBilling{Mode: string(a.Billing.Mode), MonthlyFeeMicros: a.Billing.MonthlyFeeMicros}
	}
	return out
}

// accountBillingFromProto converts a billing message; an absent one is "not
// configured".
func accountBillingFromProto(b *pb.AccountBilling) orchestration.AccountBilling {
	return orchestration.AccountBilling{
		Mode:             orchestration.BillingMode(b.GetMode()),
		MonthlyFeeMicros: b.GetMonthlyFeeMicros(),
	}
}

// optionalAccountBilling converts an update's billing to the service's "nil
// means unchanged" pointer.
func optionalAccountBilling(b *pb.AccountBilling) *orchestration.AccountBilling {
	if b == nil {
		return nil
	}
	billing := accountBillingFromProto(b)
	return &billing
}

// unixOrZero renders a time as Unix seconds, mapping the zero time to 0 so a
// never-set timestamp is not serialized as a large negative number.
func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// accountCredentialFromProto converts an inbound proto credential envelope to
// the orchestration type. Inbound only — the reverse converter deliberately
// does not exist.
func accountCredentialFromProto(c *pb.AccountCredential) *orchestration.AccountCredential {
	if c == nil {
		return nil
	}
	cred := &orchestration.AccountCredential{
		AccessToken:       c.AccessToken,
		RefreshToken:      c.RefreshToken,
		TokenEndpoint:     c.TokenEndpoint,
		ClientID:          c.ClientId,
		Scopes:            c.Scopes,
		AccessTokenEnvKey: c.AccessTokenEnvKey,
	}
	if c.ExpiresAtUnix > 0 {
		cred.ExpiresAt = time.Unix(c.ExpiresAtUnix, 0).UTC()
	}
	if len(c.Env) > 0 {
		cred.Env = make(map[string]string, len(c.Env))
		for k, v := range c.Env {
			cred.Env[k] = v
		}
	}
	if len(c.Files) > 0 {
		cred.Files = make(map[string][]byte, len(c.Files))
		for k, v := range c.Files {
			cred.Files[k] = v
		}
	}
	return cred
}

// accountUsageToProto converts an orchestration.UsageReport to its proto form.
func accountUsageToProto(u *orchestration.UsageReport) *pb.AccountUsage {
	if u == nil {
		return nil
	}
	out := &pb.AccountUsage{
		TotalTokens:     u.TotalTokens,
		BudgetLimit:     u.BudgetLimit,
		PercentUsed:     u.PercentUsed,
		LastUpdatedUnix: u.LastUpdated,
	}
	if len(u.ByAgent) > 0 {
		out.ByAgent = make(map[string]int64, len(u.ByAgent))
		for k, v := range u.ByAgent {
			out.ByAgent[k] = v
		}
	}
	return out
}

// convertAccountError maps service-layer account errors to gRPC status codes.
func convertAccountError(err error) error {
	var notFound *service.AccountNotFoundError
	var invalid *service.AccountInvalidError
	var noTracker *service.AccountUsageTrackerNotConfiguredError
	var notReleasable *service.AccountNotReleasableError
	var projectScope *service.AccountProjectScopeError
	var noTokens *service.AccountTokenManagerNotConfiguredError
	var secretUnresolved *orchestration.AccountSecretRefUnresolvedError
	var noSecretResolver *orchestration.AccountSecretResolverNotConfiguredError
	var noReserver *service.AccountSessionReserverNotConfiguredError

	switch {
	case stderrors.As(err, &notFound):
		return status.Error(codes.NotFound, err.Error())
	case stderrors.Is(err, orchestration.ErrAccountSessionCountChanged):
		// Aborted: an optimistic claim lost its race; the caller re-selects.
		return status.Error(codes.Aborted, err.Error())
	case stderrors.Is(err, orchestration.ErrAccountsSaturated):
		// ResourceExhausted: the account is at its session cap. It frees
		// when a session ends; the caller is not wrong, the account is busy.
		return status.Error(codes.ResourceExhausted, err.Error())
	case stderrors.As(err, &invalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case stderrors.As(err, &notReleasable):
		// FailedPrecondition, not PermissionDenied: the caller is entitled to
		// this account and the account exists — it is the account's STATE that
		// refuses, and for a limit hold that state clears on its own.
		return status.Error(codes.FailedPrecondition, err.Error())
	case stderrors.As(err, &secretUnresolved):
		// FailedPrecondition for the same reason: the account exists, and the
		// tenant secret it names does not resolve.
		return status.Error(codes.FailedPrecondition, err.Error())
	case stderrors.As(err, &projectScope):
		return status.Error(codes.PermissionDenied, err.Error())
	case stderrors.As(err, &noTracker), stderrors.As(err, &noTokens), stderrors.As(err, &noReserver), stderrors.As(err, &noSecretResolver):
		return status.Error(codes.Unavailable, err.Error())
	case stderrors.Is(err, orchestration.ErrAccountVersionConflict):
		// Aborted: a concurrent write changed the account under this one — a
		// rewrite that kept losing, or an inline rotation overtaken by a switch
		// to a secret reference. Nothing was written; the caller may retry.
		return status.Error(codes.Aborted, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// Compile-time interface satisfaction check.
var _ pb.AccountServiceServer = (*AccountServer)(nil)

// =============================================================================
// Credential materialization
//
// THE ONE RPC ON THIS SERVICE WHOSE RESPONSE CARRIES SECRET MATERIAL. The
// invariant stated at the top of this file still holds for every other method;
// this one is the deliberate, separately-permissioned exception that lets a
// pooled credential reach the worker host running the agent
// (go-quicbaas#734).
// =============================================================================

// MaterializeAccountCredential handles the MaterializeAccountCredential RPC.
//
// Tenancy is the CALLER'S OWN: the tenant is read off the authenticated
// principal, never off the request, and a tenant_id naming anything else is
// refused. That is what makes the release tenant-scoped in a deployment where
// the caller is a daemon holding one narrow grant rather than an operator.
func (s *AccountServer) MaterializeAccountCredential(
	ctx context.Context,
	req *pb.MaterializeAccountCredentialRequest,
) (*pb.MaterializeAccountCredentialResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}
	s.logger.WithField("account_id", req.AccountId).Debug("Processing MaterializeAccountCredential request")

	subject, err := callerTenant(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}

	resp, err := s.accountSvc.MaterializeAccountCredential(ctx, &service.MaterializeAccountCredentialRequest{
		TenantID:  subject.TenantID,
		AccountID: req.AccountId,
		ProjectID: req.ProjectId,
		JobID:     req.JobId,
		LeaseID:   req.LeaseId,
		AgentID:   subject.ID,
	})
	if err != nil {
		return nil, convertAccountError(err)
	}

	return &pb.MaterializeAccountCredentialResponse{
		AccountId:           resp.AccountID,
		Provider:            string(resp.Provider),
		DisplayName:         resp.DisplayName,
		ProjectId:           resp.ProjectID,
		RefreshPolicy:       string(resp.RefreshPolicy),
		Credential:          materializedCredentialToProto(resp.Credential),
		ExpiresAtUnix:       unixOrZero(resp.ExpiresAt),
		UpdatedAtUnix:       unixOrZero(resp.UpdatedAt),
		LastRefreshedAtUnix: unixOrZero(resp.LastRefreshedAt),
		LeaseId:             resp.LeaseID,
		LeaseExpiresAtUnix:  unixOrZero(resp.LeaseExpiresAt),
		SingleUse:           resp.SingleUse,
		Timestamp:           resp.Timestamp.Unix(),
	}, nil
}

// materializedCredentialToProto projects a released envelope onto the wire.
//
// The refresh grant (refresh_token / token_endpoint / client_id) is
// DELIBERATELY ABSENT from the proto message this builds — renewal already
// happened on the central, and a recipient that must not refresh must not
// receive the means to.
func materializedCredentialToProto(c *orchestration.AccountCredential) *pb.MaterializedAccountCredential {
	if c == nil {
		return nil
	}
	out := &pb.MaterializedAccountCredential{
		AccessToken:       c.AccessToken,
		ExpiresAtUnix:     unixOrZero(c.ExpiresAt),
		Scopes:            c.Scopes,
		AccessTokenEnvKey: c.AccessTokenEnvKey,
		Generation:        c.Generation,
	}
	if len(c.Env) > 0 {
		out.Env = make(map[string]string, len(c.Env))
		for k, v := range c.Env {
			out.Env[k] = v
		}
	}
	if len(c.Files) > 0 {
		out.Files = make(map[string][]byte, len(c.Files))
		for k, v := range c.Files {
			out.Files[k] = v
		}
	}
	return out
}

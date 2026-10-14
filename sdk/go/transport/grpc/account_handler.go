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
	"math"
	"strconv"
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
	"Materialize":      handleAccountMaterialize,
	"ReserveSession":   handleAccountReserveSession,
	"ReleaseSession":   handleAccountReleaseSession,
}

// handleAccountReserveSession claims one session slot on an account.
func handleAccountReserveSession(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.account.ReserveSession"
	sdkReq, ok := req.(*account.ReserveSessionRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	pbReq := &pb.ReserveAccountSessionRequest{TenantId: sdkReq.TenantID, AccountId: sdkReq.AccountID}
	if sdkReq.ExpectedLive != nil {
		expected := int32(*sdkReq.ExpectedLive)
		pbReq.ExpectedLive = &expected
	}
	resp, err := pb.NewAccountServiceClient(conn).ReserveAccountSession(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &account.SessionReservation{ReservationID: resp.ReservationId}, nil
}

// handleAccountReleaseSession gives one reservation back.
func handleAccountReleaseSession(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.account.ReleaseSession"
	sdkReq, ok := req.(*account.ReleaseSessionRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewAccountServiceClient(conn).ReleaseAccountSession(ctx, &pb.ReleaseAccountSessionRequest{
		TenantId: sdkReq.TenantID, AccountId: sdkReq.AccountID, ReservationId: sdkReq.ReservationID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &account.SessionReservation{ReservationID: resp.ReservationId}, nil
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
	sessionCap, err := sessionCapToProto(op, sdkReq.MaxConcurrentSessions)
	if err != nil {
		return nil, err
	}
	resp, err := pb.NewAccountServiceClient(conn).CreateAccount(ctx, &pb.CreateAccountRequest{
		TenantId:      sdkReq.TenantID,
		ProjectId:     sdkReq.ProjectID,
		Provider:      string(sdkReq.Provider),
		DisplayName:   sdkReq.DisplayName,
		IsDefault:     sdkReq.Default,
		RefreshPolicy: string(sdkReq.RefreshPolicy),
		Billing:       accountBillingToProto(sdkReq.Billing),
		Spend:         accountSpendPolicyToProto(sdkReq.Spend),
		Credential:    accountCredentialToProto(sdkReq.Credential),

		CredentialSecretRef:   sdkReq.CredentialSecretRef,
		CredentialSecretFile:  sdkReq.CredentialSecretFile,
		MaxConcurrentSessions: sessionCap,
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
	paging := pageFieldsFromParams(sdkReq.Params)
	resp, err := pb.NewAccountServiceClient(conn).ListAccounts(ctx, &pb.ListAccountsRequest{
		TenantId:  sdkReq.TenantID,
		Provider:  string(sdkReq.Provider),
		ProjectId: sdkReq.ProjectID,
		Limit:     paging.limit,
		Offset:    paging.offset,
		Sort:      paging.sort,
		Q:         paging.q,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &account.ListAccountsResponse{
		Accounts: make([]*account.AccountInfo, 0, len(resp.Accounts)),
		Envelope: pageEnvelopeFromProto(resp.GetTotal(), resp.GetLimit(), resp.GetOffset(), resp.GetNextOffset()),
	}
	out.Facets = facetsFromProto(resp.GetFacets())
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
	var sessionCap *int32
	if sdkReq.MaxConcurrentSessions != nil {
		checked, capErr := sessionCapToProto(op, *sdkReq.MaxConcurrentSessions)
		if capErr != nil {
			return nil, capErr
		}
		sessionCap = &checked
	}
	resp, err := pb.NewAccountServiceClient(conn).UpdateAccount(ctx, &pb.UpdateAccountRequest{
		TenantId:       sdkReq.TenantID,
		AccountId:      sdkReq.AccountID,
		DisplayName:    sdkReq.DisplayName,
		ProjectId:      sdkReq.ProjectID,
		ClearProjectId: sdkReq.ClearProjectID,
		RefreshPolicy:  string(sdkReq.RefreshPolicy),
		Billing:        accountBillingToProto(sdkReq.Billing),
		Spend:          accountSpendPolicyToProto(sdkReq.Spend),

		CredentialSecretRef:   sdkReq.CredentialSecretRef,
		CredentialSecretFile:  sdkReq.CredentialSecretFile,
		MaxConcurrentSessions: sessionCap,
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
		TotalTokens:  resp.Usage.TotalTokens,
		BudgetLimit:  resp.Usage.BudgetLimit,
		PercentUsed:  resp.Usage.PercentUsed,
		ByAgent:      resp.Usage.ByAgent,
		LastUpdated:  resp.Usage.LastUpdatedUnix,
		LiveSessions: int(resp.LiveSessions),
		Spend:        spendHeadroomFromProto(resp.GetSpend()),
	}, nil
}

// spendHeadroomFromProto converts the spend-gate verdict, leaving an absent
// one — a server that wires no gate — nil.
func spendHeadroomFromProto(h *pb.SpendHeadroom) *account.SpendHeadroom {
	if h == nil {
		return nil
	}
	out := &account.SpendHeadroom{
		Admits:   h.GetAdmits(),
		Refusal:  h.GetRefusal(),
		Window:   h.GetWindow(),
		Axis:     h.GetAxis(),
		Used:     h.GetUsed(),
		Limit:    h.GetLimit(),
		Failures: int(h.GetFailures()),
	}
	if at := h.GetFreesAt(); at != nil {
		freesAt := at.AsTime()
		out.FreesAt = &freesAt
	}
	return out
}

// sessionCapToProto narrows a session cap to the wire's int32, refusing one
// it cannot carry.
//
// AN UNCHECKED NARROWING IS A SILENT UNCAP. On a 64-bit client 4294967296
// narrows to 0, which the server stores as unmetered, and a negative value can
// wrap past the server's own check. So the range is enforced here, before the
// value is converted.
func sessionCapToProto(op string, v int) (int32, error) {
	if v < 0 || v > math.MaxInt32 {
		return 0, errors.NewInvalidInput(op, "max_concurrent_sessions must be between 0 (unmetered) and "+
			strconv.Itoa(math.MaxInt32)+", got "+strconv.Itoa(v))
	}
	return int32(v), nil
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

// accountBillingToProto converts an SDK billing; nil stays absent, which on an
// update leaves the account's billing unchanged.
func accountBillingToProto(b *account.Billing) *pb.AccountBilling {
	if b == nil {
		return nil
	}
	return &pb.AccountBilling{Mode: string(b.Mode), MonthlyFeeMicros: b.MonthlyFeeMicros}
}

// accountBillingFromProto converts a record's billing; absent is nil.
func accountBillingFromProto(b *pb.AccountBilling) *account.Billing {
	if b == nil {
		return nil
	}
	return &account.Billing{Mode: account.BillingMode(b.GetMode()), MonthlyFeeMicros: b.GetMonthlyFeeMicros()}
}

// accountFromProto converts the proto metadata record to the SDK type.
func accountFromProto(a *pb.Account) *account.AccountInfo {
	if a == nil {
		return nil
	}
	return &account.AccountInfo{
		ID:                    a.Id,
		TenantID:              a.TenantId,
		ProjectID:             a.ProjectId,
		Provider:              account.Provider(a.Provider),
		DisplayName:           a.DisplayName,
		Default:               a.IsDefault,
		Status:                account.Status(a.Status),
		RefreshPolicy:         account.RefreshPolicy(a.RefreshPolicy),
		CredentialSecretRef:   a.CredentialSecretRef,
		CredentialSecretFile:  a.CredentialSecretFile,
		AvailableAt:           timeFromUnix(a.AvailableAtUnix),
		CredentialExpiresAt:   timeFromUnix(a.CredentialExpiresAtUnix),
		LastValidatedAt:       timeFromUnix(a.LastValidatedAtUnix),
		CredentialHealth:      account.CredentialHealth(a.CredentialHealth),
		CreatedAt:             timeFromUnix(a.CreatedAtUnix),
		UpdatedAt:             timeFromUnix(a.UpdatedAtUnix),
		LastUsedAt:            timeFromUnix(a.LastUsedAtUnix),
		LastRefreshedAt:       timeFromUnix(a.LastRefreshedAtUnix),
		LastError:             a.LastError,
		MaxConcurrentSessions: int(a.MaxConcurrentSessions),
		Billing:               accountBillingFromProto(a.GetBilling()),
		Spend:                 accountSpendPolicyFromProto(a.GetSpend()),
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

// handleAccountMaterialize releases one account's credential envelope.
//
// It is the ONE handler in this file whose response carries secret material.
// The proto message it decodes deliberately has no refresh-grant fields, so
// the SDK type it builds cannot carry one either.
func handleAccountMaterialize(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.account.Materialize"
	sdkReq, ok := req.(*account.MaterializeCredentialRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewAccountServiceClient(conn).MaterializeAccountCredential(ctx,
		&pb.MaterializeAccountCredentialRequest{
			TenantId:  sdkReq.TenantID,
			AccountId: sdkReq.AccountID,
			ProjectId: sdkReq.ProjectID,
			JobId:     sdkReq.JobID,
			LeaseId:   sdkReq.LeaseID,
		})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &account.MaterializedCredential{
		AccountID:       resp.AccountId,
		Provider:        account.Provider(resp.Provider),
		DisplayName:     resp.DisplayName,
		ProjectID:       resp.ProjectId,
		RefreshPolicy:   account.RefreshPolicy(resp.RefreshPolicy),
		Credential:      releasedCredentialFromProto(resp.Credential),
		ExpiresAt:       timeFromUnix(resp.ExpiresAtUnix),
		UpdatedAt:       timeFromUnix(resp.UpdatedAtUnix),
		LastRefreshedAt: timeFromUnix(resp.LastRefreshedAtUnix),
		LeaseID:         resp.LeaseId,
		LeaseExpiresAt:  timeFromUnix(resp.LeaseExpiresAtUnix),
		SingleUse:       resp.SingleUse,
	}, nil
}

// releasedCredentialFromProto converts a released envelope to the SDK type.
func releasedCredentialFromProto(c *pb.MaterializedAccountCredential) *account.ReleasedCredential {
	if c == nil {
		return nil
	}
	out := &account.ReleasedCredential{
		AccessToken:       c.AccessToken,
		ExpiresAt:         timeFromUnix(c.ExpiresAtUnix),
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

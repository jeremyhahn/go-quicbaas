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

package embedded

import (
	"context"
	"reflect"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// =============================================================================
// Pooled provider accounts — in-process handlers.
//
// SECURITY INVARIANT: the converters below read only orchestration.Account
// metadata. accountCredentialToOrchestration is the sole INBOUND credential
// converter.
//
// There is exactly one outbound counterpart, releasedCredentialFromOrchestration,
// and it serves exactly one handler: Materialize, which releases a pooled
// credential to the caller so it can reach the worker host that runs the agent
// (go-quicbaas#734). It deliberately drops the refresh grant, so even the
// in-process caller cannot obtain the long-lived half of a credential through
// this verb.
// =============================================================================

// accountServiceName is the transport service key for account handlers.
const accountServiceName = "quicbaas.Account"

// accountService resolves the AccountService, returning an UnknownMethodError
// (the transport's "not available here" signal) when it is not configured.
func accountService(svc service.QuicBaaS, method string) (service.AccountService, error) {
	accountSvc := svc.Account()
	if accountSvc == nil {
		return nil, NewUnknownMethodError(accountServiceName, method)
	}
	return accountSvc, nil
}

func handleAccountCreate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*accountSDK.CreateAccountRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(accountServiceName, "Create",
			"*account.CreateAccountRequest", reflect.TypeOf(req).String())
	}
	accountSvc, err := accountService(svc, "Create")
	if err != nil {
		return nil, err
	}
	resp, err := accountSvc.CreateAccount(ctx, &service.CreateAccountRequest{
		TenantID:      r.TenantID,
		ProjectID:     r.ProjectID,
		Provider:      orchestration.AccountProvider(r.Provider),
		DisplayName:   r.DisplayName,
		Default:       r.Default,
		RefreshPolicy: orchestration.AccountRefreshPolicy(r.RefreshPolicy),
		Credential:    accountCredentialToOrchestration(r.Credential),

		CredentialSecretRef:  r.CredentialSecretRef,
		CredentialSecretFile: r.CredentialSecretFile,
	})
	if err != nil {
		return nil, err
	}
	return accountInfoFromOrchestration(resp.Account), nil
}

func handleAccountGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*accountSDK.GetAccountRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(accountServiceName, "Get",
			"*account.GetAccountRequest", reflect.TypeOf(req).String())
	}
	accountSvc, err := accountService(svc, "Get")
	if err != nil {
		return nil, err
	}
	resp, err := accountSvc.GetAccount(ctx, &service.GetAccountRequest{
		TenantID:  r.TenantID,
		AccountID: r.AccountID,
	})
	if err != nil {
		return nil, err
	}
	return accountInfoFromOrchestration(resp.Account), nil
}

func handleAccountList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*accountSDK.ListAccountsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(accountServiceName, "List",
			"*account.ListAccountsRequest", reflect.TypeOf(req).String())
	}
	accountSvc, err := accountService(svc, "List")
	if err != nil {
		return nil, err
	}
	resp, err := accountSvc.ListAccounts(ctx, &service.ListAccountsRequest{
		TenantID:  r.TenantID,
		Status:    orchestration.AccountStatus(r.Status),
		Provider:  orchestration.AccountProvider(r.Provider),
		ProjectID: r.ProjectID,
		Page:      pageFromSDK(r.Params),
	})
	if err != nil {
		return nil, err
	}
	out := &accountSDK.ListAccountsResponse{
		Accounts: make([]*accountSDK.AccountInfo, 0, len(resp.Accounts)),
		Envelope: page.Envelope{
			Total:      resp.Total,
			Limit:      resp.Limit,
			Offset:     resp.Offset,
			NextOffset: resp.NextOffset,
			Facets:     resp.Facets,
		},
	}
	for _, a := range resp.Accounts {
		out.Accounts = append(out.Accounts, accountInfoFromOrchestration(a))
	}
	return out, nil
}

func handleAccountUpdate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*accountSDK.UpdateAccountRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(accountServiceName, "Update",
			"*account.UpdateAccountRequest", reflect.TypeOf(req).String())
	}
	accountSvc, err := accountService(svc, "Update")
	if err != nil {
		return nil, err
	}
	resp, err := accountSvc.UpdateAccount(ctx, &service.UpdateAccountRequest{
		TenantID:       r.TenantID,
		AccountID:      r.AccountID,
		DisplayName:    r.DisplayName,
		ProjectID:      r.ProjectID,
		ClearProjectID: r.ClearProjectID,
		RefreshPolicy:  orchestration.AccountRefreshPolicy(r.RefreshPolicy),

		CredentialSecretRef:  r.CredentialSecretRef,
		CredentialSecretFile: r.CredentialSecretFile,
	})
	if err != nil {
		return nil, err
	}
	return accountInfoFromOrchestration(resp.Account), nil
}

func handleAccountDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*accountSDK.DeleteAccountRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(accountServiceName, "Delete",
			"*account.DeleteAccountRequest", reflect.TypeOf(req).String())
	}
	accountSvc, err := accountService(svc, "Delete")
	if err != nil {
		return nil, err
	}
	resp, err := accountSvc.DeleteAccount(ctx, &service.DeleteAccountRequest{
		TenantID:  r.TenantID,
		AccountID: r.AccountID,
	})
	if err != nil {
		return nil, err
	}
	return &accountSDK.DeleteAccountResponse{AccountID: resp.AccountID, Deleted: resp.Deleted}, nil
}

func handleAccountRotateCredential(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*accountSDK.RotateCredentialRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(accountServiceName, "RotateCredential",
			"*account.RotateCredentialRequest", reflect.TypeOf(req).String())
	}
	accountSvc, err := accountService(svc, "RotateCredential")
	if err != nil {
		return nil, err
	}
	resp, err := accountSvc.RotateAccountCredential(ctx, &service.RotateAccountCredentialRequest{
		TenantID:   r.TenantID,
		AccountID:  r.AccountID,
		Credential: accountCredentialToOrchestration(r.Credential),
	})
	if err != nil {
		return nil, err
	}
	return &accountSDK.AccountAckResponse{AccountID: resp.AccountID}, nil
}

func handleAccountRevoke(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*accountSDK.RevokeAccountRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(accountServiceName, "Revoke",
			"*account.RevokeAccountRequest", reflect.TypeOf(req).String())
	}
	accountSvc, err := accountService(svc, "Revoke")
	if err != nil {
		return nil, err
	}
	resp, err := accountSvc.RevokeAccount(ctx, &service.RevokeAccountRequest{
		TenantID:  r.TenantID,
		AccountID: r.AccountID,
		Reason:    r.Reason,
	})
	if err != nil {
		return nil, err
	}
	return accountInfoFromOrchestration(resp.Account), nil
}

func handleAccountSetDefault(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*accountSDK.SetDefaultAccountRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(accountServiceName, "SetDefault",
			"*account.SetDefaultAccountRequest", reflect.TypeOf(req).String())
	}
	accountSvc, err := accountService(svc, "SetDefault")
	if err != nil {
		return nil, err
	}
	resp, err := accountSvc.SetDefaultAccount(ctx, &service.SetDefaultAccountRequest{
		TenantID:  r.TenantID,
		AccountID: r.AccountID,
	})
	if err != nil {
		return nil, err
	}
	return &accountSDK.AccountAckResponse{AccountID: resp.AccountID}, nil
}

func handleAccountUsage(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*accountSDK.GetAccountUsageRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(accountServiceName, "Usage",
			"*account.GetAccountUsageRequest", reflect.TypeOf(req).String())
	}
	accountSvc, err := accountService(svc, "Usage")
	if err != nil {
		return nil, err
	}
	resp, err := accountSvc.GetAccountUsage(ctx, &service.GetAccountUsageRequest{
		TenantID:  r.TenantID,
		AccountID: r.AccountID,
	})
	if err != nil {
		return nil, err
	}
	if resp.Usage == nil {
		return &accountSDK.UsageReport{}, nil
	}
	return &accountSDK.UsageReport{
		TotalTokens: resp.Usage.TotalTokens,
		BudgetLimit: resp.Usage.BudgetLimit,
		PercentUsed: resp.Usage.PercentUsed,
		ByAgent:     resp.Usage.ByAgent,
		LastUpdated: resp.Usage.LastUpdated,
	}, nil
}

// =============================================================================
// Converters
// =============================================================================

// accountCredentialToOrchestration converts the inbound SDK credential
// envelope. Inbound only — there is no reverse converter by design.
func accountCredentialToOrchestration(c *accountSDK.Credential) *orchestration.AccountCredential {
	if c == nil {
		return nil
	}
	return &orchestration.AccountCredential{
		Env:               c.Env,
		Files:             c.Files,
		AccessToken:       c.AccessToken,
		RefreshToken:      c.RefreshToken,
		ExpiresAt:         c.ExpiresAt,
		TokenEndpoint:     c.TokenEndpoint,
		ClientID:          c.ClientID,
		Scopes:            c.Scopes,
		AccessTokenEnvKey: c.AccessTokenEnvKey,
	}
}

// accountInfoFromOrchestration converts an account metadata record to the SDK
// type. It touches metadata fields only.
func accountInfoFromOrchestration(a *orchestration.Account) *accountSDK.AccountInfo {
	if a == nil {
		return nil
	}
	return &accountSDK.AccountInfo{
		ID:                   a.ID,
		TenantID:             a.TenantID,
		ProjectID:            a.ProjectID,
		Provider:             accountSDK.Provider(a.Provider),
		DisplayName:          a.DisplayName,
		Default:              a.Default,
		Status:               accountSDK.Status(a.Status),
		RefreshPolicy:        accountSDK.RefreshPolicy(a.RefreshPolicy),
		CredentialSecretRef:  a.CredentialSecretRef,
		CredentialSecretFile: a.CredentialSecretFile,
		AvailableAt:          a.AvailableAt,
		CredentialExpiresAt:  a.CredentialExpiresAt,
		LastValidatedAt:      a.LastValidatedAt,
		CredentialHealth:     accountSDK.CredentialHealth(a.CredentialHealth()),
		CreatedAt:            a.CreatedAt,
		UpdatedAt:            a.UpdatedAt,
		LastUsedAt:           a.LastUsedAt,
		LastRefreshedAt:      a.LastRefreshedAt,
		LastError:            a.LastError,
	}
}

// handleAccountMaterialize releases one account's credential envelope
// in-process.
//
// The embedded transport has no wire and no separate principal: the caller IS
// the process holding the pool. Tenancy therefore comes from the request, as
// it does for every other embedded handler, and the release rules the service
// layer enforces (revoked / errored / limit-held refused, project fence
// honoured, one audit event) are what make the verb narrow here too.
func handleAccountMaterialize(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*accountSDK.MaterializeCredentialRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(accountServiceName, "Materialize",
			"*account.MaterializeCredentialRequest", reflect.TypeOf(req).String())
	}
	accountSvc, err := accountService(svc, "Materialize")
	if err != nil {
		return nil, err
	}
	resp, err := accountSvc.MaterializeAccountCredential(ctx, &service.MaterializeAccountCredentialRequest{
		TenantID:  r.TenantID,
		AccountID: r.AccountID,
		ProjectID: r.ProjectID,
		JobID:     r.JobID,
		LeaseID:   r.LeaseID,
	})
	if err != nil {
		return nil, err
	}
	return &accountSDK.MaterializedCredential{
		AccountID:       resp.AccountID,
		Provider:        accountSDK.Provider(resp.Provider),
		DisplayName:     resp.DisplayName,
		ProjectID:       resp.ProjectID,
		RefreshPolicy:   accountSDK.RefreshPolicy(resp.RefreshPolicy),
		Credential:      releasedCredentialFromOrchestration(resp.Credential),
		ExpiresAt:       resp.ExpiresAt,
		UpdatedAt:       resp.UpdatedAt,
		LastRefreshedAt: resp.LastRefreshedAt,
		LeaseID:         resp.LeaseID,
		LeaseExpiresAt:  resp.LeaseExpiresAt,
		SingleUse:       resp.SingleUse,
	}, nil
}

// releasedCredentialFromOrchestration converts a released envelope to the SDK
// type, dropping the refresh grant (refresh token, token endpoint, client id).
func releasedCredentialFromOrchestration(c *orchestration.AccountCredential) *accountSDK.ReleasedCredential {
	if c == nil {
		return nil
	}
	return &accountSDK.ReleasedCredential{
		Env:               c.Env,
		Files:             c.Files,
		AccessToken:       c.AccessToken,
		ExpiresAt:         c.ExpiresAt,
		Scopes:            c.Scopes,
		AccessTokenEnvKey: c.AccessTokenEnvKey,
	}
}

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

// ---------------------------------------------------------------------------
// Pooled provider account domain types.
//
// Mirrors sdk/go/client/account/client.go and the REST surface at
// pkg/api/rest/handlers_account.go (/api/v1/agent-accounts).
//
// SECURITY INVARIANT: AccountCredential is a REQUEST-ONLY shape. It is a
// field of CreateAccountRequest and RotateAccountCredentialRequest and of no
// response type — no transport ever returns credential material, so it must
// never be rendered or logged by a caller of this client.
// ---------------------------------------------------------------------------

/** AccountProvider identifies which CLI/backend a pooled account authenticates. */
export type AccountProvider = 'claude' | 'codex' | 'opencode';

/** AccountStatus is the lifecycle state of a pooled account. */
export type AccountStatus = 'active' | 'revoked' | 'error';

/**
 * AccountRefreshPolicy selects how a pooled account's credential is kept
 * usable. "oauth_refresh" is renewed via an RFC 6749 refresh_token grant;
 * "static_token" is a long-lived credential QuicBaaS never renews.
 */
export type AccountRefreshPolicy = 'oauth_refresh' | 'static_token';

/**
 * AccountCredential is the secret envelope for one account. REQUEST-ONLY:
 * accepted by createAccount and rotateAccountCredential, never returned.
 *
 * refresh_token is REQUIRED for the "oauth_refresh" policy and FORBIDDEN for
 * "static_token" — the server rejects a static_token account that carries
 * one. Callers building the form must omit the field entirely rather than
 * send it empty.
 */
export interface AccountCredential {
  /** env holds static environment variables injected into the spawned subprocess verbatim. */
  env?: Record<string, string>;
  /** files holds provider config file bytes (base64) keyed by a relative path. */
  files?: Record<string, string>;
  /** access_token is the bearer credential handed to the spawn. */
  access_token?: string;
  /** refresh_token is redeemed by the oauth_refresh policy. Forbidden for static_token. */
  refresh_token?: string;
  /** expires_at is when access_token stops working (ISO 8601). */
  expires_at?: string;
  /** token_endpoint is the RFC 6749 endpoint used to refresh access_token. Required for oauth_refresh. */
  token_endpoint?: string;
  /** client_id is the OAuth2 client identifier sent with the refresh grant. */
  client_id?: string;
  /** scopes records the scopes the current access token was issued with. */
  scopes?: string[];
  /** access_token_env_key is the environment variable access_token is projected into. */
  access_token_env_key?: string;
}

/** Account is the non-secret metadata record for one pooled provider account. */
export interface Account {
  id: string;
  tenant_id: string;
  /** project_id optionally scopes the account to one project. Absent means tenant-wide. */
  project_id?: string;
  provider: AccountProvider;
  display_name?: string;
  /** default marks this account the tenant+provider (+project) fallback. */
  default: boolean;
  status: AccountStatus;
  refresh_policy: AccountRefreshPolicy;
  created_at: string;
  updated_at: string;
  last_used_at?: string;
  last_refreshed_at?: string;
  /** last_error carries the most recent failed-refresh message when status is "error". */
  last_error?: string;
}

/** CreateAccountRequest provisions a new pooled account. */
export interface CreateAccountRequest {
  /** tenant_id owns the account. Required. */
  tenant_id: string;
  /** project_id optionally scopes the account to one project. */
  project_id?: string;
  /** provider identifies the backend that consumes the credential. Required. */
  provider: AccountProvider;
  display_name?: string;
  /** default marks the account as the tenant+provider fallback. */
  default?: boolean;
  /** refresh_policy selects how the credential is renewed. Required. */
  refresh_policy: AccountRefreshPolicy;
  /** credential is the initial secret envelope. Required; never echoed back. */
  credential: AccountCredential;
}

/** CreateAccountResponse is the server response after account creation. */
export interface CreateAccountResponse {
  account: Account;
}

/** GetAccountResponse is the server response for an account lookup. */
export interface GetAccountResponse {
  account: Account;
}

/** ListAccountsRequest selects which accounts to list. */
export interface ListAccountsRequest {
  /** tenant_id whose accounts to list. Required. */
  tenant_id: string;
  /** provider filters to one provider when set. */
  provider?: AccountProvider;
  /** project_id filters to one project when set. */
  project_id?: string;
}

/** ListAccountsResponse contains the matching accounts. */
export interface ListAccountsResponse {
  accounts: Account[];
  total: number;
}

/** UpdateAccountRequest changes non-secret metadata. Empty fields are left unchanged. */
export interface UpdateAccountRequest {
  /** tenant_id identifies the owning tenant. Required. */
  tenant_id: string;
  /** account_id is the account to update. Required. */
  account_id: string;
  display_name?: string;
  project_id?: string;
  /** clear_project_id removes the project scope when true. */
  clear_project_id?: boolean;
  refresh_policy?: AccountRefreshPolicy;
}

/** UpdateAccountResponse is the server response after an account update. */
export interface UpdateAccountResponse {
  account: Account;
}

/** DeleteAccountResponse confirms account deletion. */
export interface DeleteAccountResponse {
  account_id: string;
  deleted: boolean;
}

/**
 * RotateAccountCredentialRequest supplies a replacement credential envelope.
 * The envelope must match the account's existing refresh_policy — rotating
 * cannot change the policy itself; use updateAccount for that.
 */
export interface RotateAccountCredentialRequest {
  tenant_id: string;
  account_id: string;
  credential: AccountCredential;
}

/** RotateAccountCredentialResponse acknowledges the rotation. */
export interface RotateAccountCredentialResponse {
  account_id: string;
}

/** RevokeAccountRequest identifies an account to revoke. */
export interface RevokeAccountRequest {
  tenant_id: string;
  account_id: string;
  reason?: string;
}

/** RevokeAccountResponse is the server response after revocation. */
export interface RevokeAccountResponse {
  account: Account;
}

/** SetDefaultAccountResponse acknowledges the default assignment. */
export interface SetDefaultAccountResponse {
  account_id: string;
}

/** AccountUsage is the cumulative token usage recorded against an account. */
export interface AccountUsage {
  total_tokens: number;
  budget_limit: number;
  percent_used: number;
  by_agent?: Record<string, number>;
  last_updated: number;
}

/** GetAccountUsageResponse carries an account's usage report. */
export interface GetAccountUsageResponse {
  usage: AccountUsage;
}

// ---------------------------------------------------------------------------
// AccountsClient interface
// ---------------------------------------------------------------------------

/**
 * AccountsClient exposes pooled provider account lifecycle management.
 * Maps to /api/v1/agent-accounts REST endpoints.
 */
export interface AccountsClient {
  /**
   * createAccount provisions a new pooled account and stores its initial
   * credential envelope.
   * POST /api/v1/agent-accounts
   */
  createAccount(req: CreateAccountRequest): Promise<CreateAccountResponse>;

  /**
   * getAccount retrieves account metadata by ID.
   * GET /api/v1/agent-accounts/:accountId?tenant_id=...
   */
  getAccount(tenantId: string, accountId: string): Promise<GetAccountResponse>;

  /**
   * listAccounts returns the tenant's accounts, optionally filtered by
   * provider and/or project.
   * GET /api/v1/agent-accounts?tenant_id=...
   */
  listAccounts(req: ListAccountsRequest): Promise<ListAccountsResponse>;

  /**
   * updateAccount changes an account's non-secret metadata.
   * PUT /api/v1/agent-accounts/:accountId?tenant_id=...
   */
  updateAccount(req: UpdateAccountRequest): Promise<UpdateAccountResponse>;

  /**
   * deleteAccount permanently removes an account and its credential envelope.
   * DELETE /api/v1/agent-accounts/:accountId?tenant_id=...
   */
  deleteAccount(tenantId: string, accountId: string): Promise<DeleteAccountResponse>;

  /**
   * rotateAccountCredential replaces an account's stored credential envelope.
   * POST /api/v1/agent-accounts/:accountId/rotate?tenant_id=...
   */
  rotateAccountCredential(req: RotateAccountCredentialRequest): Promise<RotateAccountCredentialResponse>;

  /**
   * revokeAccount marks an account revoked, excluding it from every
   * selection strategy.
   * POST /api/v1/agent-accounts/:accountId/revoke?tenant_id=...
   */
  revokeAccount(req: RevokeAccountRequest): Promise<RevokeAccountResponse>;

  /**
   * setDefaultAccount marks an account the tenant (or tenant+project)
   * default for its provider.
   * POST /api/v1/agent-accounts/:accountId/default?tenant_id=...
   */
  setDefaultAccount(tenantId: string, accountId: string): Promise<SetDefaultAccountResponse>;

  /**
   * getAccountUsage reports cumulative token usage recorded against an account.
   * GET /api/v1/agent-accounts/:accountId/usage?tenant_id=...
   */
  getAccountUsage(tenantId: string, accountId: string): Promise<GetAccountUsageResponse>;
}

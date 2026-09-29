import type { AccountSpendPolicy } from './spend.js';
import type { AccountPageParams, PageResult } from './page.js';
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
/**
 * AccountBilling is how a pooled account is billed, which decides the billed
 * figure every turn on it carries beside its list-price equivalent.
 */
export interface AccountBilling {
    /** mode is 'subscription' (a turn adds nothing to the bill) or 'api' (a turn bills its list price). */
    mode?: 'subscription' | 'api';
    /** monthly_fee_micros is a subscription's flat monthly fee in millionths of a US dollar. */
    monthly_fee_micros?: number;
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
    /** billing is how the account is billed; absent when not configured. */
    billing?: AccountBilling;
    /** spend is what the account may spend and how failures take it out of service; absent is unmetered. */
    spend?: AccountSpendPolicy;
    /**
     * credential_secret_ref names the tenant secret the credential is read from
     * on every spawn, when the account is reference-sourced. A name, never the value.
     */
    credential_secret_ref?: string;
    /** credential_secret_file is where a referenced secret is materialized instead of the access token. */
    credential_secret_file?: string;
    created_at: string;
    updated_at: string;
    last_used_at?: string;
    last_refreshed_at?: string;
    /** last_error carries the most recent failed-refresh message when status is "error". */
    last_error?: string;
    /** max_concurrent_sessions caps the account's live sessions; absent or 0 is unmetered. */
    max_concurrent_sessions?: number;
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
    /** billing is how the account is billed; absent leaves it not configured. */
    billing?: AccountBilling;
    /** spend is the account's spend policy; absent is unmetered. */
    spend?: AccountSpendPolicy;
    /**
     * credential is the initial secret envelope, held inline; never echoed back.
     * Exactly one of credential and credential_secret_ref is required.
     */
    credential?: AccountCredential;
    /**
     * credential_secret_ref names the tenant secret (short name, e.g.
     * "agents/claude/claude1/oauth_token") the credential is read from on every
     * spawn. Writing the secret rotates the account with no account write.
     */
    credential_secret_ref?: string;
    /** credential_secret_file, with credential_secret_ref, materializes the secret as this file (e.g. ".codex/auth.json"). */
    credential_secret_file?: string;
    /** max_concurrent_sessions caps the account's live sessions; 0 is unmetered. */
    max_concurrent_sessions?: number;
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
    /** billing, when present, replaces how the account is billed; an empty object clears it. */
    billing?: AccountBilling;
    /** spend, when present, replaces the spend policy; an empty object makes the account unmetered. */
    spend?: AccountSpendPolicy;
    /**
     * credential_secret_ref switches the account to (or re-points it at) a tenant
     * secret; re-applying the current one returns an account whose secret was
     * missing to service.
     */
    credential_secret_ref?: string;
    credential_secret_file?: string;
    /** max_concurrent_sessions replaces the session cap (0 is unmetered); omit to leave it unchanged. */
    max_concurrent_sessions?: number;
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
/**
 * SpendHeadroom is the spend gate's read-only verdict on one account: whether
 * its own spend policy admits more work and, if not, why and until when.
 */
export interface SpendHeadroom {
    admits: boolean;
    /** refusal is 'quota_exhausted' or 'breaker_open' when admits is false. */
    refusal?: string;
    window?: string;
    axis?: string;
    used?: number;
    limit?: number;
    failures?: number;
    /** frees_at (RFC 3339) is when the refusal lifts. */
    frees_at?: string;
}
/** GetAccountUsageResponse carries an account's usage report. */
export interface GetAccountUsageResponse {
    usage: AccountUsage;
    /** live_sessions is how many sessions the account is serving now. */
    live_sessions: number;
    /** spend is the spend gate's verdict; absent when the server wires no gate. */
    spend?: SpendHeadroom;
}
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
     *
     * ONE PAGE OF THEM since the endpoint gained paging: it sends no `limit`
     * and the server's default page is 25. Use listAccountsPage for the count
     * and the page after. (Its `total` has always been the number that
     * matched, which is now the number BEFORE the page was cut.)
     */
    listAccounts(req: ListAccountsRequest): Promise<ListAccountsResponse>;
    /**
     * listAccountsPage returns ONE PAGE of the tenant's accounts, newest first,
     * with the count of everything that matched.
     * GET /api/v1/agent-accounts?tenant_id=...&limit=&offset=&sort=&q=
     */
    listAccountsPage(req: AccountPageParams & {
        tenant_id: string;
    }): Promise<PageResult<Account>>;
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
//# sourceMappingURL=accounts.d.ts.map
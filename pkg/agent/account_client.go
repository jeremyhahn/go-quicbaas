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

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// AccountAPIClient — the daemon's read side of the tenant's CENTRAL pooled
// provider account pool.
//
// The pool itself lives on the control plane (one pool per tenant, rotated
// there, audited there). This client is how a worker host reaches it: the same
// mTLS channel and the same bearer token the heartbeat already uses, against
// four routes on /api/v1/agent-accounts.
//
// It is deliberately a THIN client and not a second SDK. The SDK's account
// client (sdk/go/client/account) speaks the same verbs, but it is built for a
// caller that constructs a full SDK Transport; the daemon already holds a
// configured *http.Client (mTLS, the control plane's CA) and a TokenSource,
// and threading those into a second stack would mean two places to keep the
// daemon's credentials correct. See go-quicbaas#734.
//
// NOTHING HERE EVER LOGS A CREDENTIAL. The materialize response is decoded
// into a value and handed to the caller; it is never formatted into an error,
// never written to disk by this file, and the only field of it this file
// mentions in a message is the account ID.
// =============================================================================

// accountAPIBasePath is the control plane's pooled-account REST mount. It
// mirrors pkg/api/rest/routes.go.
const accountAPIBasePath = "/api/v1/agent-accounts"

// accountAPITimeout bounds one control-plane call. It matches the heartbeat's
// per-attempt bound: both run on the same channel to the same host, and a
// spawn blocked on a stuck control plane is no more tolerable than a stuck
// heartbeat.
const accountAPITimeout = 15 * time.Second

// AccountAPIClientConfig bundles the client's dependencies.
type AccountAPIClientConfig struct {
	// ServerAddr is the base URL of the control plane (e.g. https://host:8443).
	ServerAddr string

	// TenantID is the tenant whose pool this daemon reads. It is the daemon's
	// OWN tenant: the control plane binds tenancy from the authenticated
	// principal and refuses a value naming anything else, so this is sent for
	// the routes that require the query parameter rather than as a choice.
	TenantID string

	// HTTPClient is the transport. It must already present the daemon's client
	// certificate and trust the control plane's CA — the same client the
	// heartbeat and the enroller use.
	HTTPClient *http.Client

	// Tokens supplies the bearer token for each call.
	Tokens TokenSource

	// Logger is used for diagnostics. Required.
	Logger interfaces.Logger
}

// AccountAPIClient reads a tenant's pooled provider accounts from the control
// plane and releases one account's credential envelope to this daemon.
type AccountAPIClient struct {
	serverAddr string
	tenantID   string
	httpClient *http.Client
	tokens     TokenSource
	logger     interfaces.Logger
}

// NewAccountAPIClient constructs the client, refusing an incomplete
// configuration rather than failing on the first spawn that needs an account.
func NewAccountAPIClient(cfg AccountAPIClientConfig) (*AccountAPIClient, error) {
	switch {
	case cfg.ServerAddr == "":
		return nil, ErrAccountClientNoServer
	case cfg.TenantID == "":
		return nil, ErrAccountClientNoTenant
	case cfg.HTTPClient == nil:
		return nil, ErrAccountClientNoHTTPClient
	case cfg.Tokens == nil:
		return nil, ErrAccountClientNoTokens
	case cfg.Logger == nil:
		return nil, ErrAccountClientNoLogger
	}
	return &AccountAPIClient{
		serverAddr: strings.TrimRight(cfg.ServerAddr, "/"),
		tenantID:   cfg.TenantID,
		httpClient: cfg.HTTPClient,
		tokens:     cfg.Tokens,
		logger:     cfg.Logger,
	}, nil
}

// TenantID reports the tenant this client reads the pool of.
func (c *AccountAPIClient) TenantID() string { return c.tenantID }

// =============================================================================
// Wire shapes
//
// These mirror pkg/api/rest/handlers_account.go's response envelopes. They are
// restated rather than imported because pkg/api/rest is behind the `rest`
// build tag and the daemon must build without it, and the wire format is v1 and
// singular (see CLAUDE.md's SDK boundary note) so there is exactly one shape to
// keep in step.
//
// What keeps them in step is not a comment: the agent-enrollment E2E
// (test/integration/agent_enrollment/account_release_test.go) drives this
// client against the PRODUCTION handlers, bound out of the real route table,
// so a field renamed on either side fails there rather than on a worker host.
// =============================================================================

// accountWire is the non-secret account metadata record.
type accountWire struct {
	ID                  string    `json:"id"`
	TenantID            string    `json:"tenant_id"`
	ProjectID           string    `json:"project_id,omitempty"`
	Provider            string    `json:"provider"`
	DisplayName         string    `json:"display_name,omitempty"`
	Default             bool      `json:"default"`
	Status              string    `json:"status"`
	RefreshPolicy       string    `json:"refresh_policy"`
	AvailableAt         time.Time `json:"available_at,omitempty"`
	CredentialExpiresAt time.Time `json:"credential_expires_at,omitempty"`
	LastValidatedAt     time.Time `json:"last_validated_at,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	LastUsedAt          time.Time `json:"last_used_at,omitempty"`
	LastRefreshedAt     time.Time `json:"last_refreshed_at,omitempty"`
	LastError           string    `json:"last_error,omitempty"`
}

// toOrchestration converts the wire record to the domain type the resolver
// selects over.
func (a *accountWire) toOrchestration() *orchestration.Account {
	if a == nil {
		return nil
	}
	return &orchestration.Account{
		ID:                  a.ID,
		TenantID:            a.TenantID,
		ProjectID:           a.ProjectID,
		Provider:            orchestration.AccountProvider(a.Provider),
		DisplayName:         a.DisplayName,
		Default:             a.Default,
		Status:              orchestration.AccountStatus(a.Status),
		RefreshPolicy:       orchestration.AccountCredentialMode(a.RefreshPolicy),
		AvailableAt:         a.AvailableAt,
		CredentialExpiresAt: a.CredentialExpiresAt,
		LastValidatedAt:     a.LastValidatedAt,
		CreatedAt:           a.CreatedAt,
		UpdatedAt:           a.UpdatedAt,
		LastUsedAt:          a.LastUsedAt,
		LastRefreshedAt:     a.LastRefreshedAt,
		LastError:           a.LastError,
	}
}

// accountEnvelopeWire is the single-account response envelope.
type accountEnvelopeWire struct {
	Account *accountWire `json:"account"`
}

// accountListWire is the list response envelope.
type accountListWire struct {
	Accounts []*accountWire `json:"accounts"`
	Total    int            `json:"total"`
}

// accountUsageWire is the usage response envelope.
type accountUsageWire struct {
	Usage struct {
		TotalTokens int64 `json:"total_tokens"`
	} `json:"usage"`
}

// releasedCredentialWire is the RELEASED credential envelope. It carries no
// refresh grant, because the control plane deliberately does not send one.
type releasedCredentialWire struct {
	Env               map[string]string `json:"env,omitempty"`
	Files             map[string][]byte `json:"files,omitempty"`
	AccessToken       string            `json:"access_token,omitempty"`
	ExpiresAt         time.Time         `json:"expires_at,omitempty"`
	Scopes            []string          `json:"scopes,omitempty"`
	AccessTokenEnvKey string            `json:"access_token_env_key,omitempty"`
}

// materializeWire is the credential-release response envelope.
type materializeWire struct {
	AccountID       string                  `json:"account_id"`
	Provider        string                  `json:"provider"`
	ProjectID       string                  `json:"project_id,omitempty"`
	RefreshPolicy   string                  `json:"refresh_policy,omitempty"`
	Credential      *releasedCredentialWire `json:"credential"`
	ExpiresAt       time.Time               `json:"expires_at,omitempty"`
	UpdatedAt       time.Time               `json:"updated_at,omitempty"`
	LastRefreshedAt time.Time               `json:"last_refreshed_at,omitempty"`
	LeaseID         string                  `json:"lease_id,omitempty"`
	LeaseExpiresAt  time.Time               `json:"lease_expires_at,omitempty"`
}

// MaterializedAccountCredential is one released envelope plus the metadata the
// daemon's cache needs to decide when it went stale.
type MaterializedAccountCredential struct {
	// AccountID is the released account.
	AccountID string

	// Credential is the released envelope. It never leaves the daemon's own
	// secret store except as environment/file material projected into a spawn.
	Credential *orchestration.AccountCredential

	// UpdatedAt is the account record's last write AS OF THE RELEASE. Reported
	// for diagnostics only: it moves on any write, including the usage stamp
	// the release itself makes, so it is not a usable staleness test.
	UpdatedAt time.Time

	// LastRefreshedAt is when the ENVELOPE was last replaced — an operator's
	// rotate, or an oauth renewal on the control plane. It is the
	// cache-invalidation signal: a later read reporting a newer one proves this
	// copy is superseded, without asking for the secret again.
	LastRefreshedAt time.Time

	// LeaseID is the DELIVERY GRANT this copy is held under.
	//
	// It is not a secret and it is not optional bookkeeping: it is what the
	// daemon presents when it refreshes this copy, and therefore what lets the
	// control plane refuse the refresh once the grant has been revoked. A
	// daemon that lost it would silently become un-cuttable-off.
	LeaseID string

	// LeaseExpiresAt is when the grant stops being refreshable without renewal.
	// It is NOT the credential's expiry, which is on the envelope.
	LeaseExpiresAt time.Time
}

// =============================================================================
// Verbs
// =============================================================================

// ListAccounts returns the tenant's accounts, optionally filtered to one
// provider and/or one project.
func (c *AccountAPIClient) ListAccounts(
	ctx context.Context,
	provider orchestration.AccountProvider,
	projectID string,
) ([]*orchestration.Account, error) {
	query := url.Values{}
	query.Set("tenant_id", c.tenantID)
	if provider != "" {
		query.Set("provider", string(provider))
	}
	if projectID != "" {
		query.Set("project_id", projectID)
	}

	var envelope accountListWire
	if err := c.do(ctx, http.MethodGet, accountAPIBasePath+"?"+query.Encode(), nil, &envelope); err != nil {
		return nil, err
	}
	out := make([]*orchestration.Account, 0, len(envelope.Accounts))
	for _, a := range envelope.Accounts {
		if converted := a.toOrchestration(); converted != nil {
			out = append(out, converted)
		}
	}
	return out, nil
}

// GetAccount returns one account's metadata record.
func (c *AccountAPIClient) GetAccount(ctx context.Context, accountID string) (*orchestration.Account, error) {
	var envelope accountEnvelopeWire
	if err := c.do(ctx, http.MethodGet, c.itemPath(accountID, ""), nil, &envelope); err != nil {
		return nil, err
	}
	account := envelope.Account.toOrchestration()
	if account == nil {
		return nil, &AccountAPIError{Op: "get account", AccountID: accountID,
			Message: "control plane returned no account record"}
	}
	return account, nil
}

// AccountTokens returns the cumulative tokens recorded against an account,
// satisfying orchestration.AccountUsageLookup so the daemon's
// LeastUsageAccountStrategy balances on the SAME ledger the control plane's
// does. Without it a daemon asked for the "least_usage" strategy would have no
// such strategy registered and would refuse the spawn.
func (c *AccountAPIClient) AccountTokens(ctx context.Context, tenantID, accountID string) (int64, error) {
	if tenantID != "" && tenantID != c.tenantID {
		return 0, &AccountAPITenantMismatchError{Requested: tenantID, Bound: c.tenantID}
	}
	var envelope accountUsageWire
	if err := c.do(ctx, http.MethodGet, c.itemPath(accountID, "/usage"), nil, &envelope); err != nil {
		return 0, err
	}
	return envelope.Usage.TotalTokens, nil
}

// MaterializeCredential releases one account's credential envelope to this
// daemon.
//
// It is the ONLY call in this package that receives secret material. The
// returned envelope is handed straight to the caller's cache; it is not
// logged, not persisted by this file, and not echoed into any error.
// leaseID presents the delivery grant this daemon already holds for the
// account, empty on a first release. Presenting it is what makes a refusal
// possible: once the grant is revoked, this call fails and the copy the caller
// holds can never be replaced.
func (c *AccountAPIClient) MaterializeCredential(
	ctx context.Context,
	accountID, projectID, leaseID string,
) (*MaterializedAccountCredential, error) {
	body := struct {
		ProjectID string `json:"project_id,omitempty"`
		LeaseID   string `json:"lease_id,omitempty"`
	}{ProjectID: projectID, LeaseID: leaseID}

	var envelope materializeWire
	if err := c.do(ctx, http.MethodPost, c.itemPath(accountID, "/materialize"), &body, &envelope); err != nil {
		return nil, err
	}
	if envelope.Credential == nil {
		return nil, &AccountAPIError{Op: "materialize credential", AccountID: accountID,
			Message: "control plane released no credential envelope"}
	}
	return &MaterializedAccountCredential{
		AccountID: accountID,
		Credential: &orchestration.AccountCredential{
			Env:               envelope.Credential.Env,
			Files:             envelope.Credential.Files,
			AccessToken:       envelope.Credential.AccessToken,
			ExpiresAt:         envelope.Credential.ExpiresAt,
			Scopes:            envelope.Credential.Scopes,
			AccessTokenEnvKey: envelope.Credential.AccessTokenEnvKey,
		},
		UpdatedAt:       envelope.UpdatedAt,
		LastRefreshedAt: envelope.LastRefreshedAt,
		LeaseID:         envelope.LeaseID,
		LeaseExpiresAt:  envelope.LeaseExpiresAt,
	}, nil
}

// itemPath builds the /{id}{suffix} path with the tenant_id query parameter
// every item-scoped account route requires.
func (c *AccountAPIClient) itemPath(accountID, suffix string) string {
	return accountAPIBasePath + "/" + url.PathEscape(accountID) + suffix +
		"?tenant_id=" + url.QueryEscape(c.tenantID)
}

// =============================================================================
// Transport
// =============================================================================

// do issues one authenticated request and decodes the JSON response into out.
//
// A 401 invalidates the cached token and retries exactly once, mirroring
// HTTPHeartbeatSender.SendHeartbeat: both share the same TokenSource, so a
// token the control plane has rotated out from under the daemon shows up on
// whichever path calls first, and each must be able to recover alone. The
// ladder deliberately stops there — the heartbeat's 404 re-registration path
// is not repeated here, because an account call is not how a daemon should
// discover it has been forgotten, and racing two re-enrollments would be worse
// than failing the spawn that asked.
func (c *AccountAPIClient) do(ctx context.Context, method, path string, body, out interface{}) error {
	attemptCtx, cancel := context.WithTimeout(ctx, accountAPITimeout)
	defer cancel()

	err := c.attempt(attemptCtx, method, path, body, out, false)
	if !errors.Is(err, errAccountAPIUnauthorized) {
		return err
	}

	// Worth a line: a daemon whose bearer the control plane keeps rotating out
	// from under it recovers silently here, and the only evidence an operator
	// would otherwise have is a latency bump on every spawn.
	c.logger.Debugf("agent account pool: control plane refused %s with 401; retrying with a fresh token", method)
	c.tokens.Invalidate()
	retryCtx, retryCancel := context.WithTimeout(ctx, accountAPITimeout)
	defer retryCancel()
	return c.attempt(retryCtx, method, path, body, out, true)
}

// attempt issues one request. afterRefresh distinguishes the retry, so a
// second 401 surfaces as a terminal error rather than looping.
func (c *AccountAPIClient) attempt(ctx context.Context, method, path string, body, out interface{}, afterRefresh bool) error {
	op := method + " " + strings.SplitN(path, "?", 2)[0]

	token, tokenErr := c.tokens.Token(ctx)
	if tokenErr != nil {
		return &AccountAPIError{Op: "acquire token for " + op, Cause: tokenErr}
	}

	var reader io.Reader
	if body != nil {
		encoded, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return &AccountAPIError{Op: "encode request for " + op, Cause: marshalErr}
		}
		reader = bytes.NewReader(encoded)
	}

	req, reqErr := http.NewRequestWithContext(ctx, method, c.serverAddr+path, reader)
	if reqErr != nil {
		return &AccountAPIError{Op: "build request for " + op, Cause: reqErr}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, doErr := c.httpClient.Do(req)
	if doErr != nil {
		return &AccountAPIError{Op: op, Cause: doErr}
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if out == nil {
			return nil
		}
		if decodeErr := json.NewDecoder(resp.Body).Decode(out); decodeErr != nil {
			return &AccountAPIError{Op: "decode response for " + op, Cause: decodeErr}
		}
		return nil
	case resp.StatusCode == http.StatusUnauthorized && !afterRefresh:
		return errAccountAPIUnauthorized
	default:
		return &AccountAPIError{Op: op, Status: resp.StatusCode, Message: accountAPIFailureDetail(resp)}
	}
}

// accountAPIMaxErrorBody bounds how much of a failure body is read into an
// error message.
const accountAPIMaxErrorBody = 512

// accountAPIFailureDetail extracts the control plane's own explanation of a
// refusal so an operator sees "account is restricted to project X" rather than
// a bare status code.
//
// It reads a BOUNDED prefix and only from a FAILURE response. A 2xx body is
// never read here, which is what keeps a released credential out of every
// error string this package can produce.
func accountAPIFailureDetail(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, accountAPIMaxErrorBody))
	if err != nil || len(body) == 0 {
		return ""
	}
	var envelope struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		if envelope.Error != "" {
			return envelope.Error
		}
		if envelope.Message != "" {
			return envelope.Message
		}
	}
	return strings.TrimSpace(string(body))
}

// =============================================================================
// Typed errors
// =============================================================================

// errAccountAPIUnauthorized is the internal signal that a call was refused 401
// and is worth one retry with a freshly exchanged token. It never escapes do().
var errAccountAPIUnauthorized = errors.New("agent account api: 401 unauthorized")

// Construction errors. They name the missing dependency so a misconfigured
// daemon fails at startup rather than on the first spawn that needs an account.
var (
	// ErrAccountClientNoServer reports a client built with no control plane
	// address.
	ErrAccountClientNoServer = errors.New("agent account api: control plane address is required")

	// ErrAccountClientNoTenant reports a client built with no tenant.
	ErrAccountClientNoTenant = errors.New("agent account api: tenant ID is required")

	// ErrAccountClientNoHTTPClient reports a client built with no HTTP client.
	ErrAccountClientNoHTTPClient = errors.New("agent account api: HTTP client is required")

	// ErrAccountClientNoTokens reports a client built with no token source.
	ErrAccountClientNoTokens = errors.New("agent account api: token source is required")

	// ErrAccountClientNoLogger reports a client built with no logger.
	ErrAccountClientNoLogger = errors.New("agent account api: logger is required")
)

// AccountAPIError reports a failed control-plane account call.
type AccountAPIError struct {
	// Op names the request that failed, e.g. "POST /api/v1/agent-accounts/x/materialize".
	Op string

	// AccountID is the account the call concerned, when the failure is
	// account-specific.
	AccountID string

	// Status is the HTTP status, zero for a transport or encoding failure.
	Status int

	// Message is the control plane's own explanation, when it sent one.
	Message string

	// Cause is the underlying transport / encoding error, when there was one.
	Cause error
}

// Error implements the error interface.
func (e *AccountAPIError) Error() string {
	msg := "agent account api: " + e.Op
	if e.AccountID != "" {
		msg += " (account " + e.AccountID + ")"
	}
	if e.Status != 0 {
		msg += ": status " + strconv.Itoa(e.Status)
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying transport error, if any.
func (e *AccountAPIError) Unwrap() error { return e.Cause }

// AccountAPITenantMismatchError reports a call asking about a tenant other
// than the one this daemon is enrolled in.
//
// It is refused rather than forwarded: the control plane would refuse it too
// (tenancy is bound from the authenticated principal), and refusing here makes
// the mistake legible as a wiring bug on the daemon instead of a 403 that looks
// like a missing grant.
type AccountAPITenantMismatchError struct {
	// Requested is the tenant the caller named.
	Requested string

	// Bound is the tenant this daemon is enrolled in.
	Bound string
}

// Error implements the error interface.
func (e *AccountAPITenantMismatchError) Error() string {
	return "agent account api: request names tenant " + strconv.Quote(e.Requested) +
		" but this daemon is enrolled in " + strconv.Quote(e.Bound)
}

// Compile-time proof the client satisfies the usage lookup the least-usage
// strategy depends on.
var _ orchestration.AccountUsageLookup = (*AccountAPIClient)(nil)

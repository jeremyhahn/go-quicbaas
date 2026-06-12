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

package iam

import (
	"context"
	"log/slog"
	"sync"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// WebAuthnFactoryConfig holds the relying-party configuration used by the
// per-tenant xkms WebAuthn services. The fields mirror
// xkmswebauthn.Config — the bridge between quicbaas's IAMWebAuthnConfig
// (in pkg/config) and the underlying go-webauthn library.
type WebAuthnFactoryConfig struct {
	RPID                    string
	RPDisplayName           string
	RPOrigins               []string
	Timeout                 time.Duration
	UserVerification        string
	AttestationPreference   string
	ResidentKeyRequirement  string
	AuthenticatorAttachment string
	Debug                   bool
}

// WebAuthnFactoryParams contains the dependencies required to build a
// per-tenant xkms WebAuthn service.
type WebAuthnFactoryParams struct {
	// Config is the WebAuthn relying-party configuration. Required.
	Config WebAuthnFactoryConfig

	// QRDB is the qrdb SDK client used to back the per-tenant user and
	// credential stores via xkms's qrdb adapter pair (Phase 4). The
	// factory does not take ownership of the client lifecycle.
	// Required.
	QRDB *qrdbsdk.Client

	// Logger is the slog logger passed to each xkms Service. Optional;
	// defaults to slog.Default() when nil.
	Logger *slog.Logger

	// Settings resolves a tenant's per-tenant WebAuthn override so the factory
	// can build a service with that tenant's RPID/RPDisplayName/RPOrigins,
	// falling back per-field to Config when the override is empty. Optional;
	// when nil every tenant uses the global Config (today's behavior).
	Settings TenantWebAuthnSettingsProvider
}

// TenantWebAuthnSettingsProvider resolves a tenant's WebAuthn relying-party
// override. It is satisfied by the IAM TenantManager (GetTenant -> Settings.
// WebAuthn). Returning (nil, nil) means the tenant has no override and the
// factory uses the global Config unchanged.
type TenantWebAuthnSettingsProvider interface {
	// WebAuthnSettingsFor returns the tenant's WebAuthn override, or nil when
	// the tenant configured none. A non-nil error fails service construction
	// closed (the tenant's WebAuthn surface is unavailable rather than
	// silently using a stale/global RP).
	WebAuthnSettingsFor(ctx context.Context, tenantID string) (*TenantWebAuthnConfig, error)
}

// WebAuthnFactoryConfigError reports an invalid factory configuration.
type WebAuthnFactoryConfigError struct {
	Field string
}

// Error implements error.
func (e *WebAuthnFactoryConfigError) Error() string {
	if e == nil || e.Field == "" {
		return "iam.webauthn.factory: invalid configuration"
	}
	return "iam.webauthn.factory: missing or invalid field: " + e.Field
}

// WebAuthnSettingsReadError reports a genuine store/read failure while
// resolving a tenant's per-tenant WebAuthn relying-party override. It is a
// fail-closed signal: a known-but-unreadable tenant must NOT silently fall
// back to the global RP (an isolation leak). The factory denies the WebAuthn
// ceremony for that tenant rather than serving it under the wrong relying
// party. A not-found tenant is NOT this error — that case is the legitimate
// "no per-tenant override" path and still falls back to the global RP.
type WebAuthnSettingsReadError struct {
	TenantID string
	Cause    error
}

// Error implements error.
func (e *WebAuthnSettingsReadError) Error() string {
	if e == nil {
		return "iam.webauthn.settings: read error"
	}
	msg := "iam.webauthn.settings: failed to resolve tenant override: " + e.TenantID
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *WebAuthnSettingsReadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// WebAuthnLoginSessionError reports a failure to correlate a FinishLogin
// assertion back to its originating BeginLogin ceremony session. It is an
// auth fail-closed signal: the manager refuses to dispatch the assertion to
// xkms when the per-ceremony challenge is absent or does not match any stored
// session, preventing a concurrent or forged request from being cross-wired
// onto another in-flight login.
type WebAuthnLoginSessionError struct {
	Reason string
}

// Error implements error.
func (e *WebAuthnLoginSessionError) Error() string {
	if e == nil || e.Reason == "" {
		return "iam.webauthn.login: login session not found"
	}
	return "iam.webauthn.login: " + e.Reason
}

// WebAuthnUserResolverError reports that FinishLogin could not map the asserted
// webauthn identity back to a canonical broker iam.User. It is an auth
// fail-closed signal: the manager refuses to surface a half-populated user
// (which would yield a token with an unusable subject and no roles) when the
// canonical user store is missing or returns no user.
type WebAuthnUserResolverError struct {
	Reason string
}

// Error implements error.
func (e *WebAuthnUserResolverError) Error() string {
	if e == nil || e.Reason == "" {
		return "iam.webauthn.login: canonical user resolution failed"
	}
	return "iam.webauthn.login: " + e.Reason
}

// WebAuthnServiceFactory builds and caches per-tenant xkms WebAuthn
// services. Each tenant gets its own *xkmswebauthn.Service backed by
// the xkms qrdb adapter pair scoped to that tenant id (Option A from
// the Phase 5 plan: lazy-cached per-tenant services). The cache uses
// sync.Map for concurrent-safe lookups; the first lookup for a given
// tenant constructs the service, subsequent lookups hit the cache.
//
// The factory deliberately does not pre-create services for every
// tenant — most tenants never use webauthn, and constructing a service
// per tenant carries a per-instance cost (the user/credential store
// adapter pair plus the in-memory ceremony session store). Lazy
// construction keeps the platform's footprint proportional to active
// usage.
type WebAuthnServiceFactory struct {
	params WebAuthnFactoryParams
	logger *slog.Logger

	// services caches one *xkmswebauthn.Service per tenant.
	services sync.Map // map[tenantID]*xkmssdk.WebAuthnService

	// constructLock serialises ServiceFor calls for the same tenant id
	// so concurrent lookups don't construct duplicate services. The
	// stored value is a *sync.Mutex per tenant id; the per-tenant locks
	// avoid global contention.
	constructLocks sync.Map // map[tenantID]*sync.Mutex
}

// NewWebAuthnServiceFactory constructs the factory. The QRDB client and
// the WebAuthn config must be valid; the factory validates the config
// up-front so misconfiguration surfaces at startup rather than at
// first ceremony.
func NewWebAuthnServiceFactory(params WebAuthnFactoryParams) (*WebAuthnServiceFactory, error) {
	if params.QRDB == nil {
		return nil, &WebAuthnFactoryConfigError{Field: "QRDB"}
	}
	if params.Config.RPID == "" {
		return nil, &WebAuthnFactoryConfigError{Field: "Config.RPID"}
	}
	if params.Config.RPDisplayName == "" {
		return nil, &WebAuthnFactoryConfigError{Field: "Config.RPDisplayName"}
	}
	if len(params.Config.RPOrigins) == 0 {
		return nil, &WebAuthnFactoryConfigError{Field: "Config.RPOrigins"}
	}
	logger := params.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &WebAuthnServiceFactory{
		params: params,
		logger: logger,
	}, nil
}

// ServiceFor returns the cached xkms webauthn service for the given
// tenant id, constructing one on first access. Concurrent calls for
// the same tenant id construct only one service. The per-tenant
// relying-party configuration is resolved from the settings provider
// (when wired) with per-field fallback to the global Config; a tenant
// that configures nothing keeps the global RP unchanged.
func (f *WebAuthnServiceFactory) ServiceFor(ctx context.Context, tenantID string) (*xkmssdk.WebAuthnService, error) {
	if tenantID == "" {
		return nil, &WebAuthnFactoryConfigError{Field: "tenantID"}
	}
	if v, ok := f.services.Load(tenantID); ok {
		return v.(*xkmssdk.WebAuthnService), nil
	}

	// Per-tenant construction lock to prevent duplicate construction.
	lockI, _ := f.constructLocks.LoadOrStore(tenantID, &sync.Mutex{})
	lock := lockI.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	// Re-check after acquiring the lock — another goroutine may have
	// constructed the service while we waited.
	if v, ok := f.services.Load(tenantID); ok {
		return v.(*xkmssdk.WebAuthnService), nil
	}

	svc, err := f.buildService(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	f.services.Store(tenantID, svc)
	return svc, nil
}

// resolveConfig builds the effective per-tenant WebAuthn relying-party
// configuration: it starts from the global Config and overrides only the
// fields the tenant explicitly set (per-field fallback). An empty RPID
// inherits the global RPID, empty origins inherit the global origins, and so
// on, mirroring the DisplayName->Name / SandboxPolicy->server-default idiom.
// When no settings provider is wired, or the tenant configured no override,
// the global Config is returned unchanged (today's behavior).
//
// A genuine store/read error from the settings provider is propagated
// (fail-closed): the WebAuthn ceremony for that tenant is denied rather than
// silently served under the global relying party. The provider distinguishes
// "tenant not found / no override" (-> nil, nil -> global fallback) from a
// store read error (-> non-nil error).
func (f *WebAuthnServiceFactory) resolveConfig(ctx context.Context, tenantID string) (WebAuthnFactoryConfig, error) {
	cfg := f.params.Config
	if f.params.Settings == nil {
		return cfg, nil
	}
	override, err := f.params.Settings.WebAuthnSettingsFor(ctx, tenantID)
	if err != nil {
		return WebAuthnFactoryConfig{}, err
	}
	if override == nil {
		return cfg, nil
	}
	if override.RPID != "" {
		cfg.RPID = override.RPID
	}
	if override.RPDisplayName != "" {
		cfg.RPDisplayName = override.RPDisplayName
	}
	if len(override.RPOrigins) > 0 {
		cfg.RPOrigins = override.RPOrigins
	}
	return cfg, nil
}

// buildService constructs a single per-tenant xkms webauthn service.
// The user and credential stores are wired through xkms's
// QRDBUserStoreAdapter (Phase 4), keyed by tenant id. The session
// store is the lightweight in-memory store that ships with xkms —
// ceremony sessions are short-lived (~60s) and process-local, so the
// in-memory implementation is a deliberate fit.
func (f *WebAuthnServiceFactory) buildService(ctx context.Context, tenantID string) (*xkmssdk.WebAuthnService, error) {
	resolved, err := f.resolveConfig(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	cfg := &xkmssdk.WebAuthnConfig{
		RPID:                    resolved.RPID,
		RPDisplayName:           resolved.RPDisplayName,
		RPOrigins:               resolved.RPOrigins,
		Timeout:                 resolved.Timeout,
		UserVerification:        resolved.UserVerification,
		AttestationPreference:   resolved.AttestationPreference,
		ResidentKeyRequirement:  resolved.ResidentKeyRequirement,
		AuthenticatorAttachment: resolved.AuthenticatorAttachment,
		Debug:                   resolved.Debug,
	}
	users, creds, err := xkmssdk.NewWebAuthnQRDBUserStoreAdapter(xkmssdk.WebAuthnQRDBUserStoreOptions{
		Users:    f.params.QRDB.Users(),
		KV:       f.params.QRDB.KV(),
		TenantID: tenantID,
	})
	if err != nil {
		return nil, err
	}
	sessions := xkmssdk.NewWebAuthnMemorySessionStore()
	return xkmssdk.NewWebAuthnService(xkmssdk.WebAuthnServiceParams{
		Config:          cfg,
		UserStore:       users,
		SessionStore:    sessions,
		CredentialStore: creds,
		Logger:          f.logger.With("component", "iam.webauthn", "tenant_id", tenantID),
	})
}

// Reset clears all cached services. Tests use this to force fresh
// construction; production callers should not need it. The factory
// remains usable after Reset.
func (f *WebAuthnServiceFactory) Reset() {
	f.services.Range(func(k, _ any) bool {
		f.services.Delete(k)
		return true
	})
}

// ResetTenant evicts the cached service for a single tenant so the next
// ServiceFor rebuilds it from the tenant's current settings. Callers invoke
// it after a tenant updates its WebAuthn override (RPID/origins/display name)
// so the change takes effect without restarting the process. Evicting an
// uncached tenant is a no-op.
func (f *WebAuthnServiceFactory) ResetTenant(tenantID string) {
	f.services.Delete(tenantID)
}

// OnTenantSettingsChanged implements TenantSettingsObserver: it evicts the
// tenant's cached WebAuthn service so the next ServiceFor rebuilds it from the
// tenant's updated RPID/origins/display-name override.
func (f *WebAuthnServiceFactory) OnTenantSettingsChanged(tenantID string) {
	f.ResetTenant(tenantID)
}

// TenantManagerWebAuthnSettings adapts a TenantManager into a
// TenantWebAuthnSettingsProvider for the factory: it resolves a tenant's
// WebAuthn override from TenantSettings.WebAuthn. A tenant that does not exist
// or that configured no override yields (nil, nil) so the factory uses the
// global Config (today's behavior is preserved).
type TenantManagerWebAuthnSettings struct {
	tenants TenantManager
}

// NewTenantManagerWebAuthnSettings wraps a TenantManager as a
// TenantWebAuthnSettingsProvider.
func NewTenantManagerWebAuthnSettings(tenants TenantManager) *TenantManagerWebAuthnSettings {
	return &TenantManagerWebAuthnSettings{tenants: tenants}
}

// WebAuthnSettingsFor returns the tenant's WebAuthn override, or nil when the
// tenant has no per-tenant settings.
//
// It fails closed on a genuine store/read error: a known-but-unreadable
// tenant must NOT silently fall back to the global relying party (an isolation
// leak). The resolution distinguishes the two cases the tenant store already
// exposes:
//
//   - tenant not found (dberrors.IsNotFound) -> (nil, nil): the legitimate
//     "no per-tenant override" path; the factory uses the global RP. Existing
//     tenants that never configured an override are unaffected.
//   - any other GetTenant error (storage/transport/decode failure) ->
//     (*WebAuthnSettingsReadError): the WebAuthn ceremony for that tenant is
//     denied rather than served under the wrong relying party.
func (a *TenantManagerWebAuthnSettings) WebAuthnSettingsFor(ctx context.Context, tenantID string) (*TenantWebAuthnConfig, error) {
	if a == nil || a.tenants == nil {
		return nil, nil
	}
	tenant, err := a.tenants.GetTenant(ctx, tenantID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			// No such tenant: the legitimate not-configured case. Fall back
			// to the global RP, preserving today's behavior.
			return nil, nil
		}
		// Genuine store/read error for a tenant that may well exist: fail
		// closed instead of leaking the global RP into another tenant's
		// ceremony.
		return nil, &WebAuthnSettingsReadError{TenantID: tenantID, Cause: err}
	}
	return tenant.Settings.WebAuthn, nil
}

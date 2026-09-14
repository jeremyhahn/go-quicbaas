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
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	// go-xkms/sdk/go re-exports the WebAuthn CredentialStore and Credential
	// types but not UserStore, so the registrant lookup the factor label and
	// the assurance gate depend on takes the same justified direct package
	// import webauthn_factor_writethrough.go documents.
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
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

	// AllowPublicRegistration lets a registration ceremony CREATE an account
	// for an email the store has never seen. It carries the operator's
	// iam.self_service_registration.enabled decision, because that is the
	// surface that registers an unknown email
	// (pkg/api/rest/iam_register_self.go: BeginRegistration is called for an
	// email the identity store only creates at the FINISH step).
	//
	// go-xkms v0.9.2-alpha made this closed by default — v0.9.1 created the
	// account implicitly, and its own config test asserted "a nil Config must
	// read as open". Leaving it unmapped would therefore have silently
	// disabled self-service registration on a node that has it switched on.
	// An account that already exists still enrols its first passkey through
	// the same entry point either way; only implicit creation is withheld.
	AllowPublicRegistration bool
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

	// AttestationRecorder resolves the node's trusted-server attestation
	// recorder for the fido2 write-through. Required: without it a completed
	// ceremony cannot record what it proved, and the write-through fails the
	// enrollment closed. The server derives it by type assertion on the node's
	// auth-factor servicer (pkg/server); tests inject a fake.
	AttestationRecorder AttestationRecorderResolver

	// Logger is the slog logger passed to each xkms Service. Optional;
	// defaults to slog.Default() when nil.
	Logger *slog.Logger

	// Settings resolves a tenant's per-tenant WebAuthn override so the factory
	// can build a service with that tenant's RPID/RPDisplayName/RPOrigins,
	// falling back per-field to Config when the override is empty. Optional;
	// when nil every tenant uses the global Config (today's behavior).
	Settings TenantWebAuthnSettingsProvider

	// Assurance is the minimum attestation level a NON-administrative
	// registration must prove. Administrative registrations are always held to
	// hardware regardless of this value.
	//
	// The zero value is WebAuthnAssuranceHardware, so a caller that does not
	// set it gets the strict policy. This is deliberate: the field is the one
	// an operator uses to LOOSEN the gate, never to tighten it.
	Assurance WebAuthnAssurance

	// AAGUIDAllowlist is the authenticator-model gate applied once the
	// attestation statement has proven the AAGUID is meaningful. Optional in
	// the sense that the factory constructs without it — but a nil allowlist
	// DENIES any enrollment that must prove hardware, rather than being
	// skipped. See AAGUIDAllowlist.Check.
	AAGUIDAllowlist *AAGUIDAllowlist

	// Admins resolves whether a registrant holds an administrative role, so a
	// tenant-permissive deployment still holds its administrators to hardware.
	// Optional; nil means the gate cannot tell, and it resolves an unknown
	// account to administrative.
	Admins WebAuthnAdminResolver

	// AttestationRoots are the manufacturer attestation roots an authenticator's
	// batch attestation certificate must chain to before it counts as hardware.
	//
	// Nil means no roots are configured, which REFUSES hardware enrollment
	// rather than trusting any attestation certificate: go-webauthn assigns
	// "basic_full" without walking the trust path, so an unverified chain is a
	// claim rather than evidence. See webauthn_attestation_chain.go.
	AttestationRoots *WebAuthnAttestationRoots
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
	if params.AttestationRecorder == nil {
		return nil, &WebAuthnFactoryConfigError{Field: "AttestationRecorder"}
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
		if svc, ok := v.(*xkmssdk.WebAuthnService); ok {
			return svc, nil
		}
	}

	// Per-tenant construction lock to prevent duplicate construction.
	lockI, _ := f.constructLocks.LoadOrStore(tenantID, &sync.Mutex{})
	lock, _ := lockI.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	// Re-check after acquiring the lock — another goroutine may have
	// constructed the service while we waited.
	if v, ok := f.services.Load(tenantID); ok {
		if svc, ok := v.(*xkmssdk.WebAuthnService); ok {
			return svc, nil
		}
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

// xkmsWebAuthnConfig projects the resolved per-tenant factory config onto the
// go-xkms relying-party config. It is the one place the projection happens,
// kept separate from buildService so the field-for-field mapping — including
// AllowPublicRegistration, which go-xkms reads as closed when left nil — can
// be asserted without a live qrdb transport.
func xkmsWebAuthnConfig(resolved WebAuthnFactoryConfig) *xkmssdk.WebAuthnConfig {
	allowPublicRegistration := resolved.AllowPublicRegistration
	return &xkmssdk.WebAuthnConfig{
		RPID:                    resolved.RPID,
		RPDisplayName:           resolved.RPDisplayName,
		RPOrigins:               resolved.RPOrigins,
		Timeout:                 resolved.Timeout,
		UserVerification:        resolved.UserVerification,
		AttestationPreference:   resolved.AttestationPreference,
		ResidentKeyRequirement:  resolved.ResidentKeyRequirement,
		AuthenticatorAttachment: resolved.AuthenticatorAttachment,
		Debug:                   resolved.Debug,
		AllowPublicRegistration: &allowPublicRegistration,
	}
}

// buildService constructs a single per-tenant xkms webauthn service.
// The user and credential stores are wired through xkms's
// QRDBUserStoreAdapter (Phase 4), keyed by tenant id. The session
// store is the lightweight in-memory store that ships with xkms —
// ceremony sessions are short-lived (~60s) and process-local, so the
// in-memory implementation is a deliberate fit.
//
// The credential store is decorated with webAuthnFactorWriteThrough so a
// completed registration ALSO enrolls the authenticator in qrdb's
// UserAuthFactor store — the store the unified login ceremony resolves
// against. This is the single choke point every registration entry point
// converges on (xkms Service.FinishRegistration -> CredentialStore.Save);
// see webauthn_factor_writethrough.go for the ordering and compensation
// rationale. The same decorator revokes that factor when the credential is
// deleted, so the pair stays consistent whichever direction the mutation runs.
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
		AttestationPreference:   f.attestationConveyance(resolved.AttestationPreference),
		ResidentKeyRequirement:  resolved.ResidentKeyRequirement,
		AuthenticatorAttachment: resolved.AuthenticatorAttachment,
		Debug:                   resolved.Debug,
		AllowPublicRegistration: &resolved.AllowPublicRegistration,
	}
	users, creds, err := f.tenantStores(tenantID)
	if err != nil {
		return nil, err
	}
	logger := f.tenantLogger(tenantID)
	writeThrough, err := f.newFactorWriteThrough(creds, users, tenantID)
	if err != nil {
		return nil, err
	}
	sessions := xkmssdk.NewWebAuthnMemorySessionStore()
	return xkmssdk.NewWebAuthnService(xkmssdk.WebAuthnServiceParams{
		Config:          cfg,
		UserStore:       users,
		SessionStore:    sessions,
		CredentialStore: writeThrough,
		Logger:          logger,
	})
}

// tenantLogger returns the logger every per-tenant WebAuthn component in this
// factory annotates its records with, so a relying party built here and one
// built for the composed go-xkms module are attributable to the same tenant in
// the log.
func (f *WebAuthnServiceFactory) tenantLogger(tenantID string) *slog.Logger {
	return f.logger.With("component", "iam.webauthn", "tenant_id", tenantID)
}

// tenantStores builds the qrdb-backed WebAuthn user and credential store pair
// scoped to tenantID. It is the same adapter construction go-xkms performs for
// its own relying party, so both surfaces read and write ONE store.
func (f *WebAuthnServiceFactory) tenantStores(tenantID string) (xkmswebauthn.UserStore, xkmssdk.WebAuthnCredentialStore, error) {
	if f == nil || f.params.QRDB == nil {
		return nil, nil, &WebAuthnFactoryConfigError{Field: "QRDB"}
	}
	users, creds, err := xkmssdk.NewWebAuthnQRDBUserStoreAdapter(xkmssdk.WebAuthnQRDBUserStoreOptions{
		Users:    f.params.QRDB.Users(),
		KV:       f.params.QRDB.KV(),
		TenantID: tenantID,
	})
	if err != nil {
		return nil, nil, err
	}
	return users, creds, nil
}

// newFactorWriteThrough is THE construction path for the qrdb fido2
// write-through decorator. Both relying parties that persist passkeys on a
// composed node go through it: the per-tenant service this factory builds
// (buildService) and the composed go-xkms module's own service, which reaches
// it through DecorateCredentialStore. Forking the construction is what
// produced go-quicbaas#484 — a credential registered on one surface with no
// login factor on the other — so there is deliberately only one.
func (f *WebAuthnServiceFactory) newFactorWriteThrough(
	inner xkmssdk.WebAuthnCredentialStore,
	users xkmswebauthn.UserStore,
	tenantID string,
) (*webAuthnFactorWriteThrough, error) {
	if f == nil || f.params.QRDB == nil {
		return nil, &WebAuthnFactoryConfigError{Field: "QRDB"}
	}
	return newWebAuthnFactorWriteThrough(
		inner, users, f.params.QRDB.UserAuthFactors(), f.params.AttestationRecorder, tenantID, f.gate(), f.tenantLogger(tenantID))
}

// DecorateCredentialStore wraps inner — a WebAuthn credential store built by a
// relying party this factory did NOT construct — so a registration completed
// there enrolls the same qrdb fido2 login factor a registration on quicbaas's
// own surface does.
//
// The composed go-xkms module serves its own registration ceremony at
// /api/v1/webauthn/sdk/registration/* (what `xkey auth register` posts to) over
// the SAME qrdb-backed credential store, through a relying party it builds
// itself. Without this decoration that surface persists a credential and no
// login factor, and the unified login ceremony then refuses the resulting
// assertion with "factor not found" — go-quicbaas#484.
//
// The user store is rebuilt here rather than taken from the caller because the
// factor's label and the assurance gate's administrator check both read the
// registrant through the store scoped to tenantID, and only this factory knows
// how that adapter is constructed.
func (f *WebAuthnServiceFactory) DecorateCredentialStore(
	inner xkmssdk.WebAuthnCredentialStore,
	tenantID string,
) (xkmssdk.WebAuthnCredentialStore, error) {
	if f == nil {
		return nil, &WebAuthnFactoryConfigError{Field: "factory"}
	}
	if tenantID == "" {
		return nil, &WebAuthnFactorStoreConfigError{Field: "tenantID"}
	}
	// Validated BEFORE the stores are derived: a doomed call must not first
	// construct qrdb adapters it will throw away, and the refusal must not
	// depend on how far into construction the failure happens to surface.
	if inner == nil {
		return nil, &WebAuthnFactorStoreConfigError{Field: "CredentialStore"}
	}
	// Only the user store is wanted: inner already IS the credential store
	// the caller's relying party built over this tenant, so the adapter's own
	// credential store would be a second handle on the same records.
	users, _, err := f.tenantStores(tenantID)
	if err != nil {
		return nil, err
	}
	writeThrough, err := f.newFactorWriteThrough(inner, users, tenantID)
	if err != nil {
		// Widened explicitly rather than returned through, so an error yields a
		// genuinely nil interface. `return f.newFactorWriteThrough(...)` would
		// hand back a nil *webAuthnFactorWriteThrough inside a NON-nil
		// interface, which reads as a usable store to any caller that checks
		// the value rather than the error — go-xkms's own decorator seam grew a
		// typed-nil guard for exactly this shape. Today every reachable failure
		// is refused above, so this is the guard that keeps that true when a new
		// failure mode is added to newFactorWriteThrough.
		return nil, err
	}
	return writeThrough, nil
}

// gate builds the enrollment assurance gate the per-tenant write-through
// applies. It is constructed per service rather than stored once so a factory
// Reset picks up a re-wired allowlist.
func (f *WebAuthnServiceFactory) gate() *WebAuthnAttestationGate {
	return NewWebAuthnAttestationGate(
		f.params.Assurance,
		f.params.AAGUIDAllowlist,
		f.params.Admins,
	)
}

// AttestationRoots returns the configured manufacturer attestation roots, or
// nil when none are configured. Ceremony entry points read them through the
// factory so the trust store and the gate that depends on it are wired from one
// place.
func (f *WebAuthnServiceFactory) AttestationRoots() *WebAuthnAttestationRoots {
	if f == nil {
		return nil
	}
	return f.params.AttestationRoots
}

// VerifyAttestationChain verifies a parsed registration's attestation trust
// path and returns a context carrying the result for the assurance gate.
//
// It is the single helper every ceremony entry point calls, so the three
// surfaces that can finish a registration — the manager, the tenant-scoped
// route that dispatches into xkms, and the multi-protocol FIDO2 service — agree
// on what was verified rather than each deciding for itself.
//
// A verification failure is NOT fatal here. Enrollment policy belongs to the
// gate: a tenant-permissive deployment may legitimately enroll an authenticator
// with no usable chain, and refusing at this point would apply the
// administrator's rule to everyone. The context simply carries no verified
// chain, which the gate reads as "not proven" and refuses only where hardware
// is required. The reason is returned so the caller can log it.
func (f *WebAuthnServiceFactory) VerifyAttestationChain(
	ctx context.Context,
	parsed *protocol.ParsedCredentialCreationData,
) (context.Context, error) {
	chain, err := f.AttestationRoots().VerifyChain(parsed)
	if err != nil {
		// Clear, do not pass through: a proof this server did not just produce
		// must not survive a verification that failed.
		return clearAttestationChain(ctx), err
	}
	return contextWithAttestationChain(ctx, chain), nil
}

// attestationConveyance resolves the attestation conveyance the relying party
// asks the browser for.
//
// A deployment that requires hardware MUST request "direct": with the default
// "none" conveyance the browser strips the attestation statement before the RP
// ever sees it, so the authenticator reports no AAGUID and there is nothing to
// verify. That is the defect that made the AAGUID allowlist vacuous even when
// an operator configured it strictly — the gate was asked to test an
// identifier the ceremony had already discarded.
//
// The override raises conveyance, never lowers it: an operator who configured
// "enterprise" keeps it, and one who configured "none" while requiring
// hardware gets "direct" rather than a gate that can only ever refuse.
func (f *WebAuthnServiceFactory) attestationConveyance(configured string) string {
	// Conveyance is raised whenever ANY registration on this node might have to
	// prove hardware — which includes assurance "any", because administrators
	// are held to hardware under that policy too. Raising it only for
	// assurance=="hardware" left a deployment where admins could never enroll:
	// the browser stripped the statement the gate then demanded.
	if f.params.Assurance.AllowsSoftwareAdministrators() {
		return configured
	}
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case "direct", "enterprise":
		return configured
	default:
		return "direct"
	}
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

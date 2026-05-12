//go:build iam

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
	"log/slog"
	"sync"
	"time"

	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
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
	services sync.Map // map[tenantID]*xkmswebauthn.Service

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
// the same tenant id construct only one service.
func (f *WebAuthnServiceFactory) ServiceFor(tenantID string) (*xkmswebauthn.Service, error) {
	if tenantID == "" {
		return nil, &WebAuthnFactoryConfigError{Field: "tenantID"}
	}
	if v, ok := f.services.Load(tenantID); ok {
		return v.(*xkmswebauthn.Service), nil
	}

	// Per-tenant construction lock to prevent duplicate construction.
	lockI, _ := f.constructLocks.LoadOrStore(tenantID, &sync.Mutex{})
	lock := lockI.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	// Re-check after acquiring the lock — another goroutine may have
	// constructed the service while we waited.
	if v, ok := f.services.Load(tenantID); ok {
		return v.(*xkmswebauthn.Service), nil
	}

	svc, err := f.buildService(tenantID)
	if err != nil {
		return nil, err
	}
	f.services.Store(tenantID, svc)
	return svc, nil
}

// buildService constructs a single per-tenant xkms webauthn service.
// The user and credential stores are wired through xkms's
// QRDBUserStoreAdapter (Phase 4), keyed by tenant id. The session
// store is the lightweight in-memory store that ships with xkms —
// ceremony sessions are short-lived (~60s) and process-local, so the
// in-memory implementation is a deliberate fit.
func (f *WebAuthnServiceFactory) buildService(tenantID string) (*xkmswebauthn.Service, error) {
	cfg := &xkmswebauthn.Config{
		RPID:                    f.params.Config.RPID,
		RPDisplayName:           f.params.Config.RPDisplayName,
		RPOrigins:               f.params.Config.RPOrigins,
		Timeout:                 f.params.Config.Timeout,
		UserVerification:        f.params.Config.UserVerification,
		AttestationPreference:   f.params.Config.AttestationPreference,
		ResidentKeyRequirement:  f.params.Config.ResidentKeyRequirement,
		AuthenticatorAttachment: f.params.Config.AuthenticatorAttachment,
		Debug:                   f.params.Config.Debug,
	}
	users, creds, err := xkmswebauthn.NewQRDBUserStoreAdapter(xkmswebauthn.QRDBUserStoreOptions{
		Users:    f.params.QRDB.Users(),
		KV:       f.params.QRDB.KV(),
		TenantID: tenantID,
	})
	if err != nil {
		return nil, err
	}
	sessions := xkmswebauthn.NewMemorySessionStore()
	return xkmswebauthn.NewService(xkmswebauthn.ServiceParams{
		Config:          cfg,
		UserStore:       users,
		SessionStore:    sessions,
		CredentialStore: creds,
		Logger:          f.logger.With("component", "iam.webauthn", "tenant_id", tenantID),
	})
}

// Reset clears the cached services. Tests use this to force fresh
// construction; production callers should not need it. The factory
// remains usable after Reset.
func (f *WebAuthnServiceFactory) Reset() {
	f.services.Range(func(k, _ any) bool {
		f.services.Delete(k)
		return true
	})
}

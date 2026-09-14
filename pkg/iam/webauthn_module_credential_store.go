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

package iam

import (
	"context"
	"log/slog"
	"sync/atomic"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// =============================================================================
// The composed go-xkms module's WebAuthn credential store
//
// WHAT THIS CLOSES
//
// A composed node serves TWO WebAuthn registration surfaces over ONE
// qrdb-backed credential store:
//
//	quicbaas  /api/v1/auth/register/*, /api/v1/iam/{tenant}/webauthn/*
//	go-xkms   /api/v1/webauthn/sdk/registration/*   (what `xkey auth register` posts to)
//
// The first is built by WebAuthnServiceFactory and decorates the store with
// webAuthnFactorWriteThrough, so a completed ceremony also enrols the qrdb
// fido2 auth factor the unified login ceremony resolves. The second is built
// by go-xkms itself and, until go-xkms grew a credential-store seam
// (go-xkms#350), could not be decorated at all: it persisted the credential
// and no factor, and every login with that passkey then failed
// "transport: factor not found" — go-quicbaas#484.
//
// This file is the composer side of that seam.
//
// WHY IT IS LATE-BOUND
//
// The go-xkms ServiceModule is registered on the qrdb node while the node is
// being CONSTRUCTED (pkg/server unifiedModuleServerOptions), and IAM — the
// WebAuthn relying-party factory included — is initialised afterwards, from
// the node's own client. So the decorator has to be handed to go-xkms before
// the thing it decorates with exists. This is the same ordering the login
// factor verifiers solve with an atomic late-bound shell
// (pkg/server/login_factor_verifiers.go), and it is solved the same way here.
//
// WHY IT FAILS CLOSED AT Save RATHER THAN AT COMPOSITION
//
// go-xkms's seam is `func(CredentialStore) CredentialStore` — it cannot report
// an error, and returning the undecorated store on failure would silently
// reproduce the exact split-brain bug this closes. The two honest dispositions
// are to fail composition or to fail the ceremony, and composition is not
// available: at composition time the factory legitimately does not exist yet,
// so there is nothing to fail on. Every registration is therefore refused with
// a typed error until the factory binds, and NOTHING is persisted — no
// credential, no factor, no residue. A registrant sees the ceremony fail; they
// never see it succeed and then find they cannot log in.
// =============================================================================

// WebAuthnFactorStoreUnboundError reports that a WebAuthn credential could not
// be persisted because the per-tenant relying-party factory the qrdb fido2
// write-through is built from is not bound yet (IAM initialisation has not
// completed, or completed without a WebAuthn surface).
//
// It is a fail-closed signal. Persisting the credential without it would leave
// a passkey that registers successfully and can never complete a login.
type WebAuthnFactorStoreUnboundError struct {
	// TenantID is the tenant the relying party being served is scoped to.
	TenantID string
}

// Error implements error.
func (e *WebAuthnFactorStoreUnboundError) Error() string {
	msg := "iam.webauthn.factor: the WebAuthn relying-party factory is not bound, " +
		"so a registration cannot enroll the login factor it requires"
	if e != nil && e.TenantID != "" {
		msg += " (tenant " + e.TenantID + ")"
	}
	return msg
}

// WebAuthnServiceFactoryResolver returns the per-tenant WebAuthn relying-party
// factory once IAM initialisation has produced one, and nil before that. It is
// the late-binding seam described in this file's commentary.
type WebAuthnServiceFactoryResolver func() *WebAuthnServiceFactory

// NewWebAuthnModuleCredentialStoreDecorator returns the credential-store
// decorator to hand go-xkms's ServiceModule
// (xkmssdk.WithServiceModuleWebAuthnCredentialStoreDecorator), so passkeys
// registered through the module's own ceremony routes enroll the same qrdb
// fido2 login factor quicbaas's own surface enrolls.
//
// tenantID must be the tenant the module's relying party is scoped to; the
// module builds ONE relying party over ONE tenant, so one decorator is correct
// for every registration it can ever see. resolve is called on the first
// mutation, not here — see the commentary above.
//
// It returns an error for what IS knowable at composition time (an empty
// tenant, a missing resolver) so the daemon refuses to start rather than
// serving a registration surface that will reject every ceremony.
func NewWebAuthnModuleCredentialStoreDecorator(
	tenantID string,
	resolve WebAuthnServiceFactoryResolver,
	logger *slog.Logger,
) (xkmssdk.ServiceModuleWebAuthnCredentialStoreDecorator, error) {
	if tenantID == "" {
		return nil, &WebAuthnFactorStoreConfigError{Field: "tenantID"}
	}
	if resolve == nil {
		return nil, &WebAuthnFactorStoreConfigError{Field: "WebAuthnServiceFactoryResolver"}
	}
	if logger == nil {
		logger = slog.Default()
	}
	build := func(inner xkmssdk.WebAuthnCredentialStore) (*webAuthnFactorWriteThrough, error) {
		factory := resolve()
		if factory == nil {
			return nil, &WebAuthnFactorStoreUnboundError{TenantID: tenantID}
		}
		decorated, err := factory.DecorateCredentialStore(inner, tenantID)
		if err != nil {
			return nil, err
		}
		// DecorateCredentialStore returns the interface; the concrete type is
		// what this file caches, and it is the only implementation that
		// constructor can produce.
		writeThrough, ok := decorated.(*webAuthnFactorWriteThrough)
		if !ok {
			return nil, &WebAuthnFactorStoreConfigError{Field: "CredentialStore"}
		}
		return writeThrough, nil
	}
	return func(inner xkmssdk.WebAuthnCredentialStore) xkmssdk.WebAuthnCredentialStore {
		if inner == nil {
			// go-xkms rejects a nil return with its own typed error and
			// leaves the WebAuthn routes unmounted. That is the right
			// disposition for a store we cannot wrap: no registration
			// surface at all beats one that enrolls half a passkey.
			logger.Error("xkms module webauthn: no credential store to decorate; " +
				"the module's WebAuthn routes will stay unmounted")
			return nil
		}
		return &lateBoundWebAuthnFactorStore{
			WebAuthnCredentialStore: inner,
			tenantID:                tenantID,
			build:                   build,
			logger:                  logger,
		}
	}, nil
}

// lateBoundWebAuthnFactorStore is the credential store go-xkms's relying party
// persists through on a composed node. Reads are the wrapped store's, promoted
// unchanged. Every MUTATION is routed through the qrdb fido2 write-through,
// which is constructed on first use from the late-bound relying-party factory
// and cached.
//
// Save, Delete and DeleteByUserID all fail closed while the factory is
// unbound: Save must not persist a credential whose login factor cannot be
// enrolled, and Delete must not strip a credential whose factor cannot be
// revoked (which would strand a factor resolving at login to a passkey that no
// longer exists — the same split in the other direction).
type lateBoundWebAuthnFactorStore struct {
	// The wrapped store. Embedded so GetByUserID, GetByCredentialID and
	// Update — none of which need factor bookkeeping — are promoted.
	xkmssdk.WebAuthnCredentialStore

	// tenantID is the tenant the decorated relying party is scoped to.
	tenantID string

	// build constructs the write-through from the late-bound factory.
	build func(inner xkmssdk.WebAuthnCredentialStore) (*webAuthnFactorWriteThrough, error)

	// resolved caches the constructed write-through. Two concurrent first
	// mutations may both build one; the loser's is discarded, and either is
	// equivalent — the type is a stateless wrapper over the same stores.
	resolved atomic.Pointer[webAuthnFactorWriteThrough]

	logger *slog.Logger
}

// Compile-time guard: the late-bound store satisfies the interface go-xkms's
// credential-store seam requires.
var _ xkmssdk.WebAuthnCredentialStore = (*lateBoundWebAuthnFactorStore)(nil)

// writeThrough returns the cached qrdb fido2 write-through, constructing it on
// first use. It returns a typed error — never a bare inner store — while the
// relying-party factory is unbound.
func (s *lateBoundWebAuthnFactorStore) writeThrough() (*webAuthnFactorWriteThrough, error) {
	if cached := s.resolved.Load(); cached != nil {
		return cached, nil
	}
	built, err := s.build(s.WebAuthnCredentialStore)
	if err != nil {
		return nil, err
	}
	if !s.resolved.CompareAndSwap(nil, built) {
		return s.resolved.Load(), nil
	}
	s.logger.Info("xkms module webauthn: credential store bound to the qrdb login-factor write-through",
		"tenant_id", s.tenantID)
	return built, nil
}

// Save persists the credential and enrols the matching qrdb fido2 login
// factor. It refuses — persisting nothing — while the factory is unbound.
func (s *lateBoundWebAuthnFactorStore) Save(ctx context.Context, cred *xkmssdk.WebAuthnCredential) error {
	wt, err := s.writeThrough()
	if err != nil {
		s.logger.Error("xkms module webauthn: refusing to persist a registration that cannot enroll its login factor",
			"tenant_id", s.tenantID, "error", err)
		return err
	}
	return wt.Save(ctx, cred)
}

// Delete removes the credential and revokes its qrdb fido2 login factor. It
// refuses — deleting nothing — while the factory is unbound.
func (s *lateBoundWebAuthnFactorStore) Delete(ctx context.Context, credID []byte) error {
	wt, err := s.writeThrough()
	if err != nil {
		s.logger.Error("xkms module webauthn: refusing to delete a credential whose login factor cannot be revoked",
			"tenant_id", s.tenantID, "error", err)
		return err
	}
	return wt.Delete(ctx, credID)
}

// DeleteByUserID removes every credential for a user, revoking each one's qrdb
// fido2 login factor. It refuses — deleting nothing — while the factory is
// unbound.
func (s *lateBoundWebAuthnFactorStore) DeleteByUserID(ctx context.Context, userID []byte) error {
	wt, err := s.writeThrough()
	if err != nil {
		s.logger.Error("xkms module webauthn: refusing to delete credentials whose login factors cannot be revoked",
			"tenant_id", s.tenantID, "error", err)
		return err
	}
	return wt.DeleteByUserID(ctx, userID)
}

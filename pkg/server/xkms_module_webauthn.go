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

package server

import (
	"log/slog"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// xkmsModuleWebAuthnTenantID is the qrdb tenant the composed go-xkms module's
// WebAuthn relying party is scoped to, and therefore the tenant its
// credential-store decorator must enroll login factors under.
//
// The module builds exactly ONE relying party, over ONE tenant: it never sets
// rest.HostConfig.WebAuthnTenantID (go-xkms pkg/apimodule module_host.go), so
// rest.BuildWebAuthnService falls back to rest.DefaultWebAuthnTenant, which is
// the qrdb system tenant. One decorator is consequently correct for every
// registration that surface can serve — there is no second tenant it could
// mis-attribute a factor to.
//
// It is spelled as qrdbsdk.SystemTenantID rather than a literal because that
// is the identifier both projects are naming; go-xkms/sdk/go does not
// re-export rest.DefaultWebAuthnTenant, so it cannot yet be sourced directly
// (requested on go-xkms#353). A divergence would not mis-file a login factor —
// the factor binds to the qrdb uint64 user id carried by the credential's user
// handle, not to this string — but it would scope the registrant lookup the
// factor label and the assurance gate read to the wrong tenant, so the
// coupling is named here rather than left implicit.
const xkmsModuleWebAuthnTenantID = qrdbsdk.SystemTenantID

// xkmsModuleWebAuthnCredentialStoreDecorator builds the credential-store
// decorator handed to the composed go-xkms ServiceModule.
//
// go-xkms serves its own WebAuthn registration ceremony at
// /api/v1/webauthn/sdk/registration/* — the routes `xkey auth register` posts
// to — through a relying party it builds itself over the SAME qrdb-backed
// credential store quicbaas's own relying party uses. Without this decoration
// that surface persists a credential and no qrdb fido2 auth factor, and the
// unified login ceremony then refuses the resulting assertion with
// "transport: factor not found": the passkey registers and can never log in
// (go-quicbaas#484, seam added by go-xkms#350).
//
// The factory is resolved late because this runs while the qrdb node is being
// constructed, before IAM initialisation exists to build one; see
// pkg/iam/webauthn_module_credential_store.go for why a failure to resolve
// fails the ceremony rather than silently handing back the undecorated store.
func (s *Server) xkmsModuleWebAuthnCredentialStoreDecorator() (xkmssdk.ServiceModuleWebAuthnCredentialStoreDecorator, error) {
	return iam.NewWebAuthnModuleCredentialStoreDecorator(
		xkmsModuleWebAuthnTenantID,
		s.webAuthnServiceFactory,
		slog.Default().With("component", "xkms.module.webauthn"),
	)
}

// webAuthnServiceFactory returns the per-tenant WebAuthn relying-party factory
// once IAM initialisation has bound one, and nil before that. It is the
// resolver half of the late binding described above.
func (s *Server) webAuthnServiceFactory() *iam.WebAuthnServiceFactory {
	if s == nil {
		return nil
	}
	return s.webauthnFactory.Load()
}

// bindXKMSModuleWebAuthnFactory publishes the per-tenant WebAuthn
// relying-party factory built during IAM initialisation, so consumers composed
// before it — the go-xkms module's credential-store decorator — can reach it.
//
// A node with no WebAuthn surface leaves it unbound, which is announced here
// rather than discovered by a registrant whose ceremony fails: go-xkms mounts
// its registration routes regardless of what quicbaas composed, so an unbound
// factory means those routes will refuse every registration.
func (s *Server) bindXKMSModuleWebAuthnFactory(c *iamComponents) {
	if c == nil || c.webauthnManager == nil {
		s.logger.Warn("xkms module webauthn: no WebAuthn manager was initialized, so registrations served by " +
			"the go-xkms module's ceremony routes will be refused (they cannot enroll a qrdb login factor)")
		return
	}
	factory := c.webauthnManager.Factory()
	if factory == nil {
		s.logger.Warn("xkms module webauthn: the WebAuthn manager carries no relying-party factory, so " +
			"registrations served by the go-xkms module's ceremony routes will be refused")
		return
	}
	s.webauthnFactory.Store(factory)
	s.logger.Infof("xkms module webauthn: credential-store write-through bound for tenant %q",
		xkmsModuleWebAuthnTenantID)
}

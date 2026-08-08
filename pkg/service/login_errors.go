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

package service

// LoginCeremonyUnavailableError is returned by the login facade when no
// in-process LoginClient is wired (login disabled, IAM absent, or the qrdb
// node not yet constructed). Transports map it to a 503 / Unavailable status.
type LoginCeremonyUnavailableError struct{}

func (e *LoginCeremonyUnavailableError) Error() string {
	return "login: ceremony unavailable (no in-process login client wired)"
}

// MTLSPeerCertRequiredError is returned when an mtls FinishLogin request
// arrives over a network transport with no verified peer certificate in the
// context and the context is not embedded-trusted. This is the fail-closed
// anti-spoofing guard: a network mTLS login can only succeed when the lookup
// key is derived from the verified TLS handshake.
type MTLSPeerCertRequiredError struct{}

func (e *MTLSPeerCertRequiredError) Error() string {
	return "login: mtls factor requires a verified peer certificate; refusing client-supplied lookup key"
}

// LoginRequestNilError is returned when a nil request is passed to a login
// facade method.
type LoginRequestNilError struct {
	// Op is the facade operation that received the nil request.
	Op string
}

func (e *LoginRequestNilError) Error() string {
	return "login: nil request for " + e.Op
}

// LoginRefreshIssuanceError reports that a login completed its factor ceremony
// but the session's renewal credential could not be minted. The login is
// rejected rather than handing back a bearer that dies at its first expiry.
type LoginRefreshIssuanceError struct {
	Cause error
}

func (e *LoginRefreshIssuanceError) Error() string {
	return "login ceremony: failed to issue session refresh credential: " + e.Cause.Error()
}

func (e *LoginRefreshIssuanceError) Unwrap() error { return e.Cause }

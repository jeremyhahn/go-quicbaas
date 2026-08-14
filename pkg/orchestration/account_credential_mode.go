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

package orchestration

// =============================================================================
// AccountCredentialMode semantics.
//
// The domain spells the two modes "oauth_refresh" / "static_token" while every
// transport (REST/gRPC/QUIC/MCP/CLI/SDK) still spells them "oauth2" / "none"
// (see account_types.go). Both spellings name the SAME mode, so no behavioral
// decision may compare a mode with == against one spelling: an account stored
// with the canonical spelling would then silently stop being refreshed.
//
// canonicalAccountCredentialModes is the single map every such decision goes
// through — an O(1) lookup rather than a switch that has to be kept in sync in
// several places.
// =============================================================================

// canonicalAccountCredentialModes maps every accepted spelling of a credential
// mode (canonical and transport alias alike) to its canonical form.
var canonicalAccountCredentialModes = map[AccountCredentialMode]AccountCredentialMode{
	AccountCredentialModeOAuthRefresh: AccountCredentialModeOAuthRefresh,
	AccountRefreshOAuth2:              AccountCredentialModeOAuthRefresh,
	AccountCredentialModeStaticToken:  AccountCredentialModeStaticToken,
	AccountRefreshNone:                AccountCredentialModeStaticToken,
}

// Canonical resolves m to its canonical AccountCredentialMode, accepting both
// the domain spelling and the transport alias. The second result is false for
// an empty or unrecognized mode, which callers must treat as a validation
// failure rather than guessing a default — see AccountCredentialMode's doc
// comment for why neither guess is safe.
func (m AccountCredentialMode) Canonical() (AccountCredentialMode, bool) {
	canonical, ok := canonicalAccountCredentialModes[m]
	return canonical, ok
}

// IsOAuthRefresh reports whether m selects the RFC 6749 refresh_token grant,
// under either spelling.
func (m AccountCredentialMode) IsOAuthRefresh() bool {
	canonical, ok := canonicalAccountCredentialModes[m]
	return ok && canonical == AccountCredentialModeOAuthRefresh
}

// IsStaticToken reports whether m selects the never-renewed mode, under either
// spelling.
func (m AccountCredentialMode) IsStaticToken() bool {
	canonical, ok := canonicalAccountCredentialModes[m]
	return ok && canonical == AccountCredentialModeStaticToken
}

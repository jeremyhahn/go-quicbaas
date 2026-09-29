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

import (
	"context"
	"errors"
	"strings"
)

// =============================================================================
// Authentication rejections — the account's LOGIN is dead, not its allowance.
//
// A provider that refuses the account's credential outright (401, "token is
// expired", "refresh token already used", "sign in again") is a different
// failure from a limit: nothing lifts it at a reset time. Only a person who
// logs the account in again does. Before this, such a turn failed with
// "codex (8591 bytes of stderr withheld)", the account stayed `active`, and
// every review dispatch walked into the same refusal every five minutes
// (go-codec watcher, 2026-09-28).
//
// THE CLASSIFICATION HAPPENS WHERE THE PROCESS RAN, as for a limit
// (account_limit_evidence.go). The backend that read the CLI's stderr decides,
// attaches ProviderAuthEvidence to the turn's TurnFailureEvidence, and the
// verdict crosses the daemon wire with the rest of it. The control plane then
// marks the account AccountStatusError with a last_error that says a re-login
// is needed, which takes it out of every selection and puts it in the
// operator's inbox. The turn's error comes back as TurnAccountAuthRejectedError,
// on which the SDLC runner rolls its session onto another account and a spawn
// fails over to the next Agent.
//
// The rules are narrow and fail in the cheap direction, like the limit rules:
// a missed rejection costs the failed turn it already cost, while a false one
// parks a working account until an operator clears it, so only phrases that
// name the login itself are matched.
// =============================================================================

// AccountReloginNeededPrefix begins the last_error of an account a provider
// refused for its login, so every surface that reads it — the inbox above
// all — can say what the operator has to do.
const AccountReloginNeededPrefix = "re-login needed: "

// ErrAccountAuthRejected indicates the provider refused the account's
// credential itself; it needs a new login.
var ErrAccountAuthRejected = errors.New("account: provider rejected the login")

// authRejectionPhrases name a dead login in the CLIs' own words. Matched
// case-insensitively against the provider's output, never QuicBaaS's wrapper.
var authRejectionPhrases = []string{
	// codex: a refresh against a rotated-away refresh token.
	"refresh token already used",
	"refresh_token_reused",
	"access token could not be refreshed",
	"please log out and sign in again",
	"authentication token is expired",
	// shared: the OAuth/API layer's own words.
	"token is expired",
	"token has expired",
	"invalid_grant",
	"invalid api key",
	"invalid x-api-key",
	"authentication_error",
	// claude: the CLI's prompt to log in again.
	"please run /login",
	"oauth token has expired",
}

// authStatusContext are the words that make a bare "401" a status code rather
// than a number, mirroring httpStatusLimitContext.
var authStatusContext = []string{"unauthorized", "status", "http", "auth"}

// ProviderAuthEvidence is a backend's verdict that the provider refused the
// account's login.
type ProviderAuthEvidence struct {
	// Signal is the provider's own refusal text, redacted and truncated by
	// redactLimitSignal. It is what the account's last_error carries.
	Signal string
}

// canonical returns the evidence with its signal redacted, so text read off
// the wire is never stored as it arrived.
func (e *ProviderAuthEvidence) canonical() *ProviderAuthEvidence {
	if e == nil {
		return nil
	}
	return &ProviderAuthEvidence{Signal: redactLimitSignal(e.Signal)}
}

// providerAuthEvidence classifies text — a CLI's stderr or failed result —
// and returns the evidence of an authentication rejection, or nil.
func providerAuthEvidence(text string) *ProviderAuthEvidence {
	if !authRejected(text) {
		return nil
	}
	return &ProviderAuthEvidence{Signal: redactLimitSignal(text)}
}

// authRejected reports whether text names a dead login.
func authRejected(text string) bool {
	if text == "" {
		return false
	}
	lowered := strings.ToLower(text)
	for _, phrase := range authRejectionPhrases {
		if strings.Contains(lowered, phrase) {
			return true
		}
	}
	return statusCodePresent(lowered, "401", authStatusContext)
}

// statusCodePresent reports whether code stands alone as a number in lowered
// with one of the context words somewhere in it. It shares
// httpStatusLimitPattern's boundary rule: digits inside a longer token are not
// a status.
func statusCodePresent(lowered, code string, context []string) bool {
	if !limitPhrasePresent(lowered, context) {
		return false
	}
	for idx := strings.Index(lowered, code); idx >= 0; {
		before := idx == 0 || !isIdentifierByte(lowered[idx-1])
		end := idx + len(code)
		after := end == len(lowered) || !isIdentifierByte(lowered[end])
		if before && after {
			return true
		}
		next := strings.Index(lowered[end:], code)
		if next < 0 {
			return false
		}
		idx = end + next
	}
	return false
}

// isIdentifierByte reports whether c continues a token: a status code next to
// one of these is part of a longer identifier.
func isIdentifierByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}

// AccountAuthRejectedError reports an account whose login the provider
// refused.
type AccountAuthRejectedError struct {
	AccountID string
	Provider  AccountProvider

	// Signal is the redacted refusal text.
	Signal string
}

// Error implements the error interface.
func (e *AccountAuthRejectedError) Error() string {
	return ErrAccountAuthRejected.Error() + ": account " + e.AccountID + " (" + string(e.Provider) +
		") needs a new login"
}

// Unwrap returns the sentinel.
func (e *AccountAuthRejectedError) Unwrap() error { return ErrAccountAuthRejected }

// TurnAccountAuthRejectedError is a turn that failed because the provider
// refused the session's account login. It unwraps to both the rejection and
// the turn's own error, so evidence readers keep working.
type TurnAccountAuthRejectedError struct {
	SessionID string
	Rejection *AccountAuthRejectedError
	Cause     error
}

// Error implements the error interface.
func (e *TurnAccountAuthRejectedError) Error() string {
	msg := "session " + e.SessionID + ": " + e.Rejection.Error()
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the rejection and the turn's error.
func (e *TurnAccountAuthRejectedError) Unwrap() []error {
	return []error{e.Rejection, e.Cause}
}

// AccountAuthRejectionOf returns the rejection err carries, when it carries
// one.
func AccountAuthRejectionOf(err error) (*AccountAuthRejectedError, bool) {
	var rejected *AccountAuthRejectedError
	if errors.As(err, &rejected) && rejected != nil {
		return rejected, true
	}
	return nil, false
}

// authRejectionSignal decides whether a failed turn was an authentication
// rejection: the backend's carried verdict first, then — for an in-process
// backend whose error still holds the provider's text — the same rules over
// that text.
func authRejectionSignal(turnErr error) (string, bool) {
	if evidence, carried := EvidenceOf(turnErr); carried && evidence.ProviderAuth != nil {
		return evidence.ProviderAuth.canonical().Signal, true
	}
	text := limitFailureText(turnErr)
	if text == turnErr.Error() {
		// Only QuicBaaS's own wrapper is left; its vocabulary is not the
		// provider's, so it is not classified.
		return "", false
	}
	if !authRejected(text) {
		return "", false
	}
	return redactLimitSignal(text), true
}

// errorOnAccountAuthRejection marks the session's account errored when the
// turn failed because the provider refused its login, and returns the turn's
// error marked as such. Any other failure is returned untouched.
//
// The account leaves every selection (AccountStatusError is never
// selectable), its last_error says a re-login is needed, and the inbox's
// account alert shows it — so no later spawn walks into the same refusal.
func (s *agentServiceImpl) errorOnAccountAuthRejection(ctx context.Context, sessionID string, turnErr error) error {
	if sessionID == "" || turnErr == nil {
		return turnErr
	}
	if _, limited := AccountLimitOf(turnErr); limited {
		return turnErr
	}
	mem, ok := s.liveSessionRecord(sessionID)
	if !ok || mem.AccountID() == "" {
		return turnErr
	}
	signal, rejected := authRejectionSignal(turnErr)
	if !rejected {
		return turnErr
	}
	accountID := mem.AccountID()
	provider := AccountProvider(mem.Backend)
	s.markAccountLoginDead(ctx, mem.TenantID, accountID, signal)
	return &TurnAccountAuthRejectedError{
		SessionID: sessionID,
		Rejection: &AccountAuthRejectedError{AccountID: accountID, Provider: provider, Signal: signal},
		Cause:     turnErr,
	}
}

// markAccountLoginDead puts the account in AccountStatusError with a
// last_error naming the re-login. A revoked account stays revoked.
func (s *agentServiceImpl) markAccountLoginDead(ctx context.Context, tenantID, accountID, signal string) {
	if s.accountStore == nil {
		return
	}
	err := s.accountStore.Touch(ctx, tenantID, accountID, TouchAccountOptions{
		SetStatusValid:  true,
		SetStatus:       AccountStatusError,
		SetLastError:    AccountReloginNeededPrefix + signal,
		PreserveRevoked: true,
	})
	var revoked *AccountRevokedError
	if err != nil && !errors.As(err, &revoked) {
		s.logger.Warnf("orchestration: failed to mark account %s as needing a re-login: %s", accountID, err.Error())
		return
	}
	s.logger.Warnf("orchestration: account %s (tenant %s) needs a re-login; it is out of selection until an operator fixes it",
		accountID, tenantID)
}

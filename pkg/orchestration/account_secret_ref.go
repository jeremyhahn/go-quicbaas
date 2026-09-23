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
	"path"
	"strings"
	"unicode"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Credential secret references.
//
// An account holds its credential in exactly one of two places:
//
//   - INLINE: the operator hands the envelope to create/rotate and it lives in
//     the account's own private envelope (AccountSecretStore). This is how
//     every account worked before references existed, and it keeps working.
//
//   - BY REFERENCE: Account.CredentialSecretRef names a secret in the account
//     tenant's own secrets keyspace — the SAME store and the SAME short names
//     an SDLC binding's forge.token_secret_ref uses — and the value is read
//     through AccountSecretResolver every time the credential is resolved.
//     Nothing is copied onto the account and nothing is cached: writing a new
//     version of the secret rotates every later spawn without touching the
//     account.
//
// The resolver is a PORT. pkg/service implements it over SecretsService, which
// scopes the read by the tenant it is handed — the account's tenant, never the
// caller's — so a system administrator who provisions an account for tenant T
// gets T's secret, not the system tenant's (the mis-homing go-quicbaas#535
// measured for the account record itself).
//
// A worker host never sees a reference. The agent daemon receives the RESOLVED
// envelope through the materialize verb, and its account wire shape carries no
// reference field (pkg/agent/account_client.go), so a daemon has neither the
// name nor any route to the store.
// =============================================================================

// AccountSecretResolver reads the value of a tenant secret by its short name.
//
// ref is tenant-relative (e.g. "agents/claude/claude1/oauth_token"); the
// implementation is responsible for addressing it inside tenantID's keyspace
// and nowhere else. The returned bytes are the secret's value verbatim.
type AccountSecretResolver interface {
	ResolveAccountSecret(ctx context.Context, tenantID, ref string) ([]byte, error)
}

// WithAccountSecretResolver installs the resolver AccountTokenManager reads a
// referenced credential through. Without one, resolving an account that names
// a CredentialSecretRef fails with AccountSecretResolverNotConfiguredError
// rather than silently reading the (absent) inline envelope.
func WithAccountSecretResolver(resolver AccountSecretResolver) AccountTokenManagerOption {
	return func(m *AccountTokenManager) {
		m.secretRefs = resolver
	}
}

// accountSecretTenantPrefix is the storage prefix the secrets service puts in
// front of every tenant's secrets. A reference that already carries it is the
// literal-key workaround go-quicbaas#534 forced on operators; resolving it
// would address tenant/<id>/tenant/<id>/..., so it is refused at write time
// with a message naming the short form.
const accountSecretTenantPrefix = "tenant/"

// UsesSecretRef reports whether the account reads its credential from a
// tenant secret rather than an inline envelope.
func (a *Account) UsesSecretRef() bool {
	return a != nil && a.CredentialSecretRef != ""
}

// ValidateAccountCredentialSource enforces, at write time, the rules for where
// an account's credential comes from:
//
//   - exactly one source: an inline credential or a secret reference;
//   - a credential file is meaningful only with a reference;
//   - a referenced credential is never renewed by QuicBaaS, so the account must
//     be static_token (renewal would have to write a rotated refresh token back
//     into a secret the operator owns);
//   - the reference must be a short, tenant-relative secret name;
//   - the value must land somewhere: a provider access-token environment
//     variable, or the named credential file.
//
// hasInline reports whether the request carried an inline credential.
func (r *AccountProviderRegistry) ValidateAccountCredentialSource(provider AccountProvider, mode AccountCredentialMode, hasInline bool, ref, file string) error {
	if hasInline && ref != "" {
		return &AccountCredentialSourceError{
			Field:   "credential_secret_ref",
			Message: "an account takes an inline credential or a credential_secret_ref, not both",
		}
	}
	if !hasInline && ref == "" {
		return &AccountCredentialSourceError{
			Field:   "credential",
			Message: "an account needs a credential: supply an inline credential or a credential_secret_ref",
		}
	}
	if ref == "" {
		if file != "" {
			return &AccountCredentialSourceError{
				Field:   "credential_secret_file",
				Message: "credential_secret_file names where a referenced secret is written; it needs a credential_secret_ref",
			}
		}
		return nil
	}
	return r.validateSecretRef(provider, mode, ref, file)
}

// validateSecretRef checks a reference-sourced account's mode, name and
// projection target.
func (r *AccountProviderRegistry) validateSecretRef(provider AccountProvider, mode AccountCredentialMode, ref, file string) error {
	canonical, ok := mode.Canonical()
	if !ok {
		return &AccountCredentialModeInvalidError{Mode: string(mode)}
	}
	if canonical != AccountCredentialModeStaticToken {
		return &AccountCredentialSourceError{
			Field:   "refresh_policy",
			Message: "a credential_secret_ref account is never renewed by QuicBaaS; use refresh_policy none and rotate by writing the secret",
		}
	}
	if err := ValidateAccountSecretRef(ref); err != nil {
		return err
	}
	if file != "" {
		return ValidateAccountSecretFile(file)
	}
	descriptor, known := r.Descriptor(provider)
	if !known || descriptor.AccessTokenEnvKey == "" {
		return &AccountCredentialSourceError{
			Field: "credential_secret_file",
			Message: "provider " + string(provider) + " has no access-token environment variable, " +
				"so a referenced secret needs credential_secret_file naming where the provider reads it (e.g. .codex/auth.json)",
		}
	}
	return nil
}

// ValidateAccountSecretRef checks that ref is a short, tenant-relative secret
// name: slash-separated segments, none empty, none "." or "..", no whitespace
// or control characters, and not the tenant-prefixed storage path.
func ValidateAccountSecretRef(ref string) error {
	invalid := func(reason string) error {
		return &AccountCredentialSourceError{Field: "credential_secret_ref", Message: reason}
	}
	if ref == "" {
		return invalid("must not be empty")
	}
	if strings.HasPrefix(ref, accountSecretTenantPrefix) {
		return invalid("name the secret by its short, tenant-relative name (e.g. agents/claude/claude1/oauth_token); " +
			"the tenant/<id>/ storage prefix is added by the secrets store")
	}
	for _, r := range ref {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return invalid("must not contain whitespace or control characters")
		}
	}
	for _, segment := range strings.Split(ref, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return invalid("must be slash-separated names with no empty, '.' or '..' segment")
		}
	}
	return nil
}

// ValidateAccountSecretFile checks that file is a clean path relative to the
// per-session home directory the credential is materialized into.
func ValidateAccountSecretFile(file string) error {
	invalid := func(reason string) error {
		return &AccountCredentialSourceError{Field: "credential_secret_file", Message: reason}
	}
	if strings.HasPrefix(file, "/") {
		return invalid("must be relative to the session home directory, not absolute")
	}
	if path.Clean(file) != file || file == "." {
		return invalid("must be a clean relative path (no '.', '..', or repeated separators)")
	}
	if file == ".." || strings.HasPrefix(file, "../") {
		return invalid("must not escape the session home directory")
	}
	return nil
}

// credentialFromSecret builds the stored-shape envelope a referenced secret
// value stands for. With a credential file the value is that file's content,
// verbatim; otherwise it is the access token, with the surrounding whitespace a
// `secrets put --from-stdin` of an env-file line or a shell echo leaves behind
// trimmed off.
func credentialFromSecret(account *Account, value []byte) (*AccountCredential, error) {
	if account.CredentialSecretFile != "" {
		if len(value) == 0 {
			return nil, errAccountSecretEmpty
		}
		content := make([]byte, len(value))
		copy(content, value)
		return &AccountCredential{Files: map[string][]byte{account.CredentialSecretFile: content}}, nil
	}
	token := strings.TrimSpace(string(value))
	if token == "" {
		return nil, errAccountSecretEmpty
	}
	return &AccountCredential{AccessToken: token}, nil
}

// errAccountSecretEmpty is the cause recorded when a referenced secret exists
// but holds nothing usable.
var errAccountSecretEmpty = errors.New("secret is empty")

// ReadAccountSecretRef reads the secret account names and builds the
// credential it stands for, WITHOUT recording anything on the account.
//
// It is the one read both sides use: the token manager at spawn time (which
// then records a failure on the account) and the account service at write time
// (which refuses to save a reference that does not resolve, so the failure is
// the operator's request rather than the next spawn). A failure is an
// AccountSecretRefUnresolvedError; the caller's own context ending is returned
// as the context's error, because that is not the secret's fault.
func ReadAccountSecretRef(ctx context.Context, resolver AccountSecretResolver, account *Account) (*AccountCredential, error) {
	if resolver == nil {
		return nil, &AccountSecretResolverNotConfiguredError{AccountID: account.ID}
	}
	value, err := resolver.ResolveAccountSecret(ctx, account.TenantID, account.CredentialSecretRef)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, newAccountSecretRefUnresolvedError(account, err)
	}
	cred, err := credentialFromSecret(account, value)
	if err != nil {
		return nil, newAccountSecretRefUnresolvedError(account, err)
	}
	return cred, nil
}

// resolveSecretRef reads a reference-sourced account's credential for a
// spawn.
//
// A failure that is the secret's — absent, empty, disabled, in another
// tenant — puts the account in AccountStatusError with a last_error naming the
// reference, so it drops out of selection and shows up where an operator
// looks (the attention inbox reads AccountStatusError), instead of every spawn
// rediscovering it.
func (m *AccountTokenManager) resolveSecretRef(ctx context.Context, account *Account) (*AccountCredential, error) {
	cred, err := ReadAccountSecretRef(ctx, m.secretRefs, account)
	if err != nil {
		var unresolved *AccountSecretRefUnresolvedError
		if errors.As(err, &unresolved) {
			// Conditional on the account still reading the reference this
			// read attempted, as last applied, and on it not being revoked:
			// an operator who re-pointed or re-applied the reference while
			// this read was failing has already answered it, and an error
			// stamp must never replace revoked. A refused stamp is the right
			// outcome, so its error is not this spawn's failure. A stamp that
			// FAILED is reported alongside the read failure it was recording.
			source := account.Source()
			stampErr := m.touchRefreshOutcome(ctx, account, AccountRefreshOutcomeFailure, TouchAccountOptions{
				SetStatusValid:  true,
				SetStatus:       AccountStatusError,
				SetLastError:    unresolved.OperatorMessage(),
				RequireSource:   &source,
				PreserveRevoked: true,
			})
			var stampFailed *AccountRefreshStampFailedError
			if errors.As(stampErr, &stampFailed) {
				return nil, withStampFailure(err, stampFailed)
			}
		}
		return nil, err
	}
	return m.providers.Effective(account.Provider, cred), nil
}

// newAccountSecretRefUnresolvedError builds the typed failure for account's
// reference.
func newAccountSecretRefUnresolvedError(account *Account, cause error) *AccountSecretRefUnresolvedError {
	return &AccountSecretRefUnresolvedError{
		TenantID:  account.TenantID,
		AccountID: account.ID,
		Ref:       account.CredentialSecretRef,
		Reason:    secretRefFailureReason(cause),
		Cause:     cause,
	}
}

// secretRefFailureReason names the failure class without carrying the
// underlying error text, which is never written onto the account record.
func secretRefFailureReason(cause error) string {
	switch {
	case errors.Is(cause, errAccountSecretEmpty):
		return "the secret is empty"
	case qerrors.IsNotFound(cause):
		return "the secret does not exist in this tenant"
	default:
		return "the secret could not be read"
	}
}

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrAccountCredentialSource indicates an account's credential source is
	// malformed: both or neither of inline and reference, a reference on a
	// refreshable account, or a bad reference name or file.
	ErrAccountCredentialSource = errors.New("account: invalid credential source")

	// ErrAccountSecretResolverNotConfigured indicates an account names a
	// credential secret but no AccountSecretResolver was wired.
	ErrAccountSecretResolverNotConfigured = errors.New("account: credential secret resolver not configured")
)

// AccountCredentialSourceError reports a malformed credential source.
type AccountCredentialSourceError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *AccountCredentialSourceError) Error() string {
	return ErrAccountCredentialSource.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel, and ErrAccountInvalid: every source error is a
// validation failure.
func (e *AccountCredentialSourceError) Unwrap() []error {
	return []error{ErrAccountCredentialSource, ErrAccountInvalid}
}

// AccountSecretResolverNotConfiguredError reports an account that names a
// credential secret on a node with no resolver wired.
type AccountSecretResolverNotConfiguredError struct {
	AccountID string
}

// Error implements the error interface.
func (e *AccountSecretResolverNotConfiguredError) Error() string {
	return ErrAccountSecretResolverNotConfigured.Error() + ": id=" + e.AccountID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountSecretResolverNotConfiguredError) Unwrap() error {
	return ErrAccountSecretResolverNotConfigured
}

// AccountSecretRefUnresolvedError reports that the secret an account's
// credential is referenced from could not be turned into a credential.
//
// IT UNWRAPS TO ErrAccountCredentialNotFound. The account's metadata exists and
// its credential does not — the exact condition that sentinel names — so the
// spawn path parks the work uncharged (FaultAccountCredentialMissing) instead
// of retrying into the same refusal.
type AccountSecretRefUnresolvedError struct {
	TenantID  string
	AccountID string
	Ref       string

	// Reason is the operator-facing failure class; it never carries secret
	// material or the underlying error text.
	Reason string

	// Cause is the resolver's error, kept for errors.As.
	Cause error
}

// Error implements the error interface.
func (e *AccountSecretRefUnresolvedError) Error() string {
	return ErrAccountCredentialNotFound.Error() + ": tenant=" + e.TenantID + " id=" + e.AccountID +
		": credential_secret_ref " + e.Ref + ": " + e.Reason
}

// OperatorMessage is the text recorded as the account's last_error.
func (e *AccountSecretRefUnresolvedError) OperatorMessage() string {
	return "credential_secret_ref " + e.Ref + ": " + e.Reason +
		"; write the secret, then re-apply the reference (account update --credential-secret-ref) to return the account to service"
}

// Unwrap returns the sentinel ONLY, never the cause.
//
// The cause is the secrets service's own error, and it can carry that
// backend's diagnostics — a quicbaas QBError with its context map. A transport
// that walks the chain for the innermost structured error (REST's
// newErrorResponse does) would serialize those diagnostics to the caller in
// place of this error's deliberately sanitized message. The cause stays on the
// Cause field for code in this process that needs it, and Reason carries the
// operator-facing class.
func (e *AccountSecretRefUnresolvedError) Unwrap() error {
	return ErrAccountCredentialNotFound
}

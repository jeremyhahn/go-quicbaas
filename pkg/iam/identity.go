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
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"

	"github.com/google/uuid"
)

// Email validation regex (RFC 5322 simplified).
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// Username validation: alphanumeric, dash, underscore, 3-64 chars.
var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,64}$`)

// IdentityStore manages users and roles with multi-tenant support.
type IdentityStore struct {
	userStorage UserStorage
	roleStorage RoleStorage
	tenants     TenantManager

	// webauthnCredentials is the cascade seam DeleteUser tears a user's
	// WebAuthn credentials down through. Late-bound because the WebAuthn
	// manager is constructed after the identity store in the server
	// lifecycle; nil when the deployment runs without WebAuthn.
	webauthnCredentials WebAuthnCredentialReaper

	// webauthnCredentialLister is the read half of the same seam, used by
	// UpdateUser to say in the log what a rename did to the user's passkeys.
	// It is resolved from the reaper rather than wired separately — see
	// SetWebAuthnCredentialReaper.
	webauthnCredentialLister WebAuthnCredentialLister

	// logger records what an update did to a user's enrolled credentials.
	// Optional: log() falls back to the process default when it is unset.
	logger *slog.Logger
}

// SetWebAuthnCredentialReaper wires the cascade DeleteUser runs before removing
// a user, so the user's WebAuthn credentials do not outlive the account they
// were registered for. It mirrors XKMSWebAuthnManager's late-binding setters:
// the WebAuthn manager is built later in the server lifecycle than the identity
// store. Passing nil disables the cascade.
//
// A reaper that can also LIST credentials additionally supplies the count the
// rename log reports. The production reaper (*XKMSWebAuthnManager) satisfies
// both, so resolving the lister here rather than through a second setter is
// what makes the log line impossible to leave unwired — a deployment cannot
// enable the cascade and forget the accounting.
func (s *IdentityStore) SetWebAuthnCredentialReaper(reaper WebAuthnCredentialReaper) {
	s.webauthnCredentials = reaper
	lister, _ := reaper.(WebAuthnCredentialLister)
	s.webauthnCredentialLister = lister
}

// NewIdentityStore creates a new identity store with the provided options.
func NewIdentityStore(opts IdentityStoreOptions) *IdentityStore {
	return &IdentityStore{
		userStorage: opts.UserStorage,
		roleStorage: opts.RoleStorage,
		tenants:     opts.Tenants,
		logger:      opts.Logger,
	}
}

// log returns the store's logger, or the process default when the store was
// built without one.
func (s *IdentityStore) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

// GetTenant retrieves a tenant by ID. This is used by REST handlers to validate
// tenant existence before listing resources.
func (s *IdentityStore) GetTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	if s.tenants == nil {
		return nil, api.NewServiceUnavailableError("tenant manager")
	}
	return s.tenants.GetTenant(ctx, tenantID)
}

// validateEmail checks if an email address is valid.
func validateEmail(email string) error {
	if email == "" {
		return &ValidationError{Field: "email", Message: "email cannot be empty"}
	}
	if !emailRegex.MatchString(email) {
		return &ValidationError{Field: "email", Message: "invalid email format"}
	}
	return nil
}

// validateUsername checks if a username is valid.
func validateUsername(username string) error {
	if username == "" {
		return &ValidationError{Field: "username", Message: "username cannot be empty"}
	}
	if !usernameRegex.MatchString(username) {
		return &ValidationError{Field: "username", Message: "username must be 3-64 alphanumeric characters, dash, or underscore"}
	}
	return nil
}

// validateDisplayName rejects a display name shaped like an email address.
//
// The guard is load-bearing because the qrdb username is DERIVED from Name
// (iam.User.LoginName), and a login ceremony's subject is that username. An
// email-shaped Name therefore lets a principal present a subject that
// resolveSubject — seeing an email shape — resolves through the EMAIL index, to
// a different account. The holder of that name authenticates honestly with their
// own factor and is handed somebody else's id and roles.
//
// The disjoint-shape rule in resolveSubject is what makes the two indexes safe
// to consult, and it only works if the shapes really are disjoint: qrdb usernames
// cannot contain "@" (validateUsername's charset), so barring the shape here is
// the other half of that guarantee. It was briefly removed on the reasoning that
// deriving the username from the canonical email made it unnecessary — true of
// that derivation, but the derivation was reverted (a username has to stay the
// name an administrator types), and the guard has to come back with it.
func validateDisplayName(name string) error {
	if validateEmail(strings.TrimSpace(name)) == nil {
		return &ValidationError{
			Field:   "name",
			Message: "display name must not be an email address",
		}
	}
	return nil
}

// validateTenantID checks if a tenant ID is valid and not empty.
func validateTenantID(tenantID string) error {
	if tenantID == "" {
		return &ValidationError{Field: "tenant_id", Message: "tenant ID cannot be empty"}
	}
	return nil
}

// validateUserID checks if a user ID is valid and not empty.
func validateUserID(userID string) error {
	if userID == "" {
		return &ValidationError{Field: "user_id", Message: "user ID cannot be empty"}
	}
	return nil
}

// validateRoleID checks if a role ID is valid and not empty.
func validateRoleID(roleID string) error {
	if roleID == "" {
		return &ValidationError{Field: "role_id", Message: "role ID cannot be empty"}
	}
	return nil
}

// validateRoleName checks that a role name is present. It is the lookup-side
// counterpart of the CreateRole name check, so a caller that resolves by name
// gets a field-named validation error instead of a store miss.
func validateRoleName(name string) error {
	if name == "" {
		return &ValidationError{Field: "name", Message: "role name cannot be empty"}
	}
	return nil
}

// supportedAuthMethods is the closed set of credential kinds an account may
// hold, in the order they are reported to a caller that names one outside it.
// QuicBaaS is passwordless: WebAuthn and PIV are the only mechanisms that can
// move an account out of `pending`, so there is no third value.
var supportedAuthMethods = []string{AuthMethodWebAuthn, AuthMethodPIV}

// supportedCreateLifecycles is the closed set of states a user may be CREATED
// in, and it is qrdb's set rather than one restated here: suspended and locked
// are things that HAPPEN to an account, not states it can be born in.
//
// UpdateUser has its own set (supportedUpdateLifecycles) rather than this one:
// an account CAN be suspended or locked by an administrator, it just cannot be
// born that way.
var supportedCreateLifecycles = []Lifecycle{LifecyclePending, LifecycleActive, LifecycleDisabled}

// supportedUpdateLifecycles is the closed set of states a caller may assert
// through the PUBLIC update surface. It is every state except `deleting`.
//
// The transition table is not enough on its own here, and the reasoning that
// said it was is the defect. active -> deleting is a LEGAL move — it is how a
// deletion begins — so the table admits it, and every transport that exposes
// lifecycle as an ordinary update field therefore let a caller start a deletion
// that never runs one. `deleting` is terminal, so the record could not be
// repaired by a later update: the account was permanently unable to
// authenticate while its credentials and sessions stayed
// live, because the cascade that removes them lives in DeleteUser.
//
// The deletion workflow does not go through here. It claims the record with
// MarkUserDeleting — a lifecycle-only storage op, separate precisely because a
// terminal record refuses a full-record write — so reserving the state costs
// the workflow nothing.
var supportedUpdateLifecycles = []Lifecycle{
	LifecyclePending,
	LifecycleActive,
	LifecycleSuspended,
	LifecycleLocked,
	LifecycleDisabled,
}

// validateUpdateUserLifecycle rejects a state the public update surface must not
// assert. Whether the move is LEGAL from where the record currently sits is a
// separate question, and still qrdb's to answer on the way to the store; this
// only decides which states a caller may name at all.
func validateUpdateUserLifecycle(state Lifecycle) error {
	for _, supported := range supportedUpdateLifecycles {
		if state == supported {
			return nil
		}
	}
	if state == LifecycleDeleting {
		return fieldValidationError("lifecycle",
			"deleting is not an assignable state; it is claimed by the deletion "+
				"workflow, which then removes the credentials and sessions "+
				"the account owns. Asserting it here would strand "+
				"the account in a terminal state with those still live: delete "+
				"the user instead")
	}
	names := make([]string, 0, len(supportedUpdateLifecycles))
	for _, supported := range supportedUpdateLifecycles {
		names = append(names, string(supported))
	}
	return fieldValidationError("lifecycle",
		"unsupported user state "+strconv.Quote(string(state))+
			"; supported: "+strings.Join(names, ", "))
}

// fieldValidationError wraps a field validation failure in the coded error
// every transport already maps to 400 / InvalidArgument.
//
// The neighbouring validators (validateEmail, validateUsername) return a bare
// *ValidationError, which the REST error mapper does not recognise and reports
// as 500 (#335); that gap is filed rather than widened here. errors.As still
// reaches the *ValidationError, so the field name survives.
func fieldValidationError(field, message string) error {
	return api.NewInvalidInputCauseError("invalid "+field,
		&ValidationError{Field: field, Message: message})
}

// validateAuthMethods rejects any authentication method outside the closed set
// and returns the list normalized: whitespace trimmed, duplicates dropped,
// order preserved. An empty input is left empty for the caller to default.
//
// The check exists because the option became effective. While CreateUser
// silently discarded AuthMethods, an unsupported value was harmless; now it is
// persisted, and a user created with one — "password", say — is not merely
// mislabelled. The invite delivered to them advertises an activation mechanism
// that does not exist, and no code path can activate the account: ActivateFIDO2
// and PIVActivator are the only two, and neither answers to any other name. The
// account can never complete enrollment, which is the live failure this repo
// hit with burnin-operator.
func validateAuthMethods(methods []string) ([]string, error) {
	if len(methods) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(methods))
	seen := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		name := strings.TrimSpace(method)
		if name == "" {
			return nil, fieldValidationError("auth_methods",
				"an authentication method cannot be blank")
		}
		if !isSupportedAuthMethod(name) {
			return nil, fieldValidationError("auth_methods",
				"unsupported authentication method "+strconv.Quote(name)+
					"; supported: "+strings.Join(supportedAuthMethods, ", "))
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	return normalized, nil
}

// isSupportedAuthMethod reports whether name is one of the credential kinds an
// account may hold.
func isSupportedAuthMethod(name string) bool {
	for _, method := range supportedAuthMethods {
		if method == name {
			return true
		}
	}
	return false
}

// validateCreateUserState rejects a lifecycle state outside the closed set. An
// empty state is accepted: CreateUser defaults it to pending.
//
// Same reason as validateAuthMethods — State was dropped before and is written
// now. A misspelled state is not inert: PIVActivator refuses a user that is not
// exactly `pending`, and the "may this user authenticate" check excludes only
// `disabled`, so a typo produces an account that can neither be activated nor
// be kept out by the state it claims to be in.
func validateCreateUserLifecycle(state Lifecycle) error {
	if state == "" {
		return nil
	}
	for _, supported := range supportedCreateLifecycles {
		if state == supported {
			return nil
		}
	}
	names := make([]string, 0, len(supportedCreateLifecycles))
	for _, supported := range supportedCreateLifecycles {
		names = append(names, string(supported))
	}
	return fieldValidationError("state",
		"unsupported user state "+strconv.Quote(string(state))+
			"; supported: "+strings.Join(names, ", "))
}

// CreateUser creates a new user in the specified tenant.
func (s *IdentityStore) CreateUser(ctx context.Context, tenantID string, opts CreateUserOptions) (*User, error) {
	// Validate inputs
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateEmail(opts.Email); err != nil {
		return nil, err
	}
	if err := validateUsername(opts.Username); err != nil {
		return nil, err
	}

	// Both of these are validated here, above every write, so a rejected value
	// leaves no user behind. Validating them next to the record literal would
	// still return the right error and still be wrong: the tenant quota has
	// been consumed by then and, once this seam grows an invite (#293), a
	// token would have been minted for an account that is about to be refused.
	authMethods, err := validateAuthMethods(opts.AuthMethods)
	if err != nil {
		return nil, err
	}
	if err := validateCreateUserLifecycle(opts.Lifecycle); err != nil {
		return nil, err
	}

	// Pre-validate requested role references BEFORE any write. Every
	// transport (REST, QUIC, MCP, embedded, and gRPC/Unix via the IAM
	// service adapter) converges on this seam, so a bad role ID fails fast
	// with the storage backend's typed not-found error and no
	// partially-created user is left behind — the create stays atomic from
	// the caller's perspective (a retry does not fail with already_exists).
	for _, roleID := range opts.Roles {
		if _, err := s.GetRole(ctx, tenantID, roleID); err != nil {
			return nil, err
		}
	}

	// Check tenant quota
	if s.tenants != nil {
		if err := s.tenants.CheckQuota(ctx, tenantID, QuotaResourceUsers); err != nil {
			return nil, err
		}
	}

	// Check email uniqueness within the tenant — the scope qrdb's canonical
	// index actually enforces. A global check would refuse an address its owner
	// is entitled to hold in a second tenant.
	existing, err := s.userStorage.GetUserByEmail(ctx, tenantID, opts.Email)
	if err == nil && existing != nil {
		return nil, ErrEmailAlreadyExists
	}
	// Ignore not-found errors (expected for new user creation)
	if err != nil && !dberrors.IsNotFound(err) {
		return nil, api.NewOperationFailedError("check email uniqueness", err)
	}

	// Create user. The record carries no id yet: qrdb mints the Snowflake that
	// IS the identity, and CreateUser stamps it back onto the record. Nothing
	// here invents one, which is what used to require a correlation table to
	// reconcile afterwards.
	now := time.Now().UTC()
	// Use Name if provided, otherwise fall back to Username. The fallback is
	// already safe (validateUsername's charset excludes "@"); an explicit Name
	// is not, so it is checked.
	displayName := strings.TrimSpace(opts.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(opts.Username)
	}
	if err := validateDisplayName(displayName); err != nil {
		return nil, err
	}
	roles := make([]string, 0)
	if len(opts.Roles) > 0 {
		roles = append(roles, opts.Roles...)
	}
	// Every option this function accepts is written to the record. It used to
	// build the user from nine fields and silently drop the rest, so a caller
	// setting AuthMethods got none, and State was left empty on every user in
	// the system — not "pending", not anything. An empty state is not a
	// lifecycle: PIVActivator refuses a user that is not pending, so an
	// account created anywhere but REST could not be PIV-activated at all,
	// and REST only worked because it issued a SECOND write afterwards to set
	// the fields this one had thrown away.
	if len(authMethods) == 0 {
		authMethods = []string{AuthMethodWebAuthn}
	}
	// An explicit Lifecycle WINS over the Enabled shorthand, exactly as it does
	// in UpdateUser. Enabled is one bool over six states, so it cannot express
	// the difference between an account waiting for its owner to activate and
	// one an administrator turned off — and letting it overrule a caller who
	// named the state turns every non-active state into `disabled`.
	//
	// That is not hypothetical: an export carries both, deriving Enabled from
	// CanAuthenticate(), so a PENDING account exports as pending + Enabled=false
	// and used to import as disabled. A round trip must not change what an
	// account is.
	//
	// Enabled therefore applies only when the caller named no state. Nil still
	// means the caller said nothing, so it cannot disable anyone.
	lifecycle := opts.EffectiveLifecycle()

	user := &User{
		TenantID: tenantID,
		Email:    strings.ToLower(strings.TrimSpace(opts.Email)),
		// The handle the caller chose, kept as its own field. It was validated
		// above and then overwritten by the display name, so the store indexed
		// — and qrdb's unique Username received — a value nobody asked for.
		Username:    strings.TrimSpace(opts.Username),
		Name:        displayName,
		Attributes:  opts.Attributes,
		Roles:       roles,
		AuthMethods: authMethods,
		Lifecycle:   lifecycle,
		Created:     now,
		Updated:     now,
	}

	if err := s.userStorage.CreateUser(ctx, user); err != nil {
		return nil, api.NewOperationFailedError("create user", err)
	}

	// A user is created before any invite is issued, so there is nothing to
	// strip here yet. Redacting anyway keeps the rule "no user leaves this
	// store carrying a token" true of every method, rather than true of the
	// ones that happen to need it today.
	return user, nil
}

// GetUser retrieves a user by ID within a tenant.
func (s *IdentityStore) GetUser(ctx context.Context, tenantID, userID string) (*User, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateUserID(userID); err != nil {
		return nil, err
	}

	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserByEmail retrieves a user by their canonical login key within a tenant.
func (s *IdentityStore) GetUserByEmail(ctx context.Context, tenantID, email string) (*User, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateEmail(email); err != nil {
		return nil, err
	}

	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.userStorage.GetUserByEmail(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserByPIVThumbprint retrieves a user by their registered PIV certificate thumbprint.
func (s *IdentityStore) GetUserByPIVThumbprint(ctx context.Context, thumbprint string) (*User, error) {
	if thumbprint == "" {
		return nil, NewIAMError(ErrUserInvalid, "iam.identity.get_user_by_piv_thumbprint", "thumbprint is required", nil)
	}
	user, err := s.userStorage.GetUserByPIVThumbprint(ctx, thumbprint)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// GetUserByUsername retrieves a user by tenant-scoped username. It is the seam
// the RBAC role-binding service uses to resolve a username to a user record
// (the broker has no username->id index of its own; qrdb owns it).
func (s *IdentityStore) GetUserByUsername(ctx context.Context, tenantID, username string) (*User, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if username == "" {
		return nil, NewIAMError(ErrUserInvalid, "iam.identity.get_user_by_username", "username is required", nil)
	}
	user, err := s.userStorage.GetUserByUsername(ctx, tenantID, username)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// UpdateUser updates an existing user.
//
// Changing the email address KEEPS the user's WebAuthn credentials, and says so
// in the log. The rename carries them because credential ownership was never
// the email: xkms's qrdb-backed WebAuthn store files a credential under the
// qrdb user id (`__xkms:webauthn/users/<id>/credentials`) and its qrdb fido2
// login factor under the same uint64, while the email is only the index the
// lookup enters the qrdb user record through. qrdb moves that index with the
// record, so after a rename the new address resolves to the same user id and
// the same credentials, and the old address resolves to nothing.
//
// This function used to REVOKE those credentials on every email change, on the
// belief that the store was keyed by (tenant, email) and would strand them.
// That was true of xkms's standalone backend store and has never been true of
// the qrdb-backed adapter this product wires, so the revocation destroyed
// working passkeys to prevent an orphan that could not occur — go-quicbaas#582,
// measured on the burn-in node when a rename took the tenant user's only
// passkey with it and returned 200.
func (s *IdentityStore) UpdateUser(ctx context.Context, tenantID, userID string, opts UpdateUserOptions) (*User, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateUserID(userID); err != nil {
		return nil, err
	}

	// Role references are resolved BEFORE any write, exactly as CreateUser
	// resolves them. Assigning a role through AssignRole checks that
	// the role exists; assigning the same role by writing the slice through
	// here did not, so an update could leave an account holding a role id that
	// names nothing — an authorization input that grants nothing today and
	// silently starts granting if something later claims that id.
	if opts.Roles != nil {
		for _, roleID := range *opts.Roles {
			if _, err := s.GetRole(ctx, tenantID, roleID); err != nil {
				return nil, err
			}
		}
	}
	// Get existing user
	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	// An email change is staged, not applied, until the write: assigning
	// user.Email up front would leave the caller's user object (and any store
	// that hands back the record rather than a copy) carrying the new address
	// even on the validation paths below that return an error. previousEmail is
	// the address this user is still reachable by, which the rename log names;
	// both are empty unless this call actually changes the email.
	previousEmail := ""
	pendingEmail := ""

	// Apply updates
	if opts.Email != nil {
		newEmail := strings.ToLower(strings.TrimSpace(*opts.Email))
		if err := validateEmail(newEmail); err != nil {
			return nil, err
		}

		// Check email uniqueness if changing
		if newEmail != user.Email {
			existing, err := s.userStorage.GetUserByEmail(ctx, tenantID, newEmail)
			if err == nil && existing != nil {
				return nil, ErrEmailAlreadyExists
			}
			// Ignore not-found errors (expected when email is available)
			if err != nil && !dberrors.IsNotFound(err) {
				return nil, api.NewOperationFailedError("check email uniqueness", err)
			}
			previousEmail = user.Email
			pendingEmail = newEmail
		}
	}

	// Two fields, two writes. This used to be a precedence rule — Name wins,
	// otherwise Username is written INTO Name — which meant a caller who
	// renamed a user by display name silently changed the handle every role
	// assignment and ceremony subject resolves through, and a caller who sent
	// both had one of them dropped without being told.
	if opts.Name != nil {
		name := strings.TrimSpace(*opts.Name)
		if err := validateDisplayName(name); err != nil {
			return nil, err
		}
		user.Name = name
	}
	if opts.Username != nil {
		username := strings.TrimSpace(*opts.Username)
		if err := validateUsername(username); err != nil {
			return nil, err
		}
		user.Username = username
	}

	if opts.Attributes != nil {
		user.Attributes = *opts.Attributes
	}

	// Enabled is a coarse projection of Lifecycle, so it is applied as a
	// TRANSITION rather than a field write: disabling moves the account to
	// disabled, and enabling moves it to active. Writing a derived field
	// directly is how a record ends up claiming to be locked and active at once.
	if opts.Enabled != nil {
		if *opts.Enabled {
			user.Lifecycle = LifecycleActive
		} else {
			user.Lifecycle = LifecycleDisabled
		}
	}

	if opts.Roles != nil {
		user.Roles = *opts.Roles
	}

	// An explicit lifecycle wins over the Enabled shorthand: a caller that
	// named the state meant that state — provided it is one this surface may
	// assert at all.
	if opts.Lifecycle != nil {
		if err := validateUpdateUserLifecycle(*opts.Lifecycle); err != nil {
			return nil, err
		}
		user.Lifecycle = *opts.Lifecycle
	}

	if opts.AuthMethods != nil {
		// Held to the same closed set as create. Validating only the create
		// path would make the rule true of one door into the same record: the
		// REST create handler writes the caller's auth_methods a SECOND time
		// through this function to attach the invite, and a bare UpdateUser
		// could set any string at all.
		authMethods, err := validateAuthMethods(*opts.AuthMethods)
		if err != nil {
			return nil, err
		}
		user.AuthMethods = authMethods
	}

	if opts.PIVCertThumbprints != nil {
		user.SetPIVThumbprints(*opts.PIVCertThumbprints...)
	} else if opts.PIVCertThumbprint != nil {
		user.SetPIVThumbprint(*opts.PIVCertThumbprint)
	}

	if opts.SPKICertThumbprint != nil {
		user.SetSPKIThumbprint(*opts.SPKICertThumbprint)
	}

	user.Updated = time.Now().UTC()

	// Count the credentials filed under the OLD address before the rename, so
	// the log below can report what the rename did to them rather than only
	// what it found afterwards. Taken here because the old address is the only
	// key that still resolves them at this point; the same count taken after
	// the write would answer a different question.
	//
	// It is diagnostic, not a gate: an unreadable credential store yields an
	// unknown count and the rename proceeds. Failing the rename closed on it
	// would be borrowing the very behaviour go-quicbaas#582 removed — letting
	// the WebAuthn stack veto an identity edit it no longer has a stake in.
	credentialsBefore := s.countWebAuthnCredentials(ctx, tenantID, previousEmail)

	if pendingEmail != "" {
		user.Email = pendingEmail
	}

	if err := s.userStorage.UpdateUser(ctx, user); err != nil {
		return nil, api.NewOperationFailedError("update user", err)
	}

	if previousEmail != "" {
		s.logEmailChangeCredentialCarry(ctx, tenantID, userID, previousEmail, user.Email, credentialsBefore)
	}

	// Redacted like every other read. The update that ATTACHES an invite is the
	// one call that would otherwise hand the token straight back, and the REST
	// create handler used to build its response from exactly this value; it now
	// renders the token it holds from the invite store instead.
	return user, nil
}

// webAuthnCredentialCount is a credential tally that knows whether it was
// actually taken. A deployment with no WebAuthn surface wired, or a credential
// store that could not be read, yields Known == false — which the rename log
// reports as unknown rather than as zero. "This account had no passkeys" and
// "nobody looked" are different facts, and conflating them is precisely how
// go-quicbaas#582 stayed invisible until an operator noticed a passkey missing.
type webAuthnCredentialCount struct {
	// Total is the number of credentials registered under the address.
	Total int

	// Known reports whether Total was measured rather than assumed.
	Known bool
}

// countWebAuthnCredentials reports how many WebAuthn credentials are registered
// under (tenantID, email). It never fails the caller: a store that cannot be
// read is logged and reported as an unknown count.
func (s *IdentityStore) countWebAuthnCredentials(ctx context.Context, tenantID, email string) webAuthnCredentialCount {
	if s.webauthnCredentialLister == nil || email == "" {
		return webAuthnCredentialCount{}
	}
	creds, err := s.webauthnCredentialLister.ListCredentials(ctx, tenantID, email)
	if err != nil {
		s.log().Warn("iam.identity: webauthn credentials could not be counted",
			"tenant_id", tenantID,
			"email", email,
			"error", err)
		return webAuthnCredentialCount{}
	}
	return webAuthnCredentialCount{Total: len(creds), Known: true}
}

// logEmailChangeCredentialCarry records what a rename did to the renamed user's
// enrolled WebAuthn credentials. An update that changes the address a user
// answers to is the one edit that used to destroy their passkeys and say
// nothing about it (go-quicbaas#582), so it is the one edit that reports the
// tally on both sides of the write.
//
// A tally that does not match is logged at ERROR and does not fail the update:
// the user record is already committed by this point, so the honest disposition
// is a loud record of the discrepancy rather than an error return that would
// misrepresent a completed write as a rejected one.
func (s *IdentityStore) logEmailChangeCredentialCarry(
	ctx context.Context,
	tenantID, userID, previousEmail, newEmail string,
	before webAuthnCredentialCount,
) {
	after := s.countWebAuthnCredentials(ctx, tenantID, newEmail)
	attrs := []any{
		"tenant_id", tenantID,
		"user_id", userID,
		"previous_email", previousEmail,
		"email", newEmail,
	}
	switch {
	case !before.Known || !after.Known:
		s.log().Info("iam.identity: user email changed; webauthn credentials stay bound to the user id and were not re-keyed", attrs...)
	case after.Total == before.Total:
		s.log().Info("iam.identity: user email changed; webauthn credentials carried to the new address",
			append(attrs, "webauthn_credentials", after.Total)...)
	default:
		s.log().Error("iam.identity: user email changed and the webauthn credential count changed with it",
			append(attrs,
				"webauthn_credentials_before", before.Total,
				"webauthn_credentials_after", after.Total)...)
	}
}

// DeleteUser deletes a user from the tenant, along with the WebAuthn
// credentials registered to it.
//
// The credential cascade runs FIRST and fails the deletion closed. Deleting the
// qrdb user record does not reach the xkms credential blobs or the per-user
// credential index that hang off its id, nor the qrdb fido2 login factors
// enrolled beside them, so a deletion that skipped the cascade would leave all
// three behind with nothing left that names them: the account they belonged to
// is the only handle any operator verb takes. The cascade resolves them through
// the user's email while the record still exists, which is why it must run
// before the delete and why an unreachable credential store aborts the deletion
// rather than producing that residue.
func (s *IdentityStore) DeleteUser(ctx context.Context, tenantID, userID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if err := validateUserID(userID); err != nil {
		return err
	}

	// Confirm the user exists before claiming it, so deleting an unknown user
	// reports not-found rather than a failure to mark something that was never
	// there. Only its EXISTENCE is used: every field the cascade needs comes
	// from the post-claim re-read below, because this snapshot predates the
	// claim and can already be stale.
	if _, err := s.userStorage.GetUser(ctx, tenantID, userID); err != nil {
		return err
	}

	// Close the account BEFORE sweeping its credentials. `deleting` is terminal
	// — nothing transitions out of it — so from this write on the account cannot
	// authenticate and no concurrent request can make it able to again. Doing it
	// after the sweep, or leaving it to the storage layer's own delete, would
	// leave the account live for the whole multi-step cascade and live again if
	// any step of that cascade failed.
	//
	// It FAILS CLOSED. A marker that cannot be written is not a cosmetic loss:
	// it is the one step that guarantees the account stops authenticating before
	// anything it owns is torn down, so continuing without it would sweep a live
	// account's credentials and — if any later step failed — leave that account
	// live, intact, and missing its passkey.
	//
	// It goes through MarkUserDeleting rather than UpdateUser because the store
	// refuses any update that would CHANGE a record already marked; a
	// full-record write could therefore never mark the same record twice, and
	// twice is what a retried deletion does. Re-marking is a no-op, so a
	// resumed deletion still gets past this point.
	if err := s.userStorage.MarkUserDeleting(ctx, tenantID, userID); err != nil {
		return api.NewOperationFailedError("mark user deleting", err)
	}

	// Re-read AFTER the claim. The read above happened before the account was
	// closed, so a concurrent update could have committed a new email, cert
	// binding in between — and sweeping the pre-claim
	// snapshot would remove the record while leaving those behind. Nothing can
	// change the record after the claim, because `deleting` is terminal, so this
	// snapshot is the last one and it is complete.
	// A FAILED re-read aborts. Keeping the pre-claim snapshot and continuing
	// would sweep the record as it looked before the claim and then delete it,
	// orphaning anything committed in between — which is the exact failure this
	// re-read exists to prevent, so falling back to the stale copy would make the
	// step decorative. Aborting is safe and resumable: the account is already
	// terminal, so nothing can make it authenticate again, and a retry starts
	// from a fresh snapshot.
	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return api.NewOperationFailedError("re-read user after deletion claim", err)
	}
	user.Lifecycle = LifecycleDeleting

	if s.webauthnCredentials != nil && user.Email != "" {
		if _, rErr := s.webauthnCredentials.DeleteCredentials(ctx, tenantID, user.Email); rErr != nil {
			return api.NewOperationFailedError("delete webauthn credentials", rErr)
		}
	}

	if err := s.userStorage.DeleteUser(ctx, tenantID, userID); err != nil {
		return api.NewOperationFailedError("delete user", err)
	}

	return nil
}

// ListUsers returns a list of users in the tenant.
func (s *IdentityStore) ListUsers(ctx context.Context, tenantID string, opts ListOptions) ([]*User, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}

	users, err := s.userStorage.ListUsers(ctx, tenantID, opts)
	if err != nil {
		return nil, api.NewOperationFailedError("list users", err)
	}

	return users, nil
}

// CreateRole creates a new role in the specified tenant.
func (s *IdentityStore) CreateRole(ctx context.Context, tenantID string, opts CreateRoleOptions) (*Role, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if opts.Name == "" {
		return nil, &ValidationError{Field: "name", Message: "role name cannot be empty"}
	}

	// Check tenant quota
	if s.tenants != nil {
		if err := s.tenants.CheckQuota(ctx, tenantID, QuotaResourceRoles); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	roleID := opts.ID
	if roleID == "" {
		roleID = uuid.New().String()
	}
	role := &Role{
		ID:          roleID,
		TenantID:    tenantID,
		Name:        strings.TrimSpace(opts.Name),
		Description: strings.TrimSpace(opts.Description),
		Permissions: opts.Permissions,
		Created:     now,
		Updated:     now,
	}

	if role.Permissions == nil {
		role.Permissions = []string{}
	}

	if err := s.roleStorage.CreateRole(ctx, role); err != nil {
		return nil, api.NewOperationFailedError("create role", err)
	}

	return role, nil
}

// GetRole retrieves a role by ID within a tenant.
func (s *IdentityStore) GetRole(ctx context.Context, tenantID, roleID string) (*Role, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateRoleID(roleID); err != nil {
		return nil, err
	}

	role, err := s.roleStorage.GetRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}

	return role, nil
}

// RoleNames returns the names of the roles user is bound to, in assignment
// order and without repeats. It is what the OIDC `groups` claim carries: the
// account record holds no group list of its own, so the groups a token states
// are the roles the identity store binds the user to.
//
// A bound role that no longer exists is skipped — it grants nothing, so it
// names nothing. Any other lookup failure is returned, because a claim built
// from a partial read would under-state the account without saying so.
func (s *IdentityStore) RoleNames(ctx context.Context, user *User) ([]string, error) {
	if user == nil || len(user.Roles) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(user.Roles))
	for _, roleID := range user.Roles {
		role, err := s.GetRole(ctx, user.TenantID, roleID)
		if err != nil {
			if dberrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		if role.Name == "" || slices.Contains(names, role.Name) {
			continue
		}
		names = append(names, role.Name)
	}
	return names, nil
}

// GetRoleByName retrieves a role by its tenant-scoped name.
//
// It is the service-layer seam for "which role is named N in tenant T" — the
// question qrdb's role.NameResolver asks so a grant binds to the identifier a
// role is actually persisted under. Callers must not answer it by listing and
// filtering: the configured storage answers from a name index, while a listing
// is capped by the store's page size and can miss a role that exists.
//
// Returns ErrRoleNotFound when the tenant holds no role of that name.
func (s *IdentityStore) GetRoleByName(ctx context.Context, tenantID, name string) (*Role, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateRoleName(name); err != nil {
		return nil, err
	}

	return s.roleStorage.GetRoleByName(ctx, tenantID, name)
}

// UpdateRole updates an existing role.
func (s *IdentityStore) UpdateRole(ctx context.Context, tenantID, roleID string, opts UpdateRoleOptions) (*Role, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateRoleID(roleID); err != nil {
		return nil, err
	}

	role, err := s.roleStorage.GetRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}

	if opts.Name != nil {
		name := strings.TrimSpace(*opts.Name)
		if name == "" {
			return nil, &ValidationError{Field: "name", Message: "role name cannot be empty"}
		}
		role.Name = name
	}

	if opts.Description != nil {
		role.Description = strings.TrimSpace(*opts.Description)
	}

	if opts.Permissions != nil {
		role.Permissions = *opts.Permissions
	}

	role.Updated = time.Now().UTC()

	if err := s.roleStorage.UpdateRole(ctx, role); err != nil {
		return nil, api.NewOperationFailedError("update role", err)
	}

	return role, nil
}

// deleteRoleCascadePage is how many users DeleteRole lists per page while
// stripping a role from its holders. It matches userPage in DeleteTenant so
// both cascades read the user store in the same page size.
const deleteRoleCascadePage = 256

// DeleteRole deletes a role from the tenant.
//
// Before the role is deleted, it is stripped from every holder the user
// store lists, page by page. A failure partway, listing or updating, returns
// at once and leaves the role in place, so a retry re-runs the cascade.
//
// Known gap (go-quicbaas#435): a role deleted through the embedded qrdb
// node's own role surface never reaches this method, so holders keep the
// deleted role id. That cascade belongs upstream in qrdb.
func (s *IdentityStore) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if err := validateRoleID(roleID); err != nil {
		return err
	}

	// Strip the role from every holder before deleting it. Stripping a role
	// leaves the user in the listing, so the offset must advance past each
	// page; DeleteTenant keeps Offset at 0 only because it deletes the users
	// it lists. Do not fold this loop into that idiom.
	for offset := 0; ; {
		users, err := s.userStorage.ListUsers(ctx, tenantID, ListOptions{
			Limit:  deleteRoleCascadePage,
			Offset: offset,
		})
		if err != nil {
			return api.NewOperationFailedError("list users at offset "+strconv.Itoa(offset), err)
		}
		if err := s.stripRoleFromUsers(ctx, users, roleID); err != nil {
			return err
		}
		if len(users) < deleteRoleCascadePage {
			break
		}
		offset += len(users)
	}

	if err := s.roleStorage.DeleteRole(ctx, tenantID, roleID); err != nil {
		return api.NewOperationFailedError("delete role", err)
	}

	return nil
}

// stripRoleFromUsers removes roleID from each user that holds it and saves
// the change. Users that do not hold the role are left untouched.
func (s *IdentityStore) stripRoleFromUsers(ctx context.Context, users []*User, roleID string) error {
	for _, user := range users {
		if !slices.Contains(user.Roles, roleID) {
			continue
		}
		newRoleIDs := make([]string, 0, len(user.Roles)-1)
		for _, rid := range user.Roles {
			if rid != roleID {
				newRoleIDs = append(newRoleIDs, rid)
			}
		}
		user.Roles = newRoleIDs
		user.Updated = time.Now().UTC()
		if err := s.userStorage.UpdateUser(ctx, user); err != nil {
			return api.NewOperationFailedError("update user "+user.ID, err)
		}
	}
	return nil
}

// ListRoles returns a list of roles in the tenant.
func (s *IdentityStore) ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}

	roles, err := s.roleStorage.ListRoles(ctx, tenantID, opts)
	if err != nil {
		return nil, api.NewOperationFailedError("list roles", err)
	}

	return roles, nil
}

// ReconcileBuiltInRoles converges a tenant's stored built-in roles onto the
// definitions the running build carries and reports only the roles that
// changed.
//
// It is the single supported repair for the drift a permission rename or
// addition creates: a tenant is seeded with the built-in definitions that
// existed when it was created, and every ordinary write path refuses a
// built-in afterwards (ErrSystemRoleImmutable), so without this a renamed verb
// strands every existing tenant with no remedy at any privilege level.
//
// Reconciliation is idempotent — a tenant that already agrees with the build
// reports no changed roles and writes nothing — so it is safe to run on every
// daemon start as well as on demand.
//
// The configured role storage must implement BuiltInRoleReconciler; only a
// qrdb-backed storage can, because the built-in definitions are qrdb's.
// Anything else is refused with ErrRoleReconcileUnsupported rather than
// reconciled against a set this package invented.
func (s *IdentityStore) ReconcileBuiltInRoles(ctx context.Context, tenantID string) (*RoleReconcileReport, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}

	reconciler, ok := s.roleStorage.(BuiltInRoleReconciler)
	if !ok {
		return nil, ErrRoleReconcileUnsupported
	}

	report, err := reconciler.ReconcileBuiltInRoles(ctx, tenantID)
	if err != nil {
		return nil, api.NewOperationFailedError("reconcile built-in roles", err)
	}
	if report == nil {
		report = &RoleReconcileReport{TenantID: tenantID}
	}
	return report, nil
}

// AssignRole assigns a role to a user.
func (s *IdentityStore) AssignRole(ctx context.Context, tenantID, userID, roleID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if err := validateUserID(userID); err != nil {
		return err
	}
	if err := validateRoleID(roleID); err != nil {
		return err
	}

	// Verify user and role exist
	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}

	if _, err := s.roleStorage.GetRole(ctx, tenantID, roleID); err != nil {
		return err
	}

	// Check if role is already assigned
	for _, rid := range user.Roles {
		if rid == roleID {
			return ErrRoleAlreadyAssigned
		}
	}

	// Assign role
	user.Roles = append(user.Roles, roleID)
	user.Updated = time.Now().UTC()

	if err := s.userStorage.UpdateUser(ctx, user); err != nil {
		return api.NewOperationFailedError("update user", err)
	}

	return nil
}

// RevokeRole revokes a role from a user.
func (s *IdentityStore) RevokeRole(ctx context.Context, tenantID, userID, roleID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if err := validateUserID(userID); err != nil {
		return err
	}
	if err := validateRoleID(roleID); err != nil {
		return err
	}

	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}

	// Check if role is assigned
	found := false
	newRoleIDs := make([]string, 0, len(user.Roles))
	for _, rid := range user.Roles {
		if rid == roleID {
			found = true
		} else {
			newRoleIDs = append(newRoleIDs, rid)
		}
	}

	if !found {
		return ErrRoleNotAssigned
	}

	// Revoke role
	user.Roles = newRoleIDs
	user.Updated = time.Now().UTC()

	if err := s.userStorage.UpdateUser(ctx, user); err != nil {
		return api.NewOperationFailedError("update user", err)
	}

	return nil
}

// GetUserRoles retrieves all roles assigned to a user.
func (s *IdentityStore) GetUserRoles(ctx context.Context, tenantID, userID string) ([]*Role, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateUserID(userID); err != nil {
		return nil, err
	}

	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	roles := make([]*Role, 0, len(user.Roles))
	for _, roleID := range user.Roles {
		role, err := s.roleStorage.GetRole(ctx, tenantID, roleID)
		if err != nil {
			// coded-sentinel-ok: the CLASS is the intent, and fail-safe. A role
			// the store cannot produce is skipped however it went missing; a
			// dropped role can only narrow the permissions this resolves to.
			if errors.Is(err, ErrRoleNotFound) {
				continue // Skip deleted roles
			}
			return nil, api.NewOperationFailedError("get role "+roleID, err)
		}
		roles = append(roles, role)
	}

	return roles, nil
}

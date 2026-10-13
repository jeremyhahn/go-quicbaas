package iam

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

const (
	// userActivationClaimPrefix keys a user's activation claim in the
	// tenant's own store. The resolved store is already confined to the
	// tenant, so the user id alone identifies the claim.
	userActivationClaimPrefix = "iam/user-activation-claims/"

	// userActivationWorkTimeout bounds the work an activation does while it
	// holds its claim: the invite redemption, the factor enrollment and the
	// user update.
	userActivationWorkTimeout = 2 * time.Minute

	// userActivationClaimLease is how long a claim protects an activation that
	// never released it, for example because its process died. It covers the
	// bounded work, the detached re-read and rollback, and the release, with
	// margin for clock skew between the node that took the claim and the node
	// that later judges it expired.
	userActivationClaimLease = userActivationWorkTimeout + 3*inviteRollbackTimeout + 2*time.Minute

	userActivationHolderBytes = 16
)

// UserActivationClaims arbitrates the pending -> active transition of a user.
//
// A pending user can hold more than one live invite, for example when
// invite-mode provisioning is retried, and each token is single-use only on
// its own. Two activations presenting different tokens for the same user
// would each win their own redemption and each enroll a login factor. The
// claim closes that: an activation takes the user's claim before it redeems
// or enrolls anything, and only one claim per user can exist.
//
// The STORE decides the claim. It is a create-if-absent write to the tenant's
// own store, evaluated inside qrdb's state machine, so it holds across nodes;
// an in-process lock would not.
//
// A claim ends in one of two ways. A successful activation completes it, so no
// later activation of the same user can start, whatever token it holds, until
// the user is pending again. A failed activation releases it, so the user can
// be re-invited. A claim whose holder vanished without doing either can be
// taken over once its lease has expired.
//
// "Pending again" is judged from the user record the caller read, not from
// the claim alone: a completed claim yields only to an activation whose user
// became pending after the claim completed, which is what an administrator
// resetting the user to pending produces. A stale read of the record from
// before the activation is older than the completion, so it cannot reopen the
// user.
type UserActivationClaims struct {
	resolver qrdbsdk.TenantStoreResolver
}

// NewUserActivationClaims builds the claim store over the tenant store
// resolver of the embedded qrdb node.
func NewUserActivationClaims(resolver qrdbsdk.TenantStoreResolver) (*UserActivationClaims, error) {
	if resolver == nil {
		return nil, &UserActivationClaimConfigError{Field: "TenantStoreResolver"}
	}
	return &UserActivationClaims{resolver: resolver}, nil
}

// userActivationClaimRecord is the stored value of a claim. A held claim
// carries the expiry of its lease; a completed one carries the time the user
// was activated instead.
type userActivationClaimRecord struct {
	Holder      string    `json:"holder"`
	ExpiresAt   time.Time `json:"expires_at"`
	CompletedAt time.Time `json:"completed_at"`
}

// UserActivationClaim is a claim held by one activation.
type UserActivationClaim struct {
	store  qrdbsdk.AtomicKVStore
	key    string
	holder string
	value  []byte
}

// Claim takes the activation claim for a user. pendingSince is the Updated
// time of the pending user record the caller read. It returns a
// *UserActivationInProgressError, which matches ErrUserNotPending, when
// another activation holds the claim or has activated the user since that
// record was written.
func (c *UserActivationClaims) Claim(ctx context.Context, tenantID, userID string, pendingSince time.Time) (*UserActivationClaim, error) {
	if c == nil || c.resolver == nil {
		return nil, &UserActivationClaimConfigError{Field: "TenantStoreResolver"}
	}
	store, err := c.resolver.Resolve(ctx, tenantID)
	if err != nil {
		return nil, &UserActivationClaimError{UserID: userID, Op: "resolve store", Cause: err}
	}
	atomic, ok := store.(qrdbsdk.AtomicKVStore)
	if !ok {
		return nil, &UserActivationClaimUnsupportedError{StoreType: fmt.Sprintf("%T", store)}
	}

	holder, err := newUserActivationHolder()
	if err != nil {
		return nil, &UserActivationClaimError{UserID: userID, Op: "generate holder", Cause: err}
	}
	value, err := json.Marshal(userActivationClaimRecord{
		Holder:    holder,
		ExpiresAt: time.Now().UTC().Add(userActivationClaimLease),
	})
	if err != nil {
		return nil, &UserActivationClaimError{UserID: userID, Op: "encode claim", Cause: err}
	}
	claim := &UserActivationClaim{store: atomic, key: userActivationClaimPrefix + userID, holder: holder, value: value}

	stored, err := atomic.PutIfAbsent(ctx, claim.key, value)
	if err != nil {
		return nil, &UserActivationClaimError{UserID: userID, Op: "claim", Cause: err}
	}
	if stored {
		return claim, nil
	}
	return claim.takeOver(ctx, userID, pendingSince)
}

// takeOver replaces a claim whose lease has expired, or a completed claim the
// user has since been reset from. The swap is conditional on the exact value
// read, so of several activations that find the same claim, exactly one takes
// it over.
func (h *UserActivationClaim) takeOver(ctx context.Context, userID string, pendingSince time.Time) (*UserActivationClaim, error) {
	current, err := h.store.Get(ctx, h.key)
	if err != nil {
		// The holder may have released the claim between the create and this
		// read. The caller retries the activation rather than this method
		// retrying the create, which keeps the outcome a single decision.
		return nil, &UserActivationInProgressError{UserID: userID, Cause: err}
	}
	var record userActivationClaimRecord
	if err := json.Unmarshal(current, &record); err != nil {
		return nil, &UserActivationInProgressError{UserID: userID, Cause: err}
	}
	if !record.supersededBy(pendingSince, time.Now()) {
		return nil, &UserActivationInProgressError{UserID: userID}
	}
	swapped, err := h.store.CompareAndSwap(ctx, h.key, current, h.value)
	if err != nil {
		return nil, &UserActivationClaimError{UserID: userID, Op: "take over expired claim", Cause: err}
	}
	if !swapped {
		return nil, &UserActivationInProgressError{UserID: userID}
	}
	return h, nil
}

// supersededBy reports whether an activation that read a pending user record
// written at pendingSince may take this claim over at now.
func (r userActivationClaimRecord) supersededBy(pendingSince, now time.Time) bool {
	if !r.CompletedAt.IsZero() {
		return pendingSince.After(r.CompletedAt)
	}
	return now.After(r.ExpiresAt)
}

// Complete records that the user is active, so no later activation of the
// user can start until the user is pending again. activatedAt is the Updated
// time written to the activated user record.
func (h *UserActivationClaim) Complete(ctx context.Context, activatedAt time.Time) error {
	final, err := json.Marshal(userActivationClaimRecord{Holder: h.holder, CompletedAt: activatedAt})
	if err != nil {
		return &UserActivationClaimError{Op: "encode completed claim", Cause: err}
	}
	swapped, err := h.store.CompareAndSwap(ctx, h.key, h.value, final)
	if err != nil {
		return &UserActivationClaimError{Op: "complete claim", Cause: err}
	}
	if !swapped {
		return &UserActivationClaimLostError{Key: h.key}
	}
	h.value = final
	return nil
}

// Release gives up the claim of a failed activation. It deletes the claim only
// while it still holds this activation's value, so a claim taken over after
// its lease expired is never removed from under its new holder.
func (h *UserActivationClaim) Release(ctx context.Context) error {
	deleted, err := h.store.CompareAndDelete(ctx, h.key, h.value)
	if err != nil {
		return &UserActivationClaimError{Op: "release claim", Cause: err}
	}
	if !deleted {
		return &UserActivationClaimLostError{Key: h.key}
	}
	return nil
}

func newUserActivationHolder() (string, error) {
	buf := make([]byte, userActivationHolderBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// UserActivationClaimConfigError reports a claim store built without a
// required dependency.
type UserActivationClaimConfigError struct {
	Field string
}

func (e *UserActivationClaimConfigError) Error() string {
	return "iam: user activation claims: " + e.Field + " is required"
}

// UserActivationClaimUnsupportedError reports a tenant store that cannot
// perform the guarded writes a claim needs. The claim fails closed rather than
// approximating arbitration with unguarded writes.
type UserActivationClaimUnsupportedError struct {
	StoreType string
}

func (e *UserActivationClaimUnsupportedError) Error() string {
	return "iam: user activation claims: store " + e.StoreType + " cannot perform guarded writes"
}

// UserActivationClaimError reports a store failure while taking, completing or
// releasing a claim.
type UserActivationClaimError struct {
	UserID string
	Op     string
	Cause  error
}

func (e *UserActivationClaimError) Error() string {
	msg := "iam: user activation claim: " + e.Op
	if e.UserID != "" {
		msg += " for user " + e.UserID
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

func (e *UserActivationClaimError) Unwrap() error { return e.Cause }

// UserActivationClaimLostError reports that a claim no longer holds the value
// its holder wrote: its lease expired and another activation took it over.
type UserActivationClaimLostError struct {
	Key string
}

func (e *UserActivationClaimLostError) Error() string {
	return "iam: user activation claim " + e.Key + " is no longer held"
}

// UserActivationInProgressError reports that another activation holds the
// user's claim or has already activated the user. It matches
// ErrUserNotPending, so every transport answers it as the conflict it is.
type UserActivationInProgressError struct {
	UserID string
	Cause  error
}

func (e *UserActivationInProgressError) Error() string {
	msg := "iam: user " + e.UserID + " is already being activated"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

func (e *UserActivationInProgressError) Unwrap() error { return ErrUserNotPending }

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

package sdlc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// outcomeReservationPrefix is the key space of the outcome reservations:
// sdlc/outcome-reservation/{tenant}/{project}/{issue}/{reconcile key}.
const outcomeReservationPrefix = "sdlc/outcome-reservation/"

// outcomeLeaseSuffix names the key beside a reservation that carries its
// renewals. The reservation value itself is never rewritten: the store has no
// guarded swap, and a delete-then-create would manufacture a vacant moment in
// which another node wins.
const outcomeLeaseSuffix = "/lease"

// OutcomeReservationMargin is added to the posting budget when deriving a
// reservation's TTL, so a post that spends its whole budget still finishes
// inside the lease.
const OutcomeReservationMargin = 5 * time.Minute

// OutcomeReservationTTL derives how long a reservation must outlive a node that
// is still posting: the whole surfacing budget — every attempt, its timeout and
// the wait between attempts — plus a margin. A fixed constant would lapse under
// a long RetryInterval and let a second node post beside the first.
func OutcomeReservationTTL(policy SurfacePolicy) time.Duration {
	resolved := policy.Resolved()
	attempts := time.Duration(resolved.RetryAttempts)
	return attempts*(resolved.RetryInterval+resolved.PostTimeout) + OutcomeReservationMargin
}

// OutcomeReserver decides, across nodes, which one posts an outcome comment.
type OutcomeReserver interface {
	// Reserve takes the one-writer reservation for (issue, reconcileKey) for
	// ttl and returns the lease the winner holds while it posts. It returns
	// *OutcomeReservedElsewhereError when another node holds an unexpired
	// reservation.
	Reserve(ctx context.Context, tenantID, projectID string, issue int, reconcileKey string, ttl time.Duration) (OutcomeLease, error)
}

// OutcomeLease is the winner's hold on a reservation.
type OutcomeLease interface {
	// Hold fences the next post: it confirms the reservation is still this
	// node's exact value and renews it. It returns
	// *OutcomeReservationLostError when another node displaced it, in which
	// case the caller must not post.
	Hold(ctx context.Context) error
}

// outcomeReservationGuard is the store capability a reservation needs: a
// guarded create and a guarded delete, discovered by type assertion exactly as
// the orchestration stores discover them.
type outcomeReservationGuard interface {
	PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error)
	CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error)
}

// outcomeReservation is the value stored under a reservation key. The store
// has no native TTL on a guarded create, so the expiry travels in the value and
// an expired holder is displaced with a guarded delete of exactly that value.
// The nonce makes the value unique to one reservation, which is what lets a
// holder fence on it.
type outcomeReservation struct {
	Holder    string    `json:"holder"`
	Nonce     string    `json:"nonce"`
	ExpiresAt time.Time `json:"expires_at"`
}

// outcomeRenewal is the value under the lease key: the new expiry of the
// reservation carrying Nonce.
type outcomeRenewal struct {
	Nonce     string    `json:"nonce"`
	ExpiresAt time.Time `json:"expires_at"`
}

// kvOutcomeReserver is the OutcomeReserver over a shared KV store.
type kvOutcomeReserver struct {
	kv     interfaces.KVStore
	guard  outcomeReservationGuard
	holder string
	now    func() time.Time
}

// NewOutcomeReserver returns the reservation over kv, which every node sweeping
// the same projects must share. holder names this node in the stored value; now
// may be nil.
//
// The store MUST offer PutIfAbsent and CompareAndDelete. There is no emulation
// with Exists-then-Put, which is precisely the race the reservation exists to
// close, so a store without them is refused here.
//
// EVERY STORE OPERATION IS SCOPED TO THE RESERVATION'S TENANT through
// orchestration.ContextWithTenant, the same helper the tenant-scoped sweeps use.
// A tenant-routing store picks the physical store from the context, so an
// unscoped call would land a barrier-isolated tenant's reservation in the shared
// system store.
func NewOutcomeReserver(kv interfaces.KVStore, holder string, now func() time.Time) (OutcomeReserver, error) {
	switch {
	case kv == nil:
		return nil, &InvalidInputError{Field: "store", Message: "the reservation is kept in the store the nodes share"}
	case holder == "":
		return nil, &InvalidInputError{Field: "holder", Message: "a reservation names the node that holds it"}
	}
	guard, ok := kv.(outcomeReservationGuard)
	if !ok {
		return nil, &OutcomeReservationUnguardedError{}
	}
	if now == nil {
		now = time.Now
	}
	return &kvOutcomeReserver{kv: kv, guard: guard, holder: holder, now: now}, nil
}

// Reserve implements OutcomeReserver.
func (r *kvOutcomeReserver) Reserve(ctx context.Context, tenantID, projectID string, issue int, reconcileKey string, ttl time.Duration) (OutcomeLease, error) {
	if ttl <= 0 {
		return nil, &InvalidInputError{Field: "ttl", Message: "must be positive; a reservation without a lease outlives the node that took it"}
	}
	ctx = orchestration.ContextWithTenant(ctx, tenantID)
	key := outcomeReservationPrefix + url.PathEscape(tenantID) + "/" + url.PathEscape(projectID) + "/" +
		strconv.Itoa(issue) + "/" + base64.RawURLEncoding.EncodeToString([]byte(reconcileKey))

	nonce, err := newNonce()
	if err != nil {
		return nil, &OutcomeReservationError{Key: key, Operation: "nonce", Cause: err}
	}
	value, err := json.Marshal(outcomeReservation{Holder: r.holder, Nonce: nonce, ExpiresAt: r.now().Add(ttl)})
	if err != nil {
		return nil, &OutcomeReservationError{Key: key, Operation: "marshal", Cause: err}
	}
	lease := &kvOutcomeLease{reserver: r, tenantID: tenantID, key: key, value: value, nonce: nonce, ttl: ttl}

	won, err := r.guard.PutIfAbsent(ctx, key, value)
	if err != nil {
		return nil, &OutcomeReservationError{Key: key, Operation: "PutIfAbsent", Cause: err}
	}
	if won {
		return lease, nil
	}

	// Someone holds it. Only an EXPIRED holder is displaced, and only by a delete
	// guarded on that holder's exact value, so a third node's fresh reservation
	// is never removed by this node's cleanup.
	held, err := r.kv.Get(ctx, key)
	if err != nil {
		return nil, &OutcomeReservationError{Key: key, Operation: "Get", Cause: err}
	}
	var current outcomeReservation
	if err := json.Unmarshal(held, &current); err != nil {
		return nil, &OutcomeReservationError{Key: key, Operation: "unmarshal", Cause: err}
	}
	expiry, err := r.effectiveExpiry(ctx, key, current)
	if err != nil {
		return nil, err
	}
	if r.now().Before(expiry) {
		return nil, &OutcomeReservedElsewhereError{Issue: issue}
	}
	deleted, err := r.guard.CompareAndDelete(ctx, key, held)
	if err != nil {
		return nil, &OutcomeReservationError{Key: key, Operation: "CompareAndDelete", Cause: err}
	}
	if !deleted {
		return nil, &OutcomeReservedElsewhereError{Issue: issue}
	}
	won, err = r.guard.PutIfAbsent(ctx, key, value)
	if err != nil {
		return nil, &OutcomeReservationError{Key: key, Operation: "PutIfAbsent", Cause: err}
	}
	if !won {
		return nil, &OutcomeReservedElsewhereError{Issue: issue}
	}
	return lease, nil
}

// effectiveExpiry is when the holder's reservation lapses: the later of the
// expiry it was created with and its latest renewal. A renewal naming another
// nonce belongs to a displaced holder and is ignored.
func (r *kvOutcomeReserver) effectiveExpiry(ctx context.Context, key string, current outcomeReservation) (time.Time, error) {
	leaseKey := key + outcomeLeaseSuffix
	exists, err := r.kv.Exists(ctx, leaseKey)
	if err != nil {
		return time.Time{}, &OutcomeReservationError{Key: leaseKey, Operation: "Exists", Cause: err}
	}
	if !exists {
		return current.ExpiresAt, nil
	}
	raw, err := r.kv.Get(ctx, leaseKey)
	if err != nil {
		return time.Time{}, &OutcomeReservationError{Key: leaseKey, Operation: "Get", Cause: err}
	}
	var renewal outcomeRenewal
	if err := json.Unmarshal(raw, &renewal); err != nil {
		return time.Time{}, &OutcomeReservationError{Key: leaseKey, Operation: "unmarshal", Cause: err}
	}
	if renewal.Nonce == current.Nonce && renewal.ExpiresAt.After(current.ExpiresAt) {
		return renewal.ExpiresAt, nil
	}
	return current.ExpiresAt, nil
}

// newNonce returns a random identifier: one reservation, or one binding's
// epoch.
func newNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// kvOutcomeLease is the winner's hold on one reservation.
type kvOutcomeLease struct {
	reserver *kvOutcomeReserver
	tenantID string
	key      string
	value    []byte
	nonce    string
	ttl      time.Duration
}

// Hold implements OutcomeLease. The check and the renewal are two store
// operations, so a displacement between them is possible; the window is one
// round trip rather than the whole retry budget, which is what a fence before
// each attempt buys.
func (l *kvOutcomeLease) Hold(ctx context.Context) error {
	r := l.reserver
	ctx = orchestration.ContextWithTenant(ctx, l.tenantID)
	held, err := r.kv.Get(ctx, l.key)
	if err != nil {
		return &OutcomeReservationError{Key: l.key, Operation: "Get", Cause: err}
	}
	if !bytes.Equal(held, l.value) {
		return &OutcomeReservationLostError{Key: l.key}
	}
	renewal, err := json.Marshal(outcomeRenewal{Nonce: l.nonce, ExpiresAt: r.now().Add(l.ttl)})
	if err != nil {
		return &OutcomeReservationError{Key: l.key, Operation: "marshal", Cause: err}
	}
	if err := r.kv.Put(ctx, l.key+outcomeLeaseSuffix, renewal); err != nil {
		return &OutcomeReservationError{Key: l.key + outcomeLeaseSuffix, Operation: "Put", Cause: err}
	}
	return nil
}

// outcomeLeaseKey is the context key carrying the winner's lease to the post.
type outcomeLeaseKey struct{}

// withOutcomeLease returns ctx carrying lease, so postWithRetry can fence each
// attempt without every poster growing a parameter.
func withOutcomeLease(ctx context.Context, lease OutcomeLease) context.Context {
	return context.WithValue(ctx, outcomeLeaseKey{}, lease)
}

// holdOutcomeLease fences one post attempt. A context carrying no lease — the
// engine's own posts, or a node with no reserver — is not fenced.
func holdOutcomeLease(ctx context.Context) error {
	lease, ok := ctx.Value(outcomeLeaseKey{}).(OutcomeLease)
	if !ok {
		return nil
	}
	return lease.Hold(ctx)
}

// OutcomeReservationUnguardedError reports a store that cannot decide a
// contested create, and so cannot arbitrate between nodes.
type OutcomeReservationUnguardedError struct{}

// Error implements the error interface.
func (*OutcomeReservationUnguardedError) Error() string {
	return "outcome reservation: the store offers no PutIfAbsent and CompareAndDelete, so it cannot decide which node posts"
}

// OutcomeReservationError reports a store fault while reserving. The caller
// fails closed on it: nothing is posted until a later sweep reserves.
type OutcomeReservationError struct {
	Key       string
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *OutcomeReservationError) Error() string {
	return "outcome reservation " + e.Key + ": " + e.Operation + ": " + e.Cause.Error()
}

// Unwrap exposes the store's own error.
func (e *OutcomeReservationError) Unwrap() error { return e.Cause }

// OutcomeReservedElsewhereError reports that another node holds the
// reservation and will post, so this node skips.
type OutcomeReservedElsewhereError struct {
	Issue int
}

// Error implements the error interface.
func (e *OutcomeReservedElsewhereError) Error() string {
	return "issue #" + strconv.Itoa(e.Issue) + ": another node holds the outcome reservation and posts it"
}

// OutcomeReservationLostError reports a winner whose reservation was displaced
// while it was still posting. It must not post.
type OutcomeReservationLostError struct {
	Key string
}

// Error implements the error interface.
func (e *OutcomeReservationLostError) Error() string {
	return "outcome reservation " + e.Key + ": displaced by another node before the post; not posting"
}

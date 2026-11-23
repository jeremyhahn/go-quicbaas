// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"sort"
	"strconv"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// consentKeyspace holds the tenant-scoped consent requests. It is a sibling
// of placementKeyspace, not a child, so a spec scan never sees a request.
const consentKeyspace = "orch/placement-consent/"

// consentIDDomain separates the request-id hash from any other hash over the
// same fields.
const consentIDDomain = "quicbaas/placement-consent/v1"

// ConsentState is the state of a placement consent request.
type ConsentState string

const (
	// ConsentPending: raised and not yet answered.
	ConsentPending ConsentState = "pending"
	// ConsentGranted: a person agreed to the move.
	ConsentGranted ConsentState = "granted"
	// ConsentRefused: a person refused the move.
	ConsentRefused ConsentState = "refused"
	// ConsentIncoherent is never stored. It is reported for a stored request
	// that does not describe its own key: its move does not hash to its id,
	// it names another tenant, or its state and answer disagree. Such a
	// request grants nothing and cannot be answered.
	ConsentIncoherent ConsentState = "incoherent"
)

// Errors a ConsentLedger returns, matched with errors.Is.
var (
	ErrConsentInvalid    = errors.New("placement consent: invalid move or answer")
	ErrConsentNotFound   = errors.New("placement consent: request not found")
	ErrConsentNotPending = errors.New("placement consent: request is not pending")
	ErrConsentStale      = errors.New("placement consent: request changed while it was answered")
	ErrConsentIncoherent = errors.New("placement consent: stored request does not match its id")
)

// ConsentMove names one move that needs consent. Its fields, with the tenant,
// are exactly what identifies a request (RequestID).
//
// Class is recorded as given and not interpreted: which classes exist, and
// where a session's class comes from, is open (placement design, open
// question 2). The ledger only requires it to be named.
//
// The destination host is deliberately not part of the move: the design
// identifies a move by its source, and the actuator picks and admission-checks
// the destination itself (open question 3 leaves how a custody session is
// relocated undecided; a grant here says only that this work may leave this
// host under this spec generation).
type ConsentMove struct {
	SpecName       string `json:"spec_name"`
	SpecGeneration uint64 `json:"spec_generation"`
	Class          string `json:"class"`
	WorkID         string `json:"work_id"`
	SourceHost     string `json:"source_host"`
}

// RequestID returns the id of the consent request for this move in tenantID:
// the hex SHA-256 of the tenant, spec name, spec generation, class, work id
// and source host, each length-prefixed so distinct moves cannot collide. Two
// control-plane replicas derive the same id without coordinating, and a new
// spec generation derives a new id, so editing a spec voids its grants.
func (m ConsentMove) RequestID(tenantID string) string {
	h := sha256.New()
	writeConsentField(h, consentIDDomain)
	writeConsentField(h, tenantID)
	writeConsentField(h, m.SpecName)
	writeConsentField(h, strconv.FormatUint(m.SpecGeneration, 10))
	writeConsentField(h, m.Class)
	writeConsentField(h, m.WorkID)
	writeConsentField(h, m.SourceHost)
	return hex.EncodeToString(h.Sum(nil))
}

// writeConsentField writes one field as an 8-byte big-endian length followed
// by its bytes.
func writeConsentField(h hash.Hash, field string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(field)))
	_, _ = h.Write(size[:])
	_, _ = h.Write([]byte(field))
}

// validate refuses a move that does not name every field of its id.
func (m ConsentMove) validate() error {
	for _, f := range []struct{ name, value string }{
		{"spec_name", m.SpecName}, {"class", m.Class}, {"work_id", m.WorkID}, {"source_host", m.SourceHost},
	} {
		if f.value == "" {
			return &ConsentInvalidError{Field: "move." + f.name, Reason: "is empty"}
		}
	}
	if m.SpecGeneration == 0 {
		return &ConsentInvalidError{Field: "move.spec_generation", Reason: "is zero; a stored spec has a generation"}
	}
	return nil
}

// ConsentAnswer is a person's decision on a pending request.
//
// By names the decider. The ledger only requires it to be non-empty; the
// service sets it from the authenticated subject, never from a request body,
// and the permission that may answer is the service's concern (open question
// 5), not the ledger's.
type ConsentAnswer struct {
	// State is ConsentGranted or ConsentRefused.
	State ConsentState `json:"state"`
	// By is the subject that answered.
	By string `json:"by"`
	// Reason is optional free text recorded with the answer.
	Reason string `json:"reason,omitempty"`
}

// PlacementConsentRequest is one stored consent request.
type PlacementConsentRequest struct {
	// ID is Move.RequestID(TenantID).
	ID       string      `json:"id"`
	TenantID string      `json:"tenant_id"`
	Move     ConsentMove `json:"move"`
	// State is pending, granted or refused as stored, or incoherent as read.
	State ConsentState `json:"state"`
	// By, Reason and AnsweredAt are set by the answer, empty while pending.
	By         string    `json:"by,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	RaisedAt   time.Time `json:"raised_at"`
	AnsweredAt time.Time `json:"answered_at,omitempty"`
}

// ConsentLedger stores per-move consent requests for one tenant at a time.
// It is storage and rules only: it does not authorize the caller, and it
// never decides a move itself.
//
// The ledger is per tenant: keys are tenant-scoped and a request is only
// reachable from its own tenant (open question 4 leaves whether hosts are
// shared across tenants open; a host is just a string here). Requests are
// never expired or deleted, and an answer is final (the narrowest reading:
// a changed mind needs a new spec generation, which raises a new request).
// Every replica may raise: the derived id and the guarded create make a
// concurrent raise converge on one request, so no leader is assumed (open
// question 6). A grant is necessary for a consented move, not sufficient:
// whether a class is actuated at all (open question 8, custody) is the
// actuator's rule.
type ConsentLedger interface {
	// Raise creates a pending request for move, or returns the request
	// already stored for it, answered or not. It never overwrites a stored
	// request. A stored request that is incoherent returns
	// *ConsentIncoherentError.
	Raise(ctx context.Context, tenantID string, move ConsentMove) (*PlacementConsentRequest, error)

	// Answer swaps a pending request to granted or refused and returns it.
	// An invalid answer returns *ConsentInvalidError, an unknown id
	// *ConsentNotFoundError, a request that is not pending
	// *ConsentNotPendingError, and an answer that lost the swap to another
	// writer *ConsentStaleError. A refused answer changes nothing.
	Answer(ctx context.Context, tenantID, requestID string, answer ConsentAnswer) (*PlacementConsentRequest, error)

	// Get returns one request, or *ConsentNotFoundError. A request that does
	// not describe its own key is returned with State ConsentIncoherent.
	Get(ctx context.Context, tenantID, requestID string) (*PlacementConsentRequest, error)

	// List returns the tenant's requests in state (all of them when state is
	// empty), ordered by id. A stored request that does not decode fails the
	// list.
	List(ctx context.Context, tenantID string, state ConsentState) ([]*PlacementConsentRequest, error)

	// Granted reports whether move has a granted request. No request is
	// false with no error. A failed read, undecodable bytes or an incoherent
	// request is false with an error. It never reports true with an error.
	Granted(ctx context.Context, tenantID string, move ConsentMove) (bool, error)
}

type consentLedger struct {
	kv      interfaces.KVStore
	creator guardedCreator
	swapper guardedSwapper
	now     func() time.Time
}

// NewConsentLedger returns a ConsentLedger over kv. kv must decide contested
// writes: without PutIfAbsent two raises could store two requests, and
// without CompareAndSwap an answer could overwrite another. Either missing,
// or a nil kv, returns *PlacementStoreUnguardedError.
func NewConsentLedger(kv interfaces.KVStore) (ConsentLedger, error) {
	if kv == nil {
		return nil, &PlacementStoreUnguardedError{Missing: "a store"}
	}
	creator, ok := kv.(guardedCreator)
	if !ok {
		return nil, &PlacementStoreUnguardedError{Missing: "PutIfAbsent"}
	}
	swapper, ok := kv.(guardedSwapper)
	if !ok {
		return nil, &PlacementStoreUnguardedError{Missing: "CompareAndSwap"}
	}
	return &consentLedger{kv: kv, creator: creator, swapper: swapper, now: func() time.Time { return time.Now().UTC() }}, nil
}

func consentKey(tenantID, requestID string) (string, error) {
	return entityKey(consentKeyspace, tenantID, "request_id", requestID)
}

func (l *consentLedger) Raise(ctx context.Context, tenantID string, move ConsentMove) (*PlacementConsentRequest, error) {
	if err := move.validate(); err != nil {
		return nil, err
	}
	id := move.RequestID(tenantID)
	key, err := consentKey(tenantID, id)
	if err != nil {
		return nil, err
	}
	req := &PlacementConsentRequest{
		ID: id, TenantID: tenantID, Move: move, State: ConsentPending, RaisedAt: l.now(),
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("placement consent %q: encode: %w", id, err)
	}
	stored, err := l.creator.PutIfAbsent(ctx, key, data)
	if err != nil {
		return nil, fmt.Errorf("placement consent %q: create: %w", id, err)
	}
	if stored {
		return req, nil
	}
	// Another raise created it first: return what is stored, answer and all.
	_, existing, err := l.read(ctx, tenantID, id, key)
	if err != nil {
		return nil, err
	}
	if existing.State == ConsentIncoherent {
		return nil, &ConsentIncoherentError{TenantID: tenantID, RequestID: id}
	}
	return existing, nil
}

func (l *consentLedger) Answer(ctx context.Context, tenantID, requestID string, answer ConsentAnswer) (*PlacementConsentRequest, error) {
	if answer.State != ConsentGranted && answer.State != ConsentRefused {
		return nil, &ConsentInvalidError{Field: "answer.state", Reason: fmt.Sprintf("is %q; want granted or refused", answer.State)}
	}
	if answer.By == "" {
		return nil, &ConsentInvalidError{Field: "answer.by", Reason: "is empty; an answer names its decider"}
	}
	key, err := consentKey(tenantID, requestID)
	if err != nil {
		return nil, err
	}
	raw, current, err := l.read(ctx, tenantID, requestID, key)
	if err != nil {
		return nil, err
	}
	if current.State != ConsentPending {
		return nil, &ConsentNotPendingError{TenantID: tenantID, RequestID: requestID, State: current.State}
	}
	next := *current
	next.State = answer.State
	next.By = answer.By
	next.Reason = answer.Reason
	next.AnsweredAt = l.now()
	data, err := json.Marshal(&next)
	if err != nil {
		return nil, fmt.Errorf("placement consent %q: encode: %w", requestID, err)
	}
	swapped, err := l.swapper.CompareAndSwap(ctx, key, raw, data)
	if err != nil {
		return nil, fmt.Errorf("placement consent %q: answer: %w", requestID, err)
	}
	if !swapped {
		return nil, &ConsentStaleError{TenantID: tenantID, RequestID: requestID}
	}
	return &next, nil
}

func (l *consentLedger) Get(ctx context.Context, tenantID, requestID string) (*PlacementConsentRequest, error) {
	key, err := consentKey(tenantID, requestID)
	if err != nil {
		return nil, err
	}
	_, req, err := l.read(ctx, tenantID, requestID, key)
	return req, err
}

func (l *consentLedger) List(ctx context.Context, tenantID string, state ConsentState) ([]*PlacementConsentRequest, error) {
	if state != "" && !state.known() {
		return nil, &ConsentInvalidError{Field: "state", Reason: fmt.Sprintf("is %q", state)}
	}
	prefix, err := tenantPrefix(consentKeyspace, tenantID)
	if err != nil {
		return nil, err
	}
	var requests []*PlacementConsentRequest
	err = l.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		req, err := decodeConsent(tenantID, idFromKey(key, prefix), value)
		if err != nil {
			return err
		}
		if state == "" || req.State == state {
			requests = append(requests, req)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].ID < requests[j].ID })
	return requests, nil
}

func (l *consentLedger) Granted(ctx context.Context, tenantID string, move ConsentMove) (bool, error) {
	if err := move.validate(); err != nil {
		return false, err
	}
	id := move.RequestID(tenantID)
	key, err := consentKey(tenantID, id)
	if err != nil {
		return false, err
	}
	_, req, err := l.read(ctx, tenantID, id, key)
	if err != nil {
		if errors.Is(err, ErrConsentNotFound) {
			return false, nil
		}
		return false, err
	}
	if req.State == ConsentIncoherent {
		return false, &ConsentIncoherentError{TenantID: tenantID, RequestID: id}
	}
	return req.State == ConsentGranted, nil
}

// read returns the stored bytes and the decoded request. A missing key is
// *ConsentNotFoundError; any other read failure or undecodable bytes is an
// error, never a default.
func (l *consentLedger) read(ctx context.Context, tenantID, requestID, key string) ([]byte, *PlacementConsentRequest, error) {
	raw, err := l.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, nil, &ConsentNotFoundError{TenantID: tenantID, RequestID: requestID}
		}
		return nil, nil, fmt.Errorf("placement consent %q: read: %w", requestID, err)
	}
	if raw == nil {
		return nil, nil, &ConsentNotFoundError{TenantID: tenantID, RequestID: requestID}
	}
	req, err := decodeConsent(tenantID, requestID, raw)
	if err != nil {
		return nil, nil, err
	}
	return raw, req, nil
}

// decodeConsent decodes a stored request read from requestID's key in
// tenantID, and marks it ConsentIncoherent when it does not describe that key.
func decodeConsent(tenantID, requestID string, raw []byte) (*PlacementConsentRequest, error) {
	var req PlacementConsentRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("placement consent %q: decode: %w", requestID, err)
	}
	if !req.coherent(tenantID, requestID) {
		req.State = ConsentIncoherent
	}
	return &req, nil
}

// coherent reports whether a stored request describes the key it was read
// from: its tenant and id are the key's, its move hashes to that id, its
// state is a stored one, and it carries a decider exactly when answered.
func (r *PlacementConsentRequest) coherent(tenantID, requestID string) bool {
	if r.TenantID != tenantID || r.ID != requestID || r.Move.RequestID(tenantID) != requestID {
		return false
	}
	switch r.State {
	case ConsentPending:
		return r.By == ""
	case ConsentGranted, ConsentRefused:
		return r.By != ""
	default:
		return false
	}
}

func (s ConsentState) known() bool {
	switch s {
	case ConsentPending, ConsentGranted, ConsentRefused, ConsentIncoherent:
		return true
	}
	return false
}

// ConsentInvalidError refuses a move or an answer that cannot be recorded.
type ConsentInvalidError struct {
	Field  string
	Reason string
}

func (e *ConsentInvalidError) Error() string {
	return fmt.Sprintf("placement consent: %s %s", e.Field, e.Reason)
}

// Unwrap returns ErrConsentInvalid.
func (e *ConsentInvalidError) Unwrap() error { return ErrConsentInvalid }

// ConsentNotFoundError: no request of that id in the tenant.
type ConsentNotFoundError struct {
	TenantID  string
	RequestID string
}

func (e *ConsentNotFoundError) Error() string {
	return fmt.Sprintf("placement consent request %q not found in tenant %q", e.RequestID, e.TenantID)
}

// Unwrap returns ErrConsentNotFound.
func (e *ConsentNotFoundError) Unwrap() error { return ErrConsentNotFound }

// ConsentNotPendingError refuses an answer to a request that is already
// answered, or incoherent. An answer is final.
type ConsentNotPendingError struct {
	TenantID  string
	RequestID string
	State     ConsentState
}

func (e *ConsentNotPendingError) Error() string {
	return fmt.Sprintf("placement consent request %q in tenant %q is %s, not pending", e.RequestID, e.TenantID, e.State)
}

// Unwrap returns ErrConsentNotPending.
func (e *ConsentNotPendingError) Unwrap() error { return ErrConsentNotPending }

// ConsentStaleError refuses an answer whose swap lost to another writer
// between its read and its write. The caller re-reads; the other answer
// stands.
type ConsentStaleError struct {
	TenantID  string
	RequestID string
}

func (e *ConsentStaleError) Error() string {
	return fmt.Sprintf("placement consent request %q in tenant %q changed while it was answered; read it again", e.RequestID, e.TenantID)
}

// Unwrap returns ErrConsentStale.
func (e *ConsentStaleError) Unwrap() error { return ErrConsentStale }

// ConsentIncoherentError refuses to use a stored request that does not
// describe its own key. It grants nothing.
type ConsentIncoherentError struct {
	TenantID  string
	RequestID string
}

func (e *ConsentIncoherentError) Error() string {
	return fmt.Sprintf("placement consent request %q in tenant %q does not match its id", e.RequestID, e.TenantID)
}

// Unwrap returns ErrConsentIncoherent.
func (e *ConsentIncoherentError) Unwrap() error { return ErrConsentIncoherent }

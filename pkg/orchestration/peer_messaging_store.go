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

package orchestration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

const (
	// inboxKeyPrefix is the KV key prefix for inbox messages.
	inboxKeyPrefix = "inbox:"

	// dedupKeyPrefix is the KV key prefix for the durable delivered marker.
	dedupKeyPrefix = "dedup:"

	// dedupClaimKeyPrefix is the KV key prefix for the transient delivery
	// claim.
	dedupClaimKeyPrefix = "dedupclaim:"

	// subscriberBufferSize is the capacity of per-agent subscriber channels.
	subscriberBufferSize = 100
)

// =============================================================================
// Exactly-once delivery: two keys, and why one cannot do the job
// =============================================================================
//
// One message ID owns two keys, because delivery has three states and one key
// distinguishes only two of them.
//
//	dedupclaim:{tenant}:{agent}:{msgID}  the CLAIM   — transient
//	dedup:{tenant}:{agent}:{msgID}       the MARKER  — durable
//
// The claim decides WHO delivers. PutIfAbsent settles that in one operation on
// the store's own plane, so two nodes racing one message ID produce one winner
// and one inbox entry. It is released on failure and after a completed
// delivery, so it is normally absent.
//
// The marker records THAT a delivery happened, and it is what
// DuplicateMessageError is now built on. It has to be a separate key rather
// than a second value under the claim key, because the verbs available are a
// guarded CREATE and a guarded DELETE and there is no guarded SWAP: promoting
// a claim value to a delivered value in place would have to be
// CompareAndDelete followed by PutIfAbsent, which manufactures a moment when
// the key is absent — and in that moment another sender wins a fresh claim and
// delivers the message a second time. (account_store.go makes the same point
// about a scope pointer, for the same reason.)
//
// THE THIRD STATE IS WHY INBOX PRESENCE CANNOT STAND IN FOR THE MARKER. A
// delivered message that has been acknowledged has no inbox entry —
// Acknowledge removes the entry and deliberately keeps the dedup record — and
// an inbox key embeds the sender's timestamp, so a redelivery carrying a
// different timestamp derives a key that was never written. Both look exactly
// like "not delivered" to a reader of the inbox, and both are the ordinary
// lifecycle rather than a corner. Asking the marker instead answers all three
// states: marker present is delivered, claim held with no marker is undecidable
// and retryable, neither present is a free message ID.

// inboxClaim is the value written under a claim key while one delivery is
// being decided.
//
// THE NONCE IS LOAD-BEARING AND IS NOT DECORATION. The claim is released with
// CompareAndDelete against this exact value, so the value must identify THIS
// attempt. A constant or empty value would compare equal to every claim any
// attempt ever wrote, and a slow releaser would then delete a REDELIVERY's
// fresh claim — handing a third sender a claim the second sender still holds.
// A constant is safe only under "nothing ever deletes a claim", an invariant
// that dies the moment a reaper exists, which WP10 is.
type inboxClaim struct {
	// Nonce identifies one attempt. Random per Send, never reused.
	Nonce string `json:"nonce"`

	// Sender names the agent that took the claim. Diagnostic to the decision,
	// load-bearing to an operator staring at a stalled claim.
	Sender string `json:"sender"`

	ClaimedAt time.Time `json:"claimed_at"`
}

// inboxDelivery is the durable marker proving one message ID reached one
// inbox. It outlives the inbox entry, which Acknowledge removes.
type inboxDelivery struct {
	Sender      string    `json:"sender"`
	DeliveredAt time.Time `json:"delivered_at"`
}

// kvPeerMessagingService is a KV-backed implementation of PeerMessagingService.
// It stores messages as JSON under structured keys and decides delivery with a
// guarded claim. Subscriber notifications are delivered via non-blocking
// channel sends.
type kvPeerMessagingService struct {
	store  interfaces.KVStore
	policy InboxPolicy
	logger interfaces.Logger
	subs   sync.Map // key: "tenantID:agentID" -> value: chan *PeerMessage

	// creator and deleter are store's guarded-write capabilities, asserted
	// once at construction. Nil when the backing store cannot decide a
	// contested delivery, in which case Send refuses rather than probing and
	// hoping.
	creator guardedCreator
	deleter guardedDeleter
}

// NewPeerMessagingService creates a new KV-backed peer messaging service.
//
// The backing store MUST be able to decide a guarded write: delivery is
// arbitrated by PutIfAbsent on a claim key and released by CompareAndDelete on
// that claim's exact value. A store offering neither refuses every Send rather
// than falling back to a probe — see PeerInboxUnguardedError. The capability is
// discovered by type assertion because not every KVStore sits somewhere that
// can honour a guarded write, which is go-qrdb's own prescribed pattern and the
// one taskStore and accountStore already follow.
func NewPeerMessagingService(store interfaces.KVStore, policy InboxPolicy, logger interfaces.Logger) PeerMessagingService {
	svc := &kvPeerMessagingService{
		store:  store,
		policy: policy,
		logger: logger,
	}
	// Asserted once here rather than per call: the capability is a static
	// property of the injected store, so probing it on every Send would ask
	// the same question forever and answer it identically.
	if creator, ok := store.(guardedCreator); ok {
		svc.creator = creator
	}
	if deleter, ok := store.(guardedDeleter); ok {
		svc.deleter = deleter
	}
	return svc
}

// Send delivers a message to the recipient agent's inbox exactly once.
//
// The order of the steps IS the guarantee, and each one is placed where it is
// for a reason a rearrangement would break:
//
//  1. Take the claim. Losing it means another attempt is deciding this ID, and
//     the marker says whether that attempt already succeeded.
//  2. Having won the claim, ask the marker whether this ID was delivered
//     before. Safe as a probe-then-write in a way the code this replaced was
//     not: exclusion here comes from a claim decided on the store's own plane
//     and held cluster-wide, not from a process-local lock. store_keys.go
//     records four separate comments that claimed a keyed mutex made a probe
//     and a write atomic, and each was true in one process and false across a
//     fleet. THIS IS NOT A FIFTH. Delete the claim and the probe is worthless;
//     that is exactly the difference.
//  3. Capacity is checked INSIDE the claim. Outside it, an attempt could win
//     the right to deliver and then be refused admission, burning the message's
//     only delivery attempt and dropping it silently.
//  4. Write the inbox entry, then the marker. Marker last, because a marker
//     without a message would suppress a delivery that never happened.
//  5. Release the claim last of all. Marker before release means there is no
//     moment when the ID is unclaimed AND unmarked, which is the moment a
//     second delivery would slip through.
//
// Every failure BEFORE the inbox write releases the claim, so a refused send
// leaves nothing behind. Failures AFTER it deliberately do not — see the
// marker-write branch.
func (s *kvPeerMessagingService) Send(ctx context.Context, msg *PeerMessage) error {
	if err := s.validateMessage(msg); err != nil {
		return err
	}
	if err := s.guarded(); err != nil {
		return err
	}

	// Check message size.
	data, err := json.Marshal(msg)
	if err != nil {
		return &PeerInboxError{Op: "marshal", Cause: err}
	}
	if s.policy.MaxMessageSize > 0 && len(data) > s.policy.MaxMessageSize {
		return &MessageTooLargeError{Size: len(data), Max: s.policy.MaxMessageSize}
	}

	tenantID := s.tenantIDFromMessage(msg)
	dedupKey := s.dedupKey(tenantID, msg.To, msg.ID)
	claimKey := s.dedupClaimKey(tenantID, msg.To, msg.ID)

	// ---- 1. Take the claim. ------------------------------------------------
	claim, err := s.newClaim(msg.From)
	if err != nil {
		return err
	}
	marker, err := s.newDelivery(msg.From)
	if err != nil {
		return err
	}
	won, err := s.creator.PutIfAbsent(ctx, claimKey, claim)
	if err != nil {
		return &PeerInboxError{Op: "dedup_claim", Cause: err}
	}
	if !won {
		return s.lostClaim(ctx, tenantID, msg.To, msg.ID, dedupKey)
	}

	// ---- 2. Was this ID already delivered? ---------------------------------
	delivered, err := s.store.Exists(ctx, dedupKey)
	if err != nil {
		s.releaseClaim(ctx, claimKey, claim)
		return &PeerInboxError{Op: "dedup_check", Cause: err}
	}
	if delivered {
		s.releaseClaim(ctx, claimKey, claim)
		return &DuplicateMessageError{TenantID: tenantID, AgentID: msg.To, MsgID: msg.ID}
	}

	// ---- 3. Capacity, inside the claim and approximate. ---------------------
	size, err := s.InboxSize(ctx, tenantID, msg.To)
	if err != nil {
		s.releaseClaim(ctx, claimKey, claim)
		return err
	}
	evict := false
	if s.policy.MaxMessages > 0 && size >= s.policy.MaxMessages {
		switch s.policy.OverflowAction {
		case "drop_oldest":
			// Deferred until the inbox write lands. Evicting first would
			// destroy a held message on behalf of a delivery that might then
			// fail, which trades a bounded overshoot for an unbounded loss.
			evict = true
		default:
			// "reject" is the default overflow action.
			s.releaseClaim(ctx, claimKey, claim)
			return &InboxFullError{
				TenantID: tenantID,
				AgentID:  msg.To,
				Size:     size,
				Max:      s.policy.MaxMessages,
			}
		}
	}

	// ---- 4. Deliver, then record the delivery. ------------------------------
	inboxKey := s.inboxKey(tenantID, msg.To, msg.Timestamp, msg.ID)
	if err := s.store.Put(ctx, inboxKey, data); err != nil {
		s.releaseClaim(ctx, claimKey, claim)
		return &PeerInboxError{Op: "put_message", Cause: err}
	}

	if _, err := s.creator.PutIfAbsent(ctx, dedupKey, marker); err != nil {
		// THE CLAIM IS DELIBERATELY NOT RELEASED HERE, and this is the one
		// place the release-on-failure rule stops. The message is IN the
		// inbox; releasing would let a retry take a fresh claim, find no
		// marker, and deliver it a second time under its own timestamp — a
		// real duplicate, which is the failure this whole path exists to
		// remove. Holding the claim stalls redelivery loudly instead, until
		// WP10's run-is-authority reaping clears it. Loud stall over silent
		// duplicate, the same trade every guard in this package makes.
		return &PeerInboxError{Op: "put_dedup", Cause: err}
	}

	if evict {
		if err := s.dropOldest(ctx, tenantID, msg.To, inboxKey); err != nil {
			// The delivery itself is complete and recorded, so the claim is
			// released even though this call reports an error: the marker now
			// answers every later sender, and holding the claim as well would
			// stall nothing useful. Only the eviction failed, which leaves the
			// inbox over its approximate limit until the next delivery.
			s.releaseClaim(ctx, claimKey, claim)
			return err
		}
	}

	// ---- 5. Release the claim, then notify. ---------------------------------
	s.releaseClaim(ctx, claimKey, claim)

	// Notify subscriber (non-blocking).
	s.notifySubscriber(tenantID, msg.To, msg)

	return nil
}

// Poll returns messages in the recipient's inbox with timestamps >= since.
func (s *kvPeerMessagingService) Poll(ctx context.Context, tenantID, agentID string, since int64) ([]*PeerMessage, error) {
	prefix := inboxKeyPrefix + tenantID + ":" + agentID + ":"
	var messages []*PeerMessage

	err := s.store.Scan(ctx, prefix, func(key string, value []byte) error {
		ts, parseErr := s.extractTimestamp(key, tenantID, agentID)
		if parseErr != nil {
			// Skip unparseable keys.
			s.logger.Debugf("peer messaging: skipping unparseable inbox key: %s", key)
			return nil
		}
		if ts < since {
			return nil
		}
		var msg PeerMessage
		if err := json.Unmarshal(value, &msg); err != nil {
			s.logger.Debugf("peer messaging: skipping corrupt inbox entry: %s", key)
			return nil
		}
		messages = append(messages, &msg)
		return nil
	})
	if err != nil {
		return nil, &PeerInboxError{Op: "poll_scan", Cause: err}
	}

	// Sort by timestamp ascending (chronological order).
	sort.Slice(messages, func(i, j int) bool {
		if messages[i].Timestamp == messages[j].Timestamp {
			return messages[i].ID < messages[j].ID
		}
		return messages[i].Timestamp < messages[j].Timestamp
	})

	return messages, nil
}

// Subscribe returns a channel that receives new messages for the agent.
func (s *kvPeerMessagingService) Subscribe(_ context.Context, tenantID, agentID string) (<-chan *PeerMessage, error) {
	if tenantID == "" {
		return nil, &PeerSubscriptionError{TenantID: tenantID, AgentID: agentID, Message: "tenant ID is required"}
	}
	if agentID == "" {
		return nil, &PeerSubscriptionError{TenantID: tenantID, AgentID: agentID, Message: "agent ID is required"}
	}

	ch := make(chan *PeerMessage, subscriberBufferSize)
	subKey := tenantID + ":" + agentID
	s.subs.Store(subKey, ch)

	return ch, nil
}

// Acknowledge removes a message from the agent's inbox. The delivered marker
// is retained, which is why the marker rather than the inbox entry is what
// DuplicateMessageError is decided on: after this call the message is gone from
// the inbox and still must not be delivered again.
func (s *kvPeerMessagingService) Acknowledge(ctx context.Context, tenantID, agentID, msgID string) error {
	prefix := inboxKeyPrefix + tenantID + ":" + agentID + ":"
	var targetKey string

	err := s.store.Scan(ctx, prefix, func(key string, _ []byte) error {
		// Keys are formatted as inbox:{tenant}:{agent}:{timestamp}:{msgID}
		// Extract the msgID from the end of the key.
		if s.extractMsgID(key) == msgID {
			targetKey = key
		}
		return nil
	})
	if err != nil {
		return &PeerInboxError{Op: "ack_scan", Cause: err}
	}

	if targetKey == "" {
		// Message not found; this is idempotent, so return nil.
		return nil
	}

	if err := s.store.Delete(ctx, targetKey); err != nil {
		return &PeerInboxError{Op: "ack_delete", Cause: err}
	}

	return nil
}

// InboxSize returns the number of messages currently in the agent's inbox.
func (s *kvPeerMessagingService) InboxSize(ctx context.Context, tenantID, agentID string) (int, error) {
	prefix := inboxKeyPrefix + tenantID + ":" + agentID + ":"
	keys, err := s.store.List(ctx, prefix)
	if err != nil {
		return 0, &PeerInboxError{Op: "inbox_size", Cause: err}
	}
	return len(keys), nil
}

// =============================================================================
// Internal Helpers
// =============================================================================

// =============================================================================
// The delivery claim
// =============================================================================

// guarded reports whether the backing store can decide a contested delivery.
//
// Checked before Send touches anything, so a store without the capability
// refuses up front instead of discovering a missing capability partway through
// a two-key sequence and leaving one of them written.
func (s *kvPeerMessagingService) guarded() error {
	if s.creator == nil || s.deleter == nil {
		return &PeerInboxUnguardedError{
			Message: "exactly-once delivery is decided by a guarded claim; this store can neither take one nor release it",
		}
	}
	return nil
}

// newClaim builds this attempt's claim value.
func (s *kvPeerMessagingService) newClaim(sender string) ([]byte, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, &PeerInboxError{Op: "claim_nonce", Cause: err}
	}
	claim, err := json.Marshal(&inboxClaim{
		Nonce:     hex.EncodeToString(nonce),
		Sender:    sender,
		ClaimedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, &PeerInboxError{Op: "claim_encode", Cause: err}
	}
	return claim, nil
}

// newDelivery builds the durable marker recording that a delivery landed.
//
// BUILT BEFORE THE INBOX WRITE, NOT AFTER IT, and the ordering is the point
// rather than a convenience. Encoding is the last thing that can fail without
// consequence: once the message is in the inbox, a failure to produce its
// marker leaves a delivery that no later send can see, and there is nothing
// useful to do about it at that point. Every way of failing to build the
// marker therefore happens while the inbox is still untouched and the claim can
// simply be given back.
func (s *kvPeerMessagingService) newDelivery(sender string) ([]byte, error) {
	marker, err := json.Marshal(&inboxDelivery{Sender: sender, DeliveredAt: time.Now().UTC()})
	if err != nil {
		return nil, &PeerInboxError{Op: "marker_encode", Cause: err}
	}
	return marker, nil
}

// lostClaim answers an attempt that did not win the claim.
//
// The marker is the ONLY evidence consulted, and the distinction it draws is
// the point of the whole two-key layout: present means this message reached
// the inbox and the caller is holding a genuine duplicate; absent means
// another attempt holds the claim and has not recorded a delivery, which is
// either a holder still in flight or a holder that died — states no read can
// tell apart. Reporting a duplicate for the second case would tell a sender
// its message arrived when it may never have.
func (s *kvPeerMessagingService) lostClaim(ctx context.Context, tenantID, agentID, msgID, dedupKey string) error {
	delivered, err := s.store.Exists(ctx, dedupKey)
	if err != nil {
		return &PeerInboxError{Op: "dedup_check", Cause: err}
	}
	if delivered {
		return &DuplicateMessageError{TenantID: tenantID, AgentID: agentID, MsgID: msgID}
	}
	return &DeliveryInFlightError{TenantID: tenantID, AgentID: agentID, MsgID: msgID}
}

// releaseClaim gives up a claim this attempt is holding.
//
// GUARDED ON THE EXACT CLAIM VALUE, never a blind Delete. A release that
// deleted whatever it found would delete a REDELIVERY's fresh claim whenever
// this attempt was slow enough for its own claim to have been reaped and
// retaken — handing two senders the same claim, which is the outcome the claim
// exists to prevent. CompareAndDelete removes only the claim this attempt
// wrote, so a stale releaser deletes nothing.
//
// Failure is reported and swallowed. The caller is already returning a refusal
// or has already delivered; an unreleased claim stalls later attempts on this
// one message ID until it is reaped, and turning it into a second error would
// replace an accurate answer with a bookkeeping complaint.
func (s *kvPeerMessagingService) releaseClaim(ctx context.Context, claimKey string, claim []byte) {
	released, err := s.deleter.CompareAndDelete(ctx, claimKey, claim)
	if err != nil {
		s.logger.Warnf("peer messaging: failed to release delivery claim %s: %v", claimKey, err)
		return
	}
	if !released {
		// The claim moved on: reaped, or already released. Either way it is
		// not this attempt's to remove.
		s.logger.Debugf("peer messaging: delivery claim no longer held by this attempt: %s", claimKey)
	}
}

// validateMessage checks that all required fields are populated.
func (s *kvPeerMessagingService) validateMessage(msg *PeerMessage) error {
	if msg == nil {
		return &PeerMessageValidationError{Field: "message", Message: "message must not be nil"}
	}
	if msg.ID == "" {
		return &PeerMessageValidationError{Field: "id", Message: "message ID is required"}
	}
	if msg.From == "" {
		return &PeerMessageValidationError{Field: "from", Message: "sender is required"}
	}
	if msg.To == "" {
		return &PeerMessageValidationError{Field: "to", Message: "recipient is required"}
	}
	if msg.Action == "" {
		return &PeerMessageValidationError{Field: "action", Message: "action is required"}
	}
	return nil
}

// tenantIDFromMessage extracts the tenant ID from message metadata.
// Falls back to "default" if not specified.
func (s *kvPeerMessagingService) tenantIDFromMessage(msg *PeerMessage) string {
	if msg.Metadata != nil {
		if tid, ok := msg.Metadata["tenant_id"]; ok && tid != "" {
			return tid
		}
	}
	return "default"
}

// inboxKey builds the KV key for an inbox message.
// Format: inbox:{tenantID}:{agentID}:{timestamp_nanos}:{msgID}.
func (s *kvPeerMessagingService) inboxKey(tenantID, agentID string, timestamp int64, msgID string) string {
	return inboxKeyPrefix + tenantID + ":" + agentID + ":" + strconv.FormatInt(timestamp, 10) + ":" + msgID
}

// dedupKey builds the KV key holding the durable delivered marker.
// Format: dedup:{tenantID}:{agentID}:{msgID}.
func (s *kvPeerMessagingService) dedupKey(tenantID, agentID, msgID string) string {
	return dedupKeyPrefix + tenantID + ":" + agentID + ":" + msgID
}

// dedupClaimKey builds the KV key holding the transient delivery claim.
// Format: dedupclaim:{tenantID}:{agentID}:{msgID}.
func (s *kvPeerMessagingService) dedupClaimKey(tenantID, agentID, msgID string) string {
	return dedupClaimKeyPrefix + tenantID + ":" + agentID + ":" + msgID
}

// extractTimestamp parses the timestamp from an inbox key.
// Key format: inbox:{tenantID}:{agentID}:{timestamp}:{msgID}.
func (s *kvPeerMessagingService) extractTimestamp(key, tenantID, agentID string) (int64, error) {
	// Skip past "inbox:{tenantID}:{agentID}:"
	prefixLen := len(inboxKeyPrefix) + len(tenantID) + 1 + len(agentID) + 1
	if prefixLen >= len(key) {
		return 0, &PeerInboxError{Op: "extract_timestamp", Cause: nil}
	}
	rest := key[prefixLen:]
	// Find the colon separating timestamp from msgID.
	for i := 0; i < len(rest); i++ {
		if rest[i] == ':' {
			return strconv.ParseInt(rest[:i], 10, 64)
		}
	}
	// No colon found; entire rest is the timestamp (shouldn't happen with valid keys).
	return strconv.ParseInt(rest, 10, 64)
}

// extractMsgID returns the message ID from an inbox key.
// Key format: inbox:{tenantID}:{agentID}:{timestamp}:{msgID}.
func (s *kvPeerMessagingService) extractMsgID(key string) string {
	// Walk backwards to find the last colon.
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == ':' {
			return key[i+1:]
		}
	}
	return ""
}

// dropOldest evicts the oldest message from the agent's inbox, excluding the
// message just delivered.
//
// THE EXCLUSION IS REQUIRED, not a refinement. Eviction now runs after the
// inbox write, so without it a message whose timestamp is older than everything
// already queued would evict ITSELF: Send would delete the entry it had just
// written, discard its own marker, and return nil — a silent drop reported as a
// successful delivery, which is precisely the outcome this design exists to
// make impossible. Excluding the new key always leaves a candidate, because
// eviction runs only when the inbox was already at its limit.
//
// EVICTION ALSO CLEARS THE EVICTED MESSAGE'S MARKER. An eviction means the
// system FORGOT the message, and a marker outliving the message it describes
// would suppress a re-send of something that was thrown away — the message
// would be permanently undeliverable, having never been read. The marker is
// removed with a guarded delete against the exact value read, so an eviction
// racing a legitimate rewrite of that key removes only what it actually saw.
func (s *kvPeerMessagingService) dropOldest(ctx context.Context, tenantID, agentID, keepKey string) error {
	prefix := inboxKeyPrefix + tenantID + ":" + agentID + ":"
	keys, err := s.store.List(ctx, prefix)
	if err != nil {
		return &PeerInboxError{Op: "drop_oldest_list", Cause: err}
	}

	// Sort keys to find the oldest (lowest timestamp).
	sort.Strings(keys)
	oldestKey := ""
	for _, key := range keys {
		if key != keepKey {
			oldestKey = key
			break
		}
	}
	if oldestKey == "" {
		return nil
	}

	// The inbox entry goes first. The reverse order would leave a window in
	// which the message is queued with no marker, and a concurrent re-send
	// would take a fresh claim and queue a SECOND copy under its own
	// timestamp. Failing here instead orphans a marker, which is bounded debt
	// and is what this code did before eviction touched markers at all.
	if err := s.store.Delete(ctx, oldestKey); err != nil {
		return &PeerInboxError{Op: "drop_oldest_delete", Cause: err}
	}
	if err := s.forgetDelivery(ctx, tenantID, agentID, s.extractMsgID(oldestKey)); err != nil {
		return err
	}

	s.logger.Debugf("peer messaging: dropped oldest message from inbox: %s", oldestKey)
	return nil
}

// forgetDelivery removes the delivered marker for an evicted message, so a
// re-send of it is admissible again.
func (s *kvPeerMessagingService) forgetDelivery(ctx context.Context, tenantID, agentID, msgID string) error {
	if msgID == "" {
		// An inbox key with no trailing identifier describes no message, so
		// there is no marker it could name.
		return nil
	}
	dedupKey := s.dedupKey(tenantID, agentID, msgID)
	marker, err := s.store.Get(ctx, dedupKey)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil
		}
		return &PeerInboxError{Op: "drop_oldest_marker_read", Cause: err}
	}
	if marker == nil {
		// Nothing to compare against, so nothing this call can safely delete.
		return nil
	}
	if _, err := s.deleter.CompareAndDelete(ctx, dedupKey, marker); err != nil {
		return &PeerInboxError{Op: "drop_oldest_marker_delete", Cause: err}
	}
	return nil
}

// notifySubscriber sends a message to the subscriber channel via non-blocking send.
func (s *kvPeerMessagingService) notifySubscriber(tenantID, agentID string, msg *PeerMessage) {
	subKey := tenantID + ":" + agentID
	val, ok := s.subs.Load(subKey)
	if !ok {
		return
	}
	ch, ok := val.(chan *PeerMessage)
	if !ok {
		return
	}
	// Non-blocking send: drop notification if subscriber buffer is full.
	select {
	case ch <- msg:
	default:
		s.logger.Debugf("peer messaging: subscriber buffer full, dropping notification for %s", subKey)
	}
}

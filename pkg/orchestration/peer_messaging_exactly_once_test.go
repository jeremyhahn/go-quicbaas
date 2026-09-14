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
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// =============================================================================
// The race harness
// =============================================================================

// peerRaceKVStore fires a hook from INSIDE a store call, so a competing
// delivery runs while the caller is mid-decision.
//
// THE PLACEMENT IS THE WHOLE TEST. Two sends issued one after the other exercise
// nothing: the first finishes before the second begins, and a store with no
// arbitration at all passes. The window has to open at the instant the racing
// call has read state it is about to act on, which is why Exists fires its hook
// AFTER reading and returns the value it read BEFORE the competitor ran. That
// stale answer is exactly what a real contender acts on, and it is what the
// unguarded code turned into a second delivery.
type peerRaceKVStore struct {
	*peerTestKVStore

	mu      sync.Mutex
	hookOn  string // "exists" or "putifabsent"
	hookKey string
	hook    func()
	fired   bool
}

func newPeerRaceKVStore(on, key string, hook func()) *peerRaceKVStore {
	return &peerRaceKVStore{
		peerTestKVStore: newPeerTestKVStore(),
		hookOn:          on,
		hookKey:         key,
		hook:            hook,
	}
}

// fire opens the window at most once. Once only, because the competing send
// re-enters the same store methods and an unguarded hook would recurse forever.
func (s *peerRaceKVStore) fire(on, key string) {
	s.mu.Lock()
	if s.fired || s.hook == nil || on != s.hookOn || key != s.hookKey {
		s.mu.Unlock()
		return
	}
	s.fired = true
	hook := s.hook
	s.mu.Unlock()
	hook()
}

// didFire reports whether the window ever opened. Asserted by every test using
// this harness: a barrier that silently stopped synchronising would turn every
// later run into a false green.
func (s *peerRaceKVStore) didFire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fired
}

func (s *peerRaceKVStore) Exists(ctx context.Context, key string) (bool, error) {
	found, err := s.peerTestKVStore.Exists(ctx, key)
	s.fire("exists", key)
	return found, err
}

func (s *peerRaceKVStore) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	s.fire("putifabsent", key)
	return s.peerTestKVStore.PutIfAbsent(ctx, key, value)
}

// inboxKeysFor returns the recipient's inbox keys — the damage plane. Every
// race assertion in this file lands here rather than on the losers' error
// values: an error says what one caller was told, and the question exactly-once
// answers is how many copies of the message exist.
func inboxKeysFor(t *testing.T, store *peerTestKVStore, tenantID, agentID string) []string {
	t.Helper()
	keys, err := store.List(context.Background(), inboxKeyPrefix+tenantID+":"+agentID+":")
	require.NoError(t, err)
	return keys
}

// =============================================================================
// The race: two senders, one message ID
// =============================================================================

// TestPeerMessaging_Race_CompetitorDuringClaimHolderDecision places the window
// where the unguarded code lost: the holder has read "not delivered" and is
// about to write, and a competing send of the same ID runs in that gap.
//
// The two attempts carry DIFFERENT timestamps, which is what makes a duplicate
// visible. An inbox key embeds the timestamp, so two deliveries at the same
// timestamp overwrite one another and a broken store looks healthy; at
// different timestamps they are two entries, and two entries is the defect.
func TestPeerMessaging_Race_CompetitorDuringClaimHolderDecision(t *testing.T) {
	ctx := context.Background()
	var svc PeerMessagingService
	var competitorErr error

	store := newPeerRaceKVStore("exists", dedupKeyPrefix+testTenantID+":agent-b:msg-race", func() {
		// Fired from inside the holder's own marker probe: the holder owns the
		// claim right now and has not yet written anything.
		competitorErr = svc.Send(ctx, testMessage("msg-race", "agent-a", "agent-b", "advisory", 2000))
	})
	svc = NewPeerMessagingService(store, defaultPolicy(), &peerTestLogger{})

	holderErr := svc.Send(ctx, testMessage("msg-race", "agent-a", "agent-b", "advisory", 1000))

	require.True(t, store.didFire(), "the race window never opened; this test proved nothing")
	require.NoError(t, holderErr, "the claim holder must complete its delivery")

	// The damage plane: one message ID, one inbox entry.
	keys := inboxKeysFor(t, store.peerTestKVStore, testTenantID, "agent-b")
	require.Len(t, keys, 1, "one message ID must produce exactly one inbox entry")
	require.Equal(t, inboxKeyPrefix+testTenantID+":agent-b:1000:msg-race", keys[0],
		"the surviving entry must be the claim holder's")

	// The competitor lost the claim and must be told the truth about why: the
	// holder had not recorded a delivery, so "already delivered" would have
	// been a lie at that instant.
	var inFlight *DeliveryInFlightError
	require.ErrorAs(t, competitorErr, &inFlight, "the loser must get the retryable class, not a false duplicate")
	require.Equal(t, "msg-race", inFlight.MsgID)
	require.False(t, errors.Is(competitorErr, ErrDuplicateMessage))
}

// TestPeerMessaging_Race_CompetitorCompletesBeforeClaim places the window
// before the claim is taken, so the competitor's delivery finishes — marker
// written, claim released — while this attempt is still reaching for the claim.
//
// The attempt therefore WINS a claim on an ID that was already delivered, which
// is the case the marker probe inside the claim exists for. Without that probe
// a released claim would read as a free message ID.
func TestPeerMessaging_Race_CompetitorCompletesBeforeClaim(t *testing.T) {
	ctx := context.Background()
	var svc PeerMessagingService
	var competitorErr error

	store := newPeerRaceKVStore("putifabsent", dedupClaimKeyPrefix+testTenantID+":agent-b:msg-late", func() {
		competitorErr = svc.Send(ctx, testMessage("msg-late", "agent-a", "agent-b", "advisory", 2000))
	})
	svc = NewPeerMessagingService(store, defaultPolicy(), &peerTestLogger{})

	lateErr := svc.Send(ctx, testMessage("msg-late", "agent-a", "agent-b", "advisory", 1000))

	require.True(t, store.didFire(), "the race window never opened; this test proved nothing")
	require.NoError(t, competitorErr, "the competitor ran unopposed and must have delivered")

	keys := inboxKeysFor(t, store.peerTestKVStore, testTenantID, "agent-b")
	require.Len(t, keys, 1, "the late attempt must not add a second copy")
	require.Equal(t, inboxKeyPrefix+testTenantID+":agent-b:2000:msg-late", keys[0])

	var dup *DuplicateMessageError
	require.ErrorAs(t, lateErr, &dup, "winning a released claim for a delivered ID is a duplicate, not a fresh send")
	require.Equal(t, "msg-late", dup.MsgID)

	// The late attempt must not leave its claim behind: it refused, so it owns
	// nothing.
	held, err := store.Exists(ctx, dedupClaimKeyPrefix+testTenantID+":agent-b:msg-late")
	require.NoError(t, err)
	require.False(t, held, "a refused attempt must release the claim it took")
}

// =============================================================================
// The pin: a delivered message stays delivered after it is consumed
// =============================================================================

// TestPeerMessaging_DeliveredThenAcknowledged_ResendIsDuplicate is the case
// that decided the two-key layout, and it is a pin rather than a nicety.
//
// Acknowledge removes the inbox entry and keeps the delivered marker, so after
// an ordinary consume there is NO inbox entry to find. A design that answered
// "was this delivered?" by looking for the message would call this state
// undelivered and hand the sender a retryable error forever — for the normal
// lifecycle, not a corner case. The marker is what makes the answer survive
// consumption.
func TestPeerMessaging_DeliveredThenAcknowledged_ResendIsDuplicate(t *testing.T) {
	svc, store := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	require.NoError(t, svc.Send(ctx, testMessage("msg-consumed", "agent-a", "agent-b", "advisory", 1000)))
	require.NoError(t, svc.Acknowledge(ctx, testTenantID, "agent-b", "msg-consumed"))

	// Precondition: the inbox really is empty, so the answer cannot be coming
	// from a leftover entry.
	require.Empty(t, inboxKeysFor(t, store, testTenantID, "agent-b"))

	// A redelivery at a DIFFERENT timestamp, which is the second way inbox
	// presence fails as a proxy: this key was never written even before the ack.
	err := svc.Send(ctx, testMessage("msg-consumed", "agent-a", "agent-b", "advisory", 2000))

	var dup *DuplicateMessageError
	require.ErrorAs(t, err, &dup, "a consumed message must still refuse redelivery as a duplicate")
	require.Equal(t, "msg-consumed", dup.MsgID)
	require.False(t, errors.Is(err, ErrDeliveryInFlight), "delivered-and-consumed is decided, not in flight")
	require.Empty(t, inboxKeysFor(t, store, testTenantID, "agent-b"), "the refused resend must not queue anything")
}

// =============================================================================
// A held claim with no delivery is retryable, never a duplicate
// =============================================================================

// TestPeerMessaging_ClaimHeldWithoutDelivery_IsRetryable covers the state a
// crashed holder leaves: claim present, marker absent, nothing in the inbox.
//
// It is indistinguishable from a holder still in flight — the same two keys in
// the same state — so the only honest answer is "retry". Answering "duplicate"
// would tell a sender its message arrived when nothing ever did.
func TestPeerMessaging_ClaimHeldWithoutDelivery_IsRetryable(t *testing.T) {
	svc, store := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	// A holder that took the claim and died before writing anything.
	abandoned, err := json.Marshal(&inboxClaim{Nonce: "abandoned", Sender: "agent-x", ClaimedAt: time.Unix(0, 0).UTC()})
	require.NoError(t, err)
	took, err := store.PutIfAbsent(ctx, dedupClaimKeyPrefix+testTenantID+":agent-b:msg-stalled", abandoned)
	require.NoError(t, err)
	require.True(t, took)

	sendErr := svc.Send(ctx, testMessage("msg-stalled", "agent-a", "agent-b", "advisory", 1000))

	var inFlight *DeliveryInFlightError
	require.ErrorAs(t, sendErr, &inFlight, "a claim with no delivery is retryable, not decided")
	require.Equal(t, "msg-stalled", inFlight.MsgID)
	require.False(t, errors.Is(sendErr, ErrDuplicateMessage), "nothing was delivered, so nothing is a duplicate")
	require.Empty(t, inboxKeysFor(t, store, testTenantID, "agent-b"))

	// The abandoned claim is untouched: this attempt never owned it.
	held, err := store.Get(ctx, dedupClaimKeyPrefix+testTenantID+":agent-b:msg-stalled")
	require.NoError(t, err)
	require.Equal(t, abandoned, held, "a losing attempt must not disturb the holder's claim")
}

// TestPeerMessaging_ClaimHeldWithDelivery_IsDuplicate is the other half of the
// loser's answer, and the state that exists for a real instant on every
// successful delivery: the holder has written the marker and has not yet
// released the claim.
//
// A loser arriving in that window has evidence the message landed, so it gets
// the duplicate rather than the retry. The two tests together are what make
// DeliveryInFlightError a statement about missing evidence rather than a
// blanket answer for everyone who loses a claim.
func TestPeerMessaging_ClaimHeldWithDelivery_IsDuplicate(t *testing.T) {
	svc, store := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	held, err := json.Marshal(&inboxClaim{Nonce: "holder", Sender: "agent-x", ClaimedAt: time.Unix(0, 0).UTC()})
	require.NoError(t, err)
	took, err := store.PutIfAbsent(ctx, dedupClaimKeyPrefix+testTenantID+":agent-b:msg-landed", held)
	require.NoError(t, err)
	require.True(t, took)

	// The holder got as far as recording the delivery.
	marker, err := json.Marshal(&inboxDelivery{Sender: "agent-x", DeliveredAt: time.Unix(0, 0).UTC()})
	require.NoError(t, err)
	require.NoError(t, store.Put(ctx, dedupKeyPrefix+testTenantID+":agent-b:msg-landed", marker))

	sendErr := svc.Send(ctx, testMessage("msg-landed", "agent-a", "agent-b", "advisory", 1000))

	var dup *DuplicateMessageError
	require.ErrorAs(t, sendErr, &dup, "a loser with a marker in hand has proof, not a reason to retry")
	require.Equal(t, "msg-landed", dup.MsgID)
	require.False(t, errors.Is(sendErr, ErrDeliveryInFlight))
}

// TestPeerMessaging_CapacityReadFailure_ReleasesClaim covers a capacity check
// that cannot read the inbox. Nothing was delivered, so the claim goes back.
func TestPeerMessaging_CapacityReadFailure_ReleasesClaim(t *testing.T) {
	kv := newFaultKVStore()
	kv.listErr = errors.New("inbox size unavailable")
	svc := NewPeerMessagingService(kv, defaultPolicy(), &peerTestLogger{})
	ctx := context.Background()

	err := svc.Send(ctx, testMessage("msg-cap", "agent-a", "agent-b", "advisory", 1000))

	var inboxErr *PeerInboxError
	require.ErrorAs(t, err, &inboxErr)
	require.Equal(t, "inbox_size", inboxErr.Op)

	kv.listErr = nil
	held, existsErr := kv.Exists(ctx, dedupClaimKeyPrefix+testTenantID+":agent-b:msg-cap")
	require.NoError(t, existsErr)
	require.False(t, held, "a send that never reached the inbox must give its claim back")
}

// TestPeerMessaging_DropOldest_EvictionDeleteFailure_IsReported covers eviction
// failing to remove the inbox entry it selected. The marker is left alone,
// because clearing it while the message is still queued would let a re-send
// queue a second copy.
func TestPeerMessaging_DropOldest_EvictionDeleteFailure_IsReported(t *testing.T) {
	policy := InboxPolicy{MaxMessages: 1, MaxMessageSize: 10240, OverflowAction: "drop_oldest"}
	kv := newFaultKVStore()
	svc := NewPeerMessagingService(kv, policy, &peerTestLogger{})
	ctx := context.Background()

	require.NoError(t, svc.Send(ctx, testMessage("msg-1", "agent-a", "agent-b", "advisory", 1000)))

	kv.delErr = errors.New("evict failed")
	err := svc.Send(ctx, testMessage("msg-2", "agent-a", "agent-b", "advisory", 2000))

	var inboxErr *PeerInboxError
	require.ErrorAs(t, err, &inboxErr)
	require.Equal(t, "drop_oldest_delete", inboxErr.Op)

	kv.delErr = nil
	marker, existsErr := kv.Exists(ctx, dedupKeyPrefix+testTenantID+":agent-b:msg-1")
	require.NoError(t, existsErr)
	require.True(t, marker, "a message still queued must keep its marker")
}

// TestPeerMessaging_ForgetDelivery_MarkerReadFailure_IsReported covers eviction
// that cannot read the marker it must compare against. A guarded delete needs
// the value it is guarding on, so an unreadable marker stops the eviction
// rather than being removed blind.
func TestPeerMessaging_ForgetDelivery_MarkerReadFailure_IsReported(t *testing.T) {
	kv := newFaultKVStore()
	svc := NewPeerMessagingService(kv, defaultPolicy(), &peerTestLogger{})
	kvSvc := svc.(*kvPeerMessagingService)
	ctx := context.Background()

	kv.getErr = errors.New("marker read failed")
	err := kvSvc.forgetDelivery(ctx, testTenantID, "agent-b", "msg-unreadable")

	var inboxErr *PeerInboxError
	require.ErrorAs(t, err, &inboxErr)
	require.Equal(t, "drop_oldest_marker_read", inboxErr.Op)
}

// =============================================================================
// The nonce: a stale release must not delete a fresh claim
// =============================================================================

// TestPeerMessaging_ReleaseClaim_LeavesAFreshClaimAlone is the reason the claim
// carries a nonce at all, and it is the assertion most at risk of being vacuous.
//
// The two claims here differ ONLY in their nonce — same sender, same instant —
// because that is the field under test. Letting them differ by sender or by
// ClaimedAt would leave the test passing with the nonce removed entirely, which
// would make it decorative rather than load-bearing.
func TestPeerMessaging_ReleaseClaim_LeavesAFreshClaimAlone(t *testing.T) {
	svc, store := newTestPeerService(defaultPolicy())
	kvSvc := svc.(*kvPeerMessagingService)
	ctx := context.Background()
	claimKey := dedupClaimKeyPrefix + testTenantID + ":agent-b:msg-nonce"
	claimedAt := time.Unix(1755000000, 0).UTC()

	stale, err := json.Marshal(&inboxClaim{Nonce: "nonce-one", Sender: "agent-a", ClaimedAt: claimedAt})
	require.NoError(t, err)
	fresh, err := json.Marshal(&inboxClaim{Nonce: "nonce-two", Sender: "agent-a", ClaimedAt: claimedAt})
	require.NoError(t, err)

	// The first attempt's claim is reaped, and a second attempt takes a new one.
	took, err := store.PutIfAbsent(ctx, claimKey, stale)
	require.NoError(t, err)
	require.True(t, took)
	require.NoError(t, store.Delete(ctx, claimKey))
	took, err = store.PutIfAbsent(ctx, claimKey, fresh)
	require.NoError(t, err)
	require.True(t, took)

	// The first attempt, finally waking up, releases what it thinks it holds.
	kvSvc.releaseClaim(ctx, claimKey, stale)

	held, err := store.Get(ctx, claimKey)
	require.NoError(t, err)
	require.Equal(t, fresh, held, "a stale release must not delete the claim a later attempt holds")
}

// TestPeerMessaging_NewClaim_NonceIsPerAttempt proves the nonce actually
// distinguishes two attempts. Asserted on the decoded Nonce field rather than
// on the encoded bytes: the bytes also carry a wall-clock timestamp, so
// comparing them would pass with a constant nonce and prove nothing.
func TestPeerMessaging_NewClaim_NonceIsPerAttempt(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	kvSvc := svc.(*kvPeerMessagingService)

	first, err := kvSvc.newClaim("agent-a")
	require.NoError(t, err)
	second, err := kvSvc.newClaim("agent-a")
	require.NoError(t, err)

	var a, b inboxClaim
	require.NoError(t, json.Unmarshal(first, &a))
	require.NoError(t, json.Unmarshal(second, &b))
	require.NotEmpty(t, a.Nonce)
	require.NotEqual(t, a.Nonce, b.Nonce, "two attempts by one sender must carry different nonces")
	require.Equal(t, "agent-a", a.Sender)
}

// =============================================================================
// Refusals release the claim
// =============================================================================

// TestPeerMessaging_InboxFull_ReleasesClaim covers the refusal path: capacity is
// checked while holding the claim, so a refusal that kept it would leave the
// message ID permanently unsendable — the sender told to retry, and every retry
// blocked by its own abandoned claim.
func TestPeerMessaging_InboxFull_ReleasesClaim(t *testing.T) {
	policy := InboxPolicy{MaxMessages: 2, MaxMessageSize: 10240, OverflowAction: "reject"}
	svc, store := newTestPeerService(policy)
	ctx := context.Background()

	require.NoError(t, svc.Send(ctx, testMessage("msg-1", "agent-a", "agent-b", "advisory", 1000)))
	require.NoError(t, svc.Send(ctx, testMessage("msg-2", "agent-a", "agent-b", "advisory", 2000)))

	err := svc.Send(ctx, testMessage("msg-3", "agent-a", "agent-b", "advisory", 3000))
	require.ErrorIs(t, err, ErrInboxFull)

	held, existsErr := store.Exists(ctx, dedupClaimKeyPrefix+testTenantID+":agent-b:msg-3")
	require.NoError(t, existsErr)
	require.False(t, held, "a capacity refusal must give the claim back")

	// And the proof that giving it back matters: once there is room, the same
	// message sends.
	require.NoError(t, svc.Acknowledge(ctx, testTenantID, "agent-b", "msg-1"))
	require.NoError(t, svc.Send(ctx, testMessage("msg-3", "agent-a", "agent-b", "advisory", 3000)))
}

// TestPeerMessaging_MarkerWriteFailure_HoldsTheClaim covers the ONE failure
// that deliberately does not release.
//
// The message is already in the inbox when the marker write fails. Releasing
// would let a retry take a fresh claim, find no marker, and deliver a second
// copy under its own timestamp — a real duplicate. Holding the claim stalls
// redelivery loudly instead, until it is reaped.
func TestPeerMessaging_MarkerWriteFailure_HoldsTheClaim(t *testing.T) {
	kv := newFaultKVStore()
	kv.putIfAbsentErr = errors.New("marker write failed")
	kv.putIfAbsentErrPrefix = dedupKeyPrefix
	svc := NewPeerMessagingService(kv, defaultPolicy(), &peerTestLogger{})
	ctx := context.Background()

	err := svc.Send(ctx, testMessage("msg-marker", "agent-a", "agent-b", "advisory", 1000))

	var inboxErr *PeerInboxError
	require.ErrorAs(t, err, &inboxErr)
	require.Equal(t, "put_dedup", inboxErr.Op)

	// The message landed, so the claim must still be held.
	held, existsErr := kv.Exists(ctx, dedupClaimKeyPrefix+testTenantID+":agent-b:msg-marker")
	require.NoError(t, existsErr)
	require.True(t, held, "a delivery that landed without a marker must keep its claim")

	// A retry is stalled rather than allowed to deliver a second copy.
	kv.putIfAbsentErr = nil
	retryErr := svc.Send(ctx, testMessage("msg-marker", "agent-a", "agent-b", "advisory", 2000))
	require.ErrorIs(t, retryErr, ErrDeliveryInFlight, "the held claim must stall the retry, not admit it")

	keys, listErr := kv.List(ctx, inboxKeyPrefix+testTenantID+":agent-b:")
	require.NoError(t, listErr)
	require.Len(t, keys, 1, "the stalled retry must not add a second copy")
}

// =============================================================================
// Eviction
// =============================================================================

// TestPeerMessaging_DropOldest_ForgetsTheEvictedMarker covers the pre-existing
// bug the exactly-once work absorbed: an eviction that kept the marker left the
// evicted message permanently undeliverable, deduped against a delivery that had
// been thrown away and never read.
func TestPeerMessaging_DropOldest_ForgetsTheEvictedMarker(t *testing.T) {
	policy := InboxPolicy{MaxMessages: 2, MaxMessageSize: 10240, OverflowAction: "drop_oldest"}
	svc, store := newTestPeerService(policy)
	ctx := context.Background()

	require.NoError(t, svc.Send(ctx, testMessage("msg-1", "agent-a", "agent-b", "advisory", 1000)))
	require.NoError(t, svc.Send(ctx, testMessage("msg-2", "agent-a", "agent-b", "advisory", 2000)))
	require.NoError(t, svc.Send(ctx, testMessage("msg-3", "agent-a", "agent-b", "advisory", 3000)))

	// msg-1 was evicted, so the system forgot it — marker included.
	marker, err := store.Exists(ctx, dedupKeyPrefix+testTenantID+":agent-b:msg-1")
	require.NoError(t, err)
	require.False(t, marker, "an evicted message's marker must go with it")

	// Which is what makes it admissible again.
	require.NoError(t, svc.Send(ctx, testMessage("msg-1", "agent-a", "agent-b", "advisory", 4000)),
		"a message thrown away unread must be re-sendable")

	// The marker for a message still in the inbox is untouched.
	kept, err := store.Exists(ctx, dedupKeyPrefix+testTenantID+":agent-b:msg-3")
	require.NoError(t, err)
	require.True(t, kept)
}

// TestPeerMessaging_DropOldest_NeverEvictsTheMessageJustDelivered covers the
// hazard created by moving eviction after the inbox write: a message older than
// everything queued is, by the eviction rule's own ordering, the oldest entry
// the instant it lands.
//
// Without the exclusion Send would delete the entry it had just written, drop
// its own marker, and return nil — a silent loss reported as a delivery, which
// is the single outcome this design refuses to produce.
func TestPeerMessaging_DropOldest_NeverEvictsTheMessageJustDelivered(t *testing.T) {
	policy := InboxPolicy{MaxMessages: 2, MaxMessageSize: 10240, OverflowAction: "drop_oldest"}
	svc, store := newTestPeerService(policy)
	ctx := context.Background()

	require.NoError(t, svc.Send(ctx, testMessage("msg-2", "agent-a", "agent-b", "advisory", 2000)))
	require.NoError(t, svc.Send(ctx, testMessage("msg-3", "agent-a", "agent-b", "advisory", 3000)))

	// Older than both messages already queued.
	require.NoError(t, svc.Send(ctx, testMessage("msg-1", "agent-a", "agent-b", "advisory", 1000)))

	messages, err := svc.Poll(ctx, testTenantID, "agent-b", 0)
	require.NoError(t, err)
	ids := make([]string, 0, len(messages))
	for _, msg := range messages {
		ids = append(ids, msg.ID)
	}
	require.Equal(t, []string{"msg-1", "msg-3"}, ids,
		"the delivery that just landed must survive; the oldest OTHER message is evicted")

	marker, err := store.Exists(ctx, dedupKeyPrefix+testTenantID+":agent-b:msg-1")
	require.NoError(t, err)
	require.True(t, marker, "a surviving delivery must keep its marker")
}

// =============================================================================
// A store that cannot decide a delivery refuses to make one
// =============================================================================

// TestPeerMessaging_UnguardedStore_RefusesToSend covers the fail-closed
// construction. memKVStore offers the six plain verbs and neither guarded one,
// so it cannot arbitrate a claim — and a store that cannot arbitrate must
// refuse rather than fall back to the probe-then-write this design replaced.
func TestPeerMessaging_UnguardedStore_RefusesToSend(t *testing.T) {
	kv := newMemKVStore()
	svc := NewPeerMessagingService(kv, defaultPolicy(), &peerTestLogger{})

	err := svc.Send(context.Background(), testMessage("msg-1", "agent-a", "agent-b", "advisory", 1000))

	var unguarded *PeerInboxUnguardedError
	require.ErrorAs(t, err, &unguarded, "an unguarded store must refuse, not deliver on a hope")
	require.ErrorIs(t, err, ErrPeerInboxUnguarded)

	keys, listErr := kv.List(context.Background(), inboxKeyPrefix)
	require.NoError(t, listErr)
	require.Empty(t, keys, "a refused send must write nothing")
}

// =============================================================================
// Store-failure branches on the claim path
// =============================================================================

// TestPeerMessaging_ClaimWriteFailure_IsReported covers a store that fails the
// claim itself: nothing was decided, so nothing may be delivered.
func TestPeerMessaging_ClaimWriteFailure_IsReported(t *testing.T) {
	kv := newFaultKVStore()
	kv.putIfAbsentErr = errors.New("claim write failed")
	kv.putIfAbsentErrPrefix = dedupClaimKeyPrefix
	svc := NewPeerMessagingService(kv, defaultPolicy(), &peerTestLogger{})
	ctx := context.Background()

	err := svc.Send(ctx, testMessage("msg-claim", "agent-a", "agent-b", "advisory", 1000))

	var inboxErr *PeerInboxError
	require.ErrorAs(t, err, &inboxErr)
	require.Equal(t, "dedup_claim", inboxErr.Op)

	keys, listErr := kv.List(ctx, inboxKeyPrefix)
	require.NoError(t, listErr)
	require.Empty(t, keys, "an undecided delivery must not reach the inbox")
}

// TestPeerMessaging_LostClaim_MarkerReadFailure_IsReported covers the loser's
// marker read failing: the attempt cannot tell a duplicate from a stall, and
// reports the store failure rather than guessing between them.
func TestPeerMessaging_LostClaim_MarkerReadFailure_IsReported(t *testing.T) {
	kv := newFaultKVStore()
	svc := NewPeerMessagingService(kv, defaultPolicy(), &peerTestLogger{})
	ctx := context.Background()

	// Somebody else already holds the claim.
	took, err := kv.PutIfAbsent(ctx, dedupClaimKeyPrefix+testTenantID+":agent-b:msg-lost", []byte(`{"nonce":"other"}`))
	require.NoError(t, err)
	require.True(t, took)

	kv.existsErr = errors.New("marker read failed")
	sendErr := svc.Send(ctx, testMessage("msg-lost", "agent-a", "agent-b", "advisory", 1000))

	var inboxErr *PeerInboxError
	require.ErrorAs(t, sendErr, &inboxErr)
	require.Equal(t, "dedup_check", inboxErr.Op)
	require.False(t, errors.Is(sendErr, ErrDuplicateMessage))
	require.False(t, errors.Is(sendErr, ErrDeliveryInFlight))
}

// TestPeerMessaging_ForgetDelivery_NothingToForget covers the two states in
// which an eviction has no marker to clear: an inbox key carrying no message
// identifier, and a marker key holding no value to compare against. Neither is
// an error — there is simply nothing a guarded delete could safely target.
func TestPeerMessaging_ForgetDelivery_NothingToForget(t *testing.T) {
	svc, store := newTestPeerService(defaultPolicy())
	kvSvc := svc.(*kvPeerMessagingService)
	ctx := context.Background()

	require.NoError(t, kvSvc.forgetDelivery(ctx, testTenantID, "agent-b", ""),
		"an inbox key with no message ID names no marker")

	// A marker key present but valueless: nothing to compare, so nothing to
	// delete. Written through Put rather than the claim path precisely because
	// no delivery ever writes this shape.
	require.NoError(t, store.Put(ctx, dedupKeyPrefix+testTenantID+":agent-b:msg-empty", nil))
	require.NoError(t, kvSvc.forgetDelivery(ctx, testTenantID, "agent-b", "msg-empty"))

	require.NoError(t, kvSvc.forgetDelivery(ctx, testTenantID, "agent-b", "msg-absent"),
		"a message with no marker at all is already forgotten")
}

// TestPeerMessaging_DropOldest_ListFailure_IsReported covers eviction that
// cannot read the inbox it is meant to trim.
func TestPeerMessaging_DropOldest_ListFailure_IsReported(t *testing.T) {
	svc := NewPeerMessagingService(newFaultKVStore(), defaultPolicy(), &peerTestLogger{})
	kvSvc := svc.(*kvPeerMessagingService)
	kvSvc.store.(*faultKVStore).listErr = errors.New("list failed")

	err := kvSvc.dropOldest(context.Background(), testTenantID, "agent-b", "")

	var inboxErr *PeerInboxError
	require.ErrorAs(t, err, &inboxErr)
	require.Equal(t, "drop_oldest_list", inboxErr.Op)
}

// TestPeerMessaging_DropOldest_OnlyTheNewMessageIsPresent covers the inbox
// holding nothing BUT the delivery just made. The exclusion leaves no
// candidate, and evicting the new message is the one thing eviction must never
// do, so it evicts nothing.
func TestPeerMessaging_DropOldest_OnlyTheNewMessageIsPresent(t *testing.T) {
	svc, store := newTestPeerService(defaultPolicy())
	kvSvc := svc.(*kvPeerMessagingService)
	ctx := context.Background()

	require.NoError(t, svc.Send(ctx, testMessage("msg-only", "agent-a", "agent-b", "advisory", 1000)))
	onlyKey := inboxKeyPrefix + testTenantID + ":agent-b:1000:msg-only"

	require.NoError(t, kvSvc.dropOldest(ctx, testTenantID, "agent-b", onlyKey))

	require.Equal(t, []string{onlyKey}, inboxKeysFor(t, store, testTenantID, "agent-b"),
		"with only the new delivery present, eviction must remove nothing")
}

// TestPeerMessaging_DropOldest_MarkerDeleteFailure_IsReported covers eviction
// failing to clear the evicted marker. The delivery itself already landed, so
// the error names the eviction rather than the send.
func TestPeerMessaging_DropOldest_MarkerDeleteFailure_IsReported(t *testing.T) {
	policy := InboxPolicy{MaxMessages: 1, MaxMessageSize: 10240, OverflowAction: "drop_oldest"}
	kv := newFaultKVStore()
	svc := NewPeerMessagingService(kv, policy, &peerTestLogger{})
	ctx := context.Background()

	require.NoError(t, svc.Send(ctx, testMessage("msg-1", "agent-a", "agent-b", "advisory", 1000)))

	kv.compareAndDeleteErr = errors.New("marker delete failed")
	err := svc.Send(ctx, testMessage("msg-2", "agent-a", "agent-b", "advisory", 2000))

	var inboxErr *PeerInboxError
	require.ErrorAs(t, err, &inboxErr)
	require.Equal(t, "drop_oldest_marker_delete", inboxErr.Op)

	// The delivery that triggered the eviction is in the inbox regardless: it
	// completed before the eviction ran.
	keys, listErr := kv.List(ctx, inboxKeyPrefix+testTenantID+":agent-b:")
	require.NoError(t, listErr)
	require.Equal(t, []string{inboxKeyPrefix + testTenantID + ":agent-b:2000:msg-2"}, keys)
}

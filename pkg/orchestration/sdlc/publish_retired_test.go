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
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The trusted signing history — a rotation does not orphan a pull request
// =============================================================================
//
// Measured on go-luks2 PR #210 and #194 (2026-09-25): every factory pull
// request opened before the signing keys moved into the KMS carried commits
// the retired key signed, and the publisher, holding them to the NEW key,
// refused every push and every train that carried one — for ever, because
// the factory never rewrites a pushed branch.

// retiredSigningKey is the project's pre-rotation key: a real ed25519 key
// from a seed no other fixture uses.
var retiredSigningKey = func() string {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(200 - i)
	}
	key, err := ssh.NewPublicKey(ed25519.NewKeyFromSeed(seed).Public())
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " agent-api-signing"
}()

var retiredSigningFingerprint = func() string {
	fp, err := sshKeyFingerprint(retiredSigningKey)
	if err != nil {
		panic(err)
	}
	return fp
}()

// testRetiredAt is when the retired key stopped being the project's.
var testRetiredAt = time.Date(2026, 9, 24, 22, 15, 0, 0, time.UTC)

// newRetiredHistoryPublisher is a publisher whose project retired
// retiredSigningKey at testRetiredAt.
func newRetiredHistoryPublisher(container *fakeContainer) *publisher {
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})
	pub.project.Git.Signing.Retired = []orchestration.RetiredSigningKey{
		{PublicKey: retiredSigningKey, Name: "agent-api-signing", RetiredAt: testRetiredAt},
	}
	return pub
}

// TestPublisher_ACommitTheRetiredKeySignedBeforeItsRetirementIsPublished: the
// branch carries a commit the project's own identity made with the retired
// key before the rotation, and one the current key made after; both verify.
func TestPublisher_ACommitTheRetiredKeySignedBeforeItsRetirementIsPublished(t *testing.T) {
	container := &fakeContainer{ahead: true, head: "head-worker", outgoing: []outgoingCommit{
		{SHA: "pre-rotation", Status: "G", Signer: retiredSigningFingerprint, Time: testRetiredAt.Add(-72 * time.Hour)},
		{SHA: "post-rotation", Status: "G", Signer: testSigningFingerprint},
	}}
	pub := newRetiredHistoryPublisher(container)

	if _, err := pub.Push(testPublishPlan()); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(container.pushes) != 1 {
		t.Fatalf("pushes = %d, want 1", len(container.pushes))
	}
}

// TestPublisher_TheRetiredKeyIsTrustedOnlyForWhatItWasTrustedFor: the
// history is not a second current key. A commit it signed at or after its
// retirement, one someone else committed, one git could not admit through
// the allowed_signers file's valid-before ("U"), and one with no time are all
// refused, and nothing is pushed.
func TestPublisher_TheRetiredKeyIsTrustedOnlyForWhatItWasTrustedFor(t *testing.T) {
	before := testRetiredAt.Add(-time.Hour)
	for name, commit := range map[string]outgoingCommit{
		"after retirement": {Status: "G", Signer: retiredSigningFingerprint, Time: testRetiredAt.Add(time.Hour)},
		"at retirement":    {Status: "G", Signer: retiredSigningFingerprint, Time: testRetiredAt},
		"someone else's":   {Status: "G", Signer: retiredSigningFingerprint, Time: before, Committer: "mallory@example.test"},
		"git refused it":   {Status: "U", Signer: retiredSigningFingerprint, Time: before},
		"an unknown key":   {Status: "G", Signer: "SHA256:never-this-projects", Time: before},
		"unsigned":         {Status: "N", Time: before},
		"a bad signature":  {Status: "B", Signer: retiredSigningFingerprint, Time: before},
	} {
		t.Run(name, func(t *testing.T) {
			commit.SHA = "suspect"
			container := &fakeContainer{ahead: true, head: "head-worker", outgoing: []outgoingCommit{commit}}
			pub := newRetiredHistoryPublisher(container)

			_, err := pub.Push(testPublishPlan())
			var unsigned *UnsignedCommitError
			if !errors.As(err, &unsigned) || unsigned.Commit != "suspect" {
				t.Fatalf("err = %v, want UnsignedCommitError naming the commit", err)
			}
			if len(container.pushes) != 0 {
				t.Fatalf("pushes = %+v, want none", container.pushes)
			}
		})
	}
}

// TestPublisher_AnInvalidHistoryEntryTrustsNothing: a retired entry that
// does not validate widens nothing — its key is refused as a stranger's.
func TestPublisher_AnInvalidHistoryEntryTrustsNothing(t *testing.T) {
	container := &fakeContainer{ahead: true, head: "head-worker", outgoing: []outgoingCommit{
		{SHA: "pre-rotation", Status: "G", Signer: retiredSigningFingerprint, Time: testRetiredAt.Add(-time.Hour)},
	}}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})
	pub.project.Git.Signing.Retired = []orchestration.RetiredSigningKey{{PublicKey: retiredSigningKey}}

	if _, err := pub.Push(testPublishPlan()); !errors.Is(err, ErrUnsignedCommit) {
		t.Fatalf("err = %v, want ErrUnsignedCommit", err)
	}
}

// TestParseSignedCommit_ReadsEveryFieldAndToleratesAMissingSigner: the log
// line is split on the unit separator, the time is read as Unix seconds, and
// a short or unreadable line leaves fields empty rather than guessing.
func TestParseSignedCommit_ReadsEveryFieldAndToleratesAMissingSigner(t *testing.T) {
	got := parseSignedCommit("abc\x1fG\x1f1790380800\x1fa@b\x1fSHA256:x")
	if got.SHA != "abc" || got.Status != "G" || got.Committer != "a@b" || got.Signer != "SHA256:x" ||
		!got.Time.Equal(time.Unix(1790380800, 0)) {
		t.Fatalf("parsed %+v", got)
	}
	short := parseSignedCommit("abc\x1fN\x1fnot-a-time")
	if short.Status != "N" || !short.Time.IsZero() || short.Signer != "" || short.Committer != "" {
		t.Fatalf("parsed %+v, want the missing fields empty", short)
	}
}

// TestForgeAccess_ADeliveryCarriesTheTrustedHistoryAndACheckoutDoesNot: the
// history is written into the delivery container's allowed_signers, bounded
// by each key's retirement; a reader that signs nothing gets none.
func TestForgeAccess_ADeliveryCarriesTheTrustedHistoryAndACheckoutDoesNot(t *testing.T) {
	project := sshAccessProject()
	project.Git.Signing.Retired = []orchestration.RetiredSigningKey{{PublicKey: retiredSigningKey, RetiredAt: testRetiredAt}}

	rig := newAccessRig(t)
	rig.signing.publicKey = testSigningKey
	req, _, err := rig.open(t, project, SigningScopeDelivery)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	signers := string(req.Workspace.Spec.Files[orchestration.WorkspaceAllowedSignersFile])
	if !strings.Contains(signers, `valid-before="20260924221500Z"`) || len(req.Workspace.Spec.Spec.RetiredSigningKeys) != 1 {
		t.Fatalf("allowed_signers = %q, want the retired key bounded by its retirement", signers)
	}

	reader := newAccessRig(t)
	req, _, err = reader.open(t, project, SigningScopeCheckout)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(req.Workspace.Spec.Spec.RetiredSigningKeys) != 0 {
		t.Fatal("a reader's spec carries signing history it has no use for")
	}
}

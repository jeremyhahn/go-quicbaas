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

package service

import (
	"context"
	stderrors "errors"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// =============================================================================
// One resolution, not two.
//
// FinishRegistration needs the tenant servicer to run the ceremony AND the
// factory that owns the attestation trust store to verify the statement. It
// used to resolve them independently, and the SECOND resolution failing was
// not an error: the code cleared the attestation proof and finished the
// registration anyway. A real hardware key was then recorded as software — or
// an administrator refused — because of a transient failure in the
// verification path, with nothing about the credential having changed.
//
// That is the parseCertPool family: a failure in the verification path
// degrading the evidence instead of stopping. The fix removes the class rather
// than guarding the second lookup — there is one lookup now, so two answers
// cannot disagree.
// =============================================================================

// countingWebAuthnManagerGetter records how many times the FIDO2 service
// resolves the WebAuthn manager, and can fail after the first success — the
// precise shape that used to cost a registration its proof.
type countingWebAuthnManagerGetter struct {
	calls    int
	mgr      WebAuthnManager
	err      error
	failFrom int // 1-based call number from which to start returning errAfterFirst
}

var errAfterFirst = stderrors.New("manager resolution failed on a later call")

func (g *countingWebAuthnManagerGetter) GetWebAuthnManager(_ context.Context) (WebAuthnManager, error) {
	g.calls++
	if g.failFrom > 0 && g.calls >= g.failFrom {
		return nil, errAfterFirst
	}
	return g.mgr, g.err
}

// TestFIDO2Service_VerificationDoesNotResolveTheFactoryItself is the structural
// assertion behind the fix.
//
// verifyAttestationChain no longer looks the factory up; it is handed the one
// the servicer was built from. A counting getter proves it: the verification
// path consults the manager ZERO times, so there is no second resolution whose
// failure could clear the proof. Before the fix this count was one, and a
// failure at that point was silently absorbed.
//
// Asserting the count rather than the outcome is deliberate. The outcome of a
// failed second lookup was "registration succeeds, proof gone" — indistinguishable
// from a credential that legitimately proved nothing, which is exactly why the
// defect was invisible.
func TestFIDO2Service_VerificationDoesNotResolveTheFactoryItself(t *testing.T) {
	getter := &countingWebAuthnManagerGetter{}
	svc := &fido2Service{manager: getter}

	ctx := svc.verifyAttestationChain(context.Background(), nil, "tenant-a",
		`{"id":"x","type":"public-key","rawId":"eA","response":{}}`)

	if ctx == nil {
		t.Fatal("verifyAttestationChain returned a nil context")
	}
	if getter.calls != 0 {
		t.Errorf("the verification path resolved the WebAuthn manager %d times, want 0 — "+
			"a second resolution is the failure that used to clear a registration's "+
			"attestation proof while letting the registration succeed", getter.calls)
	}
}

// TestFIDO2Service_FinishRegistrationResolvesOnce pins the other half: the one
// resolution that does happen belongs to the servicer path, so a resolution
// failure REFUSES the request instead of degrading it.
func TestFIDO2Service_FinishRegistrationResolvesOnce(t *testing.T) {
	getter := &countingWebAuthnManagerGetter{err: stderrors.New("unavailable")}
	svc := NewFIDO2Service(getter)

	_, err := svc.FinishRegistration(context.Background(), "tenant-a",
		&xkmstransport.FinishRegistrationRequest{
			SessionID:       "session-1",
			AttestationJSON: `{"id":"x","type":"public-key","rawId":"eA","response":{}}`,
		})

	if err == nil {
		t.Fatal("a registration proceeded although the WebAuthn factory could not be resolved; " +
			"it must refuse rather than finish without a verified attestation")
	}
	if getter.calls != 1 {
		t.Errorf("FinishRegistration resolved the manager %d times, want exactly 1", getter.calls)
	}
}

// TestFIDO2Service_FinishRegistrationRefusesWhenTheFactoryFailsLate covers the
// reported scenario end to end: the first resolution succeeds and a later one
// fails. There is now no later one, so the request cannot reach the ceremony
// with its proof quietly discarded — it fails on the single resolution it does
// perform.
func TestFIDO2Service_FinishRegistrationRefusesWhenTheFactoryFailsLate(t *testing.T) {
	getter := &countingWebAuthnManagerGetter{
		mgr:      &fakeLegacyWebAuthnManager{},
		failFrom: 2,
	}
	svc := NewFIDO2Service(getter)

	_, err := svc.FinishRegistration(context.Background(), "tenant-a",
		&xkmstransport.FinishRegistrationRequest{
			SessionID:       "session-1",
			AttestationJSON: `{"id":"x","type":"public-key","rawId":"eA","response":{}}`,
		})

	if err == nil {
		t.Fatal("the registration finished despite an unresolvable WebAuthn factory")
	}
	if getter.calls > 1 {
		t.Errorf("the service resolved the manager %d times; a second resolution reintroduces "+
			"the disagreement this fix removed", getter.calls)
	}
}

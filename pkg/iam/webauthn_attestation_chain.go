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

package iam

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"os"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
)

// =============================================================================
// WebAuthn attestation TRUST PATH
//
// WHY A VERIFIED ATTESTATION TYPE IS NOT ENOUGH
//
// go-webauthn sets Credential.AttestationType to "basic_full" after running the
// packed format's verification procedure — but that procedure checks only that
// the statement is internally consistent: the signature verifies under the
// attestation certificate's own public key, the certificate has the right
// shape, and its optional AAGUID extension matches the authenticator data. The
// library says so itself, in the last line of handleBasicAttestation:
//
//	// Step 2.4 If successful, return attestation type Basic and attestation
//	// trust path x5c.
//	// We don't handle trust paths yet but we're done.
//
// and VerifyAttestation returns early — before any metadata validation —
// whenever no metadata.Provider is supplied, which is the case for every
// ceremony this platform runs.
//
// So "basic_full" means "somebody signed this with a certificate", not "a
// manufacturer signed this". An attacker generates their own attestation
// certificate, puts an allowlisted AAGUID in its id-fido-gen-ce-aaguid
// extension AND in the authenticator data, signs the statement with it, and
// obtains a credential that reports basic_full with an allowlisted AAGUID. Both
// halves of the gate are satisfied by material the attacker chose.
//
// Treating that as hardware would reintroduce the exact silent downgrade this
// work exists to remove, one level further down. Hardware therefore requires
// what the library deliberately does not do: verifying the x5c chain against
// attestation roots THE OPERATOR configured.
//
// WHERE THIS RUNS
//
// The chain lives in the attestation object, which the credential store never
// sees — xkms's Credential carries the attestation TYPE and the AAGUID, not the
// statement. So the entry points that hold the raw attestation JSON verify the
// chain and stash the result on the request context, and the assurance gate at
// the credential-store choke point requires that result before it will call
// anything hardware. A path that does not verify cannot produce a hardware
// credential, because the gate's default answer is "not proven".
// =============================================================================

// oidFIDOGenCeAAGUID is id-fido-gen-ce-aaguid (1.3.6.1.4.1.45724.1.1.4), the
// certificate extension a batch attestation certificate carries to name the
// authenticator model it attests for.
var oidFIDOGenCeAAGUID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 45724, 1, 1, 4}

// WebAuthnAttestationRoots holds the FIDO attestation root certificates a
// deployment trusts — the manufacturer roots that sign authenticator batch
// attestation certificates (Yubico's FIDO2 attestation root, Feitian's, and so
// on), as published by the vendor or extracted from the FIDO Metadata Service.
//
// A nil pool proves nothing: VerifyChain on a nil receiver refuses, so a
// deployment that configured no roots cannot mint hardware credentials by
// omission.
type WebAuthnAttestationRoots struct {
	roots *x509.CertPool
	count int
}

// NewWebAuthnAttestationRoots builds the pool from PEM-encoded root
// certificates. A bundle yielding no certificate is refused: a pool that
// silently ended up empty would verify nothing while looking configured.
func NewWebAuthnAttestationRoots(rootsPEM []byte) (*WebAuthnAttestationRoots, error) {
	pool, count, err := parseCertPool(rootsPEM)
	if err != nil {
		return nil, &WebAuthnAttestationRootsError{
			Reason: "the WebAuthn attestation root bundle could not be parsed",
			Cause:  err,
		}
	}
	if count == 0 {
		return nil, &WebAuthnAttestationRootsError{
			Reason: "the WebAuthn attestation root bundle contains no CA certificates",
		}
	}
	return &WebAuthnAttestationRoots{roots: pool, count: count}, nil
}

// NewWebAuthnAttestationRootsFromFile loads the pool from an operator-supplied
// PEM file. An empty path yields (nil, nil): the deployment configured no
// roots, which is not an error at startup — it means hardware enrollment is
// refused at ceremony time with a message naming the setting, rather than the
// whole WebAuthn surface failing to come up.
func NewWebAuthnAttestationRootsFromFile(path string) (*WebAuthnAttestationRoots, error) {
	if path == "" {
		return nil, nil
	}
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, &WebAuthnAttestationRootsError{
			Reason: "the WebAuthn attestation root file " + path + " could not be read",
			Cause:  err,
		}
	}
	roots, err := NewWebAuthnAttestationRoots(pemBytes)
	if err != nil {
		// Same reasoning as the PIV loader: the parse error knows what is wrong
		// with the bundle, only this layer knows which file it came from.
		return nil, &WebAuthnAttestationRootsError{
			Reason: "the WebAuthn attestation root file " + path + " is not a usable trust bundle",
			Cause:  err,
		}
	}
	return roots, nil
}

// Configured reports whether at least one trust anchor is loaded.
func (r *WebAuthnAttestationRoots) Configured() bool {
	return r != nil && r.count > 0
}

// Count returns the number of configured attestation roots.
func (r *WebAuthnAttestationRoots) Count() int {
	if r == nil {
		return 0
	}
	return r.count
}

// WebAuthnAttestationChain is the result of verifying one registration's
// attestation trust path. Its fields are unexported for the same reason
// AttestationResult's are: only this package can produce one that says the
// chain verified.
type WebAuthnAttestationChain struct {
	// verified is true only when the x5c chain built a path to a configured
	// attestation root.
	verified bool

	// aaguid is the hex-encoded AAGUID from the ATTESTED authenticator data,
	// bound to the certificate's own AAGUID extension when it carries one.
	aaguid string
}

// Verified reports whether the attestation chained to a configured root.
func (c *WebAuthnAttestationChain) Verified() bool {
	return c != nil && c.verified
}

// AAGUID returns the hex-encoded AAGUID the verified chain vouches for.
func (c *WebAuthnAttestationChain) AAGUID() string {
	if c == nil {
		return ""
	}
	return c.aaguid
}

// VerifyChain checks that the credential's attestation statement chains to a
// configured attestation root, and that the certificate's AAGUID extension —
// when present — matches the authenticator data.
//
// It returns a typed refusal rather than a bare false so the caller can report
// WHY a security key was rejected. A statement with no x5c (self attestation,
// or no attestation at all) is refused here: those are software-grade evidence
// and are classified as such by the assurance gate, which never reaches this
// call for them.
func (r *WebAuthnAttestationRoots) VerifyChain(parsed *protocol.ParsedCredentialCreationData) (*WebAuthnAttestationChain, error) {
	if !r.Configured() {
		return nil, &WebAuthnAttestationChainError{
			Reason: "no WebAuthn attestation roots are configured, so this authenticator's manufacturer cannot be verified; " +
				"set iam.webauthn.attestation_roots_file to the vendor attestation roots this deployment trusts " +
				"(an unconfigured trust store denies rather than trusting any attestation certificate)",
		}
	}
	if parsed == nil {
		return nil, &WebAuthnAttestationChainError{Reason: "no attestation was presented"}
	}

	chainCerts, err := attestationX5C(parsed.Response.AttestationObject.AttStatement)
	if err != nil {
		return nil, err
	}
	if len(chainCerts) == 0 {
		return nil, &WebAuthnAttestationChainError{
			Reason: "the attestation statement carries no certificate chain, so it vouches only for itself; " +
				"a self-signed or absent attestation is not evidence that the key lives in hardware",
		}
	}

	leaf := chainCerts[0]
	intermediates := x509.NewCertPool()
	for _, cert := range chainCerts[1:] {
		intermediates.AddCert(cert)
	}

	// KeyUsages is ExtKeyUsageAny deliberately: a FIDO batch attestation
	// certificate is an end-entity certificate whose EKUs are unconstrained by
	// the WebAuthn spec, so pinning one would refuse valid authenticators. The
	// property being verified is the path to the operator's anchor.
	if _, verifyErr := leaf.Verify(x509.VerifyOptions{
		Roots:         r.roots,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); verifyErr != nil {
		return nil, &WebAuthnAttestationChainError{
			Reason: "the attestation certificate " + leaf.Subject.String() +
				" (issued by " + leaf.Issuer.String() + ") does not chain to a trusted WebAuthn attestation root",
			Cause: verifyErr,
		}
	}

	authDataAAGUID := parsed.Response.AttestationObject.AuthData.AttData.AAGUID

	// Bind the model to the certificate. go-webauthn already performs this
	// comparison for the packed format, but not every format's handler does,
	// and repeating it here is what lets the gate trust the AAGUID it
	// allowlists: without the binding, a genuine manufacturer certificate could
	// be replayed alongside authenticator data naming a different model.
	if certAAGUID, ok := certificateAAGUID(leaf); ok {
		if !bytes.Equal(certAAGUID, authDataAAGUID) {
			return nil, &WebAuthnAttestationChainError{
				Reason: "the attestation certificate names authenticator model " +
					hex.EncodeToString(certAAGUID) +
					" but the authenticator reported " + hex.EncodeToString(authDataAAGUID),
			}
		}
	}

	return &WebAuthnAttestationChain{
		verified: true,
		aaguid:   hex.EncodeToString(authDataAAGUID),
	}, nil
}

// attestationX5C extracts and parses the x5c certificate chain from an
// attestation statement. A missing x5c is not an error — it is the
// self-attestation shape — so it yields an empty slice; a malformed one is.
func attestationX5C(attStatement map[string]interface{}) ([]*x509.Certificate, error) {
	raw, ok := attStatement["x5c"]
	if !ok {
		return nil, nil
	}
	entries, ok := raw.([]interface{})
	if !ok {
		return nil, &WebAuthnAttestationChainError{Reason: "the attestation statement's x5c is not a certificate array"}
	}
	certs := make([]*x509.Certificate, 0, len(entries))
	for _, entry := range entries {
		der, ok := entry.([]byte)
		if !ok {
			return nil, &WebAuthnAttestationChainError{Reason: "an x5c entry is not a DER certificate"}
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, &WebAuthnAttestationChainError{
				Reason: "an x5c certificate could not be parsed",
				Cause:  err,
			}
		}
		certs = append(certs, cert)
	}
	return certs, nil
}

// certificateAAGUID reads the id-fido-gen-ce-aaguid extension. The value is
// DER-wrapped: an X.509 extension encodes its payload in an OCTET STRING, and
// the AAGUID is itself an OCTET STRING, so it arrives doubly wrapped.
func certificateAAGUID(cert *x509.Certificate) ([]byte, bool) {
	for _, ext := range cert.Extensions {
		if !ext.Id.Equal(oidFIDOGenCeAAGUID) {
			continue
		}
		var aaguid []byte
		if _, err := asn1.Unmarshal(ext.Value, &aaguid); err != nil {
			return nil, false
		}
		return aaguid, true
	}
	return nil, false
}

// =============================================================================
// Request-scoped hand-off
//
// The chain is verified where the attestation object exists (the ceremony entry
// points) and consumed where the credential is persisted (the credential-store
// write-through). The context is the correct channel for that: it is exactly a
// request-scoped value, it already flows unbroken from the handler through
// xkms's Service.FinishRegistration to CredentialStore.Save, and it cannot be
// forged by a caller.
// =============================================================================

// attestationChainContextKey is the unexported context key type, so no other
// package can write this value into a context.
type attestationChainContextKey struct{}

// contextWithAttestationChain records a verified attestation trust path on the
// context for the assurance gate to consume.
//
// It is UNEXPORTED, and that is a security property rather than tidiness. An
// exported setter would let any caller — an embedded, in-process consumer of
// the SDK above all — stamp a chain of its own choosing onto the context and
// have the gate treat it as this server's verification. The only way to place
// this value is to run WebAuthnServiceFactory.VerifyAttestationChain, which
// checks the statement against the roots THIS deployment configured.
//
// Passing nil CLEARS any chain already on the context. That is what a failed
// verification does: a stale or preloaded proof must never survive a
// verification that did not succeed.
func contextWithAttestationChain(ctx context.Context, chain *WebAuthnAttestationChain) context.Context {
	return context.WithValue(ctx, attestationChainContextKey{}, chain)
}

// attestationChainFromContext returns the verified trust path recorded for this
// ceremony, or nil when none was. Nil means "the chain was never verified",
// which the gate treats as not-hardware — never as an implicit pass.
func attestationChainFromContext(ctx context.Context) *WebAuthnAttestationChain {
	if ctx == nil {
		return nil
	}
	chain, _ := ctx.Value(attestationChainContextKey{}).(*WebAuthnAttestationChain)
	return chain
}

// ClearAttestationChain removes any attestation proof from the context.
//
// It is the counterpart the ceremony entry points outside this package need:
// they must be able to guarantee no proof survives a path that did not verify,
// on every early exit (an absent attestation, an unparseable one, an
// unavailable trust store). Exporting a CLEAR is safe in a way exporting a
// setter is not — it can only ever remove evidence, never manufacture it.
func ClearAttestationChain(ctx context.Context) context.Context {
	return clearAttestationChain(ctx)
}

// clearAttestationChain removes any attestation proof from the context.
//
// Every entry point calls it when verification fails. Returning the incoming
// context unchanged would be wrong in exactly the case that matters: a caller
// that arranged for a chain to be present — a stale one from an earlier
// ceremony on a reused context, or a deliberately planted one — would have its
// value survive a verification that just refused, and the gate would read it as
// this server's own proof.
func clearAttestationChain(ctx context.Context) context.Context {
	if attestationChainFromContext(ctx) == nil {
		return ctx
	}
	return contextWithAttestationChain(ctx, nil)
}

// =============================================================================
// Typed errors
// =============================================================================

// WebAuthnAttestationRootsError reports that the attestation trust material
// could not be loaded.
type WebAuthnAttestationRootsError struct {
	// Reason describes the configuration problem in operator terms.
	Reason string

	// Cause is the underlying read or parse failure, when there was one.
	Cause error
}

// Error implements error.
func (e *WebAuthnAttestationRootsError) Error() string {
	if e == nil {
		return "iam.webauthn.attestation: invalid attestation root configuration"
	}
	msg := "iam.webauthn.attestation: " + e.Reason
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is / errors.As.
func (e *WebAuthnAttestationRootsError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// WebAuthnAttestationChainError reports that an attestation statement could not
// be tied to a trusted manufacturer.
type WebAuthnAttestationChainError struct {
	// Reason is the operator-facing explanation.
	Reason string

	// Cause is the underlying path-building or parse failure, when there was
	// one.
	Cause error
}

// Error implements error.
func (e *WebAuthnAttestationChainError) Error() string {
	if e == nil || e.Reason == "" {
		return "iam.webauthn.attestation: the attestation statement is not proven to come from a trusted manufacturer"
	}
	msg := "iam.webauthn.attestation: " + e.Reason
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is / errors.As.
func (e *WebAuthnAttestationChainError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// AttestationChainUnverifiedError reports that a registration presented a
// manufacturer-grade attestation TYPE but no verified trust path.
//
// It is a distinct error from AttestationRequiredError on purpose: that one
// means "the authenticator proved less than hardware", this one means "the
// authenticator claimed hardware and nothing confirmed the claim". The second
// is the dangerous case, because the claim looks like evidence.
type AttestationChainUnverifiedError struct {
	// AAGUID is the hex-encoded model the credential claimed.
	AAGUID string
}

// Error implements error.
func (e *AttestationChainUnverifiedError) Error() string {
	msg := "iam.webauthn.attestation: the authenticator presented a manufacturer attestation statement, " +
		"but its certificate chain was not verified against a trusted attestation root"
	if e != nil && e.AAGUID != "" {
		msg += " (claimed model " + e.AAGUID + ")"
	}
	return msg + "; an unverified attestation certificate can be generated by anyone, " +
		"so it is not evidence that the key lives in hardware. " +
		"Set iam.webauthn.attestation_roots_file to the vendor attestation roots this deployment trusts."
}

// AttestationChainMismatchError reports that the verified trust path vouches
// for a different authenticator model than the credential reports.
type AttestationChainMismatchError struct {
	// VerifiedAAGUID is the model the verified chain attested for.
	VerifiedAAGUID string

	// CredentialAAGUID is the model the stored credential reports.
	CredentialAAGUID string
}

// Error implements error.
func (e *AttestationChainMismatchError) Error() string {
	if e == nil {
		return "iam.webauthn.attestation: the verified attestation does not match the credential"
	}
	return "iam.webauthn.attestation: the verified attestation vouches for authenticator model " +
		e.VerifiedAAGUID + " but the credential reports " + e.CredentialAAGUID +
		"; the attestation does not belong to this credential"
}

// =============================================================================
// Carrying the PROVEN result back out
//
// The gate runs deep inside the credential store, but the caller that mints the
// session token needs to know what it proved — self-service registration mints
// a token for the credential it just enrolled, and dropping the result there
// would leave a freshly registered administrator holding an unattested session
// until they logged in again.
//
// The result travels back the same way the chain travels in: on the context.
// A recorder is placed by the ceremony entry point before the call and filled
// by the gate after it succeeds. It is a POINTER to a mutable cell rather than
// a value, because the gate runs below the entry point in the call stack and
// context values cannot be added on the way back up.
// =============================================================================

// attestationRecorderContextKey is the unexported key for the recorder cell.
type attestationRecorderContextKey struct{}

// attestationRecorder is the write-once cell the gate fills.
//
// Access is mutex-guarded because the ceremony that fills it and the caller
// that reads it are only ordered by the call returning; a store elsewhere in
// the stack must still be visible to the reader without a data race.
type attestationRecorder struct {
	mu     sync.Mutex
	result *AttestationResult
}

// set records the proven attestation.
func (r *attestationRecorder) set(result *AttestationResult) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.result = result
	r.mu.Unlock()
}

// get returns the recorded attestation, or nil when the gate never ran.
func (r *attestationRecorder) get() *AttestationResult {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.result
}

// contextWithAttestationRecorder attaches a fresh recorder and returns it
// alongside the derived context.
func contextWithAttestationRecorder(ctx context.Context) (context.Context, *attestationRecorder) {
	recorder := &attestationRecorder{}
	return context.WithValue(ctx, attestationRecorderContextKey{}, recorder), recorder
}

// recordAttestation stores the gate's result on the recorder, when one was
// attached. A ceremony driven by a path that attached none simply records
// nothing — the result is an optimisation for the caller, never the evidence
// itself, which is why nothing depends on its presence.
func recordAttestation(ctx context.Context, result *AttestationResult) {
	if ctx == nil {
		return
	}
	recorder, _ := ctx.Value(attestationRecorderContextKey{}).(*attestationRecorder)
	recorder.set(result)
}

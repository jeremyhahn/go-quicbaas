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
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"os"
	"time"
)

// =============================================================================
// PIV slot attestation
//
// WHAT A PIV ATTESTATION ACTUALLY PROVES
//
// A PIV smartcard can prove that the private key in one of its slots was
// generated ON the card and cannot be exported. It does so by signing a
// per-slot attestation certificate with a factory-provisioned attestation key
// (slot F9 on a YubiKey), whose own certificate chains to the vendor's PIV
// attestation root. The chain is therefore:
//
//	slot attestation certificate  ->  device F9 intermediate  ->  vendor root
//
// Only the last link is a trust anchor an operator configures. Everything else
// travels with the card.
//
// WHAT WAS HAPPENING INSTEAD
//
// The PIV login handler accepted any TLS client certificate whose thumbprint
// matched a binding and issued a token claiming acr=hardware-attested,
// attest_level=hardware. Nothing verified a chain: the optional TrustedIssuers
// list compared Subject DN STRINGS, which any self-signed certificate can
// simply assert, and it was documented as "empty list = permissive". A
// self-signed software key with a copied issuer DN produced a token
// indistinguishable from one backed by a YubiKey.
//
// A self-signed slot certificate is not evidence of hardware. It is a signature
// by the very key whose storage is in question — a circular claim.
// =============================================================================

// =============================================================================
// The PIV attestation certificate profile
//
// A slot attestation certificate is not merely "another certificate from the
// right CA". It is a distinct artifact the DEVICE produces, and it carries
// extensions describing the card that produced it — firmware version, device
// serial, the PIN and touch policy of the attested slot, the form factor.
// Those extensions are what an ordinary client-authentication certificate
// cannot have any reason to carry, which is what makes them a usable positive
// marker.
//
// The arc below is Yubico's (1.3.6.1.4.1.41482.3), which is the only PIV slot
// attestation profile in meaningful deployment: slot attestation is a YubiKey
// extension to PIV, not something NIST SP 800-73 defines. Recognising a second
// vendor is one entry in this list.
//
// ONLY PRESENCE IS READ, NEVER THE VALUES. Deciding whether a key lives in
// hardware does not require knowing which firmware or which serial; it requires
// knowing that the certificate was issued under the attestation profile and
// chains to a vendor anchor. Parsing the values would be a vendor attestation
// PARSER, which belongs in go-xkms with the rest of the hardware layer, not
// here — see the note on Verify.
// =============================================================================

var (
	// oidYubicoPIVFirmwareVersion is 1.3.6.1.4.1.41482.3.3, the firmware
	// version of the card that produced the attestation.
	oidYubicoPIVFirmwareVersion = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 41482, 3, 3}

	// oidYubicoPIVSerialNumber is 1.3.6.1.4.1.41482.3.7, the device serial.
	oidYubicoPIVSerialNumber = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 41482, 3, 7}

	// oidYubicoPIVPinTouchPolicy is 1.3.6.1.4.1.41482.3.8, the PIN and touch
	// policy of the attested slot.
	oidYubicoPIVPinTouchPolicy = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 41482, 3, 8}

	// oidYubicoPIVFormFactor is 1.3.6.1.4.1.41482.3.9, the device form factor.
	oidYubicoPIVFormFactor = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 41482, 3, 9}
)

// pivAttestationProfileOIDs are the extensions whose presence marks a
// certificate as issued under a PIV slot attestation profile. Any one of them
// is sufficient: a card stamps several, and requiring a specific one would
// refuse a future firmware that drops it.
var pivAttestationProfileOIDs = []asn1.ObjectIdentifier{
	oidYubicoPIVFirmwareVersion,
	oidYubicoPIVSerialNumber,
	oidYubicoPIVPinTouchPolicy,
	oidYubicoPIVFormFactor,
}

// carriesPIVAttestationProfile reports whether a certificate was issued under a
// recognised PIV slot attestation profile.
func carriesPIVAttestationProfile(cert *x509.Certificate) bool {
	if cert == nil {
		return false
	}
	for _, ext := range cert.Extensions {
		for _, oid := range pivAttestationProfileOIDs {
			if ext.Id.Equal(oid) {
				return true
			}
		}
	}
	return false
}

// PIVAttestationVerifier verifies that a PIV slot certificate chains to an
// operator-configured attestation trust anchor.
//
// A nil verifier proves nothing: Verify on a nil receiver returns a typed
// refusal rather than success, so a deployment that never configured an
// attestation CA cannot mint hardware-attested tokens by omission.
type PIVAttestationVerifier struct {
	// roots are the trusted PIV attestation CA certificates (e.g. the Yubico
	// PIV Attestation root). A chain must terminate in one of these.
	roots *x509.CertPool

	// intermediates are additional CA certificates used to complete a chain —
	// typically vendor sub-CAs. The device's own F9 certificate is supplied
	// per-verification instead, since it is card-specific.
	intermediates *x509.CertPool

	// rootCount is the number of anchors loaded, kept for operator-facing
	// logging and for the "configured at all" predicate.
	rootCount int
}

// NewPIVAttestationVerifier builds a verifier from PEM-encoded attestation CA
// certificates. It refuses a bundle that yields no usable CA certificate: an
// anchor set that silently ended up empty would verify nothing while looking
// configured, which is the failure mode this whole file exists to remove.
func NewPIVAttestationVerifier(attestationCAPEM []byte, intermediatePEM []byte) (*PIVAttestationVerifier, error) {
	roots, rootCount, err := parseCertPool(attestationCAPEM)
	if err != nil {
		return nil, err
	}
	if rootCount == 0 {
		return nil, &PIVAttestationConfigError{Reason: "the attestation CA bundle contains no CA certificates"}
	}
	intermediates, _, err := parseCertPool(intermediatePEM)
	if err != nil {
		return nil, err
	}
	return &PIVAttestationVerifier{
		roots:         roots,
		intermediates: intermediates,
		rootCount:     rootCount,
	}, nil
}

// NewPIVAttestationVerifierFromFiles loads the verifier from operator-supplied
// PEM files. intermediatePath may be empty when no vendor sub-CA bundle is
// configured.
func NewPIVAttestationVerifierFromFiles(attestationCAPath, intermediatePath string) (*PIVAttestationVerifier, error) {
	if attestationCAPath == "" {
		return nil, &PIVAttestationConfigError{
			Reason: "no PIV attestation CA is configured, so a presented certificate cannot be shown to come from a smartcard; " +
				"set iam.piv_login.attestation_ca_file to the vendor PIV attestation root " +
				"(an unconfigured verifier denies rather than assuming hardware)",
		}
	}
	caPEM, err := os.ReadFile(attestationCAPath)
	if err != nil {
		return nil, &PIVAttestationConfigError{
			Reason: "the attestation CA file " + attestationCAPath + " could not be read",
			Cause:  err,
		}
	}
	var intermediatePEM []byte
	if intermediatePath != "" {
		intermediatePEM, err = os.ReadFile(intermediatePath)
		if err != nil {
			return nil, &PIVAttestationConfigError{
				Reason: "the attestation intermediate file " + intermediatePath + " could not be read",
				Cause:  err,
			}
		}
	}
	verifier, err := NewPIVAttestationVerifier(caPEM, intermediatePEM)
	if err != nil {
		// The bundle was read but is not usable. Naming the file here is the
		// difference between an operator fixing a path in one minute and
		// hunting a verification that fails for a reason reported nowhere.
		return nil, &PIVAttestationConfigError{
			Reason: "the attestation CA file " + attestationCAPath + " is not a usable trust bundle",
			Cause:  err,
		}
	}
	return verifier, nil
}

// Configured reports whether the verifier holds at least one trust anchor.
func (v *PIVAttestationVerifier) Configured() bool {
	return v != nil && v.rootCount > 0
}

// RootCount returns the number of configured attestation trust anchors.
func (v *PIVAttestationVerifier) RootCount() int {
	if v == nil {
		return 0
	}
	return v.rootCount
}

// Verify establishes that the key authenticating this login lives on a
// smartcard, by finding a SEPARATE attestation certificate that vouches for
// that exact key, was issued under a PIV slot attestation profile, and chains
// to a configured attestation anchor.
//
// loginCert is the credential being authenticated — the TLS client certificate.
// presented is everything else the client supplied alongside it: on a YubiKey,
// the slot attestation certificate and the device's F9 intermediate.
//
// # WHY THE LOGIN CERTIFICATE IS NEVER ITS OWN EVIDENCE
//
// The anchor is operator configuration, and an operator can point it at a CA
// that also issues ordinary client-authentication certificates. Nothing about
// a certificate's PATH distinguishes a key generated on a card from one
// generated with openssl and certified by the same CA. So "the login
// certificate chains to the attestation anchor" is not evidence of hardware —
// it is evidence that the deployment reused a CA, and treating it as sufficient
// re-opens exactly the fail-open this file exists to close, reachable through
// configuration alone with no attacker involved.
//
// Nor is "some second certificate" enough. Under that same reused CA a holder
// submits their CSR twice and gets two certificates for one key; the second is
// distinct, chains, and binds the right key while saying nothing about where
// the key lives. A negative rule ("not the login certificate") is defeated by
// producing another one.
//
// # THE THREE THINGS THAT MUST HOLD
//
// The evidence is a certificate the DEVICE produced, so all three are required
// of the same certificate:
//
//  1. It is presented ALONGSIDE the login certificate, not the login
//     certificate itself.
//  2. Its SubjectPublicKeyInfo is byte-identical to the login certificate's, so
//     it speaks about the key that is actually authenticating. Without this a
//     genuine attestation borrowed from another card would launder a software
//     key.
//  3. It carries a PIV slot attestation profile marker (see the OID block
//     above), which an ordinary client certificate has no reason to carry.
//
// and then it must chain to an operator-configured anchor. Presented
// certificates are chain material, never trust anchors: material supplied by
// the party under verification can only help complete a path to an anchor the
// operator chose.
//
// A deployment whose cards cannot produce a slot attestation is not silently
// downgraded here; it is refused, and the operator chooses honestly through
// iam.piv_login.allow_unattested, which yields attest_level=software.
//
// # WHERE THIS BELONGS
//
// Only the PRESENCE of the profile marker is read. Deciding storage does not
// require decoding the firmware version or the serial, and decoding them would
// make this a vendor attestation parser — which belongs in go-xkms with the
// rest of the hardware layer. As of go-xkms v0.8.0-alpha no such parser exists
// (pkg/pivcert models slots and verifies generic chains; pkg/xkms.AttestPIVSlot
// PRODUCES attestation material and nothing verifies it), so the minimum policy
// check lives here.
func (v *PIVAttestationVerifier) Verify(loginCert *x509.Certificate, presented []*x509.Certificate) (*AttestationResult, error) {
	if !v.Configured() {
		return nil, &PIVAttestationUnverifiedError{
			Reason: "no PIV attestation CA is configured, so this certificate cannot be shown to come from a smartcard; " +
				"set iam.piv_login.attestation_ca_file to the vendor PIV attestation root " +
				"(an unconfigured verifier denies rather than assuming hardware)",
		}
	}
	if loginCert == nil {
		return nil, &PIVAttestationUnverifiedError{Reason: "no certificate was presented"}
	}

	// A self-signed login certificate with nothing alongside it is refused
	// explicitly rather than being left to fail as an anonymous "unknown
	// authority". The operator-facing reason is the point: it names what is
	// wrong with the credential they inserted.
	if len(presented) == 0 && isSelfSigned(loginCert) {
		return nil, &PIVAttestationUnverifiedError{
			Reason: "the certificate is self-signed, which proves possession of the key but not that the key lives on a smartcard; " +
				"present a slot attestation certificate that chains to a trusted PIV attestation CA",
		}
	}

	intermediates := v.chainMaterial(presented)

	// Candidates are what the client presented ALONGSIDE the login certificate.
	// The login certificate is excluded on purpose and is not a special case:
	// it is the credential under examination, and a credential cannot be its
	// own evidence.
	candidates := make([]*x509.Certificate, 0, len(presented))
	for _, cert := range presented {
		if cert != nil && !cert.Equal(loginCert) {
			candidates = append(candidates, cert)
		}
	}
	if len(candidates) == 0 {
		return nil, &PIVAttestationUnverifiedError{
			Reason: "no attestation certificate was presented alongside the login certificate, so nothing vouches for where its key lives; " +
				"present the slot attestation certificate for this key (for example `ykman piv keys attest <slot>`)",
		}
	}

	var (
		lastErr     error
		foreignKeys []*x509.Certificate
		unprofiled  []*x509.Certificate
	)
	for _, candidate := range candidates {
		// The binding: this certificate must speak about the key that is
		// actually authenticating.
		if !bytes.Equal(candidate.RawSubjectPublicKeyInfo, loginCert.RawSubjectPublicKeyInfo) {
			foreignKeys = append(foreignKeys, candidate)
			continue
		}
		// The profile: an ordinary certificate about the right key is still an
		// ordinary certificate. Only a device issues under this profile.
		if !carriesPIVAttestationProfile(candidate) {
			unprofiled = append(unprofiled, candidate)
			continue
		}
		if err := v.chainsToAnchor(candidate, intermediates); err != nil {
			lastErr = err
			continue
		}
		return &AttestationResult{
			level:  AttestLevelHardware,
			format: "piv-attestation-chain",
			method: AttestMethodPIV,
		}, nil
	}

	// Two refusals deserve their own message, because both are certificates
	// that LOOK right until you check the one thing they get wrong, and the
	// generic "does not chain" would send the operator to their trust store
	// instead of to the certificate they actually got wrong.
	//
	// First: a certificate that chains but attests a different key — the
	// borrowed-attestation attack.
	for _, cert := range foreignKeys {
		if v.chainsToAnchor(cert, intermediates) == nil {
			return nil, &PIVAttestationUnverifiedError{
				Reason: "the presented attestation certificate " + cert.Subject.String() +
					" chains to a trusted PIV attestation CA but attests a DIFFERENT key than the one authenticating; " +
					"present the slot attestation certificate for THIS key (for example `ykman piv keys attest <slot>`)",
			}
		}
	}

	// Second: a certificate about the right key, chaining to the anchor, that
	// is simply not an attestation. This is what an operator sees when they
	// have pointed attestation_ca_file at their own login CA, and naming it is
	// what stops them concluding the trust store is wrong.
	for _, cert := range unprofiled {
		if v.chainsToAnchor(cert, intermediates) == nil {
			return nil, &PIVAttestationUnverifiedError{
				Reason: "the presented certificate " + cert.Subject.String() +
					" is about the authenticating key and chains to a trusted CA, but it is an ordinary certificate rather than a slot attestation: " +
					"it carries no PIV attestation profile extension, so it says nothing about where the key lives; " +
					"present the certificate produced by `ykman piv keys attest <slot>`, and point " +
					"iam.piv_login.attestation_ca_file at the VENDOR attestation root rather than at the CA that issues login certificates",
			}
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, &PIVAttestationUnverifiedError{
		Reason: "no presented certificate both attests the authenticating key and chains to a trusted PIV attestation CA",
	}
}

// chainMaterial combines the operator-configured intermediates with the
// certificates the client presented. The client's certificates can only help
// build a path to an operator-chosen root; they are never added to the roots.
func (v *PIVAttestationVerifier) chainMaterial(presented []*x509.Certificate) *x509.CertPool {
	if len(presented) == 0 {
		return v.intermediates
	}
	pool := x509.NewCertPool()
	if v.intermediates != nil {
		pool = v.intermediates.Clone()
	}
	for _, cert := range presented {
		if cert != nil {
			pool.AddCert(cert)
		}
	}
	return pool
}

// chainsToAnchor verifies one candidate certificate against the configured
// attestation roots.
//
// KeyUsages is deliberately ANY. A PIV slot attestation certificate is an
// end-entity certificate whose extended key usages describe what the SLOT key
// is for (client auth, signing, key management), not what the attestation is
// for; pinning ExtKeyUsageClientAuth would refuse valid attestations for the
// signature and key-management slots. The property being verified is the path
// to the vendor anchor.
func (v *PIVAttestationVerifier) chainsToAnchor(cert *x509.Certificate, intermediates *x509.CertPool) error {
	chains, err := cert.Verify(x509.VerifyOptions{
		Roots:         v.roots,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return &PIVAttestationChainError{
			Subject: cert.Subject.String(),
			Issuer:  cert.Issuer.String(),
			Cause:   err,
		}
	}
	if len(chains) == 0 {
		return &PIVAttestationChainError{
			Subject: cert.Subject.String(),
			Issuer:  cert.Issuer.String(),
		}
	}
	return nil
}

// UnattestedPIVCredential returns the attestation for a PIV credential whose
// hardware provenance was NOT established — the result an operator gets after
// explicitly opting into iam.piv_login.allow_unattested.
//
// It exists so an unattested login is REPRESENTABLE rather than unrecorded. A
// handler that simply skipped verification would issue a token with no
// attest_level at all, which reads the same as a token from a path that never
// considered the question. This says "software", so downstream policy and audit
// can both see the deployment chose to accept an unproven credential.
func UnattestedPIVCredential() *AttestationResult {
	return &AttestationResult{
		level:  AttestLevelSoftware,
		format: "unverified",
		method: AttestMethodPIV,
	}
}

// isSelfSigned reports whether the certificate is its own issuer AND its own
// signature verifies under its own public key. The name comparison alone is not
// enough — a certificate can carry a matching issuer DN it did not sign — so
// the signature check is what makes the answer meaningful.
//
// CheckSignature is used rather than CheckSignatureFrom because the latter
// additionally requires the issuer to be a CA, and the certificate this
// function exists to recognise is precisely a self-signed END-ENTITY
// certificate: a software key claiming to be a PIV slot. CheckSignatureFrom
// would reject it for the wrong reason and report it as an anonymous
// unknown-authority failure instead of naming the actual problem.
func isSelfSigned(cert *x509.Certificate) bool {
	if cert == nil {
		return false
	}
	if cert.Subject.String() != cert.Issuer.String() {
		return false
	}
	return cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) == nil
}

// parseCertPool decodes PEM CA certificates into a pool, returning the pool and
// the number of certificates it holds. Malformed PEM is an error rather than an
// empty pool: quietly trusting nothing is the failure this file removes.
func parseCertPool(pemBytes []byte) (*x509.CertPool, int, error) {
	pool := x509.NewCertPool()
	count := 0
	rest := pemBytes
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, 0, &PIVAttestationConfigError{
				Reason: "a PEM CERTIFICATE block in the attestation bundle could not be parsed",
				Cause:  err,
			}
		}
		// A trust anchor must be a CA. Adding whatever the bundle contained is
		// how an attestation certificate becomes its own root: point this at a
		// card's slot-attestation certificate instead of the vendor CA and the
		// verification below builds a one-element path from that certificate to
		// ITSELF, reporting hardware with no manufacturer CA anywhere in the
		// deployment. The whole point of an anchor set is that the operator,
		// not the party under verification, chose what is trusted.
		//
		// BasicConstraintsValid is checked alongside IsCA because IsCA is the
		// zero value false on a certificate that never carried the extension —
		// testing IsCA alone would pass a cert whose CA-ness was never
		// asserted at all, which is exactly the end-entity case.
		if !cert.BasicConstraintsValid || !cert.IsCA {
			return nil, 0, &AttestationAnchorNotCAError{Subject: cert.Subject.String()}
		}
		pool.AddCert(cert)
		count++
	}
	return pool, count, nil
}

// =============================================================================
// Typed errors
// =============================================================================

// AttestationAnchorNotCAError reports that a certificate offered as an
// attestation trust anchor is not a CA certificate.
//
// It is a configuration error rather than a verification failure: the bundle an
// operator named cannot serve as an anchor set, and refusing at load time with
// the offending subject is what makes a mistyped path diagnosable. Accepting it
// would silently turn an end-entity certificate into a root.
type AttestationAnchorNotCAError struct {
	// Subject is the distinguished name of the offending certificate.
	Subject string
}

// Error implements error.
func (e *AttestationAnchorNotCAError) Error() string {
	if e == nil {
		return "iam.attestation: a trust anchor is not a CA certificate"
	}
	return "iam.attestation: the certificate " + e.Subject + " is not a CA certificate and cannot be an " +
		"attestation trust anchor; a bundle of anchors holds the manufacturer's CA certificates, not a " +
		"device's own attestation certificate (check that the configured file is the vendor CA bundle)"
}

// PIVAttestationConfigError reports that the PIV attestation trust material
// could not be loaded.
type PIVAttestationConfigError struct {
	// Reason describes the configuration problem in operator terms.
	Reason string

	// Cause is the underlying read or parse failure, when there was one.
	Cause error
}

// Error implements error.
func (e *PIVAttestationConfigError) Error() string {
	if e == nil {
		return "iam.piv.attestation: invalid configuration"
	}
	msg := "iam.piv.attestation: " + e.Reason
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is / errors.As.
func (e *PIVAttestationConfigError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// PIVAttestationUnverifiedError reports that hardware could not be established
// for a presented certificate — because nothing was configured to check it
// against, or because the certificate vouches only for itself.
type PIVAttestationUnverifiedError struct {
	// Reason is the operator-facing explanation.
	Reason string
}

// Error implements error.
func (e *PIVAttestationUnverifiedError) Error() string {
	if e == nil || e.Reason == "" {
		return "iam.piv.attestation: the certificate is not proven to be hardware-backed"
	}
	return "iam.piv.attestation: " + e.Reason
}

// PIVAttestationChainError reports that a presented slot certificate does not
// chain to any configured PIV attestation CA.
type PIVAttestationChainError struct {
	// Subject is the presented certificate's subject DN.
	Subject string

	// Issuer is the presented certificate's issuer DN.
	Issuer string

	// Cause is the underlying x509 path-building failure, when there was one.
	Cause error
}

// Error implements error.
func (e *PIVAttestationChainError) Error() string {
	if e == nil {
		return "iam.piv.attestation: certificate does not chain to a trusted PIV attestation CA"
	}
	msg := "iam.piv.attestation: certificate " + e.Subject +
		" (issued by " + e.Issuer + ") does not chain to a trusted PIV attestation CA"
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is / errors.As.
func (e *PIVAttestationChainError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

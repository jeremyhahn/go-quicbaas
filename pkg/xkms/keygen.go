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

// keygen.go is the ONLY file in go-quicbaas permitted to create an asymmetric
// key pair. `make check-local-keygen` fails the build on any other site, so the
// rule below cannot quietly erode.
//
// # Why a key is ever created here at all
//
// go-xkms owns key custody, and QuicBaaS composes it: when QuicBaaS needs a key
// that the PLATFORM will hold, it asks go-xkms, which generates inside the
// keystore (TPM 2.0, PKCS#11, or PKCS#8) and hands back an opaque
// crypto.Signer. Nothing here is involved in that, and nothing here should be.
//
// This file exists for the other case: a credential whose private key belongs
// to the REQUESTER — the platform administrator's personal certificate, a
// deployed fleet node daemon's mTLS identity, a `ca issue` caller's key pair. go-xkms
// deliberately cannot produce one. Its keystore backends wrap every generated
// key in a non-exportable opaque handle (go-xkms pkg/xkms compositeBackend), so
// TrustedCA.IssueCertificate returns IssuedCertificate.PrivateKeyPEM as nil on
// every backend and every path — verified against a real software CA in
// pkg/testutil/xkmsca's TestIssueCertificateNeverExportsThePrivateKey.
//
// That is not a gap to route around; it is the custody rule. go-xkms states it
// in its own implementation of the same flow (pkg/init/firstboot_adapter.go
// GenerateAdminCert): "The key is generated HERE, outside the CA's keystore,
// deliberately: it is the administrator's personal credential, not platform key
// material, so it must never enter the platform's key custody — the CA only
// ever sees the CSR."
//
// So the rule QuicBaaS follows is go-xkms's rule, with one addition: there is
// exactly ONE generator, it is gated in CI, and every certificate built on its
// output is signed by go-xkms and by nothing else.
//
// # About the entropy source
//
// The generator passes the go-xkms FIPS RNG resolver (Reader, which prefers
// TPM 2.0 then PKCS#11 then software) rather than crypto/rand. Be precise about
// what that buys, because it is less than it looks:
//
//	"Since Go 1.26, a secure source of random bytes is always used, and the
//	Reader is ignored unless GODEBUG=cryptocustomrand=1 is set. This setting
//	will be removed in a future Go release."
//	                       -- crypto/ecdsa.GenerateKey, crypto/rsa.GenerateKey
//
// So on the ECDSA and RSA paths the reader is DISCARDED by the toolchain and
// the key comes from Go's FIPS 140-3 approved DRBG. Only ed25519.GenerateKey
// still consumes what it is handed, and that is asserted by
// TestGenerateRequesterKey_XKMSResolverIsOnTheEd25519Path so the day it stops
// being true is a test failure and not a quiet one. The resolver is passed
// anyway — uniformly, on every path — because the alternative is per-algorithm
// special-casing of a decision that belongs to the toolchain, and because a
// FIPS-approved DRBG is the correct source when the reader is refused.
//
// The value of this file is therefore NOT the entropy: it is that there is one
// audited site instead of six, and a build gate that keeps it that way.

package xkms

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"io"
	"sort"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// Canonical requester-key algorithm names. These are the spellings this package
// generates under; every accepted alias resolves to one of them.
const (
	// KeyAlgorithmECDSAP256 is ECDSA over NIST P-256. It is the default when a
	// caller names no algorithm, matching the go-xkms CA's own default.
	KeyAlgorithmECDSAP256 = "ecdsa-p256"

	// KeyAlgorithmECDSAP384 is ECDSA over NIST P-384.
	KeyAlgorithmECDSAP384 = "ecdsa-p384"

	// KeyAlgorithmRSA2048 is RSA with a 2048-bit modulus.
	KeyAlgorithmRSA2048 = "rsa-2048"

	// KeyAlgorithmRSA3072 is RSA with a 3072-bit modulus.
	KeyAlgorithmRSA3072 = "rsa-3072"

	// KeyAlgorithmRSA4096 is RSA with a 4096-bit modulus.
	KeyAlgorithmRSA4096 = "rsa-4096"

	// KeyAlgorithmEd25519 is Ed25519.
	KeyAlgorithmEd25519 = "ed25519"
)

// ErrUnsupportedKeyAlgorithm is the sentinel every unsupported-algorithm
// failure matches, so a caller can map it onto its own API error without
// depending on the concrete type.
var ErrUnsupportedKeyAlgorithm error = &api.UnsupportedKeyAlgorithmError{}

// requesterKeyGenerator creates one key pair from the supplied entropy source.
// The returned x509.PublicKeyAlgorithm is what an encoder needs to choose the
// private key's PEM block type.
type requesterKeyGenerator func(rand io.Reader) (crypto.Signer, x509.PublicKeyAlgorithm, error)

// requesterKeyGenerators dispatches a canonical algorithm name to its
// generator. A map rather than a switch so the lookup is O(1) and the supported
// set is enumerable (see SupportedKeyAlgorithms).
var requesterKeyGenerators = map[string]requesterKeyGenerator{
	KeyAlgorithmECDSAP256: func(rand io.Reader) (crypto.Signer, x509.PublicKeyAlgorithm, error) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand) // local-keygen-ok: the single audited requester-key generator; see the file comment.
		return key, x509.ECDSA, err
	},
	KeyAlgorithmECDSAP384: func(rand io.Reader) (crypto.Signer, x509.PublicKeyAlgorithm, error) {
		key, err := ecdsa.GenerateKey(elliptic.P384(), rand) // local-keygen-ok: the single audited requester-key generator; see the file comment.
		return key, x509.ECDSA, err
	},
	KeyAlgorithmRSA2048: func(rand io.Reader) (crypto.Signer, x509.PublicKeyAlgorithm, error) {
		key, err := rsa.GenerateKey(rand, 2048) // local-keygen-ok: the single audited requester-key generator; see the file comment.
		return key, x509.RSA, err
	},
	KeyAlgorithmRSA3072: func(rand io.Reader) (crypto.Signer, x509.PublicKeyAlgorithm, error) {
		key, err := rsa.GenerateKey(rand, 3072) // local-keygen-ok: the single audited requester-key generator; see the file comment.
		return key, x509.RSA, err
	},
	KeyAlgorithmRSA4096: func(rand io.Reader) (crypto.Signer, x509.PublicKeyAlgorithm, error) {
		key, err := rsa.GenerateKey(rand, 4096) // local-keygen-ok: the single audited requester-key generator; see the file comment.
		return key, x509.RSA, err
	},
	KeyAlgorithmEd25519: func(rand io.Reader) (crypto.Signer, x509.PublicKeyAlgorithm, error) {
		_, key, err := ed25519.GenerateKey(rand) // local-keygen-ok: the single audited requester-key generator; see the file comment.
		return key, x509.Ed25519, err
	},
}

// keyAlgorithmAliases maps every spelling a caller may supply onto a canonical
// name. It carries the documented go-xkms transport
// IssueCertificateRequest.Algorithm values plus the CLI shorthands ("ecdsa",
// "rsa") that shipped before this file existed, so no caller's input changes
// meaning. The empty string is an alias too: it means "the default".
var keyAlgorithmAliases = map[string]string{
	"":           KeyAlgorithmECDSAP256,
	"ecdsa":      KeyAlgorithmECDSAP256,
	"ecdsa-p256": KeyAlgorithmECDSAP256,
	"ecdsap256":  KeyAlgorithmECDSAP256,
	"p256":       KeyAlgorithmECDSAP256,
	"ecdsa-p384": KeyAlgorithmECDSAP384,
	"ecdsap384":  KeyAlgorithmECDSAP384,
	"p384":       KeyAlgorithmECDSAP384,
	"rsa":        KeyAlgorithmRSA2048,
	"rsa2048":    KeyAlgorithmRSA2048,
	"rsa-2048":   KeyAlgorithmRSA2048,
	"rsa3072":    KeyAlgorithmRSA3072,
	"rsa-3072":   KeyAlgorithmRSA3072,
	"rsa4096":    KeyAlgorithmRSA4096,
	"rsa-4096":   KeyAlgorithmRSA4096,
	"ed25519":    KeyAlgorithmEd25519,
}

// CanonicalKeyAlgorithm resolves an operator- or wire-supplied algorithm
// spelling to its canonical name. Matching is case-insensitive and ignores
// surrounding whitespace. An empty algorithm resolves to KeyAlgorithmECDSAP256.
//
// It returns an *api.UnsupportedKeyAlgorithmError (matching
// ErrUnsupportedKeyAlgorithm) for a spelling this package does not generate.
func CanonicalKeyAlgorithm(algorithm string) (string, error) {
	canonical, ok := keyAlgorithmAliases[strings.ToLower(strings.TrimSpace(algorithm))]
	if !ok {
		return "", &api.UnsupportedKeyAlgorithmError{Algorithm: algorithm}
	}
	return canonical, nil
}

// SupportedKeyAlgorithms returns the canonical algorithm names in sorted order.
// Callers use it to report what an operator may ask for.
func SupportedKeyAlgorithms() []string {
	out := make([]string, 0, len(requesterKeyGenerators))
	for name := range requesterKeyGenerators {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// GenerateRequesterKey creates the key pair for a credential the REQUESTER will
// hold. Randomness is drawn from the go-xkms FIPS RNG resolver (Reader) on the
// Ed25519 path; on the ECDSA and RSA paths Go 1.26 discards the supplied reader
// and uses its own FIPS 140-3 DRBG. See the file comment.
//
// The returned crypto.Signer signs the CSR the go-xkms CA then signs; the
// returned x509.PublicKeyAlgorithm selects the PEM block type when the key is
// encoded for delivery (go-xkms sdk/go EncodePrivateKeyPEM).
//
// This is the only key-creation entry point in go-quicbaas. Read the file
// comment before adding a second one: for platform key material the answer is
// the go-xkms keystore, not this function.
//
// Errors: *api.UnsupportedKeyAlgorithmError (matching
// ErrUnsupportedKeyAlgorithm) for an algorithm this package does not generate,
// and *api.KeyGenerationError wrapping an RNG or crypto failure.
func GenerateRequesterKey(algorithm string) (crypto.Signer, x509.PublicKeyAlgorithm, error) {
	canonical, err := CanonicalKeyAlgorithm(algorithm)
	if err != nil {
		return nil, x509.UnknownPublicKeyAlgorithm, err
	}

	generate, ok := requesterKeyGenerators[canonical]
	if !ok {
		// Unreachable while keyAlgorithmAliases only names canonical values that
		// requesterKeyGenerators holds; reported rather than panicked so a future
		// alias added without a generator surfaces as an error, not a crash.
		return nil, x509.UnknownPublicKeyAlgorithm, &api.UnsupportedKeyAlgorithmError{Algorithm: algorithm}
	}

	signer, keyAlgorithm, err := generate(Reader())
	if err != nil {
		return nil, x509.UnknownPublicKeyAlgorithm, &api.KeyGenerationError{Algorithm: canonical, Cause: err}
	}
	return signer, keyAlgorithm, nil
}

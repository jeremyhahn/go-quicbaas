// Package deviceattest01 re-exports go-xkms's ACME device-attest-01
// challenge: the server-side verifier of a device's attestation statement
// (the "tpm" and "dice" formats) and the device-side Setup that answers the
// challenge with a TPM2_Certify statement. Every name is an alias of
// go-xkms's pkg/acme/challenges/device-attest-01; go-quicbaas adds nothing.
//
// Verify refuses every format it can verify with ErrKeyBindingRequired: the
// attested key must be compared with the CSR's, one request later, so an
// ACME server verifies through VerifyAttestation and authorizes issuance only
// after VerifiedAttestation.BindCSRKey has run on the CSR being finalized.
package deviceattest01

import (
	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

	da01 "github.com/jeremyhahn/go-xkms/pkg/acme/challenges/device-attest-01"
)

// ChallengeType is the ACME challenge type this package verifies.
const ChallengeType = xkmsacme.ACMEChallengeTypeDeviceAttest01

// AttestationFormat names the attestation statement's format.
type AttestationFormat = da01.AttestationFormat

// The attestation formats device-attest-01 verifies.
const (
	FormatTPM  = da01.FormatTPM
	FormatDICE = da01.FormatDICE
)

// AttestationObject is the CBOR attestation object a device submits.
type AttestationObject = da01.AttestationObject

// AttestationStatement is the attestation object's format-specific statement.
type AttestationStatement = da01.AttestationStatement

// AttestationRequest is the body a device answers the challenge with.
type AttestationRequest = da01.AttestationRequest

// AttestationVerifier is the ACME server's policy over an attestation: the
// checks it adds to the object and the roots the x5c chain must reach.
type AttestationVerifier = da01.AttestationVerifier

// VerifiedAttestation is what a verified attestation established; it
// authorizes nothing until BindCSRKey has run against the CSR's key.
type VerifiedAttestation = da01.VerifiedAttestation

// TrustedPlatformModule is the TPM a device answers a "tpm" challenge with.
type TrustedPlatformModule = da01.TrustedPlatformModule

// ChallengeClient is the shared, SSRF-safe HTTP client the challenge
// verifiers contact client-named targets through.
type ChallengeClient = xkmsacme.ACMEChallengeClient

// ChallengeClientConfig configures a ChallengeClient.
type ChallengeClientConfig = xkmsacme.ACMEChallengeClientConfig

var (
	// NewChallengeClient builds the shared challenge-validation client.
	NewChallengeClient = xkmsacme.NewACMEChallengeClient

	// Verify verifies a device-attest-01 challenge in the shape an ACME
	// challenge verifier is called in.
	Verify = da01.Verify

	// VerifyAttestation is Verify, returning what the attestation
	// established so the finalize step can bind it to the CSR.
	VerifyAttestation = da01.VerifyAttestation

	// ParseAttestationObject decodes a CBOR attestation object.
	ParseAttestationObject = da01.ParseAttestationObject

	// EncodeAttestationForSubmission encodes an attestation object for the
	// challenge response.
	EncodeAttestationForSubmission = da01.EncodeAttestationForSubmission

	// ValidateAttestationFormat reports whether a format is one this
	// challenge verifies.
	ValidateAttestationFormat = da01.ValidateAttestationFormat

	// Setup answers a device-attest-01 challenge with a "tpm" statement for
	// the TPM-resident certificate key.
	Setup = da01.Setup

	// TPMExtraData is the extraData a "tpm" statement's TPM2_Certify is made
	// over: the hash, under alg, of the key authorization.
	TPMExtraData = da01.TPMExtraData

	// DICENonce is the nonce a "dice" alias certificate echoes.
	DICENonce = da01.DICENonce
)

// The verifier's sentinel errors.
var (
	ErrInvalidKeyAlgorithm       = da01.ErrInvalidKeyAlgorithm
	ErrInvalidAttestationFormat  = da01.ErrInvalidAttestationFormat
	ErrInvalidAttestationObject  = da01.ErrInvalidAttestationObject
	ErrTPMAttestationInvalid     = da01.ErrTPMAttestationInvalid
	ErrAIKCertificateInvalid     = da01.ErrAIKCertificateInvalid
	ErrCertificateChainInvalid   = da01.ErrCertificateChainInvalid
	ErrKeyBindingRequired        = da01.ErrKeyBindingRequired
	ErrKeyBindingUnavailable     = da01.ErrKeyBindingUnavailable
	ErrNoVerifiedAttestation     = da01.ErrNoVerifiedAttestation
	ErrAttestationKeyUnavailable = da01.ErrAttestationKeyUnavailable
	ErrNoCertificateKey          = da01.ErrNoCertificateKey
	ErrCertifyFailed             = da01.ErrCertifyFailed
)

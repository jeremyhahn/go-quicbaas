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

package server

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"log/slog"

	"github.com/jeremyhahn/go-qrdb/pkg/ceremony"
	"github.com/jeremyhahn/go-qrdb/pkg/credentials"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// CeremonyWiringError indicates a failure constructing the bootstrap ceremony
// service stack (barrier, credential service, ceremony service).
type CeremonyWiringError struct {
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *CeremonyWiringError) Error() string {
	if e.Cause == nil {
		return "ceremony wiring: " + e.Stage + " failed"
	}
	return "ceremony wiring: " + e.Stage + " failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *CeremonyWiringError) Unwrap() error {
	return e.Cause
}

// CeremonyServices bundles the trio of services produced by the bootstrap
// ceremony wiring path: a credential service, a ceremony service, and the
// underlying core ceremony.CeremonyService for callers that need direct
// access (e.g., to register a CeremonyCA implementation).
type CeremonyServices struct {
	// Credential is the SDK-typed credential service used by the BaaS
	// embedded transport. Always non-nil when err is nil.
	Credential qrdbsdk.ServiceCredentialServicer

	// Ceremony is the SDK-typed ceremony service used by the BaaS embedded
	// transport. Always non-nil when err is nil.
	Ceremony qrdbsdk.ServiceCeremonyServicer

	// Core is the underlying ceremony.CeremonyService instance. Exposed so
	// later wiring stages (CA, threshold registration, external CA pools)
	// can reach it without re-constructing the stack.
	Core *ceremony.CeremonyService
}

// buildBootstrapCeremonyServices constructs the bootstrap ceremony service
// stack used by the BaaS embedded transport. The stack is intentionally
// barrier-less for the bootstrap window: it uses the manual credential
// strategy (in-memory, blocking retrieval) and a software-strategy
// StorageBarrier over a brand-new in-memory backend.
//
// The bootstrap barrier is separate from the data-plane barrier wired by
// initBarrier. Its sole purpose is to back the ceremony service's officer
// registry, share store, and pending-cert state during the init ceremony.
// Once the operator completes the ceremony and unseals the data plane, this
// bootstrap barrier remains in memory and is discarded on next restart.
//
// Returns a CeremonyServices bundle on success, or a CeremonyWiringError on
// any construction failure.
func buildBootstrapCeremonyServices(logger *slog.Logger, dataDir string) (*CeremonyServices, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// 1. Build a software-strategy StorageBarrier over an in-memory backend.
	//    The bootstrap ceremony only needs a barrier for its officer
	//    registry; the data plane uses its own barrier. We use the deferred
	//    software strategy because the operator's passphrase is not known
	//    at construction time — it arrives later via the ceremony's Initialize
	//    call when SOPin is supplied through the credential service.
	backend := qrdbsdk.NewMemoryBackend()
	softwareStrategy := qrdbsdk.NewDeferredSoftwareStrategy()
	barrier, err := qrdbsdk.NewStorageBarrier(
		logger,
		backend,
		qrdbsdk.BarrierConfig{
			RootKeyPath: "bootstrap/barrier/root-key",
		},
		softwareStrategy,
	)
	if err != nil {
		return nil, &CeremonyWiringError{Stage: "barrier", Cause: err}
	}

	// 2. Build a manual credential service. Manual strategy stores SO/User
	//    PINs in memory with blocking retrieval semantics, matching the
	//    bootstrap window where the operator submits PINs interactively.
	credCore, err := credentials.NewCredentialService(
		&credentials.Config{Strategy: credentials.StrategyManual},
		barrier,
		logger,
	)
	if err != nil {
		return nil, &CeremonyWiringError{Stage: "credential-core", Cause: err}
	}

	// 3. Build the core ceremony service with the bootstrap barrier and
	//    credential service.
	ceremonyCfg := &ceremony.CeremonyConfig{
		DataDir: dataDir,
	}
	ceremonyCore, err := ceremony.NewCeremonyService(ceremonyCfg, barrier, credCore, logger)
	if err != nil {
		return nil, &CeremonyWiringError{Stage: "ceremony-core", Cause: err}
	}

	// 4. Wrap each in the SDK service-layer types that the BaaS embedded
	//    transport consumes via baasTransport.ceremonySvc / .credentialSvc.
	return &CeremonyServices{
		Credential: qrdbservice.NewCredentialService(credCore),
		Ceremony:   qrdbservice.NewCeremonyService(ceremonyCore),
		Core:       ceremonyCore,
	}, nil
}

// SOCertSPKIThumbprintError indicates a failure computing the SPKI
// thumbprint for an SO certificate (e.g., the certificate PEM is malformed
// or contains no public key).
type SOCertSPKIThumbprintError struct {
	Reason string
}

// Error implements the error interface.
func (e *SOCertSPKIThumbprintError) Error() string {
	return "so cert spki thumbprint: " + e.Reason
}

// ErrSOCertEmpty indicates the SO certificate PEM input is empty.
var ErrSOCertEmpty = errors.New("so cert spki thumbprint: empty PEM input")

// ErrSOCertNoPEMBlock indicates the SO certificate PEM input does not contain
// a parseable PEM block.
var ErrSOCertNoPEMBlock = errors.New("so cert spki thumbprint: no PEM block")

// ErrSOCertNoSPKI indicates the SO certificate PEM block is empty or missing
// SubjectPublicKeyInfo bytes.
var ErrSOCertNoSPKI = errors.New("so cert spki thumbprint: no SubjectPublicKeyInfo bytes")

// SOCertSPKIThumbprint computes the SHA-256 SubjectPublicKeyInfo thumbprint
// of an SO certificate, returned as base64url-encoded text per RFC 7517 §4.8.
//
// The thumbprint is the SHA-256 digest of the DER-encoded SPKI bytes (RFC
// 5280 §4.1.2.7), NOT of the full certificate. SPKI binding is what allows
// admin recognition to survive certificate renewal: as long as the public
// key remains the same, a renewed SO cert presents the same thumbprint and
// is recognized as the same admin user.
//
// The input MUST be a single PEM-encoded X.509 certificate. Pass the cert
// PEM directly; do NOT attempt to parse and re-encode — the SPKI bytes are
// extracted from the inner certificate body without reconstructing them.
func SOCertSPKIThumbprint(certPEM []byte) (string, error) {
	if len(certPEM) == 0 {
		return "", ErrSOCertEmpty
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", ErrSOCertNoPEMBlock
	}
	// Parse the cert through x509 indirectly: we use the SDK helper that
	// exposes RawSubjectPublicKeyInfo. This is preferable to reinventing the
	// SPKI extraction in two places.
	spki, err := extractSPKI(block.Bytes)
	if err != nil {
		return "", &SOCertSPKIThumbprintError{Reason: err.Error()}
	}
	if len(spki) == 0 {
		return "", ErrSOCertNoSPKI
	}
	sum := sha256.Sum256(spki)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// extractSPKI returns the DER-encoded SubjectPublicKeyInfo bytes of the given
// certificate. It uses the parsed certificate's RawSubjectPublicKeyInfo field
// (RFC 5280 §4.1.2.7), which avoids re-encoding the public key and is stable
// across go versions.
func extractSPKI(certDER []byte) ([]byte, error) {
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}
	if len(cert.RawSubjectPublicKeyInfo) == 0 {
		return nil, ErrSOCertNoSPKI
	}
	// Defensive copy: callers must not mutate the certificate's internal
	// buffer. The slice is small (~272B for P-256, ~550B for RSA-4096).
	out := make([]byte, len(cert.RawSubjectPublicKeyInfo))
	copy(out, cert.RawSubjectPublicKeyInfo)
	return out, nil
}

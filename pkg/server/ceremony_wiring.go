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
	"log/slog"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"
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

// CeremonyServices bundles the services produced by the bootstrap ceremony
// wiring path: a barrier service, credential service, ceremony service, and
// the underlying core ceremony.CeremonyService for callers that need direct
// access (e.g., to register a CeremonyCA implementation).
type CeremonyServices struct {
	// Barrier is the BarrierServicer backed by the bootstrap ceremony's
	// in-memory StorageBarrier. It is injected into the BaaS embedded
	// transport via WithBarrierService so that all five API transports
	// (REST, gRPC, QUIC, MCP, Unix) share the same barrier instance with
	// no split-brain state. Always non-nil when err is nil.
	Barrier BaasBarrierService

	// Credential is the SDK-typed credential service used by the BaaS
	// embedded transport. Always non-nil when err is nil.
	Credential qrdbsdk.ServiceCredentialServicer

	// Ceremony is the SDK-typed ceremony service used by the BaaS embedded
	// transport. Always non-nil when err is nil.
	Ceremony qrdbsdk.ServiceCeremonyServicer

	// Core is the underlying ceremony.CeremonyService instance. Exposed so
	// later wiring stages (CA, threshold registration, external CA pools)
	// can reach it without re-constructing the stack.
	Core *qrdbsdk.CeremonyService
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
// trustDomain is the SPIFFE trust domain the node's mTLS authenticator pins;
// qrdb refuses every ceremony issuance until one is installed.
//
// Returns a CeremonyServices bundle on success, or a CeremonyWiringError on
// any construction failure.
func buildBootstrapCeremonyServices(logger *slog.Logger, dataDir, trustDomain string) (*CeremonyServices, error) {
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
	credCore, err := qrdbsdk.NewVersionedCredentialService(
		&qrdbsdk.CredentialConfig{Strategy: qrdbsdk.CredentialStrategyManual},
		barrier,
		logger,
	)
	if err != nil {
		return nil, &CeremonyWiringError{Stage: "credential-core", Cause: err}
	}

	// 3. Build the core ceremony service with the bootstrap barrier and
	//    credential service.
	ceremonyCfg := &qrdbsdk.CeremonyConfig{
		DataDir: dataDir,
	}
	ceremonyCore, err := qrdbsdk.NewCeremonyService(ceremonyCfg, barrier, logger)
	if err != nil {
		return nil, &CeremonyWiringError{Stage: "ceremony-core", Cause: err}
	}
	ceremonyCore.SetTrustDomain(trustDomain)

	// 4. Wrap each in the SDK service-layer types that the BaaS embedded
	//    transport consumes via baasTransport.barrierSvc / .ceremonySvc /
	//    .credentialSvc.
	return &CeremonyServices{
		Barrier:    qrdbsdk.NewBarrierService(barrier),
		Credential: qrdbsdk.NewCredentialService(credCore),
		Ceremony:   qrdbsdk.NewServiceCeremonyService(ceremonyCore),
		Core:       ceremonyCore,
	}, nil
}

// newBootstrapCeremonyClient builds the in-process qrdb SDK client that
// exposes the bootstrap ceremony service stack (barrier, ceremony, and
// credential servicers) through the canonical qrdb embedded transport.
//
// It exists for the seal-disabled deployment state: the embedded qrdb node
// builds its seal-layer services (and registers them on its own embedded
// transport) only when cfg.Seal.Enabled is true. Without a seal barrier the
// node's SDK client answers every barrier/ceremony/credential call with a
// NilServiceError, which would leave the whole multi-protocol surface (REST,
// gRPC, QUIC, MCP, Unix, and the SDK embedded transport) without a working
// bootstrap-window ceremony. This client wraps the SAME servicer instances
// stored in s.ceremonyServices, so every transport operates on one barrier
// with no split-brain state.
//
// The KV and Admin servicers come from the node's service graph; the qrdb
// embedded transport requires both. pinBackend is the canonical
// pin.PINBackend the daemon verifies SO / user PINs against; wiring it via
// qrdbembedded.WithPINBackend is what makes the transport-level barrier
// verbs honor the SOPin / UserPin fields of BarrierInitializeRequest (and
// verify the SO PIN on rekey / recovery-generate) instead of silently
// dropping them. The same backend is attached to cs.Core, the selected
// ceremony core on either storage path, because Initialize provisions the
// mandatory SO / User PINs on it; with a nil pinBackend the core refuses
// Initialize with qrdb's ErrBackendNotConfigured. ca is
// the CeremonyCA the transport hands `qrdb init` (CeremonyInitialize takes
// its CA from the transport, not from the core's SetCA); nil leaves the
// ceremony on the BYO-TLS path. All
// construction failures are returned as CeremonyWiringError with a stage
// identifying the failing step.
func newBootstrapCeremonyClient(
	kv qrdbsdk.KVServicer,
	admin qrdbsdk.AdminServicer,
	cs *CeremonyServices,
	pinBackend qrdbsdk.PINBackend,
	ca qrdbsdk.CeremonyCA,
) (*qrdbsdk.Client, error) {
	if cs == nil || cs.Barrier == nil || cs.Ceremony == nil || cs.Credential == nil {
		return nil, &CeremonyWiringError{Stage: "bootstrap-client services"}
	}

	// cs.Core is whichever ceremony core startService selected: the
	// bootstrap stack's own core on the memory fallback, or the persistent
	// data-plane core. Initialize provisions the SO/User PINs on that core's
	// backend, so the server's PIN backend is attached here, once, for both
	// storage paths.
	if pinBackend != nil && cs.Core != nil {
		cs.Core.SetPINBackend(pinBackend)
	}

	opts := []qrdbembedded.Option{
		qrdbembedded.WithBarrierService(cs.Barrier),
		qrdbembedded.WithCeremonyService(cs.Ceremony),
		qrdbembedded.WithCredentialService(cs.Credential),
	}
	if pinBackend != nil {
		opts = append(opts, qrdbembedded.WithPINBackend(pinBackend))
	}
	if ca != nil {
		opts = append(opts, qrdbembedded.WithCeremonyCA(ca))
	}
	tp, err := qrdbembedded.New(kv, admin, opts...)
	if err != nil {
		return nil, &CeremonyWiringError{Stage: "bootstrap-transport", Cause: err}
	}

	// Connect is a no-op for the qrdb embedded transport (it never dials
	// anything), so the client is usable immediately.
	client, err := qrdbsdk.NewClient(tp)
	if err != nil {
		return nil, &CeremonyWiringError{Stage: "bootstrap-client", Cause: err}
	}
	return client, nil
}

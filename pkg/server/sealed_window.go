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

package server

import (
	"context"
	"errors"
	"net"
	"runtime"
	"strconv"
	"time"

	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Sealed-window boot phase (qrdb's sealed-window model on quicbaas's
// transports)
//
// When the embedded qrdb node boots with seal enabled and no auto-unseal
// material (no WithSealPassphrase, no hardware/cloud auto-unseal strategy),
// the node comes up SEALED: Client() is nil and only the seal-layer services
// (Barrier / Ceremony / Credential) exist. Previously quicbaasd would panic
// at the first GetKVStore call. Instead, initialize() now enters the
// sealed-window phase:
//
//  1. The node's OWN seal services are wired into s.ceremonyServices and an
//     in-process qrdb SDK client (sealedWindowClient) is composed over them
//     via the embedded transport, so every transport serves the SAME barrier
//     instance the node will complete its boot with (officer registrations
//     and enclave-held shares made during the window survive into the full
//     boot, exactly matching qrdb's node.ServeSealedWindow semantics).
//  2. All five quicbaas transports come up serving ONLY the canonical
//     sealed-window surface: each sealed-phase listener carries a
//     constant-sealed probe plus the allowlist derived from the single
//     table in pkg/api/sealedwindow, so nothing off-allowlist is reachable
//     for the listener's whole lifetime. Authorization gates are NOT
//     constructed in this phase — there is no OIDC provider yet — the
//     sealed-window deny-everything-off-allowlist gate IS the control, and
//     the allowlisted handlers enforce their own credentials (officer cert
//     + nonce checks), exactly as on a qrdb node.
//  3. initialize() blocks, polling the node's IsSealed. When an operator
//     drives the barrier unsealed through ANY transport (passphrase unseal,
//     Shamir share quorum, recovery keys), the loop tears the sealed-phase
//     listeners down, calls node.Unseal with empty credentials to complete
//     the node boot (BuildServices + client — whose unified listeners bind
//     the very ports the sealed listeners held, which is why teardown comes
//     first, #139), and the normal full boot continues — building the real
//     service graph and restarting all transports with the full surface plus
//     the probe-driven sealed gates (so a post-boot re-seal immediately
//     re-narrows the surface).
// =============================================================================

// sealedWindowPollInterval is how often the sealed-window wait loop samples
// the node's sealed state.
const sealedWindowPollInterval = 250 * time.Millisecond

// EmbeddedSealedBootError is returned when an embedded-mode (library) server
// is constructed over a sealed qrdb node. Embedded mode has no transport
// listeners to drive the unseal through and Start must not block the
// embedder, so the boot is refused with a typed error instead of the former
// nil-client panic. Embedders supply WithSealPassphrase (or a hardware
// auto-unseal strategy) to boot a seal-enabled node in embedded mode.
type EmbeddedSealedBootError struct{}

// Error implements the error interface.
func (e *EmbeddedSealedBootError) Error() string {
	return "sealed window: embedded mode cannot serve a sealed-window boot; " +
		"supply server.WithSealPassphrase or an auto-unseal seal strategy"
}

// SealedWindowBootError describes a failure while entering, serving, or
// completing the sealed-window boot phase.
type SealedWindowBootError struct {
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *SealedWindowBootError) Error() string {
	if e.Cause == nil {
		return "sealed window: " + e.Stage + " failed"
	}
	return "sealed window: " + e.Stage + " failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *SealedWindowBootError) Unwrap() error { return e.Cause }

// SealedWindowShutdownError is returned when the daemon is shut down while
// still waiting for the operator to unseal the barrier.
type SealedWindowShutdownError struct{}

// Error implements the error interface.
func (e *SealedWindowShutdownError) Error() string {
	return "sealed window: shutdown requested before the barrier was unsealed"
}

// newSealedWindowClient composes the in-process qrdb SDK client for the
// sealed-window phase: the node's OWN Barrier / Ceremony / Credential
// servicers over the canonical qrdb embedded transport, with lazy KV / Admin
// proxies that fail closed until Unseal completes the boot (see
// sealed_window_servicers.go). Every quicbaas transport then serves barrier,
// ceremony, and credential verbs from the SAME instances the node reuses
// post-unseal — no split-brain state.
func newSealedWindowClient(s *Server, cs *CeremonyServices) (*qrdbsdk.Client, error) {
	if cs == nil || cs.Barrier == nil || cs.Ceremony == nil || cs.Credential == nil {
		return nil, &SealedWindowBootError{Stage: "node seal services"}
	}

	opts := []qrdbembedded.Option{
		qrdbembedded.WithBarrierService(cs.Barrier),
		qrdbembedded.WithCeremonyService(cs.Ceremony),
		qrdbembedded.WithCredentialService(cs.Credential),
	}
	// Wire the canonical pin.PINBackend so barrier verbs served during the
	// sealed window honor the SOPin / UserPin fields of the canonical
	// requests (provision on initialize, verify on rekey / recovery) against
	// the SAME backend the node verifies post-unseal.
	if s.pinBackend != nil {
		opts = append(opts, qrdbembedded.WithPINBackend(s.pinBackend))
	}
	// CeremonyInitialize takes its CA from the transport, not from the
	// ceremony core: hand it the same late-bound platform CA the node was
	// composed with, so `qrdb init` during the sealed window can issue.
	if s.ceremonyCA != nil {
		opts = append(opts, qrdbembedded.WithCeremonyCA(s.ceremonyCA))
	}
	tp, err := qrdbembedded.New(
		newSealedKVServicerProxy(s),
		newSealedAdminServicerProxy(s),
		opts...,
	)
	if err != nil {
		return nil, &SealedWindowBootError{Stage: "sealed embedded transport", Cause: err}
	}

	client, err := qrdbsdk.NewClient(tp)
	if err != nil {
		return nil, &SealedWindowBootError{Stage: "sealed sdk client", Cause: err}
	}
	return client, nil
}

// buildSealedWindowFacade constructs the minimal service.QuicBaaS facade the
// sealed-phase transport servers are composed over. It is a REAL facade over
// a REAL qrdb SDK client (the sealed-window client): the barrier / ceremony /
// credential surfaces work, everything data-plane fails closed with the
// typed SealedNodeUnavailableError — and is unreachable anyway because the
// sealed-phase listeners deny everything off-allowlist.
func (s *Server) buildSealedWindowFacade() (service.QuicBaaS, *ShardedClient, error) {
	embeddedCfg := &config.QRDBConfig{Transport: config.QRDBTransportEmbedded}
	sharded := &ShardedClient{
		factory:    newTransportFactory(embeddedCfg, nil, nil),
		nodeClient: s.sealedWindowClient,
		// The sealed-window client's lifetime is owned by the sealed-window
		// phase (closed in stopSealedTransports), not by the ShardedClient.
		nodeOwned: true,
	}

	svcConfig := &service.ServiceConfig{
		NodeID:          s.config.Raft.NodeID,
		Mode:            s.config.Server.Mode,
		Version:         "1.0.0",
		Shards:          []uint64{1},
		EnableAuth:      false,
		EnableMetrics:   false,
		EnableScheduler: false,
	}

	svc, err := service.NewServiceBuilder().
		WithLogger(logger.NewAdapter(s.logger)).
		WithConfig(svcConfig).
		WithQRDBClient(sharded).
		Build()
	if err != nil {
		return nil, nil, &SealedWindowBootError{Stage: "sealed facade", Cause: err}
	}
	return svc, sharded, nil
}

// runSealedWindow serves the sealed-window boot phase and blocks until the
// operator drives the barrier unsealed (completing the node boot before
// returning) or shutdown is requested. On return with nil error the node is
// fully booted (Client() non-nil) and every sealed-phase listener has been
// torn down; initialize() then proceeds with the normal full boot.
func (s *Server) runSealedWindow() error {
	if s.config.Server.Embedded {
		return &EmbeddedSealedBootError{}
	}

	s.logger.Warn("Embedded qrdb node booted SEALED: entering sealed-window mode. " +
		"Only barrier/ceremony/health verbs are reachable until the barrier is unsealed.")

	// 1. Wire the node's own seal-layer services. These are the instances
	// the node reuses when Unseal completes the boot, so ceremony enclave
	// state made during the window survives into the full boot.
	svcs := s.getQRDBNode().Services()
	if svcs == nil || svcs.Barrier == nil || svcs.Ceremony == nil || svcs.Credential == nil {
		return &SealedWindowBootError{Stage: "node seal services"}
	}
	s.ceremonyServices = &CeremonyServices{
		Barrier:    svcs.Barrier,
		Credential: svcs.Credential,
		Ceremony:   svcs.Ceremony,
	}
	// One listener probe for every sealed-phase transport, read live: the
	// listener gates refuse every request once the barrier unseals, before
	// the full service graph exists.
	s.sealedListenerProbe = qrdbsdk.NewSealedWindowListenerProbe(s.SealedWindowProbe(), svcs.Ceremony)

	client, err := newSealedWindowClient(s, s.ceremonyServices)
	if err != nil {
		return err
	}
	s.sealedWindowClient = client

	// 2. Compose the minimal facade and bring up all five transports with
	// the constant-sealed gates.
	facade, _, err := s.buildSealedWindowFacade()
	if err != nil {
		_ = s.stopSealedTransports() // abort path: the causal error wins; failures are Warn-logged inside
		return err
	}
	s.sealedWindowFacade = facade

	if err := s.startSealedGRPCServer(); err != nil {
		_ = s.stopSealedTransports() // abort path: the causal error wins; failures are Warn-logged inside
		return err
	}
	if err := s.startSealedRESTServer(); err != nil {
		_ = s.stopSealedTransports() // abort path: the causal error wins; failures are Warn-logged inside
		return err
	}

	s.logger.Info("Sealed-window listeners serving the canonical sealed surface; waiting for unseal")

	// 3. Block until the barrier is driven unsealed through any transport.
	if err := s.awaitUnseal(); err != nil {
		_ = s.stopSealedTransports() // abort path: the causal error wins; failures are Warn-logged inside
		return err
	}

	// The unseal and a shutdown can race: awaitUnseal returns on whichever
	// it observes first, and a shutdown that lands just after the unseal
	// was observed would otherwise commit this boot to the full bring-up
	// with the operator already gone. Re-check before committing. Past this
	// point the boot is committed: node.Unseal and the remainder of
	// initialize() are not shutdown-aware (a deliberate bound — making the
	// whole boot pipeline cancellable is an architectural change), so a
	// shutdown arriving later waits for the boot to complete and then tears
	// it down through the normal path.
	select {
	case <-s.shutdownChan:
		_ = s.stopSealedTransports() // abort path: the causal error wins; failures are Warn-logged inside
		return &SealedWindowShutdownError{}
	default:
	}

	// 4. Tear the sealed-phase listeners down BEFORE completing the node
	// boot. Completing the boot first can never work: the node's own
	// unified-server listeners bind the same configured ports the sealed
	// listeners still hold, so Unseal failed deterministically with
	// "address already in use" and the daemon exited instead of booting
	// (#139). The fail-closed property survives the reorder — between this
	// teardown and the full boot's listeners nothing is listening at all,
	// which is strictly narrower than the sealed allowlist surface. The
	// operator's unseal response has already been drained by the graceful
	// stop inside stopSealedTransports. A teardown failure aborts the boot:
	// a sealed listener that did not provably release its port would turn
	// the deterministic collision this reorder fixes into a racy one.
	if err := s.stopSealedTransports(); err != nil {
		return &SealedWindowBootError{Stage: "sealed transport teardown", Cause: err}
	}

	// 5. Complete the node boot. The barrier is already unsealed, so Unseal
	// with empty credentials proceeds directly to the data-plane build; the
	// full boot then re-creates every transport with the complete service
	// graph and the live probe-driven sealed gates. startService rewires
	// s.ceremonyServices from the node (same instances).
	if err := s.getQRDBNode().Unseal(context.Background(), qrdbsdk.Credentials{}); err != nil {
		return &SealedWindowBootError{Stage: "complete node boot", Cause: err}
	}

	s.logger.Info("Barrier unsealed: sealed window closed, completing full boot")
	return nil
}

// awaitUnseal polls the node's sealed state until it reports unsealed or
// shutdown is requested. Channel-based (ticker + shutdownChan select), no
// sleeps.
func (s *Server) awaitUnseal() error {
	ticker := time.NewTicker(sealedWindowPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !s.getQRDBNode().IsSealed() {
				return nil
			}
		case <-s.shutdownChan:
			return &SealedWindowShutdownError{}
		}
	}
}

// startSealedGRPCServer brings up the sealed-phase gRPC server on the same
// TCP + Unix listeners the full boot will use, serving ONLY the qrdb.v1
// composition surface and the quicbaas Barrier / Ceremony mirrors behind the
// canonical sealed-window interceptors (qrdb's own interceptors, consumed
// via the SDK re-export). The Unix-over-gRPC transport shares the server,
// so this single bring-up covers both.
func (s *Server) startSealedGRPCServer() error {
	creds := grpcapi.ServerTLSCredentialsFromConfig(s.tlsConfig)

	allow := sealedwindow.GRPCFullMethodAllowlist()
	probe := s.sealedListenerProbe

	grpcOpts := []ggrpc.ServerOption{
		ggrpc.Creds(creds),
		ggrpc.MaxRecvMsgSize(10 * 1024 * 1024),
		ggrpc.MaxSendMsgSize(10 * 1024 * 1024),
		ggrpc.MaxConcurrentStreams(256),
		ggrpc.NumStreamWorkers(uint32(runtime.NumCPU())),
		ggrpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 15 * time.Minute,
			MaxConnectionAge:  30 * time.Minute,
			Time:              5 * time.Minute,
			Timeout:           20 * time.Second,
		}),
		ggrpc.ChainUnaryInterceptor(
			grpcapi.UnaryTimeoutInterceptor(30*time.Second),
			grpcapi.UnaryLoggingInterceptor(s.logger),
			grpcapi.UnaryRecoveryInterceptor(s.logger),
			qrdbsdk.SealedWindowListenerUnaryInterceptor(probe, allow),
			// The qrdb.v1 ceremony servicer cannot reach the platform
			// ceremony CA until go-qrdb #1129; refuse its CA-needing modes.
			ceremonyCARefusalUnaryInterceptor(),
		),
		ggrpc.ChainStreamInterceptor(
			grpcapi.StreamLoggingInterceptor(s.logger),
			grpcapi.StreamRecoveryInterceptor(s.logger),
			qrdbsdk.SealedWindowListenerStreamInterceptor(probe, allow),
		),
	}

	grpcServer := ggrpc.NewServer(grpcOpts...)

	// Barrier / Ceremony quicbaas mirrors + the qrdb.v1 composition surface.
	// Both resolve their servicers through buildBarrierCeremonyServices /
	// buildQRDBRegisterOptions, which serve the sealed-window client and the
	// node's seal services in this phase.
	s.registerBarrierCeremonyServices(grpcServer)
	s.registerQRDBService(grpcServer)

	s.grpcServer = grpcServer

	listenersStarted := 0
	if s.config.Server.Unix.Socket != "" {
		if err := s.startUnixSocketListener(grpcServer); err != nil {
			return &SealedWindowBootError{Stage: "sealed grpc unix listener", Cause: err}
		}
		listenersStarted++
	}
	if s.config.Server.GRPC.Enabled {
		tcpAddress := sealedListenAddress(s.config, s.config.Server.GRPC.Port)
		tcpListener, err := listenFunc("tcp", tcpAddress)
		if err != nil {
			return &SealedWindowBootError{Stage: "sealed grpc tcp listener", Cause: err}
		}
		s.grpcListener = tcpListener
		go func() {
			s.logger.Infof("Sealed-window gRPC server listening on TCP %s", tcpAddress)
			if serveErr := grpcServer.Serve(tcpListener); serveErr != nil {
				s.logger.WithError(serveErr).Error("Sealed-window gRPC server TCP error")
			}
		}()
		listenersStarted++
	}
	if listenersStarted == 0 {
		return &SealedWindowBootError{Stage: "sealed grpc listeners", Cause: nil}
	}
	return nil
}

// stopSealedTransports tears down every sealed-phase listener and releases
// the sealed-window client + facade so the full boot can re-create the
// transports over the complete service graph. Idempotent: nil components
// are skipped. The joined error reports every error-returning component
// that failed to release (gRPC's stop API is void by design — GracefulStop
// is synchronous and its listeners are closed by it); a non-nil return
// means a sealed-phase listener or resource may still be live, so a caller
// about to rebind the configured ports must treat it as fatal rather than
// proceed into the #139 collision.
func (s *Server) stopSealedTransports() error {
	var errs []error
	if s.getRESTServer() != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := s.stopRESTServer(ctx); err != nil {
			s.logger.WithError(err).Warn("Failed to stop sealed-window REST server")
			errs = append(errs, err)
		}
		cancel()
		s.setRESTServer(nil)
	}
	if s.grpcServer != nil {
		s.stopGRPCServer()
		s.grpcServer = nil
		s.grpcListener = nil
		s.grpcUnixListener = nil
	}
	if s.sealedWindowFacade != nil {
		if err := s.sealedWindowFacade.Close(); err != nil {
			s.logger.WithError(err).Warn("Failed to close sealed-window facade")
			errs = append(errs, err)
		}
		s.sealedWindowFacade = nil
	}
	if s.sealedWindowClient != nil {
		if err := s.sealedWindowClient.Close(); err != nil {
			s.logger.WithError(err).Warn("Failed to close sealed-window client")
			errs = append(errs, err)
		}
		s.sealedWindowClient = nil
	}
	// The full boot's startService rewires ceremonyServices from the node.
	s.ceremonyServices = nil
	return errors.Join(errs...)
}

// sealedListenAddress resolves the listen address for a sealed-window
// listener: the dedicated sealed-phase bind host (loopback unless
// Server.SealedBindHost opts into a routable address) joined with the
// protocol port. Server.Host and the per-protocol BindAddress overrides
// deliberately do not apply while sealed; they govern only the post-unseal
// full-boot listeners.
func sealedListenAddress(cfg *config.Config, port int) string {
	return net.JoinHostPort(cfg.Server.ResolveSealedBindHost(), strconv.Itoa(port))
}

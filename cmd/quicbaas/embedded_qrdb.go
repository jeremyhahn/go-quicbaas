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

package main

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// EmbeddedQRDBWiringError is returned when the CLI cannot construct the
// QRDBEmbeddedServicers payload needed by WithEmbeddedQRDB. The Stage
// field identifies which dependency was missing or which call failed.
type EmbeddedQRDBWiringError struct {
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *EmbeddedQRDBWiringError) Error() string {
	if e.Cause == nil {
		return "embedded qrdb wiring: " + e.Stage + " unavailable"
	}
	return "embedded qrdb wiring: " + e.Stage + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *EmbeddedQRDBWiringError) Unwrap() error {
	return e.Cause
}

// buildEmbeddedQRDBServicers assembles the QRDBEmbeddedServicers value that
// the SDK's WithEmbeddedQRDB option expects.
//
// Node path (qrdbNode is running): KV and Admin come directly from the node's
// service graph via GetQRDBKVServicer/GetQRDBAdminServicer; the node's SDK
// client provides barrier, ceremony, credential, and tenant servicers via
// its embedded transport. This path no longer relies on the legacy BaasTransport
// or EmbeddedTransportForShard/BaasKVAdapter/BaasAdminAdapter machinery.
//
// The function is safe to call only after the embedded server has been
// started — the caller must hold a valid *embeddedServer reference.
func buildEmbeddedQRDBServicers(es *embeddedServer) (quicbaas.QRDBEmbeddedServicers, error) {
	if es == nil || es.server == nil {
		return quicbaas.QRDBEmbeddedServicers{}, &EmbeddedQRDBWiringError{Stage: "embedded server"}
	}

	// Node path: use the real qrdb engine node's service graph directly.
	// The node's KV and Admin servicers are the canonical service-layer
	// implementations backed by the in-process engine. All barrier/ceremony/
	// credential operations route through the node's SDK client embedded
	// transport without any intermediate BaasTransport layer.
	kv := es.server.GetQRDBKVServicer()
	admin := es.server.GetQRDBAdminServicer()

	if kv == nil || admin == nil {
		return quicbaas.QRDBEmbeddedServicers{}, &EmbeddedQRDBWiringError{Stage: "qrdb node services"}
	}

	servicers := quicbaas.QRDBEmbeddedServicers{
		KV:    kv,
		Admin: admin,
	}

	// Wire the user store and the unified-login UserAuthFactor service
	// INSTANCE from the node. Enrollment of user-login factors
	// (Security().Auth().Factors().Add) resolves the caller's subject to a
	// uint64 user id via this user store and writes the factor into THIS
	// UserAuthFactor instance — the exact store the login ceremony resolves
	// against — so an enrolled factor is immediately login-resolvable.
	if userSvc := es.server.GetQRDBUserServicer(); userSvc != nil {
		servicers.User = userSvc
	}
	if uafSvc := es.server.GetQRDBUserAuthFactorServicer(); uafSvc != nil {
		servicers.UserAuthFactor = uafSvc
	}

	// Wire ceremony and credential services from the node's bootstrap
	// ceremony stack. These are the real services backed by the qrdb node.
	if ceremonyStack := es.server.GetCeremonyServices(); ceremonyStack != nil {
		servicers.Ceremony = ceremonyStack.Ceremony
		servicers.Credential = ceremonyStack.Credential
	}

	// Wire the REAL data-plane barrier (persistent PebbleDB-backed
	// StorageBarrier). Security().Barrier() must operate on this barrier —
	// NOT the bootstrap ceremony's ephemeral in-memory barrier.
	//
	// On the node path the barrier is exposed via GetCeremonyServices().Barrier
	// (populated from the node's seal subsystem in startService). Fall back to
	// nil (barrier operations return NotSupported) when seal is disabled.
	if dp := es.server.GetDataPlaneBarrierService(); dp != nil {
		servicers.Barrier = dp
	} else if ceremonyStack := es.server.GetCeremonyServices(); ceremonyStack != nil && ceremonyStack.Barrier != nil {
		servicers.Barrier = ceremonyStack.Barrier
	}

	return servicers, nil
}

// embeddedQRDBOption returns the SDK option that wires the in-process qrdb
// sub-client. Returns a wrapped error when buildEmbeddedQRDBServicers
// fails so the caller can surface the wiring failure to the user instead
// of silently dropping back to a no-op qrdb client.
func embeddedQRDBOption(es *embeddedServer) (quicbaas.Option, error) {
	servicers, err := buildEmbeddedQRDBServicers(es)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "embeddedQRDBOption",
			"failed to build embedded qrdb servicers")
	}
	return quicbaas.WithEmbeddedQRDB(servicers), nil
}

// getServerForEmbeddedWiring is a compile-time assertion that *server.Server
// exposes the accessor surface the CLI needs. This avoids the previous
// dependency on server.NewBaasKVAdapter, server.NewBaasAdminAdapter,
// sharded.EmbeddedTransportForShard, and baasTransport.
var _ interface {
	GetQRDBKVServicer() quicbaas.QRDBKVServicer
	GetQRDBAdminServicer() quicbaas.QRDBAdminServicer
	GetQRDBUserServicer() quicbaas.QRDBUserServicer
	GetQRDBUserAuthFactorServicer() quicbaas.QRDBUserAuthFactorServicer
	GetCeremonyServices() *server.CeremonyServices
	GetDataPlaneBarrierService() server.BaasBarrierService
} = (*server.Server)(nil)

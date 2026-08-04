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
	"fmt"

	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// acmeShardID is the qrdb shard whose KVStore backs ACME persistence. ACME
// state (accounts / orders / authorizations / certificates) is low-volume
// control-plane data, so it shares the default shard rather than being
// distributed.
const acmeShardID uint64 = 0

// buildACMEService composes the go-xkms ACME (RFC 8555) servicer that backs
// Security().ACME(). It sources every dependency from the existing server
// wiring — the same TrustedCA that backs Security().Certificate() (asserted to
// the TCG CA interface the ACME handlers require), the server's qrdb KVStore
// (so ACME persistence shares the configured datastore), and the externally
// reachable base URL derived from the REST listener — rather than standing up
// any parallel infrastructure.
//
// It returns (nil, nil) when ACME cannot be composed because a prerequisite is
// absent (no CA, no qrdb client, no resolvable base URL). That is not an error:
// the QuicBaaS facade treats an absent ACMEServicer as "ACME not configured".
// A non-nil error is returned only when the CA cannot satisfy the TCG
// interface or the xkms RestService construction fails.
func (s *Server) buildACMEService(caAdapter *caServiceAdapter) (service.ACMEServicer, error) {
	if caAdapter == nil || s.xkmsCA == nil || s.qrdbClient == nil {
		return nil, nil
	}

	// The ACME RFC 8555 handlers require the TCG-extended CA surface. The
	// quicbaas TrustedCA is created by pkg/ca.New, whose concrete type
	// implements TCGCA — the same assertion xkmsd performs in cmd/xkmsd/acme.go.
	tcgCA, ok := s.xkmsCA.(xkmsacme.CACertificateAuthority)
	if !ok {
		return nil, &ACMECAAssertError{}
	}

	baseURI := s.acmeBaseURI()
	if baseURI == "" {
		return nil, nil
	}

	// ACME persistence shares the server's qrdb datastore via the SDK
	// KVStore, exactly as xkmsd shares its durable engine. No separate object
	// store is created.
	shardClient, err := s.qrdbClient.ForShard(acmeShardID)
	if err != nil {
		return nil, &ACMEStorageError{Cause: err}
	}
	storageFactory, err := xkmsacme.NewACMEStorageFactoryFromKVStore(shardClient.KVStore())
	if err != nil {
		return nil, &ACMEStorageError{Cause: err}
	}

	acmeSvc, err := service.NewACMEService(&service.ACMEServiceParams{
		BaseRESTURI:       baseURI,
		CA:                tcgCA,
		StorageFactory:    storageFactory,
		IssueCA:           caAdapter,
		CertResolver:      caAdapter,
		OrderPreSignHooks: s.buildClusterPreSignHooks(),
		ClusterCACN:       s.clusterACMEIssuerCN(),
	})
	if err != nil {
		return nil, err
	}
	return acmeSvc, nil
}

// acmeBaseURI derives the externally reachable ACME base URL from the REST
// listener configuration. The RFC 8555 directory is advertised at
// "<base>/acme/directory". Returns an empty string when REST is disabled or no
// host/port can be resolved (ACME has no transport of its own — it rides REST).
func (s *Server) acmeBaseURI() string {
	if !s.config.Server.REST.Enabled || s.config.Server.REST.Port <= 0 {
		return ""
	}
	host := s.config.Server.Host
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}
	return fmt.Sprintf("https://%s:%d", host, s.config.Server.REST.Port)
}

// ACMECAAssertError is returned when the configured TrustedCA does not
// implement the TCG-extended CA interface the ACME handlers require.
type ACMECAAssertError struct{}

func (e *ACMECAAssertError) Error() string {
	return "server: ACME requires a TCG-capable certificate authority"
}

// ACMEStorageError wraps a failure to construct the qrdb-backed ACME storage
// factory.
type ACMEStorageError struct {
	Cause error
}

func (e *ACMEStorageError) Error() string {
	return "server: failed to construct ACME storage factory: " + e.Cause.Error()
}

func (e *ACMEStorageError) Unwrap() error { return e.Cause }
